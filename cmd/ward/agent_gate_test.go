package main

import (
	"bytes"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/shell"
)

// gateRunner builds a Runner whose stdin is a canned reader and whose stderr is a
// capture buffer, so the pre-launch gate (ward#366) is driven without a terminal.
func gateRunner(stdin string) (*Runner, *bytes.Buffer) {
	var errb bytes.Buffer
	return &Runner{Runner: &shell.Runner{
		Stdin:  strings.NewReader(stdin),
		Stderr: &errb,
	}}, &errb
}

// The status block names the resolved facts the launch used to scroll past: access,
// repo, agent binary + mode, image, ward version, and any --with-repo grants.
func TestRenderScratchGateContents(t *testing.T) {
	p := sampleUpPlan()
	p.ReadOnly = true
	p.WardVersionSource = wardVersionSourceExplicit
	p.ExtraRepos = []targetRepo{{Owner: "coilyco-flight-deck", Name: "umbra"}}
	var b bytes.Buffer
	renderScratchGate(&b, newScratchGateStatus(p, true, false, "v0.16.0", ""))
	got := b.String()
	for _, want := range []string{
		"read-only",                         // access
		"coilyco-gaming/sample-game",        // repo slug
		"claude (claude)",                   // agent binary (mode)
		p.Image,                             // resolved image
		"explicit pin v0.16.0",              // ward version pin
		"/gitcache/surface-scratch",         // read-only scratch root
		"go-build",                          // Go cache root under the scratch
		diskBytes(surfaceScratchFloorBytes), // budget floor
		"coilyco-flight-deck/umbra",         // --with-repo grant
		"Press Enter to launch",             // action prompt
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status block missing %q; got:\n%s", want, got)
		}
	}
}

// A writable plan with no grants says "writable" and omits the with-repo line.
func TestRenderScratchGateWritableNoExtras(t *testing.T) {
	var b bytes.Buffer
	renderScratchGate(&b, newScratchGateStatus(sampleUpPlan(), false, false, "v0.16.0", ""))
	got := b.String()
	if !strings.Contains(got, "writable") {
		t.Errorf("a writable plan must render access=writable; got:\n%s", got)
	}
	if !strings.Contains(got, "/scratch") {
		t.Errorf("a writable plan must show its scratch root; got:\n%s", got)
	}
	if strings.Contains(got, "with:") {
		t.Errorf("no --with-repo grants means no with: line; got:\n%s", got)
	}
}

// An empty/dev ward version pin renders the in-container-resolves note, never a bare
// blank the operator can't read.
func TestNewScratchGateStatusVersionFallback(t *testing.T) {
	p := sampleUpPlan()
	for _, pin := range []string{"", "dev"} {
		p.WardVersion = pin
		s := newScratchGateStatus(p, false, false, "v0.16.0", "")
		if !strings.Contains(s.wardVersion, "latest") {
			t.Errorf("ward version %q should fall back to a latest note; got %q", pin, s.wardVersion)
		}
	}
}

// A host-default ward version prints as host ward, while an explicit pin prints as
// an explicit pin so the startup log is unambiguous.
func TestRenderScratchGateVersionSources(t *testing.T) {
	var hostBuf, pinBuf bytes.Buffer
	hostPlan := sampleUpPlan()
	hostPlan.WardVersionSource = wardVersionSourceHost
	renderScratchGate(&hostBuf, newScratchGateStatus(hostPlan, false, false, "v0.16.0", ""))

	pinPlan := sampleUpPlan()
	pinPlan.WardVersionSource = wardVersionSourceExplicit
	renderScratchGate(&pinBuf, newScratchGateStatus(pinPlan, false, false, "v0.16.0", ""))

	if !strings.Contains(hostBuf.String(), "host ward v0.16.0") {
		t.Fatalf("host-default ward version should say host ward; got:\n%s", hostBuf.String())
	}
	if !strings.Contains(pinBuf.String(), "explicit pin v0.16.0") {
		t.Fatalf("explicit pin should say explicit pin; got:\n%s", pinBuf.String())
	}
}

// When ward is behind, the gate still launches, but it no longer advertises an
// upgrade path.
func TestRenderScratchGateOutdatedLaunchOnly(t *testing.T) {
	var behindBuf, currentBuf bytes.Buffer
	renderScratchGate(&behindBuf, newScratchGateStatus(sampleUpPlan(), false, true, "v0.16.0", "v0.17.0"))
	renderScratchGate(&currentBuf, newScratchGateStatus(sampleUpPlan(), false, false, "v0.17.0", ""))

	behind := behindBuf.String()
	if behind != currentBuf.String() {
		t.Fatalf("a behind ward should render the same launch-only gate; got:\n%s\n---\n%s", behind, currentBuf.String())
	}
	if !strings.Contains(behind, "Press Enter to launch") {
		t.Fatalf("gate missing launch prompt; got:\n%s", behind)
	}
	if strings.Contains(behind, "upgrade") {
		t.Errorf("the gate must not advertise upgrade anymore; got:\n%s", behind)
	}
}

// Enter (empty line) always launches, and any other input falls through to the
// same behavior so a stray keypress can't strand the operator.
func TestReadScratchGateChoice(t *testing.T) {
	cases := []struct {
		in   string
		want gateChoice
	}{
		{"\n", gateLaunch},
		{"", gateLaunch}, // EOF on closed stdin -> launch, never wedge
		{"u\n", gateLaunch},
		{"upgrade\n", gateLaunch},
		{"go\n", gateLaunch}, // anything else launches
	}
	for _, tc := range cases {
		if got := readScratchGateChoice(strings.NewReader(tc.in)); got != tc.want {
			t.Errorf("readScratchGateChoice(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// With a terminal attached, the gate renders the block and a bare Enter proceeds to
// launch. Version is "dev" under test, so behind is false and no network is touched.
func TestRunScratchGateTTYEnterLaunches(t *testing.T) {
	defer stubGateTTY(t, true)()
	r, errb := gateRunner("\n")
	r.runScratchGate(t.Context(), sampleUpPlan(), false)
	if !strings.Contains(errb.String(), "pre-launch") {
		t.Errorf("the gate must render its status block to stderr; got:\n%s", errb.String())
	}
}

// Without a terminal (headless/piped), the gate never reads stdin and never renders
// the block: it falls straight through to launch so a non-TTY stdin can't hang.
func TestRunScratchGateNoTTYFallsThrough(t *testing.T) {
	defer stubGateTTY(t, false)()
	// A stdin that would BLOCK if read, proving the non-TTY path never reads it.
	r, errb := gateRunner("u\n")
	r.runScratchGate(t.Context(), sampleUpPlan(), false)
	if strings.Contains(errb.String(), "pre-launch") {
		t.Errorf("a non-TTY launch must not render the gate block; got:\n%s", errb.String())
	}
}

// A ward pin older than this host's is refused by default (naming both versions); an
// equal-or-newer pin passes, and --allow-ward-downgrade opts past a downgrade (ward#529).
func TestWardDowngradeGuard(t *testing.T) {
	cases := []struct {
		name             string
		resolved, host   string
		allow            bool
		wantErr          bool
		wantNamesInError []string
	}{
		{name: "downgrade refused", resolved: "v0.297.0", host: "v0.298.0", wantErr: true, wantNamesInError: []string{"v0.297.0", "v0.298.0"}},
		{name: "downgrade minor", resolved: "v0.298.0", host: "v0.299.5", wantErr: true, wantNamesInError: []string{"v0.298.0", "v0.299.5"}},
		{name: "equal allowed", resolved: "v0.298.0", host: "v0.298.0"},
		{name: "newer allowed", resolved: "v0.299.0", host: "v0.298.0"},
		{name: "downgrade opted in", resolved: "v0.297.0", host: "v0.298.0", allow: true},
		{name: "dev host never refuses", resolved: "v0.297.0", host: "dev"},
		{name: "dev pin never refuses", resolved: "dev", host: "v0.298.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := wardDowngradeGuard(tc.resolved, tc.host, tc.allow)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wardDowngradeGuard(%q, %q, allow=%v) err=%v, wantErr=%v", tc.resolved, tc.host, tc.allow, err, tc.wantErr)
			}
			for _, want := range tc.wantNamesInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal must name %q; got: %v", want, err)
				}
			}
		})
	}
}

// stubGateTTY swaps the gate's terminal probe for the run, restoring it on cleanup.
func stubGateTTY(t *testing.T, attached bool) func() {
	t.Helper()
	prev := gateTerminalAttached
	gateTerminalAttached = func() bool { return attached }
	return func() { gateTerminalAttached = prev }
}
