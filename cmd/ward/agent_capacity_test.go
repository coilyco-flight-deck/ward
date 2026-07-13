package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

func engineerRepoAndGlobalCountDockerStub(t *testing.T, repo string, repoCount, globalCount int) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ] && [ \"$8\" = --filter ] && [ \"$9\" = label=ward.repo=" + repo + " ]; then\n")
	for i := 0; i < repoCount; i++ {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(fmt.Sprintf("engineer-%02d", i+1)))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ]; then\n")
	for i := 0; i < globalCount; i++ {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(fmt.Sprintf("engineer-%02d", i+1)))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("printf '%s\\n' \"unexpected docker args: $*\" >&2\n")
	b.WriteString("exit 1\n")
	if err := os.WriteFile(stub, []byte(b.String()), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

func TestEngineerContainerLimitBelowAndAtLimit(t *testing.T) {
	limit := engineerContainerLimitDefault()
	t.Run("below limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit-1))
		if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false); err != nil {
			t.Fatalf("enforceEngineerContainerLimit below limit: %v", err)
		}
	})

	t.Run("at limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit))
		err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false)
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

// TestEngineerContainerLimitOverrideCapacity covers ward#1045: --override-capacity
// grants one loud launch past the OOM ceiling and never stacks past limit+1.
func TestEngineerContainerLimitOverrideCapacity(t *testing.T) {
	limit := engineerContainerLimitDefault()

	t.Run("at limit with override launches loudly", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit))
		var err error
		stderr := captureTestStderr(t, func() {
			err = r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", true)
		})
		if err != nil {
			t.Fatalf("enforceEngineerContainerLimit at limit with override: %v", err)
		}
		for _, want := range []string{
			"WARNING",
			fmt.Sprintf("(%d/%d)", limit+1, limit),
			"OOM",
			"--override-capacity",
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("override warning missing %q: %q", want, stderr)
			}
		}
	})

	t.Run("already over the ceiling refuses even with override", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit+1))
		var err error
		stderr := captureTestStderr(t, func() {
			err = r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", true)
		})
		if err == nil {
			t.Fatal("enforceEngineerContainerLimit over limit with override: want error, got nil")
		}
		if !isEngineerCapacityError(err) {
			t.Fatalf("over-limit override refusal should classify as capacity backpressure: %T %v", err, err)
		}
		if !strings.Contains(stderr, "exactly one launch past the ceiling") {
			t.Errorf("over-limit override refusal should explain the one-launch bound: %q", stderr)
		}
	})

	t.Run("below limit with override stays quiet", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit-1))
		var err error
		stderr := captureTestStderr(t, func() {
			err = r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", true)
		})
		if err != nil {
			t.Fatalf("enforceEngineerContainerLimit below limit with override: %v", err)
		}
		if strings.Contains(stderr, "WARNING") {
			t.Errorf("below-limit override should not warn: %q", stderr)
		}
	})
}

func TestEngineerContainerLimitFromBundleOverride(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "3h"
    agent-reservation-recheck-max "15s"
    agent-reap-idle "1h"
    agent-reap-max-cpu "5.0"
    engineer-container-limit "15"
    engineer-repo-working-limit "3"
    engineer-open-pr-branch-limit "6"
    director-max-parallel "10"
    director-limit "50"
    director-poll-interval "30s"
    reviewer-timeout "8m"
    config-bundle-ttl "600s"
    container-assets-ttl "1h"
    container-read-only-extra-repo-ttl "24h"
    container-reap-keep "10"
}
workflow default=merge-remote-main {
    repo "coilyco-flight-deck/ward" workflow=pull-request-and-merge
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
	if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false); err != nil {
		t.Fatalf("enforceEngineerContainerLimit below overridden limit: %v", err)
	}

	r, _, _ = bufRunner(engineerCountDockerStub(t, 15))
	err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false)
	if err == nil {
		t.Fatal("enforceEngineerContainerLimit at overridden limit: want error, got nil")
	}
	if !strings.Contains(err.Error(), "limit 15") {
		t.Fatalf("enforceEngineerContainerLimit overridden limit error = %v", err)
	}
}

func TestLaunchAgentContainerRejectsAtLimitWithoutReservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r, _, _ := bufRunner(engineerRepoAndGlobalCountDockerStub(t, "coilyco-flight-deck/ward", 0, engineerContainerLimitDefault()))
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

func TestActiveEngineerLaunchCountIgnoresLaunchIntents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r, _, _ := bufRunner(engineerRepoAndGlobalCountDockerStub(t, "coilyco-flight-deck/ward", engineerRepoWorkingLimitDefault()-1, 0))
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 885}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	reservationPath, err := agentReservationPath(agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 883})
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(reservationPath, agentReservation{
		Owner:     "coilyco-flight-deck",
		Repo:      "ward",
		Number:    883,
		Mode:      "codex",
		Container: "reserved-engineer-03",
		Branch:    "main",
		Host:      "test-host",
		PID:       1234,
		At:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}

	count, err := r.activeEngineerLaunchCountForRepo(context.Background(), "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo: %v", err)
	}
	if count != engineerRepoWorkingLimitDefault()-1 {
		t.Fatalf("activeEngineerLaunchCountForRepo = %d, want %d", count, engineerRepoWorkingLimitDefault()-1)
	}
	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", "coilyco-flight-deck/ward"); err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck should ignore launch intents, got %v", err)
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatal("launchRepoEngineerBackpressureCheck should not touch the target issue reservation")
	}
}
