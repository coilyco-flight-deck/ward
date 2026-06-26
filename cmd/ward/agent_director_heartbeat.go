package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// agent_director_heartbeat.go is the LLM-in-the-loop heartbeat + interactive-surface
// spine of `ward agent director` (ward#351). See docs/agent-director.md.

// directorDecideTimeout caps the per-tick dispatch-decision one-shot so a wedged host
// agent can't hold a tick hostage; on timeout the tick falls back to rank.
const directorDecideTimeout = 3 * time.Minute

// directorBackend is the heartbeat's seam onto the world: the #346 ledger layer, the
// LLM decision, the dispatch path, and the interactive surface (tests inject a fake).
type directorBackend interface {
	// poll reconciles in-flight engineers against reality (reads WARD-OUTCOME).
	poll(ctx context.Context)
	// refresh rebuilds the ledger from the live backlog; errors are non-fatal here.
	refresh(ctx context.Context)
	// entries returns every tracked ledger entry across the scope.
	entries() []*backlogEntry
	// decide is the LLM one-shot: from the ranked picks, budget, and ledger state it
	// returns which entries to dispatch this tick (<= avail).
	decide(ctx context.Context, picks []*backlogEntry, avail int, entries []*backlogEntry) []*backlogEntry
	// dispatch launches one chosen issue's engineer carry and records the transition.
	dispatch(ctx context.Context, p *backlogEntry) error
	// surface hands control to an interactive session on drain; ran=false means none
	// was available (no terminal), so the loop exits cleanly.
	surface(ctx context.Context) (ran bool, err error)
	// sleep waits one heartbeat interval, returning early on cancellation.
	sleep(ctx context.Context, d time.Duration) error
	// reportDrained prints the "headless backlog drained" summary.
	reportDrained() error
	// reportMaxCycles notes a stop forced by --max-cycles.
	reportMaxCycles(queued, inflight int)
	// summary prints the terminal disposition by state.
	summary() error
}

// runDirectorLoop is the heartbeat: poll + reconcile, refresh, then surface (on drain)
// or LLM-decide + dispatch, then sleep. Loops until drained, --max-cycles, or cancel.
func runDirectorLoop(ctx context.Context, cfg backlogConfig, be directorBackend) error {
	for cycle := 1; ; cycle++ {
		// Deterministic half: reconcile in-flight, then pick up issues closed/promoted/
		// filed since the last pass. Both reuse #346's ledger layer.
		be.poll(ctx)
		be.refresh(ctx)

		entries := be.entries()
		queued, inflight := backlogLaneCounts(entries)

		// Drain -> surface, rather than exit (ward#351).
		if queued == 0 && inflight == 0 {
			stop, err := directorHandleDrain(ctx, be)
			if err != nil || stop {
				return err
			}
			continue
		}

		if cfg.maxCycles > 0 && cycle >= cfg.maxCycles {
			be.reportMaxCycles(queued, inflight)
			return be.summary()
		}

		// LLM half: one host one-shot decides which queued issues to dispatch, bounded
		// by the free-slot budget; dispatch the chosen set, then sleep cheaply.
		if err := directorDispatchTick(ctx, cfg, be, entries, inflight); err != nil {
			return err
		}
		if err := be.sleep(ctx, cfg.pollInterval); err != nil {
			return err
		}
	}
}

// directorHandleDrain reports the drained lane and surfaces an interactive session;
// stop=true ends the heartbeat (no session, or the human gave no new work).
func directorHandleDrain(ctx context.Context, be directorBackend) (bool, error) {
	if err := be.reportDrained(); err != nil {
		return false, err
	}
	ran, err := be.surface(ctx)
	if err != nil {
		return false, err
	}
	if !ran {
		return true, nil
	}
	// The human had the floor; re-check the lane. New headless work resumes the
	// heartbeat, else the drain is a deliberate stop.
	be.refresh(ctx)
	q2, i2 := backlogLaneCounts(be.entries())
	return q2 == 0 && i2 == 0, nil
}

// directorDispatchTick asks the LLM which queued issues to dispatch under the free-slot
// budget and dispatches the chosen set; a no-op when no slots are free or none queued.
func directorDispatchTick(ctx context.Context, cfg backlogConfig, be directorBackend, entries []*backlogEntry, inflight int) error {
	avail := cfg.maxParallel - inflight
	if avail <= 0 {
		return nil
	}
	picks := backlogQueuedPicks(entries)
	if len(picks) == 0 {
		return nil
	}
	for _, p := range be.decide(ctx, picks, avail, entries) {
		if err := be.dispatch(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// --- live backend ----------------------------------------------------------

// liveDirector is the production directorBackend, driving the real ledger, LLM one-shot,
// engineer dispatch, and architect surface for one director run.
type liveDirector struct {
	r     *Runner
	label string
	repos []string
	cfg   backlogConfig
}

func (d *liveDirector) poll(ctx context.Context) { d.r.backlogPoll(ctx, d.label, d.repos) }

// refresh is best-effort in-loop: a transient read error must not kill a heartbeat
// that is otherwise tracking live containers (the next tick retries).
func (d *liveDirector) refresh(ctx context.Context) {
	if err := d.r.backlogRefresh(ctx, d.label, d.repos, d.cfg.limit); err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: refresh failed (%v); continuing with the prior ledger\n", d.label, err)
	}
}

func (d *liveDirector) entries() []*backlogEntry { return d.r.backlogScopeEntries(d.repos) }

func (d *liveDirector) decide(ctx context.Context, picks []*backlogEntry, avail int, entries []*backlogEntry) []*backlogEntry {
	return d.r.directorDecide(ctx, d.label, d.cfg.mode, picks, avail, entries)
}

func (d *liveDirector) dispatch(ctx context.Context, p *backlogEntry) error {
	return d.r.backlogDispatchOne(ctx, d.label, d.cfg.carry, p)
}

func (d *liveDirector) surface(ctx context.Context) (bool, error) {
	return d.r.directorSurface(ctx, d.label, d.repos[0], d.cfg)
}

func (d *liveDirector) sleep(ctx context.Context, dur time.Duration) error {
	fmt.Fprintf(os.Stderr, "%s: heartbeat - sleeping %s before the next tick (no active LLM)...\n", d.label, dur)
	return backlogSleep(ctx, dur)
}

func (d *liveDirector) reportDrained() error { return d.r.backlogPrintDrained(d.label, d.repos) }

func (d *liveDirector) reportMaxCycles(queued, inflight int) {
	fmt.Fprintf(os.Stderr, "%s: reached --max-cycles %d (%d queued, %d in flight); stopping.\n",
		d.label, d.cfg.maxCycles, queued, inflight)
}

func (d *liveDirector) summary() error { return d.r.backlogPrintSummary(d.repos) }

// --- the LLM dispatch decision ---------------------------------------------

// directorDecide asks the mode's host agent which queued issues to dispatch this tick,
// bounded by avail; every fail-open path returns the deterministic rank floor (#346).
func (r *Runner) directorDecide(ctx context.Context, label string, mode containerMode, picks []*backlogEntry, avail int, entries []*backlogEntry) []*backlogEntry {
	floor := picks
	if len(floor) > avail {
		floor = floor[:avail]
	}
	bin := mode.agentBinary()
	argv, ok := mode.hostPreflightArgv(directorDecidePrompt(picks, avail, entries))
	if !ok || !hostHasBinary(bin) {
		fmt.Fprintf(os.Stderr, "%s: %s self-assessment unavailable; dispatching the top %d queued issue(s) by rank.\n", label, bin, len(floor))
		return floor
	}
	fmt.Fprintf(os.Stderr, "%s: heartbeat - asking %s which of %d queued issue(s) to dispatch (up to %d free slot(s))...\n\n", label, bin, len(picks), avail)
	dctx, cancel := context.WithTimeout(ctx, directorDecideTimeout)
	defer cancel()
	out, err := r.capturePreflight(dctx, argv)
	read := strings.TrimSpace(string(out))
	if read != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", read)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: dispatch-decision read did not complete (%v); dispatching the top %d by rank.\n", label, err, len(floor))
		return floor
	}
	nums, ok := parseDirectorDecision(read)
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: no clear DISPATCH verdict; dispatching the top %d by rank.\n", label, len(floor))
		return floor
	}
	chosen := selectDirectorPicks(picks, nums, avail)
	if len(chosen) == 0 {
		fmt.Fprintf(os.Stderr, "%s: the agent chose to hold - dispatching nothing this tick.\n", label)
	}
	return chosen
}

// directorDecidePrompt asks which queued issues to dispatch, given the ranked candidates,
// the budget, and in-flight + recent-outcome state, ending on a DISPATCH line. Pure.
func directorDecidePrompt(picks []*backlogEntry, avail int, entries []*backlogEntry) string {
	var b strings.Builder
	b.WriteString("You are the dispatch judgment in an autonomous backlog heartbeat. Each queued issue " +
		"you choose is carried to a merged PR by a fresh autonomous engineer, so dispatching costs a full run.\n\n")
	fmt.Fprintf(&b, "You may dispatch at most %d issue(s) this tick (the free-slot budget under --max-parallel).\n\n", avail)

	b.WriteString("QUEUED (ranked, highest first):\n")
	for _, p := range picks {
		fmt.Fprintf(&b, "- #%d [%s] %s\n", p.Num, tierOrDash(p.Tier), backlogTruncate(p.Title, 80))
	}

	if inflight := directorStateLines(entries, "dispatched"); len(inflight) > 0 {
		b.WriteString("\nIN FLIGHT (already dispatched, do not re-pick):\n")
		for _, ln := range inflight {
			fmt.Fprintf(&b, "- %s\n", ln)
		}
	}
	if recent := directorOutcomeLines(entries); len(recent) > 0 {
		b.WriteString("\nRECENT OUTCOMES:\n")
		for _, ln := range recent {
			fmt.Fprintf(&b, "- %s\n", ln)
		}
	}

	b.WriteString("\nDecide which queued issues to dispatch now. Prefer the highest-ranked, but you may hold " +
		"a tick (e.g. if recent runs are failing in a way that suggests waiting). Answer in 1-2 sentences, then " +
		"a final line of exactly one of:\n" +
		"  \"DISPATCH: <comma-separated issue numbers>\" - dispatch those queued issues (numbers MUST come from the QUEUED list);\n" +
		"  \"DISPATCH: none\" - hold this tick and dispatch nothing.\n")
	return b.String()
}

// directorStateLines renders the entries in a given state as "#N [tier] title" lines.
func directorStateLines(entries []*backlogEntry, state string) []string {
	var out []string
	for _, e := range entries {
		if e.State == state {
			out = append(out, fmt.Sprintf("#%d [%s] %s", e.Num, tierOrDash(e.Tier), backlogTruncate(e.Title, 70)))
		}
	}
	return out
}

// directorOutcomeLines renders the recent terminal dispositions (done/blocked/failed)
// with their outcome text, so the agent can weigh a failing streak.
func directorOutcomeLines(entries []*backlogEntry) []string {
	var out []string
	for _, e := range entries {
		switch e.State {
		case "done", "blocked", "failed":
			line := fmt.Sprintf("#%d -> %s", e.Num, e.State)
			if e.LastOutcome != nil && strings.TrimSpace(e.LastOutcome.Text) != "" {
				line += ": " + backlogTruncate(e.LastOutcome.Text, 100)
			}
			out = append(out, line)
		}
	}
	return out
}

// directorDispatchRE captures the issue list (or "none") on a DISPATCH verdict line,
// tolerating markdown decoration. Mirrors parseRouteVerdict / parsePreflightVerdict.
var directorDispatchRE = regexp.MustCompile(`(?i)^dispatch\b[\s:.\-–—]*(.*)$`)

// parseDirectorDecision reads the final DISPATCH line into issue numbers (last wins);
// ok=false means none found (fall open to rank), ok=true+empty is an explicit hold.
func parseDirectorDecision(read string) ([]int, bool) {
	var (
		nums  []int
		found bool
	)
	for _, raw := range strings.Split(read, "\n") {
		s := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "*_`>#-•· "))
		m := directorDispatchRE.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		found = true
		nums = parseIssueNumbers(m[1])
	}
	return nums, found
}

// parseIssueNumbers pulls every integer out of a free-form list; a list with no digits
// (an explicit hold) yields an empty slice.
func parseIssueNumbers(s string) []int {
	var out []int
	for _, tok := range regexp.MustCompile(`\d+`).FindAllString(s, -1) {
		if n, err := strconv.Atoi(tok); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// selectDirectorPicks keeps the picks the agent chose, in rank order, de-duped and
// capped at avail; a chosen number not in the queued set is ignored.
func selectDirectorPicks(picks []*backlogEntry, nums []int, avail int) []*backlogEntry {
	want := map[int]bool{}
	for _, n := range nums {
		want[n] = true
	}
	var out []*backlogEntry
	for _, p := range picks {
		if len(out) >= avail {
			break
		}
		if want[p.Num] {
			out = append(out, p)
		}
	}
	return out
}

// --- the interactive surface -----------------------------------------------

// directorSurface hands control to a read-only architect session on drain (ward#351).
// ran=false (no terminal) exits the loop; it inherits director's flags (ward#355).
func (r *Runner) directorSurface(ctx context.Context, label, contextRepo string, cfg backlogConfig) (bool, error) {
	if !terminalAttached() {
		fmt.Fprintf(os.Stderr, "%s: headless lane drained and no terminal attached; nothing to surface to, exiting.\n", label)
		return false, nil
	}
	fmt.Fprintf(os.Stderr, "%s: headless lane drained - surfacing a read-only %s session on %s for new direction "+
		"(the heartbeat resumes when the queue refills; exit it to stop)...\n\n", label, cfg.mode.agentBinary(), contextRepo)
	cmd := agentArchitectCommand()
	if err := cmd.Run(ctx, directorSurfaceArgv(contextRepo, cfg)); err != nil {
		return true, fmt.Errorf("%s: interactive surface session: %w", label, err)
	}
	return true, nil
}

// directorSurfaceArgv builds the architect-surface argv from director's forwarded flags.
// It runs on director's OWN --driver (cfg.mode), never the engineer driver (ward#355).
func directorSurfaceArgv(contextRepo string, cfg backlogConfig) []string {
	cy := cfg.carry
	argv := []string{architectSurface, "--repo", contextRepo, "--driver", string(cfg.mode)}
	if v := strings.TrimSpace(cy.image); v != "" {
		argv = append(argv, "--image", v)
	}
	if v := strings.TrimSpace(cy.tag); v != "" {
		argv = append(argv, "--tag", v)
	}
	if v := strings.TrimSpace(cy.wardVersion); v != "" {
		argv = append(argv, "--ward-version", v)
	}
	if v := strings.TrimSpace(cfg.wardSource); v != "" {
		argv = append(argv, "--ward-source", v)
	}
	if cy.aws {
		argv = append(argv, "--aws")
	}
	if cy.hostNet {
		argv = append(argv, "--host-net")
	}
	if cy.tsSidecar {
		argv = append(argv, "--ts-sidecar")
	}
	if cfg.noPull {
		argv = append(argv, "--no-pull")
	}
	for _, wr := range cfg.withRepo {
		if s := strings.TrimSpace(wr); s != "" {
			argv = append(argv, "--with-repo", s)
		}
	}
	return argv
}

// backlogPrintDrained reports the drained headless lane with its terminal disposition
// (done / parked-blocked / parked-failed, ...), shown before surfacing.
func (r *Runner) backlogPrintDrained(label string, repos []string) error {
	counts := map[string]int{}
	for _, e := range r.backlogScopeEntries(repos) {
		counts[e.State]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s: headless backlog drained - nothing queued or in flight (%s).\n", label, strings.Join(repos, ", "))
	for _, st := range []string{"done", "blocked", "failed", "surfaced", "skipped"} {
		if counts[st] > 0 {
			fmt.Fprintf(&b, "  %-10s %d\n", drainedStateLabel(st), counts[st])
		}
	}
	return r.emit(b.String())
}

// drainedStateLabel renames the parked states for the drain summary so a human reads
// "parked-blocked"/"parked-failed" rather than the bare ledger state.
func drainedStateLabel(state string) string {
	switch state {
	case "blocked":
		return "parked-blocked"
	case "failed":
		return "parked-failed"
	default:
		return state
	}
}
