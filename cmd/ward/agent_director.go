package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// agent_director.go is `ward agent director`, the read-only director surface plus
// the opt-in autonomous backlog-supervisor role (ward#347, ward#346).

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
	backlogLanes = []string{"headless", "pull-request", "interactive", "consult", "untriaged"}
	// directorScopeSetupAttached is a seam for tests; production prompts only
	// when stdin/stdout are attached to a terminal.
	directorScopeSetupAttached = terminalAttached
)

const (
	backlogKindIssue                   = "issue"
	backlogKindPullRequest             = "pull-request"
	backlogReservationWaitingReaper    = "waiting-reaper"
	backlogReservationSafeToRedispatch = "safe-to-redispatch"
)

// backlogIssue is one open issue read from the live backlog, the ranking input.
// Body is populated for the startup triage pass (ward#397); ranking ignores it.
type backlogIssue struct {
	Number int
	Kind   string
	Author string
	Title  string
	Body   string
	Labels []string
	URL    string
}

// rankedBacklogIssue annotates an issue with its tier/mode/lane after ranking.
type rankedBacklogIssue struct {
	Num   int
	Kind  string
	Title string
	Tier  string
	Mode  string
	Lane  string
	URL   string
}

// backlogEntry is one tracked issue in a repo's ledger.
// It moves through queued -> dispatched -> done/submitted/merge-ready/blocked/failed.
type backlogEntry struct {
	Num          int             `yaml:"num"`
	Kind         string          `yaml:"kind,omitempty"`
	Title        string          `yaml:"title"`
	Tier         string          `yaml:"tier,omitempty"`
	Mode         string          `yaml:"mode,omitempty"`
	Lane         string          `yaml:"lane"`
	URL          string          `yaml:"url,omitempty"`
	State        string          `yaml:"state"`
	Container    string          `yaml:"container,omitempty"`
	DispatchedAt string          `yaml:"dispatched_at,omitempty"`
	LastOutcome  *backlogOutcome `yaml:"last_outcome,omitempty"`
	// RedispatchAttempts bounds the deterministic pre-launch-death retry before the
	// entry parks as an orphaned/needs-redispatch block (ward#595).
	RedispatchAttempts int `yaml:"redispatch_attempts,omitempty"`
	// repo is the owning slug, set only when entries are aggregated across a scope.
	repo string `yaml:"-"`
}

// backlogLedger is one repo's durable state file (YAML under ~/.ward/backlog).
type backlogLedger struct {
	Repo    string                   `yaml:"repo"`
	Updated string                   `yaml:"updated"`
	Issues  map[string]*backlogEntry `yaml:"issues"`
}

// dispatchEngineer is the container/harness flag set the director forwards into each
// engineer it dispatches, so the run inherits the operator's container intent (ward#355).
type dispatchEngineer struct {
	harness           containerMode // the engineer harness: --engineer-harness, else director's --harness
	image             string
	tag               string
	wardVersion       string
	wardVersionSource string
	// overrideReservation forwards --override-reservation (ward#352, ward#1045);
	// --override-capacity is per-launch only and never propagated by the loop.
	overrideReservation bool
	verificationFixture bool
}

// engineerArgv renders the `ward agent engineer` argv that carries one issue, forwarding
// every set container/harness flag so the engineer matches director's intent (ward#355).
func (c dispatchEngineer) engineerArgv(ref agentIssueRef) []string {
	// --quiet-seed keeps the in-process engineer's seed dump off the shared
	// director console (ward#519); --skip-host-preflight leaves review on.
	argv := []string{"engineer", ref.String(), "--harness", string(c.harness), "--quiet-seed", "--skip-host-preflight"}
	if img := strings.TrimSpace(c.image); img != "" {
		argv = append(argv, "--image", img)
	}
	if tag := strings.TrimSpace(c.tag); tag != "" {
		argv = append(argv, "--tag", tag)
	}
	if c.wardVersionSource == wardVersionSourceExplicit {
		if v := strings.TrimSpace(c.wardVersion); v != "" {
			argv = append(argv, "--ward-version", v)
		}
	}
	if c.overrideReservation {
		argv = append(argv, "--override-reservation")
	}
	if c.verificationFixture {
		argv = append(argv, "--verification-fixture", "--workflow", string(workflowRemoteBranchOnly))
	}
	return argv
}

// backlogConfig is the resolved knob set for one `ward agent director` run.
type backlogConfig struct {
	mode         containerMode
	maxParallel  int
	limit        int
	pollInterval time.Duration
	maxCycles    int
	dryRun       bool
	print        bool
	burndown     bool
	triage       bool
	issueRef     *agentIssueRef
	dispatch     dispatchEngineer
	// surface fields configure director's OWN surface session (ward#355, ward#353):
	// context bundle + ward-source + with-repo + no-pull on top of dispatch.
	contextBundle string
	wardSource    string
	noPull        bool
	withRepo      []string
}

// directorFlags is director's flag set: backlog/heartbeat knobs plus container/harness
// parity with the engineer + its surface (ward#355). See docs/agent-director.md.
func directorFlags() []cli.Flag {
	flags := agentHarnessFlags()
	flags = append(flags,
		&cli.StringFlag{Name: "engineer-harness", Usage: "harness for the engineers the director dispatches: " + agentHarnessChoices() + " (default: inherit --harness)"},
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: director.default-scope from ~/.ward/config.yaml)"},
		&cli.StringSliceFlag{Name: "org", Usage: "expand every repo an org owns into the scope (owner; repeatable), unioned with --repo and de-duped (ward#370)"},
		&cli.StringSliceFlag{Name: "with-repo", Usage: "grant director's own session an additional writable repo to clone (owner/name; repeatable), landed at /workspace/<owner>/<repo> (ward#1526)."},
		&cli.IntFlag{Name: "max-parallel", Value: directorMaxParallelDefault(), Usage: "in-flight container cap from typed defaults or ~/.ward/config.yaml"},
		&cli.BoolFlag{Name: "burndown", Aliases: []string{"drain"}, Usage: "run the autonomous headless backlog burndown loop. Without this flag, director opens its read-only surface after status refresh"},
		verificationFixtureFlag(),
		&cli.BoolFlag{Name: "triage", Value: true, Usage: "run the startup triage pass before burndown: label each untriaged open issue's tier (P0-P4) + automation mode (headless/interactive/consult) to warm the headless lane (ward#397). On by default for --burndown. --no-triage skips it"},
		&cli.BoolFlag{Name: "no-triage", Usage: "skip the startup triage pass and leave existing labels untouched (ward#397)"},
		&cli.IntFlag{Name: "limit", Value: directorLimitDefault(), Usage: "open issues read per repo per refresh"},
		&cli.DurationFlag{Name: "poll-interval", Value: directorPollIntervalDefault(), Usage: "wait between dispatch/poll cycles"},
		&cli.IntFlag{Name: "max-cycles", Value: 0, Usage: "stop after N heartbeat ticks (0 = run until drained with no new direction)"},
		&cli.BoolFlag{Name: "dry-run", Usage: "show the ranked lanes + planned dispatches, then exit without launching"},
	)
	flags = append(flags, agentImageFlags()...)
	return append(flags,
		&cli.BoolFlag{Name: "print", Usage: "resolve director's container/harness plan + the planned dispatches and exit; launch nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
		&cli.BoolFlag{Name: "override-reservation", Usage: "propagate --override-reservation to dispatched engineers so they reclaim a stale or foreign reservation instead of deferring (ward#352, ward#1045); off by default. Never touches the pool ceiling - --override-capacity is per-launch only, not a director knob"},
	)
}

// directorEngineerHarness resolves the dispatched-engineer harness: --engineer-harness
// if set, else director's own --harness (the two-level precedence from ward#355).
func directorEngineerHarness(c *cli.Command, directorMode containerMode) (containerMode, error) {
	raw := strings.TrimSpace(c.String("engineer-harness"))
	if raw == "" {
		return directorMode, nil
	}
	m, err := parseMode(raw)
	if err != nil {
		return "", fmt.Errorf("invalid --engineer-harness %q: want %s", raw, agentHarnessChoices())
	}
	return m, nil
}

func directorTriageEnabled(c *cli.Command) bool {
	return (c.Bool("burndown") || c.IsSet("triage")) && c.Bool("triage") && !c.Bool("no-triage")
}

type directorStartupStep struct {
	Category string
	Detail   string
}

var directorStartupStepStyle = map[string]string{
	"inventory": "36;1",
	"preview":   "90;1",
	"refresh":   "34;1",
	"surface":   "32;1",
	"heartbeat": "35;1",
	"triage":    "33;1",
}

func (r *Runner) directorStartupColors() bool {
	return terminalAttached() && r != nil && r.Runner != nil && r.Runner.Stdout == os.Stdout && r.Runner.Stderr == os.Stderr
}

func directorStartupCategoryLabel(category string, color bool) string {
	category = strings.TrimSpace(category)
	if category == "" {
		category = "action"
	}
	label := strings.ToUpper(category) + ":"
	if !color {
		return label
	}
	if style, ok := directorStartupStepStyle[strings.ToLower(category)]; ok {
		return "\x1b[" + style + "m" + label + "\x1b[0m"
	}
	return "\x1b[1m" + label + "\x1b[0m"
}

func directorStartupBanner(label string, steps []directorStartupStep, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s startup:\n\n", label)
	for i, step := range steps {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  %-12s %s\n", directorStartupCategoryLabel(step.Category, color), step.Detail)
	}
	b.WriteByte('\n')
	return b.String()
}

func directorStartupSteps(repos []string, cfg backlogConfig, preview bool) []directorStartupStep {
	var steps []directorStartupStep
	if len(repos) > 0 {
		steps = append(steps, directorStartupStep{
			Category: "inventory",
			Detail:   "print the current backlog snapshot for " + strings.Join(repos, ", "),
		})
	}
	if cfg.triage && !preview && cfg.issueRef == nil {
		steps = append(steps, directorStartupStep{
			Category: "triage",
			Detail:   "label untriaged issues before the loop starts",
		})
	}
	if directorNeedsLiveBacklog(cfg) {
		detail := "refresh the live backlog before opening the surface"
		if cfg.issueRef != nil {
			detail = "refresh the live record for " + cfg.issueRef.String() + " before opening the surface"
		}
		steps = append(steps, directorStartupStep{
			Category: "refresh",
			Detail:   detail,
		})
	}
	switch {
	case preview:
		steps = append(steps, directorStartupStep{
			Category: "preview",
			Detail:   "render the plan without launching anything",
		})
	case cfg.burndown:
		steps = append(steps, directorStartupStep{
			Category: "heartbeat",
			Detail:   "enter the autonomous backlog loop",
		})
	default:
		steps = append(steps, directorStartupStep{
			Category: "surface",
			Detail:   "open the read-only director session",
		})
	}
	return steps
}

func (r *Runner) backlogPrintStartup(label string, repos []string, cfg backlogConfig, preview bool) error {
	return r.emit(directorStartupBanner(label, directorStartupSteps(repos, cfg, preview), r.directorStartupColors()))
}

func (r *Runner) backlogLaunchAfterStatus(ctx context.Context, label string, repos []string, cfg backlogConfig, preview bool) error {
	if cfg.print {
		if err := r.backlogPrintDirectorPlan(label, repos, cfg); err != nil {
			return err
		}
	}
	if preview {
		return r.backlogPrintPlanned(label, repos, cfg.maxParallel)
	}
	if !cfg.burndown {
		_, err := r.directorSurface(ctx, label, repos[0], cfg)
		return err
	}
	return runDirectorLoop(ctx, cfg, &liveDirector{r: r, label: label, repos: repos, cfg: cfg})
}

// agentDirectorCommand wires `ward agent director` (audited via WrapVerb, trust-gated
// through ownerAllowed; ward#347, was backlog). See docs/agent-director.md.
func agentDirectorCommand() *cli.Command {
	return &cli.Command{
		Name:      "director",
		Usage:     "Open the attached director surface for a repo or one exact issue ref. Use --burndown to run the autonomous headless lane heartbeat.",
		ArgsUsage: "(issue ref | scope via --repo; default: director.default-scope from ~/.ward/config.yaml)",
		Description: `director opens the attached read-only control surface for a repo's backlog. It
refreshes the ledger from the live backlog, prints the ranked lanes, and then
hands control to an interactive director session without dispatching engineers.

Use --burndown to run the autonomous heartbeat. In that mode each tick reconciles
in-flight engineers (reading their WARD-WORKFLOW comments), refreshes the ledger
from the live backlog (ranking issues into lanes by tier/mode labels), asks a host
one-shot which queued headless issues to dispatch under --max-parallel, dispatches
the chosen set via ward's native engineer, then sleeps cheaply with no LLM held
open. Those engineer dispatches inherit the director's own harness by default,
and --engineer-harness explicitly overrides that default. When the headless lane
drains - nothing queued and nothing in flight - it surfaces an interactive session
for new direction rather than exiting, and resumes the heartbeat if the queue
refills (ward#351).

When a single issue ref or Forgejo issue URL is given, the director fetches and
validates that exact issue before status refresh. Under --burndown, the later
refresh loop stays pinned to that issue instead of widening back into the repo
backlog.

During a full --burndown lane, press Enter during the sleep offer to open the
same session.

  warded director --repo coilyco-flight-deck/ward         # one repo
  warded director coilyco-flight-deck/ward#988            # one issue, fail-closed scope
  warded director https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/988 # same as above
  warded director --repo a/b,c/d --max-parallel <bundled-default> # comma-separated scope
  warded director --org coilyco-flight-deck                # every repo the org owns (ward#370)
  warded director --burndown --repo coilyco-flight-deck/ward # opt into autonomous dispatch
  warded director --dry-run                                # ranked lanes + planned dispatches, launch nothing

It is attached/interactive only - there is no --detach (a detached director poses
runaway-dispatch risk). State lives in a durable per-repo ledger under ~/.ward/backlog,
so a killed burndown loop resumes from disk. Only the narrow headless lane is
auto-dispatched during --burndown. Interactive issues are surfaced, not launched.
See docs/agent-director.md.`,
		Flags: directorFlags(),
		// queue and merge are the read-only boundaries for ward-owned work.
		Commands: []*cli.Command{agentDirectorQueueCommand(), agentDirectorMergeCommand()},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentHarness(c)
			if err != nil {
				return fmt.Errorf("ward agent director: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".director",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentBacklog(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentBacklog resolves + trust-gates the scope, then drives the loop (the
// director role's loop body; ward#347).
func (r *Runner) runAgentBacklog(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "director")
	var (
		repos    []string
		issueRef *agentIssueRef
		err      error
	)
	if arg := strings.TrimSpace(c.Args().First()); arg != "" {
		ref, ierr := r.resolveDirectorIssueRef(ctx, c, label, mode, arg)
		if ierr != nil {
			return ierr
		}
		repos = []string{ref.repoSlug()}
		issueRef = &ref
	} else {
		repos, err = r.resolveDirectorScope(ctx, c, label)
		if err != nil {
			return err
		}
	}
	if err := r.backlogTrustGate(label, repos); err != nil {
		return err
	}
	if err := validateVerificationFixtureDirectorOptions(c, issueRef); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	engDriver, err := directorEngineerHarness(c, mode)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	cfg := backlogConfig{
		mode:         mode,
		maxParallel:  c.Int("max-parallel"),
		limit:        c.Int("limit"),
		pollInterval: c.Duration("poll-interval"),
		maxCycles:    c.Int("max-cycles"),
		dryRun:       c.Bool("dry-run"),
		print:        c.Bool("print"),
		burndown:     c.Bool("burndown"),
		triage:       directorTriageEnabled(c),
		issueRef:     issueRef,
		dispatch: dispatchEngineer{
			harness:             engDriver,
			image:               c.String("image"),
			tag:                 c.String("tag"),
			wardVersion:         strings.TrimSpace(c.String("ward-version")),
			wardVersionSource:   resolveWardVersionSource(c, c.String("ward-version")),
			overrideReservation: overrideReservation(c),
			verificationFixture: verificationFixtureRequested(c),
		},
		contextBundle: strings.TrimSpace(c.String("context-bundle")),
		wardSource:    strings.TrimSpace(c.String("ward-source")),
		noPull:        c.Bool("no-pull"),
		withRepo:      c.StringSlice("with-repo"),
	}
	if cfg.maxParallel < 1 {
		cfg.maxParallel = 1
	}
	if verificationFixtureRequested(c) {
		cfg.maxParallel = 1
		cfg.triage = false
	}
	return r.driveBacklog(ctx, label, repos, cfg)
}

// resolveDirectorIssueRef validates a single issue-scoped director target. Plain
// director requires open; burndown also requires headless and unreserved.
func (r *Runner) resolveDirectorIssueRef(ctx context.Context, c *cli.Command, label string, mode containerMode, arg string) (agentIssueRef, error) {
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return agentIssueRef{}, fmt.Errorf("%s: %w", label, err)
	}
	if !r.ownerAllowed(ref.Owner) {
		return agentIssueRef{}, r.untrustedOwnerErr(label, ref.Owner)
	}
	issue, err := r.fetchIssueByForge(ctx, label, ref.Forge, mode, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return agentIssueRef{}, fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	if err := validateDirectorIssueTarget(c, label, ref, issue); err != nil {
		return agentIssueRef{}, err
	}
	comments, cerr := r.fetchIssueComments(ctx, ref)
	if cerr != nil {
		return agentIssueRef{}, fmt.Errorf("%s: resolve issue %s comments: %w", label, ref, cerr)
	}
	target, terr := approvalTargetFromIssue(ref, issue)
	if terr != nil {
		return agentIssueRef{}, fmt.Errorf("%s: %w", label, terr)
	}
	if _, aerr := admitActorContent(target, comments, loadActorAuthorityPolicy()); aerr != nil {
		return agentIssueRef{}, fmt.Errorf("%s: %w", label, aerr)
	}
	if !directorBurndownRequested(c) {
		return ref, nil
	}
	if err := r.precheckReservation(ctx, label, resolvedWork{Ref: ref, Title: issue.Title, Body: issue.Body, Comments: comments}, false); err != nil {
		return agentIssueRef{}, err
	}
	return ref, nil
}

func directorBurndownRequested(c *cli.Command) bool {
	return c != nil && c.Bool("burndown")
}

func validateDirectorIssueTarget(c *cli.Command, label string, ref agentIssueRef, issue *Issue) error {
	if err := validateVerificationFixtureTarget(c, ref, issue); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if st := strings.ToLower(strings.TrimSpace(issue.State)); st != "open" {
		return dispatchDeclineErr(dispatchIssueClosed, "issue-closed",
			"%s: issue %s is %s, not open - nothing to do", label, ref, emptyDefault(st, "unknown"))
	}
	if !directorBurndownRequested(c) {
		return nil
	}
	tier := backlogTierOf(issue.Labels)
	modeLabel := backlogModeOf(issue.Labels)
	if lane := backlogLaneForLabels(tier, modeLabel); lane != "headless" {
		return dispatchDeclineErr(dispatchModeCeiling, "mode-ceiling",
			"%s: issue %s is not headless/autonomous eligible (tier=%s mode=%s)", label, ref, emptyDefault(tier, "--"), emptyDefault(modeLabel, "--"))
	}
	return nil
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
			return r.untrustedOwnerErr(label, owner)
		}
	}
	return nil
}

// driveBacklog sets the heartbeat up: optional triage, the initial refresh + status
// print + --dry-run preview, then hands the live backend to runDirectorLoop (ward#351).
func (r *Runner) driveBacklog(ctx context.Context, label string, repos []string, cfg backlogConfig) error {
	// --print and --dry-run are both launch-nothing previews, so neither triggers triage.
	preview := cfg.dryRun || cfg.print
	if err := r.backlogPrintStartup(label, repos, cfg, preview); err != nil {
		return err
	}
	if cfg.triage && !preview && cfg.issueRef == nil {
		r.backlogTriage(ctx, label, repos, cfg.mode, cfg.limit)
	}
	if directorNeedsLiveBacklog(cfg) {
		if err := r.backlogRefreshForDirector(ctx, label, cfg, repos); err != nil {
			return err
		}
	}
	if err := r.backlogPrintStatus(repos); err != nil {
		return err
	}
	return r.backlogLaunchAfterStatus(ctx, label, repos, cfg, preview)
}

// directorNeedsLiveBacklog reports whether startup needs to enumerate the live backlog
// instead of opening the read-only surface from the stored ledger.
func directorNeedsLiveBacklog(cfg backlogConfig) bool {
	return cfg.burndown || cfg.triage || cfg.issueRef != nil
}

// backlogRefreshForDirector keeps the live heartbeat on exactly one issue when the
// director was scoped by issue ref, instead of widening back into the repo backlog.
func (r *Runner) backlogRefreshForDirector(ctx context.Context, label string, cfg backlogConfig, repos []string) error {
	if cfg.issueRef != nil {
		return r.backlogRefreshIssue(ctx, label, cfg.mode, *cfg.issueRef)
	}
	return r.backlogRefresh(ctx, label, repos, cfg.limit)
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

// resolveDirectorScope unions explicit --repo slugs with each --org's expansion,
// falling back to the config default (ward#370, ward#398).
func (r *Runner) resolveDirectorScope(ctx context.Context, c *cli.Command, label string) ([]string, error) {
	explicit := parseScopeRepos(c.String("repo"), "")
	orgs := dedupeSlugs(c.StringSlice("org"))
	burndown := c.Bool("burndown")
	if len(explicit) == 0 && len(orgs) == 0 {
		return r.resolveDirectorDefaultScope(ctx, label, burndown)
	}
	expanded, err := r.expandOrgScopes(ctx, label, orgs)
	if err != nil {
		return nil, err
	}
	repos := mergeScopeRepos(explicit, expanded)
	if len(repos) == 0 {
		return nil, fmt.Errorf("%s: --repo/--org scope resolved to no repos", label)
	}
	if burndown {
		return r.filterBurndownRepos(label, repos)
	}
	return repos, nil
}

// resolveDirectorDefaultScope resolves the no-flag scope (ward#398): the config-stored
// director.default-scope is the only implicit fallback. See docs/agent-director.md.
func (r *Runner) resolveDirectorDefaultScope(ctx context.Context, label string, burndown bool) ([]string, error) {
	cfgOrgs, cfgRepos, err := loadDirectorDefaultScope()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if len(cfgOrgs) == 0 && len(cfgRepos) == 0 && directorScopeSetupAttached() {
		cfgOrgs, cfgRepos, err = r.promptDirectorDefaultScope(label)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
	}
	if len(cfgOrgs) > 0 || len(cfgRepos) > 0 {
		expanded, eerr := r.expandOrgScopes(ctx, label, cfgOrgs)
		if eerr != nil {
			return nil, eerr
		}
		repos := mergeScopeRepos(cfgRepos, expanded)
		if len(repos) == 0 {
			return nil, fmt.Errorf("%s: director.default-scope resolved to no repos", label)
		}
		if burndown {
			return r.filterBurndownRepos(label, repos)
		}
		return repos, nil
	}
	return nil, fmt.Errorf("%s: no --repo/--org given and no director.default-scope in ~/.ward/config.yaml", label)
}

type directorScopeKind string

const (
	directorScopeRepo   directorScopeKind = "repo"
	directorScopeOrg    directorScopeKind = "org"
	directorScopeCancel directorScopeKind = "cancel"
)

func (r *Runner) promptDirectorDefaultScope(label string) (orgs, repos []string, err error) {
	reader := bufio.NewReader(r.gateIn())
	w := r.gateErr()
	_, _ = fmt.Fprintf(w, "%s: no --repo/--org given and no director.default-scope in ~/.ward/config.yaml\n\n", label)
	_, _ = fmt.Fprintln(w, "Choose a default director scope to save:")
	_, _ = fmt.Fprintln(w, "  1) Repo scope (owner/name, comma-separated)")
	_, _ = fmt.Fprintln(w, "  2) Org scope (owner, comma-separated)")
	_, _ = fmt.Fprintln(w, "  3) Cancel")
	_, _ = fmt.Fprint(w, "Selection [1-3]: ")
	choice, err := readDirectorScopeChoice(reader)
	if err != nil {
		return nil, nil, err
	}
	if choice == directorScopeCancel {
		return nil, nil, errors.New("director scope setup canceled")
	}
	prompt := "Repo scope"
	if choice == directorScopeOrg {
		prompt = "Org scope"
	}
	_, _ = fmt.Fprintf(w, "%s: ", prompt)
	entries, err := readDirectorScopeEntries(reader, choice)
	if err != nil {
		return nil, nil, err
	}
	if err := writeDirectorDefaultScope(entries); err != nil {
		return nil, nil, err
	}
	path, _ := config.GlobalConfigPath()
	if path != "" {
		_, _ = fmt.Fprintf(w, "%s: saved director.default-scope to %s\n\n", label, path)
	}
	orgs, repos = partitionScopeEntries(entries)
	return orgs, repos, nil
}

func readDirectorScopeChoice(reader *bufio.Reader) (directorScopeKind, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read scope selection: %w", err)
	}
	switch strings.TrimSpace(line) {
	case "1", "repo", "repos", "r":
		return directorScopeRepo, nil
	case "2", "org", "orgs", "o":
		return directorScopeOrg, nil
	case "3", "cancel", "c", "q", "quit":
		return directorScopeCancel, nil
	default:
		return "", fmt.Errorf("invalid scope selection %q (want 1, 2, or 3)", strings.TrimSpace(line))
	}
}

func readDirectorScopeEntries(reader *bufio.Reader, kind directorScopeKind) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read scope value: %w", err)
	}
	entries := dedupeSlugs(strings.Split(line, ","))
	if len(entries) == 0 {
		return nil, errors.New("empty director.default-scope")
	}
	for _, entry := range entries {
		hasSlash := strings.Contains(entry, "/")
		switch {
		case kind == directorScopeRepo && !hasSlash:
			return nil, fmt.Errorf("repo scope entry %q must be owner/name", entry)
		case kind == directorScopeOrg && hasSlash:
			return nil, fmt.Errorf("org scope entry %q must be an owner, not owner/name", entry)
		}
	}
	return entries, nil
}

func writeDirectorDefaultScope(entries []string) error {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", filepath.Dir(path), err)
	}
	doc, mapping, err := loadOrCreateGlobalConfigDocument(path)
	if err != nil {
		return err
	}
	director := mappingChildMapping(mapping, "director")
	setMappingValue(director, "default-scope", scopeEntriesNode(entries))
	return writeYAMLDocument(path, doc)
}

func loadOrCreateGlobalConfigDocument(path string) (*yaml.Node, *yaml.Node, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- ~/.ward/config.yaml is the intended operator-local input.
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		return doc, doc.Content[0], nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	mapping, hasMapping, err := documentMapping(&doc, path)
	if err != nil {
		return nil, nil, err
	}
	if !hasMapping {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		mapping = doc.Content[0]
	}
	return &doc, mapping, nil
}

func mappingChildMapping(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		child := mapping.Content[i+1]
		if child.Kind != yaml.MappingNode {
			child.Kind = yaml.MappingNode
			child.Tag = "!!map"
			child.Value = ""
			child.Content = nil
		}
		return child
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, scalarNode(key), child)
	return child
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func scopeEntriesNode(entries []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, entry := range dedupeSlugs(entries) {
		node.Content = append(node.Content, scalarNode(entry))
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// wardGlobalConfig is the slice of ~/.ward/config.yaml ward reads today.
type wardGlobalConfig struct {
	DefaultHarness string `yaml:"default-harness"`
	Director       struct {
		DefaultScope []string `yaml:"default-scope"`
		MaxParallel  int      `yaml:"max-parallel"`
		Limit        int      `yaml:"limit"`
		PollInterval string   `yaml:"poll-interval"`
	} `yaml:"director"`
	Agent struct {
		Image          string `yaml:"image"`
		ReleaseChannel string `yaml:"release-channel"`
		Redaction      struct {
			EnvNames []string `yaml:"env-names"`
			Patterns []string `yaml:"patterns"`
		} `yaml:"redaction"`
		Workflow struct {
			Default      string            `yaml:"default"`
			Repositories map[string]string `yaml:"repositories"`
		} `yaml:"workflow"`
		Review struct {
			Skip []string `yaml:"skip"`
		} `yaml:"review"`
	} `yaml:"agent"`
	Container struct {
		MemoryLimit string `yaml:"memory-limit"`
		StagingDir  string `yaml:"staging-dir"`
	} `yaml:"container"`
}

// loadWardGlobalConfig reads the operator-local ward config. A missing file is
// valid and returns the zero value.
func loadWardGlobalConfig() (wardGlobalConfig, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return wardGlobalConfig{}, err
	}
	var cfg wardGlobalConfig
	if err := config.OverlayFile(&cfg, path); err != nil {
		return wardGlobalConfig{}, err
	}
	return cfg, nil
}

// loadDirectorDefaultScope reads director.default-scope from ~/.ward/config.yaml,
// partitioning into org tokens and owner/name slugs; a missing file is no error.
func loadDirectorDefaultScope() (orgs, repos []string, err error) {
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return nil, nil, err
	}
	orgs, repos = partitionScopeEntries(cfg.Director.DefaultScope)
	return orgs, repos, nil
}

// loadReviewSkips reads agent.review.skip from ~/.ward/config.yaml.
// It is a host-local default, so a missing file is not an error.
func loadReviewSkips() ([]string, error) {
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return nil, err
	}
	return cfg.Agent.Review.Skip, nil
}

// partitionScopeEntries splits a de-duped scope list into org tokens (no slash) and
// bare owner/name repo slugs. Pure + testable; the split key is a single '/'.
func partitionScopeEntries(entries []string) (orgs, repos []string) {
	for _, e := range dedupeSlugs(entries) {
		if strings.Contains(e, "/") {
			repos = append(repos, e)
		} else {
			orgs = append(orgs, e)
		}
	}
	return orgs, repos
}

// expandOrgScopes expands each --org to its repo slugs via the existing org-repo-list
// endpoint; an org that lists no usable repos errors rather than draining empty.
func (r *Runner) expandOrgScopes(ctx context.Context, label string, orgs []string) ([]string, error) {
	if len(orgs) == 0 {
		return nil, nil
	}
	cl := r.hostForgejoClient(ctx)
	var out []string
	for _, org := range orgs {
		repos, lerr := cl.listOwnerRepos(ctx, org)
		if lerr != nil {
			return nil, fmt.Errorf("%s: cannot expand --org %q: %w", label, org, lerr)
		}
		slugs := orgReposToSlugs(org, repos)
		if len(slugs) == 0 {
			return nil, fmt.Errorf("%s: --org %q expanded to no repos (unknown org, or only archived/empty repos)", label, org)
		}
		out = append(out, slugs...)
	}
	return out, nil
}

// orgReposToSlugs maps an org's repo list to owner/name scope slugs, dropping
// archived and empty repos (nothing for an engineer to carry there). Pure + testable.
func orgReposToSlugs(org string, repos []repoBrief) []string {
	var slugs []string
	for _, rb := range repos {
		if rb.Archived || rb.Empty {
			continue
		}
		slugs = append(slugs, org+"/"+rb.Name)
	}
	return slugs
}

// parseScopeRepos resolves the scope: a comma-separated --repo list, else the
// git-origin default; de-duped, order-preserving. Ports backlog-loop.py's parse_repos.
func parseScopeRepos(raw, def string) []string {
	if strings.TrimSpace(raw) == "" {
		raw = def
	}
	return dedupeSlugs(strings.Split(raw, ","))
}

func (r *Runner) filterBurndownRepos(label string, repos []string) ([]string, error) {
	if len(repos) == 0 {
		return nil, nil
	}
	kept := repos[:0:0]
	for _, slug := range repos {
		if !r.burndownEnabled(slug) {
			fmt.Fprintf(os.Stderr, "burndown: skipping %s (filtered)\n", slug)
			continue
		}
		kept = append(kept, slug)
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("%s: scope resolved to no repos (every repo has burndown disabled)", label)
	}
	return kept, nil
}

// mergeScopeRepos unions the given slug lists into one de-duped, order-preserving
// scope - explicit --repo slugs first, then each --org's expansion (ward#370).
func mergeScopeRepos(lists ...[]string) []string {
	var all []string
	for _, l := range lists {
		all = append(all, l...)
	}
	return dedupeSlugs(all)
}

// dedupeSlugs trims blanks and drops duplicate slugs, preserving first-seen order -
// the shared normalizer behind --repo parsing and the --org union.
func dedupeSlugs(in []string) []string {
	var out []string
	for _, slug := range in {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		dup := false
		for _, s := range out {
			if s == slug {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, slug)
		}
	}
	return out
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
	laneRank := map[string]int{"headless": 0, "pull-request": 1, "interactive": 2, "consult": 3, "untriaged": 4}
	out := make([]rankedBacklogIssue, 0, len(issues))
	for _, it := range issues {
		kind := backlogKindOf(it.Kind)
		tier := backlogTierOf(it.Labels)
		mode := backlogModeOf(it.Labels)
		out = append(out, rankedBacklogIssue{
			Num:   it.Number,
			Kind:  kind,
			Title: it.Title,
			Tier:  tier,
			Mode:  mode,
			Lane:  backlogLaneForKind(kind, tier, mode),
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

func backlogKindOf(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case backlogKindPullRequest:
		return backlogKindPullRequest
	default:
		return backlogKindIssue
	}
}

// backlogLaneForKind maps a backlog item's kind and labels to a lane.
func backlogLaneForKind(kind, tier, mode string) string {
	if backlogKindOf(kind) == backlogKindPullRequest {
		return backlogKindPullRequest
	}
	return backlogLaneForLabels(tier, mode)
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
	case "pull-request", "interactive":
		return "surfaced"
	default:
		return "skipped"
	}
}

// isStrandedDispatchError reports whether e is a pre-ward#524 launch/infra stranding:
// parked `failed` with the "dispatch-error" status the old classifier stamped (ward#527).
func isStrandedDispatchError(e *backlogEntry) bool {
	return e != nil && e.State == "failed" && e.LastOutcome != nil && e.LastOutcome.Status == "dispatch-error"
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
	case rk.Lane == "headless" && isStrandedDispatchError(entry):
		// ward#527 self-heal: re-queue a legacy dispatch-error stranding.
		entry.State = "queued"
		entry.LastOutcome = nil
	}
	entry.Num = rk.Num
	entry.Kind = backlogKindOf(rk.Kind)
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

// backlogOutcomeRE parses the status + optional status emoji + reason that
// follow the WARD-WORKFLOW marker.
var backlogOutcomeRE = regexp.MustCompile(`(?i)^(done|submitted|merge-ready|pending|ready-for-merge|blocked|failed)\b(?:\s+[✅🛑❌])?[\s:.\-]*(.*)`)

// parseBacklogOutcome classifies the latest comment leading with WARD-WORKFLOW,
// nil when none. Ports backlog-loop.py's parse_outcome.
func parseBacklogOutcome(comments []issueComment) *backlogOutcome {
	latest, ok := latestBacklogOutcomeComment(comments)
	if !ok {
		return nil
	}
	o, ok := backlogOutcomeOfComment(latest.Body)
	if !ok {
		return nil
	}
	return &o
}

// latestBacklogOutcomeComment returns the most recent comment body carrying a
// WARD-WORKFLOW marker.
func latestBacklogOutcomeComment(comments []issueComment) (issueComment, bool) {
	if humanFeedbackOutcomeBlocked(comments, time.Time{}) {
		return issueComment{}, false
	}
	type hit struct {
		at time.Time
		c  issueComment
	}
	var hits []hit
	for _, c := range comments {
		if !trustedMachineComment(c, recordKindOutcome) {
			continue
		}
		if _, ok := backlogOutcomeOfComment(c.Body); !ok {
			continue
		}
		hits = append(hits, hit{at: c.CreatedAt, c: c})
	}
	if len(hits) == 0 {
		return issueComment{}, false
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at.Before(hits[j].at) })
	return hits[len(hits)-1].c, true
}

// directorRunMeta is the small policy payload the director merge command reads
// from a worker's final comment: workflow marker + review summary.
type directorRunMeta struct {
	Workflow           string
	Review             string
	MergeAuthorization string
	Outcome            backlogOutcome
	HasOutcome         bool
	IssueRef           string
	QA                 qaCommentMeta
	PRHeadSHA          string
	PRRef              string
	Status             directorMergeStatusSummary
	CommentedBy        string
	CommentedAt        time.Time
}

// latestQAVerdictComment returns the newest QA verdict comment that matches the
// requested issue/PR/head SHA tuple. Older or stale SHAs do not override.
func latestQAVerdictComment(comments []issueComment, issueRef, prRef, headSHA string) (qaCommentMeta, bool) {
	type hit struct {
		at time.Time
		m  qaCommentMeta
	}
	var hits []hit
	issueRef = strings.TrimSpace(issueRef)
	prRef = strings.TrimSpace(prRef)
	headSHA = strings.TrimSpace(headSHA)
	if issueRef == "" || prRef == "" || headSHA == "" {
		return qaCommentMeta{}, false
	}
	for _, c := range comments {
		if !trustedMachineComment(c, recordKindQA) {
			continue
		}
		meta, ok := parseQAVerdictComment(c.Body)
		if !ok {
			continue
		}
		if strings.TrimSpace(meta.IssueRef) != issueRef {
			continue
		}
		if strings.TrimSpace(meta.PRRef) != prRef {
			continue
		}
		if strings.TrimSpace(meta.ReviewedSHA) != headSHA {
			continue
		}
		hits = append(hits, hit{at: c.CreatedAt, m: meta})
	}
	if len(hits) == 0 {
		return qaCommentMeta{}, false
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at.Before(hits[j].at) })
	return hits[len(hits)-1].m, true
}

// parseDirectorRunMeta parses a final worker comment for the director merge gate.
// It accepts the machine line shape documented in headlessReflection.
func parseDirectorRunMeta(body string) directorRunMeta {
	meta := directorRunMeta{}
	if o, ok := backlogOutcomeOfComment(body); ok {
		meta.Outcome = o
		meta.HasOutcome = true
	}
	for _, ln := range strings.Split(body, "\n") {
		s := backlogCommentLine(ln)
		if s == "" {
			continue
		}
		for _, part := range strings.Split(s, ";") {
			field := strings.TrimSpace(part)
			lower := strings.ToLower(field)
			switch {
			case strings.HasPrefix(lower, "workflow:"):
				meta.Workflow = string(canonicalWorkflow(workflowMode(strings.TrimSpace(field[len("workflow:"):]))))
			case strings.HasPrefix(lower, "review summary:"):
				meta.Review = strings.TrimSpace(field[len("review summary:"):])
			case strings.HasPrefix(lower, "director merge authorization:"):
				meta.MergeAuthorization = strings.TrimSpace(field[len("director merge authorization:"):])
			case strings.HasPrefix(lower, "checked head sha:"):
				meta.Status.HeadSHA = strings.TrimSpace(field[len("checked head sha:"):])
			case strings.HasPrefix(lower, "status state:"):
				meta.Status.State = strings.TrimSpace(field[len("status state:"):])
			case strings.HasPrefix(lower, "status context:"):
				meta.Status.Checks = parseDirectorStatusContexts(strings.TrimSpace(field[len("status context:"):]))
			}
		}
	}
	return meta
}

func parseDirectorStatusContexts(s string) []directorMergeStatusContext {
	s = strings.TrimSpace(s)
	if s == "" || s == "<status unavailable>" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]directorMergeStatusContext, 0, len(parts))
	for _, part := range parts {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		ctx, state, ok := strings.Cut(field, "=")
		if !ok {
			out = append(out, directorMergeStatusContext{Context: field})
			continue
		}
		out = append(out, directorMergeStatusContext{Context: strings.TrimSpace(ctx), State: strings.TrimSpace(state)})
	}
	return out
}

// backlogCommentLine normalizes the leading quote/list markers the same way the
// WARD-WORKFLOW parser does, so the policy parser can read wrapped comments too.
func backlogCommentLine(ln string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), ">*-•# "))
}

// backlogOutcomeOfComment parses the WARD-WORKFLOW status from one comment body,
// reporting ok=false when the body carries no leading marker line.
func backlogOutcomeOfComment(body string) (backlogOutcome, bool) {
	header, ok := parseWorkflowCommentHeader(body)
	if !ok {
		return backlogOutcome{}, false
	}
	if o, ok := backlogOutcomeFromPRURLHeader(body, header); ok {
		return o, true
	}
	o := backlogOutcome{Status: "unknown"}
	if strings.Contains(strings.TrimSpace(header.Variant), "://") {
		return backlogOutcome{}, false
	}
	status := normalizeBacklogOutcomeStatus(header.Variant)
	switch {
	case workflowCommentIsTerminalOutcomeVariant(header.Variant):
		o.Status = status
		o.Text = header.Detail
	case !header.Legacy || workflowCommentIsLegacyWorkflowCommentVariant(header.Variant):
		return backlogOutcome{}, false
	default:
		o.Text = workflowCommentDetail(header.Raw)
	}
	if m := backlogOutcomeRE.FindStringSubmatch(header.Detail); m != nil {
		o.Status = normalizeBacklogOutcomeStatus(strings.ToLower(m[1]))
		o.Text = workflowCommentDetail(m[2])
	} else if o.Status != "unknown" {
		o.Text = workflowCommentDetail(o.Text)
	}
	o.Text = backlogTruncate(o.Text, 500)
	return o, true
}

func backlogOutcomeFromPRURLHeader(body string, header workflowCommentHeader) (backlogOutcome, bool) {
	pr, ok := parseWorkflowOutcomePRRef(header.Variant)
	if !ok {
		return backlogOutcome{}, false
	}
	if strings.TrimSpace(header.Detail) != "" {
		return backlogOutcome{}, false
	}
	o := backlogOutcome{
		Status:   "submitted",
		Text:     workflowCommentDetail(pr.url()),
		PRURL:    pr.url(),
		PRNumber: pr.Number,
	}
	if auth, ok := workflowCommentFieldValue(body, "director merge authorization:"); ok {
		switch strings.ToLower(strings.TrimSpace(auth)) {
		case "reviewed-and-ready", "merge-ready":
			o.Status = "merge-ready"
		}
	}
	return o, true
}

func normalizeBacklogOutcomeStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "pending":
		return "submitted"
	case "ready-for-merge":
		return "merge-ready"
	default:
		return strings.TrimSpace(strings.ToLower(status))
	}
}

// backlogOutcomeState maps a parsed outcome status to the ledger state it lands in.
// Submitted and merge-ready stay explicit nonterminal states. Unknowns park blocked.
func backlogOutcomeState(status string) string {
	switch normalizeBacklogOutcomeStatus(status) {
	case "done":
		return "done"
	case "failed":
		return "failed"
	case "blocked":
		return "blocked"
	case "submitted":
		return "submitted"
	case "merge-ready":
		return "merge-ready"
	default:
		return "blocked"
	}
}

func backlogStateSummaryOrder() []string {
	return []string{"done", "submitted", "merge-ready", "blocked", "failed", "queued", backlogReservationSafeToRedispatch, backlogReservationWaitingReaper, "dispatched", "surfaced", "skipped"}
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

// backlogLaneCounts tallies queued, in-flight, and reservation-hold work.
// The loop drains when all three reach zero.
func backlogLaneCounts(entries []*backlogEntry) (queued, inflight, held int) {
	for _, e := range entries {
		switch e.State {
		case "queued", backlogReservationSafeToRedispatch:
			queued++
		case "dispatched":
			inflight++
		case backlogReservationWaitingReaper:
			held++
		}
	}
	return queued, inflight, held
}

// backlogQueuedPicks returns the queued headless entries across the scope, ranked
// (tier then repo then number), ready to dispatch.
func backlogQueuedPicks(entries []*backlogEntry) []*backlogEntry {
	var picks []*backlogEntry
	for _, e := range entries {
		switch e.State {
		case "queued", backlogReservationSafeToRedispatch:
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

// backlogHasReservationComment reports whether the thread still carries any
// reservation marker, fresh or stale.
func backlogHasReservationComment(comments []issueComment) bool {
	for _, c := range comments {
		if trustedMachineComment(c, recordKindReservation) {
			return true
		}
	}
	return false
}

// backlogReservationState classifies reservation-only holds for the director.
// The issue thread is canonical. Fresh holds wait for the reaper/TTL, stale re-enters.
func backlogReservationState(comments []issueComment, now time.Time, ttl time.Duration) string {
	if _, held := freshReservationComment(comments, now, ttl); held {
		return backlogReservationWaitingReaper
	}
	if backlogHasReservationComment(comments) {
		return backlogReservationSafeToRedispatch
	}
	return ""
}

// --- loop steps ------------------------------------------------------------

// backlogRefresh rebuilds each repo's ledger from its live open backlog.
func (r *Runner) backlogRefresh(ctx context.Context, label string, repos []string, limit int) error {
	cl := r.hostForgejoClient(ctx)
	for _, repo := range repos {
		if err := r.backlogRefreshRepo(ctx, cl, label, repo, limit); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) backlogRefreshRepo(ctx context.Context, cl *forgejoClient, label, repo string, limit int) error {
	owner, name, _ := strings.Cut(repo, "/")
	rawIssues, lerr := cl.listOpenIssueFeedByType(ctx, owner, name, limit, "issues")
	if lerr != nil {
		return fmt.Errorf("%s: %w", label, lerr)
	}
	rawPRs, perr := cl.listOpenIssueFeedByType(ctx, owner, name, limit, "pulls")
	if perr != nil {
		return fmt.Errorf("%s: %w", label, perr)
	}
	led, lerr := loadBacklogLedger(repo)
	if lerr != nil {
		return fmt.Errorf("%s: %w", label, lerr)
	}
	issues := make([]backlogIssue, 0, len(rawIssues))
	for _, ri := range rawIssues {
		bi := backlogIssue{Number: ri.Number, Kind: backlogKindIssue, Author: ri.User.Login, Title: ri.Title, Body: ri.Body, URL: ri.HTMLURL}
		for _, l := range ri.Labels {
			if l.Name != "" {
				bi.Labels = append(bi.Labels, l.Name)
			}
		}
		issues = append(issues, bi)
	}
	prBacklog := make([]backlogIssue, 0, len(rawPRs))
	for _, pr := range rawPRs {
		labels := make([]string, 0, len(pr.Labels))
		for _, l := range pr.Labels {
			if l.Name != "" {
				labels = append(labels, l.Name)
			}
		}
		prBacklog = append(prBacklog, backlogIssue{
			Number: pr.Number,
			Kind:   backlogKindPullRequest,
			Author: pr.User.Login,
			Title:  pr.Title,
			Body:   pr.Body,
			URL:    pr.HTMLURL,
			Labels: labels,
		})
	}
	refreshBacklogLedger(led, rankBacklogIssues(combineOpenBacklogIssues(issues, prBacklog)))
	_ = r.backlogRefreshReservationStates(ctx, cl, repo, led)
	_ = r.backlogRefreshClosedIssueStates(ctx, cl, repo, led)
	if serr := saveBacklogLedger(led); serr != nil {
		return fmt.Errorf("%s: %w", label, serr)
	}
	return nil
}

// backlogRefreshIssue rebuilds the ledger from exactly one validated issue, so the
// issue-ref director path never widens into the repo backlog.
func (r *Runner) backlogRefreshIssue(ctx context.Context, label string, mode containerMode, ref agentIssueRef) error {
	issue, err := r.fetchIssueByForge(ctx, label, ref.Forge, mode, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	led, err := loadBacklogLedger(ref.repoSlug())
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if led.Issues == nil {
		led.Issues = map[string]*backlogEntry{}
	}
	key := strconv.Itoa(ref.Number)
	for k := range led.Issues {
		if k != key {
			delete(led.Issues, k)
		}
	}
	refreshBacklogLedger(led, rankBacklogIssues([]backlogIssue{{
		Number: ref.Number,
		Kind:   backlogKindIssue,
		Title:  issue.Title,
		Body:   issue.Body,
		Labels: append([]string(nil), issue.Labels...),
		URL:    issue.URL,
	}}))
	if backlogClosedIssueState(issue.State) {
		if e := led.Issues[strconv.Itoa(ref.Number)]; e != nil {
			_ = backlogRefreshClosedIssueEntry(ref.repoSlug(), e)
		}
	}
	if err := saveBacklogLedger(led); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// backlogRefreshReservationStates overlays issue-thread reservation freshness onto
// the freshly ranked ledger so stale holds re-enter the queue and fresh holds park.
func (r *Runner) backlogRefreshReservationStates(ctx context.Context, cl Tracker, repo string, led *backlogLedger) bool {
	changed := false
	now := time.Now().UTC()
	ttl := agentReservationTTL()
	tr := targetRepo{Owner: ownerOf(repo), Name: nameOf(repo)}
	for _, e := range led.Issues {
		if r.backlogRefreshReservationState(ctx, cl, repo, tr, now, ttl, e) {
			changed = true
		}
	}
	return changed
}

func (r *Runner) backlogRefreshReservationState(ctx context.Context, cl Tracker, repo string, tr targetRepo, now time.Time, ttl time.Duration, e *backlogEntry) bool {
	if backlogRedispatchSweepTracked(e) {
		comments, err := cl.ListIssueComments(ctx, tr.Owner, tr.Name, e.Num)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backlog: note: cannot read redispatch state for %s#%d (%v)\n", repo, e.Num, err)
			return false
		}
		return r.backlogSweepNeedsRedispatch(ctx, repo, tr, e, comments)
	}
	if !backlogRefreshReservationTracked(e) {
		return false
	}
	comments, err := cl.ListIssueComments(ctx, tr.Owner, tr.Name, e.Num)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backlog: note: cannot read reservation state for %s#%d (%v)\n", repo, e.Num, err)
		return false
	}
	state := backlogReservationState(comments, now, ttl)
	switch state {
	case backlogReservationWaitingReaper:
		if e.State == state {
			return false
		}
		e.State = state
		e.Container = ""
		e.DispatchedAt = ""
		fmt.Fprintf(os.Stderr, "  %s#%d -> %s (fresh reservation still waiting for reap/TTL)\n", repo, e.Num, state)
		return true
	case backlogReservationSafeToRedispatch:
		if e.State == state {
			return false
		}
		e.State = state
		e.Container = ""
		e.DispatchedAt = ""
		fmt.Fprintf(os.Stderr, "  %s#%d -> %s (reservation stale)\n", repo, e.Num, state)
		return true
	default:
		return r.backlogRefreshReservationHold(repo, e, comments, now, ttl)
	}
}

// backlogRefreshClosedIssueStates removes closed issue rows from dispatchable
// headless states without disturbing open stale reservations.
func (r *Runner) backlogRefreshClosedIssueStates(ctx context.Context, cl Tracker, repo string, led *backlogLedger) bool {
	changed := false
	tr := targetRepo{Owner: ownerOf(repo), Name: nameOf(repo)}
	for _, e := range led.Issues {
		if r.backlogRefreshClosedIssueState(ctx, cl, repo, tr, e) {
			changed = true
		}
	}
	return changed
}

func (r *Runner) backlogRefreshClosedIssueState(ctx context.Context, cl Tracker, repo string, tr targetRepo, e *backlogEntry) bool {
	if backlogKindOf(e.Kind) != backlogKindIssue || e.Lane != "headless" {
		return false
	}
	switch e.State {
	case "queued", backlogReservationSafeToRedispatch:
	default:
		return false
	}
	issue, err := cl.GetIssue(ctx, tr.Owner, tr.Name, e.Num)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backlog: note: cannot read issue state for %s#%d (%v)\n", repo, e.Num, err)
		return false
	}
	if strings.EqualFold(strings.TrimSpace(issue.State), "open") {
		return false
	}
	return backlogRefreshClosedIssueEntry(repo, e)
}

func backlogClosedIssueState(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "closed")
}

func backlogRefreshClosedIssueEntry(repo string, e *backlogEntry) bool {
	if backlogKindOf(e.Kind) != backlogKindIssue || e.Lane != "headless" {
		return false
	}
	switch e.State {
	case "queued", backlogReservationSafeToRedispatch:
	default:
		return false
	}
	e.State = "blocked"
	e.Container = ""
	e.DispatchedAt = ""
	e.LastOutcome = &backlogOutcome{
		Status: "issue-closed",
		Text:   "issue is closed; remove the stale reservation or cleanup metadata before redispatching",
	}
	fmt.Fprintf(os.Stderr, "  %s#%d -> blocked (issue is closed)\n", repo, e.Num)
	return true
}

// backlogRedispatchSweepTracked marks the parked headless issues the ward#1149 marker
// sweep considers; done is excluded (about to close, not to redispatch). See docs.
func backlogRedispatchSweepTracked(e *backlogEntry) bool {
	if backlogKindOf(e.Kind) != backlogKindIssue || e.Lane != "headless" {
		return false
	}
	switch e.State {
	case "submitted", "merge-ready", "blocked", "failed":
		return true
	default:
		return false
	}
}

// backlogSweepNeedsRedispatch gives the needs-redispatch marker an owner (ward#1149):
// an unhandled marker re-queues the parked entry, bounded by redispatchAttemptCap.
func (r *Runner) backlogSweepNeedsRedispatch(_ context.Context, repo string, _ targetRepo, e *backlogEntry, comments []issueComment) bool {
	if latestDirectorQueueSignal(comments).Kind != directorQueueSignalRedispatch {
		return false
	}
	if e.RedispatchAttempts >= redispatchAttemptCap {
		if e.State == "blocked" {
			return false
		}
		e.State = "blocked"
		e.LastOutcome = &backlogOutcome{
			Status: "redispatch-cap-reached",
			Text: fmt.Sprintf("needs-redispatch marker swept %d× without a completed run (ward#1149); "+
				"re-dispatch cap reached - fix the failing dispatch or re-dispatch by hand", e.RedispatchAttempts),
		}
		fmt.Fprintf(os.Stderr, "  %s#%d -> blocked (needs-redispatch sweep cap reached)\n", repo, e.Num)
		return true
	}
	e.RedispatchAttempts++
	e.State = "queued"
	e.Container = ""
	e.DispatchedAt = ""
	e.LastOutcome = &backlogOutcome{
		Status: "needs-redispatch-requeued",
		Text: fmt.Sprintf("unhandled needs-redispatch marker on the thread; re-queued for re-dispatch "+
			"(attempt %d/%d, ward#1149)", e.RedispatchAttempts, redispatchAttemptCap),
	}
	fmt.Fprintf(os.Stderr, "  %s#%d -> queued (needs-redispatch marker swept, attempt %d/%d)\n", repo, e.Num, e.RedispatchAttempts, redispatchAttemptCap)
	return true
}

func backlogRefreshReservationTracked(e *backlogEntry) bool {
	if backlogKindOf(e.Kind) != backlogKindIssue || e.Lane != "headless" {
		return false
	}
	switch e.State {
	case "done", "submitted", "merge-ready", "blocked", "failed":
		return false
	default:
		return true
	}
}

// backlogRefreshReservationHold keeps the cache aligned to the thread state.
// The thread is canonical, and the local cache only annotates it.
func (r *Runner) backlogRefreshReservationHold(repo string, e *backlogEntry, comments []issueComment, now time.Time, ttl time.Duration) bool {
	state := backlogReservationState(comments, now, ttl)
	switch state {
	case backlogReservationWaitingReaper:
		if e.State == state {
			return false
		}
		e.State = state
		e.Container = ""
		e.DispatchedAt = ""
		fmt.Fprintf(os.Stderr, "  %s#%d -> %s (fresh reservation still waiting for reap/TTL)\n", repo, e.Num, state)
		return true
	case backlogReservationSafeToRedispatch:
		if e.State == state {
			return false
		}
		e.State = state
		e.Container = ""
		e.DispatchedAt = ""
		fmt.Fprintf(os.Stderr, "  %s#%d -> %s (reservation stale)\n", repo, e.Num, state)
		return true
	default:
		if e.State == backlogReservationWaitingReaper || e.State == backlogReservationSafeToRedispatch {
			e.State = "queued"
			e.Container = ""
			e.DispatchedAt = ""
			fmt.Fprintf(os.Stderr, "  %s#%d -> queued (reservation marker cleared)\n", repo, e.Num)
			return true
		}
		return false
	}
}

// backlogDispatchOne launches one queued issue and records the transition. A launch error
// is classified (ward#352): a reservation conflict defers, anything else parks failed.
func (r *Runner) backlogDispatchOne(ctx context.Context, label string, dispatch dispatchEngineer, p *backlogEntry) error {
	ref := agentIssueRef{Owner: ownerOf(p.repo), Repo: nameOf(p.repo), Number: p.Num}
	fmt.Fprintf(os.Stderr, "%s: dispatching %s ...\n", label, ref)
	if derr := r.backlogDispatch(ctx, dispatch, ref); derr != nil {
		state, outcome, deferred := directorDispatchDisposition(derr)
		if deferred {
			fmt.Fprintf(os.Stderr, "%s: deferring %s: %v (left eligible, retried on a later tick)\n", label, ref, derr)
		} else {
			fmt.Fprintf(os.Stderr, "%s: dispatch FAILED for %s: %v\n", label, ref, derr)
		}
		return r.updateBacklogEntry(p.repo, p.Num, func(e *backlogEntry) {
			e.State = state
			e.LastOutcome = outcome
		})
	}
	container := backlogDispatchContainerName(dispatch, ref)
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

// backlogDispatchContainerName renders the issue-scoped container name.
// The director records it without asking Docker.
func backlogDispatchContainerName(dispatch dispatchEngineer, ref agentIssueRef) string {
	return issueScopedContainerName(roleEngineer, dispatch.harness, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
}

// directorDispatchDisposition classifies a dispatch error for the ledger (ward#352,
// ward#524, ward#527). See docs/agent-director-dispatch.md.
func directorDispatchDisposition(err error) (state string, outcome *backlogOutcome, deferred bool) {
	if isEngineerCapacityError(err) || isOpenPRBackpressureError(err) {
		return "queued", &backlogOutcome{Status: "deferred", Text: backlogTruncate(err.Error(), 300)}, true
	}
	if isDispatchDecline(err) {
		return "failed", &backlogOutcome{Status: "declined", Text: backlogTruncate(err.Error(), 300)}, false
	}
	return "queued", &backlogOutcome{Status: "deferred", Text: backlogTruncate(err.Error(), 300)}, true
}

// isDispatchDecline reports whether err is a coded per-issue decline (NO-GO / wrong-repo
// / untrusted-owner / issue-closed / mode-ceiling). See docs/agent-director-dispatch.md.
func isDispatchDecline(err error) bool {
	c := exitcode.From(err)
	if c == nil {
		return false
	}
	switch c.Code() {
	case dispatchNoGo, dispatchWrongRepo, dispatchUntrustedOwner, dispatchIssueClosed, dispatchModeCeiling:
		// A closed issue or below-ceiling mode label is terminal: retrying rediscovers
		// the same refusal, so the director marks it failed (ward#600, ward#607).
		return true
	}
	return false
}

// backlogDispatch launches one issue's headless run in-process via the engineer command
// (ward#347), forwarding director's container/harness flags into its argv (ward#355).
func (r *Runner) backlogDispatch(ctx context.Context, dispatch dispatchEngineer, ref agentIssueRef) error {
	cmd := agentEngineerCommand()
	return cmd.Run(ctx, dispatch.engineerArgv(ref))
}

// backlogPoll reconciles each dispatched issue across the scope against reality.
func (r *Runner) backlogPoll(ctx context.Context, label string, repos []string) {
	cl, err := r.hostTrackerClient(ctx, trackerForgejo, currentAgentMode())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot poll (%v)\n", label, err)
		return
	}
	for _, repo := range repos {
		r.backlogPollRepo(ctx, label, repo, cl)
	}
}

// backlogPollRepo reconciles one repo's dispatched issues and saves on any change.
func (r *Runner) backlogPollRepo(ctx context.Context, label, repo string, cl Tracker) {
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

// backlogReconcile moves one exited dispatched entry to its outcome state.
// The issue thread is canonical, and local container state is only a cache.
func (r *Runner) backlogReconcile(ctx context.Context, cl Tracker, repo string, tr targetRepo, e *backlogEntry) bool {
	if e.State != "dispatched" {
		return false
	}
	comments, cerr := cl.ListIssueComments(ctx, tr.Owner, tr.Name, e.Num)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "backlog: note: cannot read outcome for %s#%d (%v)\n", repo, e.Num, cerr)
		return false
	}
	now := time.Now().UTC()
	if who, held := freshReservationComment(comments, now, agentReservationTTL()); held {
		fmt.Fprintf(os.Stderr, "  %s#%d remains dispatched under the canonical issue reservation (%s)\n", repo, e.Num, who)
		return false
	}
	outcome := parseBacklogOutcome(comments)
	if outcome == nil {
		if !prelaunchDeathRelease(comments, parseBacklogDispatchedAt(e.DispatchedAt)) {
			return false
		}
		state, oc, attempts := reconcileNoOutcome(comments, parseBacklogDispatchedAt(e.DispatchedAt), e.RedispatchAttempts)
		e.State = state
		e.LastOutcome = oc
		e.RedispatchAttempts = attempts
		if state == "queued" {
			// Re-queued for another attempt: drop the dead run's dispatch record so the
			// next tick re-dispatches cleanly (ward#595).
			e.Container = ""
			e.DispatchedAt = ""
		}
		fmt.Fprintf(os.Stderr, "  %s#%d -> %s%s\n", repo, e.Num, e.State, suffixText(oc.Text))
		return true
	}
	e.State = backlogOutcomeState(outcome.Status)
	e.LastOutcome = outcome
	fmt.Fprintf(os.Stderr, "  %s#%d -> %s%s\n", repo, e.Num, e.State, suffixText(outcome.Text))
	return true
}

// redispatchAttemptCap bounds the deterministic pre-launch-death retry (ward#595):
// a multi-host fleet retries on a healthy host, a sick host exhausts the cap.
const redispatchAttemptCap = 3

// parseBacklogDispatchedAt parses an entry's RFC3339 dispatch stamp, zero-time on
// empty/malformed (so a release still counts as a pre-launch death when unknown).
func parseBacklogDispatchedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// prelaunchDeathRelease reports whether the thread has a reservation-release marker
// at/after dispatchedAt - a pre-launch death, not a run that ran (ward#595/#264).
func prelaunchDeathRelease(comments []issueComment, dispatchedAt time.Time) bool {
	for _, c := range comments {
		if trustedMachineComment(c, recordKindReservationRelease) && !c.CreatedAt.Before(dispatchedAt) {
			return true
		}
	}
	return false
}

// reconcileNoOutcome classifies a gone dispatched container with no terminal outcome.
// A pre-launch death re-queues (bounded), else it parks failed (ward#595, docs).
func reconcileNoOutcome(comments []issueComment, dispatchedAt time.Time, attempts int) (state string, outcome *backlogOutcome, nextAttempts int) {
	if !prelaunchDeathRelease(comments, dispatchedAt) {
		return "failed", &backlogOutcome{
			Status: "exited-no-outcome",
			Text:   "container exited without a WARD-WORKFLOW comment; read its log",
		}, attempts
	}
	attempts++
	if attempts >= redispatchAttemptCap {
		return "blocked", &backlogOutcome{
			Status: "orphaned-needs-redispatch",
			Text: fmt.Sprintf("container died pre-launch %d× (smoke-test death, ward#222/#264/#595); re-dispatch cap "+
				"reached - fix the host or re-dispatch by hand", attempts),
		}, attempts
	}
	return "queued", &backlogOutcome{
		Status: "prelaunch-death-requeued",
		Text: fmt.Sprintf("container died pre-launch (smoke-test death, ward#595); re-queued for re-dispatch "+
			"(attempt %d/%d)", attempts, redispatchAttemptCap),
	}, attempts
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
				backlogEntryDisplayRef(e), tierOrDash(e.Tier), stateOrDash(e.State), backlogTruncate(e.Title, 60))
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
			marker = "  -> ward agent engineer " + p.repo + "#" + strconv.Itoa(p.Num)
		}
		fmt.Fprintf(&b, "    %-30s [%-2s] %s%s\n",
			p.repo+"#"+strconv.Itoa(p.Num), tierOrDash(p.Tier), backlogTruncate(p.Title, 50), marker)
	}
	return r.emit(b.String())
}

func backlogEntryDisplayRef(e *backlogEntry) string {
	return backlogEntryKindPrefix(e.Kind) + " " + e.repo + "#" + strconv.Itoa(e.Num)
}

func backlogEntryKindPrefix(kind string) string {
	switch backlogKindOf(kind) {
	case backlogKindPullRequest:
		return "PR"
	default:
		return "issue"
	}
}

// combineOpenBacklogIssues folds the live open issue and PR feeds into one ranking
// input while preserving every issue row and tagging PR rows explicitly.
func combineOpenBacklogIssues(issues, prs []backlogIssue) []backlogIssue {
	out := make([]backlogIssue, 0, len(issues)+len(prs))
	out = append(out, issues...)
	for _, pr := range prs {
		pr.Kind = backlogKindPullRequest
		out = append(out, pr)
	}
	return out
}

// backlogPrintDirectorPlan renders director's OWN container/harness plan for --print
// (ward#355): the harness split, the image pin, the dispatch argv. Launches nothing.
func (r *Runner) backlogPrintDirectorPlan(label string, repos []string, cfg backlogConfig) error {
	cy := cfg.dispatch
	var b strings.Builder
	fmt.Fprintf(&b, "\n# %s (print)\n", label)
	fmt.Fprintf(&b, "scope:           %s\n", strings.Join(repos, ", "))
	if err := appendDirectorLaunchConfig(&b, cfg); err != nil {
		return err
	}
	// Show the exact argv each dispatch forwards, with a placeholder ref slot.
	argv := cy.engineerArgv(agentIssueRef{Owner: "owner", Repo: "repo", Number: 0})
	argv[1] = "<owner/repo#N>"
	fmt.Fprintf(&b, "dispatch:        ward agent %s\n", strings.Join(argv, " "))
	return r.emit(b.String())
}

// appendDirectorLaunchConfig renders native launch policy. AOSguard's operator
// config is intentionally absent from this control-plane path.
func appendDirectorLaunchConfig(b *strings.Builder, cfg backlogConfig) error {
	cy := cfg.dispatch
	fmt.Fprintf(b, "config source:   typed defaults + YAML overrides\n")
	fmt.Fprintf(b, "director harness: %s (its own heartbeat one-shot + drain surface)\n", cfg.mode)
	fmt.Fprintf(b, "limit:           %d\n", cfg.limit)
	fmt.Fprintf(b, "max-parallel:    %d\n", cfg.maxParallel)
	fmt.Fprintf(b, "poll-interval:   %s\n", cfg.pollInterval)
	fmt.Fprintf(b, "max-cycles:      %d\n", cfg.maxCycles)
	fmt.Fprintf(b, "burndown:        %t\n", cfg.burndown)
	fmt.Fprintf(b, "engineer-harness: %s\n", cy.harness)
	fmt.Fprintf(b, "image:           %s\n", imageRef(cy.image, cy.tag))
	fmt.Fprintf(b, "ward-version:    %s\n", wardVersionLaunchLabel(cy.wardVersion, cy.wardVersionSource))
	if cfg.wardSource != "" {
		fmt.Fprintf(b, "ward-source:     %s (surface session builds ward from here)\n", cfg.wardSource)
	}
	if cfg.contextBundle != "" {
		fmt.Fprintf(b, "context-bundle:  %s (director surface only)\n", cfg.contextBundle)
	}
	fmt.Fprintf(b, "no-pull:         %t\n", cfg.noPull)
	fmt.Fprintf(b, "override-reservation: %t (propagated to engineers; default defers on a reservation conflict)\n", cy.overrideReservation)
	fmt.Fprintf(b, "verification-fixture: %t (exact admitted issue; engineer forced to remote-branch-only)\n", cy.verificationFixture)
	if len(cfg.withRepo) > 0 {
		fmt.Fprintf(b, "with-repo:       %s (cloned into the surface session)\n", strings.Join(cfg.withRepo, ", "))
	}
	return nil
}

// backlogPrintSummary prints the terminal disposition of the run by state.
func (r *Runner) backlogPrintSummary(repos []string) error {
	counts := map[string]int{}
	for _, e := range r.backlogScopeEntries(repos) {
		counts[e.State]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nbacklog summary (%s):\n", strings.Join(repos, ", "))
	for _, st := range backlogStateSummaryOrder() {
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
