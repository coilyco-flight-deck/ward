package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
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
	allowPlaceholders := rawRef == "" || strings.TrimSpace(os.Getenv(doctorAllowPlaceholdersEnv)) != ""
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
	} else if err := validateSmartDefaultsOperational(defs, allowPlaceholders); err != nil {
		report.add("smart defaults", err)
	} else {
		report.add("smart defaults", nil)
		if err := validateRepoAuthorityOperational(defs, allowPlaceholders); err != nil {
			report.add("repo authority", err)
		} else {
			report.add("repo authority", nil)
		}
	}

	fleet, ferr := loadFleetConfigFrom(src)
	if ferr != nil {
		report.add("fleet", ferr)
	} else if err := validateFleetOperational(fleet, allowPlaceholders); err != nil {
		report.add("fleet", err)
	} else {
		report.add("fleet", nil)
	}

	if err := validateForgejoOpsOperational(src, allowPlaceholders); err != nil {
		report.add("ops bundle", err)
	} else {
		report.add("ops bundle", nil)
	}

	if err := validateExecAssetsOperational(src, allowPlaceholders); err != nil {
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

const doctorAllowPlaceholdersEnv = "WARD_DOCTOR_ALLOW_PLACEHOLDERS"

func validateSmartDefaultsOperational(defs smartDefaults, allowPlaceholders bool) error {
	for repo, wf := range defs.agentWorkflowRepos {
		if !allowPlaceholders && containsExamplePlaceholder(repo) {
			return fmt.Errorf("smart defaults: agent-workflow repo %q still looks like a placeholder", repo)
		}
		if wf == "" {
			return fmt.Errorf("smart defaults: agent-workflow repo %q has an empty workflow (fail-closed)", repo)
		}
	}
	return nil
}

func validateRepoAuthorityOperational(defs smartDefaults, allowPlaceholders bool) error {
	if len(defs.trustedOwners) == 0 {
		return fmt.Errorf("smart defaults: repo-authority needs at least one trusted-owner (fail-closed)")
	}
	for _, owner := range defs.trustedOwners {
		if !allowPlaceholders && containsExamplePlaceholder(owner) {
			return fmt.Errorf("smart defaults: repo-authority trusted-owner %q still looks like a placeholder", owner)
		}
	}
	if len(defs.repoAuthorityRules) == 0 {
		return fmt.Errorf("smart defaults: repo-authority needs at least one repo routing rule (fail-closed)")
	}
	for _, rule := range defs.repoAuthorityRules {
		if !allowPlaceholders && containsExamplePlaceholder(rule.Pattern) {
			return fmt.Errorf("smart defaults: repo-authority repo %q still looks like a placeholder", rule.Pattern)
		}
	}
	return nil
}

func validateFleetOperational(fleet fleetconfig.Fleet, allowPlaceholders bool) error {
	if err := validateFleetDefaultsOperational(fleet, allowPlaceholders); err != nil {
		return err
	}
	return validateFleetRolesOperational(fleet)
}

func validateFleetDefaultsOperational(fleet fleetconfig.Fleet, allowPlaceholders bool) error {
	if fleet.Defaults == (fleetconfig.Defaults{}) {
		return nil
	}
	if fleet.Defaults.Agent == "" {
		return fmt.Errorf("fleet: defaults.agent is required (fail-closed)")
	}
	if fleetAgentByName(fleet, fleet.Defaults.Agent).Name == "" {
		return fmt.Errorf("fleet: defaults.agent %q is not a recognized agent", fleet.Defaults.Agent)
	}
	if !allowPlaceholders && containsExamplePlaceholder(fleet.Defaults.Agent) {
		return fmt.Errorf("fleet: defaults.agent %q still looks like a placeholder", fleet.Defaults.Agent)
	}
	if fleet.Defaults.Attribution.Name == "" || fleet.Defaults.Attribution.Email == "" {
		return fmt.Errorf("fleet: defaults.attribution needs both name and email (fail-closed)")
	}
	if !allowPlaceholders && containsExamplePlaceholder(fleet.Defaults.Attribution.Name) {
		return fmt.Errorf("fleet: defaults.attribution.name %q still looks like a placeholder", fleet.Defaults.Attribution.Name)
	}
	if !allowPlaceholders && containsExamplePlaceholder(fleet.Defaults.Attribution.Email) {
		return fmt.Errorf("fleet: defaults.attribution.email %q still looks like a placeholder", fleet.Defaults.Attribution.Email)
	}
	return nil
}

func validateFleetRolesOperational(fleet fleetconfig.Fleet) error {
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

func validateForgejoOpsOperational(src configSource, allowPlaceholders bool) error {
	file, node, err := loadForgejoGuardfileNodeFrom(src)
	if err != nil {
		return err
	}
	if !allowPlaceholders {
		if err := validateNoPlaceholderSentinels(node); err != nil {
			return fmt.Errorf("%s: %w; %s", setupBundlePathOnDisk(src, file.path), err, setupPlaceholderRemediation())
		}
	}
	if _, err := buildForgejoOpsFrom(src); err != nil {
		return err
	}
	return nil
}

func validateExecAssetsOperational(src configSource, allowPlaceholders bool) error {
	files, err := loadBundleKDLFiles(src)
	if err != nil {
		return fmt.Errorf("read exec guardfiles: %w", err)
	}
	for _, file := range files {
		if src.execMixedDialects && !isExecDialectGuardfile(file.src) {
			continue
		}
		if err := validateExecAssetOperational(file, allowPlaceholders); err != nil {
			return err
		}
	}
	return mountWardKdlExecFrom(setupCompileRoot(), src, leanRunner())
}

func validateExecAssetOperational(file bundleKDLFile, allowPlaceholders bool) error {
	gf, err := execverb.Parse(file.src)
	if err != nil {
		return fmt.Errorf("parse exec guardfile %s: %w", file.path, err)
	}
	if err := validateExecGuardfileOperational(gf, allowPlaceholders); err != nil {
		return fmt.Errorf("%s: %w", file.path, err)
	}
	return nil
}

func validateExecGuardfileOperational(gf *execverb.Guardfile, allowPlaceholders bool) error {
	if !allowPlaceholders && containsExamplePlaceholder(gf.Bin) {
		return fmt.Errorf("exec bin %q still looks like a placeholder", gf.Bin)
	}
	for _, tok := range gf.ArgvPrefix {
		if !allowPlaceholders && containsExamplePlaceholder(tok) {
			return fmt.Errorf("exec argv-prefix token %q still looks like a placeholder", tok)
		}
	}
	if err := validateExecEnvOperational(gf, allowPlaceholders); err != nil {
		return err
	}
	if err := validateExecAllowOperational(gf, allowPlaceholders); err != nil {
		return err
	}
	if err := validateExecGrantsOperational(gf, allowPlaceholders); err != nil {
		return err
	}
	return validateExecWhensOperational(gf, allowPlaceholders)
}

func validateExecEnvOperational(gf *execverb.Guardfile, allowPlaceholders bool) error {
	for _, env := range gf.Env {
		if !allowPlaceholders && containsExamplePlaceholder(env.Name) {
			return fmt.Errorf("exec env name %q still looks like a placeholder", env.Name)
		}
		if !allowPlaceholders && containsExamplePlaceholder(env.Provider) {
			return fmt.Errorf("exec env provider %q still looks like a placeholder", env.Provider)
		}
		if !allowPlaceholders && containsExamplePlaceholder(env.Address) {
			return fmt.Errorf("exec env address %q still looks like a placeholder", env.Address)
		}
	}
	return nil
}

func validateExecAllowOperational(gf *execverb.Guardfile, allowPlaceholders bool) error {
	for _, allow := range gf.Allow {
		if !allowPlaceholders && containsExamplePlaceholder(allow) {
			return fmt.Errorf("exec allow binary %q still looks like a placeholder", allow)
		}
	}
	return nil
}

func validateExecGrantsOperational(gf *execverb.Guardfile, allowPlaceholders bool) error {
	for _, g := range gf.Grants {
		for _, tok := range g.Subcommand {
			if !allowPlaceholders && containsExamplePlaceholder(tok) {
				return fmt.Errorf("exec grant subcommand %q still looks like a placeholder", tok)
			}
		}
		for _, tok := range g.Argv {
			if !allowPlaceholders && containsExamplePlaceholder(tok) {
				return fmt.Errorf("exec grant argv token %q still looks like a placeholder", tok)
			}
		}
		if !allowPlaceholders && containsExamplePlaceholder(g.Bin) {
			return fmt.Errorf("exec grant bin %q still looks like a placeholder", g.Bin)
		}
	}
	return nil
}

func validateExecWhensOperational(gf *execverb.Guardfile, allowPlaceholders bool) error {
	for _, w := range gf.Whens {
		for _, tok := range w.SourceCmd {
			if !allowPlaceholders && containsExamplePlaceholder(tok) {
				return fmt.Errorf("exec when source command %q still looks like a placeholder", tok)
			}
		}
		for _, glob := range w.Patterns {
			if !allowPlaceholders && containsExamplePlaceholder(glob) {
				return fmt.Errorf("exec when glob %q still looks like a placeholder", glob)
			}
		}
	}
	for _, w := range gf.WrapWhens {
		for _, glob := range w.Patterns {
			if !allowPlaceholders && containsExamplePlaceholder(glob) {
				return fmt.Errorf("exec wrap-when glob %q still looks like a placeholder", glob)
			}
		}
	}
	return nil
}

func containsExamplePlaceholder(s string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(s)), "example")
}
