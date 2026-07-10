package main

import (
	"context"
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
	t.Run("below limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, engineerContainerLimit-1))
		if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer"); err != nil {
			t.Fatalf("enforceEngineerContainerLimit below limit: %v", err)
		}
	})

	t.Run("at limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, engineerContainerLimit))
		err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer")
		if err == nil {
			t.Fatal("enforceEngineerContainerLimit at limit: want error, got nil")
		}
		for _, want := range []string{
			"global engineer limit is reached",
			"10 running",
			"limit 10",
			"ward agent reap",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("enforceEngineerContainerLimit at limit missing %q: %v", want, err)
			}
		}
	})
}

func TestLaunchAgentContainerRejectsAtLimitWithoutReservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r, _, _ := bufRunner(engineerCountDockerStub(t, engineerContainerLimit))
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
