package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// container.go wires the `ward container` verb family and owns the docker side
// effects + host-side forgejo-token resolution. See docs/container.md.

//go:embed containerassets/entrypoint.sh containerassets/AGENTS.container.md
var containerAssets embed.FS

// forgejoTokenSSMPath is the SSM parameter NAME for the git-over-HTTPS push
// token (user coilysiren), resolved on the host and never entering the image.

// #nosec G101 -- this is an SSM parameter path, not an embedded secret.
const forgejoTokenSSMPath = "/forgejo/api-token"

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
			containerDownCommand(),
			containerListCommand(),
		},
	}
}

func containerUpCommand() *cli.Command {
	return &cli.Command{
		Name:      "up",
		Usage:     "Start a new container and run the agent against a fresh clone of the target repo.",
		ArgsUsage: "[owner/name | clone-url]   (omit to infer from cwd's git remote)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "mode", Value: "claude", Usage: "agent + context level: claude|codex|qwen (progressively less context)"},
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
	defer cleanupAssets()

	plan := buildUpPlan(c, repo, mode, cwd, assetsDir)

	if c.Bool("print") {
		return printPlan(c, plan)
	}
	if !c.Bool("no-pull") {
		if perr := r.Runner.Exec(ctx, "docker", "pull", plan.Image); perr != nil {
			fmt.Fprintf(os.Stderr, "ward container: image pull failed (%v); trying the local image\n", perr)
		}
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx)
	if err != nil {
		return err
	}
	defer cleanupEnv()

	return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
}

// buildUpPlan assembles the pure plan from parsed flags and resolved inputs.
func buildUpPlan(c *cli.Command, repo targetRepo, mode containerMode, cwd, assetsDir string) upPlan {
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
		WardVersion:    Version,
		WardFromSource: wardSrc != "",
	}
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

// writeTokenEnvFile resolves the forgejo token on the host into a private
// (0600) --env-file, so it never enters argv/audit; the caller removes it.
func (r *Runner) writeTokenEnvFile(ctx context.Context) (path string, cleanup func(), err error) {
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
					return r.Runner.Exec(ctx, "docker", dockerExecArgv(name, true, rest)...)
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

// writeContainerAssets materializes the embedded entrypoint + doctrine into a
// per-run tmp dir the container mounts read-only at /opt/ward.
func writeContainerAssets() (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "ward-container-assets-*")
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
