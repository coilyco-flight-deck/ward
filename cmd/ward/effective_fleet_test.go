package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontierAgentDefaultsAreKeyedAndComplete(t *testing.T) {
	for _, name := range frontierAgentNames() {
		a, ok := frontierAgentDefaults[name]
		if !ok {
			t.Fatalf("frontier defaults missing %q", name)
		}
		if a.Name != name {
			t.Errorf("frontier defaults[%q].Name = %q", name, a.Name)
		}
		if a.Binary == "" || len(a.Argv.Headless) == 0 || a.Argv.Headless[0] != a.Binary {
			t.Errorf("frontier defaults[%q] is not a complete launch shape: %+v", name, a)
		}
	}
}

func writeFleetBundle(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureAgentsPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write bundle agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureRolesPath), []byte(`roles {
    role engineer {
    }
    role director {
        guardfiles tailscale.kdl
    }
}`), 0o644); err != nil {
		t.Fatalf("write bundle roles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(canonicalSmartDefaultsBlock(t, func(defs *smartDefaults) {
		defs.directorMaxParallel = 10
	})), 0o644); err != nil {
		t.Fatalf("write bundle defaults: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner coilysiren
        repo "coilysiren/*" forge=github
    }
}`), 0o644); err != nil {
		t.Fatalf("write bundle repos: %v", err)
	}
}

func TestLoadFleetConfigResolvesFrontierDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFleetBundle(t, dir, `
agents {
    schema-version 2
    agent claude {
    }
}
`)
	raw, err := loadRawFleetConfigFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadRawFleetConfigFrom: %v", err)
	}
	f, err := resolveEffectiveFleet(raw)
	if err != nil {
		t.Fatalf("resolveEffectiveFleet: %v", err)
	}

	if got := len(f.Agents); got < len(frontierAgentOrder) {
		t.Fatalf("len(Agents) = %d, want at least %d", got, len(frontierAgentOrder))
	}
	claude, ok := fleetAgent(f, string(modeClaude))
	if !ok {
		t.Fatal("effective fleet missing claude")
	}
	if claude.Binary != "claude" || claude.ContextLevel != 2 {
		t.Fatalf("claude resolved unexpectedly: %+v", claude)
	}
	if got := strings.Join(claude.Argv.Headless, " "); got != "claude -p --verbose --output-format stream-json" {
		t.Fatalf("claude headless argv = %q", got)
	}
	if _, ok := fleetAgent(f, string(modeCodex)); !ok {
		t.Fatal("effective fleet missing codex")
	}
	opencode, ok := fleetAgent(f, string(modeOpencode))
	if !ok {
		t.Fatal("effective fleet missing opencode")
	}
	if opencode.Model != "" || opencode.Endpoint != "" {
		t.Fatalf("baked opencode config must not choose deployment-local values: %+v", opencode)
	}
	goose, ok := fleetAgent(f, string(modeGoose))
	if !ok {
		t.Fatal("effective fleet missing goose")
	}
	if goose.Model != "" {
		t.Fatalf("baked goose config must not choose a deployment-local model: %+v", goose)
	}
}

func TestLoadFleetConfigSparseFrontierOverride(t *testing.T) {
	dir := t.TempDir()
	writeFleetBundle(t, dir, `
agents {
    schema-version 2
    agent claude {
        context-level 1
    }
}
`)
	raw, err := loadRawFleetConfigFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadRawFleetConfigFrom: %v", err)
	}
	f, err := resolveEffectiveFleet(raw)
	if err != nil {
		t.Fatalf("resolveEffectiveFleet: %v", err)
	}

	claude, ok := fleetAgent(f, string(modeClaude))
	if !ok {
		t.Fatal("effective fleet missing claude")
	}
	if claude.Binary != "claude" || claude.ContextLevel != 1 {
		t.Fatalf("claude resolved unexpectedly: %+v", claude)
	}
	if got := strings.Join(claude.Argv.Headless, " "); got != "claude -p --verbose --output-format stream-json" {
		t.Fatalf("claude headless argv = %q", got)
	}
}

func TestLoadFleetConfigRejectsIncompleteCustomAgent(t *testing.T) {
	dir := t.TempDir()
	writeFleetBundle(t, dir, `
agents {
    schema-version 2
    agent widget {
        context-level 1
        argv {
            headless widget run
            interactive widget
        }
    }
}
`)
	raw, err := loadRawFleetConfigFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadRawFleetConfigFrom: %v", err)
	}
	_, err = resolveEffectiveFleet(raw)
	if err == nil {
		t.Fatal("resolveEffectiveFleet accepted an incomplete custom agent; want a loud failure")
	}
	if !strings.Contains(err.Error(), "has no binary") {
		t.Fatalf("resolveEffectiveFleet error = %v, want missing-binary failure", err)
	}
}
