package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/version"
)

// agent_gate.go: the interactive pre-launch gate before the seedless TUI (ward#366).
// See docs/agent-gate.md for the full behavior, affordances, and seams.

// gateTerminalAttached is the TTY probe, behind a seam so tests drive the gated
// path without a real terminal. Production is the real terminalAttached.
var gateTerminalAttached = terminalAttached

// gateChoice is the operator's pick at the pre-launch gate.
type gateChoice int

const (
	gateLaunch gateChoice = iota // proceed straight into the TUI launch (Enter)
)

// wardDowngradeGuard refuses a container ward pin older than the dispatching host: it
// ships an older, buggy reaper (docs/agent-ward-downgrade.md, ward#529). allow opts in.
func wardDowngradeGuard(resolved, host string, allow bool) error {
	if !version.Behind(resolved, host) {
		return nil // equal, newer, or nothing to compare against - fine.
	}
	if allow {
		return nil // operator explicitly opted into the older reaper.
	}
	return fmt.Errorf(
		"refusing to dispatch a container pinned to ward %s, older than this host's ward %s: "+
			"the in-container reaper is the last line against lost or false-salvaged work, and an "+
			"older reaper can reproduce already-fixed bugs (ward#529). Pass --allow-ward-downgrade "+
			"to override, or clear --ward-version / WARD_AGENT_VERSION to inherit this host's ward",
		resolved, host)
}

// scratchGateStatus is the compact pre-flight summary the gate renders before the
// alt-screen TUI takes the terminal - the facts printScratchPlan already knows.
type scratchGateStatus struct {
	access      string   // read-only | writable
	repo        string   // owner/repo slug
	mode        string   // --harness value (claude/codex/opencode/goose)
	agentBinary string   // the in-container binary the mode launches
	image       string   // resolved docker image
	wardVersion string   // the ward release the container will run
	withRepos   []string // --with-repo grants landed alongside the primary repo
	behind      bool     // the host ward binary is behind the latest release
	current     string   // the host ward version
	latest      string   // the latest ward release tag
}

// newScratchGateStatus distills a resolved plan into the gate's status facts;
// behind/current/latest carry the stale-ward read for affordance B.
func newScratchGateStatus(p upPlan, readOnly, behind bool, current, latest string) scratchGateStatus {
	access := "writable"
	if readOnly {
		access = "read-only"
	}
	// "" or "dev" means the entrypoint resolves latest in-container - say so rather
	// than print a bare blank the operator can't act on.
	wv := strings.TrimSpace(p.WardVersion)
	if wv == "" || wv == "dev" {
		wv = "latest (resolved in-container)"
	}
	extras := make([]string, 0, len(p.ExtraRepos))
	for _, e := range p.ExtraRepos {
		extras = append(extras, e.slug())
	}
	return scratchGateStatus{
		access:      access,
		repo:        p.Repo.slug(),
		mode:        string(p.Mode),
		agentBinary: lookupAgent(p.Mode).Record().Binary,
		image:       p.Image,
		wardVersion: wv,
		withRepos:   extras,
		behind:      behind,
		current:     current,
		latest:      latest,
	}
}

// renderScratchGate writes the status block + action prompt to w. Pure and
// io.Writer-driven so tests assert the contents without a terminal.
func renderScratchGate(w io.Writer, s scratchGateStatus) {
	var b strings.Builder
	b.WriteString("\n── ward pre-launch ─────────────────────────────────\n")
	writef(&b, "  access:   %s\n", s.access)
	writef(&b, "  repo:     %s\n", s.repo)
	writef(&b, "  agent:    %s (%s)\n", s.agentBinary, s.mode)
	writef(&b, "  image:    %s\n", s.image)
	writef(&b, "  ward:     %s\n", s.wardVersion)
	if len(s.withRepos) > 0 {
		writef(&b, "  with:     %s\n", strings.Join(s.withRepos, ", "))
	}
	// The read-only catalog.dependsOn context set is resolved in-container from the
	// fresh clone (ward#580), so the host cannot name it here before launch.
	b.WriteString("  context:  catalog.dependsOn resolved in-container (read-only)\n")
	b.WriteString("────────────────────────────────────────────────────\n")
	if s.behind {
		writef(&b, "host ward %s is behind the latest release %s.\n", s.current, s.latest)
	}
	b.WriteString("Press Enter to launch.\n")
	_, _ = io.WriteString(w, b.String())
}

// readScratchGateChoice blocks for one input line and always launches. The gate
// is informational now.
func readScratchGateChoice(r io.Reader) gateChoice {
	_, _ = bufio.NewReader(r).ReadString('\n')
	return gateLaunch
}

// runScratchGate renders the status block and waits for the operator's go before
// the launch (ward#366).
func (r *Runner) runScratchGate(ctx context.Context, plan upPlan, readOnly bool) {
	latest, behind := r.wardOutdated(ctx)
	if !gateTerminalAttached() {
		// Headless/piped: no terminal to gate to. Keep the stale-ward heads-up
		// (ward#143) and fall straight through to the launch.
		if behind {
			writef(r.gateErr(), "%s", wardOutdatedNotice(Version, latest))
		}
		return
	}
	renderScratchGate(r.gateErr(), newScratchGateStatus(plan, readOnly, behind, Version, latest))
	_ = readScratchGateChoice(r.gateIn())
}

// gateErr is the gate's status-comms writer (stderr), falling back to os.Stderr.
func (r *Runner) gateErr() io.Writer {
	if r != nil && r.Runner != nil && r.Runner.Stderr != nil {
		return r.Runner.Stderr
	}
	return os.Stderr
}

// gateIn is the gate's operator-input reader (stdin), behind the Runner for tests.
func (r *Runner) gateIn() io.Reader {
	if r != nil && r.Runner != nil && r.Runner.Stdin != nil {
		return r.Runner.Stdin
	}
	return os.Stdin
}
