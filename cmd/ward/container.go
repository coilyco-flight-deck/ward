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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// container.go wires the `ward container` verb family and owns the docker side
// effects + host-side forgejo-token resolution. See docs/container.md.

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

// containerCommand is the `ward container` umbrella.
func containerCommand() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "Ephemeral, least-access dev containers (one per run) that clone fresh and carry a feature to merge.",
		Description: `container spins up a throwaway docker container per invocation to work a
single feature end to end - implement, commit, merge to main, resolve
conflicts, and push. The target repo is cloned fresh inside the container
(cached in a shared volume), never bind-mounted, so the host's repo tree is
untouched and any number of containers can run at once. Only the cwd is
mounted (read-only, for operating context); --aws opts the broader SSM read
surface in. See docs/container.md.`,
		Commands: []*cli.Command{
			containerUpCommand(),
			containerExecCommand(),
			containerReapCommand(),
			containerDownCommand(),
			containerListCommand(),
			containerAgentPreCommitCommand(),
		},
	}
}

func containerUpCommand() *cli.Command {
	return &cli.Command{
		Name:      "up",
		Usage:     "Start a new container and run the agent against a fresh clone of the target repo.",
		ArgsUsage: "[owner/name | clone-url]   (omit to infer from cwd's git remote)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "mode", Value: "claude", Usage: "agent + context level: claude|codex|qwen|goose (progressively less context)"},
			&cli.StringFlag{Name: "branch", Usage: "feature branch to create/checkout inside the clone"},
			&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
			&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
			&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
			&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
			&cli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "run detached instead of interactive"},
			&cli.BoolFlag{Name: "print", Usage: "print the docker invocation and exit; resolve no secrets, run nothing"},
			&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.up",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runContainerUp(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runContainerUp resolves the target, builds the plan, and runs docker. In
// --print mode it resolves no secrets and runs nothing.
func (r *Runner) runContainerUp(ctx context.Context, c *cli.Command) error {
	mode, err := parseMode(c.String("mode"))
	if err != nil {
		return err
	}
	repo, cwd, err := r.resolveTarget(ctx, c.Args().First())
	if err != nil {
		return err
	}
	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// A detached run leaves its assets for the next sweep (it cannot delete the
	// still-mounted dir on return); an attached run cleans up on exit.
	if !c.Bool("detach") {
		defer cleanupAssets()
	}

	plan := buildUpPlan(c, repo, mode, cwd, assetsDir, nil)

	if c.Bool("print") {
		return printPlan(c, plan)
	}
	if !c.Bool("no-pull") {
		if perr := r.Runner.Exec(ctx, "docker", "pull", plan.Image); perr != nil {
			fmt.Fprintf(os.Stderr, "ward container: image pull failed (%v); trying the local image\n", perr)
		}
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()

	return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
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
	default:
		return agentCreds{}
	}
}

// buildUpPlan assembles the pure plan from parsed flags and resolved inputs.
// agentArgs seed the in-container agent's argv; `container up` passes nil.
func buildUpPlan(c *cli.Command, repo targetRepo, mode containerMode, cwd, assetsDir string, agentArgs []string) upPlan {
	wardSrc := c.String("ward-source")
	awsHome := ""
	if c.Bool("aws") {
		awsHome = filepath.Join(homeDir(), ".aws")
	}
	return upPlan{
		Image:          imageRef(c.String("image"), c.String("tag")),
		Name:           containerName(repo, randHex(4)),
		Repo:           repo,
		Mode:           mode,
		Branch:         c.String("branch"),
		ForgejoBase:    forgejoBaseURL,
		HostCwd:        cwd,
		Mounts:         leastAccessMounts(cwd, mountOpts{AssetsDir: assetsDir, AWSHome: awsHome, WardSource: wardSrc}),
		Interactive:    !c.Bool("detach"),
		TTY:            !c.Bool("detach") && terminalAttached(),
		WardVersion:    Version,
		WardFromSource: wardSrc != "",
		AgentArgs:      agentArgs,
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

// writeTokenEnvFile resolves the forgejo token (+ optional base64'd agent creds)
// into a private 0600 --env-file, so none enters argv/audit. Caller removes it.
func (r *Runner) writeTokenEnvFile(ctx context.Context, creds agentCreds) (path string, cleanup func(), err error) {
	out, err := r.Runner.Capture(ctx, "aws", "ssm", "get-parameter",
		"--name", forgejoTokenSSMPath, "--with-decryption",
		"--query", "Parameter.Value", "--output", "text")
	if err != nil {
		return "", func() {}, fmt.Errorf("ward container: resolve %s from SSM (host needs aws creds): %w", forgejoTokenSSMPath, err)
	}
	token := strings.TrimSpace(string(out))
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

func containerExecCommand() *cli.Command {
	return &cli.Command{
		Name:            "exec",
		Usage:           "Run a command inside a running ward container: ward container exec <name> -- <cmd...>",
		ArgsUsage:       "<name> -- <cmd...>",
		SkipFlagParsing: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.exec",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					name, rest := splitExecArgs(cmd.Args().Slice())
					if name == "" || len(rest) == 0 {
						return fmt.Errorf("ward container exec: usage: ward container exec <name> -- <cmd...>")
					}
					return r.Runner.Exec(ctx, "docker", dockerExecArgv(name, terminalAttached(), rest)...)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// splitExecArgs peels the container name off the front and the command after
// the first `--`.
func splitExecArgs(argv []string) (name string, cmd []string) {
	if len(argv) == 0 {
		return "", nil
	}
	name = argv[0]
	for i, a := range argv[1:] {
		if a == "--" {
			return name, argv[1+i+1:]
		}
	}
	return name, argv[1:]
}

func containerDownCommand() *cli.Command {
	return &cli.Command{
		Name:      "down",
		Usage:     "Stop and remove a ward container (the shared gitcache volume is kept).",
		ArgsUsage: "<name>",
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.down",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					name := cmd.Args().First()
					if name == "" {
						return fmt.Errorf("ward container down: name the container (see `ward container ls`)")
					}
					return r.Runner.Exec(ctx, "docker", dockerDownArgv(name)...)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func containerListCommand() *cli.Command {
	return &cli.Command{
		Name:    "ls",
		Aliases: []string{"list"},
		Usage:   "List ward-managed containers.",
		Flags:   []cli.Flag{&cli.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "include stopped containers"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.ls",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.Runner.Exec(ctx, "docker", dockerListArgv(cmd.Bool("all"))...)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// printPlan emits the docker invocation for --print: the token is never
// resolved, so the env-file appears as a redacted placeholder.
func printPlan(c *cli.Command, p upPlan) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	pull := fmt.Sprintf("docker pull %s\n", p.Image)
	if c.Bool("no-pull") {
		pull = fmt.Sprintf("# pull skipped (--no-pull); image: %s\n", p.Image)
	}
	run := fmt.Sprintf("docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, pull+run)
	return err
}

// randHex returns n random bytes as a lowercase hex string, the unique suffix
// that lets repeated `ward container up` calls coexist.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// rand.Read never fails on supported platforms; fall back so a name is
		// still produced rather than panicking a dev command.
		return "00000000"[:2*n]
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
