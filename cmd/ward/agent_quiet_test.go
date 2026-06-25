package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	t.Run("headless pull swallows stdout, stderr, and the scout hint", func(t *testing.T) {
		r, out, errb := bufRunner(stub)
		p := sampleUpPlan()
		p.Interactive = false
		r.pullAgentImage(t.Context(), p, "ward agent headless")
		if out.Len() != 0 || errb.Len() != 0 {
			t.Errorf("headless pull leaked output: stdout=%q stderr=%q", out.String(), errb.String())
		}
	})

	t.Run("interactive pull streams docker output", func(t *testing.T) {
		r, out, _ := bufRunner(stub)
		p := sampleUpPlan()
		p.Interactive = true
		r.pullAgentImage(t.Context(), p, "ward agent work")
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
	want := []string{"hints=false", "hints=", "hints=false", "hints="}
	if len(lines) != len(want) {
		t.Fatalf("hints log = %v, want %v", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("hints log line %d = %q, want %q (full: %v)", i, lines[i], w, lines)
		}
	}
}
