package reviewpanel

import (
	"fmt"
	"strings"
)

// panel.go orchestrates the cost-tiered, heterogeneous panel (ward#134) over injected
// seams so the flow is unit-testable; cmd/ward supplies the real Run/Avail funcs.

// Reviewer is one candidate panelist: a family, its default model, and whether it is
// the paid tier (run only when the free tier does not settle it) or free.
type Reviewer struct {
	Family string
	Model  string
	Paid   bool
}

// RunFunc runs one reviewer with the built prompt and returns its raw stdout; a
// non-nil error is a reviewer failure and becomes a fail-closed block.
type RunFunc func(rv Reviewer, prompt string) (stdout string, err error)

// AvailFunc reports whether a reviewer can actually run here (binary present,
// credential/endpoint reachable), and a human reason when it cannot.
type AvailFunc func(rv Reviewer) (ok bool, reason string)

// Deps bundles the impure seams the orchestrator drives; cmd/ward wires the real
// subprocess + probe implementations, tests wire fakes.
type Deps struct {
	Run   RunFunc
	Avail AvailFunc
	Now   func() int64
}

// Config is one panel run's inputs; Execute filters the worker's own family out of
// Candidates itself (the worker may never review its own diff: correlated blind spots).
type Config struct {
	Worker     string
	Class      Class
	Issue      string
	SessionID  string
	Candidates []Reviewer
	Prompt     PromptInput
}

// Execute runs the panel + returns the persisted result, fail-closed: drop the worker
// family + unavailable reviewers, run free then escalate to paid (docs/dispatch-review).
func (d Deps) Execute(cfg Config) PanelResult {
	res := PanelResult{
		Timestamp: d.now(),
		Issue:     cfg.Issue,
		Worker:    cfg.Worker,
		Class:     cfg.Class.orDefault(),
		SessionID: cfg.SessionID,
	}

	eligible, dropped := d.partitionEligible(cfg)
	if len(eligible) == 0 {
		res.Gate = GateAdvisory
		res.Note = advisoryNote(cfg.Worker, dropped)
		return res
	}

	free, paid := splitTier(eligible)
	var results []ReviewerResult
	results = append(results, d.runTier(cfg, free)...)

	if d.escalate(cfg.Class, results, len(free)) {
		results = append(results, d.runTier(cfg, paid)...)
	}

	res.Reviewers = results
	res.Gate, res.Passes, res.Threshold = Evaluate(res.Class, results)
	// A panel with zero binding results (Evaluate returns advisory) mirrors the note.
	if res.Gate == GateAdvisory {
		res.Note = advisoryNote(cfg.Worker, dropped)
	}
	return res
}

// partitionEligible drops the worker's own family and any unavailable reviewer,
// returning the runnable set and a human list of what was dropped and why.
func (d Deps) partitionEligible(cfg Config) (eligible []Reviewer, dropped []string) {
	for _, rv := range cfg.Candidates {
		if strings.EqualFold(rv.Family, cfg.Worker) {
			dropped = append(dropped, rv.Family+" (worker's own family - never reviews its own diff)")
			continue
		}
		if ok, reason := d.avail(rv); !ok {
			dropped = append(dropped, rv.Family+" (unavailable: "+reason+")")
			continue
		}
		eligible = append(eligible, rv)
	}
	return eligible, dropped
}

// runTier runs each reviewer in the tier and parses its verdict fail-closed.
func (d Deps) runTier(cfg Config, tier []Reviewer) []ReviewerResult {
	prompt := RefutePrompt(cfg.Prompt)
	out := make([]ReviewerResult, 0, len(tier))
	for _, rv := range tier {
		stdout, err := d.run(rv, prompt)
		if err != nil {
			out = append(out, ReviewerResult{
				Family: rv.Family, Model: rv.Model, Verdict: Block,
				Error:  err.Error(),
				Reason: "reviewer failed to run; blocking fail-closed",
			})
			continue
		}
		out = append(out, ParseVerdict(rv.Family, rv.Model, stdout))
	}
	return out
}

// escalate decides whether to spend the paid tier: on a high-risk class, when no free
// reviewer ran, or when the free tier did not already pass (tiebreaker on a block).
func (d Deps) escalate(class Class, freeResults []ReviewerResult, freeCount int) bool {
	if class.orDefault() == ClassRefactor {
		return true
	}
	if freeCount == 0 {
		return true
	}
	gate, _, _ := Evaluate(class, freeResults)
	return gate != GatePass
}

// splitTier separates a reviewer set into the always-run free tier and the
// escalation-only paid tier, preserving order within each.
func splitTier(rs []Reviewer) (free, paid []Reviewer) {
	for _, rv := range rs {
		if rv.Paid {
			paid = append(paid, rv)
		} else {
			free = append(free, rv)
		}
	}
	return free, paid
}

// advisoryNote is the loud, human-facing line the single-family fallback emits so
// the operator knows the trust floor was NOT raised on this diff.
func advisoryNote(worker string, dropped []string) string {
	return fmt.Sprintf(
		"ADVISORY-ONLY REVIEW: no heterogeneous reviewer family was available besides the worker (%s), "+
			"so the adversarial panel could not run and did NOT gate this diff. Dropped: %s. "+
			"A human should review this change with the extra scrutiny an unrun panel would have applied.",
		worker, strings.Join(dropped, "; "))
}

func (d Deps) now() int64 {
	if d.Now != nil {
		return d.Now()
	}
	return 0
}

func (d Deps) run(rv Reviewer, prompt string) (string, error) {
	if d.Run == nil {
		return "", fmt.Errorf("no reviewer runner wired")
	}
	return d.Run(rv, prompt)
}

func (d Deps) avail(rv Reviewer) (bool, string) {
	if d.Avail == nil {
		return true, ""
	}
	return d.Avail(rv)
}
