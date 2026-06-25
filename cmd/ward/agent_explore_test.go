package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `explore` is a top-level agent surface alongside work/headless/task/reply/ask/
// sandbox: the read-only sibling of `sandbox` (ward#293).
func TestAgentHasExploreSurface(t *testing.T) {
	surfaces := map[string]bool{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = true
	}
	if !surfaces["explore"] {
		t.Errorf("ward agent missing %q surface; got %v", "explore", surfaces)
	}
}

// A read-only plan exports WARD_READONLY=1 so the entrypoint revokes the push
// credential and the reaper skips salvage; a writable sandbox plan never sets it.
func TestReadOnlyPlanExportsFlag(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_READONLY"]; ok {
		t.Error("a default (writable) plan must not set WARD_READONLY")
	}
	p.ReadOnly = true
	if p.wardEnv()["WARD_READONLY"] != "1" {
		t.Error("a read-only plan must export WARD_READONLY=1")
	}
}

// readBootstrapEnv maps WARD_READONLY onto bootstrapEnv.ReadOnly (the in-container
// side that revokes the credential + composes the restriction).
func TestReadBootstrapEnvReadOnly(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")

	t.Setenv("WARD_READONLY", "")
	if e, _ := readBootstrapEnv(); e.ReadOnly {
		t.Error("ReadOnly should default false when WARD_READONLY is unset")
	}
	t.Setenv("WARD_READONLY", "1")
	if e, _ := readBootstrapEnv(); !e.ReadOnly {
		t.Error("WARD_READONLY=1 should set bootstrapEnv.ReadOnly")
	}
}

// readReapEnv maps WARD_READONLY onto reapEnv.ReadOnly so the reaper short-circuits
// before it can push or salvage a read-only session's working tree (ward#293).
func TestReadReapEnvReadOnly(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")

	t.Setenv("WARD_READONLY", "1")
	if e, _ := readReapEnv(); !e.ReadOnly {
		t.Error("WARD_READONLY=1 should set reapEnv.ReadOnly")
	}
	t.Setenv("WARD_READONLY", "")
	if e, _ := readReapEnv(); e.ReadOnly {
		t.Error("ReadOnly should default false when WARD_READONLY is unset")
	}
}

// composeContext appends the read-only restriction block only for a read-only run,
// since a seedless explore session has no prompt to carry the "do not push" rule.
func TestComposeContextReadOnlyBlock(t *testing.T) {
	const marker = "Read-only session (this overrides the autonomy doctrine above)"
	r := &Runner{}

	writable := composeInto(t, r, false)
	if strings.Contains(writable, marker) {
		t.Error("a writable session must not get the read-only restriction block")
	}
	readonly := composeInto(t, r, true)
	if !strings.Contains(readonly, marker) {
		t.Error("a read-only session must get the read-only restriction block")
	}
	if !strings.Contains(readonly, "warded explore") {
		t.Error("the read-only block should name the warded explore surface")
	}
}

// composeInto runs composeContext against a temp AGENT_HOME with no host context
// (level 0) and returns the composed CLAUDE.md text.
func composeInto(t *testing.T, r *Runner, readOnly bool) string {
	t.Helper()
	home := t.TempDir()
	r.composeContext(bootstrapEnv{
		Mode:         "claude",
		ContextLevel: "0",
		ContextSrc:   filepath.Join(t.TempDir(), "absent"),
		AgentHome:    home,
		ReadOnly:     readOnly,
	})
	out, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("composed context not written: %v", err)
	}
	return string(out)
}
