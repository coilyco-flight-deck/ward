package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
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

// forgejoTokenSSMPath is the host-resolved git-over-HTTPS push token: the
// coilyco-ops bot, not a personal PAT (ward#161). See docs/agent-attribution.md.

// #nosec G101 -- this is an SSM parameter path, not an embedded secret.
const forgejoTokenSSMPath = "/forgejo/coilyco-ops/api-token"

// ollamaHostSSMPath is the SSM param for the tower Ollama endpoint goose binds;
// ward resolves it host-side (the container has no aws creds). docs/agent.md (ward#186).
const ollamaHostSSMPath = "/coilysiren/ollama/host"

// containerCommand is the Hidden `ward container` umbrella (ward#263): only the
// entrypoint-internal reap/bootstrap leaves remain. See docs/container.md.
func containerCommand() *cli.Command {
	return &cli.Command{
		Name:   "container",
		Hidden: true,
		Usage:  "Entrypoint-internal container plumbing (reap/bootstrap plus startup helpers). Use `ward agent` to run a feature.",
		Description: `container is plumbing-only as of ward#263: the user-facing lifecycle verbs
(up/exec/down/ls) were retired in favour of ` + "`ward agent`" + `. The leaves that
remain here - reap, bootstrap, and a few startup helpers - are invoked by the
in-container entrypoint, not by hand. See docs/agent.md for the contributor surface.`,
		Commands: []*cli.Command{
			containerReapCommand(),
			containerBootstrapCommand(),
			containerResolveContextCommand(),
			containerSubstrateInventoryCommand(),
			containerSubstrateCatalogCommand(),
			containerBrokerCommand(),
			containerForwardCommand(),
			containerDrainExitCommand(),
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

var directorSurfaceSessionSuffix = dictatableID
var stageWardBootstrapBinary = realStageWardBootstrapBinary

// dictatableID returns the aos/o2r short agent-id shape: two lowercase letters
// from the dictatable alphabet, then two digits.
func dictatableID() string {
	const letters = "abcdefghjkmpqrstuvwxyz"
	const digits = "456789"

	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "zz00"
	}
	return string([]byte{
		letters[int(raw[0])%len(letters)],
		letters[int(raw[1])%len(letters)],
		digits[int(raw[2])%len(digits)],
		digits[int(raw[3])%len(digits)],
	})
}

// buildUpPlan assembles the pure plan from parsed flags and resolved inputs;
// agentArgs seed the agent's argv. Errors only on a bad --repo grant (ward#230).
func buildUpPlan(c *cli.Command, repo targetRepo, mode containerMode, role, cwd, assetsDir string, agentArgs []string, mountSurfaceExtras bool) (upPlan, error) {
	wardSrc := c.String("ward-source")
	// The container downloads this host's ward version by default; --ward-version
	// (env WARD_AGENT_VERSION) overrides it to pin a known-good release (ward#312).
	wardVersion := strings.TrimSpace(c.String("ward-version"))
	if wardVersion == "" {
		wardVersion = Version
	}
	wardVersionSource := resolveWardVersionSource(c, wardVersion)
	// A pin behind this host ships an older in-container reaper - the last line against
	// lost/false-salvaged work - so refuse the downgrade unless opted in (ward#529).
	if err := wardDowngradeGuard(wardVersion, Version, c.Bool("allow-ward-downgrade")); err != nil {
		return upPlan{}, err
	}
	// Host/cloud capability is the role's guardfile set (ward#578; docs/agent-flags.md),
	// resolved to the mechanisms ward composes; a tailnet grant implies the ~/.aws mount.
	capab := resolveCapability(c, role)
	hostNet, tsSidecar, err := resolveTailnetMechanism(c, runtime.GOOS, capab.tailnet)
	if err != nil {
		return upPlan{}, err
	}
	awsHome := ""
	if capab.aws {
		awsHome = filepath.Join(homeDir(), ".aws")
	}
	// extraRepoGrant reads the --repo grant on the agent surfaces and --with-repo on
	// advisor/director (ward#280, ward#362; docs/container-multi-repo.md).
	extra, err := parseExtraRepos(extraRepoGrant(c), repo)
	if err != nil {
		return upPlan{}, err
	}
	// The catalog.dependsOn read-only context set is NOT resolved here: the host cwd may
	// not be the target repo, so the container resolves it from the fresh clone (ward#580).

	// Repeatable `--config` overrides ride in as WARD_* env (ward#616); an unknown key
	// fails loud here, before any container spins. c.StringSlice is nil-safe when unset.
	configEnv, err := parseConfigOverrides(c.StringSlice("config"))
	if err != nil {
		return upPlan{}, err
	}
	// Validate the staged container-topology bundle once here so a malformed live
	// bundle fails before launch, while a missing optional file still falls back.
	if _, err := currentContainerTopologyWithError(); err != nil {
		return upPlan{}, err
	}

	// The director surface opts into read-only binds of the redacted agent-log drain.
	// It also mounts the Docker socket so it can reap engineers (ward#1001).
	agentLogs := ""
	if mountSurfaceExtras {
		agentLogs = agentLogsRedactedDir()
	}
	// The per-container machine id rides the ward.machine label. Director surface
	// containers use a short dictatable id suffix instead of the machine id.
	machine := randHex()
	return upPlan{
		Image:             imageRef(c.String("image"), c.String("tag")),
		Name:              containerRoleName(role, mode, repo, 0, containerNameSuffix(role, machine)),
		Role:              role,
		ConfigRole:        role,
		Machine:           machine,
		Repo:              repo,
		Mode:              mode,
		Branch:            c.String("branch"),
		ForgejoBase:       forgejoBaseURL,
		HostCwd:           cwd,
		AWSHome:           awsHome,
		Mounts:            appendSurfaceMounts(leastAccessMounts(cwd, mountOpts{AssetsDir: assetsDir, AWSHome: awsHome, WardSource: wardSrc, AgentLogsDir: agentLogs}), mountSurfaceExtras),
		Interactive:       !c.Bool("detach"),
		TTY:               !c.Bool("detach") && terminalAttached(),
		WardVersion:       wardVersion,
		WardVersionSource: wardVersionSource,
		WardFromSource:    wardSrc != "",
		AgentArgs:         agentArgs,
		ExtraRepos:        extra,
		HostNet:           hostNet,
		TSSidecar:         tsSidecar,
		SkipPreflight:     c.Bool("skip-preflight") || c.Bool("no-preflight"),
		ConfigEnv:         configEnv,
	}, nil
}

func appendSurfaceMounts(mounts []mountSpec, mountSurfaceExtras bool) []mountSpec {
	if mountSurfaceExtras {
		mounts = append(mounts, dockerSockMount())
	}
	return mounts
}

func containerNameSuffix(role string, machine string) string {
	if role == roleSession || role == roleDirector {
		return directorSurfaceSessionSuffix()
	}
	return machine
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
		writeln(w, msg)
	}
}

// maybeWarnAWSMount warns when the aws capability bound ~/.aws but the host has no
// creds there, so an empty-dir mount doesn't read as working SSM (ward#579).
func (r *Runner) maybeWarnAWSMount(plan upPlan) {
	if plan.AWSHome == "" {
		return
	}
	if msg, warn := awsMountMissingWarning(plan.AWSHome, awsHomeHasCreds(plan.AWSHome)); warn {
		w := r.Runner.Stderr
		if w == nil {
			w = os.Stderr
		}
		writeln(w, msg)
	}
}

// awsHomeHasCreds reports whether the host ~/.aws dir holds a config or credentials
// file (the two the AWS SDK reads); a missing or empty dir reads false (ward#579).
func awsHomeHasCreds(awsHome string) bool {
	if awsHome == "" {
		return false
	}
	for _, name := range []string{"credentials", "config"} {
		if fi, err := os.Stat(filepath.Join(awsHome, name)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
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
		writef(os.Stderr, "ward container: could not resolve %s from SSM (%v); goose will fall back to its config default ollama host\n", ollamaHostSSMPath, err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveForgejoToken resolves the child env-file's push/API token: GitHub and GitLab
// from host-side env or CLI fallbacks; Forgejo via broker seed, env, then SSM.
func (r *Runner) resolveForgejoToken(ctx context.Context, target broker.Target, f forge) (string, error) {
	if tok := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); tok != "" {
		return tok, nil
	}
	if r == nil || r.Runner == nil {
		return "", fmt.Errorf("ward container: resolve Forgejo token: no shell runner configured")
	}
	if f == forgeGitHub {
		return r.resolveGitHubToken(ctx, target.Owner, target.Repo)
	}
	if f == forgeGitLab {
		return r.resolveGitLabToken(ctx, target.Owner, target.Repo)
	}
	if tok, ok := r.brokerDispatchSeed(ctx, target); ok {
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

func (r *Runner) writeTokenEnvFile(ctx context.Context, target broker.Target, fg forge, creds []agentsapi.EnvLine) (path string, cleanup func(), err error) {
	token, err := r.resolveForgejoToken(ctx, target, fg)
	if err != nil {
		return "", func() {}, err
	}
	// Land the env-file where the docker CLI can read it at `docker run`: a snap
	// docker's private /tmp hides a /tmp path (ward#569; docs/container-env.md).
	dir := launchStagingDir()
	sweepStaleLaunchEnvFiles(dir)
	f, err := os.CreateTemp(dir, launchEnvFilePrefix+"*")
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
	if err := writeEnvLine(f, cleanup, "FORGEJO_TOKEN", token, "env-file"); err != nil {
		return "", func() {}, err
	}
	if err := writeForgeTokenEnvLines(f, cleanup, fg, token); err != nil {
		return "", func() {}, err
	}
	if err := writeAgentCredEnvLines(f, cleanup, creds); err != nil {
		return "", func() {}, err
	}
	if cerr := f.Close(); cerr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("ward container: close env-file: %w", cerr)
	}
	return path, cleanup, nil
}

func writeEnvLine(f *os.File, cleanup func(), key, value, kind string) error {
	if _, err := fmt.Fprintf(f, "%s=%s\n", key, value); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("ward container: write %s to env-file: %w", kind, err)
	}
	return nil
}

func writeForgeTokenEnvLines(f *os.File, cleanup func(), fg forge, token string) error {
	switch fg {
	case forgeForgejo:
		return nil
	case forgeGitHub:
		for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
			if err := writeEnvLine(f, cleanup, key, token, "github token"); err != nil {
				return err
			}
		}
	case forgeGitLab:
		for _, key := range []string{"GITLAB_TOKEN"} {
			if err := writeEnvLine(f, cleanup, key, token, "gitlab token"); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeAgentCredEnvLines(f *os.File, cleanup func(), creds []agentsapi.EnvLine) error {
	for _, line := range creds {
		if err := writeEnvLine(f, cleanup, line.Key, line.Value, "agent creds"); err != nil {
			return err
		}
	}
	return nil
}

// launchEnvFilePrefix is the temp-name prefix for the docker --env-file; a shared
// const so the stale-orphan sweep can recognize leftovers written by launchStagingDir.
const launchEnvFilePrefix = "ward-forgejo-env-"

// launchStagingDir is the $HOME-else-$TMPDIR dir a snap docker can reach for both
// the --env-file and the assets bind-mount (ward#569, ward#574; docs/container-env.md).
func launchStagingDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return os.TempDir()
	}
	return home
}

// sweepStaleLaunchEnvFiles best-effort removes past-TTL env-file orphans in dir;
// $HOME is never OS-reaped and each orphan holds a live 0600 token (ward#569).
func sweepStaleLaunchEnvFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), launchEnvFilePrefix) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || time.Since(info.ModTime()) < containerAssetsTTL() {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}

// preflightTailnet readies the ward-tailnet network for a --ts-sidecar run: a missing
// network is created (not a failure), an unattached box only warns (ward#597, #349).
func (r *Runner) preflightTailnet(ctx context.Context, plan upPlan) error {
	if !plan.TSSidecar {
		return nil
	}
	out, err := r.dockerCapture(ctx, dockerTailnetInspectArgv()...)
	if err != nil {
		// Network absent: create it rather than fail (ward#597); a fresh net has no box
		// yet, so `out` stays box-less and the warning below fires until mac-proxy converges.
		if cerr := r.ensureTailnetNetwork(ctx); cerr != nil {
			return cerr
		}
	}
	if msg, warn := proxyBoxMissingWarning(string(out)); warn {
		w := r.Runner.Stderr
		if w == nil {
			w = os.Stderr
		}
		writeln(w, msg)
	}
	return nil
}

// ensureTailnetNetwork idempotently creates the ward-tailnet network: a create that
// loses the race to "already exists" is benign if a re-inspect now finds it (ward#597).
func (r *Runner) ensureTailnetNetwork(ctx context.Context) error {
	if _, err := r.dockerCapture(ctx, dockerTailnetCreateArgv()...); err != nil {
		if _, ierr := r.dockerCapture(ctx, dockerTailnetInspectArgv()...); ierr != nil {
			return fmt.Errorf("ward agent: could not create the %q docker network for the tailnet route: %w; "+
				"create it by hand (docker network create %s) or re-run with --no-tailnet to dispatch isolated (ward#597)",
				tailnetNetwork(), err, tailnetNetwork())
		}
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

// sweepStaleContainerAssets best-effort reclaims past-TTL asset dirs in dir, left
// by detached runs that cannot delete their own still-mounted dir on return.
func sweepStaleContainerAssets(dir string, live map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), containerAssetsPrefix) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || time.Since(info.ModTime()) < containerAssetsTTL() {
			continue
		}
		if live != nil && live[filepath.Join(dir, e.Name())] {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// dockerInspectMount mirrors the subset of `docker inspect --format {{json .Mounts}}`.
// It only carries the fields needed to find the host path behind a live /opt/ward bind.
type dockerInspectMount struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// liveContainerAssetDirs returns live /opt/ward bind sources.
// If docker can't answer conclusively, the caller must skip stale sweeping.
func (r *Runner) liveContainerAssetDirs(ctx context.Context) (map[string]bool, bool) {
	out, err := r.dockerCapture(ctx, "ps", "--filter", "label="+containerLabel, "--filter", "status=running", "--format", "{{.Names}}")
	if err != nil {
		return nil, false
	}
	live := map[string]bool{}
	ok := true
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		mountsOut, merr := r.dockerCapture(ctx, "inspect", "--format", "{{json .Mounts}}", name)
		if merr != nil {
			ok = false
			continue
		}
		var mounts []dockerInspectMount
		if jerr := json.Unmarshal(mountsOut, &mounts); jerr != nil {
			ok = false
			continue
		}
		for _, m := range mounts {
			if m.Destination == containerWardAssets && strings.TrimSpace(m.Source) != "" {
				live[m.Source] = true
			}
		}
	}
	return live, ok
}

// sweepStaleContainers host-side-reclaims exited ward containers' writable layers
// before a run, keeping the recent containerReapKeep (docs/container-cleanup.md).
func (r *Runner) sweepStaleContainers(ctx context.Context) {
	out, err := r.dockerCapture(ctx, dockerExitedListArgv()...)
	if err != nil {
		// No docker / daemon down / query failed: nothing to sweep, and the
		// cleanup courtesy must never block a launch.
		return
	}
	exited := parseExitedContainerNames(string(out))
	if len(exited) == 0 {
		return
	}
	stale := staleContainersToReap(string(out), containerReapKeep())

	// Drain EVERY exited run idempotently (ward#510) then reclaim the past-keep tail,
	// drained-first so the rm never takes an un-drained log (ward#363).
	if len(stale) > 0 {
		writef(os.Stderr, "ward container: reclaiming %d exited ward container(s) past the keep-%d window (ward#272)\n", len(stale), containerReapKeep())
		writef(os.Stderr, "ward container: containers being removed: %s\n", strings.Join(stale, ", "))
	}
	if rmErr := r.drainStaleContainers(ctx, exited, stale); rmErr != nil {
		writef(os.Stderr, "ward container: stale-container sweep had a non-zero rm (%v); continuing\n", rmErr)
	} else if len(stale) > 0 {
		writef(os.Stderr, "ward container: successfully drained and removed %d containers\n", len(stale))
	}
}

// clearExitedContainer force-removes an exited container holding name so a reused
// engineer name can launch; a running one is left alone, errors never block (ward#364).
func (r *Runner) clearExitedContainer(ctx context.Context, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	out, err := r.dockerCapture(ctx, "ps", "-a",
		"--filter", "name=^"+name+"$", "--filter", "status=exited", "--format", "{{.Names}}")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return
	}
	if rmErr := r.dockerExec(ctx, "rm", "-f", name); rmErr != nil {
		writef(os.Stderr, "ward container: could not clear exited container %q for name reuse (%v); continuing\n", name, rmErr)
	}
	// The corpse is gone; drop its drain sentinel so the reused deterministic name
	// drains fresh rather than being skipped by the dead run's marker (ward#510).
	clearDrainMarker(agentLogsDir(), name)
}

// writeContainerAssets materializes the embedded entrypoint + doctrine and stages
// the matching ward binary into a per-run dir under launchStagingDir.
func writeContainerAssets(ctx context.Context, r *Runner, wardSource, wardVersion string) (dir string, cleanup func(), err error) {
	root := launchStagingDir()
	if r != nil {
		r.sweepStaleContainerAssets(ctx, root)
	}
	dir, err = os.MkdirTemp(root, containerAssetsPrefix+"*")
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
	if err := stageWardBootstrapBinary(ctx, dir, wardSource, wardVersion); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return dir, cleanup, nil
}

func (r *Runner) sweepStaleContainerAssets(ctx context.Context, dir string) {
	if r == nil || r.Runner == nil {
		return
	}
	live, ok := r.liveContainerAssetDirs(ctx)
	if !ok {
		return
	}
	sweepStaleContainerAssets(dir, live)
}

func realStageWardBootstrapBinary(ctx context.Context, dir, wardSource, wardVersion string) error {
	path := filepath.Join(dir, "ward")
	if strings.TrimSpace(wardSource) != "" {
		return buildWardBootstrapBinary(ctx, wardSource, path)
	}
	return downloadWardBootstrapBinary(ctx, wardVersion, path)
}

func buildWardBootstrapBinary(ctx context.Context, wardSource, path string) error {
	arch, err := bootstrapGOARCH()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", path, "./cmd/ward") // #nosec G204 -- fixed bootstrap argv
	cmd.Dir = wardSource
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+arch,
		"GOPROXY=direct",
		"GOSUMDB=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ward container: build Go bootstrap binary from %s: %w: %s", wardSource, err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("ward container: chmod Go bootstrap binary %s: %w", path, err)
	}
	return nil
}

func downloadWardBootstrapBinary(ctx context.Context, wardVersion, path string) error {
	tag, err := resolveWardBootstrapTag(ctx, wardVersion)
	if err != nil {
		return err
	}
	arch, err := bootstrapGOARCH()
	if err != nil {
		return err
	}
	asset := fmt.Sprintf("%s/coilyco-flight-deck/ward/releases/download/%s/ward-linux-%s", forgejoBaseURL, tag, arch)
	var lastErr error
	for attempt := 1; attempt <= bootstrapDownloadAttempts; attempt++ {
		retryable, err := downloadWardBootstrapBinaryOnce(ctx, asset, path)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == bootstrapDownloadAttempts {
			break
		}
		if !waitForBootstrapRetry(ctx) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("ward container: download Go bootstrap binary %s after %d attempt(s): %w", asset, bootstrapDownloadAttempts, lastErr)
}

const bootstrapDownloadAttempts = 5

var bootstrapDownloadSleep = time.Sleep

func waitForBootstrapRetry(ctx context.Context) bool {
	done := make(chan struct{})
	go func() {
		bootstrapDownloadSleep(bootstrapDownloadBackoff)
		close(done)
	}()
	select {
	case <-ctx.Done():
		return false
	case <-done:
		return true
	}
}

const bootstrapDownloadBackoff = 2 * time.Second

func downloadWardBootstrapBinaryOnce(ctx context.Context, asset, path string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset, nil)
	if err != nil {
		return false, fmt.Errorf("ward container: prepare bootstrap download %s: %w", asset, err)
	}
	if token := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true, fmt.Errorf("ward container: download Go bootstrap binary %s: %w", asset, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		retryable := resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return retryable, fmt.Errorf("ward container: download Go bootstrap binary %s: unexpected status %s: %s", asset, resp.Status, strings.TrimSpace(string(body)))
	}
	f, err := os.Create(path)
	if err != nil {
		return false, fmt.Errorf("ward container: create bootstrap binary %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return false, fmt.Errorf("ward container: write bootstrap binary %s: %w", path, err)
	}
	if err := f.Chmod(0o755); err != nil {
		return false, fmt.Errorf("ward container: chmod bootstrap binary %s: %w", path, err)
	}
	return false, nil
}

func resolveWardBootstrapTag(ctx context.Context, wardVersion string) (string, error) {
	tag := strings.TrimSpace(wardVersion)
	if tag != "" && tag != "dev" {
		return tag, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, forgejoBaseURL+"/api/v1/repos/coilyco-flight-deck/ward/releases/latest", nil)
	if err != nil {
		return "", fmt.Errorf("ward container: resolve bootstrap release tag: %w", err)
	}
	if token := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ward container: resolve bootstrap release tag: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ward container: resolve bootstrap release tag: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("ward container: decode bootstrap release tag: %w", err)
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return "", fmt.Errorf("ward container: resolve bootstrap release tag: empty tag_name")
	}
	return strings.TrimSpace(payload.TagName), nil
}

func bootstrapGOARCH() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH, nil
	default:
		return "", fmt.Errorf("ward container: unsupported bootstrap architecture %q", runtime.GOARCH)
	}
}
