package main

import (
	"errors"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
)

func TestRunPathPosture_NoBinariesSkips(t *testing.T) {
	got := runPathPosture(nil, func(string) (string, error) { return "", nil })
	if len(got) != 1 || got[0].severity != sevSkip {
		t.Fatalf("want one SKIP, got %+v", got)
	}
}

func TestRunPathPosture_MatchesExpected(t *testing.T) {
	bins := []repocfg.ProtectedBinary{
		{Name: "gcloud", ExpectedRealPaths: []string{"/usr/local/bin/gcloud"}},
	}
	lookup := func(name string) (string, error) {
		if name == "gcloud" {
			return "/usr/local/bin/gcloud", nil
		}
		return "", errors.New("not found")
	}
	got := runPathPosture(bins, lookup)
	if len(got) != 1 || got[0].severity != sevPass {
		t.Fatalf("want one PASS, got %+v", got)
	}
}

func TestRunPathPosture_FailsOnMismatch(t *testing.T) {
	bins := []repocfg.ProtectedBinary{
		{Name: "gcloud", ExpectedRealPaths: []string{"/usr/local/bin/gcloud"}},
	}
	lookup := func(string) (string, error) { return "/opt/homebrew/bin/gcloud", nil }
	got := runPathPosture(bins, lookup)
	if len(got) != 1 || got[0].severity != sevFail {
		t.Fatalf("want FAIL, got %+v", got)
	}
	if !strings.Contains(got[0].detail, "expected one of") {
		t.Errorf("detail should quote expected list: %q", got[0].detail)
	}
}

func TestRunPathPosture_MissingBinaryWarns(t *testing.T) {
	bins := []repocfg.ProtectedBinary{{Name: "gcloud"}}
	lookup := func(string) (string, error) { return "", errors.New("no such file") }
	got := runPathPosture(bins, lookup)
	if len(got) != 1 || got[0].severity != sevWarn {
		t.Fatalf("want WARN, got %+v", got)
	}
}

func TestRunPathPosture_EmptyExpectedIsInfo(t *testing.T) {
	bins := []repocfg.ProtectedBinary{{Name: "gcloud"}}
	lookup := func(string) (string, error) { return "/opt/homebrew/bin/gcloud", nil }
	got := runPathPosture(bins, lookup)
	if len(got) != 1 || got[0].severity != sevInfo {
		t.Fatalf("want INFO, got %+v", got)
	}
}

func TestRunSudoProbe_SkippedWhenNotForbidden(t *testing.T) {
	got := runSudoProbe(false, func() (string, int, error) {
		t.Fatal("runner should not be called when forbid is false")
		return "", 0, nil
	})
	if got.severity != sevSkip {
		t.Fatalf("want SKIP, got %+v", got)
	}
}

func TestRunSudoProbe_FailsOnCleanExit(t *testing.T) {
	got := runSudoProbe(true, func() (string, int, error) { return "", 0, nil })
	if got.severity != sevFail {
		t.Fatalf("want FAIL, got %+v", got)
	}
}

func TestRunSudoProbe_PassesOnPasswordSentinel(t *testing.T) {
	got := runSudoProbe(true, func() (string, int, error) {
		return "sudo: a password is required\n", 1, nil
	})
	if got.severity != sevPass {
		t.Fatalf("want PASS, got %+v", got)
	}
}

func TestRunSudoProbe_WarnsOnNonZeroWithoutSentinel(t *testing.T) {
	got := runSudoProbe(true, func() (string, int, error) {
		return "sudo: unknown user\n", 1, nil
	})
	if got.severity != sevWarn {
		t.Fatalf("want WARN, got %+v", got)
	}
}

func TestRunSudoProbe_WarnsOnRunnerError(t *testing.T) {
	got := runSudoProbe(true, func() (string, int, error) {
		return "", -1, errors.New("sudo not on PATH")
	})
	if got.severity != sevWarn {
		t.Fatalf("want WARN, got %+v", got)
	}
}

func TestRunCredEnvProbe_PassesWhenNothingSet(t *testing.T) {
	bins := []repocfg.ProtectedBinary{{Name: "gcloud", CredentialEnv: []string{"GOOGLE_APPLICATION_CREDENTIALS"}}}
	got := runCredEnvProbe(bins, func(string) string { return "" }, false)
	if len(got) != 1 || got[0].severity != sevPass {
		t.Fatalf("want one PASS, got %+v", got)
	}
}

func TestRunCredEnvProbe_WarnsByDefault(t *testing.T) {
	bins := []repocfg.ProtectedBinary{{Name: "gcloud", CredentialEnv: []string{"GOOGLE_APPLICATION_CREDENTIALS"}}}
	env := map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "/tmp/x.json"}
	got := runCredEnvProbe(bins, func(k string) string { return env[k] }, false)
	if len(got) != 1 || got[0].severity != sevWarn {
		t.Fatalf("want WARN, got %+v", got)
	}
}

func TestRunCredEnvProbe_StrictPromotesToFail(t *testing.T) {
	bins := []repocfg.ProtectedBinary{{Name: "gcloud", CredentialEnv: []string{"GOOGLE_APPLICATION_CREDENTIALS"}}}
	env := map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "/tmp/x.json"}
	got := runCredEnvProbe(bins, func(k string) string { return env[k] }, true)
	if len(got) != 1 || got[0].severity != sevFail {
		t.Fatalf("want FAIL under strict, got %+v", got)
	}
}

func TestRunCredEnvProbe_StableOrder(t *testing.T) {
	bins := []repocfg.ProtectedBinary{
		{Name: "gcloud", CredentialEnv: []string{"GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_AUTH_TOKEN"}},
		{Name: "aws", CredentialEnv: []string{"AWS_SECRET_ACCESS_KEY"}},
	}
	env := func(string) string { return "x" }
	got := runCredEnvProbe(bins, env, false)
	if len(got) != 3 {
		t.Fatalf("want 3 hits, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].detail, "aws") {
		t.Errorf("aws should sort first by binary, got %q", got[0].detail)
	}
	if !strings.Contains(got[1].detail, "CLOUDSDK_AUTH_TOKEN") {
		t.Errorf("CLOUDSDK should sort before GOOGLE_ within gcloud, got %q", got[1].detail)
	}
}

func TestSkipSet(t *testing.T) {
	got := skipSet([]string{"path", " SUDO ", "credentials"})
	if !got["path"] || !got["sudo"] || !got["credentials"] {
		t.Fatalf("normalization failed: %+v", got)
	}
}
