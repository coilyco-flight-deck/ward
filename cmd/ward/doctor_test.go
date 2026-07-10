package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCommandRegistered(t *testing.T) {
	if commandNamed(rootCommand().Commands, "doctor") == nil {
		t.Fatalf("root command missing doctor; got %v", commandNames(rootCommand().Commands))
	}
}

func TestRunDoctorWithValidBundle(t *testing.T) {
	dir := copyDoctorBundle(t)
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	report, err := runDoctor(context.Background())
	if err != nil {
		t.Fatalf("runDoctor with valid bundle: %v; checks=%+v; exec err=%v", err, report.checks, lastCheckErr(report.checks))
	}
	if report.failed() {
		t.Fatalf("runDoctor with valid bundle reported failure: %+v", report.checks)
	}
	if !strings.Contains(report.sourceSummary, "WARD_CONFIG_REF=file://") {
		t.Fatalf("source summary = %q, want the file bundle ref", report.sourceSummary)
	}
}

func TestRunDoctorWithBakedDefaultsKeepsRepoAuthorityClean(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "")
	t.Setenv("WARD_TARGET_OWNER", "coilysiren")
	t.Setenv("WARD_TARGET_REPO", "coilysiren/example")
	report, err := runDoctor(context.Background())
	if !strings.Contains(report.sourceSummary, "baked neutral default") {
		t.Fatalf("source summary = %q, want the baked source note", report.sourceSummary)
	}
	if containsCheck(report.checks, "repo authority") {
		t.Fatalf("runDoctor with baked config still flagged repo authority: %+v (err=%v)", report.checks, err)
	}
}

func TestStrictValidationFailures(t *testing.T) {
	t.Run("unknown keys", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
    bad-key "x"
}
repo-authority default=forgejo {
    trusted-owner coily
    repo "coily/*" forge=forgejo
}`))
		if err == nil || !strings.Contains(err.Error(), "unknown node") {
			t.Fatalf("unknown keys: got %v", err)
		}
	})

	t.Run("bad enum", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
    agent-workflow default=banana {
    }
}
repo-authority default=forgejo {
    trusted-owner coily
    repo "coily/*" forge=forgejo
}`))
		if err == nil || !strings.Contains(err.Error(), "invalid --workflow") {
			t.Fatalf("bad enum: got %v", err)
		}
	})

	t.Run("missing required block", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
}
`))
		if err == nil || !strings.Contains(err.Error(), "missing top-level `repo-authority` block") {
			t.Fatalf("missing required block: got %v", err)
		}
	})

	t.Run("duplicate workflow repo", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
    agent-workflow default=direct-main {
        repo "coily/repo" workflow=pull-requests
        repo "coily/repo" workflow=patch-only
    }
}
repo-authority default=forgejo {
    trusted-owner coily
    repo "coily/*" forge=forgejo
}`))
		if err == nil || !strings.Contains(err.Error(), "repeated") {
			t.Fatalf("duplicate workflow repo: got %v", err)
		}
	})

	t.Run("placeholder owner", func(t *testing.T) {
		defs, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
    agent-workflow default=direct-main {
    }
}
repo-authority default=forgejo {
    trusted-owner example-placeholder-owner
    repo "example-placeholder-owner/*" forge=github
}`))
		if err != nil {
			t.Fatalf("parseSmartDefaults: %v", err)
		}
		if err := validateRepoAuthorityOperational(defs); err == nil || !strings.Contains(err.Error(), "placeholder") {
			t.Fatalf("placeholder owner: got %v", err)
		}
	})

	t.Run("malformed repo pattern", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
}
repo-authority default=forgejo {
    trusted-owner coily
    repo "coily/repo/sub" forge=forgejo
}`))
		if err == nil || !strings.Contains(err.Error(), "owner/repo") {
			t.Fatalf("malformed repo pattern: got %v", err)
		}
	})

	t.Run("invalid duration", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "0"
}
repo-authority default=forgejo {
    trusted-owner coily
    repo "coily/*" forge=forgejo
}`))
		if err == nil || !strings.Contains(err.Error(), "positive duration") {
			t.Fatalf("invalid duration: got %v", err)
		}
	})

	t.Run("invalid number", func(t *testing.T) {
		_, err := parseSmartDefaults([]byte(`
smart-defaults {
    agent-reservation-ttl "1h"
    engineer-container-limit "nope"
}
repo-authority default=forgejo {
    trusted-owner coily
    repo "coily/*" forge=forgejo
}`))
		if err == nil || !strings.Contains(err.Error(), "positive integer") {
			t.Fatalf("invalid number: got %v", err)
		}
	})
}

func copyDoctorBundle(t *testing.T) string {
	t.Helper()
	src := writeBundleFixture(t)
	dst, err := os.MkdirTemp("/dev/shm", "ward-doctor-*")
	if err != nil {
		t.Fatalf("copy doctor bundle tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dst) })
	replacements := []struct {
		old string
		new string
	}{
		{"coilysiren", "coilyco-flight-deck"},
		{"example-bot", "coily-bot"},
		{"bot@example.com", "bot@coilyco.flight-deck"},
		{"git.example.com", "forgejo.coilysiren.me"},
		{"example.tailscale.invalid", "tailscale.coilyco.flight-deck"},
		{"/example/", "/coilyco/"},
		{"example*", "coily*"},
	}
	err = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		body := string(b)
		for _, repl := range replacements {
			body = strings.ReplaceAll(body, repl.old, repl.new)
		}
		body = strings.ReplaceAll(body, "example", "coily")
		b = []byte(body)
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy doctor bundle: %v", err)
	}
	return dst
}

func containsCheck(checks []doctorCheck, name string) bool {
	for _, check := range checks {
		if check.name == name && check.err != nil {
			return true
		}
	}
	return false
}

func lastCheckErr(checks []doctorCheck) error {
	for i := len(checks) - 1; i >= 0; i-- {
		if checks[i].err != nil {
			return checks[i].err
		}
	}
	return nil
}
