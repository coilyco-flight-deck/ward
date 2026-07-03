package main

import (
	"fmt"
	"sort"
	"strings"
)

// eco_gate.go is the pure, fail-closed smoke-gate verdict for `ward eco test`:
// is the boot log safe to promote? See docs/eco-test.md.

// smokeSignals is the tunable set of log substrings the gate keys off; slices so
// the operator can extend them as EcoServer's wording drifts between releases.
type smokeSignals struct {
	// readyMarkers positively confirm the server finished booting.
	readyMarkers []string
	// modExceptionMarkers flag a mod that failed to load (eco-ops#7).
	modExceptionMarkers []string
	// compileFailMarkers flag a UserCode compilation failure.
	compileFailMarkers []string
}

// defaultSmokeSignals returns the built-in marker sets, matched case-insensitively.
func defaultSmokeSignals() smokeSignals {
	return smokeSignals{
		readyMarkers: []string{
			"game world is ready",
			"server initialization complete",
			"now listening for connections",
			"server started",
			"finished startup",
		},
		modExceptionMarkers: []string{
			"modkitexception",
			"error loading mod",
			"failed to load mod",
			"exception while loading",
			"could not load assembly",
			"modkit load error",
		},
		compileFailMarkers: []string{
			"compilation failed",
			"failed to compile",
			"compiler error",
			"error cs", // Roslyn diagnostics: "error CS1002: ..."
			"mod compilation error",
		},
	}
}

// smokeResult is the gate verdict; Pass is the bit the promote gate reads.
type smokeResult struct {
	Pass         bool
	ReadyReached bool
	// ModExceptions / CompileFailures are the offending log lines, de-duplicated.
	ModExceptions   []string
	CompileFailures []string
	// MissingMods are required mods (from --mod) that never showed a loaded line.
	MissingMods []string
	// Reasons is the human-readable block list, empty on Pass.
	Reasons []string
}

// analyzeSmoke is the pure gate. Pass needs ready + no exception + no
// compile-fail + all required mods loaded.
func analyzeSmoke(log string, requiredMods []string, sig smokeSignals) smokeResult {
	res := smokeResult{}
	lines := strings.Split(log, "\n")

	for _, line := range lines {
		if matchAny(line, sig.readyMarkers) {
			res.ReadyReached = true
		}
		if matchAny(line, sig.modExceptionMarkers) {
			res.ModExceptions = appendUnique(res.ModExceptions, strings.TrimSpace(line))
		}
		if matchAny(line, sig.compileFailMarkers) {
			res.CompileFailures = appendUnique(res.CompileFailures, strings.TrimSpace(line))
		}
	}

	res.MissingMods = missingMods(log, requiredMods)

	if !res.ReadyReached {
		res.Reasons = append(res.Reasons, "server never reached a ready marker (boot did not complete)")
	}
	if len(res.ModExceptions) > 0 {
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("%d ModKit load exception line(s) in boot log (eco-ops#7)", len(res.ModExceptions)))
	}
	if len(res.CompileFailures) > 0 {
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("%d UserCode compile-failure line(s) in boot log", len(res.CompileFailures)))
	}
	if len(res.MissingMods) > 0 {
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("required mod(s) never reported loaded: %s", strings.Join(res.MissingMods, ", ")))
	}

	res.Pass = len(res.Reasons) == 0
	return res
}

// missingMods returns required mods whose name never appears on a loaded-style line.
func missingMods(log string, requiredMods []string) []string {
	if len(requiredMods) == 0 {
		return nil
	}
	lower := strings.ToLower(log)
	var missing []string
	for _, mod := range requiredMods {
		mod = strings.TrimSpace(mod)
		if mod == "" {
			continue
		}
		if !modLoaded(lower, strings.ToLower(mod)) {
			missing = append(missing, mod)
		}
	}
	return missing
}

// modLoaded reports whether the log has a line naming the mod alongside a load
// verb; a line that also carries a failure word is a load error, not a load.
func modLoaded(lowerLog, mod string) bool {
	for _, line := range strings.Split(lowerLog, "\n") {
		if !strings.Contains(line, mod) {
			continue
		}
		if strings.Contains(line, "error") || strings.Contains(line, "exception") ||
			strings.Contains(line, "failed") || strings.Contains(line, "could not") {
			continue
		}
		if strings.Contains(line, "loaded") || strings.Contains(line, "initializing") ||
			strings.Contains(line, "loading mod") || strings.Contains(line, "registered") {
			return true
		}
	}
	return false
}

// matchAny reports whether line contains any marker, case-insensitively.
func matchAny(line string, markers []string) bool {
	lower := strings.ToLower(line)
	for _, m := range markers {
		if m == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// appendUnique appends s to xs only if absent.
func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// summaryLine renders a one-line verdict for terminals and issue comments.
func (res smokeResult) summaryLine() string {
	if res.Pass {
		return "smoke gate: PASS (ready reached, no ModKit exception, no compile failure)"
	}
	reasons := append([]string(nil), res.Reasons...)
	sort.Strings(reasons)
	return "smoke gate: FAIL - " + strings.Join(reasons, "; ")
}
