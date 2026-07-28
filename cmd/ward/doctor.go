package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

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
		Usage: "Validate embedded native policy and exit.",
		Description: `doctor validates Ward's embedded agent policy: smart defaults,
fleet/roles data, and native launch assets. AOSguard owns operator specs.`,
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

	allowPlaceholders := strings.TrimSpace(os.Getenv(doctorAllowPlaceholdersEnv)) != ""
	src := bakedConfigSource()
	report.sourceSummary = src.sourceDesc()

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
	if defs.routeIntakeRepo == (targetRepo{}) {
		return fmt.Errorf("smart defaults: route-intake-repo is required for route mode (fail-closed)")
	}
	if !allowPlaceholders && containsExamplePlaceholder(defs.routeIntakeRepo.slug()) {
		return fmt.Errorf("smart defaults: route-intake-repo %q still looks like a placeholder", defs.routeIntakeRepo.slug())
	}
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
	if defs.routeIntakeRepo != (targetRepo{}) && !slices.Contains(defs.trustedOwners, defs.routeIntakeRepo.Owner) {
		return fmt.Errorf("smart defaults: route-intake-repo %q owner is not in repo-authority trusted-owner (fail-closed)", defs.routeIntakeRepo.slug())
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
	requiredRoles := map[string]bool{"engineer": true, "director": true}
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
		if role.Name == "director" && len(role.Guardfiles.List) == 0 && role.Guardfiles.Prefix == "" {
			return fmt.Errorf("fleet: role %q needs at least one guardfile binding (fail-closed)", role.Name)
		}
	}
	return nil
}

func containsExamplePlaceholder(s string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(s)), "example")
}
