package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

const setupValidatedSurfaces = "ops, exec, fleet, smart defaults"

type setupReport struct {
	sourceSummary     string
	resolvedSHA       string
	cachePath         string
	validatedSurfaces []string
	phasePlan         string
}

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Warm and validate runtime config surfaces.",
		Description: strings.Join([]string{
			"setup is the cache warmer / config doctor for the selected runtime",
			"config source. It pre-bakes and validates config surfaces without building or",
			"replacing the ward binary, and it is not a hidden prerequisite for normal ward",
			"commands.",
			"",
			"Point `WARD_CONFIG_REF` at the local setup output directly when you want a",
			"file form, for example `/path/to/ward-config.kdl` or `file:///path/to/ward-config.kdl`.",
			"",
			"Phases: config source -> auth/credential checks (stub) -> cache warm -> surface",
			"compile -> host integration checks (stub).",
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
		validatedSurfaces: []string{"ops", "exec", "fleet", "smart defaults"},
		phasePlan:         "config source -> auth/credential checks (stub) -> cache warm -> surface compile -> host integration checks (stub)",
	}

	rawRef := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	src, err := selectConfigSource()
	if err != nil {
		return report, err
	}

	if rawRef == "" {
		report.sourceSummary = configSourceSummary(rawRef, src)
		report.resolvedSHA = "embedded"
		report.cachePath = "embedded neutral default"
	} else {
		report.sourceSummary = configSourceSummary(rawRef, src)
		report.resolvedSHA = strings.TrimSpace(src.auditVersion)
		if report.resolvedSHA == "" {
			report.resolvedSHA = "unavailable"
		}
		report.cachePath = setupCachePath(rawRef)
	}

	if strings.TrimSpace(src.desc) != "" {
		if err := validateForgejoOpsOperational(src, false); err != nil {
			return report, fmt.Errorf("setup surface compile: ops: %w", err)
		}
	}
	if _, err := buildForgejoOpsFrom(src); err != nil {
		return report, fmt.Errorf("setup surface compile: ops: %w", err)
	}
	if err := mountWardKdlExecFrom(setupCompileRoot(), src, leanRunner()); err != nil {
		return report, fmt.Errorf("setup surface compile: exec: %w", err)
	}
	if _, err := loadFleetConfigFrom(src); err != nil {
		return report, fmt.Errorf("setup surface compile: fleet: %w", err)
	}
	if _, err := loadSmartDefaultsFrom(src); err != nil {
		return report, fmt.Errorf("setup surface compile: smart defaults: %w", err)
	}

	return report, nil
}

func setupCompileRoot() *cli.Command {
	return &cli.Command{
		Name: "ward",
		Commands: []*cli.Command{
			{Name: "version"},
			{Name: "setup"},
			{Name: "exec"},
			{Name: "git"},
			{Name: "audit"},
			{Name: "container"},
			{Name: "agent"},
			{Name: "agents"},
			{Name: "ops"},
		},
	}
}

func setupCachePath(rawRef string) string {
	if strings.TrimSpace(rawRef) == "" {
		return "embedded neutral default"
	}
	if localPath, ok, _ := resolveLocalConfigRef(rawRef); ok {
		return localPath
	}
	if dir, ok := strings.CutPrefix(rawRef, "file://"); ok {
		return resolvePathFromInvokeCWD(dir)
	}
	cr, err := parseConfigRef(rawRef)
	if err != nil {
		return rawRef
	}
	root, err := configBundleCacheRoot(os.Getenv)
	if err != nil {
		return rawRef
	}
	cache := filepath.Join(root, hashConfigRef(rawRef), "work")
	if cr.subpath != "." {
		cache = filepath.Join(cache, cr.subpath)
	}
	return cache
}

func printSetupReport(report setupReport) {
	_, _ = fmt.Fprintf(os.Stdout, "ward setup: phases: %s\n", report.phasePlan)
	_, _ = fmt.Fprintf(os.Stdout, "ward setup: source=%s; sha=%s; cache=%s; validated=%s\n",
		report.sourceSummary, report.resolvedSHA, report.cachePath, strings.Join(report.validatedSurfaces, ", "))
}
