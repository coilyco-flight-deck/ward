package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeContextBundleFixture(t *testing.T, role string, mode containerMode, withTool bool) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"format":"` + contextBundleFormat + `","role":"` + role + `","agent":"` + string(mode) + `"}`
	if err := os.WriteFile(filepath.Join(dir, contextBundleManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	instruction, skills, err := contextBundleLayout(string(mode))
	if err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(dir, "home", filepath.FromSlash(instruction))
	if err := os.MkdirAll(filepath.Dir(instructionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instructionPath, []byte("selected "+role+" identity"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(dir, "home", filepath.FromSlash(skills), "fixture", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("# Fixture skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withTool {
		toolPath := filepath.Join(dir, "bin", "fixture-tool")
		if err := os.MkdirAll(filepath.Dir(toolPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(toolPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestResolveContextBundle(t *testing.T) {
	t.Run("empty is compatible", func(t *testing.T) {
		got, err := resolveContextBundle("", roleEngineer, modeCodex)
		if err != nil || got.Root != "" || got.HasTools {
			t.Fatalf("empty bundle = %+v, %v", got, err)
		}
	})
	t.Run("valid directory resolves", func(t *testing.T) {
		dir := writeContextBundleFixture(t, roleEngineer, modeCodex, true)
		want, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := resolveContextBundle(dir, roleEngineer, modeCodex)
		if err != nil {
			t.Fatalf("resolve valid bundle: %v", err)
		}
		if got.Root != want || !got.HasTools {
			t.Fatalf("resolved bundle = %+v, want root %q with tools", got, want)
		}
	})
	t.Run("missing path fails before launch", func(t *testing.T) {
		_, err := resolveContextBundle(filepath.Join(t.TempDir(), "missing"), roleEngineer, modeCodex)
		if err == nil || !strings.Contains(err.Error(), "resolve --context-bundle") {
			t.Fatalf("missing bundle error = %v", err)
		}
	})
	t.Run("file is not a bundle directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "bundle")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := resolveContextBundle(file, roleEngineer, modeCodex)
		if err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("file bundle error = %v", err)
		}
	})
	t.Run("manifest must be regular", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, contextBundleManifestName), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := resolveContextBundle(dir, roleEngineer, modeCodex)
		if err == nil || !strings.Contains(err.Error(), "must be a regular file") {
			t.Fatalf("directory manifest error = %v", err)
		}
	})
}

func TestContextBundleManifestBindsRoleAndAgentWithoutAuthority(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string)
		wantErr string
	}{
		{
			name: "role mismatch",
			mutate: func(root string) {
				writeManifestForTest(t, root, `{"format":"`+contextBundleFormat+`","role":"director","agent":"codex"}`)
			},
			wantErr: `role is "director", selected Ward role is "engineer"`,
		},
		{
			name: "agent mismatch",
			mutate: func(root string) {
				writeManifestForTest(t, root, `{"format":"`+contextBundleFormat+`","role":"engineer","agent":"claude"}`)
			},
			wantErr: `agent is "claude", selected Ward agent is "codex"`,
		},
		{
			name: "authority field rejected",
			mutate: func(root string) {
				writeManifestForTest(t, root, `{"format":"`+contextBundleFormat+`","role":"engineer","agent":"codex","capabilities":["network"]}`)
			},
			wantErr: `unknown field "capabilities"`,
		},
		{
			name: "format mismatch",
			mutate: func(root string) {
				writeManifestForTest(t, root, `{"format":"other","role":"engineer","agent":"codex"}`)
			},
			wantErr: `want "` + contextBundleFormat + `"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeContextBundleFixture(t, roleEngineer, modeCodex, false)
			test.mutate(root)
			_, err := resolveContextBundle(root, roleEngineer, modeCodex)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func writeManifestForTest(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, contextBundleManifestName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextBundleRejectsPathsOutsideSelectedLayout(t *testing.T) {
	tests := []struct {
		name    string
		add     func(string)
		wantErr string
	}{
		{
			name: "unexpected root file",
			add: func(root string) {
				if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("no"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: `unexpected bundle path "README.md"`,
		},
		{
			name: "other harness instruction",
			add: func(root string) {
				p := filepath.Join(root, "home", ".claude", "CLAUDE.md")
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("no"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "outside the selected codex layout",
		},
		{
			name: "nested tool",
			add: func(root string) {
				p := filepath.Join(root, "bin", "nested", "tool")
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("no"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "must be directly under bin",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeContextBundleFixture(t, roleEngineer, modeCodex, false)
			test.add(root)
			_, err := resolveContextBundle(root, roleEngineer, modeCodex)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestContextBundleRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ordinary Windows test users cannot create symbolic links")
	}
	root := writeContextBundleFixture(t, roleEngineer, modeCodex, false)
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "home", ".agents", "skills", "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := resolveContextBundle(root, roleEngineer, modeCodex)
	if err == nil || !strings.Contains(err.Error(), "is a symbolic link") {
		t.Fatalf("error = %v", err)
	}
}

func TestContextBundleRejectsNonExecutableTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not preserve Unix executable mode bits")
	}
	root := writeContextBundleFixture(t, roleEngineer, modeCodex, false)
	tool := filepath.Join(root, "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveContextBundle(root, roleEngineer, modeCodex)
	if err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("error = %v", err)
	}
}

func TestContextBundleRequiresSelectedInstruction(t *testing.T) {
	root := writeContextBundleFixture(t, roleEngineer, modeCodex, false)
	instruction, _, err := contextBundleLayout(string(modeCodex))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "home", filepath.FromSlash(instruction))); err != nil {
		t.Fatal(err)
	}
	_, err = resolveContextBundle(root, roleEngineer, modeCodex)
	if err == nil || !strings.Contains(err.Error(), "missing selected codex instruction file") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildUpPlanMountsContextBundleReadOnly(t *testing.T) {
	bundle := writeContextBundleFixture(t, roleEngineer, modeCodex, true)
	cmd := parseCommandForTest(t, agentImageFlags(), []string{"probe", "--context-bundle", bundle})
	plan, err := buildUpPlan(cmd, targetRepo{Owner: "owner", Name: "repo"}, modeCodex, roleEngineer, t.TempDir(), t.TempDir(), nil, false)
	if err != nil {
		t.Fatalf("buildUpPlan: %v", err)
	}
	if plan.ContextBundle == "" || !plan.ContextTools {
		t.Fatalf("plan did not retain the resolved context bundle and tools: %+v", plan)
	}
	var found bool
	for _, mount := range plan.Mounts {
		if mount.Target != containerContextBundle {
			continue
		}
		found = true
		if mount.Source != plan.ContextBundle || !mount.ReadOnly || mount.Volume {
			t.Fatalf("context bundle mount = %+v, want resolved host bind read-only", mount)
		}
	}
	if !found {
		t.Fatalf("mount set has no %s: %+v", containerContextBundle, plan.Mounts)
	}
	env := plan.wardEnv()
	if got := env["WARD_CONTEXT_BUNDLE"]; got != containerContextBundle {
		t.Fatalf("WARD_CONTEXT_BUNDLE = %q, want %q", got, containerContextBundle)
	}
	if got := env["WARD_CONTEXT_TOOLS"]; got != containerContextTools {
		t.Fatalf("WARD_CONTEXT_TOOLS = %q, want %q", got, containerContextTools)
	}
	joined := strings.Join(dockerCreateArgv(plan, ""), " ")
	if !strings.Contains(joined, plan.ContextBundle+":"+containerContextBundle+":ro") {
		t.Fatalf("docker argv has no read-only context bundle bind: %s", joined)
	}
}

func TestAgentFrameworkBuildCollaborationPlanHasNoRepositoryAuthority(t *testing.T) {
	bundle := writeContextBundleFixture(t, "critic", modeCodex, true)
	cmd := parseCommandForTest(t, agentRunCommand().Flags, []string{
		"run", "--role", "critic", "--cluster", "codex-ab45", "--harness", "codex",
		"--context-bundle", bundle, "review the draft",
	})
	plan, err := buildCollaborationPlan(cmd, modeCodex, "critic", "", "codex-ab45", "review the draft", t.TempDir())
	if err != nil {
		t.Fatalf("buildCollaborationPlan: %v", err)
	}
	if !plan.Collaboration || plan.ClusterID != "codex-ab45" || plan.AgentID != "" || plan.DispatchRequestID == "" {
		t.Fatalf("collaboration identity = %+v", plan)
	}
	if plan.Repo.Owner != "" || plan.Repo.Name != "" || plan.HostCwd != "" || len(plan.ExtraRepos) != 0 {
		t.Fatalf("repository-free plan retained repository inputs: %+v", plan)
	}
	env := plan.wardEnv()
	for _, key := range []string{"WARD_TARGET_REPO", "WARD_TARGET_OWNER", "WARD_TARGET_NAME", "WARD_TARGET_ISSUE", "WARD_EXTRA_REPOS"} {
		if _, ok := env[key]; ok {
			t.Fatalf("repository-free plan exported %s=%q", key, env[key])
		}
	}
	if env[envCollaborationPlan] != "1" || env[envClusterID] != "codex-ab45" {
		t.Fatalf("collaboration env = %+v", env)
	}
	var bundleMount bool
	for _, mount := range plan.Mounts {
		if mount.Target == containerContextMount {
			t.Fatalf("repository-free plan mounted a host cwd: %+v", mount)
		}
		if mount.Target == containerContextBundle {
			bundleMount = true
			if mount.Source != plan.ContextBundle || !mount.ReadOnly || mount.Volume {
				t.Fatalf("context bundle mount = %+v, want read-only host bind", mount)
			}
		}
	}
	if !bundleMount {
		t.Fatalf("repository-free plan has no context bundle mount: %+v", plan.Mounts)
	}
	joined := strings.Join(dockerCreateArgv(plan, ""), " ")
	if strings.Contains(joined, labelRepo+"=") || !strings.Contains(joined, labelCluster+"=codex-ab45") {
		t.Fatalf("collaboration docker argv has wrong labels: %s", joined)
	}
}

func TestNoContextBundlePreservesLaunch(t *testing.T) {
	plan := sampleUpPlan()
	env := plan.wardEnv()
	if env["WARD_CONTEXT_BUNDLE"] != "" || env["WARD_CONTEXT_TOOLS"] != "" {
		t.Fatalf("ordinary launch exported context bundle env: %+v", env)
	}
	for _, mount := range plan.Mounts {
		if mount.Target == containerContextBundle {
			t.Fatalf("ordinary launch unexpectedly mounted %+v", mount)
		}
	}
	r := &Runner{}
	if err := r.projectContextBundleHome(bootstrapEnv{}); err != nil {
		t.Fatalf("empty bundle projection: %v", err)
	}
}

func TestContextBundleNestedDispatchFailsClosed(t *testing.T) {
	plan := sampleUpPlan()
	plan.ContextBundle = "/host/bundle"
	err := contextBundleDispatchGuard(true, plan)
	if err == nil || !strings.Contains(err.Error(), "cannot preserve its read-only host bind") {
		t.Fatalf("nested bundle dispatch error = %v", err)
	}
	if err := contextBundleDispatchGuard(false, plan); err != nil {
		t.Fatalf("host bundle launch was refused: %v", err)
	}
	plan.ContextBundle = ""
	if err := contextBundleDispatchGuard(true, plan); err != nil {
		t.Fatalf("ordinary nested launch was refused: %v", err)
	}
}

func TestContextBundleCoreRoleAndAgentCompatibilityMatrix(t *testing.T) {
	roles := []string{roleEngineer, roleQA, roleDirector}
	agents := []containerMode{modeClaude, modeCodex, modeGoose, modeOpencode}
	for _, role := range roles {
		for _, agent := range agents {
			t.Run(role+"/"+string(agent), func(t *testing.T) {
				home := t.TempDir()
				authority := filepath.Join(home, "AGENTS.md")
				if err := os.WriteFile(authority, []byte("ward authority"), 0o644); err != nil {
					t.Fatal(err)
				}
				instructionRel, skillsRel, err := contextBundleLayout(string(agent))
				if err != nil {
					t.Fatal(err)
				}
				instruction := filepath.Join(home, filepath.FromSlash(instructionRel))
				if agent != modeOpencode {
					if err := os.MkdirAll(filepath.Dir(instruction), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(instruction, []byte("ward authority"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				bundle := writeContextBundleFixture(t, role, agent, false)
				before, err := os.ReadFile(filepath.Join(bundle, contextBundleManifestName))
				if err != nil {
					t.Fatal(err)
				}
				r := &Runner{}
				e := bootstrapEnv{
					Role:          role,
					Mode:          string(agent),
					AgentHome:     home,
					ContextBundle: bundle,
				}
				if err := r.projectContextBundleHome(e); err != nil {
					t.Fatalf("projectContextBundleHome: %v", err)
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
				skill := filepath.Join(home, filepath.FromSlash(skillsRel), "fixture", "SKILL.md")
				if _, err := os.Stat(skill); err != nil {
					t.Fatalf("projected skill missing: %v", err)
				}
				after, err := os.ReadFile(filepath.Join(bundle, contextBundleManifestName))
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before) {
					t.Fatal("Ward changed the read-only input bundle")
				}
			})
		}
	}
}

func TestContextBundleProjectionFailsClosedOnForeignSkill(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("ward authority"), 0o644); err != nil {
		t.Fatal(err)
	}
	instruction := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(instruction), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instruction, []byte("ward authority"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(home, ".agents", "skills", "fixture", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := writeContextBundleFixture(t, roleEngineer, modeCodex, false)
	r := &Runner{}
	err := r.projectContextBundleHome(bootstrapEnv{
		Role: roleEngineer, Mode: string(modeCodex), AgentHome: home, ContextBundle: bundle,
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to replace foreign context path") {
		t.Fatalf("projection error = %v", err)
	}
}

func TestContextToolsAreAppendedToAgentPath(t *testing.T) {
	t.Setenv("PATH", "/image/bin:/usr/bin")
	got := strings.Join(setprivPrefix(bootstrapEnv{
		AgentUID: "1000", AgentGID: "1000", AgentHome: "/home/agent", ContextTools: containerContextTools,
	}), " ")
	if !strings.Contains(got, "PATH=/image/bin:/usr/bin"+string(os.PathListSeparator)+containerContextTools) {
		t.Fatalf("setpriv prefix did not append context tools: %s", got)
	}
	if strings.Index(got, "/image/bin") > strings.Index(got, containerContextTools) {
		t.Fatalf("context tools shadow image tools: %s", got)
	}
}
