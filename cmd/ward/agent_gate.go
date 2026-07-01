package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"
)

// agent_gate.go: the interactive pre-launch gate before the seedless TUI (ward#366).
// See docs/agent-gate.md for the full behavior, affordances, and seams.

// gateTerminalAttached is the TTY probe, behind a seam so tests drive the gated
// path without a real terminal. Production is the real terminalAttached.
var gateTerminalAttached = terminalAttached

// reExec is the process-replacing re-exec, behind a seam so tests intercept the
// upgrade re-launch instead of replacing the test binary.
var reExec = syscall.Exec

// gateChoice is the operator's pick at the pre-launch gate.
type gateChoice int

const (
	gateLaunch  gateChoice = iota // proceed straight into the TUI launch (Enter)
	gateUpgrade                   // upgrade the host ward, then re-launch
)

// scratchGateStatus is the compact pre-flight summary the gate renders before the
// alt-screen TUI takes the terminal - the facts printScratchPlan already knows.
type scratchGateStatus struct {
	access      string   // read-only | writable
	repo        string   // owner/repo slug
	mode        string   // --driver value (claude/codex/opencode/goose)
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
	fmt.Fprintf(&b, "  access:   %s\n", s.access)
	fmt.Fprintf(&b, "  repo:     %s\n", s.repo)
	fmt.Fprintf(&b, "  agent:    %s (%s)\n", s.agentBinary, s.mode)
	fmt.Fprintf(&b, "  image:    %s\n", s.image)
	fmt.Fprintf(&b, "  ward:     %s\n", s.wardVersion)
	if len(s.withRepos) > 0 {
		fmt.Fprintf(&b, "  with:     %s\n", strings.Join(s.withRepos, ", "))
	}
	b.WriteString("────────────────────────────────────────────────────\n")
	if s.behind {
		fmt.Fprintf(&b, "host ward %s is behind the latest release %s.\n", s.current, s.latest)
		b.WriteString("Press Enter to launch, or type u then Enter to upgrade ward and re-launch.\n")
	} else {
		b.WriteString("Press Enter to launch.\n")
	}
	_, _ = io.WriteString(w, b.String())
}

// readScratchGateChoice blocks for one input line and maps it to a choice: Enter
// launches; "u"/"upgrade" upgrades only when offered; EOF/other launch (never wedge).
func readScratchGateChoice(r io.Reader, offerUpgrade bool) gateChoice {
	line, _ := bufio.NewReader(r).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "u", "upgrade":
		if offerUpgrade {
			return gateUpgrade
		}
	}
	return gateLaunch
}

// runScratchGate renders the status block and waits for the operator's go before
// the launch (ward#366). proceed=false means an upgrade re-launch superseded it.
func (r *Runner) runScratchGate(ctx context.Context, c *cli.Command, plan upPlan, readOnly bool, label string) (proceed bool, err error) {
	latest, behind := r.wardOutdated(ctx)
	if !gateTerminalAttached() {
		// Headless/piped: no terminal to gate to. Keep the stale-ward heads-up
		// (ward#143) and fall straight through to the launch.
		if behind {
			_, _ = fmt.Fprint(r.gateErr(), wardOutdatedNotice(Version, latest))
		}
		return true, nil
	}
	renderScratchGate(r.gateErr(), newScratchGateStatus(plan, readOnly, behind, Version, latest))
	if readScratchGateChoice(r.gateIn(), behind) == gateUpgrade {
		return r.upgradeAndRelaunch(ctx, c, label)
	}
	return true, nil
}

// upgradeAndRelaunch runs `ward upgrade` then re-execs the freshly-installed
// canonical ward with the current argv (ward#366); see docs/agent-gate.md.
func (r *Runner) upgradeAndRelaunch(ctx context.Context, _ *cli.Command, label string) (proceed bool, err error) {
	w := r.gateErr()
	fmt.Fprintf(w, "%s: upgrading ward, then re-launching the same command...\n", label)
	if uerr := upgradeCommand().Run(ctx, []string{"upgrade"}); uerr != nil {
		return false, fmt.Errorf("%s: ward upgrade: %w", label, uerr)
	}
	path := canonicalWardPath()
	if path == "" {
		// Dev/source build: no canonical binary to re-exec - the acceptable v1.
		fmt.Fprintf(w, "%s: ward upgraded; re-run your command to launch on the new binary.\n", label)
		return false, nil
	}
	fmt.Fprintf(w, "%s: re-launching on the upgraded ward (%s)...\n", label, path)
	if xerr := reExec(path, os.Args, os.Environ()); xerr != nil {
		// A successful exec never returns; reaching here means the hand-off failed.
		fmt.Fprintf(w, "%s: re-exec failed (%v); ward is upgraded - re-run your command.\n", label, xerr)
		return false, nil
	}
	return false, nil // unreachable on a successful exec (the process is replaced)
}

// canonicalWardPath returns the first present canonical homebrew ward install path
// (guardBinaryPaths, the hook's allow-list), or "" on a dev/source build.
func canonicalWardPath() string {
	for _, p := range guardBinaryPaths["ward"] {
		// #nosec G304 -- stat of a fixed allow-list path; no file open follows.
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
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
