package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
	"github.com/urfave/cli/v3"
)

// ci.go is `ward ci watch`, the native hand-written successor to the retired
// scripts/watch-ci.sh (ward#88). See docs/ci-watch.md for the why.

// ci watch tunables, mirroring the retired watch-ci.sh defaults so the swap is
// behaviour-preserving. The flags still read its WATCH_CI_* env vars.
const (
	ciDefaultRepo     = "coilyco-flight-deck/ward"
	ciDefaultInterval = 10 * time.Second
	ciDefaultTimeout  = 30 * time.Minute
	ciDefaultLimit    = 40
)

// ciTask is one row of Forgejo's ListActionTasks response - a single job of a
// workflow run. Only the fields the watcher needs are decoded.
type ciTask struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	RunNumber int64  `json:"run_number"`
	HeadSHA   string `json:"head_sha"`
	HTMLURL   string `json:"html_url"`
}

// ciTasksResponse is the envelope ListActionTasks returns: the workflow_runs
// array (one entry per job) plus a total count.
type ciTasksResponse struct {
	TotalCount   int64    `json:"total_count"`
	WorkflowRuns []ciTask `json:"workflow_runs"`
}

// ciTerminalStatuses are the Forgejo task statuses that mean a job stopped
// moving; anything else keeps the watcher polling. Mirrors the script.
var ciTerminalStatuses = map[string]bool{
	"success": true, "failure": true, "failed": true,
	"cancelled": true, "canceled": true, "skipped": true, "error": true,
}

// ciIsTerminal reports whether status is a settled (non-polling) state.
func ciIsTerminal(status string) bool {
	return ciTerminalStatuses[strings.ToLower(strings.TrimSpace(status))]
}

// ciIsFailure reports whether a terminal status counts as a CI failure. Like
// the script, cancelled and skipped are terminal but not failures.
func ciIsFailure(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failure", "failed", "error":
		return true
	default:
		return false
	}
}

// ciLatestRun returns the highest run_number present in tasks. ok is false when
// there are no tasks at all (the "no run found" pre-flight).
func ciLatestRun(tasks []ciTask) (run int64, ok bool) {
	for _, t := range tasks {
		if !ok || t.RunNumber > run {
			run, ok = t.RunNumber, true
		}
	}
	return run, ok
}

// ciRunTasks returns just the tasks belonging to run.
func ciRunTasks(tasks []ciTask, run int64) []ciTask {
	var out []ciTask
	for _, t := range tasks {
		if t.RunNumber == run {
			out = append(out, t)
		}
	}
	return out
}

// ciPending counts tasks whose status is not yet terminal.
func ciPending(tasks []ciTask) int {
	n := 0
	for _, t := range tasks {
		if !ciIsTerminal(t.Status) {
			n++
		}
	}
	return n
}

// ciFailures returns the tasks that ended in a failure status.
func ciFailures(tasks []ciTask) []ciTask {
	var out []ciTask
	for _, t := range tasks {
		if ciIsFailure(t.Status) {
			out = append(out, t)
		}
	}
	return out
}

// ciOutcome is the watcher's terminal verdict, mapped to an exit code by
// reportCIOutcome.
type ciOutcome int

const (
	ciPassed   ciOutcome = iota // every job terminal, none failed -> exit 0
	ciFailed                    // every job terminal, one+ failed  -> exit 1
	ciTimedOut                  // timeout with jobs still running  -> exit 2
	ciNoRun                     // no matching run in the listing   -> exit 3
)

// ciWatcher polls a Forgejo Actions run to completion. list, sleep, and out are
// injected so the poll loop is unit-testable without a network or a real clock.
type ciWatcher struct {
	list     func(ctx context.Context) ([]ciTask, error)
	sleep    func(time.Duration)
	interval time.Duration
	timeout  time.Duration
	out      io.Writer // progress lines (stderr)
}

// watch polls until every job of run is terminal or the timeout elapses; run 0
// resolves to the latest run in the listing. Returns that run, its tasks, outcome.
func (w *ciWatcher) watch(ctx context.Context, run int64) (int64, []ciTask, ciOutcome, error) {
	var elapsed time.Duration
	for {
		tasks, err := w.list(ctx)
		if err != nil {
			return run, nil, ciNoRun, err
		}
		if run == 0 {
			latest, ok := ciLatestRun(tasks)
			if !ok {
				return 0, nil, ciNoRun, nil
			}
			run = latest
		}
		runTasks := ciRunTasks(tasks, run)
		if len(runTasks) == 0 {
			return run, nil, ciNoRun, nil
		}
		if pending := ciPending(runTasks); pending == 0 {
			if len(ciFailures(runTasks)) > 0 {
				return run, runTasks, ciFailed, nil
			}
			return run, runTasks, ciPassed, nil
		} else if elapsed >= w.timeout {
			return run, runTasks, ciTimedOut, nil
		} else {
			fmt.Fprintf(w.out, "run#%d: %d job(s) still running, polling in %s...\n", run, pending, w.interval)
			w.sleep(w.interval)
			elapsed += w.interval
		}
	}
}

// ciStatusTable renders the per-job status block printed once a run is terminal:
// a header line then "  <status>  <job>" per job, sorted by job name.
func ciStatusTable(repo string, run int64, tasks []ciTask) string {
	sorted := append([]ciTask(nil), tasks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var b strings.Builder
	fmt.Fprintf(&b, "run#%d on %s:\n", run, repo)
	for _, t := range sorted {
		fmt.Fprintf(&b, "  %-10s %s\n", t.Status, t.Name)
	}
	return b.String()
}

// ciFailureReport renders the failing-job pointers printed after the status
// table: job identity plus a run-page URL. See docs/ci-watch.md for the logs gap.
func ciFailureReport(owner, repo string, fails []ciTask) string {
	sorted := append([]ciTask(nil), fails...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var b strings.Builder
	b.WriteString("failing jobs (open the run page for each log):\n")
	for _, t := range sorted {
		url := t.HTMLURL
		if url == "" {
			url = fmt.Sprintf("%s/%s/%s/actions", forgejoBaseURL, owner, repo)
		}
		fmt.Fprintf(&b, "  %-10s %s  %s\n", t.Status, t.Name, url)
	}
	return b.String()
}

// listActionTasks GETs one page of a repo's Forgejo Actions tasks (the surface
// `ward ops forgejo tasks list` exposes), mirroring fetchForgejoIssue's shape.
func (r *Runner) listActionTasks(ctx context.Context, owner, repo string, limit int) ([]ciTask, error) {
	token, err := r.forgejoAPIToken(ctx)
	if err != nil {
		return nil, err
	}
	target := fmt.Sprintf("%s/api/v1/repos/%s/%s/actions/tasks?limit=%d",
		strings.TrimSuffix(forgejoBaseURL, "/"), owner, repo, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("ci watch: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: forgejoAPIHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ci watch: GET %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ci watch: GET %s returned HTTP %d: %s",
			target, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out ciTasksResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("ci watch: parse %s: %w", target, err)
	}
	return out.WorkflowRuns, nil
}

// ciCommand groups ward's CI helpers. Today it carries `ward ci watch`, the
// native replacement for scripts/watch-ci.sh.
func ciCommand() *cli.Command {
	return &cli.Command{
		Name:     "ci",
		Usage:    "CI helpers: watch a Forgejo Actions run to completion.",
		Commands: []*cli.Command{ciWatchCommand()},
	}
}

// ciWatchCommand builds `ward ci watch [owner/repo]`.
func ciWatchCommand() *cli.Command {
	return &cli.Command{
		Name:      "watch",
		Usage:     "watch a Forgejo Actions run until every job is terminal, then report failures.",
		ArgsUsage: "[owner/repo]",
		Description: `watch polls the Forgejo Actions tasks for owner/repo (default
` + ciDefaultRepo + `) every --interval until every job of the target run reaches a
terminal state, then prints a per-job status table. With no --run it tracks the
latest run in the listing.

Exit codes: 0 all jobs passed, 1 a job failed, 2 timed out, 3 no run found.

Inline tailing of a failing job's log waits on a Forgejo task-logs API surface
(gitea#35176); until then watch points at each failing job's run page. The
tunable flags also read the WATCH_CI_* env vars carried over from the retired
watch-ci.sh script.`,
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "run", Usage: "run number to watch (default: latest in the listing)"},
			&cli.IntFlag{
				Name:    "interval",
				Value:   int(ciDefaultInterval.Seconds()),
				Usage:   "poll interval in seconds",
				Sources: cli.EnvVars("WATCH_CI_INTERVAL"),
			},
			&cli.IntFlag{
				Name:    "timeout",
				Value:   int(ciDefaultTimeout.Seconds()),
				Usage:   "max wait in seconds before giving up",
				Sources: cli.EnvVars("WATCH_CI_TIMEOUT"),
			},
			&cli.IntFlag{
				Name:    "limit",
				Value:   ciDefaultLimit,
				Usage:   "task-list page size",
				Sources: cli.EnvVars("WATCH_CI_LIMIT"),
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return newRunner().ciWatchAction(ctx, c)
		},
	}
}

// ciWatchAction wires `ward ci watch` through ward's audit + argv-validation
// pipeline, then drives the poll loop and maps its outcome to an exit code.
func (r *Runner) ciWatchAction(ctx context.Context, c *cli.Command) error {
	repoArg := ciDefaultRepo
	if c.Args().Len() > 0 {
		repoArg = c.Args().Get(0)
	}
	owner, repo, ok := splitOwnerName(repoArg)
	if !ok {
		return exitcode.New(exitcode.UserError, "user_error",
			fmt.Errorf("ci watch: repo %q must be owner/name", repoArg),
			"pass the run's repo as owner/name, e.g. coilyco-flight-deck/ward")
	}
	run := int64(c.Int("run"))
	limit := c.Int("limit")
	interval := time.Duration(c.Int("interval")) * time.Second
	timeout := time.Duration(c.Int("timeout")) * time.Second

	spec := verb.Spec{
		Name: "ci.watch",
		ArgsFunc: func(_ *cli.Command) (map[string]string, []string) {
			return map[string]string{
				"--run":      fmt.Sprintf("%d", run),
				"--limit":    fmt.Sprintf("%d", limit),
				"--interval": interval.String(),
				"--timeout":  timeout.String(),
			}, []string{repoArg}
		},
		Action: func(ctx context.Context, _ *cli.Command) error {
			w := &ciWatcher{
				list:     func(ctx context.Context) ([]ciTask, error) { return r.listActionTasks(ctx, owner, repo, limit) },
				sleep:    time.Sleep,
				interval: interval,
				timeout:  timeout,
				out:      os.Stderr,
			}
			resolvedRun, tasks, outcome, err := w.watch(ctx, run)
			if err != nil {
				return err
			}
			return reportCIOutcome(os.Stdout, owner, repo, repoArg, resolvedRun, tasks, outcome)
		},
	}
	return r.WrapVerb(spec, r.Audit)(ctx, c)
}

// reportCIOutcome prints the status table (or the timeout/no-run message) and
// returns the coded error mapping to exit 0/1/2/3 = passed/failed/timeout/no-run.
func reportCIOutcome(out io.Writer, owner, repo, repoArg string, run int64, tasks []ciTask, outcome ciOutcome) error {
	switch outcome {
	case ciNoRun:
		return exitcode.New(3, "ci_no_run",
			fmt.Errorf("ci watch: no run found for %s", repoArg),
			"check the repo has Actions runs, or pass --run with a known run number")
	case ciTimedOut:
		fmt.Fprint(out, ciStatusTable(repoArg, run, tasks))
		return exitcode.New(2, "ci_timeout",
			fmt.Errorf("ci watch: timed out with %d job(s) still running on run#%d", ciPending(tasks), run),
			"raise --timeout, or inspect the run for a stuck job")
	default:
		fmt.Fprint(out, ciStatusTable(repoArg, run, tasks))
		fails := ciFailures(tasks)
		if len(fails) == 0 {
			fmt.Fprintln(out, "all jobs passed.")
			return nil
		}
		fmt.Fprint(out, "\n"+ciFailureReport(owner, repo, fails))
		return exitcode.New(1, "ci_failed",
			fmt.Errorf("ci watch: %d job(s) failed on run#%d", len(fails), run),
			"open the failing job's run page above for its log")
	}
}
