package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

const wardBootstrapRepo = "coilyco-flight-deck/ward"

//go:embed AGENTS.container.txt
//go:embed containerassets/*
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
var wardExecutablePath = os.Executable

// dictatableID returns the short agent-id shape: two lowercase letters from
// the dictatable alphabet, then two digits.
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
	// The container stages this host's ward version by default; --ward-version
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
	capab := resolveCapabilityWithOptOut(role, c.Bool("no-tailnet"))
	hostNet, tsSidecar := resolveTailnetMechanism(runtime.GOOS, capab.tailnet)
	awsHome := ""
	if capab.aws {
		awsHome = filepath.Join(homeDir(), ".aws")
	}
	// extraRepoGrant reads the --repo grant on the agent surfaces and --with-repo on
	// director (ward#280, ward#362; docs/container-multi-repo.md).
	extra, err := parseExtraRepos(extraRepoGrant(c), repo)
	if err != nil {
		return upPlan{}, err
	}
	// The catalog.dependsOn read-only context set is NOT resolved here: the host cwd may
	// not be the target repo, so the container resolves it from the fresh clone (ward#580).

	// Config-source env resolution fails loud here before any container spins.
	configEnv, err := resolveLaunchConfigEnv(c.StringSlice("config"), cwd)
	if err != nil {
		return upPlan{}, err
	}
	memoryLimit, memorySwap, err := resolveContainerMemorySettings()
	if err != nil {
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
		ConfigRef:         launchConfigRef(repo, cwd),
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
		MemoryLimit:       memoryLimit,
		MemorySwap:        memorySwap,
		AgentArgs:         agentArgs,
		ExtraRepos:        extra,
		HostNet:           hostNet,
		TSSidecar:         tsSidecar,
		SkipPreflight:     c.Bool("skip-preflight") || c.Bool("no-preflight"),
		ConfigEnv:         configEnv,
	}, nil
}

func launchConfigRef(repo targetRepo, cwd string) string {
	if !isCoilycoRepo(repo) {
		return ""
	}
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	ref, err := coilycoConfigRefFromTargetRepo(repo, cwd)
	if err != nil {
		return ""
	}
	return ref
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

// homeDir resolves the operator's home, used only for the optional mount source.
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
// before a run, keeping the recent containerReapTTL (docs/container-cleanup.md).
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
	containers := make([]exitedContainerSnapshot, 0, len(exited))
	for _, name := range exited {
		finishedAt, ok := r.containerFinishedAt(ctx, name)
		containers = append(containers, exitedContainerSnapshot{Name: name, FinishedAt: finishedAt, HasFinish: ok})
	}
	stale := staleContainersToReap(time.Now(), containers, containerReapTTL())

	// Drain EVERY exited run idempotently (ward#510) then reclaim the past-TTL tail,
	// drained-first so the rm never takes an un-drained log (ward#363).
	if len(stale) > 0 {
		writef(os.Stderr, "ward container: reclaiming %d exited ward container(s) past the %s window (ward#272)\n", len(stale), conciseDuration(containerReapTTL()))
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
		path := "containerassets/" + f.name
		if f.name == "AGENTS.container.md" {
			path = "AGENTS.container.txt"
		}
		data, rerr := containerAssets.ReadFile(path)
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
	pin := strings.TrimSpace(wardVersion)
	if pin != "" && pin != Version {
		return downloadWardBootstrapBinary(ctx, pin, path)
	}
	staged, err := stagePackagedWardBootstrapBinary(path)
	if err != nil {
		return err
	}
	if staged {
		return nil
	}
	if pin == "" {
		pin = Version
	}
	return downloadWardBootstrapBinary(ctx, pin, path)
}

// stagePackagedWardBootstrapBinary copies the release-matched Linux binary.
// Older packages without one fall back to the release download.
func stagePackagedWardBootstrapBinary(path string) (bool, error) {
	exe, err := wardExecutablePath()
	if err != nil {
		return false, fmt.Errorf("ward container: resolve host ward executable: %w", err)
	}
	arch, err := bootstrapGOARCH()
	if err != nil {
		return false, err
	}
	for _, candidate := range packagedWardBootstrapCandidates(exe, runtime.GOOS, arch) {
		info, statErr := os.Stat(candidate)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return false, fmt.Errorf("ward container: inspect packaged bootstrap binary %s: %w", candidate, statErr)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("ward container: packaged bootstrap binary %s is not a regular file", candidate)
		}
		if err := copyWardBootstrapBinary(candidate, path); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func packagedWardBootstrapCandidates(exe, goos, arch string) []string {
	paths := []string{exe}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		paths = append(paths, resolved)
	}
	if goos == "linux" {
		return uniquePaths(paths)
	}

	asset := bootstrapWardBinaryAssetName(arch)
	var candidates []string
	for _, executable := range paths {
		binDir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(binDir, asset),
			filepath.Join(filepath.Dir(binDir), "libexec", asset),
		)
	}
	return uniquePaths(candidates)
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		key := clean
		if runtime.GOOS == "windows" {
			key = strings.ToLower(clean)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func copyWardBootstrapBinary(source, path string) error {
	f, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("ward container: open packaged bootstrap binary %s: %w", source, err)
	}
	defer func() { _ = f.Close() }()
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(f, prefix); err != nil {
		return fmt.Errorf("ward container: packaged bootstrap binary %s is not an ELF binary: read header: %w", source, err)
	}
	if !bytes.Equal(prefix, []byte{0x7f, 'E', 'L', 'F'}) {
		return fmt.Errorf("ward container: packaged bootstrap binary %s is not an ELF binary", source)
	}
	if err := writeWardBootstrapBinary(path, io.MultiReader(bytes.NewReader(prefix), f)); err != nil {
		return fmt.Errorf("ward container: copy packaged bootstrap binary %s: %w", source, err)
	}
	return nil
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
	arch, err := bootstrapGOARCH()
	if err != nil {
		return err
	}
	assetName := bootstrapWardBinaryAssetName(arch)
	tag, err := resolveWardBootstrapTag(ctx, wardVersion, assetName)
	if err != nil {
		return err
	}
	asset := fmt.Sprintf("%s/%s/releases/download/%s/%s", forgejoBaseURL, wardBootstrapRepo, tag, assetName)
	var lastErr error
	for attempt := 1; attempt <= bootstrapDownloadAttempts; attempt++ {
		retryable, err := downloadWardBootstrapBinaryOnce(ctx, tag, assetName, asset, path)
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

func downloadWardBootstrapBinaryOnce(ctx context.Context, tag, assetName, asset, path string) (bool, error) {
	return downloadWardBootstrapBinaryURLOnce(ctx, tag, assetName, asset, path, true)
}

func downloadWardBootstrapBinaryURLOnce(ctx context.Context, tag, assetName, asset, path string, allowMetadata bool) (bool, error) {
	resp, retryable, err := requestWardBootstrapBinary(ctx, tag, assetName, asset)
	if err != nil {
		return retryable, err
	}
	defer func() { _ = resp.Body.Close() }()
	return stageWardBootstrapResponse(ctx, tag, assetName, path, allowMetadata, resp.Body)
}

func requestWardBootstrapBinary(ctx context.Context, tag, assetName, asset string) (*http.Response, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset, nil)
	if err != nil {
		return nil, false, fmt.Errorf("ward container: prepare bootstrap download: %w", err)
	}
	if token := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("ward container: download Go bootstrap binary: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, false, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, newReleaseAssetsNotReadyError(tag, assetName, strings.TrimSpace(string(body)))
	}
	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	return nil, retryable, fmt.Errorf("ward container: download Go bootstrap binary: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

func stageWardBootstrapResponse(ctx context.Context, tag, assetName, path string, allowMetadata bool, src io.Reader) (bool, error) {
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(src, prefix); err != nil {
		return false, fmt.Errorf("ward container: bootstrap download is not an ELF binary: read header: %w", err)
	}
	body := io.MultiReader(bytes.NewReader(prefix), src)
	if bytes.Equal(prefix, []byte{0x7f, 'E', 'L', 'F'}) {
		return false, writeWardBootstrapBinary(path, body)
	}
	if !allowMetadata || prefix[0] != '{' {
		return false, errors.New("ward container: bootstrap download is not an ELF binary")
	}
	metadata, err := io.ReadAll(io.LimitReader(body, 64*1024))
	if err != nil {
		return false, fmt.Errorf("ward container: read bootstrap asset metadata: %w", err)
	}
	var resolved forgejoReleaseAsset
	if err := json.Unmarshal(metadata, &resolved); err != nil {
		return false, fmt.Errorf("ward container: decode bootstrap asset metadata: %w", err)
	}
	if strings.TrimSpace(resolved.Name) != assetName {
		return false, fmt.Errorf("ward container: bootstrap asset metadata names %q, want %q", resolved.Name, assetName)
	}
	resolvedURL, err := sameOriginWardBootstrapURL(resolved.BrowserDownloadURL)
	if err != nil {
		return false, err
	}
	return downloadWardBootstrapBinaryURLOnce(ctx, tag, assetName, resolvedURL, path, false)
}

func writeWardBootstrapBinary(path string, src io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("ward container: create bootstrap binary %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, src); err != nil {
		return fmt.Errorf("ward container: write bootstrap binary %s: %w", path, err)
	}
	if err := f.Chmod(0o755); err != nil {
		return fmt.Errorf("ward container: chmod bootstrap binary %s: %w", path, err)
	}
	return nil
}

func sameOriginWardBootstrapURL(raw string) (string, error) {
	base, err := url.Parse(forgejoBaseURL)
	if err != nil {
		return "", fmt.Errorf("ward container: parse Forgejo bootstrap origin: %w", err)
	}
	resolved, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("ward container: parse resolved bootstrap download URL: %w", err)
	}
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host || resolved.User != nil {
		return "", errors.New("ward container: resolved bootstrap download URL must stay on the Forgejo origin")
	}
	return resolved.String(), nil
}

func resolveWardBootstrapTag(ctx context.Context, wardVersion, assetName string) (string, error) {
	tag := strings.TrimSpace(wardVersion)
	if tag != "" && tag != "dev" {
		return tag, nil
	}
	return resolveWardBootstrapLatestTag(ctx, assetName)
}

type forgejoReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type forgejoRelease struct {
	TagName    string                `json:"tag_name"`
	Draft      bool                  `json:"draft"`
	Prerelease bool                  `json:"prerelease"`
	Assets     []forgejoReleaseAsset `json:"assets"`
}

type releaseAssetsNotReadyError struct {
	tag   string
	asset string
	body  string
}

func (e *releaseAssetsNotReadyError) Error() string {
	msg := "ward container: release-assets-not-ready/deferred"
	if e.tag != "" {
		msg += ": " + e.tag
	}
	if e.asset != "" {
		msg += " missing " + e.asset
	}
	if body := strings.TrimSpace(e.body); body != "" {
		msg += ": " + body
	}
	return msg
}

func newReleaseAssetsNotReadyError(tag, asset, body string) error {
	return &releaseAssetsNotReadyError{tag: strings.TrimSpace(tag), asset: strings.TrimSpace(asset), body: strings.TrimSpace(body)}
}

func isReleaseAssetsNotReadyError(err error) bool {
	var target *releaseAssetsNotReadyError
	return errors.As(err, &target)
}

func bootstrapWardBinaryAssetName(arch string) string {
	return "ward-linux-" + arch
}

func resolveWardBootstrapLatestTag(ctx context.Context, assetName string) (string, error) {
	for page := 1; ; page++ {
		releases, err := fetchWardBootstrapReleasesPage(ctx, page)
		if err != nil {
			return "", err
		}
		if len(releases) == 0 {
			break
		}
		for _, release := range releases {
			if release.Draft || release.Prerelease {
				continue
			}
			if releaseHasBootstrapAsset(release, assetName) {
				if tag := strings.TrimSpace(release.TagName); tag != "" {
					return tag, nil
				}
			}
		}
	}
	return "", newReleaseAssetsNotReadyError("", assetName, "no published release carries the required bootstrap asset yet")
}

func fetchWardBootstrapReleasesPage(ctx context.Context, page int) ([]forgejoRelease, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/releases?limit=100&page=%d", forgejoBaseURL, wardBootstrapRepo, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ward container: list bootstrap releases page %d: %w", page, err)
	}
	if token := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ward container: list bootstrap releases page %d: %w", page, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("ward container: list bootstrap releases page %d: unexpected status %s: %s", page, resp.Status, strings.TrimSpace(string(body)))
	}
	var releases []forgejoRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("ward container: decode bootstrap releases page %d: %w", page, err)
	}
	return releases, nil
}

func releaseHasBootstrapAsset(release forgejoRelease, assetName string) bool {
	for _, asset := range release.Assets {
		if strings.TrimSpace(asset.Name) == assetName {
			return true
		}
	}
	return false
}

func bootstrapGOARCH() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH, nil
	default:
		return "", fmt.Errorf("ward container: unsupported bootstrap architecture %q", runtime.GOARCH)
	}
}
