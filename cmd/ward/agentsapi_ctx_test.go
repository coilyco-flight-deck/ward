package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// agentsapi_ctx_test.go pins the Phase 1 carve (ward#410): every field the
// agentsapi views expose maps to its real source, and the Exec + Log seams wire.

func TestAgentRunCtxCarve(t *testing.T) {
	e := bootstrapEnv{
		AgentHome:      "/home/ubuntu",
		TargetName:     "ward",
		TargetOwner:    "coilyco-flight-deck",
		AgentUID:       "1000",
		AgentGID:       "1000",
		Headless:       true,
		Ask:            false,
		Container:      "engineer-codex-ward-861",
		Role:           roleEngineer,
		WardVersion:    "1.2.3",
		CodexModel:     "gpt-5.4-mini",
		CodexEffort:    "low",
		CodexVerbosity: "low",
		ClaudeModel:    "sonnet",
		ClaudeEffort:   "medium",
		GooseModel:     "qwen3-coder:30b",
		OpencodeModel:  "qwen3-coder:30b",
		OllamaURL:      "http://host.docker.internal:8082/v1",
	}
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	seed := []string{"carry issue #410"}
	rc := r.agentRunCtx(context.Background(), e, seed)

	if rc.AgentHome != e.AgentHome {
		t.Errorf("AgentHome = %q, want %q", rc.AgentHome, e.AgentHome)
	}
	if rc.TargetName != e.TargetName {
		t.Errorf("TargetName = %q, want %q", rc.TargetName, e.TargetName)
	}
	if rc.AgentUID != e.AgentUID || rc.AgentGID != e.AgentGID {
		t.Errorf("UID/GID = %q/%q, want %q/%q", rc.AgentUID, rc.AgentGID, e.AgentUID, e.AgentGID)
	}
	if rc.Headless != e.Headless || rc.Ask != e.Ask {
		t.Errorf("posture = headless %v ask %v, want %v/%v", rc.Headless, rc.Ask, e.Headless, e.Ask)
	}
	if rc.CodexModel != e.CodexModel || rc.CodexEffort != e.CodexEffort || rc.CodexVerbosity != e.CodexVerbosity {
		t.Errorf("codex knobs = %q/%q/%q, want %q/%q/%q",
			rc.CodexModel, rc.CodexEffort, rc.CodexVerbosity, e.CodexModel, e.CodexEffort, e.CodexVerbosity)
	}
	if rc.ClaudeModel != e.ClaudeModel || rc.ClaudeEffort != e.ClaudeEffort {
		t.Errorf("claude knobs = %q/%q, want %q/%q (ward#616)",
			rc.ClaudeModel, rc.ClaudeEffort, e.ClaudeModel, e.ClaudeEffort)
	}
	if rc.GooseModel != e.GooseModel {
		t.Errorf("GooseModel = %q, want %q", rc.GooseModel, e.GooseModel)
	}
	// The carve reads the bootstrapEnv.OpencodeModel field into the neutral
	// OpencodeModel today.
	if rc.OpencodeModel != e.OpencodeModel {
		t.Errorf("OpencodeModel = %q, want OpencodeModel %q", rc.OpencodeModel, e.OpencodeModel)
	}
	if rc.OllamaURL != e.OllamaURL {
		t.Errorf("OllamaURL = %q, want %q", rc.OllamaURL, e.OllamaURL)
	}
	if rc.Correlation.RunID != e.Container || rc.Correlation.ContainerName != e.Container {
		t.Errorf("Correlation run/container = %q/%q, want %q/%q", rc.Correlation.RunID, rc.Correlation.ContainerName, e.Container, e.Container)
	}
	if rc.Correlation.Role != e.Role || rc.Correlation.Harness != e.Agent {
		t.Errorf("Correlation role/harness = %q/%q, want %q/%q", rc.Correlation.Role, rc.Correlation.Harness, e.Role, e.Agent)
	}
	if rc.Correlation.TargetRepo != "coilyco-flight-deck/ward" || rc.Correlation.IssueRef != "coilyco-flight-deck/ward" {
		t.Errorf("Correlation target/issue = %q/%q, want repo path", rc.Correlation.TargetRepo, rc.Correlation.IssueRef)
	}
	if rc.Correlation.Version != e.WardVersion {
		t.Errorf("Correlation version = %q, want %q", rc.Correlation.Version, e.WardVersion)
	}
	if len(rc.Seed) != 1 || rc.Seed[0] != seed[0] {
		t.Errorf("Seed = %v, want %v", rc.Seed, seed)
	}
	if rc.Exec == nil {
		t.Error("Exec seam not wired")
	}
	if rc.Log == nil {
		t.Error("Log seam not wired")
	}
}

// TestAgentTrustDirs covers ward#168: the trust set spans the target clone,
// /workspace, granted extra repos, and every warmed /substrate repo (dirs only).
func TestAgentTrustDirs(t *testing.T) {
	substrate := t.TempDir()
	for _, name := range []string{"agentic-os", "cli-guard"} {
		if err := os.Mkdir(filepath.Join(substrate, name), 0o755); err != nil {
			t.Fatalf("mkdir substrate %s: %v", name, err)
		}
	}
	// A stray file under /substrate must not be trusted as a project dir.
	if err := os.WriteFile(filepath.Join(substrate, "README"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	e := bootstrapEnv{
		TargetName:    "ward",
		ExtraRepos:    []targetRepo{{Owner: "coilyco-flight-deck", Name: "cli-guard"}},
		SubstrateDest: substrate,
	}
	got := agentTrustDirs(e)
	for _, want := range []string{
		"/workspace/ward",
		"/workspace",
		"/workspace/coilyco-flight-deck/cli-guard",
		substrate,
		filepath.Join(substrate, "agentic-os"),
		filepath.Join(substrate, "cli-guard"),
	} {
		if !slices.Contains(got, want) {
			t.Errorf("trust dirs missing %q: %v", want, got)
		}
	}
	if slices.Contains(got, filepath.Join(substrate, "README")) {
		t.Errorf("trust dirs wrongly include the stray file: %v", got)
	}
}

func TestAgentHostCtxCarve(t *testing.T) {
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	hc := r.agentHostCtx(context.Background())

	if hc.GOOS != launchHostGOOS() {
		t.Errorf("GOOS = %q, want %q", hc.GOOS, launchHostGOOS())
	}
	if hc.Home != homeDir() {
		t.Errorf("Home = %q, want %q", hc.Home, homeDir())
	}
	if hc.Exec == nil {
		t.Error("Exec seam not wired")
	}
	if hc.Log == nil {
		t.Error("Log seam not wired")
	}
}
