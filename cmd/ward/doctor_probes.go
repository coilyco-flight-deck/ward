package main

import (
	"fmt"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/sudo"
)

// Probe severities. PASS / FAIL drive the exit code; WARN / INFO / SKIP
// only surface text. The order is the rendering order inside a group.
const (
	sevPass = "PASS"
	sevWarn = "WARN"
	sevFail = "FAIL"
	sevInfo = "INFO"
	sevSkip = "SKIP"
)

// probeResult is the unit each host probe emits. Multiple results per probe
// are normal (e.g. one per protected binary).
type probeResult struct {
	probe    string
	severity string
	detail   string
}

// pathLookup mirrors os/exec.LookPath. Reused from hook_test.go's pattern so
// unit tests can swap in deterministic fakes without touching $PATH.
type pathLookupFn func(name string) (string, error)

// sudoRunner is the abstraction over running `sudo -n true`. Returns the
// command stderr text and the process exit code. exit 0 means sudo ran
// without prompting (the dangerous case under forbid_passwordless: true).
type sudoRunner func() (stderr string, exitCode int, err error)

// envLookup mirrors os.Getenv so the credential-env probe can be unit-tested
// without mutating the process environment.
type envLookup func(name string) string

// runPathPosture checks each protected binary's resolved PATH location
// against its expected_real_paths. An empty expected list emits an INFO
// row recording the resolved path. A missing binary on PATH is a WARN
// (the binary may simply not be installed on this host).
func runPathPosture(binaries []repocfg.ProtectedBinary, lookup pathLookupFn) []probeResult {
	if len(binaries) == 0 {
		return []probeResult{{probe: "path", severity: sevSkip, detail: "no protected_binaries declared"}}
	}
	out := make([]probeResult, 0, len(binaries))
	for _, b := range binaries {
		got, err := lookup(b.Name)
		if err != nil {
			out = append(out, probeResult{
				probe:    "path",
				severity: sevWarn,
				detail:   fmt.Sprintf("%s: not found on PATH (%v)", b.Name, err),
			})
			continue
		}
		if len(b.ExpectedRealPaths) == 0 {
			out = append(out, probeResult{
				probe:    "path",
				severity: sevInfo,
				detail:   fmt.Sprintf("%s -> %s (no expected_real_paths declared)", b.Name, got),
			})
			continue
		}
		if pathMatches(got, b.ExpectedRealPaths) {
			out = append(out, probeResult{
				probe:    "path",
				severity: sevPass,
				detail:   fmt.Sprintf("%s -> %s (matches expected)", b.Name, got),
			})
			continue
		}
		out = append(out, probeResult{
			probe:    "path",
			severity: sevFail,
			detail: fmt.Sprintf("%s -> %s, expected one of %s",
				b.Name, got, strings.Join(b.ExpectedRealPaths, ", ")),
		})
	}
	return out
}

// pathMatches reports whether resolved is byte-equal to any expected path.
// Symlinks aren't followed — the policy is about which PATH entry won, not
// the eventual inode.
func pathMatches(resolved string, expected []string) bool {
	for _, e := range expected {
		if e == resolved {
			return true
		}
	}
	return false
}

// runSudoProbe runs `sudo -n true` (via the injected runner) when the
// policy forbids passwordless sudo. A clean exit means sudo ran without
// a password — the failure case. A non-zero exit with a password-required
// sentinel means the host is configured the way the policy wants.
func runSudoProbe(forbid bool, runner sudoRunner) probeResult {
	if !forbid {
		return probeResult{probe: "sudo", severity: sevSkip, detail: "forbid_passwordless not set"}
	}
	stderr, exitCode, err := runner()
	if err != nil {
		return probeResult{
			probe:    "sudo",
			severity: sevWarn,
			detail:   fmt.Sprintf("could not probe sudo: %v", err),
		}
	}
	if exitCode == 0 {
		return probeResult{
			probe:    "sudo",
			severity: sevFail,
			detail:   "sudo -n true succeeded; passwordless sudo is reachable from this session",
		}
	}
	if sudo.PasswordRequired(stderr) {
		return probeResult{
			probe:    "sudo",
			severity: sevPass,
			detail:   "sudo requires a password (good)",
		}
	}
	return probeResult{
		probe:    "sudo",
		severity: sevWarn,
		detail:   fmt.Sprintf("sudo -n failed with exit %d but no password sentinel matched; stderr=%q", exitCode, oneLine(stderr)),
	}
}

// runCredEnvProbe walks every protected binary's credential_env names and
// reports which are set in this session. strict promotes hits from WARN
// to FAIL so a CI step can refuse to run with credentials on the bus.
func runCredEnvProbe(binaries []repocfg.ProtectedBinary, getenv envLookup, strict bool) []probeResult {
	type hit struct{ binary, env string }
	var hits []hit
	for _, b := range binaries {
		for _, name := range b.CredentialEnv {
			if getenv(name) != "" {
				hits = append(hits, hit{b.Name, name})
			}
		}
	}
	if len(hits) == 0 {
		return []probeResult{{probe: "credentials", severity: sevPass, detail: "no credential_env vars set"}}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].binary != hits[j].binary {
			return hits[i].binary < hits[j].binary
		}
		return hits[i].env < hits[j].env
	})
	sev := sevWarn
	if strict {
		sev = sevFail
	}
	out := make([]probeResult, 0, len(hits))
	for _, h := range hits {
		out = append(out, probeResult{
			probe:    "credentials",
			severity: sev,
			detail:   fmt.Sprintf("%s: %s is set in this session", h.binary, h.env),
		})
	}
	return out
}

// oneLine collapses whitespace runs into single spaces and trims, so a
// multi-line sudo stderr doesn't break per-line rendering.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
