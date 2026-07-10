package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/flock"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/urfave/cli/v3"
)

// container_bootstrap.go is the Go port of containerassets/entrypoint.sh - the
// PID-1 bootstrap every ward agent container runs (ward#181). See docs/container.md.

// bootstrapEnv holds the entrypoint's env-var config, read once with the bash
// defaults applied. Required vars (the bash `:?` checks) error in readBootstrapEnv.
type bootstrapEnv struct {
	TargetOwner  string
	TargetName   string
	ForgejoBase  string
	Mode         string
	Container    string
	Issue        int
	Agent        string
	ContextLevel string
	GitCache     string
	ContextSrc   string
	QwenModel    string
	OllamaURL    string
	// Cheapest codex posture by default (ward#379): mini model + low reasoning +
	// verbosity, each overridable via WARD_CODEX_*. docs/agent-credentials.md.
	CodexModel     string
	CodexEffort    string
	CodexVerbosity string
	// claude model + effort are override-only (ward#616): empty defaults keep today's
	// bare launch; WARD_CLAUDE_MODEL / WARD_CLAUDE_REASONING_EFFORT fill them.
	ClaudeModel  string
	ClaudeEffort string
	GitUserName  string
	GitUserEmail string
	// Role is the run's config role (WARD_ROLE, ward#620): director/engineer/advisor,
	// keying the per-role model/effort overlay resolved below. Empty means no overlay.
	Role       string
	AgentUID   string
	AgentGID   string
	AgentHome  string
	MirrorName string
	Branch     string
	Headless   bool
	Ask        bool
	// ReadOnly is the read-only surface session (WARD_READONLY, ward#293): revoke
	// the push credential, compose the restriction. See docs/agent-surface.md.
	ReadOnly          bool
	WardVersionSource string
	ForgejoHost       string
	// Forge is the TARGET repo's host (ward#489, GitLab added in #635): GitHub and GitLab
	// clone off CloneBase with their own push users, else Forgejo + coilyco-ops.
	Forge     forge
	CloneBase string
	CloneHost string
	// ExtraRepos are the additional writable repos this run was granted via
	// --repo (WARD_EXTRA_REPOS); each is cloned full under /workspace (ward#230).
	ExtraRepos []targetRepo
	// ContextRepos are catalog.dependsOn cloned READ-ONLY, resolved from the fresh clone
	// (ward#580); an external (non-Forgejo) dep carries its honored clone URL (ward#612).
	ContextRepos []catalogContextRepo
	// Substrate config (best-effort reference-repo warming).
	SubstrateSeed     string
	SubstrateDest     string
	SubstrateManifest string
	SubstrateTTL      string
	SubstrateSkip     bool
	TailnetSocks5     string
}

const runProvenanceFile = ".ward-run-provenance.json"

var workspaceRoot = "/workspace"

// runProvenance records the dispatch-time identity and remote baseline so the
// reaper can prove later success came from this run, not stale history.
type runProvenance struct {
	RunID        string `json:"run_id"`
	Repo         string `json:"repo"`
	Issue        int    `json:"issue"`
	ReservedAt   string `json:"reserved_at"`
	BaselineMain string `json:"baseline_main"`
}

// envOr returns the env var or a default, mirroring bash `${X:-default}`.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// fleetAgentByName returns the named agent from the parsed fleet, or a zero Agent
// when absent (its empty fields then flow through as the envOr default).
func fleetAgentByName(f fleetconfig.Fleet, name string) fleetconfig.Agent {
	for _, a := range f.Agents {
		if a.Name == name {
			return a
		}
	}
	return fleetconfig.Agent{}
}

// firstNonEmpty returns the first non-empty string, or "" when both are empty. It
// slots the per-role overlay ahead of the flat per-agent fleet default (ward#620).
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// roleAgentOverride returns the role's sparse per-agent overlay from the fleet config
// (cli-guard#192, ward#620), or a zero override when the role/agent carries none.
func roleAgentOverride(f fleetconfig.Fleet, role, agent string) fleetconfig.RoleAgentOverride {
	if role == "" {
		return fleetconfig.RoleAgentOverride{}
	}
	for _, r := range f.Roles {
		if r.Name == role {
			return r.AgentConfig[agent]
		}
	}
	return fleetconfig.RoleAgentOverride{}
}

// readBootstrapEnv reads + defaults the entrypoint env, erroring on a missing
// required var (the bash `: "${X:?...}"` checks). Pure given the environment.
func readBootstrapEnv() (bootstrapEnv, error) {
	// Defaults now source from the embedded fleet config (env > manifest, ward#416);
	// opencode is canonical, with qwen retained only as a parser alias for compatibility.
	fleet, ferr := loadFleetConfig()
	if ferr != nil {
		return bootstrapEnv{}, fmt.Errorf("load embedded fleet config for bootstrap defaults: %w", ferr)
	}
	opencode := fleetAgentByName(fleet, string(modeOpencode))
	codex := fleetAgentByName(fleet, string(modeCodex))
	claude := fleetAgentByName(fleet, string(modeClaude))
	// Per-role model/effort overlay (ward#620): the resolved role's per-agent overlay
	// slots into the precedence between WARD_* env and the flat per-agent fleet default.
	role := os.Getenv("WARD_ROLE")
	claudeOv := roleAgentOverride(fleet, role, string(modeClaude))
	codexOv := roleAgentOverride(fleet, role, string(modeCodex))
	opencodeOv := roleAgentOverride(fleet, role, string(modeOpencode))
	attribution := fleet.Defaults.Attribution
	e := bootstrapEnv{
		TargetOwner:  os.Getenv("WARD_TARGET_OWNER"),
		TargetName:   os.Getenv("WARD_TARGET_NAME"),
		ForgejoBase:  os.Getenv("WARD_FORGEJO_BASE"),
		Mode:         envOr("WARD_MODE", fleet.Defaults.Agent),
		Container:    os.Getenv("WARD_CONTAINER_NAME"),
		Issue:        0,
		Agent:        envOr("WARD_AGENT", string(modeClaude)),
		ContextLevel: envOr("WARD_CONTEXT_LEVEL", "2"),
		GitCache:     envOr("WARD_GITCACHE", "/gitcache"),
		ContextSrc:   envOr("WARD_CONTEXT_SRC", "/opt/ward-context"),
		// Precedence WARD_* env > role overlay > flat per-agent default (ward#620): the
		// role's opencode overlay (if any) leads the fleet manifest's opencode node.
		QwenModel: envOr("WARD_QWEN_MODEL", firstNonEmpty(opencodeOv.Model, opencode.Model)),
		OllamaURL: envOr("WARD_OLLAMA_URL", firstNonEmpty(opencodeOv.Endpoint, opencode.Endpoint)),
		// Cheapest codex settings (ward#379): fleet manifest's codex node, role overlay
		// ahead of it (ward#620).
		CodexModel:     envOr("WARD_CODEX_MODEL", firstNonEmpty(codexOv.Model, codex.Model)),
		CodexEffort:    envOr("WARD_CODEX_REASONING_EFFORT", firstNonEmpty(codexOv.ReasoningEffort, codex.ReasoningEffort)),
		CodexVerbosity: envOr("WARD_CODEX_VERBOSITY", firstNonEmpty(codexOv.Verbosity, codex.Verbosity)),
		// claude model + effort default empty from the fleet claude node (ward#616); the
		// role overlay (ward#620) fills them per role, and --config / WARD_CLAUDE_* still win.
		ClaudeModel:  envOr("WARD_CLAUDE_MODEL", firstNonEmpty(claudeOv.Model, claude.Model)),
		ClaudeEffort: envOr("WARD_CLAUDE_REASONING_EFFORT", firstNonEmpty(claudeOv.ReasoningEffort, claude.ReasoningEffort)),
		// Bot attribution: email is the load-bearing Forgejo match (ward#245); both
		// default from the fleet manifest's defaults.attribution.
		GitUserName:       envOr("WARD_GIT_NAME", attribution.Name),
		GitUserEmail:      envOr("WARD_GIT_EMAIL", attribution.Email),
		Role:              role,
		AgentUID:          envOr("WARD_AGENT_UID", "1000"),
		AgentGID:          envOr("WARD_AGENT_GID", "1000"),
		AgentHome:         envOr("WARD_AGENT_HOME", "/home/ubuntu"),
		MirrorName:        os.Getenv("WARD_MIRROR_NAME"),
		Branch:            os.Getenv("WARD_BRANCH"),
		Headless:          os.Getenv("WARD_HEADLESS") == "1",
		Ask:               os.Getenv("WARD_ASK") == "1",
		ReadOnly:          os.Getenv("WARD_READONLY") == "1",
		WardVersionSource: envOr(envAgentVersionSource, ""),

		SubstrateSeed:     envOr("WARD_SUBSTRATE_SEED", "/opt/substrate-seed"),
		SubstrateDest:     envOr("WARD_SUBSTRATE_DEST", "/substrate"),
		SubstrateManifest: envOr("WARD_SUBSTRATE_MANIFEST", "/opt/ward/preclone-repos.txt"),
		SubstrateTTL:      envOr("WARD_SUBSTRATE_TTL", "600"),
		SubstrateSkip:     os.Getenv("WARD_SUBSTRATE_SKIP") == "1",
		TailnetSocks5:     os.Getenv("WARD_TS_SOCKS5"),
	}
	if e.TargetOwner == "" {
		return e, fmt.Errorf("missing WARD_TARGET_OWNER")
	}
	if e.TargetName == "" {
		return e, fmt.Errorf("missing WARD_TARGET_NAME")
	}
	if e.ForgejoBase == "" {
		return e, fmt.Errorf("missing WARD_FORGEJO_BASE")
	}
	e.ForgejoHost = forgejoHostFromBase(e.ForgejoBase)
	// The TARGET forge + clone base (ward#489); CloneBase defaults to the Forgejo base
	// so a run that names neither behaves exactly as before.
	e.Forge = parseForge(os.Getenv("WARD_FORGE"))
	e.CloneBase = envOr("WARD_CLONE_BASE", e.ForgejoBase)
	e.CloneHost = forgejoHostFromBase(e.CloneBase)
	e.ExtraRepos = parseExtraReposEnv(os.Getenv("WARD_EXTRA_REPOS"), e.TargetOwner, e.TargetName)
	// e.ContextRepos is NOT read from the env: the host cwd may not be the target repo,
	// so it is resolved from the fresh clone after cloneTarget (ward#580).
	e.Issue, _ = strconv.Atoi(os.Getenv("WARD_TARGET_ISSUE"))
	return e, nil
}

// parseExtraReposEnv parses the space-separated WARD_EXTRA_REPOS list, dropping
// blanks, the target, dups, and (leniently) malformed entries (ward#230).
func parseExtraReposEnv(raw, targetOwner, targetName string) []targetRepo {
	var out []targetRepo
	seen := map[string]bool{}
	for _, ref := range strings.Fields(raw) {
		owner, name, ok := splitOwnerName(ref)
		if !ok {
			continue
		}
		if owner == targetOwner && name == targetName {
			continue
		}
		slug := owner + "/" + name
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, targetRepo{Owner: owner, Name: name})
	}
	return out
}

// forgejoHostFromBase strips scheme + path off the base URL, leaving the host;
// mirrors the bash `sed -E 's#^https?://##; s#/.*$##'`.
func forgejoHostFromBase(base string) string {
	h := strings.TrimPrefix(base, "https://")
	h = strings.TrimPrefix(h, "http://")
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	return h
}

// oneshot reports whether the run is a single-shot mode (headless or ask),
// which share the one-shot argv + stdin-pinned launch.
func (e bootstrapEnv) oneshot() bool { return e.Headless || e.Ask }

// blog logs to stderr in the entrypoint's `log()` format.
func blog(format string, a ...any) {
	writef(os.Stderr, "ward-container: "+format+"\n", a...)
}

// echoRunContextGo echoes the dynamic per-run context to stderr at startup, before any
// gate (ward#609), mirroring the bash echo_run_context; the seed rides in agentArgs.
func echoRunContextGo(e bootstrapEnv, agentArgs []string) {
	ref := e.TargetOwner + "/" + e.TargetName
	if e.Issue != 0 {
		ref = fmt.Sprintf("%s#%d", ref, e.Issue)
	}
	seed := "(no seed argv; interactive or seedless run)"
	if len(agentArgs) > 0 {
		seed = strings.Join(agentArgs, " ")
	}
	writef(os.Stderr, "===== ward run context (ward#609) =====\n"+
		"repo:     %s/%s\nref:      %s\nbranch:   %s\ndriver:   %s (agent %s)\nrun:      %s\n"+
		"workflow: %s\nward:     %s\nup:       %s\n----- seed / task text -----\n%s\n"+
		"===== end ward run context =====\n",
		e.TargetOwner, e.TargetName, ref, orDefaultLabel(e.Branch, "(default)"),
		e.Mode, e.Agent, orDefaultLabel(e.Container, "(unnamed)"),
		orDefaultLabel(os.Getenv("WARD_WORKFLOW"), "direct-main"),
		orDefaultLabel(e.WardVersionSource, wardVersionLaunchLabel(os.Getenv("WARD_VERSION"), "")),
		orDefaultLabel(os.Getenv("WARD_CONTAINER_UP"), "(unset)"), seed)
}

// echoAgentConfigGo echoes the launched agent's resolved model-context config at
// startup (ward#616), so its harness config is visible in the log, not silent.
func echoAgentConfigGo(e bootstrapEnv, rc agentsapi.RunCtx, mode containerMode) {
	model, effort, endpoint := resolvedAgentKnobs(rc, mode)
	writef(os.Stderr, "===== ward agent config (ward#616) =====\n"+
		"agent:         %s\nmodel:         %s\neffort:        %s\nendpoint:      %s\ncontext-level: %s\n"+
		"===== end ward agent config =====\n",
		string(mode),
		orDefaultLabel(model, "(harness default)"),
		orDefaultLabel(effort, "(harness default)"),
		orDefaultLabel(endpoint, "(harness default)"),
		orDefaultLabel(e.ContextLevel, "(unset)"))
}

// resolvedAgentKnobs projects the per-mode model / effort / endpoint out of the
// resolved RunCtx for the startup echo (ward#616); an unknown mode reports blanks.
func resolvedAgentKnobs(rc agentsapi.RunCtx, mode containerMode) (model, effort, endpoint string) {
	switch mode {
	case modeCodex:
		return rc.CodexModel, rc.CodexEffort, ""
	case modeOpencode:
		return rc.OpencodeModel, "", rc.OllamaURL
	case modeGoose:
		return "", "", ""
	case modeClaude:
		return rc.ClaudeModel, rc.ClaudeEffort, ""
	default:
		return "", "", ""
	}
}

// orDefaultLabel returns s, or def when s is blank.
func orDefaultLabel(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// bootstrapPrelaunchGate names the pre-launch gate the reaper reports for a mode:
// claude's is the auth smoke test, the local-model harnesses' is the ollama probe.
func bootstrapPrelaunchGate(mode containerMode) string {
	switch mode {
	case modeClaude:
		return "auth"
	case modeCodex, modeOpencode, modeGoose:
		return "ollama-probe"
	default:
		return "ollama-probe"
	}
}

// namedGate returns a pre-launch gate override if err carries one.
func namedGate(err error) (string, bool) {
	var gateName interface{ GateName() string }
	if errors.As(err, &gateName) {
		if name := strings.TrimSpace(gateName.GateName()); name != "" {
			return name, true
		}
	}
	return "", false
}

// containerBootstrapCommand is the hidden `ward container bootstrap`: the PID-1
// entrypoint port (ward#181). Hidden because it is image-internal, not for hand use.
func containerBootstrapCommand() *cli.Command {
	return &cli.Command{
		Name:            "bootstrap",
		Hidden:          true,
		Usage:           "Container PID-1 entrypoint: configure auth, clone, compose context, then launch the agent (image-internal; ward#181).",
		SkipFlagParsing: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.bootstrap",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runContainerBootstrap(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runContainerBootstrap is the Go port of the bash entrypoint main loop.
func (r *Runner) runContainerBootstrap(ctx context.Context, c *cli.Command) error { //nolint:funlen,gocyclo,cyclop
	e, err := readBootstrapEnv()
	if err != nil {
		blog("fatal: %v", err)
		return err
	}
	agentArgs := c.Args().Slice()
	blog("bootstrap start: container=%s mode=%s agent=%s issue=%d readOnly=%t headless=%t extraRepos=%d",
		e.Container, e.Mode, e.Agent, e.Issue, e.ReadOnly, e.Headless, len(e.ExtraRepos))
	// Echo the run context first, before any gate, so every abort is greppable in the
	// container log (ward#609, the docker-log backstop surface).
	echoRunContextGo(e, agentArgs)

	// The container is the isolation boundary; opt the reaper out of ward's jail
	// (cli-guard#153). Stamp container start for the reaper's PAT-age report (ward#103).
	_ = os.Setenv("CLIGUARD_NO_SANDBOX", "1")
	if os.Getenv("WARD_CONTAINER_UP") == "" {
		_ = os.Setenv("WARD_CONTAINER_UP", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	}

	// Dispatch every per-agent seam through the registry + the agentsapi capability
	// interfaces, feature-tested per mode; the mode/argv switches stay live for Phase 4.
	mode, perr := parseMode(e.Mode)
	if perr != nil {
		mode = modeClaude // match the switches' default-to-claude arm
	}
	agent := lookupAgent(mode)
	blog("bootstrap mode selected: requested=%s resolved=%s driver=%s", e.Mode, mode, e.Agent)
	rc := r.agentRunCtx(ctx, e, agentArgs)
	// Echo the resolved model-context config for the launched agent at startup (ward#616),
	// so its model/effort/endpoint/context-level are visible in the log, not silent.
	echoAgentConfigGo(e, rc, mode)

	r.configureGitAuth(ctx, e)
	blog("bootstrap installer start: %s", mode)
	if err := installHarness(agent, rc); err != nil {
		blog("fatal: %v", err)
		writeGateFailure("bootstrap", err.Error())
		return err
	}
	blog("bootstrap installer done: %s", mode)
	work, cerr := r.cloneTarget(ctx, e)
	if cerr != nil {
		return cerr
	}
	blog("bootstrap clone done: %s/%s -> %s", e.TargetOwner, e.TargetName, work)
	// Resolve the read-only context set from the FRESH CLONE, not the host cwd, which
	// may not be the target repo (ward#580). Only place e.ContextRepos is set here.
	e.ContextRepos = r.resolveCatalogContext(work, e)
	if perr := r.writeRunProvenance(ctx, work, e); perr != nil {
		blog("fatal: %v", perr)
		return perr
	}
	blog("bootstrap provenance ready: %s", work)
	blog("bootstrap hook install start: %s", work)
	r.installPreCommitHooks(ctx, e, work)
	r.installReadOnlyPushGuard(ctx, e, work)
	blog("bootstrap hook install done: %s", work)
	blog("bootstrap extra-repo clone start: %d grant(s)", len(e.ExtraRepos))
	r.cloneExtraRepos(ctx, e)
	blog("bootstrap extra-repo clone done")
	blog("bootstrap context-repo clone start: %d read-only grant(s)", len(e.ContextRepos))
	r.cloneContextRepos(ctx, e)
	blog("bootstrap context-repo clone done")
	blog("bootstrap substrate warm start")
	r.warmSubstrate(ctx, e)
	blog("bootstrap substrate warm done")
	prepareConfigBundleCache(e)
	blog("bootstrap context compose start")
	r.composeContext(e)
	blog("bootstrap permissions compose start")
	r.composePermissions(e)
	// Set the trust set here, post-warm, not in agentRunCtx (which runs pre-warm)
	// so the /substrate dirs already exist to enumerate (ward#168).
	rc.TrustDirs = agentTrustDirs(e)
	// Creds write + onboarding seed + config compose, each feature-tested per mode
	// (Phase 3, ward#418); composeAgentContainer holds the order.
	composeAgentContainer(agent, rc)
	blog("bootstrap agent container composition done")

	_ = os.Setenv("WARD_REAP_WORK", work)
	r.prepareScratchSpace("/scratch")
	defer r.reap(ctx, work)

	branch := r.captureTrim(ctx, "git", "-C", work, "branch", "--show-current")
	blog("ready: %s/%s on %s [mode=%s]", e.TargetOwner, e.TargetName, branch, e.Mode)

	argv, stream := buildAgentArgv(e, agentArgs)
	logAgentArgv(e, agentArgs)

	// Drop to the non-root agent user (claude refuses bypass-perms as root, ward#127);
	// setup ran as root. Keep ANTHROPIC_API_KEY from shadowing the OAuth creds.
	r.chownAgentTree(ctx, e, work)
	if e.ReadOnly {
		// Explore: scope push off this clone but keep the dispatch token + socket so
		// it can commission sibling runs (ward#293, ward#315).
		r.makeReadOnlyTree(work)
		r.revokePushCredential(ctx)
		r.grantDockerSocketAccess(ctx, e)
	} else if cerr := r.ensureGitCredReadable(e); cerr != nil {
		// Re-assert the credential perms git's `store` helper clobbered on the clones,
		// else the dropped agent falls back to the human token (ward#288).
		blog("fatal: %v", cerr)
		writeGateFailure("bootstrap", cerr.Error()) // reaper release-comment context (ward#609)
		return cerr
	}
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_AUTH_TOKEN")

	// LaunchGate feature-test (only claude wires one, ward#418): fail loud before
	// launch if claude can't authenticate (ward#222), as the agent user post-chown.
	if lg, ok := agent.(agentsapi.LaunchGate); ok {
		blog("bootstrap prelaunch check start: %s", e.Agent)
		if serr := lg.PreLaunchCheck(rc); serr != nil {
			blog("bootstrap prelaunch check failed: %v", serr)
			blog("fatal: %v", serr)
			// Name the gate for the reaper's reservation-release comment (ward#609).
			// Typed launch-gate errors can override the default mode gate.
			gate := bootstrapPrelaunchGate(mode)
			if named, ok := namedGate(serr); ok {
				gate = named
			}
			writeGateFailure(gate, serr.Error())
			return serr
		}
		blog("bootstrap prelaunch check passed: %s", e.Agent)
	}

	blog("launching %s as uid %s", e.Agent, e.AgentUID)
	blog("bootstrap launch handoff: %s", e.Agent)
	r.launchAgent(ctx, e, work, argv, stream)
	blog("bootstrap launch returned: agent process exited, deferred reaper runs next")
	return nil
}

// prepareScratchSpace provisions the writable throwaway area used by read-only
// sessions and points common temp env vars at it.
func (r *Runner) prepareScratchSpace(scratchDir string) {
	if err := os.MkdirAll(scratchDir, 0o1777); err != nil {
		blog("could not create %s: %v", scratchDir, err)
		return
	}
	_ = os.Chmod(scratchDir, 0o1777)
	_ = os.Setenv("TMPDIR", scratchDir)
	_ = os.Setenv("TMP", scratchDir)
	_ = os.Setenv("TEMP", scratchDir)
	blog("scratch area ready at %s", scratchDir)
}

// makeReadOnlyTree removes write bits from a cloned workspace so the surface
// session's current tree is enforced by permissions, not only doctrine.
func (r *Runner) makeReadOnlyTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			blog("read-only tree walk skipped %s: %v", path, err)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			blog("read-only tree stat skipped %s: %v", path, ierr)
			return nil
		}
		mode := info.Mode().Perm()
		if d.IsDir() {
			mode &^= 0o222
			if path == root {
				mode |= 0o555
			}
		} else {
			mode &^= 0o222
		}
		if cherr := os.Chmod(path, mode); cherr != nil { // #nosec G122 -- chmod stays inside the walked clone tree
			blog("read-only tree chmod skipped %s: %v", path, cherr)
		}
		return nil
	})
}

// --- forgejo git auth (token rides --env-file, never argv) -------------------

//nolint:gosec // Container-local credential-store path, not an embedded credential.
const forgejoGitCredentialsPath = "/etc/ward-git-credentials"

//nolint:gosec // Container-local helper path, not an embedded credential.
const forgejoGitCredentialHelperPath = "/etc/ward-git-credential-helper"

//nolint:gosec // Helper script reads a mounted credential file.
const forgejoGitCredentialHelperScript = `#!/bin/sh
cred_file=%q
case "${1:-}" in
  get)
    if [ -r "$cred_file" ]; then
      exec git credential-store --file="$cred_file" "$@"
    fi
    exit 0
    ;;
  store|erase)
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`

// configureGitAuth ports configure_git_auth: --system git identity + a
// read-only credential helper readable by root (reaper) and the dropped agent group.
func (r *Runner) configureGitAuth(ctx context.Context, e bootstrapEnv) {
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "user.name", e.GitUserName)
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "user.email", e.GitUserEmail)
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "init.defaultBranch", "main")
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "--add", "safe.directory", "*")
	token := os.Getenv("FORGEJO_TOKEN")
	if token == "" {
		blog("no FORGEJO_TOKEN: clone/push will only work for anonymous repos")
		return
	}
	if werr := writeForgejoGitCredentialHelper(forgejoGitCredentialHelperPath, forgejoGitCredentialsPath); werr != nil {
		blog("could not write git credential helper: %v", werr)
		return
	}
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "credential.helper",
		"!"+forgejoGitCredentialHelperPath)
	// Push as the forge's bot: FORGEJO_TOKEN carries the Forgejo bot token (coilyco-ops)
	// or the user-supplied GitHub/GitLab token with the forge-specific HTTPS user.
	cred := fmt.Sprintf("https://%s:%s@%s\n", e.Forge.gitPushUser(), token, e.CloneHost)
	if werr := os.WriteFile(forgejoGitCredentialsPath, []byte(cred), 0o640); werr != nil {
		blog("could not write git credentials: %v", werr)
		return
	}
	// Readable by root (reaper) and the dropped agent group, not world; git's store
	// helper stays read-only, but keep the perms explicit before the drop.
	if gid, gerr := strconv.Atoi(e.AgentGID); gerr == nil {
		_ = os.Chown(forgejoGitCredentialsPath, 0, gid)
	}
	_ = os.Chmod(forgejoGitCredentialsPath, 0o640)
}

// revokePushCredential scopes the revoke to push-to-this-clone: it drops the git push
// wiring but keeps FORGEJO_TOKEN for dispatch (ward#315). See agent-surface.md.
func (r *Runner) revokePushCredential(ctx context.Context) {
	_ = os.Remove(forgejoGitCredentialsPath)
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "--unset-all", "credential.helper")
	blog("read-only session: dropped this clone's push wiring; FORGEJO_TOKEN kept for dispatch-only (file/launch, no push; ward#315)")
}

// grantDockerSocketAccess lets the dropped agent reach the mounted socket to dispatch
// a sibling, no host-inode chmod (ward#315, ward#319). See agent-surface.md.
func (r *Runner) grantDockerSocketAccess(ctx context.Context, e bootstrapEnv) {
	const sock = "/var/run/docker.sock"
	if !isSocket(sock) {
		blog("explore: no docker socket mounted - dispatch unavailable this run (ward#315)")
		return
	}
	info, err := os.Stat(sock)
	if err != nil {
		blog("explore: could not stat docker socket; dispatch may fail: %v (ward#315)", err)
		return
	}
	sockgid, ok := fileGID(info)
	if !ok {
		blog("explore: could not read docker socket gid; dispatch may fail (ward#315)")
		return
	}
	if sockgid == 0 {
		r.bridgeDockerSocket(ctx, e, sock) // root:root: no group to join, bridge it (ward#319)
		return
	}
	u, uerr := user.LookupId(e.AgentUID)
	if uerr != nil {
		blog("explore: no passwd entry for uid %s; cannot group-grant the socket (ward#315)", e.AgentUID)
		return
	}
	gidStr := strconv.Itoa(sockgid)
	// Create a group with the socket's gid if none exists (container-only), then add
	// the agent to it. No chmod/chown touches the bind-mounted socket inode.
	if _, gerr := user.LookupGroupId(gidStr); gerr != nil {
		_ = r.Runner.Exec(ctx, "groupadd", "-g", gidStr, "dockerhost")
	}
	if aerr := r.Runner.Exec(ctx, "usermod", "-aG", gidStr, u.Username); aerr != nil {
		blog("explore: could not add %s to socket group (gid %s); dispatch may fail: %v (ward#315)", u.Username, gidStr, aerr)
		return
	}
	blog("explore: granted docker socket access to %s via group gid %s; no socket perms changed (ward#315)", u.Username, gidStr)
}

// bridgeDockerSocket bridges a root:root docker socket to an agent-group-owned socket
// via root socat, reached through DOCKER_HOST with no host-perm change (ward#319).
func (r *Runner) bridgeDockerSocket(ctx context.Context, e bootstrapEnv, sock string) {
	const bridge = "/tmp/docker-agent.sock"
	if !commandExists("socat") {
		blog("explore: socat absent from image; dispatch unavailable on a root:root socket (ward#319)")
		return
	}
	_ = os.Remove(bridge)
	listen := fmt.Sprintf("UNIX-LISTEN:%s,fork,group=%s,mode=0660", bridge, e.AgentGID)
	cmd := exec.CommandContext(ctx, "socat", listen, "UNIX-CONNECT:"+sock) // #nosec G204 -- fixed socat bridge argv
	if serr := cmd.Start(); serr != nil {
		blog("explore: could not start docker socket bridge; dispatch may fail: %v (ward#319)", serr)
		return
	}
	_ = os.Setenv("DOCKER_HOST", "unix://"+bridge)
	blog("explore: bridged root:root docker socket to %s for the agent (gid %s; ward#319)", bridge, e.AgentGID)
}

// ensureGitCredReadable re-asserts the credential perms stay readable by the
// dropped agent and the root reaper; fails loud (ward#288, docs/agent-credentials.md).
func (r *Runner) ensureGitCredReadable(e bootstrapEnv) error {
	const f = forgejoGitCredentialsPath
	if !fileExists(f) {
		return nil
	}
	gid, gerr := strconv.Atoi(e.AgentGID)
	if gerr != nil {
		return fmt.Errorf("ward#288: agent gid %q is not numeric, cannot group-own %s", e.AgentGID, f)
	}
	if cerr := os.Chown(f, 0, gid); cerr != nil {
		return fmt.Errorf("ward#288: could not group-own %s to gid %d: %w", f, gid, cerr)
	}
	if cerr := os.Chmod(f, 0o640); cerr != nil {
		return fmt.Errorf("ward#288: could not chmod %s to 0640: %w", f, cerr)
	}
	// Confirm the agent gid actually carries group-read, so a regression fails here
	// instead of degrading to the human-token fallback.
	info, serr := os.Stat(f)
	if serr != nil {
		return fmt.Errorf("ward#288: could not stat %s after re-perm: %w", f, serr)
	}
	if info.Mode().Perm()&0o040 == 0 {
		return fmt.Errorf("ward#288: %s is not group-readable after re-perm (mode %o); agent push would fall back to the human token and leak attribution", f, info.Mode().Perm())
	}
	if fgid, ok := fileGID(info); ok && fgid != gid {
		return fmt.Errorf("ward#288: %s is group-owned by gid %d, not the agent gid %d; agent cannot read the bot credential", f, fgid, gid)
	}
	return nil
}

// writeForgejoGitCredentialHelper writes the read-only helper that serves `get`
// from the shared credential file while treating `store` / `erase` as no-op success.
func writeForgejoGitCredentialHelper(path, credFile string) error {
	script := fmt.Sprintf(forgejoGitCredentialHelperScript, credFile)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return fmt.Errorf("ward container bootstrap: write git credential helper %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("ward container bootstrap: chmod git credential helper %s: %w", path, err)
	}
	return nil
}

// --- cached fresh clone (mirror in the shared gitcache volume) ---------------

// cloneTarget ports clone_target: refresh-or-create the bare mirror, then drop
// a working clone under /workspace and return its path.
func (r *Runner) cloneTarget(ctx context.Context, e bootstrapEnv) (string, error) {
	mirror := filepath.Join(e.GitCache, e.MirrorName)
	url := e.CloneBase + "/" + e.TargetOwner + "/" + e.TargetName + ".git"
	_ = os.MkdirAll(e.GitCache, 0o755)
	if isDir(mirror) {
		blog("clone start: refreshing cached mirror %s", mirror)
		if uerr := r.Runner.Exec(ctx, "git", "-C", mirror, "remote", "update", "--prune"); uerr != nil {
			blog("mirror refresh failed, using cached state")
		}
	} else {
		blog("clone start: cloning mirror %s", url)
		if cerr := r.Runner.Exec(ctx, "git", "clone", "--mirror", url, mirror); cerr != nil {
			return "", fmt.Errorf("ward container bootstrap: mirror clone failed: %w", cerr)
		}
	}
	work := filepath.Join(workspaceRoot, e.TargetName)
	_ = os.RemoveAll(work)
	blog("clone start: working clone %s -> %s", mirror, work)
	if cerr := r.Runner.Exec(ctx, "git", "clone", mirror, work); cerr != nil {
		return "", fmt.Errorf("ward container bootstrap: working clone failed: %w", cerr)
	}
	_ = r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "origin", url)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "config", "push.default", "current")
	r.checkoutRunBranch(ctx, work, e.Branch, "target repo "+e.TargetOwner+"/"+e.TargetName)
	blog("clone done: %s", work)
	return work, nil
}

// writeRunProvenance captures the dispatch-time identity and remote baseline so
// the reaper can prove later success came from this run, not stale history.
func (r *Runner) writeRunProvenance(ctx context.Context, work string, e bootstrapEnv) error {
	if e.Issue == 0 {
		blog("provenance skip: no issue for %s", work)
		return nil
	}
	// An empty repo has no origin/main to baseline against: record an empty baseline
	// (an establish-main dispatch) rather than abort bring-up (ward#599, see docs).
	baseline := r.captureTrim(ctx, "git", "-C", work, "rev-parse", "origin/main")
	if baseline == "" {
		blog("provenance: no origin/main baseline (empty repo); recording establish-main dispatch for %s", work)
	}
	prov := runProvenance{
		RunID:        e.Container,
		Repo:         e.TargetOwner + "/" + e.TargetName,
		Issue:        e.Issue,
		ReservedAt:   os.Getenv("WARD_CONTAINER_UP"),
		BaselineMain: baseline,
	}
	data, merr := json.MarshalIndent(prov, "", "  ")
	if merr != nil {
		return fmt.Errorf("ward container bootstrap: marshal provenance: %w", merr)
	}
	data = append(data, '\n')
	path := filepath.Join(work, runProvenanceFile)
	if werr := os.WriteFile(path, data, 0o600); werr != nil {
		return fmt.Errorf("ward container bootstrap: write provenance %s: %w", path, werr)
	}
	blog("provenance recorded: %s baseline=%s", path, baseline)
	return nil
}

// --- additional granted repos (ward#230): clone+operate beyond the target ----

// cloneExtraRepos clones each granted extra repo as a full feature working copy
// under /workspace; best-effort per repo. See docs/container-multi-repo.md.
func (r *Runner) cloneExtraRepos(ctx context.Context, e bootstrapEnv) {
	if len(e.ExtraRepos) == 0 {
		return
	}
	_ = os.MkdirAll(e.GitCache, 0o755)
	for _, repo := range e.ExtraRepos {
		blog("extra-repo clone start: %s/%s", repo.Owner, repo.Name)
		r.cloneExtraRepo(ctx, e, repo, false, "")
		blog("extra-repo clone done: %s/%s", repo.Owner, repo.Name)
	}
}

// resolveCatalogContext resolves the read-only context set from the fresh clone at
// work (ward#580), not the host cwd, and logs each grant. Best-effort, never fatal.
func (r *Runner) resolveCatalogContext(work string, e bootstrapEnv) []catalogContextRepo {
	target := targetRepo{Owner: e.TargetOwner, Name: e.TargetName}
	repos, notes := resolveCatalogContextRepos(work, target, e.ExtraRepos)
	for _, note := range notes {
		blog("context-repo resolved from clone: %s (%s)", note.Slug, note.Reason)
	}
	return repos
}

// cloneContextRepos clones each read-only catalog-dependency context repo under
// /workspace as reference (ward#573). Best-effort per repo; never fatal.
func (r *Runner) cloneContextRepos(ctx context.Context, e bootstrapEnv) {
	if len(e.ContextRepos) == 0 {
		return
	}
	_ = os.MkdirAll(e.GitCache, 0o755)
	for _, repo := range e.ContextRepos {
		blog("context-repo clone start: %s/%s (read-only)", repo.Owner, repo.Name)
		r.cloneExtraRepo(ctx, e, repo.targetRepo, true, repo.CloneURL)
		blog("context-repo clone done: %s/%s", repo.Owner, repo.Name)
	}
}

// cloneExtraRepo mirrors or seeds one granted repo under /workspace.
// External deps use the supplied clone URL.
func (r *Runner) cloneExtraRepo(ctx context.Context, e bootstrapEnv, repo targetRepo, readOnly bool, cloneURL string) { //nolint:gocognit,gocritic,gocyclo,cyclop,nestif
	// A whole-run read-only surface makes every clone read-only; a context repo is
	// read-only even on a writable engineer run (ward#573).
	ro := readOnly || e.ReadOnly
	external := cloneURL != ""
	mirror := filepath.Join(e.GitCache, repo.Owner+"__"+repo.Name+".git")
	url := e.CloneBase + "/" + repo.Owner + "/" + repo.Name + ".git"
	if external {
		url = cloneURL
	}
	lock := filepath.Join(e.GitCache, "."+repo.Owner+"__"+repo.Name+".lock")
	ttl := time.Duration(0)
	if ro {
		ttl = containerReadOnlyExtraRepoTTL()
	}
	r.withFlock(lock, func() {
		switch {
		case isDir(mirror):
			if ttl > 0 && !substrateMirrorStale(mirror, int64(ttl.Seconds()), time.Now()) {
				blog("extra-repo: cached mirror fresh %s/%s (advisor TTL %s)", repo.Owner, repo.Name, ttl)
			} else {
				blog("extra-repo: refreshing cached mirror %s/%s", repo.Owner, repo.Name)
				if uerr := r.Runner.Exec(ctx, "git", "-C", mirror, "remote", "update", "--prune"); uerr != nil {
					blog("extra-repo: mirror refresh failed %s/%s (using cached state)", repo.Owner, repo.Name)
				}
			}
		case external:
			// An external dep is never cloned in-container (no egress/ssh key, Forgejo mirror
			// rejected): it is host-side-seeded, so an absent mirror fails loud below (ward#612).
			_ = os.RemoveAll(mirror)
		default:
			blog("extra-repo: cloning mirror (first time) %s/%s", repo.Owner, repo.Name)
			if cerr := r.Runner.Exec(ctx, "git", "clone", "--mirror", url, mirror); cerr != nil {
				blog("extra-repo: mirror clone failed %s/%s (skipping)", repo.Owner, repo.Name)
				_ = os.RemoveAll(mirror)
			}
		}
	})
	if !isDir(mirror) {
		if external {
			// Fail loud (ward#612): name the dep + why it did not arrive, and clear the
			// stale lock so the gap never reads as "source available" (the ward#611 bug).
			_ = os.Remove(lock)
			blog("MISSING DEPENDENCY: external catalog dependency %s/%s (%s) did not hydrate: "+
				"no host-side ssh seed reached the gitcache mirror, and the sealed container "+
				"cannot clone %s itself. Seed it host-side over ssh (WARD_GITCACHE) or the "+
				"sibling ../%s reference clone will be absent (ward#611, ward#612).",
				repo.Owner, repo.Name, url, repo.Owner, repo.Name)
		}
		return
	}
	work := filepath.Join(workspaceRoot, repo.Name)
	_ = os.RemoveAll(work)
	if cerr := r.Runner.Exec(ctx, "git", "clone", mirror, work); cerr != nil {
		blog("extra-repo: working clone failed %s/%s", repo.Owner, repo.Name)
		return
	}
	_ = r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "origin", url)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "config", "push.default", "current")
	if readOnly {
		// A context repo is reference only: no feature branch, no pre-commit gate
		// (nothing is committed here), push guard forced on even on a writable run.
		r.applyReadOnlyPushGuard(ctx, work)
		blog("context-repo: ready %s/%s at %s (read-only reference)", repo.Owner, repo.Name, work)
		return
	}
	// A writable grant (incl. a whole-run read-only surface's --repo) keeps its
	// feature branch + pre-commit gate; installReadOnlyPushGuard fires iff e.ReadOnly.
	r.checkoutRunBranch(ctx, work, e.Branch, "extra repo "+repo.Owner+"/"+repo.Name)
	r.installPreCommitHooks(ctx, e, work)
	r.installReadOnlyPushGuard(ctx, e, work)
	blog("extra-repo: ready %s/%s at %s", repo.Owner, repo.Name, work)
}

// checkoutRunBranch resumes the run branch from origin/<branch> when the refreshed
// clone already has it, otherwise it preserves the existing create-from-base behavior.
func (r *Runner) checkoutRunBranch(ctx context.Context, work, branch, scope string) {
	if branch == "" {
		return
	}
	remoteRef := "refs/remotes/origin/" + branch
	if r.execIn(ctx, work, "git", "show-ref", "--verify", "--quiet", remoteRef) == nil {
		_ = r.execIn(ctx, work, "git", "checkout", "-B", branch, "origin/"+branch)
		blog("%s: branch resume from origin/%s in %s", scope, branch, work)
		return
	}
	_ = r.execIn(ctx, work, "git", "checkout", "-B", branch)
	blog("%s: branch start from cloned base in %s (no origin/%s)", scope, work, branch)
}

// --- pre-commit parity (ward#133) --------------------------------------------

// installPreCommitHooks ports install_precommit_hooks: register the repo's
// pre-commit + commit-msg hooks so agent commits hit the same gate a human's do.
func (r *Runner) installPreCommitHooks(ctx context.Context, _ bootstrapEnv, work string) {
	blog("pre-commit hook install check: %s", work)
	if !isFile(filepath.Join(work, ".pre-commit-config.yaml")) {
		blog("no .pre-commit-config.yaml in %s; skipping pre-commit install", work)
		return
	}
	if !commandExists("pre-commit") {
		blog("pre-commit not on PATH; agent commits will NOT run the repo hook suite (ward#133)")
		return
	}
	// Short-circuit like the bash `( cd && A && B )`: skip the second on failure.
	ok := r.execIn(ctx, work, "pre-commit", "install") == nil &&
		r.execIn(ctx, work, "pre-commit", "install", "--hook-type", "commit-msg") == nil
	if ok {
		blog("installed pre-commit hooks in %s (ward#133)", work)
	} else {
		blog("pre-commit install failed in %s; agent commits may bypass the hook suite (ward#133)", work)
	}
}

// --- read-only push guard (ward#299) -----------------------------------------

// readOnlyPushGuardHook is the per-clone pre-push hook body: it fires before git
// contacts the remote with the clear named wall (ward#299, agent-surface.md).
const readOnlyPushGuardHook = `#!/bin/sh
# ward#299 read-only explore push guard (message layer; bypassable). See ward#315.
echo "ward: read-only explore - this clone can't push (ward#293, ward#315)." >&2
echo "Commit/branch locally; to ship, file an issue + dispatch 'warded #N'." >&2
exit 1
`

// noPushURL is the dead push target a read-only clone's origin push URL is pointed
// at: a scheme git cannot resolve, so a push has nowhere to go (ward#327).
const noPushURL = "no-push://read-only-explore"

// revokeClonePushURL points a read-only clone's origin push URL at noPushURL,
// leaving fetch intact: a target boundary past the credential drop + hook (ward#327).
func (r *Runner) revokeClonePushURL(ctx context.Context, work string) {
	if err := r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "--push", "origin", noPushURL); err != nil {
		blog("could not strip push URL on %s; credential drop + pre-push hook still guard it (ward#327): %v", work, err)
		return
	}
	blog("stripped origin push URL on %s -> %s (ward#327)", work, noPushURL)
}

// installReadOnlyPushGuard ports install_readonly_push_guard: a read-only session
// strips origin's push URL (ward#327) + lands a pre-push hook (ward#299, see docs).
func (r *Runner) installReadOnlyPushGuard(ctx context.Context, e bootstrapEnv, work string) {
	if !e.ReadOnly {
		blog("read-only push guard skipped: writable session %s", work)
		return
	}
	r.applyReadOnlyPushGuard(ctx, work)
}

// applyReadOnlyPushGuard installs the push guard unconditionally (strip push URL,
// land the pre-push hook); the whole-run gate is installReadOnlyPushGuard (ward#573).
func (r *Runner) applyReadOnlyPushGuard(ctx context.Context, work string) {
	blog("read-only push guard install start: %s", work)
	r.revokeClonePushURL(ctx, work)
	hookDir := filepath.Join(work, ".git", "hooks")
	if !isDir(hookDir) {
		blog("no .git/hooks in %s; skipping read-only push guard (ward#299)", work)
		return
	}
	hook := filepath.Join(hookDir, "pre-push")
	if werr := os.WriteFile(hook, []byte(readOnlyPushGuardHook), 0o755); werr != nil {
		blog("could not install read-only push guard in %s: %v (ward#299)", work, werr)
		return
	}
	// chmod too: WriteFile only sets the mode on create, and git needs the exec bit.
	_ = os.Chmod(hook, 0o755)
	blog("installed read-only push guard in %s (ward#299)", work)
}

// --- warm the substrate reference repos (best-effort) ------------------------

// warmSubstrateRepo ports warm_substrate_repo, never fatal; the mirror-ensure
// core is the shared syncGitRef (gitsync.go, ward#654).
func (r *Runner) warmSubstrateRepo(ctx context.Context, e bootstrapEnv, owner, name, tier string) {
	seed := ""
	if tier == "image" {
		seed = filepath.Join(e.SubstrateSeed, owner+"__"+name+".git")
	}
	work := filepath.Join(e.SubstrateDest, name)
	// A substrate working copy is container-local and always re-dropped fresh.
	_ = os.RemoveAll(work)
	ttl, _ := strconv.ParseInt(e.SubstrateTTL, 10, 64)
	_, err := r.syncGitRef(ctx, gitRefSpec{
		url:    e.ForgejoBase + "/" + owner + "/" + name + ".git",
		mirror: filepath.Join(e.GitCache, owner+"__"+name+".git"),
		lock:   filepath.Join(e.GitCache, "."+owner+"__"+name+".lock"),
		work:   work,
		seed:   seed,
		logf: func(format string, a ...any) {
			blog("substrate: "+format, a...)
		},
	}, time.Duration(ttl)*time.Second)
	if err != nil {
		blog("substrate: sync failed %s/%s (skipping): %v", owner, name, err)
	}
}

// prepareConfigBundleCache pre-creates the gitcache's config-bundle dir while
// the bootstrap runs as root, so the agent's WARD_CONFIG_REF caches there (ward#654).
func prepareConfigBundleCache(e bootstrapEnv) {
	dir := filepath.Join(e.GitCache, "config-bundle")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		blog("config-bundle cache: %v (in-container refs fall back to the home cache)", err)
		return
	}
	// Chmod past the umask: the agent user, not root, writes here.
	_ = os.Chmod(dir, 0o777)
	blog("config-bundle cache ready at %s", dir)
}

// warmSubstrate ports warm_substrate: walk the manifest and warm each repo,
// skipping the target (clone_target owns it). Best-effort.
func (r *Runner) warmSubstrate(ctx context.Context, e bootstrapEnv) {
	if e.SubstrateSkip {
		blog("substrate warming skipped (WARD_SUBSTRATE_SKIP=1)")
		return
	}
	if !isFile(e.SubstrateManifest) {
		blog("substrate: no manifest at %s (skipping)", e.SubstrateManifest)
		return
	}
	_ = os.MkdirAll(e.GitCache, 0o755)
	_ = os.MkdirAll(e.SubstrateDest, 0o755)
	data, rerr := os.ReadFile(e.SubstrateManifest) // #nosec G304 -- bind-mounted manifest path
	if rerr != nil {
		blog("substrate: no manifest at %s (skipping)", e.SubstrateManifest)
		return
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		ref := fields[0]
		tier := "cache"
		if len(fields) > 1 {
			tier = fields[1]
		}
		owner, name, ok := splitOwnerName(ref)
		if !ok {
			continue
		}
		if owner == e.TargetOwner && name == e.TargetName {
			continue
		}
		r.warmSubstrateRepo(ctx, e, owner, name, tier)
	}
	blog("substrate ready under %s", e.SubstrateDest)
}

// splitOwnerName splits `owner/name` on the first `/`, mirroring the bash
// `${ref%%/*}` / `${ref##*/}`; both halves must be non-empty.
func splitOwnerName(ref string) (owner, name string, ok bool) {
	i := strings.IndexByte(ref, '/')
	if i < 0 {
		return "", "", false
	}
	owner = ref[:i]
	name = ref[strings.LastIndexByte(ref, '/')+1:]
	if owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}

// --- compose per-mode operating context (the least-context ladder) -----------

// readOnlyContextBlock is a read-only session's static "do not push" entry context
// (ward#293). Kept in sync with the same block in entrypoint.sh's compose_context.
const readOnlyContextBlock = `

---

## Read-only session (this overrides the autonomy doctrine above)

This is the **director's read-only surface session** (` + "`warded director`" + ` surfaced it when the
headless lane drained, or at startup before the first drain). Here "read-only" means one
thing: **this clone cannot push to its own remote**, so nothing leaves this clone. It does
not mean you are sealed off. The natural product of a surface session is commissioned work,
and that still ships. The read-only surface may also dispatch sibling engineers and
advisors when the work should outlive the session.

Capture-and-dispatch is an **obligation, not a "may"**. Every work item you surface -
a bug, a missing test, a follow-up, anything worth doing - you **must**:

- **File an issue** for it (` + "`ward ops forgejo issue create ...`" + `), then
- **Dispatch a sibling headless run** to do the actual fix - ` + "`warded <owner/repo>#N`" + `
  spins up its own sealed container with its own credential and lifecycle, does its
  own implement -> commit -> merge -> push there, and never touches this clone.
  That dispatch inherits the surface's own harness by default, so a Codex director
  sends Codex engineers unless you explicitly override the engineer driver.

Do not let a work item die in the conversation. If you named it, capture it and
dispatch it before you move on.

**Capture-and-dispatch and move on without babysitting.** The director heartbeat that
surfaced you is what polls outcomes, reconciles the lane, and does the chatty back-and-forth
- your job in this seat is to read, scope, file, and fire, then **exit to hand control back
to the heartbeat**. You file the issue, fire the headless run, and let it carry itself to
merge - you do not sit on it, poll it, or wait for it to report back.

**Prefer a sibling dispatch over an in-session subagent.** When the work is
delegable - a design proposal, a research dig, an implementation - reach for a sibling
warded run (` + "`warded advisor #N`" + ` to design or research, ` + "`warded engineer #N`" + ` to build)
before an in-session subagent. The sibling lands a durable, attributable artifact on
the canonical surface (the issue thread, a pushed commit) that outlives this session,
and the next run can read it. A subagent's output dies in this conversation's
scrollback. Reserve an in-session subagent for read-only fan-out that only feeds
**your** immediate reasoning and never needs to outlive the session.

**How this is wired** (you do not set any of it up - it is ready):

- A ` + "`FORGEJO_TOKEN`" + ` (the coilyco-ops bot's) is present, so ` + "`ward ops forgejo ...`" + ` and
  the dispatcher authenticate out of the box. The token is the bot's full credential,
  so the no-push rule below is a convention you keep, not yet a credential boundary
  (a dispatch-only token is tracked in ward#318).
- The host docker socket is mounted at ` + "`/var/run/docker.sock`" + `, so a dispatched
  ` + "`warded #N`" + ` can spawn its sibling container. If you hit a socket permission error on
  dispatch, the group-grant did not reach this host's socket - see ward#319.

You **must not**:

- Commit and push **this clone**, or merge this clone's tree to ` + "`main`" + `.
- Hand-build an authenticated push URL to get this clone's tree onto the remote by
  another route. (A dispatch-only credential is the proper guard here; until it
  lands, this is a convention you keep - ward#318.)

This clone's push wiring has been removed, so a direct ` + "`git push`" + ` from here fails.
Read the repo, reason about it, answer questions, scratch in ` + "`/scratch`" + ` if it
helps you think - then either **file + dispatch** the work or just exit.
`

// readOnlyTag annotates a log line when the run is read-only.
func readOnlyTag(readOnly bool) string {
	if readOnly {
		return ", read-only"
	}
	return ""
}

// composeContext ports compose_context: write canonical AGENTS.md, then wire
// Claude/Codex load points and Goose hints to that composed doctrine.
func (r *Runner) composeContext(e bootstrapEnv) { //nolint:cyclop
	out := filepath.Join(e.AgentHome, "AGENTS.md")
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	base, _ := os.ReadFile("/opt/ward/AGENTS.container.md") // #nosec G304 -- bind-mounted doctrine
	buf := append([]byte{}, base...)
	level, _ := strconv.Atoi(e.ContextLevel)
	switch {
	case level >= 2 && isDir(e.ContextSrc):
		for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
			if extra, ok := readFileIf(filepath.Join(e.ContextSrc, f)); ok {
				buf = append(buf, []byte("\n\n---\n\n")...)
				buf = append(buf, extra...)
			}
		}
	case level == 1:
		if extra, ok := readFileIf(filepath.Join(e.ContextSrc, "AGENTS.md")); ok {
			buf = append(buf, []byte("\n\n---\n\n")...)
			buf = append(buf, extra...)
		}
	}
	// Label the mounted substrate repos as read-first context (ward#593); empty (no
	// section appended) when no substrate repo is warmed. See docs/container-substrate.md.
	if block := substrateInventoryBlock(e.SubstrateDest); block != "" {
		buf = append(buf, []byte(block)...)
	}
	// A read-only session has no seed to carry the "do not push" constraint, so it
	// rides here as static entry context, overriding the autonomy doctrine (ward#293).
	if e.ReadOnly {
		buf = append(buf, []byte(readOnlyContextBlock)...)
	}
	_ = os.WriteFile(out, buf, 0o644) // #nosec G306 -- operating context, not a secret
	blog("composed context (level %s%s) at %s", e.ContextLevel, readOnlyTag(e.ReadOnly), out)
	adapter := mustAgentAdapter(containerMode(e.Mode))
	for _, rel := range adapter.contextFiles() {
		dest := filepath.Join(e.AgentHome, rel)
		switch rel {
		case filepath.Join(".claude", "CLAUDE.md"), filepath.Join(".codex", "AGENTS.md"):
			r.linkOrCopyContext(filepath.Join("..", "AGENTS.md"), dest, out)
			blog("linked context load point to %s", dest)
		default:
			_ = os.MkdirAll(filepath.Dir(dest), 0o755)
			if werr := os.WriteFile(dest, buf, 0o644); werr == nil { // #nosec G306 -- operating context
				blog("mirrored composed context into %s", dest)
			}
		}
	}
}

func (r *Runner) linkOrCopyContext(linkTarget, dest, src string) {
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	_ = os.Remove(dest)
	if err := os.Symlink(linkTarget, dest); err == nil {
		return
	}
	data, err := os.ReadFile(src) // #nosec G304 -- src is the composed context path.
	if err != nil {
		blog("could not read context for %s: %v", dest, err)
		return
	}
	if werr := os.WriteFile(dest, data, 0o644); werr != nil { // #nosec G306 -- operating context
		blog("could not write context fallback %s: %v", dest, werr)
	}
}

// --- container permission policy ---------------------------------------------

// composePermissions ports compose_permissions: copy the container permission
// policy into the agent's claude settings.json.
func (r *Runner) composePermissions(e bootstrapEnv) {
	blog("permissions compose start: %s", e.AgentHome)
	out := filepath.Join(e.AgentHome, ".claude", "settings.json")
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	data, rerr := os.ReadFile("/opt/ward/settings.container.json") // #nosec G304 -- bind-mounted policy
	if rerr != nil {
		blog("could not read container permission policy: %v", rerr)
		return
	}
	if werr := os.WriteFile(out, data, 0o644); werr != nil { // #nosec G306 -- permission policy, not a secret
		blog("could not write container permission policy: %v", werr)
		return
	}
	blog("wrote container permission policy to %s", out)
}

// --- reaper: deterministic teardown backstop ---------------------------------

// reap ports the bash reap() EXIT trap: salvage residual work before teardown.
// It calls the reap logic in-process (the bash exec'd `ward container reap`).
func (r *Runner) reap(ctx context.Context, work string) {
	if os.Getenv("WARD_REAP_WORK") == "" {
		return
	}
	blog("reaping: salvage residual work before teardown")
	env, eerr := readReapEnv()
	if eerr != nil {
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before the container is removed")
		return
	}
	if !isGitWorkTree(ctx, r, work) {
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before the container is removed")
		return
	}
	if rerr := r.reapWorkTree(ctx, work, env); rerr != nil {
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before the container is removed")
	}
}

// reapWorkTree reaps the target tree then verifies every --repo grant landed too
// (ward#291); the entrypoint defer never releases the reservation (agent launched).
func (r *Runner) reapWorkTree(ctx context.Context, work string, env reapEnv) error {
	terr := r.reapTargetTree(ctx, work, env, false)
	r.verifyExtraReposLanded(ctx, env)
	return terr
}

// --- launch ------------------------------------------------------------------

// launchAgent ports the tail of main(): drop to the agent user via setpriv and
// run the agent (stream-json piped through streamProgress). Non-zero exit just logs.
func (r *Runner) launchAgent(ctx context.Context, e bootstrapEnv, work string, argv []string, stream bool) {
	launch := append(setprivPrefix(e), argv...)
	blog("launch start: stream=%t oneshot=%t work=%s", stream, e.oneshot(), work)
	switch {
	case stream:
		if rerr := r.runStreaming(ctx, work, launch); rerr != nil {
			blog(r.agentDeathLogLine(ctx, e.Container, rerr))
		}
	case e.oneshot() && containerMode(e.Mode) == modeGoose:
		if rerr := r.runGooseCompletionWatch(ctx, work, launch); rerr != nil {
			blog(r.agentDeathLogLine(ctx, e.Container, rerr))
		}
	case e.oneshot():
		if rerr := r.runWithStdin(ctx, work, launch, os.DevNull); rerr != nil {
			blog(r.agentDeathLogLine(ctx, e.Container, rerr))
		}
	default:
		if rerr := r.runWithStdin(ctx, work, launch, ""); rerr != nil {
			blog(r.agentDeathLogLine(ctx, e.Container, rerr))
		}
	}
}

func (r *Runner) launchStdout() io.Writer {
	if r != nil && r.Runner != nil && r.Runner.Stdout != nil {
		return r.Runner.Stdout
	}
	return os.Stdout
}

func (r *Runner) launchStderr() io.Writer {
	if r != nil && r.Runner != nil && r.Runner.Stderr != nil {
		return r.Runner.Stderr
	}
	return os.Stderr
}

// agentDeathLogLine names an OOM kill explicitly when Docker state still knows it.
func (r *Runner) agentDeathLogLine(ctx context.Context, container string, runErr error) string {
	if state, ok := r.inspectContainerState(ctx, container); ok && state.OOMKilled {
		return fmt.Sprintf("agent exited non-zero (%v; docker state: OOMKilled=true); reaping anyway", runErr)
	}
	return fmt.Sprintf("agent exited non-zero (%v); reaping anyway", runErr)
}

// runWithStdin runs launch in work with stdin from stdinPath (os.DevNull pins
// one-shot stdin to EOF; "" keeps the inherited stdin for interactive runs).
func (r *Runner) runWithStdin(ctx context.Context, work string, launch []string, stdinPath string) error {
	cmd := exec.CommandContext(ctx, launch[0], launch[1:]...) // #nosec G204 -- fixed setpriv/agent argv
	cmd.Dir = work
	cmd.Stdout = r.launchStdout()
	cmd.Stderr = r.launchStderr()
	if stdinPath == "" {
		cmd.Stdin = os.Stdin
	} else {
		f, _ := os.Open(stdinPath) // #nosec G304 -- os.DevNull
		if f != nil {
			defer func() { _ = f.Close() }()
		}
		cmd.Stdin = f
	}
	return cmd.Run()
}

// runStreaming runs launch with stdin /dev/null and pipes its stdout through
// streamProgress (the bash `... </dev/null | stream_progress`).
func (r *Runner) runStreaming(ctx context.Context, work string, launch []string) error {
	cmd := exec.CommandContext(ctx, launch[0], launch[1:]...) // #nosec G204 -- fixed setpriv/agent argv
	cmd.Dir = work
	cmd.Stderr = r.launchStderr()
	devnull, _ := os.Open(os.DevNull)
	if devnull != nil {
		defer func() { _ = devnull.Close() }()
	}
	cmd.Stdin = devnull
	pipe, perr := cmd.StdoutPipe()
	if perr != nil {
		return perr
	}
	if serr := cmd.Start(); serr != nil {
		return serr
	}
	streamProgress(pipe, r.launchStdout())
	return cmd.Wait()
}

// gooseCompletionGrace is the short post-final-output window the watchdog gives Goose.
// Ward force-stops a process that keeps running after the terminal answer.
const gooseCompletionGrace = 5 * time.Second

// gooseCompletionOutput reports whether a line looks like Goose's terminal answer.
// The watchdog uses it as the success boundary before requesting exit.
func gooseCompletionOutput(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "implementation complete") ||
		strings.Contains(lower, "requirements satisfied")
}

type gooseCompletionWatch struct {
	sawCompletion <-chan struct{}
	stdoutDone    <-chan error
	waitDone      <-chan error
}

// startGooseCompletionWatch starts the stdout copier and process waiter for the
// Goose completion watchdog.
func startGooseCompletionWatch(cmd *exec.Cmd, stdout io.ReadCloser, w io.Writer) gooseCompletionWatch {
	sawCompletion := make(chan struct{}, 1)
	stdoutDone := make(chan error, 1)
	go func() {
		defer close(stdoutDone)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			writeln(w, line)
			if gooseCompletionOutput(line) {
				select {
				case sawCompletion <- struct{}{}:
				default:
				}
			}
		}
		stdoutDone <- sc.Err()
	}()
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	return gooseCompletionWatch{sawCompletion: sawCompletion, stdoutDone: stdoutDone, waitDone: waitDone}
}

// finishGooseCompletionWatch waits for the watchdog to observe completion and
// then lets the process exit, forcing a kill only if it stays alive.
func finishGooseCompletionWatch(cmd *exec.Cmd, watch gooseCompletionWatch) error {
	select {
	case <-watch.sawCompletion:
		blog("goose terminal completion output observed; requesting exit")
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-watch.waitDone:
		case <-time.After(gooseCompletionGrace):
			blog("goose process still running after terminal completion output; forcing termination")
			_ = cmd.Process.Kill()
			<-watch.waitDone
		}
		scanErr := <-watch.stdoutDone
		if scanErr != nil {
			return scanErr
		}
		blog("goose process exited after terminal completion output")
		return nil
	case scanErr := <-watch.stdoutDone:
		waitErr := <-watch.waitDone
		if scanErr != nil {
			return scanErr
		}
		select {
		case <-watch.sawCompletion:
			blog("goose process exited after terminal completion output")
			return nil
		default:
		}
		if waitErr != nil {
			return waitErr
		}
		return fmt.Errorf("goose exited without terminal completion output")
	}
}

// runGooseCompletionWatch runs a headless Goose under a completion watchdog.
// Once stdout shows the terminal answer, Ward exits the process if needed.
func (r *Runner) runGooseCompletionWatch(ctx context.Context, work string, launch []string) error {
	cmd := exec.CommandContext(ctx, launch[0], launch[1:]...) // #nosec G204 -- fixed setpriv/agent argv
	cmd.Dir = work
	cmd.Stderr = r.launchStderr()
	devnull, _ := os.Open(os.DevNull)
	if devnull != nil {
		defer func() { _ = devnull.Close() }()
	}
	cmd.Stdin = devnull
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return finishGooseCompletionWatch(cmd, startGooseCompletionWatch(cmd, stdout, r.launchStdout()))
}

// setprivPrefix builds the bash launch prefix: drop to the agent uid/gid with
// init-groups and pin HOME (`setpriv ... env HOME=<home>`).
func setprivPrefix(e bootstrapEnv) []string {
	return []string{
		"setpriv", "--reuid=" + e.AgentUID, "--regid=" + e.AgentGID, "--init-groups",
		"env", "HOME=" + e.AgentHome,
	}
}

// chownAgentTree ports the launch-time chown: hand the work tree + agent config
// dirs to the non-root agent user. Best-effort, like the bash `|| true`.
func (r *Runner) chownAgentTree(ctx context.Context, e bootstrapEnv, work string) {
	paths := []string{
		work,
		filepath.Join(e.AgentHome, "AGENTS.md"),
		filepath.Join(e.AgentHome, ".claude"),
		filepath.Join(e.AgentHome, ".claude.json"), // onboarding seed, so claude can persist updates (ward#305)
		filepath.Join(e.AgentHome, ".mcporter"),
		filepath.Join(e.AgentHome, ".config"),
		filepath.Join(e.AgentHome, ".codex"),
	}
	// Hand each granted extra-repo tree to the agent user too (ward#230); they
	// were cloned as root, like the target. Skip any that failed to clone.
	for _, repo := range e.ExtraRepos {
		if dest := filepath.Join(workspaceRoot, repo.Name); isDir(dest) {
			paths = append(paths, dest)
		}
	}
	// Read-only context repos are cloned as root too (ward#573); hand them over
	// so the agent can read them, even though it may never write.
	for _, repo := range e.ContextRepos {
		if dest := filepath.Join(workspaceRoot, repo.Name); isDir(dest) {
			paths = append(paths, dest)
		}
	}
	_ = r.Runner.Exec(ctx, "chown", append([]string{"-R", e.AgentUID + ":" + e.AgentGID}, paths...)...)
}

// --- headless progress (claude stream-json -> concise log lines) -------------

// streamProgress ports stream_progress: parse claude stream-json and emit the
// same concise lines, replacing jq; unparseable lines are skipped (jq fromjson?).
func streamProgress(in io.Reader, w io.Writer) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		for _, out := range streamProgressLines(ev) {
			writeln(w, out)
		}
	}
}

// streamEvent is the minimal shape streamProgress reads from a stream-json line.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Input   streamToolInput `json:"input"`
			IsError bool            `json:"is_error"`
		} `json:"content"`
	} `json:"message"`
	NumTurns   int     `json:"num_turns"`
	DurationMs float64 `json:"duration_ms"`
	Result     string  `json:"result"`
}

// streamToolInput holds the tool_use arg keys the bash jq filter coalesces, in
// the same precedence order (file_path, command, path, pattern, url).
type streamToolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
	Path     string `json:"path"`
	Pattern  string `json:"pattern"`
	URL      string `json:"url"`
}

// firstNonEmpty returns the first non-empty arg, matching the jq `//` chain.
func (t streamToolInput) firstNonEmpty() string {
	for _, v := range []string{t.FilePath, t.Command, t.Path, t.Pattern, t.URL} {
		if v != "" {
			return v
		}
	}
	return ""
}

// streamProgressLines maps one stream-json event to its concise output lines,
// matching the bash jq filter (assistant text/tool_use, user tool error, result).
func streamProgressLines(ev streamEvent) []string {
	switch ev.Type {
	case "assistant":
		return assistantProgressLines(ev)
	case "user":
		return userProgressLines(ev)
	case "result":
		return resultProgressLines(ev)
	default:
		return nil
	}
}

// assistantProgressLines renders an assistant event's text + tool_use blocks.
func assistantProgressLines(ev streamEvent) []string {
	var out []string
	for _, c := range ev.Message.Content {
		switch c.Type {
		case "text":
			t := strings.ReplaceAll(c.Text, "\n", " ")
			if len(t) > 0 {
				out = append(out, "  "+truncate(t, 140))
			}
		case "tool_use":
			arg := strings.ReplaceAll(c.Input.firstNonEmpty(), "\n", " ")
			out = append(out, "● "+c.Name+" "+truncate(arg, 120))
		}
	}
	return out
}

// userProgressLines surfaces a one-line marker for each errored tool result.
func userProgressLines(ev streamEvent) []string {
	var out []string
	for _, c := range ev.Message.Content {
		if c.Type == "tool_result" && c.IsError {
			out = append(out, "  ✗ (tool error)")
		}
	}
	return out
}

// resultProgressLines renders the terminal result summary (subtype, turns, secs).
func resultProgressLines(ev streamEvent) []string {
	subtype := ev.Subtype
	if subtype == "" {
		subtype = "?"
	}
	secs := int(ev.DurationMs / 1000)
	out := []string{fmt.Sprintf("✓ result: %s (%d turns, %ds)", subtype, ev.NumTurns, secs)}
	if ev.Result != "" {
		out = append(out, ev.Result)
	}
	return out
}

// truncate caps s to n runes, matching jq's `.[0:n]` (rune-indexed slice).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// --- small filesystem + exec helpers -----------------------------------------

// execIn runs bin in dir via the Runner's sandbox-aware ExecIn (the bash
// `( cd "$work" && ... )` subshell).
func (r *Runner) execIn(ctx context.Context, dir, bin string, argv ...string) error {
	return r.Runner.ExecIn(ctx, dir, bin, argv...)
}

// captureTrim captures stdout and trims it, returning "" on error (used for the
// readiness branch-name log line).
func (r *Runner) captureTrim(ctx context.Context, bin string, argv ...string) string {
	out, err := r.Runner.Capture(ctx, bin, argv...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// withFlock runs fn while holding an exclusive flock on lockPath (the bash
// `( flock 9 ... ) 9>lock`); a flock failure degrades to running unguarded.
func (r *Runner) withFlock(lockPath string, fn func()) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644) // #nosec G304 -- gitcache lock path
	if err != nil {
		fn()
		return
	}
	defer func() { _ = f.Close() }()
	if lerr := flock.Exclusive(f); lerr != nil {
		fn()
		return
	}
	defer func() { _ = flock.Unlock(f) }()
	fn()
}

// commandExists reports whether bin is on PATH (the bash `command -v`).
func commandExists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// ensureLaunchBinaryAvailable reports a missing selected agent binary as a hard
// bootstrap failure so the run aborts instead of falling back to a shell.
func ensureLaunchBinaryAvailable(agent string) error {
	if commandExists(agent) {
		return nil
	}
	return fmt.Errorf("selected agent binary %q is not present in this image", agent)
}

// installHarness runs the required harness install hook and then verifies the
// selected binary is actually available before launch can proceed.
func installHarness(agent agentsapi.Agent, rc agentsapi.RunCtx) error {
	if err := agent.Install(rc); err != nil {
		return err
	}
	return ensureLaunchBinaryAvailable(agent.Record().Binary)
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// isSocket reports whether path exists and is a unix socket (`[ -S ]`); used to
// probe the mounted docker socket before granting dispatch access (ward#315).
func isSocket(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// readFileIf reads path, returning (data, true) only when it is a regular file
// (the bash `[ -f X ] && cat X`).
func readFileIf(path string) ([]byte, bool) {
	if !isFile(path) {
		return nil, false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- bind-mounted operating-context path
	if err != nil {
		return nil, false
	}
	return data, true
}
