package main

import (
	"strings"
	"testing"
)

// A boot that reaches ready with no mod exception and no compile failure passes.
func TestAnalyzeSmoke_CleanBootPasses(t *testing.T) {
	log := strings.Join([]string{
		"Starting EcoServer...",
		"Compiling mods in Mods/UserCode",
		"Loaded mod EcoTelemetry",
		"Game world is ready",
	}, "\n")
	res := analyzeSmoke(log, []string{"EcoTelemetry"}, defaultSmokeSignals())
	if !res.Pass {
		t.Fatalf("clean boot should pass, got FAIL: %v", res.Reasons)
	}
	if !res.ReadyReached {
		t.Fatalf("ready marker should be detected")
	}
}

// eco-ops#7: a ModKit load exception blocks promote even when the server reached
// ready (the exact failure this gate exists to catch).
func TestAnalyzeSmoke_ModKitExceptionBlocks(t *testing.T) {
	log := strings.Join([]string{
		"Loaded mod EcoTelemetry",
		"ModKitException: failed to bind plugin EcoTelemetry",
		"Game world is ready",
	}, "\n")
	res := analyzeSmoke(log, nil, defaultSmokeSignals())
	if res.Pass {
		t.Fatalf("ModKit exception must block promote, got PASS")
	}
	if len(res.ModExceptions) != 1 {
		t.Fatalf("want 1 mod-exception line, got %d: %v", len(res.ModExceptions), res.ModExceptions)
	}
}

// A boot that never reaches ready fails closed (silence is not success).
func TestAnalyzeSmoke_NeverReadyFailsClosed(t *testing.T) {
	log := "Starting EcoServer...\nLoading world data\n"
	res := analyzeSmoke(log, nil, defaultSmokeSignals())
	if res.Pass {
		t.Fatalf("boot that never reached ready must fail closed")
	}
	if res.ReadyReached {
		t.Fatalf("no ready marker present; ReadyReached should be false")
	}
}

// A UserCode compile failure blocks promote.
func TestAnalyzeSmoke_CompileFailureBlocks(t *testing.T) {
	log := strings.Join([]string{
		"Compiling mods in Mods/UserCode",
		"error CS1002: ; expected",
		"Compilation failed",
	}, "\n")
	res := analyzeSmoke(log, nil, defaultSmokeSignals())
	if res.Pass {
		t.Fatalf("compile failure must block promote")
	}
	if len(res.CompileFailures) == 0 {
		t.Fatalf("want compile-failure lines recorded")
	}
}

// A required mod that never reports loaded blocks promote, even on a clean boot.
func TestAnalyzeSmoke_MissingRequiredModBlocks(t *testing.T) {
	log := strings.Join([]string{
		"Loaded mod SomethingElse",
		"Game world is ready",
	}, "\n")
	res := analyzeSmoke(log, []string{"EcoTelemetry"}, defaultSmokeSignals())
	if res.Pass {
		t.Fatalf("missing required mod must block promote")
	}
	if len(res.MissingMods) != 1 || res.MissingMods[0] != "EcoTelemetry" {
		t.Fatalf("want EcoTelemetry missing, got %v", res.MissingMods)
	}
}

// A mod named only on an error line is NOT counted as loaded.
func TestAnalyzeSmoke_ModOnErrorLineNotLoaded(t *testing.T) {
	log := strings.Join([]string{
		"Error loading mod EcoTelemetry",
		"Game world is ready",
	}, "\n")
	res := analyzeSmoke(log, []string{"EcoTelemetry"}, defaultSmokeSignals())
	if res.Pass {
		t.Fatalf("mod that only appears on an error line must not pass")
	}
	// It should be flagged as both a mod exception AND missing-loaded.
	if len(res.ModExceptions) == 0 {
		t.Fatalf("want the error line recorded as a mod exception")
	}
	if len(res.MissingMods) != 1 {
		t.Fatalf("want EcoTelemetry counted missing, got %v", res.MissingMods)
	}
}

// Marker matching is case-insensitive.
func TestAnalyzeSmoke_CaseInsensitive(t *testing.T) {
	res := analyzeSmoke("GAME WORLD IS READY", nil, defaultSmokeSignals())
	if !res.ReadyReached {
		t.Fatalf("ready marker should match case-insensitively")
	}
}

// summaryLine renders PASS/FAIL distinctly.
func TestSmokeResult_SummaryLine(t *testing.T) {
	pass := analyzeSmoke("Game world is ready", nil, defaultSmokeSignals())
	if !strings.Contains(pass.summaryLine(), "PASS") {
		t.Fatalf("pass summary should say PASS: %q", pass.summaryLine())
	}
	fail := analyzeSmoke("nothing here", nil, defaultSmokeSignals())
	if !strings.Contains(fail.summaryLine(), "FAIL") {
		t.Fatalf("fail summary should say FAIL: %q", fail.summaryLine())
	}
}
