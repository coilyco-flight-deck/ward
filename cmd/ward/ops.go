package main

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/guardfile"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/specverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/valuesource"
	"github.com/urfave/cli/v3"
)

// ops.go mounts the ward-kdl forgejo guardfile runtime inside the ward binary
// as `ward ops forgejo` (ward#92). See docs/ops-forgejo.md.

// opsAssets embeds the forgejo REST guardfile + spec lock plus the exec-dialect
// admin/doctor guardfile (ward#81). See docs/ops-forgejo-in-ward.md.

//go:embed opsassets/forgejo.guardfile.kdl opsassets/forgejo.swagger.lock.json
//go:embed opsassets/forgejo-admin.guardfile.kdl
var opsAssets embed.FS

// Embed paths named once so the runtime mount and the drift test agree; the
// admin path is the exec-dialect remote-exec slice (ward#81).
const (
	opsForgejoGuardfilePath      = "opsassets/forgejo.guardfile.kdl"
	opsForgejoSpecLockPath       = "opsassets/forgejo.swagger.lock.json"
	opsForgejoAdminGuardfilePath = "opsassets/forgejo-admin.guardfile.kdl"
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

	forgejo, err := specverb.Build(specverb.Config{
		Guardfile: gf,
		Spec:      spec,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		// The guardfile's `value ssm` auth resolves through ward's audited aws
		// runner (no AWS SDK), lazily - mount and --dry-run never touch SSM.
		Providers: map[string]valuesource.Provider{
			"ssm": r.ssmValueResolver,
		},
	})
	if err != nil {
		return nil, err
	}

	// Graft the exec-dialect admin/doctor remote-exec slice onto the same
	// forgejo group, so both transports share one operator verb (ward#81).
	if err := graftForgejoAdminExec(forgejo, r); err != nil {
		return nil, err
	}
	return forgejo, nil
}

// graftForgejoAdminExec appends the exec-dialect guardfile's built admin/doctor
// subtrees onto forgejo. See docs/ops-forgejo-admin.md.
func graftForgejoAdminExec(forgejo *cli.Command, r *Runner) error {
	gfBytes, err := opsAssets.ReadFile(opsForgejoAdminGuardfilePath)
	if err != nil {
		return fmt.Errorf("read embedded admin guardfile: %w", err)
	}
	gf, err := execverb.Parse(gfBytes)
	if err != nil {
		return fmt.Errorf("parse admin guardfile: %w", err)
	}
	group, err := execverb.Build(execverb.Config{
		Guardfile: gf,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		// Run is nil: the exec-dialect leaves shell out to the real `ssh`
		// transport. The wrapped binary owns its own credentials, so no SSM auth.
	})
	if err != nil {
		return fmt.Errorf("build admin guardfile: %w", err)
	}
	forgejo.Commands = append(forgejo.Commands, group.Commands...)
	return nil
}

// ssmValueResolver fetches an SSM SecureString through ward's audited aws runner,
// parameterized by the guardfile-supplied path (vs forgejoAPIToken's hardcoded one).
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
