package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
	"github.com/urfave/cli/v3"
)

// agent_review_stats.go is the measurement surface (ward#134): pass/block/advisory rates
// per class + the false-negative rate. See docs/dispatch-review-measurement.md.

// agentReviewStatsCommand reads the persisted panel log and reports aggregates.
func agentReviewStatsCommand() *cli.Command {
	return &cli.Command{
		Name:  "stats",
		Usage: "Report panel calibration from the persisted review-panel log (rates per class; false-negatives vs a reverted-issue set).",
		Description: `stats aggregates the sidecar review-panel log beside the audit
trail. It surfaces the pass/block/advisory split overall and per class - the
precision signal the per-class threshold is tuned against. Pass --reverted with a
comma-separated list of issue refs that later had to be reverted/fixed to compute
the FALSE-NEGATIVE rate: of the diffs the panel passed, how many should have been
blocked. That is the dangerous number. See docs/dispatch-review.md.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "reverted", Usage: "comma-separated issue refs (owner/repo#N) whose merged diff was later reverted/fixed - joins to passed panels to compute the false-negative rate"},
			&cli.BoolFlag{Name: "json", Usage: "emit the aggregate as JSON"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name: "agent.review.stats",
				Action: func(_ context.Context, cmd *cli.Command) error {
					return r.runAgentReviewStats(cmd)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// reviewStats is the computed aggregate over the panel log.
type reviewStats struct {
	Total         int                    `json:"total"`
	Passed        int                    `json:"passed"`
	Blocked       int                    `json:"blocked"`
	Advisory      int                    `json:"advisory"`
	ByClass       map[string]classCounts `json:"by_class"`
	Reverted      int                    `json:"reverted_passed,omitempty"`
	FalseNegRate  float64                `json:"false_negative_rate,omitempty"`
	FalseNegKnown bool                   `json:"false_negative_measured"`
}

// classCounts is the pass/block/advisory split for one class.
type classCounts struct {
	Passed   int `json:"passed"`
	Blocked  int `json:"blocked"`
	Advisory int `json:"advisory"`
}

// runAgentReviewStats reads the log, computes the aggregate, and renders it.
func (r *Runner) runAgentReviewStats(c *cli.Command) error {
	rows, err := readPanelLog()
	if err != nil {
		return fmt.Errorf("ward agent review stats: %w", err)
	}
	reverted := parseRevertedSet(c.String("reverted"))
	stats := computeReviewStats(rows, reverted)

	if c.Bool("json") {
		b, merr := json.MarshalIndent(stats, "", "  ")
		if merr != nil {
			return merr
		}
		writeln(r.Runner.Stdout, string(b))
		return nil
	}
	renderReviewStats(r.Runner.Stdout, stats)
	return nil
}

// readPanelLog loads every persisted panel row; a missing log reads as empty.
func readPanelLog() ([]reviewpanel.PanelResult, error) {
	path, err := panelLogPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) //nolint:gosec // config-resolved sidecar path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []reviewpanel.PanelResult
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec reviewpanel.PanelResult
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // a malformed row is skipped, not fatal
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// parseRevertedSet turns the --reverted flag into a lookup set of issue refs.
func parseRevertedSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			set[p] = true
		}
	}
	return set
}

// computeReviewStats aggregates the log and computes the false-negative rate from the
// reverted set. Block precision needs a human label per block (see the doc).
func computeReviewStats(rows []reviewpanel.PanelResult, reverted map[string]bool) reviewStats {
	s := reviewStats{ByClass: map[string]classCounts{}}
	revertedPassed := 0
	for _, row := range rows {
		s.Total++
		cc := s.ByClass[string(row.Class)]
		switch row.Gate {
		case reviewpanel.GatePass:
			s.Passed++
			cc.Passed++
			if reverted[row.Issue] {
				revertedPassed++
			}
		case reviewpanel.GateBlock:
			s.Blocked++
			cc.Blocked++
		case reviewpanel.GateAdvisory:
			s.Advisory++
			cc.Advisory++
		}
		s.ByClass[string(row.Class)] = cc
	}
	if len(reverted) > 0 {
		s.FalseNegKnown = true
		s.Reverted = revertedPassed
		if s.Passed > 0 {
			s.FalseNegRate = float64(revertedPassed) / float64(s.Passed)
		}
	}
	return s
}

// renderReviewStats prints the human-readable aggregate.
func renderReviewStats(w interface{ Write([]byte) (int, error) }, s reviewStats) {
	writef(w, "review-panel stats: %d panels - %d passed, %d blocked, %d advisory\n",
		s.Total, s.Passed, s.Blocked, s.Advisory)
	classes := make([]string, 0, len(s.ByClass))
	for k := range s.ByClass {
		classes = append(classes, k)
	}
	sort.Strings(classes)
	for _, k := range classes {
		cc := s.ByClass[k]
		writef(w, "  %-14s pass %d / block %d / advisory %d\n", k, cc.Passed, cc.Blocked, cc.Advisory)
	}
	if s.FalseNegKnown {
		writef(w, "false-negative rate: %d/%d passed diffs were later reverted (%.1f%%) - the dangerous number\n",
			s.Reverted, s.Passed, s.FalseNegRate*100)
	} else {
		writef(w, "false-negative rate: not measured (pass --reverted <issue,...> to compute)\n")
	}
	writef(w, "block precision: needs a human label per blocked diff (a block never merges) - see docs/dispatch-review.md\n")
}
