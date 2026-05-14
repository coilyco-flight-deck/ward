package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

// hookCommand groups Claude Code hook entry points. Subcommands read a
// hook payload on stdin and either pass through (exit 0) or block with
// a routing hint on stderr (exit 2).
//
// Today this is wired only for PreToolUse on the Bash tool. The shape
// is extensible to other hook events (PostToolUse, UserPromptSubmit)
// when there is a reason to gate on them.
func hookCommand() *cli.Command {
	return &cli.Command{
		Name:  "hook",
		Usage: "Claude Code hook entry points.",
		Commands: []*cli.Command{
			{
				Name:  "pre-tool-use",
				Usage: "PreToolUse hook for the Bash tool. Routes bare-binary invocations through the local guard wrapper with a recovery hint, and rejects guard-binary invocations resolving outside their canonical install paths.",
				Action: func(_ context.Context, _ *cli.Command) error {
					return runPreToolUse(os.Stdin, os.Stderr, os.Getenv, exec.LookPath)
				},
			},
		},
	}
}

// pathLookup mirrors exec.LookPath. Indirected for tests.
type pathLookup func(name string) (string, error)

// guardBinaryPaths is the canonical install-path allow-list per known
// guard binary. The PreToolUse hook rejects any bare invocation of one
// of these binaries that does not resolve to a listed path. Required
// by default per the agent-guard max-security posture (#14). No
// opt-out for v0; #13 (externalize routing table) carries the future
// per-consumer override path.
var guardBinaryPaths = map[string][]string{
	"agent-guard": {
		"/opt/homebrew/bin/agent-guard",
		"/usr/local/bin/agent-guard",
		"/home/linuxbrew/.linuxbrew/bin/agent-guard",
	},
	"coily": {
		"/opt/homebrew/bin/coily",
		"/usr/local/bin/coily",
		"/home/linuxbrew/.linuxbrew/bin/coily",
	},
}

// hookInput is the subset of Claude Code's PreToolUse hook payload we
// read. Unknown fields are ignored. We treat tool_input as a free-form
// map so a non-Bash tool name passes through cleanly.
type hookInput struct {
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	CWD       string                 `json:"cwd"`
}

// runPreToolUse is the testable core. Reads a hook payload from in,
// emits any block reason to errOut, and returns nil on pass-through.
// Returns a cli.Exit error with code 2 on block, which urfave/cli
// surfaces as the process exit code.
//
// Failure modes (unparseable JSON, missing fields, unknown tool, no
// matching route) all pass through. The hook is best-effort hint
// surface, never a hard gate. coily lockdown / agent-guard's own
// permissions.deny stays responsible for hard denial.
func runPreToolUse(in io.Reader, errOut io.Writer, getenv func(string) string, lookup pathLookup) error {
	// Best-effort hint surface: stdin-read failures and unparseable
	// payloads pass through silently. Hard denial belongs to
	// permissions.deny, not this hook.
	data, _ := io.ReadAll(in) //nolint:errcheck // intentional: see func doc
	if len(data) == 0 {
		return nil
	}
	var hi hookInput
	if json.Unmarshal(data, &hi) != nil {
		return nil //nolint:nilerr // intentional: malformed payload passes through silently
	}
	if hi.ToolName != "Bash" {
		return nil
	}
	cmd, _ := hi.ToolInput["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return nil
	}
	cwd := hi.CWD
	if cwd == "" {
		cwd = getenv("PWD")
	}
	guard := detectGuard(cwd)
	for _, seg := range splitSegments(cmd) {
		seg = stripEnvPrefix(strings.TrimSpace(seg))
		if seg == "" {
			continue
		}
		token := leadingToken(seg)

		// Binary-path check fires before the routing-hint pass. A
		// guard binary resolving outside its canonical install paths
		// is a path-hijack candidate, surfaced with a sharper message
		// than the routing-hint table would emit.
		if allowed, ok := guardBinaryPaths[token]; ok {
			if msg := checkBinaryPath(token, allowed, lookup); msg != "" {
				_, _ = fmt.Fprintln(errOut, msg)
				return cli.Exit("", 2)
			}
		}

		hint := routeHint(guard, token, seg)
		if hint == "" {
			continue
		}
		_, _ = fmt.Fprintln(errOut, hint)
		return cli.Exit("", 2)
	}
	return nil
}

// checkBinaryPath resolves token via lookup and returns a non-empty
// hijack-warning string when the resolved path is outside allowed.
// ENOENT (binary not on PATH) returns "" - bash will surface the
// command-not-found error naturally.
//
// Resolution uses lookup directly without canonicalizing symlinks,
// since `command -v` returns the symlink path (e.g. brew's
// /opt/homebrew/bin/coily symlink, not its Cellar realpath). Matching
// the symlink is the documented contract from coily's prior shell
// gate.
func checkBinaryPath(token string, allowed []string, lookup pathLookup) string {
	resolved, err := lookup(token)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ""
		}
		// Any other LookPath error (permission denied, malformed
		// PATH) is a stronger signal than ENOENT. Block defensively.
		return fmt.Sprintf(
			"agent-guard hook: blocked `%s`. Resolution of `%s` failed: %v. Canonical install paths: %s",
			token, token, err, strings.Join(allowed, ", "),
		)
	}
	abs, absErr := filepath.Abs(resolved)
	if absErr != nil {
		abs = resolved
	}
	for _, p := range allowed {
		if abs == p {
			return ""
		}
	}
	return fmt.Sprintf(
		"agent-guard hook: blocked `%s`. `%s` resolves to %s, which is outside the canonical install paths (%s). This looks like a PATH-hijack of the guard binary. Reinstall via the official homebrew tap or unset the offending PATH entry.",
		token, token, abs, strings.Join(allowed, ", "),
	)
}

// detectGuard walks up from cwd for the nearest config marker and
// returns "agent-guard" or "coily". Defaults to "agent-guard" when no
// marker is reachable so the hook still emits a usable hint in stranger-
// cloning-a-downstream-repo contexts.
func detectGuard(start string) string {
	if start == "" {
		return "agent-guard"
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "agent-guard"
	}
	for {
		if fileExists(filepath.Join(dir, ".agent-guard", "agent-guard.yaml")) {
			return "agent-guard"
		}
		if fileExists(filepath.Join(dir, ".coily", "coily.yaml")) {
			return "coily"
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "agent-guard"
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// splitSegments breaks a bash command into the leading-token segments
// we want to classify. Mirrors the awk in coily's lockdown-deny.sh:
// split on $( ) || && | ; & boundaries. Imperfect (we are not a shell
// parser), but tight enough to catch the cases the routing-hint surface
// needs to catch.
func splitSegments(cmd string) []string {
	replacers := []string{"$(", "\n", ")", "\n", "||", "\n", "&&", "\n", "|", "\n", ";", "\n", "&", "\n"}
	r := strings.NewReplacer(replacers...)
	return strings.Split(r.Replace(cmd), "\n")
}

// stripEnvPrefix peels leading `env VAR=val ...` and `sudo` tokens so
// `env FOO=bar gh issue view` classifies the same as bare `gh issue view`.
// Strips iteratively in case both env and sudo are present.
func stripEnvPrefix(seg string) string {
	for {
		trimmed := strings.TrimLeft(seg, " \t")
		switch {
		case strings.HasPrefix(trimmed, "sudo "):
			seg = strings.TrimPrefix(trimmed, "sudo ")
		case strings.HasPrefix(trimmed, "env "):
			rest := strings.TrimPrefix(trimmed, "env ")
			peeled := false
			for {
				rest = strings.TrimLeft(rest, " \t")
				eq := strings.IndexByte(rest, '=')
				sp := strings.IndexByte(rest, ' ')
				if eq <= 0 || (sp >= 0 && sp < eq) {
					break
				}
				if sp < 0 {
					rest = ""
				} else {
					rest = rest[sp+1:]
				}
				peeled = true
			}
			if !peeled {
				return trimmed
			}
			seg = rest
		default:
			return trimmed
		}
	}
}

// leadingToken returns the first whitespace-delimited token of seg.
// "gh issue view" -> "gh", "" -> "".
func leadingToken(seg string) string {
	i := strings.IndexAny(seg, " \t")
	if i < 0 {
		return seg
	}
	return seg[:i]
}

// coilyRoutes maps a bare leading-token to a recovery hint when the
// active guard is coily. Static, table-driven; new entries land here.
var coilyRoutes = map[string]string{
	"gh":        "use `coily ops gh ...` (audited wrapper).",
	"aws":       "use `coily ops aws ...` (audited wrapper).",
	"kubectl":   "use `coily ops kubectl ...` (audited wrapper).",
	"docker":    "use `coily docker ...` (audited wrapper).",
	"tailscale": "use `coily tailscale ...` (audited wrapper).",
	"ssh":       "use `coily ssh ...` (audited wrapper). For kai-server always `kai@kai-server`.",
	"scp":       "use `coily ssh copy ...` (audited wrapper).",
	"brew":      "use `coily brew ...` (scoped to the coilysiren/tap default-allow list).",
	"make":      "use `coily exec <verb>` (verbs declared in .coily/coily.yaml).",
	"just":      "use `coily exec <verb>` (verbs declared in .coily/coily.yaml).",
	"task":      "use `coily exec <verb>` (verbs declared in .coily/coily.yaml).",
	"invoke":    "use `coily exec <verb>` (verbs declared in .coily/coily.yaml).",
	"npm":       "use `coily pkg npm ...` (audited package-manager wrapper).",
	"pnpm":      "use `coily pkg pnpm ...` (audited package-manager wrapper).",
	"yarn":      "use `coily pkg yarn ...` (audited package-manager wrapper).",
	"bun":       "use `coily pkg bun ...` (audited package-manager wrapper).",
	"pip":       "use `coily pkg pip ...` (audited package-manager wrapper).",
	"pipx":      "use `coily pkg pipx ...` (audited package-manager wrapper).",
	"poetry":    "use `coily pkg poetry ...` (audited package-manager wrapper).",
	"uv":        "use `coily pkg uv ...` (audited package-manager wrapper).",
	"cargo":     "use `coily pkg cargo ...` (audited package-manager wrapper).",
	"gem":       "use `coily pkg gem ...` (audited package-manager wrapper).",
	"bundle":    "use `coily pkg bundle ...` (audited package-manager wrapper).",
}

// agentGuardRoutes maps a bare leading-token to a recovery hint when
// the active guard is agent-guard. Smaller surface: agent-guard wraps
// only generic dev verbs, not Kai-personal ops binaries (gh / aws / etc).
var agentGuardRoutes = map[string]string{
	"make":   "use `agent-guard exec <verb>` (verbs declared in .agent-guard/agent-guard.yaml).",
	"just":   "use `agent-guard exec <verb>` (verbs declared in .agent-guard/agent-guard.yaml).",
	"task":   "use `agent-guard exec <verb>` (verbs declared in .agent-guard/agent-guard.yaml).",
	"invoke": "use `agent-guard exec <verb>` (verbs declared in .agent-guard/agent-guard.yaml).",
}

const ghGraphQLTrap = " (and note: `gh issue view` / `gh pr view` / `gh repo view` / `gh search` use the GraphQL API by default - prefer `gh api /repos/OWNER/REPO/...` to avoid the GraphQL rate-limit budget)"

// routeHint returns the stderr block reason for a (guard, token, seg)
// combination, or "" if the token has no route under the active guard.
// The seg is inspected for token-specific extras (e.g. gh GraphQL trap).
func routeHint(guard, token, seg string) string {
	table := tableFor(guard)
	hint, ok := table[token]
	if !ok {
		return ""
	}
	out := prefix(token) + hint
	if guard == "coily" && token == "gh" && isGhGraphQLSubcommand(seg) {
		out += ghGraphQLTrap
	}
	return out
}

func tableFor(guard string) map[string]string {
	if guard == "coily" {
		return coilyRoutes
	}
	return agentGuardRoutes
}

func prefix(token string) string {
	return fmt.Sprintf("agent-guard hook: blocked bare `%s`. Recovery: ", token)
}

// isGhGraphQLSubcommand returns true if seg is a gh invocation whose
// subcommand routes through GraphQL by default. The list is the
// frequent offenders, not exhaustive: we are signaling, not policing.
func isGhGraphQLSubcommand(seg string) bool {
	rest := strings.TrimPrefix(seg, "gh ")
	if rest == seg {
		return false
	}
	rest = strings.TrimLeft(rest, " ")
	parts := strings.SplitN(rest, " ", 3)
	if len(parts) < 2 {
		return false
	}
	sub, action := parts[0], parts[1]
	switch sub {
	case "issue":
		switch action {
		case "view", "list", "status":
			return true
		}
	case "pr":
		switch action {
		case "view", "list", "status", "checks":
			return true
		}
	case "repo":
		switch action {
		case "view", "list":
			return true
		}
	case "search":
		return true
	case "project":
		return true
	}
	return false
}
