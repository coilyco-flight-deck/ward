package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/guardfile"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/specverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/valuesource"
	"github.com/urfave/cli/v3"
)

// ops.go mounts the ward-kdl forgejo guardfile runtime inside the ward binary
// as `ward ops forgejo` (ward#92). See docs/ops-forgejo.md.

// opsAssets embeds copies of the cmd/ward-kdl forgejo guardfile + spec lock
// (opsassets_test.go guards drift). See docs/ops-forgejo-in-ward.md.

//go:embed opsassets/forgejo.guardfile.kdl opsassets/forgejo.swagger.lock.json
var opsAssets embed.FS

// opsForgejoGuardfilePath / opsForgejoSpecLockPath name the embed paths once, so
// the runtime mount and the drift test agree.
const (
	opsForgejoGuardfilePath = "opsassets/forgejo.guardfile.kdl"
	opsForgejoSpecLockPath  = "opsassets/forgejo.swagger.lock.json"
)

// opsCommand is the `ops` umbrella: operator verbs run by cli-guard's specverb
// runtime. A build error degrades to a leaf that surfaces it on invocation.
func opsCommand() *cli.Command {
	forgejo, err := buildForgejoOps()
	if err != nil {
		forgejo = &cli.Command{
			Name:  "forgejo",
			Usage: "guarded Forgejo REST surface (unavailable)",
			Action: func(context.Context, *cli.Command) error {
				return fmt.Errorf("ward ops forgejo: guardfile runtime failed to mount: %w", err)
			},
		}
	}
	return &cli.Command{
		Name:     "ops",
		Usage:    "operator verbs routed through the ward-kdl guardfile runtime",
		Commands: []*cli.Command{forgejo},
	}
}

// buildForgejoOps parses the embedded guardfile + spec lock and specverb.Builds
// the `forgejo` group, audited through ward and SSM-resolved via the aws runner.
func buildForgejoOps() (*cli.Command, error) {
	r := leanRunner()

	gfBytes, err := opsAssets.ReadFile(opsForgejoGuardfilePath)
	if err != nil {
		return nil, fmt.Errorf("read embedded guardfile: %w", err)
	}
	gf, err := guardfile.Parse(gfBytes)
	if err != nil {
		return nil, fmt.Errorf("parse guardfile: %w", err)
	}
	spec, err := opsAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded spec lock: %w", err)
	}

	return specverb.Build(specverb.Config{
		Guardfile: gf,
		Spec:      spec,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		// The guardfile's `value ssm` auth resolves through forgejoTokenResolver,
		// lazily - mount and --dry-run never touch the token source.
		Providers: map[string]valuesource.Provider{
			"ssm": r.forgejoTokenResolver,
		},
	})
}

// forgejoTokenResolver resolves the Forgejo bot token: the baked $FORGEJO_TOKEN in
// a container (no SSM/aws), else the coilyco-ops bot token from SSM on a host.
func (r *Runner) forgejoTokenResolver(ctx context.Context, ssmPath string) (string, error) {
	if tok := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); tok != "" {
		return tok, nil
	}
	return r.ssmValueResolver(ctx, ssmPath)
}

// ssmValueResolver fetches an SSM SecureString through ward's audited aws runner,
// parameterized by the guardfile-supplied path.
func (r *Runner) ssmValueResolver(ctx context.Context, ssmPath string) (string, error) {
	out, err := r.Runner.Capture(ctx, "aws",
		"ssm", "get-parameter",
		"--name", ssmPath,
		"--with-decryption",
		"--query", "Parameter.Value",
		"--output", "text",
	)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", ssmPath, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("%s resolved to empty value", ssmPath)
	}
	return v, nil
}
