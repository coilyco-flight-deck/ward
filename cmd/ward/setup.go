package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

const setupPlaceholderScope = "example-owner/example-repo"
const setupNextStep = "replace the placeholder scope in ~/.ward/config.yaml and restart warded"

type setupReport struct {
	sourceSummary      string
	resolvedSHA        string
	cachePath          string
	validatedSurfaces  []string
	phasePlan          string
	localConfigPath    string
	localConfigCreated bool
	dockerPrompt       string
	nextStep           string
}

var setupDockerReadiness = func(ctx context.Context) error {
	return leanRunner().checkDockerReady(ctx)
}

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Bootstrap ~/.ward/config.yaml and validate Ward launch defaults.",
		Description: strings.Join([]string{
			"setup validates Ward's typed launch defaults without building or",
			"replacing the ward binary, and it is not a hidden prerequisite for normal ward",
			"commands.",
			"",
			"It also creates a minimal first-run ~/.ward/config.yaml with placeholder",
			"values when the file is missing.",
			"",
			"AOSguard owns operator configuration and generated API surfaces.",
			"",
			"Phases: YAML/default validation -> launch checks -> host integration checks.",
		}, "\n"),
		Action: func(ctx context.Context, _ *cli.Command) error {
			report, err := runSetup(ctx)
			if err != nil {
				return err
			}
			printSetupReport(report)
			return nil
		},
	}
}

func runSetup(ctx context.Context) (setupReport, error) {
	_ = ctx
	report := setupReport{
		validatedSurfaces: []string{"harness adapters", "smart defaults", "YAML preferences"},
		phasePlan:         "YAML/default validation -> launch checks -> host integration checks",
		nextStep:          setupNextStep,
	}

	cfgPath, created, err := ensureLocalSetupConfig()
	if err != nil {
		return report, err
	}
	report.localConfigPath = cfgPath
	report.localConfigCreated = created

	report.sourceSummary = "typed defaults + YAML overrides"
	report.resolvedSHA = "built-in"
	report.cachePath = "none"
	if _, err := loadLaunchConfig(); err != nil {
		return report, fmt.Errorf("setup surface compile: harness adapters: %w", err)
	}
	if _, err := currentSmartDefaultsWithError(); err != nil {
		return report, fmt.Errorf("setup surface compile: smart defaults: %w", err)
	}
	if err := setupDockerReadiness(ctx); err != nil {
		report.dockerPrompt = err.Error()
		report.nextStep = "initialize Docker, then restart warded"
	}

	return report, nil
}

func ensureLocalSetupConfig() (string, bool, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, fmt.Errorf("setup config dir: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("setup config stat %s: %w", path, err)
	}
	body := []byte(setupLocalConfigYAML())
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", false, fmt.Errorf("setup config write %s: %w", path, err)
	}
	return path, true, nil
}

func setupLocalConfigYAML() string {
	return strings.TrimSpace(`# ward first setup config.
# Replace the placeholder scope before using warded director without --repo.
director:
  default-scope:
    - `+setupPlaceholderScope+`
`) + "\n"
}

func printSetupReport(report setupReport) {
	_, _ = fmt.Fprintf(os.Stdout, "ward setup: phases: %s\n", report.phasePlan)
	_, _ = fmt.Fprintf(os.Stdout, "ward setup: source=%s; sha=%s; cache=%s; validated=%s\n",
		report.sourceSummary, report.resolvedSHA, report.cachePath, strings.Join(report.validatedSurfaces, ", "))
	if strings.TrimSpace(report.localConfigPath) != "" {
		status := "present"
		if report.localConfigCreated {
			status = "created"
		}
		_, _ = fmt.Fprintf(os.Stdout, "ward setup: config=%s (%s)\n", report.localConfigPath, status)
	}
	if strings.TrimSpace(report.dockerPrompt) == "" {
		_, _ = fmt.Fprintln(os.Stdout, "ward setup: docker=ready")
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "ward setup: docker=needs-init\n%s\n", report.dockerPrompt)
	}
	if strings.TrimSpace(report.nextStep) != "" {
		_, _ = fmt.Fprintf(os.Stdout, "ward setup: next step: %s\n", report.nextStep)
	}
}
