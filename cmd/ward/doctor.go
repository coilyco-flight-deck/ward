package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/guardfile"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/urfave/cli/v3"
)

type doctorCheck struct {
	name string
	err  error
}

type doctorReport struct {
	sourceSummary string
	checks        []doctorCheck
}

func (r *doctorReport) add(name string, err error) {
	r.checks = append(r.checks, doctorCheck{name: name, err: err})
}

func (r doctorReport) failed() bool {
	for _, c := range r.checks {
		if c.err != nil {
			return true
		}
	}
	return false
}

func (r doctorReport) failureCount() int {
	n := 0
	for _, c := range r.checks {
		if c.err != nil {
			n++
		}
	}
	return n
}

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Validate the resolved runtime config and exit.",
		Description: `doctor validates the selected runtime config source, the resolved smart-defaults
policy, the fleet/roles data, and the guarded ops/executable surfaces that the launch
selected config controls. It is strict and exits non-zero on any failure.`,
		Action: func(ctx context.Context, _ *cli.Command) error {
			report, err := runDoctor(ctx)
			printDoctorReport(report)
			return err
		},
	}
}

func runDoctor(ctx context.Context) (doctorReport, error) {
	_ = ctx
	report := doctorReport{}

	rawRef := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	src, err := selectConfigSource()
	if err != nil {
		report.add("config source", err)
		return report, err
	}
	if rawRef == "" {
		report.sourceSummary = "baked neutral default (no external config source active)"
	} else {
		report.sourceSummary = "WARD_CONFIG_REF=" + rawRef
	}

	defs, err := loadSmartDefaultsFrom(src)
	if err != nil {
		report.add("smart defaults", err)
	} else if err := validateSmartDefaultsOperational(defs); err != nil {
		report.add("smart defaults", err)
	} else {
		report.add("smart defaults", nil)
		if err := validateRepoAuthorityOperational(defs); err != nil {
			report.add("repo authority", err)
		} else {
			report.add("repo authority", nil)
		}
	}

	fleet, ferr := loadFleetConfigFrom(src)
	if ferr != nil {
		report.add("fleet", ferr)
	} else if err := validateFleetOperational(fleet); err != nil {
		report.add("fleet", err)
	} else {
		report.add("fleet", nil)
	}

	if err := validateForgejoOpsOperational(src); err != nil {
		report.add("ops bundle", err)
	} else {
		report.add("ops bundle", nil)
	}

	if err := validateExecAssetsOperational(src); err != nil {
		report.add("exec bundle", err)
	} else {
		report.add("exec bundle", nil)
	}

	if report.failed() {
		return report, fmt.Errorf("ward doctor: %d check(s) failed", report.failureCount())
	}
	return report, nil
}

func printDoctorReport(report doctorReport) {
	_, _ = fmt.Fprintf(os.Stdout, "ward doctor: source=%s\n", report.sourceSummary)
	for _, check := range report.checks {
		if check.err != nil {
			_, _ = fmt.Fprintf(os.Stdout, "FAIL %s: %v\n", check.name, check.err)
			continue
		}
		_, _ = fmt.Fprintf(os.Stdout, "PASS %s\n", check.name)
	}
	if report.failed() {
		_, _ = fmt.Fprintf(os.Stdout, "ward doctor: %d failure(s)\n", report.failureCount())
		return
	}
	_, _ = fmt.Fprintln(os.Stdout, "ward doctor: all checks passed")
}

func validateSmartDefaultsOperational(defs smartDefaults) error {
	for repo, wf := range defs.agentWorkflowRepos {
		if containsExamplePlaceholder(repo) {
			return fmt.Errorf("smart defaults: agent-workflow repo %q still looks like a placeholder", repo)
		}
		if wf == "" {
			return fmt.Errorf("smart defaults: agent-workflow repo %q has an empty workflow (fail-closed)", repo)
		}
	}
	return nil
}

func validateRepoAuthorityOperational(defs smartDefaults) error {
	if len(defs.trustedOwners) == 0 {
		return fmt.Errorf("smart defaults: repo-authority needs at least one trusted-owner (fail-closed)")
	}
	for _, owner := range defs.trustedOwners {
		if containsExamplePlaceholder(owner) {
			return fmt.Errorf("smart defaults: repo-authority trusted-owner %q still looks like a placeholder", owner)
		}
	}
	if len(defs.repoAuthorityRules) == 0 {
		return fmt.Errorf("smart defaults: repo-authority needs at least one repo routing rule (fail-closed)")
	}
	for _, rule := range defs.repoAuthorityRules {
		if containsExamplePlaceholder(rule.Pattern) {
			return fmt.Errorf("smart defaults: repo-authority repo %q still looks like a placeholder", rule.Pattern)
		}
	}
	return nil
}

func validateFleetOperational(fleet fleetconfig.Fleet) error {
	if fleet.Defaults.Agent == "" {
		return fmt.Errorf("fleet: defaults.agent is required (fail-closed)")
	}
	if fleetAgentByName(fleet, fleet.Defaults.Agent).Name == "" {
		return fmt.Errorf("fleet: defaults.agent %q is not a recognized agent", fleet.Defaults.Agent)
	}
	if containsExamplePlaceholder(fleet.Defaults.Agent) {
		return fmt.Errorf("fleet: defaults.agent %q still looks like a placeholder", fleet.Defaults.Agent)
	}
	if fleet.Defaults.Attribution.Name == "" || fleet.Defaults.Attribution.Email == "" {
		return fmt.Errorf("fleet: defaults.attribution needs both name and email (fail-closed)")
	}
	if containsExamplePlaceholder(fleet.Defaults.Attribution.Name) {
		return fmt.Errorf("fleet: defaults.attribution.name %q still looks like a placeholder", fleet.Defaults.Attribution.Name)
	}
	if containsExamplePlaceholder(fleet.Defaults.Attribution.Email) {
		return fmt.Errorf("fleet: defaults.attribution.email %q still looks like a placeholder", fleet.Defaults.Attribution.Email)
	}
	requiredRoles := map[string]bool{"engineer": true, "director": true, "advisor": true}
	seenRoles := map[string]bool{}
	for _, role := range fleet.Roles {
		seenRoles[role.Name] = true
	}
	for role := range requiredRoles {
		if !seenRoles[role] {
			return fmt.Errorf("fleet: missing required role %q (fail-closed)", role)
		}
	}
	for _, role := range fleet.Roles {
		if (role.Name == "director" || role.Name == "advisor") && len(role.Guardfiles.List) == 0 && role.Guardfiles.Prefix == "" {
			return fmt.Errorf("fleet: role %q needs at least one guardfile binding (fail-closed)", role.Name)
		}
	}
	return nil
}

func validateForgejoOpsOperational(src configSource) error {
	gfBytes, err := fs.ReadFile(src.fsys, src.forgejoGuardfile)
	if err != nil {
		return fmt.Errorf("read ops guardfile %s: %w", src.forgejoGuardfile, err)
	}
	gf, err := guardfile.Parse(gfBytes)
	if err != nil {
		return err
	}
	if err := validateGuardfileOperational(gf); err != nil {
		return err
	}
	if _, err := buildForgejoOpsFrom(src); err != nil {
		return err
	}
	return nil
}

func validateExecAssetsOperational(src configSource) error {
	entries, err := fs.ReadDir(src.fsys, src.execDir)
	if err != nil {
		return fmt.Errorf("read exec guardfiles: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if src.execMixedDialects {
			if ok, _ := path.Match("ward-kdl.*.guardfile.kdl", e.Name()); !ok {
				continue
			}
		}
		names = append(names, e.Name())
	}
	for _, name := range names {
		gfBytes, err := fs.ReadFile(src.fsys, path.Join(src.execDir, name))
		if err != nil {
			return fmt.Errorf("read exec guardfile %s: %w", name, err)
		}
		if src.execMixedDialects && !isExecDialectGuardfile(gfBytes) {
			continue
		}
		gf, err := execverb.Parse(gfBytes)
		if err != nil {
			return fmt.Errorf("parse exec guardfile %s: %w", name, err)
		}
		if err := validateExecGuardfileOperational(gf); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := mountWardKdlExecFrom(setupCompileRoot(), src, leanRunner()); err != nil {
		return err
	}
	return nil
}

func validateGuardfileOperational(gf *guardfile.Guardfile) error {
	if containsExamplePlaceholder(gf.BaseURL) {
		return fmt.Errorf("guardfile base-url %q still looks like a placeholder", gf.BaseURL)
	}
	for _, vs := range gf.Auth.Value {
		if containsExamplePlaceholder(vs.Provider) {
			return fmt.Errorf("guardfile auth provider %q still looks like a placeholder", vs.Provider)
		}
		if containsExamplePlaceholder(vs.Address) {
			return fmt.Errorf("guardfile auth value %q still looks like a placeholder", vs.Address)
		}
	}
	for _, p := range gf.Auth.Params {
		for _, vs := range p.Value {
			if containsExamplePlaceholder(vs.Provider) {
				return fmt.Errorf("guardfile auth param %q provider %q still looks like a placeholder", p.Name, vs.Provider)
			}
			if containsExamplePlaceholder(vs.Address) {
				return fmt.Errorf("guardfile auth param %q value %q still looks like a placeholder", p.Name, vs.Address)
			}
		}
	}
	for _, r := range gf.Restrict {
		for _, glob := range r.Globs {
			if containsExamplePlaceholder(glob) {
				return fmt.Errorf("guardfile restrict %s glob %q still looks like a placeholder", r.Param, glob)
			}
		}
	}
	return nil
}

func validateExecGuardfileOperational(gf *execverb.Guardfile) error {
	if containsExamplePlaceholder(gf.Bin) {
		return fmt.Errorf("exec bin %q still looks like a placeholder", gf.Bin)
	}
	for _, tok := range gf.ArgvPrefix {
		if containsExamplePlaceholder(tok) {
			return fmt.Errorf("exec argv-prefix token %q still looks like a placeholder", tok)
		}
	}
	for _, env := range gf.Env {
		if containsExamplePlaceholder(env.Name) {
			return fmt.Errorf("exec env name %q still looks like a placeholder", env.Name)
		}
		if containsExamplePlaceholder(env.Provider) {
			return fmt.Errorf("exec env provider %q still looks like a placeholder", env.Provider)
		}
		if containsExamplePlaceholder(env.Address) {
			return fmt.Errorf("exec env address %q still looks like a placeholder", env.Address)
		}
	}
	for _, allow := range gf.Allow {
		if containsExamplePlaceholder(allow) {
			return fmt.Errorf("exec allow binary %q still looks like a placeholder", allow)
		}
	}
	for _, g := range gf.Grants {
		for _, tok := range g.Subcommand {
			if containsExamplePlaceholder(tok) {
				return fmt.Errorf("exec grant subcommand %q still looks like a placeholder", tok)
			}
		}
		for _, tok := range g.Argv {
			if containsExamplePlaceholder(tok) {
				return fmt.Errorf("exec grant argv token %q still looks like a placeholder", tok)
			}
		}
		if containsExamplePlaceholder(g.Bin) {
			return fmt.Errorf("exec grant bin %q still looks like a placeholder", g.Bin)
		}
	}
	for _, w := range gf.Whens {
		for _, tok := range w.SourceCmd {
			if containsExamplePlaceholder(tok) {
				return fmt.Errorf("exec when source command %q still looks like a placeholder", tok)
			}
		}
		for _, glob := range w.Patterns {
			if containsExamplePlaceholder(glob) {
				return fmt.Errorf("exec when glob %q still looks like a placeholder", glob)
			}
		}
	}
	for _, w := range gf.WrapWhens {
		for _, glob := range w.Patterns {
			if containsExamplePlaceholder(glob) {
				return fmt.Errorf("exec wrap-when glob %q still looks like a placeholder", glob)
			}
		}
	}
	return nil
}

func containsExamplePlaceholder(s string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(s)), "example")
}
