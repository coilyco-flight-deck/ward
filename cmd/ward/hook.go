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

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/dispatch"
	"github.com/urfave/cli/v3"
)

// hookCommand groups Claude Code hook entry points. See docs/hook.md.
func hookCommand() *cli.Command {
	return &cli.Command{
		Name:  "hook",
		Usage: "Claude Code hook entry points.",
		Commands: []*cli.Command{
			{
				Name:  "pre-tool-use",
				Usage: "PreToolUse hook for the Bash tool. Routes bare-binary invocations through the local guard wrapper with a recovery hint, and rejects guard-binary invocations resolving outside their canonical install paths.",
				Action: func(_ context.Context, _ *cli.Command) error {
					return runPreToolUse(os.Stdin, os.Stderr, os.Getenv, exec.LookPath, defaultRegistryCheck)
				},
			},
		},
	}
}

// pathLookup mirrors exec.LookPath. Indirected for tests.
type pathLookup func(name string) (string, error)

// guardBinaryPaths is the canonical install-path allow-list per known guard binary.
// See docs/hook.md.
var guardBinaryPaths = map[string][]string{
	"ward": {
		"/opt/homebrew/bin/ward",
		"/usr/local/bin/ward",
		"/home/linuxbrew/.linuxbrew/bin/ward",
	},
	"coily": {
		"/opt/homebrew/bin/coily",
		"/usr/local/bin/coily",
		"/home/linuxbrew/.linuxbrew/bin/coily",
	},
}

// hookInput is the subset of Claude Code's PreToolUse payload we read. See docs/hook.md.
type hookInput struct {
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	CWD       string                 `json:"cwd"`
}

// registryCheck queries the sidequest registry. Returns the recovery
// message on overlap, or empty on clean.
type registryCheck func(absPath string) (string, error)

// runPreToolUse is the testable core of the PreToolUse hook. See docs/hook.md.
func runPreToolUse(in io.Reader, errOut io.Writer, getenv func(string) string, lookup pathLookup, check registryCheck) error {
	// Best-effort hint surface: read failures pass through. See docs/hook.md.
	data, _ := io.ReadAll(in) //nolint:errcheck // intentional: see func doc
	if len(data) == 0 {
		return nil
	}
	var hi hookInput
	if json.Unmarshal(data, &hi) != nil {
		return nil //nolint:nilerr // intentional: malformed payload passes through silently
	}
	if isFileWriteTool(hi.ToolName) {
		return checkFileWriteConflict(hi, errOut, check)
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

		// Binary-path check fires before the routing-hint pass. See docs/hook.md.
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

// checkBinaryPath warns when token resolves outside allowed. See docs/hook.md.
func checkBinaryPath(token string, allowed []string, lookup pathLookup) string {
	resolved, err := lookup(token)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ""
		}
		// Non-ENOENT LookPath errors are a stronger signal than ENOENT. Block defensively.
		return fmt.Sprintf(
			"ward hook: blocked `%s`. Resolution of `%s` failed: %v. Canonical install paths: %s",
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
		"ward hook: blocked `%s`. `%s` resolves to %s, which is outside the canonical install paths (%s). This looks like a PATH-hijack of the guard binary. Reinstall via the official homebrew tap or unset the offending PATH entry.",
		token, token, abs, strings.Join(allowed, ", "),
	)
}

// detectGuard walks up from cwd for the nearest config marker. See docs/hook.md.
func detectGuard(start string) string {
	if start == "" {
		return "ward"
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "ward"
	}
	for {
		if fileExists(filepath.Join(dir, ".ward", "ward.yaml")) {
			return "ward"
		}
		if fileExists(filepath.Join(dir, ".coily", "coily.yaml")) {
			return "coily"
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "ward"
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// splitSegments breaks a bash command into leading-token segments. See docs/hook.md.
func splitSegments(cmd string) []string {
	replacers := []string{"$(", "\n", ")", "\n", "||", "\n", "&&", "\n", "|", "\n", ";", "\n", "&", "\n"}
	r := strings.NewReplacer(replacers...)
	return strings.Split(r.Replace(cmd), "\n")
}

// stripEnvPrefix peels leading `env VAR=val ...` and `sudo` tokens. See docs/hook.md.
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

// coilyRoutes maps a bare leading-token to a recovery hint when active guard is coily.
var coilyRoutes = map[string]string{
	"gh":        "use `coily ops gh ...` (audited wrapper).",
	"aws":       "use `coily ops aws ...` (audited wrapper).",
	"kubectl":   "use `coily ops kubectl ...` (audited wrapper).",
	"docker":    "use `coily docker ...` (audited wrapper).",
	"tailscale": "use `coily tailscale ...` (audited wrapper).",
	"ssh":       "use `coily ssh ...` (audited wrapper). For kai-server always `kai@kai-server`.",
	"scp":       "use `coily ssh copy ...` (audited wrapper).",
	"brew":      "use `coily brew ...` (scoped to the coilysiren/tap default-allow list).",
	"mcporter":  "use `coily ops mcporter ...` (audited wrapper, hydrates env from SSM at preflight).",
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
	"nix":       "use `coily pkg nix ...` (audited package-manager wrapper).",
}

// wardRoutes maps a bare leading-token to a recovery hint. See docs/hook.md.
var wardRoutes = map[string]string{
	"make":   "use `ward exec <verb>` (verbs declared in .ward/ward.yaml).",
	"just":   "use `ward exec <verb>` (verbs declared in .ward/ward.yaml).",
	"task":   "use `ward exec <verb>` (verbs declared in .ward/ward.yaml).",
	"invoke": "use `ward exec <verb>` (verbs declared in .ward/ward.yaml).",
}

const ghGraphQLTrap = " (and note: `gh issue view` / `gh pr view` / `gh repo view` / `gh search` use the GraphQL API by default - prefer `gh api /repos/OWNER/REPO/...` to avoid the GraphQL rate-limit budget)"

// routeHint returns the stderr block reason for a (guard, token, seg). See docs/hook.md.
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
	return wardRoutes
}

func prefix(token string) string {
	return fmt.Sprintf("ward hook: blocked bare `%s`. Recovery: ", token)
}

// isGhGraphQLSubcommand returns true for gh subcommands that route through GraphQL.
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

// isFileWriteTool returns true for Claude Code tool names that mutate a
// single file_path. See coilyco-flight-deck/ward#25.
func isFileWriteTool(name string) bool {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

// checkFileWriteConflict blocks file-write tools when the registry has
// an overlapping claim. Pass-through otherwise.
func checkFileWriteConflict(hi hookInput, errOut io.Writer, check registryCheck) error {
	if check == nil {
		return nil
	}
	path, _ := hi.ToolInput["file_path"].(string)
	if path == "" {
		path, _ = hi.ToolInput["notebook_path"].(string)
	}
	if !filepath.IsAbs(path) {
		return nil
	}
	msg, err := check(path)
	if err != nil || msg == "" {
		return nil //nolint:nilerr // lookup failure or no conflict: pass through
	}
	_, _ = fmt.Fprintf(errOut, "blocked: another sidequest has claimed this path.\n%sRun `dispatch registry list` to see active sidequests, or wait for it to finish.\n", msg)
	return cli.Exit("", 2)
}

// defaultRegistryCheck queries cli-guard's Registry directly. The host
// supplies LogRoot via CLI_GUARD_DISPATCH_LOG_ROOT; unset → pass-through.
func defaultRegistryCheck(absPath string) (string, error) {
	logRoot := os.Getenv("CLI_GUARD_DISPATCH_LOG_ROOT")
	if logRoot == "" {
		return "", nil
	}
	conflicts, err := dispatch.NewRegistry(logRoot).Conflicts(absPath)
	if err != nil || len(conflicts) == 0 {
		return "", nil //nolint:nilerr // best-effort: walk errors pass through
	}
	var b strings.Builder
	for _, c := range conflicts {
		fmt.Fprintf(&b, "pid=%d ref=%s claim=%s reason=%s\n", c.PID, c.Ref, c.Claim, c.Reason)
	}
	return b.String(), nil
}
