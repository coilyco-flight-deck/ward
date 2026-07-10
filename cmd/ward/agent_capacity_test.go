package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func engineerCountDockerStub(t *testing.T, count int) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ]; then\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(fmt.Sprintf("engineer-%02d", i+1)))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("printf '%s\\n' \"unexpected docker args: $*\" >&2\n")
	b.WriteString("exit 1\n")
	if err := os.WriteFile(stub, []byte(b.String()), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write docker stub: %v", err)
	}
	return stub
}

func TestEngineerContainerLimitBelowAndAtLimit(t *testing.T) {
	limit := engineerContainerLimitDefault()
	t.Run("below limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit-1))
		if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer"); err != nil {
			t.Fatalf("enforceEngineerContainerLimit below limit: %v", err)
		}
	})

	t.Run("at limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit))
		err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer")
		if err == nil {
			t.Fatal("enforceEngineerContainerLimit at limit: want error, got nil")
		}
		var capErr *engineerCapacityError
		if !errors.As(err, &capErr) {
			t.Fatalf("enforceEngineerContainerLimit at limit returned %T, want *engineerCapacityError", err)
		}
		if !isEngineerCapacityError(err) {
			t.Fatal("enforceEngineerContainerLimit at limit should classify as engineer capacity backpressure")
		}
		for _, want := range []string{
			"global engineer limit is reached",
			"running",
			fmt.Sprintf("limit %d", limit),
			"ward agent reap",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("enforceEngineerContainerLimit at limit missing %q: %v", want, err)
			}
		}
	})
}

func TestEngineerContainerLimitFromBundleOverride(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "1h"
    agent-reservation-recheck-max "15s"
    agent-reap-idle "1h"
    agent-reap-max-cpu "5.0"
    engineer-container-limit "15"
    director-max-parallel "10"
    director-limit "50"
    director-poll-interval "30s"
    reviewer-timeout "8m"
    config-bundle-ttl "600s"
    container-assets-ttl "1h"
    container-read-only-extra-repo-ttl "24h"
    container-reap-keep "10"
    agent-workflow default=direct-main {
        repo "coilyco-flight-deck/ward" workflow=pull-requests-and-merge
    }
}
`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner coilysiren
        repo "coilyco-flight-deck/*" forge=forgejo
    }
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(reposBody), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	if got := engineerContainerLimitDefault(); got != 15 {
		t.Fatalf("engineerContainerLimitDefault() = %d, want 15", got)
	}

	r, _, _ := bufRunner(engineerCountDockerStub(t, 14))
	if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer"); err != nil {
		t.Fatalf("enforceEngineerContainerLimit below overridden limit: %v", err)
	}

	r, _, _ = bufRunner(engineerCountDockerStub(t, 15))
	err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer")
	if err == nil {
		t.Fatal("enforceEngineerContainerLimit at overridden limit: want error, got nil")
	}
	if !strings.Contains(err.Error(), "limit 15") {
		t.Fatalf("enforceEngineerContainerLimit overridden limit error = %v", err)
	}
}

func TestLaunchAgentContainerRejectsAtLimitWithoutReservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r, _, _ := bufRunner(engineerCountDockerStub(t, engineerContainerLimitDefault()))
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 884}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatalf("reservation unexpectedly existed before the launch")
	}

	err = r.launchAgentContainer(context.Background(), &cli.Command{}, modeClaude, "engineer", resolvedWork{
		Ref:   ref,
		Title: "limit test",
		Seed:  "seed",
	}, "")
	if err == nil {
		t.Fatal("launchAgentContainer at limit: want error, got nil")
	}
	if !strings.Contains(err.Error(), "global engineer limit is reached") {
		t.Fatalf("launchAgentContainer at limit error = %v", err)
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatal("launchAgentContainer at limit should not reserve the issue")
	}
}
