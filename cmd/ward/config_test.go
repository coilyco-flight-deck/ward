package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDropClearsLocalConfigRefAndPreservesPreferences(t *testing.T) {
	localRef := filepath.Join(t.TempDir(), "ward-config.kdl")
	path := writeTestWardGlobalConfig(t, strings.Join([]string{
		"config-ref: " + localRef,
		"director:",
		"  default-scope:",
		"    - coilyco-flight-deck/ward",
		"container:",
		"  memory-limit: 4g",
		"",
	}, "\n"))
	t.Setenv(wardConfigRefEnv, "")

	report, err := runConfigDrop()
	if err != nil {
		t.Fatalf("runConfigDrop: %v", err)
	}
	if !report.localCleared {
		t.Fatal("local config-ref was not cleared")
	}
	if report.localRef != localRef {
		t.Fatalf("cleared local ref = %q, want %q", report.localRef, localRef)
	}
	if !strings.Contains(report.resultSummary, "baked/default bundle") {
		t.Fatalf("result summary = %q, want baked/default bundle", report.resultSummary)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after drop: %v", err)
	}
	got := string(body)
	if strings.Contains(got, "config-ref") {
		t.Fatalf("config-ref survived drop:\n%s", got)
	}
	for _, want := range []string{"director:", "default-scope:", "coilyco-flight-deck/ward", "container:", "memory-limit: 4g"} {
		if !strings.Contains(got, want) {
			t.Fatalf("config after drop missing %q:\n%s", want, got)
		}
	}

	var out bytes.Buffer
	printConfigDropReport(&out, report)
	if !strings.Contains(out.String(), "resulting config source: baked/default bundle") {
		t.Fatalf("drop output missing resulting summary:\n%s", out.String())
	}
}

func TestConfigDropRefusesSuccessWhenEnvStillSet(t *testing.T) {
	localRef := filepath.Join(t.TempDir(), "ward-config.kdl")
	path := writeTestWardGlobalConfig(t, "config-ref: "+localRef+"\nagent:\n  review:\n    skip: [qa]\n")
	envRef := "file://" + t.TempDir()
	t.Setenv(wardConfigRefEnv, envRef)

	report, err := runConfigDrop()
	if err == nil {
		t.Fatal("runConfigDrop with inherited WARD_CONFIG_REF succeeded; want refusal")
	}
	if !strings.Contains(err.Error(), configDropEnvStillSetMessage) {
		t.Fatalf("error = %q, want env-still-set refusal", err)
	}
	if !report.localCleared {
		t.Fatal("local config-ref was not cleared before reporting inherited env")
	}
	if report.resultSummary != "inherited environment value: "+wardConfigRefEnv+"="+envRef {
		t.Fatalf("result summary = %q, want inherited env", report.resultSummary)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after env refusal: %v", err)
	}
	if strings.Contains(string(body), "config-ref") {
		t.Fatalf("config-ref survived env refusal:\n%s", string(body))
	}

	var out bytes.Buffer
	printConfigDropReport(&out, report)
	for _, want := range []string{
		wardConfigRefEnv + "=" + envRef + " is still inherited",
		"resulting config source: inherited environment value",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("drop output missing %q:\n%s", want, out.String())
		}
	}
}

func TestConfigDropAlreadyDefaultDoesNotCreateConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(wardConfigRefEnv, "")

	report, err := runConfigDrop()
	if err != nil {
		t.Fatalf("runConfigDrop already default: %v", err)
	}
	if report.localConfigPresent {
		t.Fatal("missing local config reported as present")
	}
	if report.localCleared {
		t.Fatal("already-default case reported a cleared local ref")
	}
	if !strings.Contains(report.resultSummary, "baked/default bundle") {
		t.Fatalf("result summary = %q, want baked/default bundle", report.resultSummary)
	}
	if _, err := os.Stat(filepath.Join(home, ".ward", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("already-default drop created config file or hit wrong error: %v", err)
	}
}

func TestRootCommandExposesConfigDrop(t *testing.T) {
	config := commandNamed(rootCommand().Commands, "config")
	if config == nil {
		t.Fatal("root command missing config")
	}
	if drop := commandNamed(config.Commands, "drop"); drop == nil {
		t.Fatal("config command missing drop")
	}
}

func writeTestWardGlobalConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".ward", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir global config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	return path
}
