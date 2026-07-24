package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

func writeOpaqueBundleFixture(t *testing.T, role string) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"format":"agent-compose.bundle","role":"` + role + `"}`
	if err := os.WriteFile(filepath.Join(dir, agentComposeManifest), []byte(body), 0o644); err != nil {
		t.Fatalf("write opaque manifest: %v", err)
	}
	return dir
}

func TestResolveAgentComposeBundle(t *testing.T) {
	t.Run("empty is compatible", func(t *testing.T) {
		got, err := resolveAgentComposeBundle("")
		if err != nil || got != "" {
			t.Fatalf("empty bundle = %q, %v", got, err)
		}
	})
	t.Run("valid directory resolves", func(t *testing.T) {
		dir := writeOpaqueBundleFixture(t, roleEngineer)
		want, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := resolveAgentComposeBundle(dir)
		if err != nil {
			t.Fatalf("resolve valid bundle: %v", err)
		}
		if got != want {
			t.Fatalf("resolved bundle = %q, want %q", got, want)
		}
	})
	t.Run("missing path fails before launch", func(t *testing.T) {
		_, err := resolveAgentComposeBundle(filepath.Join(t.TempDir(), "missing"))
		if err == nil || !strings.Contains(err.Error(), "resolve --agent-compose-bundle") {
			t.Fatalf("missing bundle error = %v", err)
		}
	})
	t.Run("file is not a bundle directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "bundle")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := resolveAgentComposeBundle(file)
		if err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("file bundle error = %v", err)
		}
	})
	t.Run("manifest must be regular", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, agentComposeManifest), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := resolveAgentComposeBundle(dir)
		if err == nil || !strings.Contains(err.Error(), "non-regular manifest.json") {
			t.Fatalf("directory manifest error = %v", err)
		}
	})
}

func TestBuildUpPlanMountsAgentComposeBundleReadOnly(t *testing.T) {
	bundle := writeOpaqueBundleFixture(t, roleEngineer)
	cmd := parseCommandForTest(t, agentImageFlags(), []string{"probe", "--agent-compose-bundle", bundle})
	plan, err := buildUpPlan(cmd, targetRepo{Owner: "owner", Name: "repo"}, modeCodex, roleEngineer, t.TempDir(), t.TempDir(), nil, false)
	if err != nil {
		t.Fatalf("buildUpPlan: %v", err)
	}
	if plan.AgentComposeBundle == "" {
		t.Fatal("plan did not retain the resolved agent-compose bundle")
	}
	var found bool
	for _, mount := range plan.Mounts {
		if mount.Target != containerAgentComposeBundle {
			continue
		}
		found = true
		if mount.Source != plan.AgentComposeBundle || !mount.ReadOnly || mount.Volume {
			t.Fatalf("agent-compose mount = %+v, want resolved host bind read-only", mount)
		}
	}
	if !found {
		t.Fatalf("mount set has no %s: %+v", containerAgentComposeBundle, plan.Mounts)
	}
	if got := plan.wardEnv()["WARD_AGENT_COMPOSE_BUNDLE"]; got != containerAgentComposeBundle {
		t.Fatalf("WARD_AGENT_COMPOSE_BUNDLE = %q, want %q", got, containerAgentComposeBundle)
	}
	joined := strings.Join(dockerCreateArgv(plan, ""), " ")
	if !strings.Contains(joined, plan.AgentComposeBundle+":"+containerAgentComposeBundle+":ro") {
		t.Fatalf("docker argv has no read-only agent-compose bind: %s", joined)
	}
}

func TestNoAgentComposeBundlePreservesLaunch(t *testing.T) {
	plan := sampleUpPlan()
	if got := plan.wardEnv()["WARD_AGENT_COMPOSE_BUNDLE"]; got != "" {
		t.Fatalf("ordinary launch exported WARD_AGENT_COMPOSE_BUNDLE=%q", got)
	}
	for _, mount := range plan.Mounts {
		if mount.Target == containerAgentComposeBundle {
			t.Fatalf("ordinary launch unexpectedly mounted %+v", mount)
		}
	}
	called := false
	prev := runAgentCompose
	runAgentCompose = func(context.Context, *Runner, ...string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { runAgentCompose = prev })
	r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
	if err := r.projectAgentComposeHome(t.Context(), bootstrapEnv{}); err != nil {
		t.Fatalf("empty bundle projection: %v", err)
	}
	if called {
		t.Fatal("ordinary launch invoked agent-compose")
	}
}

func TestAgentComposeNestedDispatchFailsClosed(t *testing.T) {
	plan := sampleUpPlan()
	plan.AgentComposeBundle = "/host/bundle"
	err := agentComposeDispatchGuard(true, plan)
	if err == nil || !strings.Contains(err.Error(), "cannot preserve its read-only host bind") {
		t.Fatalf("nested bundle dispatch error = %v", err)
	}
	if err := agentComposeDispatchGuard(false, plan); err != nil {
		t.Fatalf("host bundle launch was refused: %v", err)
	}
	plan.AgentComposeBundle = ""
	if err := agentComposeDispatchGuard(true, plan); err != nil {
		t.Fatalf("ordinary nested launch was refused: %v", err)
	}
}

func TestAgentComposeRoleAndHarnessCompatibilityMatrix(t *testing.T) {
	prev := runAgentCompose
	t.Cleanup(func() { runAgentCompose = prev })

	roles := []string{"engineer", "qa", "director", "advisor", "ops"}
	harnesses := []string{string(modeClaude), string(modeCodex), string(modeGoose), string(modeOpencode)}
	for _, role := range roles {
		for _, harness := range harnesses {
			t.Run(role+"/"+harness, func(t *testing.T) {
				home := t.TempDir()
				authority := filepath.Join(home, "AGENTS.md")
				if err := os.WriteFile(authority, []byte("ward authority"), 0o644); err != nil {
					t.Fatal(err)
				}
				rel, err := agentComposeInstructionRel(harness)
				if err != nil {
					t.Fatal(err)
				}
				instruction := filepath.Join(home, rel)
				if harness != string(modeOpencode) {
					if err := os.MkdirAll(filepath.Dir(instruction), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(instruction, []byte("ward authority"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				bundle := writeOpaqueBundleFixture(t, role)
				var calls [][]string
				runAgentCompose = func(_ context.Context, _ *Runner, args ...string) error {
					calls = append(calls, slices.Clone(args))
					if len(args) > 0 && args[0] == "project" {
						if err := os.MkdirAll(filepath.Dir(instruction), 0o755); err != nil {
							return err
						}
						return os.WriteFile(instruction, []byte("selected "+role+" identity"), 0o644)
					}
					return nil
				}
				r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
				e := bootstrapEnv{
					Role:               role,
					Mode:               harness,
					AgentHome:          home,
					AgentComposeBundle: bundle,
				}
				if err := r.projectAgentComposeHome(t.Context(), e); err != nil {
					t.Fatalf("projectAgentComposeHome: %v", err)
				}
				wantCalls := [][]string{
					{"verify", bundle},
					{"project", bundle, "--layout", harness, "--scope", "home", "--target", home},
				}
				if !slices.EqualFunc(calls, wantCalls, func(left, right []string) bool {
					return slices.Equal(left, right)
				}) {
					t.Fatalf("agent-compose calls = %v, want %v", calls, wantCalls)
				}
				got, err := os.ReadFile(instruction)
				if err != nil {
					t.Fatalf("read merged instruction: %v", err)
				}
				text := string(got)
				if !strings.Contains(text, "selected "+role+" identity") ||
					!strings.Contains(text, "Ward container authority context") ||
					!strings.Contains(text, "ward authority") {
					t.Fatalf("merged context lost identity or authority:\n%s", text)
				}
				manifest, err := os.ReadFile(filepath.Join(bundle, agentComposeManifest))
				if err != nil {
					t.Fatalf("read immutable fixture: %v", err)
				}
				if string(manifest) != `{"format":"agent-compose.bundle","role":"`+role+`"}` {
					t.Fatal("Ward changed the opaque input bundle")
				}
			})
		}
	}
}

func TestAgentComposeProjectionFailsClosed(t *testing.T) {
	prev := runAgentCompose
	t.Cleanup(func() { runAgentCompose = prev })
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("ward authority"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := writeOpaqueBundleFixture(t, roleEngineer)
	r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}

	t.Run("malformed bundle", func(t *testing.T) {
		runAgentCompose = func(_ context.Context, _ *Runner, args ...string) error {
			if args[0] == "verify" {
				return errors.New("manifest role is invalid")
			}
			t.Fatal("project ran after verification failed")
			return nil
		}
		err := r.projectAgentComposeHome(t.Context(), bootstrapEnv{
			Mode: string(modeCodex), AgentHome: home, AgentComposeBundle: bundle,
		})
		if err == nil || !strings.Contains(err.Error(), "verify agent-compose bundle") {
			t.Fatalf("verification error = %v", err)
		}
	})

	t.Run("projection error", func(t *testing.T) {
		instruction := filepath.Join(home, ".codex", "AGENTS.md")
		if err := os.MkdirAll(filepath.Dir(instruction), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(instruction, []byte("ward authority"), 0o644); err != nil {
			t.Fatal(err)
		}
		runAgentCompose = func(_ context.Context, _ *Runner, args ...string) error {
			if args[0] == "project" {
				return errors.New("owned path is foreign")
			}
			return nil
		}
		err := r.projectAgentComposeHome(t.Context(), bootstrapEnv{
			Mode: string(modeCodex), AgentHome: home, AgentComposeBundle: bundle,
		})
		if err == nil || !strings.Contains(err.Error(), "project agent-compose bundle") {
			t.Fatalf("projection error = %v", err)
		}
	})
}
