package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

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
			containerForwardCommand(),
		},
	}
}

// gooseOllamaHostEnvKey carries the base64'd tower Ollama endpoint goose binds,
// resolved host-side (goose is not a CredentialProvider); ward#425.
const gooseOllamaHostEnvKey = "WARD_GOOSE_OLLAMA_HOST_B64"

// resolveAgentCreds resolves the host-side credential env-file lines a mode needs
// through the drained CredentialProvider seam (goose's Ollama host aside; ward#425).
func (r *Runner) resolveAgentCreds(ctx context.Context, mode containerMode) []agentsapi.EnvLine {
	if agent, ok := agents.Lookup(string(mode)); ok {
		if cp, ok := agent.(agentsapi.CredentialProvider); ok {
			return cp.ResolveCreds(r.agentHostCtx(ctx))
		}
	}
	if mode == modeGoose {
		if host := r.resolveOllamaHost(ctx); host != "" {
			return []agentsapi.EnvLine{{Key: gooseOllamaHostEnvKey, Value: base64.StdEncoding.EncodeToString([]byte(host))}}
		}
	}
	return nil
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
	// Consolidated tailnet route (ward#362): --tailnet auto-selects host-net vs the sidecar
	// by platform (--tailnet-mode overrides); it implies --aws. docs/agent-flags.md.
	hostNet, tsSidecar, err := resolveTailnet(c, runtime.GOOS)
	if err != nil {
		return upPlan{}, err
	}
	awsHome := ""
	if c.Bool("aws") || tailnetEnabled(c) {
		awsHome = filepath.Join(homeDir(), ".aws")
	}
	// extraRepoGrant reads the --repo grant on the agent surfaces and --with-repo on
	// advisor/director (ward#280, ward#362; docs/container-multi-repo.md).
	extra, err := parseExtraRepos(extraRepoGrant(c), repo)
	if err != nil {
		return upPlan{}, err
	}
	// The per-container machine id: rides the ward.machine label, names issueless
	// roles. A role-led run overrides Role+Name after this (ward#364).
	machine := randHex()
	return upPlan{
		Image:          imageRef(c.String("image"), c.String("tag")),
		Name:           containerRoleName(roleSession, mode, repo, 0, machine),
		Role:           roleSession,
		Machine:        machine,
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
		ExtraRepos:     extra,
		HostNet:        hostNet,
		TSSidecar:      tsSidecar,
	}, nil
}

// localHasTailscale0 reports whether a tailscale0 interface exists on this host's
// netns (the netns a --host-net run joins on Linux); a probe error reads false.
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
func (r *Runner) writeTokenEnvFile(ctx context.Context, target broker.Target, creds []agentsapi.EnvLine) (path string, cleanup func(), err error) {
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
	for _, line := range creds {
		if _, werr := fmt.Fprintf(f, "%s=%s\n", line.Key, line.Value); werr != nil {
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

// preflightTailnetProxy verifies the standing mac-proxy box is attached to the
// ward-tailnet network before a --ts-sidecar run attaches (ward#349; the doc).
func (r *Runner) preflightTailnetProxy(ctx context.Context) error {
	out, err := r.Runner.Capture(ctx, "docker", dockerTailnetInspectArgv()...)
	if err != nil || !proxyBoxAttached(string(out)) {
		return fmt.Errorf("ward container: standing tailnet proxy not found - converge the mac-proxy infra role (agentic-os#291)")
	}
	return nil
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
	
	// Log the containers that will be reclaimed for monitoring and debugging
	var staleNames strings.Builder
	for i, name := range stale {
		if i > 0 {
			staleNames.WriteString(", ")
		}
		staleNames.WriteString(name)
	}
	
	fmt.Fprintf(os.Stderr, "ward container: reclaiming %d exited ward container(s) past the keep-%d window (ward#272)\n", len(stale), containerReapKeep)
	fmt.Fprintf(os.Stderr, "ward container: containers being removed: %s\n", staleNames.String())
	
	// Drain each container's console+transcript+meta to the host archive BEFORE the
	// rm takes them with it (ward#363); a raced/missed rm is logged, never fatal.
	if rmErr := r.drainStaleContainers(ctx, stale); rmErr != nil {
		fmt.Fprintf(os.Stderr, "ward container: stale-container sweep had a non-zero rm (%v); continuing\n", rmErr)
	} else {
		fmt.Fprintf(os.Stderr, "ward container: successfully drained and removed %d containers\n", len(stale))
	}
}

// clearExitedContainer force-removes an exited container holding name so a reused
// engineer name can launch; a running one is left alone, errors never block (ward#364).
func (r *Runner) clearExitedContainer(ctx context.Context, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	out, err := r.Runner.Capture(ctx, "docker", "ps", "-a",
		"--filter", "name=^"+name+"$", "--filter", "status=exited", "--format", "{{.Names}}")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return
	}
	if rmErr := r.Runner.Exec(ctx, "docker", "rm", "-f", name); rmErr != nil {
		fmt.Fprintf(os.Stderr, "ward container: could not clear exited container %q for name reuse (%v); continuing\n", name, rmErr)
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
