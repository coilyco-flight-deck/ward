package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestRunSetupReportsDockerPrompt(t *testing.T) {
	setTestHome(t, t.TempDir())
	prev := setupDockerReadiness
	setupDockerReadiness = func(context.Context) error {
		return fmt.Errorf("%s", dockerInitPrompt(fmt.Errorf("Cannot connect to the Docker daemon")))
	}
	t.Cleanup(func() { setupDockerReadiness = prev })

	report, err := runSetup(context.Background())
	if err != nil {
		t.Fatalf("runSetup should keep Docker init guidance non-fatal, got %v", err)
	}
	if !strings.Contains(report.dockerPrompt, "Docker is not ready") {
		t.Fatalf("setup report missing Docker prompt: %#v", report)
	}
	if report.nextStep != "initialize Docker, then restart warded" {
		t.Fatalf("setup next step = %q", report.nextStep)
	}

	out := captureStdout(t, func() {
		printSetupReport(report)
	})
	for _, want := range []string{
		"ward setup: docker=needs-init",
		"Docker is not ready",
		"ward setup: next step: initialize Docker, then restart warded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("setup output missing %q:\n%s", want, out)
		}
	}
}

func TestRunSetupReportsDockerReady(t *testing.T) {
	setTestHome(t, t.TempDir())
	prev := setupDockerReadiness
	setupDockerReadiness = func(context.Context) error { return nil }
	t.Cleanup(func() { setupDockerReadiness = prev })

	report, err := runSetup(context.Background())
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	if report.dockerPrompt != "" {
		t.Fatalf("dockerPrompt = %q, want empty", report.dockerPrompt)
	}
	out := captureStdout(t, func() {
		printSetupReport(report)
	})
	if !strings.Contains(out, "ward setup: docker=ready") {
		t.Fatalf("setup output should report ready Docker:\n%s", out)
	}
}
