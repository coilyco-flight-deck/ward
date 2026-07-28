package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

func TestCheckDockerReadyPasses(t *testing.T) {
	r := fakeDockerRunner(t, "24.0.0\n", 0)
	if err := r.checkDockerReady(context.Background()); err != nil {
		t.Fatalf("checkDockerReady returned %v, want nil", err)
	}
}

func TestCheckDockerReadyPromptsWhenDockerMissing(t *testing.T) {
	r := &Runner{Runner: &shell.Runner{
		Stderr: io.Discard,
		Resolve: func(bin string) (string, error) {
			return "", fmt.Errorf("%s missing", bin)
		},
	}}
	err := r.checkDockerReady(context.Background())
	if err == nil {
		t.Fatal("checkDockerReady should fail when docker cannot resolve")
	}
	got := err.Error()
	for _, want := range []string{
		"Docker is not ready",
		"Initialize or start Docker",
		"docker version --format '{{.Server.Version}}'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("docker prompt missing %q:\n%s", want, got)
		}
	}
}

func TestCheckDockerReadyPromptsWhenDaemonDown(t *testing.T) {
	r := fakeDockerRunner(t, "Cannot connect to the Docker daemon\n", 1)
	err := r.checkDockerReady(context.Background())
	if err == nil {
		t.Fatal("checkDockerReady should fail when docker exits non-zero")
	}
	got := err.Error()
	if !strings.Contains(got, "Docker is not ready") || !strings.Contains(got, "Readiness probe") {
		t.Fatalf("daemon-down error should carry the init prompt, got:\n%s", got)
	}
}
