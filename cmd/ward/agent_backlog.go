package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// agent_backlog.go is `ward agent backlog`: an autonomous supervised loop draining
// a repo's headless lane (ward#346, subsuming ward#310). See docs/agent-backlog.md.

// wardOutcomeMarker leads a headless run's final comment (ward#310); the loop reads
// only this line to classify the run. See docs/agent-backlog.md.
const wardOutcomeMarker = "WARD-OUTCOME:"

// backlogLedgerSubdir is the directory under ~/.ward holding one durable per-repo
// ledger, so a killed loop resumes from disk rather than re-deriving state.
const backlogLedgerSubdir = "backlog"

// backlogTierOrder ranks tiers high-to-low; backlogModeOrder is the stable
// mode precedence. Mirrors the tooling-issue-prioritization label vocabulary.
var (
	backlogTierOrder = []string{"P0", "P1", "P2", "P3", "P4"}
	backlogModeOrder = []string{"headless", "interactive", "consult"}
	// backlogModeLane maps a mode label to the loop lane it feeds.
	backlogModeLane = map[string]string{"headless": "headless", "interactive": "interactive", "consult": "consult"}
	// backlogLanes is the print/iteration order of the lanes the loop tracks.
	backlogLanes = []string{"headless", "interactive", "consult", "untriaged"}
)

// backlogIssue is one open issue read from the live backlog, the ranking input.
type backlogIssue struct {
	Number int
	Title  string
	Labels []string
	URL    string
}

// rankedBacklogIssue annotates an issue with its tier/mode/lane after ranking.
type rankedBacklogIssue struct {
	Num   int
	Title string
	Tier  string
	Mode  string
	Lane  string
	URL   string
}

// backlogOutcome is the parsed WARD-OUTCOME status of a finished run.
type backlogOutcome struct {
	Status string `yaml:"status"`
	Text   string `yaml:"text"`
}

// backlogEntry is one tracked issue in a repo's ledger: its ranked metadata plus
// the loop state it moves through (queued -> dispatched -> done/blocked/failed).
type backlogEntry struct {
	Num          int             `yaml:"num"`
	Title        string          `yaml:"title"`
	Tier         string          `yaml:"tier,omitempty"`
	Mode         string          `yaml:"mode,omitempty"`
	Lane         string          `yaml:"lane"`
	URL          string          `yaml:"url,omitempty"`
	State        string          `yaml:"state"`
	Container    string          `yaml:"container,omitempty"`
	DispatchedAt string          `yaml:"dispatched_at,omitempty"`
	LastOutcome  *backlogOutcome `yaml:"last_outcome,omitempty"`
	// repo is the owning slug, set only when entries are aggregated across a scope.
	repo string `yaml:"-"`
}

// backlogLedger is one repo's durable state file (YAML under ~/.ward/backlog).
type backlogLedger struct {
	Repo    string                   `yaml:"repo"`
	Updated string                   `yaml:"updated"`
	Issues  map[string]*backlogEntry `yaml:"issues"`
}

// backlogConfig is the resolved knob set for one `ward agent backlog` run.
type backlogConfig struct {
	mode         containerMode
	maxParallel  int
	limit        int
	pollInterval time.Duration
	maxCycles    int
	dryRun       bool
	triage       bool
}

// agentBacklogCommand wires `ward agent backlog` (registered in agentCommand,
// audited via WrapVerb, trust-gated through ownerAllowed). See docs/agent-backlog.md.
func agentBacklogCommand() *cli.Command {
	return &cli.Command{
		Name:      "backlog",
		Usage:     "Autonomously drive a repo's headless lane to drain: refresh, dispatch, poll, reconcile (ward#346).",
		ArgsUsage: "(scope via --repo; default: the cwd git origin)",
		Description: `backlog runs an autonomous supervised loop over a repo's open backlog. Each cycle
it refreshes the ledger from the live backlog (ranking issues into lanes by
tier/mode labels), dispatches queued headless-lane issues up to --max-parallel via
ward's native headless carry, polls the dispatched containers, reads each one's
WARD-OUTCOME comment to classify done/blocked/failed, then repeats until the
headless lane drains or a limit is hit.

  warded backlog --repo coilyco-flight-deck/ward         # one repo
  warded backlog --repo a/b,c/d --max-parallel 3         # comma-separated scope
  warded backlog --dry-run                                # ranked lanes + planned dispatches, launch nothing

State lives in a durable per-repo ledger under ~/.ward/backlog, so a killed loop
resumes from disk. Only the narrow headless lane is auto-dispatched; interactive
and consult issues are surfaced, not launched. See docs/agent-backlog.md.`,
		Flags: []cli.Flag{
			agentDriverFlag(),
			&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: the cwd git origin)"},
			&cli.IntFlag{Name: "max-parallel", Value: 2, Usage: "in-flight container cap"},
			&cli.BoolFlag{Name: "triage", Usage: "run `ward exec goose-triage` across the scope before the first refresh"},
			&cli.IntFlag{Name: "limit", Value: 50, Usage: "open issues read per repo per refresh"},
			&cli.DurationFlag{Name: "poll-interval", Value: 30 * time.Second, Usage: "wait between dispatch/poll cycles"},
			&cli.IntFlag{Name: "max-cycles", Value: 0, Usage: "stop after N cycles (0 = run until the lane drains)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show the ranked lanes + planned dispatches, then exit without launching"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentDriver(c)
			if err != nil {
				return fmt.Errorf("ward agent backlog: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".backlog",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentBacklog(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentBacklog resolves + trust-gates the scope, then drives the loop.
func (r *Runner) runAgentBacklog(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "backlog")
	def := ""
	if repo, _, err := r.resolveTarget(ctx, ""); err == nil {
		def = repo.slug()
	}
	repos := parseScopeRepos(c.String("repo"), def)
	if len(repos) == 0 {
		return fmt.Errorf("%s: no --repo given and no git origin found in the current directory", label)
	}
	if err := r.backlogTrustGate(label, repos); err != nil {
		return err
	}
	cfg := backlogConfig{
		mode:         mode,
		maxParallel:  c.Int("max-parallel"),
		limit:        c.Int("limit"),
		pollInterval: c.Duration("poll-interval"),
		maxCycles:    c.Int("max-cycles"),
		dryRun:       c.Bool("dry-run"),
		triage:       c.Bool("triage"),
	}
	if cfg.maxParallel < 1 {
		cfg.maxParallel = 1
	}
	return r.driveBacklog(ctx, label, repos, cfg)
}

// backlogTrustGate refuses the run unless every scope repo is a well-formed
// owner/name owned by a trusted org (mirrors work/headless's ownerAllowed check).
func (r *Runner) backlogTrustGate(label string, repos []string) error {
	for _, slug := range repos {
		owner, name, ok := strings.Cut(slug, "/")
		if !ok || owner == "" || name == "" {
			return fmt.Errorf("%s: invalid repo %q in scope (want owner/name)", label, slug)
		}
		if !r.ownerAllowed(owner) {
			return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
				label, owner, strings.Join(r.primaryOrgs(), ", "))
		}
	}
	return nil
}

// driveBacklog is the loop body: optional triage, refresh, then dispatch/poll
// cycles until the headless lane drains or a limit is hit, then a summary.
func (r *Runner) driveBacklog(ctx context.Context, label string, repos []string, cfg backlogConfig) error {
	if cfg.triage && !cfg.dryRun {
		r.backlogTriage(ctx, label, repos)
	}
	if err := r.backlogRefresh(ctx, label, repos, cfg.limit); err != nil {
		return err
	}
	if err := r.backlogPrintStatus(repos); err != nil {
		return err
	}
	if cfg.dryRun {
		return r.backlogPrintPlanned(label, repos, cfg.maxParallel)
	}
	for cycle := 1; ; cycle++ {
		if err := r.backlogDispatchCycle(ctx, label, repos, cfg); err != nil {
			return err
		}
		stop, err := r.backlogCycleWait(ctx, label, repos, cfg, cycle)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}
	return r.backlogPrintSummary(repos)
}

// backlogCycleWait stops the loop when the lane is drained or --max-cycles is hit;
// otherwise it waits --poll-interval, polls, and re-refreshes. stop=true ends it.
func (r *Runner) backlogCycleWait(ctx context.Context, label string, repos []string, cfg backlogConfig, cycle int) (bool, error) {
	queued, inflight := backlogLaneCounts(r.backlogScopeEntries(repos))
	if queued == 0 && inflight == 0 {
		fmt.Fprintf(os.Stderr, "%s: headless lane drained - nothing queued or in flight.\n", label)
		return true, nil
	}
	if cfg.maxCycles > 0 && cycle >= cfg.maxCycles {
		fmt.Fprintf(os.Stderr, "%s: reached --max-cycles %d (%d queued, %d in flight); stopping.\n",
			label, cfg.maxCycles, queued, inflight)
		return true, nil
	}
	fmt.Fprintf(os.Stderr, "%s: cycle %d - %d in flight, %d queued; polling in %s ...\n",
		label, cycle, inflight, queued, cfg.pollInterval)
	if err := backlogSleep(ctx, cfg.pollInterval); err != nil {
		return false, err
	}
	r.backlogPoll(ctx, label, repos)
	// Re-refresh so issues closed/promoted/filed since the last pass are picked up;
	// a transient read error is non-fatal (the next cycle retries).
	if err := r.backlogRefresh(ctx, label, repos, cfg.limit); err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: refresh failed (%v); continuing with the prior ledger\n", label, err)
	}
	return false, nil
}

// out returns the run's user-facing writer (lanes, summary), falling back to stdout.
func (r *Runner) out() io.Writer {
	if r.Runner != nil && r.Runner.Stdout != nil {
		return r.Runner.Stdout
	}
	return os.Stdout
}

// emit writes a rendered report block to the run's writer in one call.
func (r *Runner) emit(s string) error {
	_, err := io.WriteString(r.out(), s)
	return err
}

// --- scope parsing ---------------------------------------------------------

// parseScopeRepos resolves the scope: a comma-separated --repo list, else the
// git-origin default; de-duped, order-preserving. Ports backlog-loop.py's parse_repos.
func parseScopeRepos(raw, def string) []string {
	if strings.TrimSpace(raw) == "" {
		raw = def
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var seen []string
	for _, slug := range strings.Split(raw, ",") {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		dup := false
		for _, s := range seen {
			if s == slug {
				dup = true
				break
			}
		}
		if !dup {
			seen = append(seen, slug)
		}
	}
	return seen
}

// --- ranking ---------------------------------------------------------------

// backlogTierOf returns the highest-ranked tier label present, or "".
func backlogTierOf(labels []string) string {
	for _, t := range backlogTierOrder {
		for _, l := range labels {
			if l == t {
				return t
			}
		}
	}
	return ""
}

// backlogModeOf returns the first mode label present (stable precedence), or "".
func backlogModeOf(labels []string) string {
	for _, m := range backlogModeOrder {
		for _, l := range labels {
			if l == m {
				return m
			}
		}
	}
	return ""
}

// backlogLaneForLabels maps (tier, mode) to a lane; a missing label is untriaged.
func backlogLaneForLabels(tier, mode string) string {
	if tier == "" || mode == "" {
		return "untriaged"
	}
	if lane, ok := backlogModeLane[mode]; ok {
		return lane
	}
	return "consult"
}

// rankBacklogIssues tags each issue with tier/mode/lane and sorts by lane, then
// tier, then number. Ports backlog-loop.py's rank (no triage-score tie-break yet).
func rankBacklogIssues(issues []backlogIssue) []rankedBacklogIssue {
	laneRank := map[string]int{"headless": 0, "interactive": 1, "consult": 2, "untriaged": 3}
	out := make([]rankedBacklogIssue, 0, len(issues))
	for _, it := range issues {
		tier := backlogTierOf(it.Labels)
		mode := backlogModeOf(it.Labels)
		out = append(out, rankedBacklogIssue{
			Num:   it.Number,
			Title: it.Title,
			Tier:  tier,
			Mode:  mode,
			Lane:  backlogLaneForLabels(tier, mode),
			URL:   it.URL,
		})
	}
	rankOf := func(m map[string]int, k string, miss int) int {
		if v, ok := m[k]; ok {
			return v
		}
		return miss
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if la, lb := rankOf(laneRank, a.Lane, 9), rankOf(laneRank, b.Lane, 9); la != lb {
			return la < lb
		}
		if ta, tb := backlogTierIndex(a.Tier), backlogTierIndex(b.Tier); ta != tb {
			return ta < tb
		}
		return a.Num < b.Num
	})
	return out
}

// refreshBacklogLedger folds a fresh ranked backlog into the ledger, preserving
// in-flight state and dropping closed non-mid-flight issues. Ports refresh_ledger.
func refreshBacklogLedger(led *backlogLedger, ranked []rankedBacklogIssue) {
	if led.Issues == nil {
		led.Issues = map[string]*backlogEntry{}
	}
	seen := map[int]bool{}
	for _, rk := range ranked {
		seen[rk.Num] = true
		applyRankedBacklogEntry(led, rk)
	}
	dropClosedBacklogEntries(led, seen)
}

// backlogNewEntryState is the state a freshly-seen issue lands in by lane.
func backlogNewEntryState(lane string) string {
	switch lane {
	case "headless":
		return "queued"
	case "interactive":
		return "surfaced"
	default:
		return "skipped"
	}
}

// applyRankedBacklogEntry upserts one ranked issue into the ledger, seeding a new
// entry's state by lane and re-queuing one a re-triage promoted into headless.
func applyRankedBacklogEntry(led *backlogLedger, rk rankedBacklogIssue) {
	key := strconv.Itoa(rk.Num)
	entry := led.Issues[key]
	switch {
	case entry == nil:
		entry = &backlogEntry{State: backlogNewEntryState(rk.Lane)}
	case rk.Lane == "headless" && (entry.State == "skipped" || entry.State == "surfaced"):
		// A re-triage promoted this into headless from a non-in-flight holding
		// state: re-queue it rather than strand it.
		entry.State = "queued"
	}
	entry.Num = rk.Num
	entry.Title = rk.Title
	entry.Tier = rk.Tier
	entry.Mode = rk.Mode
	entry.Lane = rk.Lane
	entry.URL = rk.URL
	led.Issues[key] = entry
}

// dropClosedBacklogEntries removes entries gone from the live set, unless still
// mid-flight (a dispatched/blocked/failed issue stays visible until reconciled).
func dropClosedBacklogEntries(led *backlogLedger, seen map[int]bool) {
	for key, e := range led.Issues {
		n, _ := strconv.Atoi(key)
		if seen[n] {
			continue
		}
		switch e.State {
		case "done", "skipped", "surfaced":
			delete(led.Issues, key)
		}
	}
}

// --- outcome parsing -------------------------------------------------------

// backlogOutcomeRE parses the status + reason that follow the WARD-OUTCOME marker.
var backlogOutcomeRE = regexp.MustCompile(`(?i)^(done|blocked|failed)\b[\s:.\-]*(.*)`)

// parseBacklogOutcome classifies the latest comment leading with WARD-OUTCOME,
// nil when none. Ports backlog-loop.py's parse_outcome.
func parseBacklogOutcome(comments []issueComment) *backlogOutcome {
	type hit struct {
		at time.Time
		o  backlogOutcome
	}
	var hits []hit
	for _, c := range comments {
		o, ok := backlogOutcomeOfComment(c.Body)
		if !ok {
			continue
		}
		hits = append(hits, hit{at: c.CreatedAt, o: o})
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at.Before(hits[j].at) })
	return &hits[len(hits)-1].o
}

// backlogOutcomeOfComment parses the WARD-OUTCOME status from one comment body,
// reporting ok=false when the body carries no leading marker line.
func backlogOutcomeOfComment(body string) (backlogOutcome, bool) {
	var line string
	for _, ln := range strings.Split(body, "\n") {
		s := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), ">*-•# "))
		if strings.HasPrefix(strings.ToUpper(s), wardOutcomeMarker) {
			line = s
			break
		}
	}
	if line == "" {
		return backlogOutcome{}, false
	}
	rest := strings.TrimSpace(line[len(wardOutcomeMarker):])
	o := backlogOutcome{Status: "unknown", Text: rest}
	if m := backlogOutcomeRE.FindStringSubmatch(rest); m != nil {
		o.Status = strings.ToLower(m[1])
		o.Text = strings.TrimSpace(m[2])
	}
	o.Text = backlogTruncate(o.Text, 500)
	return o, true
}

// backlogOutcomeState maps a parsed outcome status to the ledger state it lands in;
// an unrecognized status parks as blocked (a human should look). Ports poll_repo.
func backlogOutcomeState(status string) string {
	switch status {
	case "done":
		return "done"
	case "failed":
		return "failed"
	case "blocked":
		return "blocked"
	default:
		return "blocked"
	}
}

// --- ledger persistence ----------------------------------------------------

// backlogLedgerPath resolves ~/.ward/backlog/<owner-name>.yaml for a repo slug.
func backlogLedgerPath(repo string) (string, error) {
	dir, err := config.GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, backlogLedgerSubdir, config.SanitizeSlug(repo)+".yaml"), nil
}

// loadBacklogLedger reads a repo's ledger, returning a fresh empty one when absent.
func loadBacklogLedger(repo string) (*backlogLedger, error) {
	path, err := backlogLedgerPath(repo)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path) // #nosec G304 -- path is ward-derived under ~/.ward
	if errors.Is(err, os.ErrNotExist) {
		return &backlogLedger{Repo: repo, Issues: map[string]*backlogEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backlog: read ledger %s: %w", path, err)
	}
	led := &backlogLedger{}
	if err := yaml.Unmarshal(b, led); err != nil {
		return nil, fmt.Errorf("backlog: parse ledger %s: %w", path, err)
	}
	if led.Repo == "" {
		led.Repo = repo
	}
	if led.Issues == nil {
		led.Issues = map[string]*backlogEntry{}
	}
	return led, nil
}

// saveBacklogLedger persists a ledger atomically (temp file + rename), stamping the
// update time so a killed loop's last-known state is dated.
func saveBacklogLedger(led *backlogLedger) error {
	led.Updated = time.Now().UTC().Format(time.RFC3339)
	path, err := backlogLedgerPath(led.Repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("backlog: create ledger dir: %w", err)
	}
	b, err := yaml.Marshal(led)
	if err != nil {
		return fmt.Errorf("backlog: marshal ledger %s: %w", led.Repo, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("backlog: write ledger %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("backlog: replace ledger %s: %w", path, err)
	}
	return nil
}

// updateBacklogEntry reloads a repo's ledger, applies fn to the entry for num
// (bare one if absent), and saves - a reload-per-step to avoid clobbering siblings.
func (r *Runner) updateBacklogEntry(repo string, num int, fn func(*backlogEntry)) error {
	led, err := loadBacklogLedger(repo)
	if err != nil {
		return err
	}
	key := strconv.Itoa(num)
	entry := led.Issues[key]
	if entry == nil {
		entry = &backlogEntry{Num: num, Lane: "headless"}
		led.Issues[key] = entry
	}
	fn(entry)
	return saveBacklogLedger(led)
}

// --- scope aggregation -----------------------------------------------------

// backlogScopeEntries returns every tracked entry across the scope, each tagged
// with its owning repo. A repo whose ledger fails to load is skipped with a note.
func (r *Runner) backlogScopeEntries(repos []string) []*backlogEntry {
	var out []*backlogEntry
	for _, repo := range repos {
		led, err := loadBacklogLedger(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backlog: note: skipping %s (%v)\n", repo, err)
			continue
		}
		for _, e := range led.Issues {
			e.repo = repo
			out = append(out, e)
		}
	}
	return out
}

// backlogLaneCounts tallies the headless work still outstanding: queued (ready to
// dispatch) and in flight (dispatched). The loop drains when both reach zero.
func backlogLaneCounts(entries []*backlogEntry) (queued, inflight int) {
	for _, e := range entries {
		switch e.State {
		case "queued":
			queued++
		case "dispatched":
			inflight++
		}
	}
	return queued, inflight
}

// backlogQueuedPicks returns the queued headless entries across the scope, ranked
// (tier then repo then number), ready to dispatch.
func backlogQueuedPicks(entries []*backlogEntry) []*backlogEntry {
	var picks []*backlogEntry
	for _, e := range entries {
		if e.State == "queued" {
			picks = append(picks, e)
		}
	}
	sort.SliceStable(picks, func(i, j int) bool { return backlogEntryLess(picks[i], picks[j]) })
	return picks
}

// backlogEntryLess ranks two entries by tier, then repo, then issue number - the
// shared order for queued picks and the lane-grouped status print.
func backlogEntryLess(a, b *backlogEntry) bool {
	if ti, tj := backlogTierIndex(a.Tier), backlogTierIndex(b.Tier); ti != tj {
		return ti < tj
	}
	if a.repo != b.repo {
		return a.repo < b.repo
	}
	return a.Num < b.Num
}

// backlogTierIndex ranks a tier label (unknown sorts last).
func backlogTierIndex(tier string) int {
	for i, t := range backlogTierOrder {
		if t == tier {
			return i
		}
	}
	return len(backlogTierOrder)
}

// --- loop steps ------------------------------------------------------------

// backlogTriage runs `ward exec goose-triage` across the scope so the loop owns its
// own inputs (the labels select reads). Best effort: a failure is noted, not fatal.
func (r *Runner) backlogTriage(ctx context.Context, label string, repos []string) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot resolve ward binary for --triage (%v); skipping triage\n", label, err)
		return
	}
	exe = canonicalWardExe(exe)
	for _, repo := range repos {
		fmt.Fprintf(os.Stderr, "%s: triaging %s (ward exec goose-triage) ...\n", label, repo)
		if terr := r.Runner.Exec(ctx, exe, "exec", "goose-triage", "--", "--repo", repo); terr != nil {
			fmt.Fprintf(os.Stderr, "%s: note: triage of %s failed (%v); continuing with the existing labels\n", label, repo, terr)
		}
	}
}

// backlogRefresh rebuilds each repo's ledger from its live open backlog.
func (r *Runner) backlogRefresh(ctx context.Context, label string, repos []string, limit int) error {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for _, repo := range repos {
		owner, name, _ := strings.Cut(repo, "/")
		issues, lerr := cl.listOpenIssues(ctx, owner, name, limit)
		if lerr != nil {
			return fmt.Errorf("%s: %w", label, lerr)
		}
		led, lerr := loadBacklogLedger(repo)
		if lerr != nil {
			return fmt.Errorf("%s: %w", label, lerr)
		}
		refreshBacklogLedger(led, rankBacklogIssues(issues))
		if serr := saveBacklogLedger(led); serr != nil {
			return fmt.Errorf("%s: %w", label, serr)
		}
	}
	return nil
}

// backlogDispatchCycle launches queued headless issues up to the in-flight cap,
// using ward's native headless carry. Records each launch in the ledger.
func (r *Runner) backlogDispatchCycle(ctx context.Context, label string, repos []string, cfg backlogConfig) error {
	entries := r.backlogScopeEntries(repos)
	_, inflight := backlogLaneCounts(entries)
	avail := cfg.maxParallel - inflight
	if avail <= 0 {
		return nil
	}
	launched := 0
	for _, p := range backlogQueuedPicks(entries) {
		if launched >= avail {
			break
		}
		if err := r.backlogDispatchOne(ctx, label, cfg.mode, p); err != nil {
			return err
		}
		launched++
	}
	return nil
}

// backlogDispatchOne launches one queued issue and records the transition: a launch
// error parks the issue failed; success records it dispatched with its container.
func (r *Runner) backlogDispatchOne(ctx context.Context, label string, mode containerMode, p *backlogEntry) error {
	ref := agentIssueRef{Owner: ownerOf(p.repo), Repo: nameOf(p.repo), Number: p.Num}
	fmt.Fprintf(os.Stderr, "%s: dispatching %s ...\n", label, ref)
	if derr := r.backlogDispatch(ctx, mode, ref); derr != nil {
		fmt.Fprintf(os.Stderr, "%s: dispatch FAILED for %s: %v\n", label, ref, derr)
		return r.updateBacklogEntry(p.repo, p.Num, func(e *backlogEntry) {
			e.State = "failed"
			e.LastOutcome = &backlogOutcome{Status: "dispatch-error", Text: backlogTruncate(derr.Error(), 300)}
		})
	}
	container := r.backlogRunningContainer(ctx, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	if err := r.updateBacklogEntry(p.repo, p.Num, func(e *backlogEntry) {
		e.State = "dispatched"
		e.DispatchedAt = time.Now().UTC().Format(time.RFC3339)
		e.Container = container
	}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s: dispatched %s (container %s)\n", label, ref, containerOrUnknown(container))
	return nil
}

// backlogDispatch launches one issue's headless carry in-process via the headless
// surface command (the native runAgentWork path). See docs/agent-backlog.md.
func (r *Runner) backlogDispatch(ctx context.Context, mode containerMode, ref agentIssueRef) error {
	cmd := agentSurfaceCommand("headless", true)
	argv := []string{"headless", ref.String(), "--driver", string(mode), "--no-preflight"}
	return cmd.Run(ctx, argv)
}

// backlogPoll reconciles each dispatched issue across the scope against reality.
func (r *Runner) backlogPoll(ctx context.Context, label string, repos []string) {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot poll (%v)\n", label, err)
		return
	}
	for _, repo := range repos {
		r.backlogPollRepo(ctx, label, repo, cl)
	}
}

// backlogPollRepo reconciles one repo's dispatched issues and saves on any change.
func (r *Runner) backlogPollRepo(ctx context.Context, label, repo string, cl *forgejoClient) {
	led, err := loadBacklogLedger(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot poll %s (%v)\n", label, repo, err)
		return
	}
	owner, name, _ := strings.Cut(repo, "/")
	changed := false
	for _, e := range led.Issues {
		if r.backlogReconcile(ctx, cl, repo, targetRepo{Owner: owner, Name: name}, e) {
			changed = true
		}
	}
	if changed {
		if serr := saveBacklogLedger(led); serr != nil {
			fmt.Fprintf(os.Stderr, "%s: note: cannot save %s ledger (%v)\n", label, repo, serr)
		}
	}
}

// backlogReconcile moves one exited dispatched entry to its outcome state; a gone
// container with no WARD-OUTCOME is parked failed. Returns whether it changed.
func (r *Runner) backlogReconcile(ctx context.Context, cl *forgejoClient, repo string, tr targetRepo, e *backlogEntry) bool {
	if e.State != "dispatched" || r.backlogContainerRunning(ctx, tr, e) {
		return false
	}
	comments, cerr := cl.listIssueComments(ctx, tr.Owner, tr.Name, e.Num)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "backlog: note: cannot read outcome for %s#%d (%v)\n", repo, e.Num, cerr)
		return false
	}
	outcome := parseBacklogOutcome(comments)
	if outcome == nil {
		e.State = "failed"
		e.LastOutcome = &backlogOutcome{Status: "exited-no-outcome", Text: "container exited without a WARD-OUTCOME comment; read its log"}
		fmt.Fprintf(os.Stderr, "  %s#%d -> failed: exited without a WARD-OUTCOME comment\n", repo, e.Num)
		return true
	}
	e.State = backlogOutcomeState(outcome.Status)
	e.LastOutcome = outcome
	fmt.Fprintf(os.Stderr, "  %s#%d -> %s%s\n", repo, e.Num, e.State, suffixText(outcome.Text))
	return true
}

// backlogContainerRunning reports whether a dispatched entry's container is still
// up: by recorded name when known, else by the issue's running-container probe.
func (r *Runner) backlogContainerRunning(ctx context.Context, repo targetRepo, e *backlogEntry) bool {
	if strings.TrimSpace(e.Container) != "" {
		return r.containerRunning(ctx, e.Container)
	}
	return r.backlogRunningContainer(ctx, repo, e.Num) != ""
}

// backlogRunningContainer returns the running ward container carrying an issue via
// docker ps on the `-issue-<N>-` name convention, "" when none.
func (r *Runner) backlogRunningContainer(ctx context.Context, repo targetRepo, num int) string {
	seg := fmt.Sprintf("-issue-%d-", num)
	out, err := r.Runner.Capture(ctx, "docker", "ps", "--format", "{{.Names}}", "--filter", "name="+seg)
	if err != nil {
		return ""
	}
	safe := safeRepoName(repo)
	for _, line := range strings.Split(string(out), "\n") {
		nm := strings.TrimSpace(line)
		if nm != "" && strings.Contains(nm, seg) && strings.Contains(nm, safe) {
			return nm
		}
	}
	return ""
}

// --- printing --------------------------------------------------------------

// backlogPrintStatus prints the scope's tracked issues grouped by lane and ranked.
func (r *Runner) backlogPrintStatus(repos []string) error {
	entries := r.backlogScopeEntries(repos)
	byLane := map[string][]*backlogEntry{}
	for _, e := range entries {
		lane := e.Lane
		if lane == "" {
			lane = "untriaged"
		}
		byLane[lane] = append(byLane[lane], e)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "backlog: %s (%d repos, %d tracked)\n", strings.Join(repos, ", "), len(repos), len(entries))
	for _, lane := range backlogLanes {
		items := byLane[lane]
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool { return backlogEntryLess(items[i], items[j]) })
		fmt.Fprintf(&b, "\n  %s lane (%d):\n", lane, len(items))
		for _, e := range items {
			fmt.Fprintf(&b, "    %-30s [%-2s] %-10s %s\n",
				e.repo+"#"+strconv.Itoa(e.Num), tierOrDash(e.Tier), stateOrDash(e.State), backlogTruncate(e.Title, 60))
		}
	}
	return r.emit(b.String())
}

// backlogPrintPlanned shows which queued issues the next cycle would dispatch under
// the cap - the --dry-run preview of planned launches (launching nothing).
func (r *Runner) backlogPrintPlanned(label string, repos []string, maxParallel int) error {
	picks := backlogQueuedPicks(r.backlogScopeEntries(repos))
	var b strings.Builder
	if len(picks) == 0 {
		fmt.Fprintf(&b, "\n%s (dry-run): no queued headless issues to dispatch.\n", label)
		return r.emit(b.String())
	}
	n := maxParallel
	if n > len(picks) {
		n = len(picks)
	}
	fmt.Fprintf(&b, "\n%s (dry-run): would dispatch %d of %d queued headless issue(s) (--max-parallel %d):\n",
		label, n, len(picks), maxParallel)
	for i, p := range picks {
		marker := "  (queued, waits for a free slot)"
		if i < n {
			marker = "  -> ward agent headless " + p.repo + "#" + strconv.Itoa(p.Num)
		}
		fmt.Fprintf(&b, "    %-30s [%-2s] %s%s\n",
			p.repo+"#"+strconv.Itoa(p.Num), tierOrDash(p.Tier), backlogTruncate(p.Title, 50), marker)
	}
	return r.emit(b.String())
}

// backlogPrintSummary prints the terminal disposition of the run by state.
func (r *Runner) backlogPrintSummary(repos []string) error {
	counts := map[string]int{}
	for _, e := range r.backlogScopeEntries(repos) {
		counts[e.State]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nbacklog summary (%s):\n", strings.Join(repos, ", "))
	for _, st := range []string{"done", "blocked", "failed", "queued", "dispatched", "surfaced", "skipped"} {
		if counts[st] > 0 {
			fmt.Fprintf(&b, "  %-10s %d\n", st, counts[st])
		}
	}
	return r.emit(b.String())
}

// --- small helpers ---------------------------------------------------------

// ownerOf / nameOf split a validated "owner/name" slug (validity is checked once,
// at scope resolution, so a malformed slug never reaches here).
func ownerOf(slug string) string { o, _, _ := strings.Cut(slug, "/"); return o }
func nameOf(slug string) string  { _, n, _ := strings.Cut(slug, "/"); return n }

func tierOrDash(t string) string {
	if t == "" {
		return "--"
	}
	return t
}

func stateOrDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func containerOrUnknown(c string) string {
	if strings.TrimSpace(c) == "" {
		return "(unknown - not yet visible to docker ps)"
	}
	return c
}

func suffixText(t string) string {
	if strings.TrimSpace(t) == "" {
		return ""
	}
	return ": " + backlogTruncate(t, 120)
}

// backlogTruncate caps s to n runes, appending an ellipsis when it had to cut.
func backlogTruncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return strings.TrimSpace(string(rs[:n])) + "…"
}

// backlogSleep waits d, returning early if the context is cancelled (Ctrl-C).
func backlogSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
