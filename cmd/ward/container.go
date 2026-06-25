package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"github.com/urfave/cli/v3"
)

// container.go wires the hidden `ward container` plumbing namespace (ward#263:
// reap/bootstrap) + docker side effects + host forgejo-token resolution.

//go:embed containerassets/entrypoint.sh containerassets/AGENTS.container.md
//go:embed containerassets/settings.container.json containerassets/preclone-repos.txt
var containerAssets embed.FS

// loadSubstrateManifest parses the embedded preclone manifest - the single
// source of truth for which reference repos every container warms.
func loadSubstrateManifest() ([]substrateRepo, error) {
	data, err := containerAssets.ReadFile("containerassets/" + containerSubstrateRel)
	if err != nil {
		return nil, err
	}
	return parseSubstrateManifest(string(data))
}

// forgejoTokenSSMPath is the SSM parameter NAME for the git-over-HTTPS push
// token (user coilysiren), resolved on the host and never entering the image.

// #nosec G101 -- this is an SSM parameter path, not an embedded secret.
const forgejoTokenSSMPath = "/forgejo/api-token"

// ollamaHostSSMPath is the SSM param for the tower Ollama endpoint goose binds;
// ward resolves it host-side (the container has no aws creds). docs/agent.md (ward#186).
const ollamaHostSSMPath = "/coilysiren/ollama/host"

// tsAuthKeySSMPath is the reusable + ephemeral tag:proxy tailscale auth key the
// sidecar joins with; the only SSM dep a --ts-sidecar carry has (ward#333, ward#337).
const tsAuthKeySSMPath = "/coilysiren/mac-proxy/ts-authkey"

// containerCommand is the Hidden `ward container` umbrella (ward#263): only the
// entrypoint-internal reap/bootstrap leaves remain. See docs/container.md.
func containerCommand() *cli.Command {
	return &cli.Command{
		Name:   "container",
		Hidden: true,
		Usage:  "Entrypoint-internal container plumbing (reap/bootstrap). Use `ward agent` to run a feature.",
		Description: `container is plumbing-only as of ward#263: the user-facing lifecycle verbs
(up/exec/down/ls) were retired in favour of ` + "`ward agent`" + `. The leaves that
remain here - reap and bootstrap - are invoked by the in-container entrypoint,
not by hand. See docs/agent.md for the contributor surface.`,
		Commands: []*cli.Command{
			containerReapCommand(),
			containerBootstrapCommand(),
			containerBrokerCommand(),
		},
	}
}

// agentCreds bundles the host-resolved per-mode credential blobs ward injects
// (base64'd) into the container env-file. See docs/agent.md (ward#178).
type agentCreds struct {
	Claude string
	Codex  string
	// GooseOllamaHost is the tower Ollama endpoint goose binds as its provider
	// (resolved host-side from SSM; the entrypoint seeds it into goose's config).
	GooseOllamaHost string
}

// resolveAgentCreds resolves the credential the run's mode needs (claude OAuth,
// codex auth.json, goose's tower Ollama endpoint; none for qwen's local ollama).
func (r *Runner) resolveAgentCreds(ctx context.Context, mode containerMode) agentCreds {
	switch mode {
	case modeClaude:
		return agentCreds{Claude: r.resolveClaudeCreds(ctx)}
	case modeCodex:
		return agentCreds{Codex: r.resolveCodexCreds()}
	case modeGoose:
		return agentCreds{GooseOllamaHost: r.resolveOllamaHost(ctx)}
	case modeQwen:
		// qwen runs against a local ollama; no host credential to inject.
		return agentCreds{}
	default:
		return agentCreds{}
	}
}

// buildUpPlan assembles the pure plan from parsed flags and resolved inputs;
// agentArgs seed the agent's argv. Errors only on a bad --repo grant (ward#230).
func buildUpPlan(c *cli.Command, repo targetRepo, mode containerMode, cwd, assetsDir string, agentArgs []string) (upPlan, error) {
	wardSrc := c.String("ward-source")
	// The container downloads this host's ward version by default; --ward-version
	// (env WARD_AGENT_VERSION) overrides it to pin a known-good release (ward#312).
	wardVersion := Version
	if v := strings.TrimSpace(c.String("ward-version")); v != "" {
		wardVersion = v
	}
	// Mutually-exclusive tailnet routes; either needs SSM, so either implies --aws.
	// --host-net joins the host's namespace (ward#330), --ts-sidecar a sidecar's (ward#333).
	hostNet := c.Bool("host-net")
	tsSidecar := c.Bool("ts-sidecar")
	if hostNet && tsSidecar {
		return upPlan{}, fmt.Errorf("--host-net and --ts-sidecar are mutually exclusive: --host-net inherits the host's tailnet route, --ts-sidecar runs a userspace SOCKS5 sidecar for Docker Desktop where the host VM is not a tailnet node (ward#333)")
	}
	awsHome := ""
	if c.Bool("aws") || hostNet || tsSidecar {
		awsHome = filepath.Join(homeDir(), ".aws")
	}
	// "with-repo" is the shared lookup key: the canonical name on drive/ask, the
	// alias of "--repo" on the agent surfaces (ward#280, docs/container-multi-repo.md).
	extra, err := parseExtraRepos(c.StringSlice("with-repo"), repo)
	if err != nil {
		return upPlan{}, err
	}
	return upPlan{
		Image:          imageRef(c.String("image"), c.String("tag")),
		Name:           containerName(repo, randHex()),
		Repo:           repo,
		Mode:           mode,
		Branch:         c.String("branch"),
		ForgejoBase:    forgejoBaseURL,
		HostCwd:        cwd,
		Mounts:         leastAccessMounts(cwd, mountOpts{AssetsDir: assetsDir, AWSHome: awsHome, WardSource: wardSrc}),
		Interactive:    !c.Bool("detach"),
		TTY:            !c.Bool("detach") && terminalAttached(),
		WardVersion:    wardVersion,
		WardFromSource: wardSrc != "",
		AgentArgs:      agentArgs,
		GoBootstrap:    c.Bool("go-bootstrap"),
		ExtraRepos:     extra,
		HostNet:        hostNet,
		TSSidecar:      tsSidecar,
	}, nil
}

// localHasTailscale0 reports whether a tailscale0 interface exists on this host's
// netns (the netns a --host-net carry joins on Linux); a probe error reads false.
func localHasTailscale0() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifi := range ifaces {
		if ifi.Name == "tailscale0" {
			return true
		}
	}
	return false
}

// maybeWarnHostNet prints the tailnet-unreachable warning when a --host-net plan
// won't inherit a tailnet route here (ward#332); a no-op unless plan.HostNet set.
func (r *Runner) maybeWarnHostNet(plan upPlan) {
	if !plan.HostNet {
		return
	}
	if msg, warn := hostNetTailnetWarning(runtime.GOOS, localHasTailscale0()); warn {
		w := r.Runner.Stderr
		if w == nil {
			w = os.Stderr
		}
		fmt.Fprintln(w, msg)
	}
}

// terminalAttached reports whether stdin and stdout are both terminals - the
// precondition docker needs before allocating a pseudo-TTY. See docs/container.md.
func terminalAttached() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// resolveTarget returns the target repo (explicit arg, else inferred from the
// cwd's git origin) and the cwd to mount for context.
func (r *Runner) resolveTarget(ctx context.Context, arg string) (targetRepo, string, error) {
	cwd := resolveInvokeCWD()
	if cwd == "" {
		return targetRepo{}, "", fmt.Errorf("ward container: cannot resolve the current directory")
	}
	if arg != "" {
		repo, err := parseRepoRef(arg)
		return repo, cwd, err
	}
	out, err := r.Runner.Capture(ctx, "git", "-C", cwd, "remote", "get-url", "origin")
	if err != nil {
		return targetRepo{}, "", fmt.Errorf("ward container: no repo ref given and cwd has no git origin to infer from: %w", err)
	}
	repo, err := targetFromRemoteURL(strings.TrimSpace(string(out)))
	return repo, cwd, err
}

// claudeKeychainService is the macOS login-keychain service holding the Max/Pro
// OAuth credential Claude Code reads; resolved on the host, never in the image.
const claudeKeychainService = "Claude Code-credentials"

// resolveClaudeCreds returns the claude OAuth blob (Max login) for the container's
// ~/.claude/.credentials.json: macOS keychain, else the file. See docs/agent.md.
func (r *Runner) resolveClaudeCreds(ctx context.Context) string {
	var blob string
	if runtime.GOOS == "darwin" {
		out, err := r.Runner.Capture(ctx, "security", "find-generic-password",
			"-s", claudeKeychainService, "-w")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward container: could not read claude credentials from keychain (%v); claude will be unauthenticated\n", err)
			return ""
		}
		blob = strings.TrimSpace(string(out))
	} else {
		path := filepath.Join(homeDir(), ".claude", ".credentials.json")
		data, err := os.ReadFile(path) // #nosec G304 -- fixed per-user claude creds path
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward container: could not read %s (%v); claude will be unauthenticated\n", path, err)
			return ""
		}
		blob = strings.TrimSpace(string(data))
	}
	// Early heads-up only (ward#222): the blob still ships - the in-container auth
	// smoke test is the hard gate - but the cause is now visible host-side.
	if ok, reason := claudeCredsHealth(blob, time.Now()); !ok {
		fmt.Fprintf(os.Stderr, "ward container: claude credentials look unusable (%s); the in-container auth smoke test will fail loudly (ward#222)\n", reason)
	}
	return blob
}

// claudeCredsHealth flags a clearly-unusable claude OAuth blob (empty, no access
// token, expired) before it ships into a container (ward#222). Pure + testable.
func claudeCredsHealth(blob string, now time.Time) (ok bool, reason string) {
	blob = strings.TrimSpace(blob)
	if blob == "" {
		return false, "empty credentials"
	}
	var parsed struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"`
	}
	if err := json.Unmarshal([]byte(blob), &parsed); err != nil {
		return true, "" // unrecognised shape; defer to the smoke test
	}
	token, exp := parsed.ClaudeAiOauth.AccessToken, parsed.ClaudeAiOauth.ExpiresAt
	if token == "" {
		token, exp = parsed.AccessToken, parsed.ExpiresAt
	}
	if token == "" {
		return false, "no accessToken in credentials"
	}
	if exp > 0 && now.After(time.UnixMilli(exp)) {
		return false, fmt.Sprintf("access token expired %s ago (re-run 'claude' on the host to refresh)", now.Sub(time.UnixMilli(exp)).Round(time.Second))
	}
	return true, ""
}

// resolveCodexCreds reads the container's ~/.codex/auth.json from the host file
// (best-effort: an empty return leaves codex unauthenticated). docs/agent.md.
func (r *Runner) resolveCodexCreds() string {
	path := filepath.Join(homeDir(), ".codex", "auth.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed per-user codex creds path
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container: could not read %s (%v); codex will be unauthenticated\n", path, err)
		return ""
	}
	return strings.TrimSpace(string(data))
}

// resolveOllamaHost reads the tower Ollama endpoint from SSM host-side so goose can
// bind it (the container can't resolve SSM). Best-effort: empty falls back.
func (r *Runner) resolveOllamaHost(ctx context.Context) string {
	out, err := r.Runner.Capture(ctx, "aws", "ssm", "get-parameter",
		"--name", ollamaHostSSMPath, "--with-decryption",
		"--query", "Parameter.Value", "--output", "text")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container: could not resolve %s from SSM (%v); goose will fall back to its config default ollama host\n", ollamaHostSSMPath, err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// credEnvLines renders the base64'd per-mode credential env-file lines, one per
// present blob; pure, so the secret-shaping is unit-testable. See docs/agent.md.
func credEnvLines(creds agentCreds) []string {
	var lines []string
	if creds.Claude != "" {
		lines = append(lines, "WARD_CLAUDE_CREDS_B64="+base64.StdEncoding.EncodeToString([]byte(creds.Claude)))
	}
	if creds.Codex != "" {
		lines = append(lines, "WARD_CODEX_AUTH_B64="+base64.StdEncoding.EncodeToString([]byte(creds.Codex)))
	}
	if creds.GooseOllamaHost != "" {
		lines = append(lines, "WARD_GOOSE_OLLAMA_HOST_B64="+base64.StdEncoding.EncodeToString([]byte(creds.GooseOllamaHost)))
	}
	return lines
}

// resolveForgejoToken resolves the child env-file's forge token: the broker seed
// first (broker-side, not a token the agent holds; ward#334), then env, then SSM.
func (r *Runner) resolveForgejoToken(ctx context.Context, target broker.Target) (string, error) {
	if tok, ok := r.brokerDispatchSeed(ctx, target); ok {
		return tok, nil
	}
	if tok := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); tok != "" {
		return tok, nil
	}
	out, err := r.Runner.Capture(ctx, "aws", "ssm", "get-parameter",
		"--name", forgejoTokenSSMPath, "--with-decryption",
		"--query", "Parameter.Value", "--output", "text")
	if err != nil {
		return "", fmt.Errorf("ward container: resolve %s from SSM (host needs aws creds, or set FORGEJO_TOKEN): %w", forgejoTokenSSMPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// writeTokenEnvFile resolves the forgejo token (+ optional base64'd agent creds) into a
// private 0600 --env-file (none enters argv/audit); target lets a brokered box seed it.
func (r *Runner) writeTokenEnvFile(ctx context.Context, target broker.Target, creds agentCreds) (path string, cleanup func(), err error) {
	token, err := r.resolveForgejoToken(ctx, target)
	if err != nil {
		return "", func() {}, err
	}
	f, err := os.CreateTemp("", "ward-forgejo-env-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("ward container: create env-file: %w", err)
	}
	path = f.Name()
	cleanup = func() { _ = os.Remove(path) }
	if cherr := f.Chmod(0o600); cherr != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: secure env-file: %w", cherr)
	}
	if _, werr := fmt.Fprintf(f, "FORGEJO_TOKEN=%s\n", token); werr != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: write env-file: %w", werr)
	}
	// Agent credentials (claude OAuth, codex auth.json) ride base64'd, one line
	// each, after the token; the entrypoint decodes whichever its mode needs.
	for _, line := range credEnvLines(creds) {
		if _, werr := fmt.Fprintf(f, "%s\n", line); werr != nil {
			_ = f.Close()
			cleanup()
			return "", func() {}, fmt.Errorf("ward container: write agent creds to env-file: %w", werr)
		}
	}
	if cerr := f.Close(); cerr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: close env-file: %w", cerr)
	}
	return path, cleanup, nil
}

// writeTSAuthEnvFile resolves the tag:proxy auth key from SSM into a private 0600
// --env-file (TS_AUTHKEY), never argv/audit; the sidecar reads it (ward#333).
func (r *Runner) writeTSAuthEnvFile(ctx context.Context) (path string, cleanup func(), err error) {
	out, cerr := r.Runner.Capture(ctx, "aws", "ssm", "get-parameter",
		"--name", tsAuthKeySSMPath, "--with-decryption",
		"--query", "Parameter.Value", "--output", "text")
	if cerr != nil {
		return "", func() {}, fmt.Errorf("ward container: resolve %s from SSM (host needs aws creds): %w", tsAuthKeySSMPath, cerr)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", func() {}, fmt.Errorf("ward container: %s resolved empty; cannot start the tailscale sidecar", tsAuthKeySSMPath)
	}
	f, ferr := os.CreateTemp("", "ward-ts-authkey-env-*")
	if ferr != nil {
		return "", func() {}, fmt.Errorf("ward container: create ts-authkey env-file: %w", ferr)
	}
	path = f.Name()
	cleanup = func() { _ = os.Remove(path) }
	if cherr := f.Chmod(0o600); cherr != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: secure ts-authkey env-file: %w", cherr)
	}
	if _, werr := fmt.Fprintf(f, "TS_AUTHKEY=%s\n", key); werr != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: write ts-authkey env-file: %w", werr)
	}
	if clerr := f.Close(); clerr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: close ts-authkey env-file: %w", clerr)
	}
	return path, cleanup, nil
}

// startTSSidecar resolves the tag:proxy key from SSM and runs the userspace SOCKS5
// sidecar detached, TS_AUTHKEY in an env-file (ward#333; reach awaits infra#400).
func (r *Runner) startTSSidecar(ctx context.Context, plan upPlan) error {
	envFile, cleanup, err := r.writeTSAuthEnvFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	argv := tsSidecarRunArgv(plan.Name, plan.Repo.slug(), envFile)
	if rerr := r.runDockerSilenced(ctx, false, argv...); rerr != nil {
		return fmt.Errorf("ward container: start tailscale sidecar %s: %w", tsSidecarName(plan.Name), rerr)
	}
	return nil
}

// stopTSSidecar force-removes a carry's sidecar (best-effort) on the attached path;
// a detached carry's is reclaimed by the next launch's orphan sweep (ward#333).
func (r *Runner) stopTSSidecar(ctx context.Context, carryName string) {
	_ = r.runDockerSilenced(ctx, true, "rm", "-f", tsSidecarName(carryName))
}

// sweepOrphanedSidecars force-removes sidecars whose carry is gone (best-effort,
// never blocks a launch; ward#333).
func (r *Runner) sweepOrphanedSidecars(ctx context.Context) {
	out, err := r.Runner.Capture(ctx, "docker", dockerWardListArgv()...)
	if err != nil {
		return
	}
	orphans := orphanedSidecars(string(out))
	if len(orphans) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "ward container: reclaiming %d orphaned tailscale sidecar(s) whose carry is gone (ward#333)\n", len(orphans))
	if rmErr := r.runDockerSilenced(ctx, true, dockerForceRmArgv(orphans)...); rmErr != nil {
		fmt.Fprintf(os.Stderr, "ward container: orphan-sidecar sweep had a non-zero rm (%v); continuing\n", rmErr)
	}
}

// randHex returns 4 random bytes as an 8-char lowercase hex string, the unique
// suffix that lets repeated container bring-ups against one repo coexist.
func randHex() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// rand.Read never fails on supported platforms; fall back so a name is
		// still produced rather than panicking a dev command.
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// homeDir resolves the operator's home, used only for the --aws mount source.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

// containerAssetsPrefix names the per-run asset dirs so the stale sweep can
// find them; containerAssetsTTL is how long one may linger before reclaim.
const containerAssetsPrefix = "ward-container-assets-"

const containerAssetsTTL = time.Hour

// sweepStaleContainerAssets reclaims asset dirs past the TTL - left by detached
// runs that cannot delete their own still-mounted dir on return. Best-effort.
func sweepStaleContainerAssets() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), containerAssetsPrefix) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || time.Since(info.ModTime()) < containerAssetsTTL {
			continue
		}
		_ = os.RemoveAll(filepath.Join(os.TempDir(), e.Name()))
	}
}

// sweepStaleContainers host-side-reclaims exited ward containers' writable layers
// before a run, keeping the recent containerReapKeep (docs/container-cleanup.md).
func (r *Runner) sweepStaleContainers(ctx context.Context) {
	// Reclaim any sidecar whose carry has been reaped, so a detached --ts-sidecar
	// run leaves no lingering proxy (ward#333).
	r.sweepOrphanedSidecars(ctx)
	out, err := r.Runner.Capture(ctx, "docker", dockerExitedListArgv()...)
	if err != nil {
		// No docker / daemon down / query failed: nothing to sweep, and the
		// cleanup courtesy must never block a launch.
		return
	}
	stale := staleContainersToReap(string(out), containerReapKeep)
	if len(stale) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "ward container: reclaiming %d exited ward container(s) past the keep-%d window (ward#272)\n", len(stale), containerReapKeep)
	// `docker rm` returns non-zero if one name raced into removal; the rest still
	// go, so a missed reclaim is logged, never a launch failure.
	if rmErr := r.Runner.Exec(ctx, "docker", dockerRmArgv(stale)...); rmErr != nil {
		fmt.Fprintf(os.Stderr, "ward container: stale-container sweep had a non-zero rm (%v); continuing\n", rmErr)
	}
}

// writeContainerAssets materializes the embedded entrypoint + doctrine into a
// per-run tmp dir mounted read-only at /opt/ward, sweeping stale dirs first.
func writeContainerAssets() (dir string, cleanup func(), err error) {
	sweepStaleContainerAssets()
	dir, err = os.MkdirTemp("", containerAssetsPrefix+"*")
	if err != nil {
		return "", func() {}, fmt.Errorf("ward container: create assets dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	files := []struct {
		name string
		mode os.FileMode
	}{
		{"entrypoint.sh", 0o755},
		{"AGENTS.container.md", 0o644},
		{"settings.container.json", 0o644},
		{containerSubstrateRel, 0o644},
	}
	for _, f := range files {
		data, rerr := containerAssets.ReadFile("containerassets/" + f.name)
		if rerr != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("ward container: read embedded %s: %w", f.name, rerr)
		}
		if werr := os.WriteFile(filepath.Join(dir, f.name), data, f.mode); werr != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("ward container: write %s: %w", f.name, werr)
		}
	}
	return dir, cleanup, nil
}
