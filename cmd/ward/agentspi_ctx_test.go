package main

import (
	"context"
	"io"
	"runtime"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// agentspi_ctx_test.go pins the Phase 1 carve (ward#410): every field the
// agentspi views expose maps to its real source, and the Exec + Log seams wire.

func TestAgentRunCtxCarve(t *testing.T) {
	e := bootstrapEnv{
		AgentHome:      "/home/ubuntu",
		TargetName:     "ward",
		AgentUID:       "1000",
		AgentGID:       "1000",
		Headless:       true,
		Ask:            false,
		CodexModel:     "gpt-5.4-mini",
		CodexEffort:    "low",
		CodexVerbosity: "low",
		QwenModel:      "qwen3-coder:30b",
		OllamaURL:      "http://localhost:11434/v1",
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
	// The qwen->opencode roster untangle is Phase 2; the carve reads the
	// bootstrapEnv.QwenModel field into the neutral OpencodeModel today.
	if rc.OpencodeModel != e.QwenModel {
		t.Errorf("OpencodeModel = %q, want QwenModel %q", rc.OpencodeModel, e.QwenModel)
	}
	if rc.OllamaURL != e.OllamaURL {
		t.Errorf("OllamaURL = %q, want %q", rc.OllamaURL, e.OllamaURL)
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

func TestAgentHostCtxCarve(t *testing.T) {
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	hc := r.agentHostCtx(context.Background())

	if hc.GOOS != runtime.GOOS {
		t.Errorf("GOOS = %q, want %q", hc.GOOS, runtime.GOOS)
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
