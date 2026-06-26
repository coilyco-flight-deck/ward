package main

import (
	"bytes"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
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
	p.ExtraRepos = []targetRepo{{Owner: "coilyco-flight-deck", Name: "cli-guard"}}
	var b bytes.Buffer
	renderScratchGate(&b, newScratchGateStatus(p, true, false, "v0.16.0", ""))
	got := b.String()
	for _, want := range []string{
		"read-only",                     // access
		"coilyco-gaming/eco-app",        // repo slug
		"claude (claude)",               // agent binary (mode)
		p.Image,                         // resolved image
		"v0.16.0",                       // ward version pin
		"coilyco-flight-deck/cli-guard", // --with-repo grant
		"Press Enter to launch",         // action prompt
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

// When ward is behind, the gate surfaces the version delta and offers the upgrade
// affordance; when current, it offers only the launch.
func TestRenderScratchGateOutdatedOffersUpgrade(t *testing.T) {
	var behindBuf, currentBuf bytes.Buffer
	renderScratchGate(&behindBuf, newScratchGateStatus(sampleUpPlan(), false, true, "v0.16.0", "v0.17.0"))
	renderScratchGate(&currentBuf, newScratchGateStatus(sampleUpPlan(), false, false, "v0.17.0", ""))

	behind := behindBuf.String()
	for _, want := range []string{"v0.16.0", "v0.17.0", "behind", "upgrade"} {
		if !strings.Contains(behind, want) {
			t.Errorf("outdated gate missing %q; got:\n%s", want, behind)
		}
	}
	if cur := currentBuf.String(); strings.Contains(cur, "upgrade") || strings.Contains(cur, "behind") {
		t.Errorf("a current ward must not mention upgrade/behind; got:\n%s", cur)
	}
}

// Enter (empty line) launches; "u" upgrades only when the affordance is offered, and
// otherwise falls through to launch so a stray keypress can't strand the operator.
func TestReadScratchGateChoice(t *testing.T) {
	cases := []struct {
		in           string
		offerUpgrade bool
		want         gateChoice
	}{
		{"\n", true, gateLaunch},
		{"\n", false, gateLaunch},
		{"", true, gateLaunch}, // EOF on closed stdin -> launch, never wedge
		{"u\n", true, gateUpgrade},
		{"U\n", true, gateUpgrade},
		{"upgrade\n", true, gateUpgrade},
		{"u\n", false, gateLaunch}, // not offered: u is inert
		{"go\n", true, gateLaunch}, // anything else launches
	}
	for _, tc := range cases {
		if got := readScratchGateChoice(strings.NewReader(tc.in), tc.offerUpgrade); got != tc.want {
			t.Errorf("readScratchGateChoice(%q, offer=%v) = %d, want %d", tc.in, tc.offerUpgrade, got, tc.want)
		}
	}
}

// With a terminal attached, the gate renders the block and a bare Enter proceeds to
// launch. Version is "dev" under test, so behind is false and no network is touched.
func TestRunScratchGateTTYEnterLaunches(t *testing.T) {
	defer stubGateTTY(t, true)()
	r, errb := gateRunner("\n")
	proceed, err := r.runScratchGate(t.Context(), nil, sampleUpPlan(), false, "ward agent architect")
	if err != nil {
		t.Fatalf("runScratchGate: %v", err)
	}
	if !proceed {
		t.Error("a bare Enter at the gate must proceed to launch")
	}
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
	proceed, err := r.runScratchGate(t.Context(), nil, sampleUpPlan(), false, "ward agent architect")
	if err != nil {
		t.Fatalf("runScratchGate: %v", err)
	}
	if !proceed {
		t.Error("a non-TTY launch must fall straight through to launch")
	}
	if strings.Contains(errb.String(), "pre-launch") {
		t.Errorf("a non-TTY launch must not render the gate block; got:\n%s", errb.String())
	}
}

// stubGateTTY swaps the gate's terminal probe for the run, restoring it on cleanup.
func stubGateTTY(t *testing.T, attached bool) func() {
	t.Helper()
	prev := gateTerminalAttached
	gateTerminalAttached = func() bool { return attached }
	return func() { gateTerminalAttached = prev }
}
