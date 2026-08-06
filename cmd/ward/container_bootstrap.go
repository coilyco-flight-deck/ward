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
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/flock"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/urfave/cli/v3"
)

// container_bootstrap.go implements the PID-1 bootstrap behind
// containerEntrypointScript (ward#181). See docs/container.md.

// bootstrapEnv holds the entrypoint's env-var config, read once with the bash
// defaults applied. Required vars (the bash `:?` checks) error in readBootstrapEnv.
type bootstrapEnv struct {
	TargetOwner   string
	TargetName    string
	ForgejoBase   string
	Mode          string
	Container     string
	Issue         int
	Agent         string
	ContextLevel  string
	GitCache      string
	ContextSrc    string
	OpencodeModel string
	GooseModel    string
	OllamaURL     string
	// Cheapest codex posture by default (ward#379): mini model + low reasoning +
	// verbosity, each overridable via WARD_CODEX_*. docs/agent-harnesses.md.
	CodexModel     string
	CodexEffort    string
	CodexVerbosity string
	// claude model + effort are override-only (ward#616): empty defaults keep today's
	// bare launch; WARD_CLAUDE_MODEL / WARD_CLAUDE_REASONING_EFFORT fill them.
	ClaudeModel  string
	ClaudeEffort string
	// AgentDisplayName/Pronouns are explicit harness-owner inputs (ward#1465),
	// separate from Git author attribution.
	AgentDisplayName string
	AgentPronouns    string
	GitUserName      string
	GitUserEmail     string
	// Role is opaque workflow metadata (WARD_ROLE, ward#620). It selects no
	// model, identity, credentials, mounts, network, or authority.
	Role       string
	AgentUID   string
	AgentGID   string
	AgentHome  string
	MirrorName string
	Branch     string
	Headless   bool
	Ask        bool
	// ReadOnly is the read-only surface session (WARD_READONLY, ward#293): revoke
	// the push credential, compose the restriction. See docs/agent-director.md.
	ReadOnly          bool
	WardVersionSource string
	WardVersion       string
	ForgejoHost       string
	// Forge is the TARGET repo's host (ward#489, GitLab added in #635): GitHub and GitLab
	// clone off CloneBase with their own push users, else Forgejo + coilyco-ops.
	Forge     forge
	CloneBase string
	CloneHost string
	// ExtraRepos are the additional writable repos this run was granted via
	// --repo (WARD_EXTRA_REPOS); each is cloned full at /workspace/<owner>/<repo>.
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
	ContextBundle     string
	ContextTools      string
	Collaboration     bool
	ClusterID         string
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

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// readBootstrapEnv reads + defaults the entrypoint env, erroring on a missing
// required var (the bash `: "${X:?...}"` checks). Pure given the environment.
func readBootstrapEnv() (bootstrapEnv, error) { //nolint:gocyclo,cyclop // repository-backed and collaboration plans have distinct required inputs
	// Harness mechanics are typed product defaults. Model, reasoning, identity,
	// and endpoint values come only from explicit environment or --config inputs.
	launch, ferr := loadLaunchConfig()
	if ferr != nil {
		return bootstrapEnv{}, fmt.Errorf("load typed harness config for bootstrap defaults: %w", ferr)
	}
	role := os.Getenv("WARD_ROLE")
	mode := envOr("WARD_MODE", launch.DefaultAgent)
	identity := containerMode(mode).defaultAgentIdentity()
	e := bootstrapEnv{
		TargetOwner:       os.Getenv("WARD_TARGET_OWNER"),
		TargetName:        os.Getenv("WARD_TARGET_NAME"),
		ForgejoBase:       os.Getenv("WARD_FORGEJO_BASE"),
		Mode:              mode,
		Container:         os.Getenv("WARD_CONTAINER_NAME"),
		Issue:             0,
		Agent:             envOr("WARD_AGENT", string(modeClaude)),
		ContextLevel:      envOr("WARD_CONTEXT_LEVEL", "2"),
		GitCache:          envOr("WARD_GITCACHE", "/gitcache"),
		ContextSrc:        envOr("WARD_CONTEXT_SRC", "/opt/ward-context"),
		OpencodeModel:     os.Getenv("WARD_OPENCODE_MODEL"),
		GooseModel:        os.Getenv("WARD_GOOSE_MODEL"),
		OllamaURL:         os.Getenv("WARD_OLLAMA_URL"),
		CodexModel:        os.Getenv("WARD_CODEX_MODEL"),
		CodexEffort:       os.Getenv("WARD_CODEX_REASONING_EFFORT"),
		CodexVerbosity:    os.Getenv("WARD_CODEX_VERBOSITY"),
		ClaudeModel:       os.Getenv("WARD_CLAUDE_MODEL"),
		ClaudeEffort:      os.Getenv("WARD_CLAUDE_REASONING_EFFORT"),
		AgentDisplayName:  envOr(envAgentDisplayName, identity.Name),
		AgentPronouns:     envOr(envAgentPronouns, identity.Pronouns),
		GitUserName:       os.Getenv("WARD_GIT_NAME"),
		GitUserEmail:      os.Getenv("WARD_GIT_EMAIL"),
		Role:              role,
		AgentUID:          envOr("WARD_AGENT_UID", "1000"),
		AgentGID:          envOr("WARD_AGENT_GID", "1000"),
		AgentHome:         envOr("WARD_AGENT_HOME", "/home/ubuntu/.ward"),
		MirrorName:        os.Getenv("WARD_MIRROR_NAME"),
		Branch:            os.Getenv("WARD_BRANCH"),
		Headless:          os.Getenv("WARD_HEADLESS") == "1",
		Ask:               os.Getenv("WARD_ASK") == "1",
		ReadOnly:          os.Getenv("WARD_READONLY") == "1",
		WardVersionSource: envOr(envAgentVersionSource, ""),
		WardVersion:       envOr("WARD_VERSION", ""),

		SubstrateSeed:     envOr("WARD_SUBSTRATE_SEED", "/opt/substrate-seed"),
		SubstrateDest:     envOr("WARD_SUBSTRATE_DEST", "/substrate"),
		SubstrateManifest: envOr("WARD_SUBSTRATE_MANIFEST", "/opt/ward/preclone-repos.txt"),
		SubstrateTTL:      envOr("WARD_SUBSTRATE_TTL", "600"),
		SubstrateSkip:     os.Getenv("WARD_SUBSTRATE_SKIP") == "1",
		ContextBundle:     os.Getenv("WARD_CONTEXT_BUNDLE"),
		ContextTools:      os.Getenv("WARD_CONTEXT_TOOLS"),
		Collaboration:     os.Getenv(envCollaborationPlan) == "1",
		ClusterID:         os.Getenv(envClusterID),
	}
	if !e.Collaboration && e.TargetOwner == "" {
		return e, fmt.Errorf("missing WARD_TARGET_OWNER")
	}
	if !e.Collaboration && e.TargetName == "" {
		return e, fmt.Errorf("missing WARD_TARGET_NAME")
	}
	if !e.Collaboration && e.ForgejoBase == "" {
		return e, fmt.Errorf("missing WARD_FORGEJO_BASE")
	}
	if e.Collaboration && !validClusterID(e.ClusterID) {
		return e, fmt.Errorf("invalid repository-free collaboration cluster id %q", e.ClusterID)
	}
	if mode, err := parseMode(e.Mode); err == nil {
		model := e.OpencodeModel
		if mode == modeGoose {
			model = e.GooseModel
		}
		if err := validateLocalHarnessConfig(mode, model, e.OllamaURL); err != nil {
			return e, err
		}
	}
	e.ForgejoHost = forgejoHostFromBase(e.ForgejoBase)
	// The TARGET forge + clone base (ward#489); CloneBase defaults to the Forgejo base
	// so a run that names neither behaves exactly as before.
	e.Forge = parseForge(os.Getenv("WARD_FORGE"))
	e.CloneBase = envOr("WARD_CLONE_BASE", e.ForgejoBase)
	e.CloneHost = forgejoHostFromBase(e.CloneBase)
	if !e.Collaboration {
		e.ExtraRepos = parseExtraReposEnv(os.Getenv("WARD_EXTRA_REPOS"), e.TargetOwner, e.TargetName)
	}
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
		repo, err := normalizedTargetRepo(owner, name, ref)
		if err != nil {
			continue
		}
		target := targetRepo{Owner: targetOwner, Name: targetName}
		if repo.canonicalSlug() == target.canonicalSlug() {
			continue
		}
		slug := repo.canonicalSlug()
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, repo)
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
	scope := "repo:     " + ref
	if e.Collaboration {
		ref = e.ClusterID
		scope = "cluster:  " + e.ClusterID + "\nrepo:     (none)"
	}
	if e.Issue != 0 {
		ref = fmt.Sprintf("%s#%d", ref, e.Issue)
	}
	seed := "(no seed argv; interactive or seedless run)"
	if len(agentArgs) > 0 {
		seed = strings.Join(agentArgs, " ")
		if requestID := brokeredDispatchRequestID(); requestID != "" {
			seed = fmt.Sprintf("(seed omitted from brokered container context log; dispatch request %s; %s)", requestID, seedSummary(seed))
		}
	}
	writef(os.Stderr, "===== ward run context (ward#609) =====\n"+
		"%s\nref:      %s\nbranch:   %s\ndriver:   %s (agent %s)\nrun:      %s\n"+
		"workflow: %s\nward:     %s\nup:       %s\n----- seed / task text -----\n%s\n"+
		"===== end ward run context =====\n",
		scope, ref, orDefaultLabel(e.Branch, "(default)"),
		e.Mode, e.Agent, orDefaultLabel(e.Container, "(unnamed)"),
		orDefaultLabel(os.Getenv("WARD_WORKFLOW"), "merge-remote-main"),
		orDefaultLabel(e.WardVersionSource, wardVersionLaunchLabel(e.WardVersion, "")),
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
		return rc.GooseModel, "", rc.OllamaURL
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
func (r *Runner) runContainerBootstrap(ctx context.Context, c *cli.Command) error { //nolint:funlen,gocyclo,cyclop,gocognit
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
	if e.Collaboration {
		_ = os.Unsetenv("WARD_REAP_WORK")
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

	if !e.Collaboration {
		applyWardGitIdentityFallback(e.GitUserName, e.GitUserEmail)
		r.configureGitAuth(ctx, e)
	}
	blog("bootstrap installer start: %s", mode)
	if err := installHarness(agent, rc); err != nil {
		blog("fatal: %v", err)
		writeGateFailure("bootstrap", err.Error())
		return err
	}
	blog("bootstrap installer done: %s", mode)
	work := surfaceScratchMnt
	if e.Collaboration {
		if err := r.prepareScratchSpace(work); err != nil {
			return err
		}
		blog("bootstrap collaboration plan: repository resolution and clone skipped for cluster %s", e.ClusterID)
	} else {
		var cerr error
		work, cerr = r.cloneTarget(ctx, e)
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
	}
	blog("bootstrap substrate warm start")
	r.warmSubstrate(ctx, e)
	blog("bootstrap substrate warm done")
	if e.Collaboration {
		r.makeReadOnlyTree(e.SubstrateDest)
	}
	blog("bootstrap context compose start")
	var contextErr error
	if strings.TrimSpace(e.ContextBundle) == "" {
		contextErr = r.composeContext(e)
	} else {
		contextErr = r.projectContextBundleHome(e)
	}
	if contextErr != nil {
		blog("fatal: %v", contextErr)
		writeGateFailure("context", contextErr.Error())
		return contextErr
	}
	// Set the trust set here, post-warm, not in agentRunCtx (which runs pre-warm)
	// so the /substrate dirs already exist to enumerate (ward#168).
	rc.TrustDirs = agentTrustDirs(e)
	// Selected credentials, permissions, onboarding, and config compose here.
	// composeAgentContainer feature-tests each adapter capability (ward#418).
	composeAgentContainer(agent, rc)
	scrubHarnessBootstrapEnv()
	blog("bootstrap agent container composition done")

	_ = os.Setenv("WARD_REAP_WORK", work)
	scratchDir := surfaceScratchDir(e)
	if err := r.prepareScratchSpace(scratchDir); err != nil {
		blog("fatal: %v", err)
		writeGateFailure("bootstrap", err.Error()) // reaper release-comment context (ward#609)
		return err
	}
	if err := ensureScratchAlias(scratchDir, surfaceScratchMnt); err != nil {
		blog("fatal: %v", err)
		writeGateFailure("bootstrap", err.Error()) // reaper release-comment context (ward#609)
		return err
	}
	if !e.Collaboration {
		defer r.reap(ctx, work)
		branch := r.captureTrim(ctx, "git", "-C", work, "branch", "--show-current")
		blog("ready: %s/%s on %s [mode=%s]", e.TargetOwner, e.TargetName, branch, e.Mode)
	} else {
		blog("ready: collaboration cluster %s in %s [mode=%s]", e.ClusterID, work, e.Mode)
	}

	argv, stream := buildAgentArgv(e, agentArgs)
	logAgentArgv(e, agentArgs)

	// Drop to the non-root agent user (claude refuses bypass-perms as root, ward#127).
	r.chownAgentTree(ctx, e, work)
	if e.ReadOnly {
		// Read-only surface: hand Forgejo access to the root-held broker pre-drop.
		// setprivPrefix removes the raw token from the dropped process (ward#1521).
		r.makeReadOnlyTree(work)
		r.revokePushCredential(ctx)
		if berr := r.startCredentialBroker(ctx, e); berr != nil {
			blog("fatal: %v", berr)
			writeGateFailure("broker", berr.Error())
			return berr
		}
		r.grantDockerSocketAccess(ctx, e)
	} else if !e.Collaboration {
		if cerr := r.ensureGitCredReadable(e); cerr != nil {
			// Re-assert the credential perms git's `store` helper clobbered on the clones,
			// else the dropped agent falls back to the human token (ward#288).
			blog("fatal: %v", cerr)
			writeGateFailure("bootstrap", cerr.Error()) // reaper release-comment context (ward#609)
			return cerr
		}
	}
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
	_ = os.Setenv(envAgentLaunched, "1")
	if lerr := r.launchAgent(ctx, e, work, argv, stream, agentArgs); lerr != nil {
		blog("launch failed: %v", lerr)
		writeGateFailure("harness-cli", lerr.Error())
		return fmt.Errorf("%s launch failed: %w", e.Agent, lerr)
	}
	blog("bootstrap launch returned: agent process exited, deferred reaper runs next")
	return nil
}

func scrubHarnessBootstrapEnv() {
	for _, key := range harnessBootstrapEnvKeys {
		_ = os.Unsetenv(key)
	}
}

const surfaceScratchFloorBytes = 8 * 1024 * 1024

// surfaceScratchMnt is the doctrine-promised scratch path: the composed agent
// context names it as the writable escape hatch on every surface.
var surfaceScratchMnt = "/scratch"

// surfaceScratchRoot returns the writable cache/temp root for this surface.
// Read-only director sessions use the gitcache volume for Go verification headroom.
func surfaceScratchRoot(readOnly bool, gitcache string) string {
	if readOnly {
		return rootedPathJoin(gitcache, "surface-scratch")
	}
	return surfaceScratchMnt
}

// surfaceScratchDir returns the writable cache/temp root for this surface.
func surfaceScratchDir(e bootstrapEnv) string {
	return surfaceScratchRoot(e.ReadOnly, e.GitCache)
}

// directorSurfaceScratchDir returns the scratch root the host gate advertises
// before the container launches.
func directorSurfaceScratchDir(readOnly bool) string {
	return surfaceScratchRoot(readOnly, containerGitcacheMnt)
}

// surfaceScratchGoCacheDir returns the Go build cache root under the surface
// scratch location.
func surfaceScratchGoCacheDir(scratchDir string) string {
	return rootedPathJoin(scratchDir, "go-build")
}

// surfaceScratchBudgetReport renders the current free-space budget for the
// scratch volume.
func surfaceScratchBudgetReport(scratchDir string) string {
	free, total, err := surfaceScratchDiskFreeBytes(scratchDir)
	if err != nil {
		return "disk usage unavailable"
	}
	return fmt.Sprintf("%s free of %s", diskBytes(free), diskBytes(total))
}

// prepareScratchSpace provisions the writable throwaway area and points common temp
// and Go cache env vars at it.
func (r *Runner) prepareScratchSpace(scratchDir string) error {
	if err := os.MkdirAll(scratchDir, 0o1777); err != nil {
		return fmt.Errorf("prepare scratch/cache root %s: %w", scratchDir, err)
	}
	subdirs := []string{
		surfaceScratchGoCacheDir(scratchDir),
		rootedPathJoin(scratchDir, "go-mod"),
		rootedPathJoin(scratchDir, "go-tmp"),
		rootedPathJoin(scratchDir, "xdg-cache"),
	}
	for _, dir := range subdirs {
		if err := os.MkdirAll(dir, 0o1777); err != nil {
			return fmt.Errorf("prepare scratch/cache dir %s: %w", dir, err)
		}
		_ = os.Chmod(dir, 0o1777)
	}
	_ = os.Chmod(scratchDir, 0o1777)
	if err := surfaceScratchBudgetError(scratchDir); err != nil {
		return err
	}
	_ = os.Setenv("TMPDIR", scratchDir)
	_ = os.Setenv("TMP", scratchDir)
	_ = os.Setenv("TEMP", scratchDir)
	_ = os.Setenv("GOCACHE", surfaceScratchGoCacheDir(scratchDir))
	_ = os.Setenv("GOMODCACHE", rootedPathJoin(scratchDir, "go-mod"))
	_ = os.Setenv("GOTMPDIR", rootedPathJoin(scratchDir, "go-tmp"))
	_ = os.Setenv("XDG_CACHE_HOME", rootedPathJoin(scratchDir, "xdg-cache"))
	blog("scratch/cache area ready at %s (%s; Go caches under %s)", scratchDir, surfaceScratchBudgetReport(scratchDir), surfaceScratchGoCacheDir(scratchDir))
	return nil
}

// ensureScratchAlias makes the doctrine-promised alias real when the actual scratch
// root lives elsewhere - read-only surfaces keep it on the gitcache volume (ward#1142).
func ensureScratchAlias(scratchDir, alias string) error {
	if scratchDir == alias {
		return nil
	}
	if target, err := os.Readlink(alias); err == nil && target == scratchDir {
		return nil
	}
	// Lstat does not follow links, so a stale symlink lands in the removal
	// branch while a real directory (already writable scratch) is kept.
	if fi, err := os.Lstat(alias); err == nil {
		if fi.IsDir() {
			return nil
		}
		if rerr := os.Remove(alias); rerr != nil {
			return fmt.Errorf("replace stale scratch alias %s: %w", alias, rerr)
		}
	}
	if err := os.Symlink(scratchDir, alias); err != nil {
		return fmt.Errorf("link scratch alias %s -> %s: %w", alias, scratchDir, err)
	}
	blog("scratch alias ready: %s -> %s", alias, scratchDir)
	return nil
}

func diskBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
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

// configureGitAuth installs fail-closed system Git policy plus a read-only
// credential helper readable by root (reaper) and the dropped agent group.
func (r *Runner) configureGitAuth(ctx context.Context, e bootstrapEnv) {
	for _, args := range gitSystemPolicyArgs() {
		_ = r.Runner.Exec(ctx, "git", args...)
	}
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

func gitSystemPolicyArgs() [][]string {
	return [][]string{
		{"config", "--system", "user.useConfigOnly", "true"},
		{"config", "--system", "init.defaultBranch", "main"},
		{"config", "--system", "--add", "safe.directory", "*"},
	}
}

// revokePushCredential drops this clone's push wiring; root retains its token.
// setprivPrefix removes it from the dropped read-only agent (ward#1521).
func (r *Runner) revokePushCredential(ctx context.Context) {
	_ = os.Remove(forgejoGitCredentialsPath)
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "--unset-all", "credential.helper")
	blog("read-only session: dropped this clone's push wiring; Forgejo access is brokered and the dropped agent receives no FORGEJO_TOKEN (ward#1521)")
}

// grantDockerSocketAccess lets the dropped agent reach the mounted socket.
// It supports dispatch/reap without host changes (ward#315, ward#319).
func (r *Runner) grantDockerSocketAccess(ctx context.Context, e bootstrapEnv) {
	const sock = "/var/run/docker.sock"
	if !isSocket(sock) {
		blog("surface: no docker socket mounted - dispatch and reap unavailable this run (ward#315)")
		return
	}
	info, err := os.Stat(sock)
	if err != nil {
		blog("surface: could not stat docker socket; dispatch or reap may fail: %v (ward#315)", err)
		return
	}
	sockgid, ok := fileGID(info)
	if !ok {
		blog("surface: could not read docker socket gid; dispatch or reap may fail (ward#315)")
		return
	}
	if sockgid == 0 {
		r.bridgeDockerSocket(ctx, e, sock) // root:root: no group to join, bridge it (ward#319)
		return
	}
	u, uerr := user.LookupId(e.AgentUID)
	if uerr != nil {
		blog("surface: no passwd entry for uid %s; cannot group-grant the socket (ward#315)", e.AgentUID)
		return
	}
	gidStr := strconv.Itoa(sockgid)
	// Create a group with the socket's gid if none exists (container-only), then add
	// the agent to it. No chmod/chown touches the bind-mounted socket inode.
	if _, gerr := user.LookupGroupId(gidStr); gerr != nil {
		_ = r.Runner.Exec(ctx, "groupadd", "-g", gidStr, "dockerhost")
	}
	if aerr := r.Runner.Exec(ctx, "usermod", "-aG", gidStr, u.Username); aerr != nil {
		blog("surface: could not add %s to socket group (gid %s); dispatch or reap may fail: %v (ward#315)", u.Username, gidStr, aerr)
		return
	}
	blog("surface: granted docker socket access to %s via group gid %s; no socket perms changed (ward#315)", u.Username, gidStr)
}

// bridgeDockerSocket bridges a root:root docker socket to an agent-group-owned socket
// via root socat, reached through DOCKER_HOST with no host-perm change (ward#319).
func (r *Runner) bridgeDockerSocket(ctx context.Context, e bootstrapEnv, sock string) {
	const bridge = "/tmp/docker-agent.sock"
	if !commandExists("socat") {
		blog("surface: socat absent from image; dispatch and reap unavailable on a root:root socket (ward#319)")
		return
	}
	_ = os.Remove(bridge)
	listen := fmt.Sprintf("UNIX-LISTEN:%s,fork,group=%s,mode=0660", bridge, e.AgentGID)
	cmd := exec.CommandContext(ctx, "socat", listen, "UNIX-CONNECT:"+sock) // #nosec G204 -- fixed socat bridge argv
	if serr := cmd.Start(); serr != nil {
		blog("surface: could not start docker socket bridge; dispatch or reap may fail: %v (ward#319)", serr)
		return
	}
	_ = os.Setenv("DOCKER_HOST", "unix://"+bridge)
	blog("surface: bridged root:root docker socket to %s for the agent (gid %s; ward#319)", bridge, e.AgentGID)
}

// ensureGitCredReadable re-asserts the credential perms stay readable by the
// dropped agent and the root reaper; fails loud (ward#288, docs/agent-harnesses.md).
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
// the primary working clone at /workspace/<repo> and return its path.
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
	work := primaryWorkspaceDir(workspaceRoot, targetRepo{Owner: e.TargetOwner, Name: e.TargetName})
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
// at /workspace/<owner>/<repo>; best-effort per repo. See docs/container-substrate.md.
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

// cloneContextRepos clones each read-only catalog-dependency context repo at
// /workspace/<owner>/<repo> as reference (ward#573). Best-effort per repo; never fatal.
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

// cloneExtraRepo mirrors or seeds one granted repo at /workspace/<owner>/<repo>.
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
				blog("extra-repo: cached mirror fresh %s/%s (read-only TTL %s)", repo.Owner, repo.Name, ttl)
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
	work := grantedRepoWorkspaceDir(workspaceRoot, repo)
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
// contacts the remote with the clear named wall (ward#299, agent-director.md).
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
		url:    substrateRepoCloneURL(e, owner, name),
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

// substrateRepoCloneURL applies typed repo authority: the bundled example lives
// on GitHub while Forgejo-owned entries retain the launch's configured base.
func substrateRepoCloneURL(e bootstrapEnv, owner, name string) string {
	repo := targetRepo{Owner: owner, Name: name}
	checkout := defaultSmartDefaults().authorityForRepo(owner, name).Checkout
	base := checkout.baseURL()
	if checkout == forgeForgejo && e.ForgejoBase != "" {
		base = e.ForgejoBase
	}
	return repo.cloneURL(base)
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

// interactiveIntroductionBlock prompts attached agents to identify their runtime
// lane in the first interactive reply without adding any identity plumbing.
const interactiveIntroductionBlock = `

---

## Interactive startup

When this is an attached interactive session, begin your first response with a
brief introduction: name yourself as the selected Ward role, name the harness you
are running through, and name the repo or scope you are attached to. Use the
startup context and ` + "`WARD_ROLE`" + ` / ` + "`WARD_AGENT`" + ` / ` + "`WARD_TARGET_REPO`" + ` env if you need exact values.
Keep it to one sentence, then continue with the user's request.
`

func interactiveIntroductionContext(e bootstrapEnv) []byte {
	if e.oneshot() {
		return nil
	}
	return []byte(interactiveIntroductionBlock)
}

func agentIdentityContext(e bootstrapEnv) []byte {
	identity := agentIdentityFromEnv(e.Mode, e.AgentDisplayName, e.AgentPronouns)
	if strings.TrimSpace(identity.Name) == "" {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n---\n\n## Ward agent identity\n\n")
	fmt.Fprintf(&b, "Sign Ward-authored issue bodies and commit trailers as %s.\n", identity.Label())
	fmt.Fprintf(&b, "Keep Git author and committer attribution on the fleet account configured for this run.\n")
	return []byte(b.String())
}

// readOnlyContextBlock is a read-only session's static "do not push" entry context
// (ward#293). Kept in sync with the same block in entrypoint.sh's compose_context.
const readOnlyContextBlock = `

---

## Read-only session (this overrides the autonomy doctrine above)

This is the **director's attached read-only surface session** (` + "`warded director`" + ` opened it
after rendering one live queue snapshot). Here "read-only" means one thing: **this clone
cannot push to its own remote**, so nothing leaves this clone. It does
not mean you are sealed off. The natural product of a surface session is commissioned work,
and that still ships. The read-only surface may also dispatch sibling engineers and
advisors when the work should outlive the session.

Capture-and-dispatch is an **obligation, not a "may"**. Every work item you surface -
a bug, a missing test, a follow-up, anything worth doing - you **must**:

- **File an issue** for it (` + "`ward agent issue create <owner/repo> --title ... --body-file ...`" + `), then
- **Dispatch a sibling headless run** to do the actual fix - ` + "`warded <owner/repo>#N`" + `
  spins up its own sealed container with its own credential and lifecycle, does its
  own implement -> commit -> merge -> push there, and never touches this clone.
  That dispatch inherits the surface's own harness by default, so a Codex director
  sends Codex engineers unless you explicitly override the engineer driver.

Do not let a work item die in the conversation. If you named it, capture it and
dispatch it before you move on.

**Capture-and-dispatch, then return to the harness-native goal loop.** Ward has no
autonomous loop and does not poll, choose, or redispatch work. Your job in this seat is
to read, scope, file, and fire, then continue the harness-native ` + "`/goal`" + ` loop. Do not
babysit one worker. The goal decides when to refresh live queue state and which brokered
action comes next.

**Prefer a sibling dispatch over an in-session subagent.** When the work is
delegable - a design proposal, a research dig, an implementation - reach for a sibling
warded run (` + "`warded director owner/repo#N`" + ` to scope follow-up work, ` + "`warded engineer #N`" + ` to build)
before an in-session subagent. The sibling lands a durable, attributable artifact on
the canonical surface (the issue thread, a pushed commit) that outlives this session,
and the next run can read it. A subagent's output dies in this conversation's
scrollback. Reserve an in-session subagent for read-only fan-out that only feeds
**your** immediate reasoning and never needs to outlive the session.

**How this is wired** (you do not set any of it up - it is ready):

- Forgejo access is brokered over ` + "`$WARD_BROKER_SOCK`" + `. The root bootstrap holds and
  refreshes the bot credential; this dropped agent does **not** receive ` + "`FORGEJO_TOKEN`" + `.
  Use Ward's native brokered issue, PR, and dispatch commands; never attempt to retrieve,
  print, or inject a token. If the broker reports an unrecoverable credential refresh, stop and
  surface that failure. Ward will not recycle the surface or retry autonomously.
- **PR-workflow management is native Ward** (ward#1067): ` + "`ward agent pr status <owner/repo#N>`" + `
  reads one PR head's combined CI status, ` + "`ward agent pr close <owner/repo#N> --reason TEXT`" + ` closes
  an eligible PR with explicit intent, ` + "`ward agent pr reopen <owner/repo#N>`" + ` reopens a
  closed-unmerged PR, ` + "`ward agent pr recover <owner/repo#N>`" + ` diagnoses the closed-unmerged
  state, ` + "`ward agent pr merge <owner/repo#N>`" + ` merges an eligible PR (head-pinned,
  checks-green-gated), ` + "`ward agent pr runs <owner/repo>`" + ` lists Actions runs with conclusions,
  and ` + "`ward agent pr rerun <owner/repo> <run-id>`" + ` reruns one.
  These forward through the supervised dispatch broker on Ward's compiled Forgejo client and are
  gated by fixed workflow rules.
- Fresh director surfaces mount the host Docker socket at ` + "`/var/run/docker.sock`" + `, so
  ` + "`ward agent reap`" + ` can list and stop stale engineer containers and a dispatched
  ` + "`warded #N`" + ` can spawn its sibling container. If this live surface does not have that
  mount yet, restart ` + "`warded`" + ` so the resolved config and bind set are picked up before the
  next launch. Until then, use the brokered cleanup command ` + "`ward agent stop <owner/repo#N>`" + `
  from this surface. If you hit a socket permission error, the group-grant did not reach this
  host's socket - see ward#319.

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
	rerr := r.reapTargetTree(ctx, work, env, true)
	unlanded := r.verifyExtraReposLanded(ctx, env)
	if rerr == nil && !unlanded {
		r.commentLaunchedNoOutcomeIfNeeded(ctx, env)
	}
	r.releaseReservationIfTerminalOutcome(ctx, env)
	if rerr != nil {
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before the container is removed")
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

// --- launch ------------------------------------------------------------------

// launchAgent runs the agent as its non-root user. It returns any harness failure
// so bootstrap cannot convert a CLI usage error into container success.
func (r *Runner) launchAgent(ctx context.Context, e bootstrapEnv, work string, argv []string, stream bool, seed []string) error {
	launch := append(setprivPrefix(e), argv...)
	blog("launch start: stream=%t oneshot=%t work=%s", stream, e.oneshot(), work)
	var runErr error
	switch {
	case stream:
		runErr = r.runStreaming(ctx, work, launch)
	case e.oneshot() && containerMode(e.Mode) == modeGoose:
		runErr = r.runGooseCompletionWatch(ctx, work, launch, strings.Join(seed, "\n"))
	case e.oneshot():
		runErr = r.runWithStdin(ctx, work, launch, os.DevNull)
	default:
		runErr = r.runWithStdin(ctx, work, launch, "")
	}
	if runErr != nil {
		blog(r.agentDeathLogLine(ctx, e.Container, runErr))
		return runErr
	}
	blog("bootstrap launch returned: agent process exited, deferred reaper runs next")
	return nil
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
// The prompt rides stdin because `goose run -t` treats trailing argv as options.
func (r *Runner) runGooseCompletionWatch(ctx context.Context, work string, launch []string, prompt string) error {
	cmd := exec.CommandContext(ctx, launch[0], launch[1:]...) // #nosec G204 -- fixed setpriv/agent argv
	cmd.Dir = work
	cmd.Stderr = r.launchStderr()
	cmd.Stdin = strings.NewReader(prompt)
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
// init-groups, pin HOME, and append any context tools after the image PATH.
func setprivPrefix(e bootstrapEnv) []string {
	prefix := []string{
		"setpriv", "--reuid=" + e.AgentUID, "--regid=" + e.AgentGID, "--init-groups",
		"env",
	}
	if e.ReadOnly {
		// Keep the root bootstrap/reaper environment intact, but never inherit the
		// raw bot token into the dropped director process.
		prefix = append(prefix, "-u", "FORGEJO_TOKEN")
	}
	prefix = append(prefix, "HOME="+e.AgentHome)
	if e.ContextTools != "" {
		path := os.Getenv("PATH")
		if path == "" {
			path = e.ContextTools
		} else {
			path += string(os.PathListSeparator) + e.ContextTools
		}
		prefix = append(prefix, "PATH="+path)
	}
	return prefix
}

// chownAgentTree ports the launch-time chown: hand the work tree + agent config
// dirs to the non-root agent user. Best-effort, like the bash `|| true`.
func (r *Runner) chownAgentTree(ctx context.Context, e bootstrapEnv, work string) {
	paths := agentOwnershipPaths(e, work)
	_ = r.Runner.Exec(ctx, "chown", append([]string{"-R", e.AgentUID + ":" + e.AgentGID}, paths...)...)
}

func agentOwnershipPaths(e bootstrapEnv, work string) []string {
	paths := make([]string, 0, 4)
	if _, err := os.Lstat(work); err == nil {
		paths = append(paths, work)
	}
	projection := lookupAgent(containerMode(e.Mode)).Record().Projection
	for _, rel := range projection.OwnershipPaths {
		path := filepath.Join(e.AgentHome, filepath.FromSlash(rel))
		if _, err := os.Lstat(path); err == nil {
			paths = append(paths, path)
		}
	}
	// Hand each granted extra-repo tree to the agent user too (ward#230); they
	// were cloned as root, like the target. Skip any that failed to clone.
	for _, repo := range e.ExtraRepos {
		if dest := grantedRepoWorkspaceDir(workspaceRoot, repo); isDir(dest) {
			paths = append(paths, dest)
		}
	}
	// Read-only context repos are cloned as root too (ward#573); hand them over
	// so the agent can read them, even though it may never write.
	for _, repo := range e.ContextRepos {
		if dest := grantedRepoWorkspaceDir(workspaceRoot, repo.targetRepo); isDir(dest) {
			paths = append(paths, dest)
		}
	}
	return paths
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
