package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func engineerCapacityRaceDockerStub(t *testing.T, running []string) (stub string, statePath string) {
	t.Helper()
	dir := t.TempDir()
	statePath = filepath.Join(dir, "running.txt")
	if err := os.WriteFile(statePath, []byte(strings.Join(running, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("seed running state: %v", err)
	}
	stub = filepath.Join(dir, "docker")
	script := "#!/bin/sh\n" +
		"state=" + shellQuote(statePath) + "\n" +
		"cmd=$1\n" +
		"shift\n" +
		"case \"$cmd\" in\n" +
		"  ps)\n" +
		"    if [ \"$1\" = -a ]; then\n" +
		"      exit 0\n" +
		"    fi\n" +
		"    cat \"$state\"\n" +
		"    ;;\n" +
		"  start)\n" +
		"    name=$1\n" +
		"    if ! grep -Fxq \"$name\" \"$state\"; then\n" +
		"      printf '%s\\n' \"$name\" >> \"$state\"\n" +
		"    fi\n" +
		"    printf '%s\\n' \"$name\"\n" +
		"    ;;\n" +
		"  rm)\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  cp)\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  *)\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write race docker stub: %v", err)
	}
	return stub, statePath
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
	body := `smart-defaults {
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
repo-authority default=forgejo {
    trusted-owner coilysiren
    repo "coilyco-flight-deck/*" forge=forgejo
}`
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
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

func TestEngineerCapacityLockSerializesConcurrentAdmissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	limit := engineerContainerLimitDefault()
	running := make([]string, 0, limit-1)
	for i := 0; i < limit-1; i++ {
		running = append(running, fmt.Sprintf("engineer-%02d", i+1))
	}
	stub, _ := engineerCapacityRaceDockerStub(t, running)
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, _, _ := bufRunner(stub)

	launch := func(name string) error {
		return r.withEngineerCapacityLock(func() error {
			if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer"); err != nil {
				return err
			}
			return r.dockerExec(context.Background(), "start", name)
		})
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"engineer-race-a", "engineer-race-b"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			<-start
			errs <- launch(name)
		}(name)
	}
	close(start)
	wg.Wait()
	close(errs)

	var success, refused int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case isEngineerCapacityError(err):
			refused++
		default:
			t.Fatalf("unexpected launch error: %v", err)
		}
	}
	if success != 1 || refused != 1 {
		t.Fatalf("launch results = success:%d refused:%d, want 1 each", success, refused)
	}

	names, err := r.runningEngineerContainers(context.Background())
	if err != nil {
		t.Fatalf("runningEngineerContainers: %v", err)
	}
	if got := len(names); got != limit {
		t.Fatalf("final running count = %d, want %d", got, limit)
	}
}

func TestEngineerCapacityLockFailsClosedWhenLockPathUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	lockPath, err := engineerCapacityLockPath()
	if err != nil {
		t.Fatalf("engineerCapacityLockPath: %v", err)
	}
	if err := os.MkdirAll(lockPath, 0o755); err != nil {
		t.Fatalf("mkdir lock path: %v", err)
	}

	r := &Runner{}
	called := false
	err = r.withEngineerCapacityLock(func() error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("withEngineerCapacityLock: want error when lock path cannot be opened, got nil")
	}
	if called {
		t.Fatal("withEngineerCapacityLock: fn ran despite lock path failure")
	}
	if !strings.Contains(err.Error(), "engineer capacity lock: open") {
		t.Fatalf("withEngineerCapacityLock error = %v", err)
	}
}
