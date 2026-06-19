package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
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
	Agent        string
	ContextLevel string
	GitCache     string
	ContextSrc   string
	QwenModel    string
	OllamaURL    string
	GitUserName  string
	GitUserEmail string
	AgentUID     string
	AgentGID     string
	AgentHome    string
	MirrorName   string
	Branch       string
	Headless     bool
	Ask          bool
	ForgejoHost  string
	// Substrate config (best-effort reference-repo warming).
	SubstrateSeed     string
	SubstrateDest     string
	SubstrateManifest string
	SubstrateTTL      string
	SubstrateSkip     bool
}

// envOr returns the env var or a default, mirroring bash `${X:-default}`.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// readBootstrapEnv reads + defaults the entrypoint env, erroring on a missing
// required var (the bash `: "${X:?...}"` checks). Pure given the environment.
func readBootstrapEnv() (bootstrapEnv, error) {
	e := bootstrapEnv{
		TargetOwner:  os.Getenv("WARD_TARGET_OWNER"),
		TargetName:   os.Getenv("WARD_TARGET_NAME"),
		ForgejoBase:  os.Getenv("WARD_FORGEJO_BASE"),
		Mode:         envOr("WARD_MODE", "claude"),
		Agent:        envOr("WARD_AGENT", "claude"),
		ContextLevel: envOr("WARD_CONTEXT_LEVEL", "2"),
		GitCache:     envOr("WARD_GITCACHE", "/gitcache"),
		ContextSrc:   envOr("WARD_CONTEXT_SRC", "/opt/ward-context"),
		QwenModel:    envOr("WARD_QWEN_MODEL", "qwen2.5-coder:latest"),
		OllamaURL:    envOr("WARD_OLLAMA_URL", "http://localhost:11434/v1"),
		GitUserName:  envOr("WARD_GIT_NAME", "ward-container"),
		GitUserEmail: envOr("WARD_GIT_EMAIL", "coilysiren@gmail.com"),
		AgentUID:     envOr("WARD_AGENT_UID", "1000"),
		AgentGID:     envOr("WARD_AGENT_GID", "1000"),
		AgentHome:    envOr("WARD_AGENT_HOME", "/home/ubuntu"),
		MirrorName:   os.Getenv("WARD_MIRROR_NAME"),
		Branch:       os.Getenv("WARD_BRANCH"),
		Headless:     os.Getenv("WARD_HEADLESS") == "1",
		Ask:          os.Getenv("WARD_ASK") == "1",

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
	return e, nil
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

	// The container is the isolation boundary; opt the reaper out of ward's jail
	// (cli-guard#153). Stamp container start for the reaper's PAT-age report (ward#103).
	_ = os.Setenv("CLIGUARD_NO_SANDBOX", "1")
	if os.Getenv("WARD_CONTAINER_UP") == "" {
		_ = os.Setenv("WARD_CONTAINER_UP", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	}

	r.configureGitAuth(ctx, e)
	r.installOpencode(ctx, e)
	work, cerr := r.cloneTarget(ctx, e)
	if cerr != nil {
		return cerr
	}
	r.installPreCommitHooks(ctx, e, work)
	r.installAgentPreCommitHooks(ctx, e, work)
	r.warmSubstrate(ctx, e)
	r.composeContext(e)
	r.composePermissions(e)
	r.writeClaudeCreds(e)
	r.writeCodexCreds(e)
	r.composeCodexConfig(e)
	r.composeOpencodeConfig(e)
	r.composeGooseConfig(e)

	_ = os.Setenv("WARD_REAP_WORK", work)
	defer r.reap(ctx, work)

	branch := r.captureTrim(ctx, "git", "-C", work, "branch", "--show-current")
	blog("ready: %s/%s on %s [mode=%s]", e.TargetOwner, e.TargetName, branch, e.Mode)

	if !commandExists(e.Agent) {
		blog("agent '%s' is not in this image yet (codex/qwen/goose install is a follow-up); dropping to a shell (reaper runs on exit)", e.Agent)
		_ = r.Runner.Exec(ctx, "bash")
		return nil
	}

	argv, stream := buildAgentArgv(e, agentArgs)
	logAgentArgv(e, agentArgs)

	// Drop to the non-root agent user (claude refuses bypass-perms as root, ward#127);
	// setup ran as root. Keep ANTHROPIC_API_KEY from shadowing the OAuth creds.
	r.chownAgentTree(ctx, e, work)
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_AUTH_TOKEN")

	// Fail loud before launch if claude can't authenticate (ward#222): a clear
	// abort beats a silent multi-minute hang. Runs as the agent user, post-chown.
	if serr := r.smokeTestClaudeAuth(ctx, e); serr != nil {
		blog("fatal: %v", serr)
		return serr
	}

	blog("launching %s as uid %s", e.Agent, e.AgentUID)
	return r.launchAgent(ctx, e, work, argv, stream)
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
	cred := fmt.Sprintf("https://%s:%s@%s\n", "coilysiren", token, e.ForgejoHost)
	if werr := os.WriteFile("/etc/ward-git-credentials", []byte(cred), 0o640); werr != nil {
		blog("could not write git credentials: %v", werr)
		return
	}
	// Readable by root (reaper) and the dropped agent group, not world.
	if gid, gerr := strconv.Atoi(e.AgentGID); gerr == nil {
		_ = os.Chown("/etc/ward-git-credentials", 0, gid)
	}
	_ = os.Chmod("/etc/ward-git-credentials", 0o640)
}

// --- install opencode (qwen mode): best-effort, never fatal ------------------

// installOpencode ports install_opencode: self-install opencode onto PATH for
// qwen mode (absent from the image). Best-effort; never fatal.
func (r *Runner) installOpencode(ctx context.Context, e bootstrapEnv) {
	if e.Mode != "qwen" {
		return
	}
	if commandExists("opencode") {
		blog("opencode already present in image; skipping install")
		return
	}
	blog("installing opencode (qwen-backed harness; not baked into the dev-base image yet)")
	// The bash pipes `curl ... | bash`; reproduce via `bash -c` so the installer's
	// own redirects to stderr are preserved (its stdout is the script, not output).
	_ = r.Runner.Exec(ctx, "bash", "-c", "curl -fsSL https://opencode.ai/install | bash >&2")
	src := filepath.Join(os.Getenv("HOME"), ".opencode", "bin", "opencode")
	if isExecutable(src) {
		_ = r.Runner.Exec(ctx, "install", "-m", "0755", src, "/usr/local/bin/opencode")
	}
	if !commandExists("opencode") {
		blog("opencode install failed; qwen mode will drop to a shell (use --image with opencode baked in, or fix network)")
	}
}

// --- cached fresh clone (mirror in the shared gitcache volume) ---------------

// cloneTarget ports clone_target: refresh-or-create the bare mirror, then drop
// a working clone under /workspace and return its path.
func (r *Runner) cloneTarget(ctx context.Context, e bootstrapEnv) (string, error) {
	mirror := filepath.Join(e.GitCache, e.MirrorName)
	url := e.ForgejoBase + "/" + e.TargetOwner + "/" + e.TargetName + ".git"
	_ = os.MkdirAll(e.GitCache, 0o755)
	if isDir(mirror) {
		blog("refreshing cached mirror %s", mirror)
		if uerr := r.Runner.Exec(ctx, "git", "-C", mirror, "remote", "update", "--prune"); uerr != nil {
			blog("mirror refresh failed, using cached state")
		}
	} else {
		blog("cloning mirror (first time) %s", url)
		if cerr := r.Runner.Exec(ctx, "git", "clone", "--mirror", url, mirror); cerr != nil {
			return "", fmt.Errorf("ward container bootstrap: mirror clone failed: %w", cerr)
		}
	}
	work := "/workspace/" + e.TargetName
	_ = os.RemoveAll(work)
	if cerr := r.Runner.Exec(ctx, "git", "clone", mirror, work); cerr != nil {
		return "", fmt.Errorf("ward container bootstrap: working clone failed: %w", cerr)
	}
	_ = r.Runner.Exec(ctx, "git", "-C", work, "remote", "set-url", "origin", url)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "config", "push.default", "current")
	if e.Branch != "" {
		_ = r.Runner.Exec(ctx, "git", "-C", work, "checkout", "-B", e.Branch)
	}
	return work, nil
}

// --- pre-commit parity (ward#133) --------------------------------------------

// installPreCommitHooks ports install_precommit_hooks: register the repo's
// pre-commit + commit-msg hooks so agent commits hit the same gate a human's do.
func (r *Runner) installPreCommitHooks(ctx context.Context, e bootstrapEnv, work string) {
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

// --- agent-only commit suite (ward#139): headless runs only ------------------

// installAgentPreCommitHooks ports install_agent_precommit_hooks: in headless
// runs, generate + install the agentic-os agent-only commit-msg suite.
func (r *Runner) installAgentPreCommitHooks(ctx context.Context, e bootstrapEnv, work string) {
	if !e.Headless {
		blog("not headless; skipping agent-only commit suite (ward#139)")
		return
	}
	if !isFile(filepath.Join(work, ".pre-commit-config.yaml")) {
		blog("no .pre-commit-config.yaml; skipping agent commit suite (ward#139)")
		return
	}
	if !commandExists("pre-commit") {
		blog("pre-commit not on PATH; skipping agent commit suite (ward#139)")
		return
	}
	// Generate the agent-only config in-process (the bash shelled out to `ward
	// container agent-precommit-config`), then bind it as the commit-msg hook.
	cfgRel := ".git/ward-agent-precommit.yaml"
	cfgAbs := filepath.Join(work, cfgRel)
	repoCfg, rerr := os.ReadFile(filepath.Join(work, ".pre-commit-config.yaml")) // #nosec G304 -- repo config path
	if rerr != nil {
		blog("no agentic-os hooks to enable; skipping agent commit suite (ward#139)")
		return
	}
	out, gerr := agentPreCommitConfig(repoCfg)
	if gerr != nil {
		_ = os.Remove(cfgAbs)
		blog("no agentic-os hooks to enable; skipping agent commit suite (ward#139)")
		return
	}
	if werr := os.WriteFile(cfgAbs, out, 0o644); werr != nil { // #nosec G306 -- repo-relative config, not a secret
		blog("agent commit suite install failed (ward#139)")
		return
	}
	if ierr := r.execIn(ctx, work, "pre-commit", "install", "--hook-type", "commit-msg", "--config", cfgRel); ierr == nil {
		blog("installed agent-only commit-msg suite via %s (ward#139)", cfgRel)
	} else {
		blog("agent commit suite install failed (ward#139)")
	}
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

// composeContext ports compose_context: write AGENTS.container.md as the base
// CLAUDE.md, appending host context per the level ladder; mirror to goose hints.
func (r *Runner) composeContext(e bootstrapEnv) {
	out := filepath.Join(e.AgentHome, ".claude", "CLAUDE.md")
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
	_ = os.WriteFile(out, buf, 0o644) // #nosec G306 -- operating context, not a secret
	blog("composed context (level %s) at %s", e.ContextLevel, out)
	if e.Mode == "goose" {
		ghints := filepath.Join(e.AgentHome, ".config", "goose", ".goosehints")
		_ = os.MkdirAll(filepath.Dir(ghints), 0o755)
		if werr := os.WriteFile(ghints, buf, 0o644); werr == nil { // #nosec G306 -- goose hints
			blog("mirrored composed context into %s (goose hints)", ghints)
		}
	}
}

// --- container permission policy ---------------------------------------------

// composePermissions ports compose_permissions: copy the container permission
// policy into the agent's claude settings.json.
func (r *Runner) composePermissions(e bootstrapEnv) {
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

// --- claude credentials (Max OAuth; host-resolved) ---------------------------

// writeClaudeCreds ports write_claude_creds: decode the host-injected base64
// OAuth blob into the file claude reads.
func (r *Runner) writeClaudeCreds(e bootstrapEnv) {
	b64 := os.Getenv("WARD_CLAUDE_CREDS_B64")
	if b64 == "" {
		blog("no claude credentials injected; claude will be unauthenticated")
		return
	}
	dir := filepath.Join(e.AgentHome, ".claude")
	_ = os.MkdirAll(dir, 0o755)
	dec, derr := base64.StdEncoding.DecodeString(b64)
	if derr != nil {
		blog("could not decode claude credentials: %v", derr)
		return
	}
	out := filepath.Join(dir, ".credentials.json")
	if werr := os.WriteFile(out, dec, 0o600); werr != nil {
		blog("could not write claude credentials: %v", werr)
		return
	}
	blog("wrote claude credentials to %s", out)
}

// --- codex credentials (ward#178) --------------------------------------------

// writeCodexCreds ports write_codex_creds: decode the host-injected base64
// auth.json into the file codex reads (codex mode only).
func (r *Runner) writeCodexCreds(e bootstrapEnv) {
	if e.Mode != "codex" {
		return
	}
	b64 := os.Getenv("WARD_CODEX_AUTH_B64")
	if b64 == "" {
		blog("no codex credentials injected; codex will be unauthenticated (run 'codex login' on the host to seed ~/.codex/auth.json)")
		return
	}
	dir := filepath.Join(e.AgentHome, ".codex")
	_ = os.MkdirAll(dir, 0o755)
	dec, derr := base64.StdEncoding.DecodeString(b64)
	if derr != nil {
		blog("could not decode codex credentials: %v", derr)
		return
	}
	out := filepath.Join(dir, "auth.json")
	if werr := os.WriteFile(out, dec, 0o600); werr != nil {
		blog("could not write codex credentials: %v", werr)
		return
	}
	blog("wrote codex credentials to %s", out)
}

// --- codex config (ward#178): approvals-off / sandbox-open -------------------

// composeCodexConfig ports compose_codex_config: write the approvals-off,
// sandbox-open codex config (the container is the boundary). codex mode only.
func (r *Runner) composeCodexConfig(e bootstrapEnv) {
	if e.Mode != "codex" {
		return
	}
	dir := filepath.Join(e.AgentHome, ".codex")
	_ = os.MkdirAll(dir, 0o755)
	body := "# Written by the ward container entrypoint (ward#178): container is the boundary.\n" +
		"approval_policy = \"never\"\n" +
		"sandbox_mode = \"danger-full-access\"\n"
	out := filepath.Join(dir, "config.toml")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		blog("could not write codex config: %v", werr)
		return
	}
	blog("wrote codex config (approvals off, sandbox open) to %s", out)
}

// --- opencode config (qwen mode) ---------------------------------------------

// composeOpencodeConfig ports compose_opencode_config: point opencode at the
// local ollama qwen model. qwen mode only.
func (r *Runner) composeOpencodeConfig(e bootstrapEnv) {
	if e.Mode != "qwen" {
		return
	}
	dir := filepath.Join(e.AgentHome, ".config", "opencode")
	_ = os.MkdirAll(dir, 0o755)
	body := opencodeConfigJSON(e.QwenModel, e.OllamaURL)
	out := filepath.Join(dir, "opencode.json")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		blog("could not write opencode config: %v", werr)
		return
	}
	blog("wrote qwen-backed opencode config (model ollama/%s via %s) to %s", e.QwenModel, e.OllamaURL, out)
}

// opencodeConfigJSON renders the qwen-backed opencode config, matching the bash
// heredoc byte-for-byte (the $schema key is literal, not interpolated).
func opencodeConfigJSON(model, ollamaURL string) string {
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "model": "ollama/%s",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (local)",
      "options": { "baseURL": "%s" },
      "models": { "%s": {} }
    }
  }
}
`, model, ollamaURL, model)
}

// --- goose config (ward#186) -------------------------------------------------

// composeGooseConfig ports compose_goose_config: seed goose's config.yaml with
// provider + model (+ tower Ollama host if resolved). goose mode only.
func (r *Runner) composeGooseConfig(e bootstrapEnv) {
	if e.Mode != "goose" {
		return
	}
	dir := filepath.Join(e.AgentHome, ".config", "goose")
	_ = os.MkdirAll(dir, 0o755)
	provider := envOr("WARD_GOOSE_PROVIDER", "ollama")
	model := envOr("WARD_GOOSE_MODEL", "qwen2.5")
	host := ""
	if b64 := os.Getenv("WARD_GOOSE_OLLAMA_HOST_B64"); b64 != "" {
		if dec, derr := base64.StdEncoding.DecodeString(b64); derr == nil {
			host = string(dec)
		}
	}
	body := gooseConfigYAML(provider, model, host)
	out := filepath.Join(dir, "config.yaml")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		blog("could not write goose config: %v", werr)
		return
	}
	if provider == "ollama" && host == "" {
		blog("wrote goose config (provider=%s model=%s) to %s; no tower Ollama host resolved, goose will use its built-in default", provider, model, out)
	} else {
		blog("wrote goose config (provider=%s model=%s) to %s", provider, model, out)
	}
}

// gooseConfigYAML renders goose's config.yaml, matching the bash heredoc lines.
func gooseConfigYAML(provider, model, host string) string {
	var b strings.Builder
	b.WriteString("# Written by the ward container entrypoint (ward#186): bind goose's provider.\n")
	fmt.Fprintf(&b, "GOOSE_PROVIDER: %s\n", provider)
	fmt.Fprintf(&b, "GOOSE_MODEL: %s\n", model)
	if host != "" {
		fmt.Fprintf(&b, "OLLAMA_HOST: %s\n", host)
	}
	return b.String()
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
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before 'ward container down'")
		return
	}
	if !isGitWorkTree(ctx, r, work) {
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before 'ward container down'")
		return
	}
	if rerr := r.reapWorkTree(ctx, work, env); rerr != nil {
		blog("reaper returned non-zero; check this log for an UNPRESERVED PATCH block before 'ward container down'")
	}
}

// reapWorkTree runs the same capture->integrate->land/salvage flow as
// runContainerReap, against an already-validated work tree.
func (r *Runner) reapWorkTree(ctx context.Context, work string, env reapEnv) error {
	statusSnapshot := r.captureAndCommitResidual(ctx, work, env)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin")
	if !refExists(ctx, r, work, "origin/main") {
		return r.salvage(ctx, work, env, reasonPushFail, false, nil, statusSnapshot)
	}
	residual := revCount(ctx, r, work, "origin/main..HEAD")
	if residual == 0 && strings.TrimSpace(statusSnapshot) == "" {
		fmt.Fprintln(os.Stderr, "ward container reap: nothing to reap (tree clean, HEAD on origin/main)")
		return nil
	}
	findings := scanDiff(r.diffEntries(ctx, work, "origin/main...HEAD"))
	action := decideReap(reapInputs{
		HasResidualWork:  residual > 0,
		IntegrationClean: r.integrate(ctx, work, residual),
		Findings:         findings,
	})
	return r.executeReap(ctx, work, env, action, findings, statusSnapshot)
}

// --- pre-launch auth smoke test (ward#222) -----------------------------------

// smokeTestClaudeAuth ports smoke_test_claude_auth: a bounded claude probe as
// the agent user; abort loudly on can't-auth (no silent hang). claude headless only.
func (r *Runner) smokeTestClaudeAuth(ctx context.Context, e bootstrapEnv) error {
	if !(e.Agent == "claude" && e.Headless) {
		return nil
	}
	if os.Getenv("WARD_SMOKE_TEST_SKIP") == "1" {
		blog("auth smoke test skipped (WARD_SMOKE_TEST_SKIP=1)")
		return nil
	}
	if !commandExists("claude") {
		return nil
	}
	blog("auth smoke test: probing claude before launch (ward#222)")
	probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	argv := append(setprivPrefix(e), "claude", "-p", "--output-format", "json", "Reply with the single word: ok")
	out, rc := r.captureProbe(probeCtx, argv)
	if probeCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("auth smoke test: claude -p did not respond within 90s - credentials are unusable in-container (ward#222). Refresh the host claude login (re-run 'claude' on the host) and relaunch; WARD_SMOKE_TEST_SKIP=1 bypasses")
	}
	if rc != 0 || strings.TrimSpace(out) == "" {
		return fmt.Errorf("auth smoke test: claude -p produced no usable output (exit %d) - credentials are unusable in-container (ward#222). Refresh the host claude login and relaunch; WARD_SMOKE_TEST_SKIP=1 bypasses", rc)
	}
	blog("auth smoke test: claude responded, auth OK")
	return nil
}

// captureProbe runs the smoke-test argv with stdin pinned to /dev/null and
// stderr discarded, capturing stdout; returns stdout + exit code.
func (r *Runner) captureProbe(ctx context.Context, argv []string) (string, int) {
	devnull, _ := os.Open(os.DevNull)
	if devnull != nil {
		defer func() { _ = devnull.Close() }()
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- fixed setpriv/claude argv
	cmd.Stdin = devnull
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	rc := 0
	if err != nil {
		rc = 1
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		}
	}
	return string(out), rc
}

// --- launch ------------------------------------------------------------------

// launchAgent ports the tail of main(): drop to the agent user via setpriv and
// run the agent, piping headless stream-json through streamProgress.
func (r *Runner) launchAgent(ctx context.Context, e bootstrapEnv, work string, argv []string, stream bool) error {
	launch := append(setprivPrefix(e), argv...)
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
	return nil
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

// buildAgentArgv ports the per-mode argv builder from main(): returns the agent
// argv (without the setpriv prefix) and whether to stream-wrap its output. Pure.
func buildAgentArgv(e bootstrapEnv, seed []string) (argv []string, stream bool) {
	switch e.Mode {
	case "goose":
		if e.oneshot() {
			return append([]string{"goose", "run", "-t"}, seed...), false
		}
		return []string{"goose", "session"}, false
	case "codex":
		if e.oneshot() {
			return append([]string{"codex", "exec"}, seed...), false
		}
		return append([]string{"codex"}, seed...), false
	case "qwen":
		if e.oneshot() {
			return append([]string{"opencode", "run"}, seed...), false
		}
		return []string{"opencode"}, false
	default:
		argv = []string{e.Agent}
		switch {
		case e.Ask:
			argv = append(argv, "-p")
		case e.Headless:
			argv = append(argv, "-p", "--verbose", "--output-format", "stream-json")
			stream = true
		}
		argv = append(argv, seed...)
		return argv, stream
	}
}

// logAgentArgv emits the same per-mode launch notes main() logged alongside the
// argv build (kept separate from buildAgentArgv so that stays pure).
func logAgentArgv(e bootstrapEnv, seed []string) {
	switch e.Mode {
	case "goose":
		if e.oneshot() {
			blog("one-shot: goose run -t <prompt> (goose prints to this log)")
		} else if len(seed) > 0 {
			blog("interactive goose session: seed prompt is not auto-delivered (paste the issue)")
		}
	case "codex":
		if e.oneshot() {
			blog("one-shot: codex exec <prompt> (codex prints to this log)")
		}
	case "qwen":
		if e.oneshot() {
			blog("one-shot: opencode run <prompt> (opencode prints to this log)")
		} else if len(seed) > 0 {
			blog("interactive opencode TUI: seed prompt is not auto-delivered (paste the issue)")
		}
	default:
		switch {
		case e.Ask:
			blog("ask: %s -p <question> (one-shot answer to this terminal)", e.Agent)
		case e.Headless:
			blog("headless: streaming %s progress to this log", e.Agent)
		}
	}
}

// chownAgentTree ports the launch-time chown: hand the work tree + agent config
// dirs to the non-root agent user. Best-effort, like the bash `|| true`.
func (r *Runner) chownAgentTree(ctx context.Context, e bootstrapEnv, work string) {
	_ = r.Runner.Exec(ctx, "chown", "-R", e.AgentUID+":"+e.AgentGID,
		work,
		filepath.Join(e.AgentHome, ".claude"),
		filepath.Join(e.AgentHome, ".config"),
		filepath.Join(e.AgentHome, ".codex"),
	)
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
			fmt.Fprintln(w, out)
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
	var out []string
	switch ev.Type {
	case "assistant":
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
	case "user":
		for _, c := range ev.Message.Content {
			if c.Type == "tool_result" && c.IsError {
				out = append(out, "  ✗ (tool error)")
			}
		}
	case "result":
		subtype := ev.Subtype
		if subtype == "" {
			subtype = "?"
		}
		secs := int(ev.DurationMs / 1000)
		out = append(out, fmt.Sprintf("✓ result: %s (%d turns, %ds)", subtype, ev.NumTurns, secs))
		if ev.Result != "" {
			out = append(out, ev.Result)
		}
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
	if lerr := flockExclusive(f); lerr != nil {
		fn()
		return
	}
	defer func() { _ = flockUnlock(f) }()
	fn()
}

// commandExists reports whether bin is on PATH (the bash `command -v`).
func commandExists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// isExecutable reports whether path exists and has any execute bit (`[ -x ]`).
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
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
