package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	GitUserName    string
	GitUserEmail   string
	AgentUID       string
	AgentGID       string
	AgentHome      string
	MirrorName     string
	Branch         string
	Headless       bool
	Ask            bool
	// ReadOnly is the read-only surface session (WARD_READONLY, ward#293): revoke
	// the push credential, compose the restriction. See docs/agent-surface.md.
	ReadOnly    bool
	ForgejoHost string
	// ExtraRepos are the additional writable repos this run was granted via
	// --repo (WARD_EXTRA_REPOS); each is cloned full under /workspace (ward#230).
	ExtraRepos []targetRepo
	// Substrate config (best-effort reference-repo warming).
	SubstrateSeed     string
	SubstrateDest     string
	SubstrateManifest string
	SubstrateTTL      string
	SubstrateSkip     bool
}

const runProvenanceFile = ".ward-run-provenance.json"

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

// readBootstrapEnv reads + defaults the entrypoint env, erroring on a missing
// required var (the bash `: "${X:?...}"` checks). Pure given the environment.
func readBootstrapEnv() (bootstrapEnv, error) {
	// Defaults now source from the embedded fleet config (env > manifest, ward#416);
	// opencode is canonical (qwen is the alias), so it feeds the qwen/ollama defaults.
	fleet, ferr := loadFleetConfig()
	if ferr != nil {
		return bootstrapEnv{}, fmt.Errorf("load embedded fleet config for bootstrap defaults: %w", ferr)
	}
	opencode := fleetAgentByName(fleet, string(modeOpencode))
	codex := fleetAgentByName(fleet, string(modeCodex))
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
		QwenModel:    envOr("WARD_QWEN_MODEL", opencode.Model),
		OllamaURL:    envOr("WARD_OLLAMA_URL", opencode.Endpoint),
		// Cheapest codex settings (ward#379): mini model, low reasoning + verbosity,
		// the defaults now sourced from the fleet manifest's codex node.
		CodexModel:     envOr("WARD_CODEX_MODEL", codex.Model),
		CodexEffort:    envOr("WARD_CODEX_REASONING_EFFORT", codex.ReasoningEffort),
		CodexVerbosity: envOr("WARD_CODEX_VERBOSITY", codex.Verbosity),
		// Bot attribution: email is the load-bearing Forgejo match (ward#245); both
		// default from the fleet manifest's defaults.attribution.
		GitUserName:  envOr("WARD_GIT_NAME", attribution.Name),
		GitUserEmail: envOr("WARD_GIT_EMAIL", attribution.Email),
		AgentUID:     envOr("WARD_AGENT_UID", "1000"),
		AgentGID:     envOr("WARD_AGENT_GID", "1000"),
		AgentHome:    envOr("WARD_AGENT_HOME", "/home/ubuntu"),
		MirrorName:   os.Getenv("WARD_MIRROR_NAME"),
		Branch:       os.Getenv("WARD_BRANCH"),
		Headless:     os.Getenv("WARD_HEADLESS") == "1",
		Ask:          os.Getenv("WARD_ASK") == "1",
		ReadOnly:     os.Getenv("WARD_READONLY") == "1",

		SubstrateSeed:     envOr("WARD_SUBSTRATE_SEED", "/opt/substrate-seed"),
		SubstrateDest:     envOr("WARD_SUBSTRATE_DEST", "/substrate"),
		SubstrateManifest: envOr("WARD_SUBSTRATE_MANIFEST", "/opt/ward/preclone-repos.txt"),
		SubstrateTTL:      envOr("WARD_SUBSTRATE_TTL", "600"),
		SubstrateSkip:     os.Getenv("WARD_SUBSTRATE_SKIP") == "1",
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
	e.ExtraRepos = parseExtraReposEnv(os.Getenv("WARD_EXTRA_REPOS"), e.TargetOwner, e.TargetName)
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
	fmt.Fprintf(os.Stderr, "ward-container: "+format+"\n", a...)
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

// runContainerBootstrap is the port of the bash `main()`, in the same order.
// agentArgs is the seed argv the entrypoint passes as `"$@"` to the agent.
func (r *Runner) runContainerBootstrap(ctx context.Context, c *cli.Command) error {
	e, err := readBootstrapEnv()
	if err != nil {
		blog("fatal: %v", err)
		return err
	}
	agentArgs := c.Args().Slice()
	blog("bootstrap start: container=%s mode=%s agent=%s issue=%d readOnly=%t headless=%t extraRepos=%d",
		e.Container, e.Mode, e.Agent, e.Issue, e.ReadOnly, e.Headless, len(e.ExtraRepos))

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

	r.configureGitAuth(ctx, e)
	// Installer: opencode self-installs before the clone (absent from the image).
	if inst, ok := agent.(agentsapi.Installer); ok {
		blog("bootstrap installer start: %s", mode)
		_ = inst.Install(rc)
		blog("bootstrap installer done: %s", mode)
	}
	work, cerr := r.cloneTarget(ctx, e)
	if cerr != nil {
		return cerr
	}
	blog("bootstrap clone done: %s/%s -> %s", e.TargetOwner, e.TargetName, work)
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
	blog("bootstrap substrate warm start")
	r.warmSubstrate(ctx, e)
	blog("bootstrap substrate warm done")
	blog("bootstrap context compose start")
	r.composeContext(e)
	blog("bootstrap permissions compose start")
	r.composePermissions(e)
	// Creds write + onboarding seed + config compose, each feature-tested per mode
	// (Phase 3, ward#418); composeAgentContainer holds the order.
	composeAgentContainer(agent, rc)
	blog("bootstrap agent container composition done")

	_ = os.Setenv("WARD_REAP_WORK", work)
	defer r.reap(ctx, work)

	branch := r.captureTrim(ctx, "git", "-C", work, "branch", "--show-current")
	blog("ready: %s/%s on %s [mode=%s]", e.TargetOwner, e.TargetName, branch, e.Mode)

	if !commandExists(e.Agent) {
		blog("agent '%s' binary is not present in this image (claude/codex/goose ship in dev-base, qwen self-installs opencode); dropping to a shell (reaper runs on exit)", e.Agent)
		_ = r.Runner.Exec(ctx, "bash")
		return nil
	}

	argv, stream := buildAgentArgv(e, agentArgs)
	logAgentArgv(e, agentArgs)

	// Drop to the non-root agent user (claude refuses bypass-perms as root, ward#127);
	// setup ran as root. Keep ANTHROPIC_API_KEY from shadowing the OAuth creds.
	r.chownAgentTree(ctx, e, work)
	if e.ReadOnly {
		// Explore: scope push off this clone but keep the dispatch token + socket so
		// it can commission sibling runs (ward#293, ward#315).
		r.revokePushCredential(ctx)
		r.grantDockerSocketAccess(ctx, e)
	} else if cerr := r.ensureGitCredReadable(e); cerr != nil {
		// Re-assert the credential perms git's `store` helper clobbered on the clones,
		// else the dropped agent falls back to the human token (ward#288).
		blog("fatal: %v", cerr)
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
			return serr
		}
		blog("bootstrap prelaunch check passed: %s", e.Agent)
	}

	blog("launching %s as uid %s", e.Agent, e.AgentUID)
	blog("bootstrap launch handoff: %s", e.Agent)
	r.launchAgent(ctx, e, work, argv, stream)
	return nil
}

// --- forgejo git auth (token rides --env-file, never argv) -------------------

// configureGitAuth ports configure_git_auth: --system git identity + a
// credential store helper readable by root (reaper) and the dropped agent group.
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
	_ = r.Runner.Exec(ctx, "git", "config", "--system", "credential.helper",
		"store --file=/etc/ward-git-credentials")
	// Push as the coilyco-ops bot: FORGEJO_TOKEN is the bot's (ward#245).
	cred := fmt.Sprintf("https://%s:%s@%s\n", "coilyco-ops", token, e.ForgejoHost)
	if werr := os.WriteFile("/etc/ward-git-credentials", []byte(cred), 0o640); werr != nil {
		blog("could not write git credentials: %v", werr)
		return
	}
	// Readable by root (reaper) and the dropped agent group, not world; git's store
	// helper clobbers this on clone, re-asserted before the drop (ward#288).
	if gid, gerr := strconv.Atoi(e.AgentGID); gerr == nil {
		_ = os.Chown("/etc/ward-git-credentials", 0, gid)
	}
	_ = os.Chmod("/etc/ward-git-credentials", 0o640)
}

// revokePushCredential scopes the revoke to push-to-this-clone: it drops the git push
// wiring but keeps FORGEJO_TOKEN for dispatch (ward#315). See agent-surface.md.
func (r *Runner) revokePushCredential(ctx context.Context) {
	_ = os.Remove("/etc/ward-git-credentials")
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
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		blog("explore: could not read docker socket gid; dispatch may fail (ward#315)")
		return
	}
	sockgid := int(st.Gid)
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

// ensureGitCredReadable re-asserts the credential perms git's `store` helper
// clobbers on the root-phase clones; fails loud (ward#288, docs/agent-credentials.md).
func (r *Runner) ensureGitCredReadable(e bootstrapEnv) error {
	const f = "/etc/ward-git-credentials"
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
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Gid) != gid {
		return fmt.Errorf("ward#288: %s is group-owned by gid %d, not the agent gid %d; agent cannot read the bot credential", f, st.Gid, gid)
	}
	return nil
}

// --- cached fresh clone (mirror in the shared gitcache volume) ---------------

// cloneTarget ports clone_target: refresh-or-create the bare mirror, then drop
// a working clone under /workspace and return its path.
func (r *Runner) cloneTarget(ctx context.Context, e bootstrapEnv) (string, error) {
	mirror := filepath.Join(e.GitCache, e.MirrorName)
	url := e.ForgejoBase + "/" + e.TargetOwner + "/" + e.TargetName + ".git"
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
	work := "/workspace/" + e.TargetName
	_ = os.RemoveAll(work)
	blog("clone start: working clone %s -> %s", mirror, work)
	if cerr := r.Runner.Exec(ctx, "git", "clone", mirror, work); cerr != nil {
		return "", fmt.Errorf("ward container bootstrap: working clone failed: %w", cerr)
	}
	_ = r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "origin", url)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "config", "push.default", "current")
	if e.Branch != "" {
		_ = r.Runner.Exec(ctx, "git", "-C", work, "checkout", "-B", e.Branch)
	}
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
	baseline := r.captureTrim(ctx, "git", "-C", work, "rev-parse", "origin/main")
	if baseline == "" {
		return fmt.Errorf("ward container bootstrap: capture origin/main baseline")
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
		r.cloneExtraRepo(ctx, e, repo)
		blog("extra-repo clone done: %s/%s", repo.Owner, repo.Name)
	}
}

// cloneExtraRepo mirrors+working-clones one granted repo under /workspace/<name>
// with the target's push posture + pre-commit gate; flock-guarded, never fatal.
func (r *Runner) cloneExtraRepo(ctx context.Context, e bootstrapEnv, repo targetRepo) {
	mirror := filepath.Join(e.GitCache, repo.Owner+"__"+repo.Name+".git")
	url := e.ForgejoBase + "/" + repo.Owner + "/" + repo.Name + ".git"
	lock := filepath.Join(e.GitCache, "."+repo.Owner+"__"+repo.Name+".lock")
	r.withFlock(lock, func() {
		if isDir(mirror) {
			blog("extra-repo: refreshing cached mirror %s/%s", repo.Owner, repo.Name)
			if uerr := r.Runner.Exec(ctx, "git", "-C", mirror, "remote", "update", "--prune"); uerr != nil {
				blog("extra-repo: mirror refresh failed %s/%s (using cached state)", repo.Owner, repo.Name)
			}
		} else {
			blog("extra-repo: cloning mirror (first time) %s/%s", repo.Owner, repo.Name)
			if cerr := r.Runner.Exec(ctx, "git", "clone", "--mirror", url, mirror); cerr != nil {
				blog("extra-repo: mirror clone failed %s/%s (skipping)", repo.Owner, repo.Name)
				_ = os.RemoveAll(mirror)
			}
		}
	})
	if !isDir(mirror) {
		return
	}
	work := "/workspace/" + repo.Name
	_ = os.RemoveAll(work)
	if cerr := r.Runner.Exec(ctx, "git", "clone", mirror, work); cerr != nil {
		blog("extra-repo: working clone failed %s/%s", repo.Owner, repo.Name)
		return
	}
	_ = r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "origin", url)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "config", "push.default", "current")
	if e.Branch != "" {
		_ = r.Runner.Exec(ctx, "git", "-C", work, "checkout", "-B", e.Branch)
	}
	r.installPreCommitHooks(ctx, e, work)
	r.installReadOnlyPushGuard(ctx, e, work)
	blog("extra-repo: ready %s/%s at %s", repo.Owner, repo.Name, work)
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

// substrateMirrorStale ports substrate_mirror_stale: stale when FETCH_HEAD is
// older than the TTL. A missing FETCH_HEAD (just cloned/hydrated) is fresh.
func substrateMirrorStale(mirror string, ttlSeconds int64, now time.Time) bool {
	head := filepath.Join(mirror, "FETCH_HEAD")
	fi, err := os.Stat(head)
	if err != nil {
		return false
	}
	return int64(now.Sub(fi.ModTime()).Seconds()) >= ttlSeconds
}

// warmSubstrateRepo ports warm_substrate_repo: ensure one repo's bare mirror
// exists+fresh under a flock, drop a working copy, never fatal.
func (r *Runner) warmSubstrateRepo(ctx context.Context, e bootstrapEnv, owner, name, tier string) {
	mirror := filepath.Join(e.GitCache, owner+"__"+name+".git")
	seed := filepath.Join(e.SubstrateSeed, owner+"__"+name+".git")
	url := e.ForgejoBase + "/" + owner + "/" + name + ".git"
	lock := filepath.Join(e.GitCache, "."+owner+"__"+name+".lock")
	ttl, _ := strconv.ParseInt(e.SubstrateTTL, 10, 64)
	r.withFlock(lock, func() {
		if !isDir(mirror) {
			if tier == "image" && isDir(seed) {
				blog("substrate: hydrate %s/%s from baked seed", owner, name)
				_ = r.Runner.Exec(ctx, "cp", "-a", seed, mirror)
			} else {
				blog("substrate: clone mirror (first time) %s/%s", owner, name)
				if cerr := r.Runner.Exec(ctx, "git", "clone", "--mirror", url, mirror); cerr != nil {
					blog("substrate: mirror clone failed %s/%s (skipping)", owner, name)
					_ = os.RemoveAll(mirror)
					return
				}
			}
		}
		if substrateMirrorStale(mirror, ttl, time.Now()) {
			blog("substrate: refresh %s/%s (TTL %ss elapsed)", owner, name, e.SubstrateTTL)
			if uerr := r.Runner.Exec(ctx, "git", "-C", mirror, "remote", "update", "--prune"); uerr != nil {
				blog("substrate: refresh failed %s/%s (using cached state)", owner, name)
			}
		}
	})
	if isDir(mirror) {
		work := filepath.Join(e.SubstrateDest, name)
		_ = os.RemoveAll(work)
		if cerr := r.Runner.Exec(ctx, "git", "clone", "--quiet", mirror, work); cerr != nil {
			blog("substrate: working clone failed %s/%s", owner, name)
		} else {
			_ = r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "origin", url)
		}
	}
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
and that still ships.

Capture-and-dispatch is an **obligation, not a "may"**. Every work item you surface -
a bug, a missing test, a follow-up, anything worth doing - you **must**:

- **File an issue** for it (` + "`ward ops forgejo issue create ...`" + `), then
- **Dispatch a sibling headless run** to do the actual fix - ` + "`warded <owner/repo>#N`" + `
  spins up its own sealed container with its own credential and lifecycle, does its
  own implement -> commit -> merge -> push there, and never touches this clone.

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
Read the repo, reason about it, answer questions, scratch in the working tree if it
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
func (r *Runner) composeContext(e bootstrapEnv) {
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
	// A read-only session has no seed to carry the "do not push" constraint, so it
	// rides here as static entry context, overriding the autonomy doctrine (ward#293).
	if e.ReadOnly {
		buf = append(buf, []byte(readOnlyContextBlock)...)
	}
	_ = os.WriteFile(out, buf, 0o644) // #nosec G306 -- operating context, not a secret
	blog("composed context (level %s%s) at %s", e.ContextLevel, readOnlyTag(e.ReadOnly), out)
	r.linkOrCopyContext(filepath.Join("..", "AGENTS.md"), filepath.Join(e.AgentHome, ".claude", "CLAUDE.md"), out)
	blog("linked Claude context load point to %s", out)
	r.linkOrCopyContext(filepath.Join("..", "AGENTS.md"), filepath.Join(e.AgentHome, ".codex", "AGENTS.md"), out)
	blog("linked Codex context load point to %s", out)
	if e.Mode == string(modeGoose) {
		ghints := filepath.Join(e.AgentHome, gooseHintsRel)
		_ = os.MkdirAll(filepath.Dir(ghints), 0o755)
		if werr := os.WriteFile(ghints, buf, 0o644); werr == nil { // #nosec G306 -- goose hints
			blog("mirrored composed context into %s (goose hints)", ghints)
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
			blog("agent exited non-zero (%v); reaping anyway", rerr)
		}
	case e.oneshot():
		if rerr := r.runWithStdin(ctx, work, launch, os.DevNull); rerr != nil {
			blog("agent exited non-zero (%v); reaping anyway", rerr)
		}
	default:
		if rerr := r.runWithStdin(ctx, work, launch, ""); rerr != nil {
			blog("agent exited non-zero (%v); reaping anyway", rerr)
		}
	}
}

// runWithStdin runs launch in work with stdin from stdinPath (os.DevNull pins
// one-shot stdin to EOF; "" keeps the inherited stdin for interactive runs).
func (r *Runner) runWithStdin(ctx context.Context, work string, launch []string, stdinPath string) error {
	cmd := exec.CommandContext(ctx, launch[0], launch[1:]...) // #nosec G204 -- fixed setpriv/agent argv
	cmd.Dir = work
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
	cmd.Stderr = os.Stderr
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
	streamProgress(pipe, os.Stdout)
	return cmd.Wait()
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
		filepath.Join(e.AgentHome, ".config"),
		filepath.Join(e.AgentHome, ".codex"),
	}
	// Hand each granted extra-repo tree to the agent user too (ward#230); they
	// were cloned as root, like the target. Skip any that failed to clone.
	for _, repo := range e.ExtraRepos {
		if dest := "/workspace/" + repo.Name; isDir(dest) {
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
			_, _ = fmt.Fprintln(w, out)
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
