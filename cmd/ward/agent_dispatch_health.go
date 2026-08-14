package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"github.com/urfave/cli/v3"
)

const agentDispatchHealthSchemaVersion = 4

type dispatchHealthJSON struct {
	SchemaVersion    int      `json:"schema_version"`
	GeneratedAt      string   `json:"generated_at"`
	Scope            []string `json:"scope"`
	Queued           int      `json:"queued"`
	InFlight         int      `json:"in_flight"`
	Held             int      `json:"held"`
	Submitted        int      `json:"submitted"`
	MergeReady       int      `json:"merge_ready"`
	Deferred         int      `json:"deferred"`
	Failed           int      `json:"failed"`
	Running          int      `json:"running"`
	PartialLaunch    int      `json:"partial_launch"`
	LaunchIntents    int      `json:"launch_intents"`
	CleanupNeeded    int      `json:"cleanup_needed"`
	FailedBefore     int      `json:"failed_before_start"`
	RecentDispatches int      `json:"recent_dispatches"`
	StalePrelaunch   int      `json:"stale_prelaunch"`
	DuplicateRefs    []string `json:"duplicate_refs,omitempty"`
	Signals          []string `json:"signals,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Backpressure     bool     `json:"backpressure"`
	Runaway          bool     `json:"runaway"`
	Summary          string   `json:"summary"`
}

// dispatchHealthReport is the shared computed view behind the status line,
// operator HUD, and alert line.
type dispatchHealthReport struct {
	Scope            []string
	Queued           int
	InFlight         int
	Held             int
	Submitted        int
	MergeReady       int
	Deferred         int
	Failed           int
	Running          int
	PartialLaunch    int
	LaunchIntents    int
	CleanupNeeded    int
	FailedBefore     int
	RecentDispatches int
	StalePrelaunch   int
	DuplicateRefs    []string
	Warnings         []string
	Backpressure     bool
	Runaway          bool
	Signals          []string
}

func dispatchHealthFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: the cwd git origin)"},
		&cli.StringSliceFlag{Name: "org", Usage: "expand every repo an org owns into the scope (owner; repeatable), unioned with --repo and de-duped"},
		&cli.IntFlag{Name: "limit", Value: directorLimitDefault(), Usage: "open issues read per repo for the health snapshot"},
		&cli.IntFlag{Name: "max-parallel", Value: directorMaxParallelDefault(), Usage: "in-flight engineer cap used to judge backpressure from typed defaults or ~/.ward/config.yaml"},
		&cli.BoolFlag{Name: "json", Usage: "emit the stable machine-readable JSON schema"},
		&cli.BoolFlag{Name: "line", Usage: "emit the single-line summary used by the Claude status line"},
	}
}

func agentDispatchHealthCommand() *cli.Command {
	return &cli.Command{
		Name:  "dispatch-health",
		Usage: "Show the live dispatch pathology summary for the current director scope, plus the one-line status feed used by Claude Code.",
		Description: `dispatch-health summarizes the live issue-thread lifecycle and running
engineer containers. It is the operator HUD behind the dispatch-health
status line, and it is the stable line the alert hook mirrors into the log stream.

  ward agent dispatch-health
  ward agent dispatch-health --line
  ward agent dispatch-health --json

See docs/agent-dispatch-health.md.`,
		Flags: dispatchHealthFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.dispatch-health",
				SkipPolicy: true, // reads backlog + docker state only; no repo tree mutation
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runAgentDispatchHealth(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

func (r *Runner) runAgentDispatchHealth(ctx context.Context, c *cli.Command) error {
	label := "ward agent dispatch-health"
	repos, err := r.resolveDirectorScope(ctx, c, label)
	if err != nil {
		return err
	}
	if err := r.directorTrustGate(label, repos); err != nil {
		return err
	}
	maxParallel := c.Int("max-parallel")
	if maxParallel < 1 {
		maxParallel = directorMaxParallelDefault()
	}
	limit := c.Int("limit")
	if limit < 1 {
		limit = directorLimitDefault()
	}
	items, queueErr := collectDirectorQueueItems(ctx, r.hostForgejoClient(ctx), repos, limit, time.Now().UTC(), agentReservationTTL())
	report := r.dispatchHealthSnapshotWithQueue(ctx, repos, maxParallel, items)
	if queueErr != nil {
		report.Warnings = append(report.Warnings, firstLine(queueErr.Error()))
	}
	if report.alertable() {
		fmt.Fprintf(os.Stderr, "WARD-DISPATCH-HEALTH: %s\n", report.summaryLine())
		if notifyDispatchHealth(ctx, report) {
			fmt.Fprintf(os.Stderr, "%s: desktop notification sent\n", label)
		}
	}
	if c.Bool("json") {
		payload := report.toJSON()
		payload.SchemaVersion = agentDispatchHealthSchemaVersion
		payload.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
		buf, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		return r.emit(string(buf) + "\n")
	}
	if c.Bool("line") {
		return r.emit(report.summaryLine() + "\n")
	}
	return r.emit(report.human() + "\n")
}

func (r *Runner) dispatchHealthSnapshot(ctx context.Context, repos []string, maxParallel int) dispatchHealthReport {
	items, err := collectDirectorQueueItems(ctx, r.hostForgejoClient(ctx), repos, directorLimitDefault(), time.Now().UTC(), agentReservationTTL())
	report := r.dispatchHealthSnapshotWithQueue(ctx, repos, maxParallel, items)
	if err != nil {
		report.Warnings = append(report.Warnings, firstLine(err.Error()))
	}
	return report
}

func (r *Runner) dispatchHealthSnapshotWithQueue(ctx context.Context, repos []string, maxParallel int, items []directorQueueItem) dispatchHealthReport {
	rows, err := r.agentListRows(ctx)
	report := dispatchHealthReport{
		Scope: repos,
	}
	if err != nil {
		report.Warnings = append(report.Warnings, firstLine(err.Error()))
		rows = nil
	}
	now := time.Now().UTC()
	scope := map[string]bool{}
	for _, repo := range repos {
		scope[repo] = true
	}
	stale, serr := r.stalePrelaunchReservations(ctx, now, scope)
	if serr != nil {
		report.Warnings = append(report.Warnings, firstLine(serr.Error()))
	}
	tracker, trackerErr := r.hostTrackerClient(ctx, trackerForgejo, currentAgentMode())
	if trackerErr != nil {
		report.Warnings = append(report.Warnings, firstLine(trackerErr.Error()))
		tracker = nil
	}
	activeIssue := dispatchHealthActiveIssueFilter(ctx, tracker)
	visibleRows := dispatchHealthVisibleRows(rows, scope, activeIssue)
	runningRows := dispatchHealthRunningRows(visibleRows)
	inv := agentLaunchInventoryFromRowsWithScope(visibleRows, scope)
	report.Running = inv.Running
	report.PartialLaunch = inv.PartialLaunch
	report.LaunchIntents = inv.LaunchIntents
	report.CleanupNeeded = inv.CleanupNeeded
	report.FailedBefore = inv.FailedBefore
	dispatchHealthTallyQueueItems(&report, items)
	report.RecentDispatches, report.DuplicateRefs = dispatchHealthRunningSignals(runningRows)
	report.StalePrelaunch = len(dispatchHealthStalePrelaunchReservations(stale, activeIssue))
	report.Held = report.StalePrelaunch
	report.Backpressure = maxParallel > 0 && report.InFlight >= maxParallel && report.Queued > 0
	report.Runaway = maxParallel > 0 && report.RecentDispatches > maxParallel*2
	report.Signals = dispatchHealthSignals(report)
	return report
}

type dispatchHealthIssueFilter func(agentIssueRef) bool

func dispatchHealthActiveIssueFilter(ctx context.Context, cl Tracker) dispatchHealthIssueFilter {
	if cl == nil {
		return nil
	}
	cache := map[string]bool{}
	return func(ref agentIssueRef) bool {
		if ref.Owner == "" || ref.Repo == "" || ref.Number <= 0 {
			return true
		}
		key := ref.String()
		if active, ok := cache[key]; ok {
			return active
		}
		issue, err := cl.GetIssue(ctx, ref.Owner, ref.Repo, ref.Number)
		if err != nil || issue == nil {
			cache[key] = true
			return true
		}
		if !strings.EqualFold(strings.TrimSpace(issue.State), "closed") {
			cache[key] = true
			return true
		}
		comments, err := cl.ListIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
		if err != nil {
			cache[key] = true
			return true
		}
		active := !dispatchHealthIssueHasDoneOutcome(comments)
		cache[key] = active
		return active
	}
}

func dispatchHealthIssueHasDoneOutcome(comments []issueComment) bool {
	for _, c := range comments {
		if !trustedMachineComment(c, recordKindOutcome) {
			continue
		}
		outcome, ok := backlogOutcomeOfComment(c.Body)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(outcome.Status), "done") {
			return true
		}
	}
	return false
}

func dispatchHealthTallyQueueItems(report *dispatchHealthReport, items []directorQueueItem) {
	for _, item := range items {
		switch item.Action {
		case directorQueueActionRedispatch:
			report.Queued++
			report.Deferred++
		case directorQueueActionInspectLogs:
			report.Failed++
		}
		switch item.State {
		case directorQueueStateRunning:
			report.InFlight++
		case directorQueueStateSubmittedPR:
			report.Submitted++
		case directorQueueStateMergeReadyPR:
			report.MergeReady++
		}
	}
}

func dispatchHealthVisibleRows(rows []agentRunningEngineer, scope map[string]bool, activeIssue dispatchHealthIssueFilter) []agentRunningEngineer {
	out := make([]agentRunningEngineer, 0, len(rows))
	for _, row := range rows {
		if len(scope) > 0 && !scope[row.Repo] {
			continue
		}
		if activeIssue != nil && !activeIssue(dispatchHealthRowRef(row)) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func dispatchHealthRunningRows(rows []agentRunningEngineer) []agentRunningEngineer {
	out := make([]agentRunningEngineer, 0, len(rows))
	for _, row := range rows {
		if row.Phase == agentLaunchPhaseRunning {
			out = append(out, row)
		}
	}
	return out
}

func dispatchHealthStalePrelaunchReservations(stale []stalePrelaunchReservation, activeIssue dispatchHealthIssueFilter) []stalePrelaunchReservation {
	if activeIssue == nil {
		return stale
	}
	out := make([]stalePrelaunchReservation, 0, len(stale))
	for _, hold := range stale {
		if !activeIssue(hold.Ref()) {
			continue
		}
		out = append(out, hold)
	}
	return out
}

func dispatchHealthRowRef(row agentRunningEngineer) agentIssueRef {
	owner, repo, ok := strings.Cut(strings.TrimSpace(row.Repo), "/")
	if !ok {
		return agentIssueRef{}
	}
	num, err := strconv.Atoi(strings.TrimSpace(row.Issue))
	if err != nil || num <= 0 {
		return agentIssueRef{}
	}
	return agentIssueRef{Owner: owner, Repo: repo, Number: num}
}

func dispatchHealthRunningSignals(rows []agentRunningEngineer) (recent int, duplicates []string) {
	counts := map[string]int{}
	for _, row := range rows {
		if row.Age <= 15*time.Minute {
			recent++
		}
		if strings.TrimSpace(row.Ref) != "" {
			counts[row.Ref]++
		}
	}
	for ref, n := range counts {
		if n > 1 {
			duplicates = append(duplicates, fmt.Sprintf("%s×%d", ref, n))
		}
	}
	sort.Strings(duplicates)
	return recent, duplicates
}

func dispatchHealthSignals(report dispatchHealthReport) []string {
	var out []string
	if report.Deferred > 0 {
		out = append(out, "deferred")
	}
	if report.Failed > 0 {
		out = append(out, "failed")
	}
	if report.CleanupNeeded > 0 || report.FailedBefore > 0 {
		out = append(out, "stale-records")
	}
	if len(report.DuplicateRefs) > 0 {
		out = append(out, "double-dispatch")
	}
	if report.Backpressure {
		out = append(out, "backpressure")
	}
	if report.Runaway {
		out = append(out, "runaway")
	}
	if report.PartialLaunch > 0 {
		out = append(out, "partial-launch")
	}
	if report.StalePrelaunch > 0 {
		out = append(out, "stale-prelaunch")
	}
	return out
}

func (r dispatchHealthReport) alertable() bool {
	return len(r.Signals) > 0
}

func (r dispatchHealthReport) alertKey() string {
	return strings.Join(append([]string{
		fmt.Sprintf("q=%d", r.Queued),
		fmt.Sprintf("i=%d", r.InFlight),
		fmt.Sprintf("h=%d", r.Held),
		fmt.Sprintf("s=%d", r.Submitted),
		fmt.Sprintf("m=%d", r.MergeReady),
		fmt.Sprintf("d=%d", r.Deferred),
		fmt.Sprintf("f=%d", r.Failed),
		fmt.Sprintf("r=%d", r.Running),
		fmt.Sprintf("pl=%d", r.PartialLaunch),
		fmt.Sprintf("li=%d", r.LaunchIntents),
		fmt.Sprintf("rd=%d", r.RecentDispatches),
		fmt.Sprintf("sp=%d", r.StalePrelaunch),
	}, r.Signals...), "|")
}

func (r dispatchHealthReport) summaryLine() string {
	if !r.alertable() {
		return fmt.Sprintf("dispatch-health: ok queued=%d inflight=%d held=%d submitted=%d merge-ready=%d running=%d partial-launch=%d launch-intents=%d cleanup-needed=%d failed-before-start=%d stale-prelaunch=%d",
			r.Queued, r.InFlight, r.Held, r.Submitted, r.MergeReady, r.Running, r.PartialLaunch, r.LaunchIntents, r.CleanupNeeded, r.FailedBefore, r.StalePrelaunch)
	}
	parts := []string{
		fmt.Sprintf("queued=%d", r.Queued),
		fmt.Sprintf("inflight=%d", r.InFlight),
		fmt.Sprintf("held=%d", r.Held),
		fmt.Sprintf("submitted=%d", r.Submitted),
		fmt.Sprintf("merge-ready=%d", r.MergeReady),
		fmt.Sprintf("deferred=%d", r.Deferred),
		fmt.Sprintf("failed=%d", r.Failed),
		fmt.Sprintf("running=%d", r.Running),
		fmt.Sprintf("partial-launch=%d", r.PartialLaunch),
		fmt.Sprintf("launch-intents=%d", r.LaunchIntents),
		fmt.Sprintf("cleanup-needed=%d", r.CleanupNeeded),
		fmt.Sprintf("failed-before-start=%d", r.FailedBefore),
		fmt.Sprintf("recent=%d", r.RecentDispatches),
		fmt.Sprintf("stale-prelaunch=%d", r.StalePrelaunch),
	}
	if len(r.DuplicateRefs) > 0 {
		parts = append(parts, "double-dispatch="+strings.Join(r.DuplicateRefs, ","))
	}
	if r.Backpressure {
		parts = append(parts, "backpressure=on")
	}
	if r.Runaway {
		parts = append(parts, "runaway=on")
	}
	return "dispatch-health: " + strings.Join(parts, " ")
}

func (r dispatchHealthReport) human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "dispatch health (%s)\n", strings.Join(r.Scope, ", "))
	fmt.Fprintf(&b, "  %s\n", r.summaryLine())
	if len(r.Signals) > 0 {
		fmt.Fprintf(&b, "  signals: %s\n", strings.Join(r.Signals, ", "))
	}
	if len(r.DuplicateRefs) > 0 {
		fmt.Fprintf(&b, "  duplicate refs: %s\n", strings.Join(r.DuplicateRefs, ", "))
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintf(&b, "  warnings: %s\n", strings.Join(r.Warnings, "; "))
	}
	return strings.TrimSpace(b.String())
}

func (r dispatchHealthReport) toJSON() dispatchHealthJSON {
	return dispatchHealthJSON{
		Scope:            append([]string{}, r.Scope...),
		Queued:           r.Queued,
		InFlight:         r.InFlight,
		Held:             r.Held,
		Submitted:        r.Submitted,
		MergeReady:       r.MergeReady,
		Deferred:         r.Deferred,
		Failed:           r.Failed,
		Running:          r.Running,
		PartialLaunch:    r.PartialLaunch,
		LaunchIntents:    r.LaunchIntents,
		CleanupNeeded:    r.CleanupNeeded,
		FailedBefore:     r.FailedBefore,
		RecentDispatches: r.RecentDispatches,
		StalePrelaunch:   r.StalePrelaunch,
		DuplicateRefs:    append([]string{}, r.DuplicateRefs...),
		Signals:          append([]string{}, r.Signals...),
		Warnings:         append([]string{}, r.Warnings...),
		Backpressure:     r.Backpressure,
		Runaway:          r.Runaway,
		Summary:          r.summaryLine(),
	}
}

func notifyDispatchHealth(ctx context.Context, report dispatchHealthReport) bool {
	title := "ward dispatch health"
	body := report.summaryLine()
	switch runtime.GOOS {
	case "darwin":
		if !desktopNotificationAvailable("osascript") {
			return false
		}
		script := fmt.Sprintf(`display notification "%s" with title "%s"`, appleScriptEscape(body), appleScriptEscape(title))
		return exec.CommandContext(ctx, "osascript", "-e", script).Run() == nil
	case "windows":
		if !desktopNotificationAvailable("powershell") {
			return false
		}
		script := windowsToastScript(title, body)
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script).Run() == nil
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		if !desktopNotificationAvailable("notify-send") {
			return false
		}
		return exec.CommandContext(ctx, "notify-send", title, body).Run() == nil
	default:
		return false
	}
}

func desktopNotificationAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func appleScriptEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func windowsToastScript(title, body string) string {
	title = windowsPowerShellEscape(title)
	body = windowsPowerShellEscape(body)
	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName WindowsBase | Out-Null
$type = [Windows.UI.Notifications.ToastTemplateType]::ToastText02
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent($type)
$texts = $xml.GetElementsByTagName('text')
$null = $texts.Item(0).AppendChild($xml.CreateTextNode('%s'))
$null = $texts.Item(1).AppendChild($xml.CreateTextNode('%s'))
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('ward').Show($toast)
`, title, body)
}

func windowsPowerShellEscape(s string) string {
	s = strings.ReplaceAll(s, `'`, `''`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
