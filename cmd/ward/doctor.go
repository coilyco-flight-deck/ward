package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
)

type doctorCheck struct {
	name string
	err  error
}

type doctorReport struct {
	sourceSummary string
	checks        []doctorCheck
	warnings      []string
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
		Usage: "Validate typed defaults and supported YAML configuration.",
		Description: `doctor validates Ward's typed product defaults, fixed harness
adapters, and supported operator or repository YAML. AOSguard owns operator specs.`,
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
	report.sourceSummary = "typed defaults + YAML overrides"
	if hasDirectoryEntries(historicalRawAgentLogsDir()) {
		report.warnings = append(report.warnings, fmt.Sprintf(
			"historical raw agent archives remain at %s; Ward does not read, migrate, sanitize, or delete them",
			historicalRawAgentLogsDir(),
		))
	}

	defs, err := currentSmartDefaultsWithError()
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

	launch, launchErr := loadLaunchConfig()
	if launchErr != nil {
		report.add("harness adapters", launchErr)
	} else if err := validateLaunchConfig(launch); err != nil {
		report.add("harness adapters", err)
	} else {
		report.add("harness adapters", nil)
	}

	if report.failed() {
		return report, fmt.Errorf("ward doctor: %d check(s) failed", report.failureCount())
	}
	return report, nil
}

func printDoctorReport(report doctorReport) {
	_, _ = fmt.Fprintf(os.Stdout, "ward doctor: source=%s\n", report.sourceSummary)
	for _, warning := range report.warnings {
		_, _ = fmt.Fprintf(os.Stdout, "WARN %s\n", warning)
	}
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

func hasDirectoryEntries(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
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

func containsExamplePlaceholder(s string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(s)), "example")
}
