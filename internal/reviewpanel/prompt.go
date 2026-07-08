package reviewpanel

import (
	"fmt"
	"strings"
)

// prompt.go builds the refute-by-default reviewer prompt (ward#134): assume the diff is
// wrong, hunt the reason, default to BLOCK. A pass with no refutation is not a pass.

// PromptInput is everything a reviewer is handed: the issue contract, the diff, and the
// CI output. The live worktree is implicit - the reviewer runs inside the container.
type PromptInput struct {
	Class      Class
	IssueRef   string // e.g. owner/repo#134
	IssueURL   string
	IssueTitle string
	IssueBody  string
	Skill      string
	Diff       string
	CIOutput   string
}

// maxSection bounds each pasted section so a giant diff or CI log cannot blow the
// reviewer's context; the reviewer still has the live tree to inspect in full.
const maxSection = 60000

// RefutePrompt renders the adversarial reviewer prompt: deterministic and
// self-contained, so any harness can run it and tests can assert its shape.
func RefutePrompt(in PromptInput) string {
	var b strings.Builder
	b.WriteString(
		"You are an ADVERSARIAL code reviewer on a multi-model panel. Your job is not to " +
			"approve - it is to REFUTE. Assume this diff is WRONG and find the reason it is wrong: a " +
			"correctness bug, a broken contract, an unhandled case, a security or data-loss risk, a " +
			"regression the tests do not cover, or a change that does not actually satisfy the issue. " +
			"You share the container with the worker, so inspect the LIVE TREE: run the exact tests, " +
			"grep the edited files, reproduce against the real filesystem state. Do not rely on the " +
			"pasted diff alone.\n\n")
	b.WriteString(
		"Default to BLOCK on any uncertainty. A pass with no attempted refutation is NOT a pass - if " +
			"you did not genuinely try to break it, block. Only return `pass` when you actively tried " +
			"to refute the diff and could not.\n\n")
	if skill := strings.TrimSpace(in.Skill); skill != "" {
		b.WriteString("----- REVIEW SKILL -----\n")
		b.WriteString(truncate(skill) + "\n")
		b.WriteString("----- END REVIEW SKILL -----\n\n")
	}
	b.WriteString("Risk class for this diff: " + string(in.Class.orDefault()) + ".\n\n")

	b.WriteString("----- ISSUE CONTRACT -----\n")
	if in.IssueRef != "" {
		b.WriteString("ref: " + in.IssueRef + "\n")
	}
	if in.IssueURL != "" {
		b.WriteString("url: " + in.IssueURL + " (read the live thread if you need context)\n")
	}
	if t := strings.TrimSpace(in.IssueTitle); t != "" {
		b.WriteString("title: " + t + "\n")
	}
	if body := strings.TrimSpace(in.IssueBody); body != "" {
		b.WriteString("\n" + truncate(body) + "\n")
	} else {
		b.WriteString("(issue body not inlined - fetch the url above if you need it)\n")
	}
	b.WriteString("----- END ISSUE CONTRACT -----\n\n")

	b.WriteString("----- DIFF UNDER REVIEW (git diff vs main) -----\n")
	b.WriteString(truncate(in.Diff) + "\n")
	b.WriteString("----- END DIFF -----\n\n")

	if ci := strings.TrimSpace(in.CIOutput); ci != "" {
		b.WriteString("----- CI / TEST OUTPUT (already green) -----\n")
		b.WriteString(truncate(ci) + "\n")
		b.WriteString("----- END CI OUTPUT -----\n\n")
	}

	b.WriteString(
		"Return your verdict as EXACTLY ONE fenced json block, nothing after it:\n\n" +
			"```json\n" +
			"{\"verdict\": \"pass\" or \"block\", \"reason\": \"<one or two sentences; for a block, the " +
			"specific refutation>\", \"confidence\": <0.0-1.0>}\n" +
			"```\n\n" +
			"If you cannot complete the review, emit a block verdict - a missing or malformed verdict is " +
			"treated as a block.")
	return b.String()
}

// orDefault collapses an empty class onto the default so the prompt always names
// a concrete tier.
func (c Class) orDefault() Class {
	if c == "" {
		return ClassDefault
	}
	return c
}

// truncate caps a pasted section at maxSection bytes, marking the cut so a reviewer
// falls back to the live tree rather than trusting a clipped paste.
func truncate(s string) string {
	if len(s) <= maxSection {
		return s
	}
	return s[:maxSection] + fmt.Sprintf("\n... [truncated %d bytes - inspect the live tree for the rest]", len(s)-maxSection)
}
