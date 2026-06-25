package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// dockerStub writes a stand-in `docker` that echoes a container-id hash to
// stdout, a scout hint to stderr, and the DOCKER_CLI_HINTS it saw to logPath.
func dockerStub(t *testing.T, logPath string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\n" +
		"echo \"hints=$DOCKER_CLI_HINTS\" >> " + logPath + "\n" +
		"echo deadbeefcontainerid\n" +
		"echo \"What's next: docker scout quickview ...\" 1>&2\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return stub
}

// slowDockerStub writes a stand-in `docker` that sleeps long enough for the
// shrunk heartbeat interval to fire at least once before the pull returns.
func slowDockerStub(t *testing.T) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\nsleep 0.1\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return stub
}

// bufRunner builds a Runner whose docker resolves to stub and whose stdout/stderr
// land in the returned buffers, so a test can read exactly what a launch printed.
func bufRunner(stub string) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &Runner{Runner: &shell.Runner{
		Stdout:  &out,
		Stderr:  &errb,
		Resolve: func(string) (string, error) { return stub, nil },
	}}, &out, &errb
}

// ward#306: a headless (non-interactive) launch must drop docker's pull chatter,
// scout hint, and container-id echo; an interactive launch must stream them.
func TestAgentLaunchSilencesDockerNoiseWhenHeadless(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "hints.log")
	stub := dockerStub(t, logPath)

	t.Run("headless create swallows the container-id hash", func(t *testing.T) {
		r, out, _ := bufRunner(stub)
		p := sampleUpPlan()
		p.Interactive = false
		if err := r.createAgentContainer(t.Context(), p, ""); err != nil {
			t.Fatalf("createAgentContainer: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("headless create leaked docker stdout: %q", out.String())
		}
	})

	t.Run("interactive create streams the container-id hash", func(t *testing.T) {
		r, out, _ := bufRunner(stub)
		p := sampleUpPlan()
		p.Interactive = true
		if err := r.createAgentContainer(t.Context(), p, ""); err != nil {
			t.Fatalf("createAgentContainer: %v", err)
		}
		if !strings.Contains(out.String(), "deadbeefcontainerid") {
			t.Errorf("interactive create must stream docker stdout, got %q", out.String())
		}
	})

	t.Run("headless pull swallows docker chatter but names the pull (ward#322)", func(t *testing.T) {
		r, out, errb := bufRunner(stub)
		p := sampleUpPlan()
		p.Interactive = false
		r.pullAgentImage(t.Context(), p, "ward agent engineer")
		// Docker's own stdout/scout-hint stay silenced (ward#306)...
		if out.Len() != 0 {
			t.Errorf("headless pull leaked docker stdout: %q", out.String())
		}
		if strings.Contains(errb.String(), "docker scout") {
			t.Errorf("headless pull leaked the scout hint: %q", errb.String())
		}
		// ...but the silenced pull now announces itself so a stall is attributable.
		if !strings.Contains(errb.String(), "pulling "+p.Image) {
			t.Errorf("headless pull must name the pull, got stderr=%q", errb.String())
		}
	})

	// ward#322: a slow silenced pull beats a periodic "still pulling" line, so a
	// slow/mid-push registry stall stays visible instead of hanging silently.
	t.Run("headless pull beats a heartbeat on a slow pull", func(t *testing.T) {
		slow := slowDockerStub(t)
		r, _, errb := bufRunner(slow)
		r.pullHeartbeatInterval = 10 * time.Millisecond
		p := sampleUpPlan()
		p.Interactive = false
		r.pullAgentImage(t.Context(), p, "ward agent engineer")
		if !strings.Contains(errb.String(), "still pulling "+p.Image) {
			t.Errorf("slow headless pull must emit a heartbeat, got stderr=%q", errb.String())
		}
	})

	t.Run("interactive pull streams docker output", func(t *testing.T) {
		r, out, _ := bufRunner(stub)
		p := sampleUpPlan()
		p.Interactive = true
		r.pullAgentImage(t.Context(), p, "ward agent engineer")
		if !strings.Contains(out.String(), "deadbeefcontainerid") {
			t.Errorf("interactive pull must stream docker output, got %q", out.String())
		}
	})

	// The silenced path sets DOCKER_CLI_HINTS=false (kills the scout footer); the
	// loud path leaves it unset. The stub logs the value per call, oldest first.
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read hints log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	// headless create is one silenced `docker run` on a host; in a container it
	// dispatches via create+cp+start (ward#323), so it emits a second silenced call.
	want := []string{"hints=false"}
	if inContainer() {
		want = append(want, "hints=false") // the silenced `start` (ward#340)
	}
	// then: interactive create (loud), headless pull (silenced), interactive pull (loud).
	want = append(want, "hints=", "hints=false", "hints=")
	if len(lines) != len(want) {
		t.Fatalf("hints log = %v, want %v", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("hints log line %d = %q, want %q (full: %v)", i, lines[i], w, lines)
		}
	}
}
