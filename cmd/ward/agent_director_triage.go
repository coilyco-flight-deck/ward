package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// agent_director_triage.go is director's startup triage pass (ward#397): before the init
// gate it labels each open issue's tier + mode. See docs/director-startup-triage.md.

// triageBodyLimit caps how much of each issue body rides the batched judgment prompt, so
// a 50-issue startup pass stays a single affordable one-shot.
const triageBodyLimit = 800

// p0ContentNet is the recall stage of P0 assignment: the content-rule regexes ported from
// tooling-issue-prioritization's p0-content-rules.yaml (over-matching on purpose).
var p0ContentNet = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(securestring.*(commit|leak|expos)|leak(s|ed|ing)?\b.{0,30}(secret|credential|token|key|oauth)|(secret|credential|token|api[ -]?key|oauth).{0,30}(leak|exposed|plaintext|into commit|in commit)|rotate.{0,20}(leaked|exposed|compromis)|literal username.*token|token.*literal username)`),
	regexp.MustCompile(`(?i)(arbitrary code execution|remote code execution|\bACE\b|chmod \+x.*run|gate bypass|bypass(es|ed)?\b.{0,25}(gate|lockdown|guard)|lockdown.{0,20}bypass|auth(entication|z)?\s*bypass|sandbox escape|privilege escalation|laundering gap)`),
	regexp.MustCompile(`(?i)(data[ -]?loss|drops? (local )?commits?|lost commit|silently.{0,20}(lost|dropped|swap)|crosses? (commit )?messages|stash.*swap.*staged|overwrit.{0,15}(data|commits?))`),
	regexp.MustCompile(`(?i)(crashloop|crash[ -]?loop|crashlooping|bot down|crashes? post-reboot|runners offline|all .{0,20}pipelines? stalled|every (deploy|release) fails?|deploys? fail .{0,20}(name=null|every)|deploy path broken end-to-end|100% failure|fail(s|ing) every \dmin)`),
	regexp.MustCompile(`(?i)(blocks all|blocking all|blocks every|blocks the .* (deploy|pipeline|release|build)|blocks other committed)`),
}

// p0ContentCandidate reports whether an issue's title+body trips the P0 content-net - the
// recall stage that nominates a P0 candidate for the judgment confirm. Pure.
func p0ContentCandidate(title, body string) bool {
	hay := title + "\n" + body
	for _, re := range p0ContentNet {
		if re.MatchString(hay) {
			return true
		}
	}
	return false
}

// triageCandidate is one open issue the startup pass will consider labelling; NeedTier /
// NeedMode gate which axes are written, so an already-set human label is left untouched.
type triageCandidate struct {
	Num         int
	Title       string
	Body        string
	NeedTier    bool
	NeedMode    bool
	P0Candidate bool
}

// triageVerdict is the per-issue judgment parsed from the batched one-shot; a missing
// field stays at its fail-closed zero (no confidence -> consult, no score -> the floor).
type triageVerdict struct {
	P0Confirmed bool
	Score       int
	Mode        string
	Confident   bool
}

// collectTriageCandidates keeps issues missing a tier or mode label (a fully-labeled one
// is already triaged), tags each with the P0 content verdict, and orders by number. Pure.
func collectTriageCandidates(issues []backlogIssue) []triageCandidate {
	var out []triageCandidate
	for _, it := range issues {
		needTier := backlogTierOf(it.Labels) == ""
		needMode := backlogModeOf(it.Labels) == ""
		if !needTier && !needMode {
			continue
		}
		out = append(out, triageCandidate{
			Num:         it.Number,
			Title:       it.Title,
			Body:        it.Body,
			NeedTier:    needTier,
			NeedMode:    needMode,
			P0Candidate: p0ContentCandidate(it.Title, it.Body),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// triageMode resolves the mode label fail-closed: only a confident headless/interactive
// promotes out of human-gated, everything else lands consult. Pure.
func triageMode(v triageVerdict) string {
	if v.Confident && (v.Mode == "headless" || v.Mode == "interactive") {
		return v.Mode
	}
	return "consult"
}

// scoredTriage pairs an issue with its urgency score for the percentile cut.
type scoredTriage struct {
	Num   int
	Score int
}

// assignTierBands cuts the non-P0 remainder into P1-P4 by percentile (top 20/20/20, then
// 40); a pool with no urgency signal all lands P3, the default tier. Pure.
func assignTierBands(items []scoredTriage) map[int]string {
	out := map[int]string{}
	if len(items) == 0 {
		return out
	}
	sorted := make([]scoredTriage, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Score != sorted[j].Score {
			return sorted[i].Score > sorted[j].Score
		}
		return sorted[i].Num < sorted[j].Num
	})
	if sorted[0].Score == sorted[len(sorted)-1].Score {
		for _, it := range sorted {
			out[it.Num] = "P3"
		}
		return out
	}
	n := float64(len(sorted))
	for i, it := range sorted {
		switch frac := float64(i) / n; {
		case frac < 0.20:
			out[it.Num] = "P1"
		case frac < 0.40:
			out[it.Num] = "P2"
		case frac < 0.60:
			out[it.Num] = "P3"
		default:
			out[it.Num] = "P4"
		}
	}
	return out
}

// assignTriageTiers carves confirmed P0s out first, then percentile-cuts the scored
// remainder; an issue that already has a tier is excluded and keeps it. Pure.
func assignTriageTiers(cands []triageCandidate, verdicts map[int]triageVerdict) map[int]string {
	out := map[int]string{}
	var scored []scoredTriage
	for _, c := range cands {
		if !c.NeedTier {
			continue
		}
		v := verdicts[c.Num]
		if c.P0Candidate && v.P0Confirmed {
			out[c.Num] = "P0"
			continue
		}
		scored = append(scored, scoredTriage{Num: c.Num, Score: v.Score})
	}
	for num, tier := range assignTierBands(scored) {
		out[num] = tier
	}
	return out
}

// triagePrompt renders the batched judgment one-shot: the tier/mode rubric plus every
// candidate (P0-net hits flagged), asking for one machine-readable line per issue. Pure.
func triagePrompt(cands []triageCandidate) string {
	var b strings.Builder
	b.WriteString("You are the startup triage judgment for an autonomous backlog supervisor. For each open " +
		"issue below, assign an urgency SCORE and an automation MODE. Your labels feed a dispatch gate: only a " +
		"confident headless/interactive clears an issue for autonomous work, everything else stays human-gated. " +
		"When unsure, say so - the safe default is the gated one.\n\n")
	b.WriteString("URGENCY SCORE (0-3): 3 = important and the clear next thing (near-term committed value); " +
		"2 = real backlog you intend to act on; 1 = low but kept (the default when unsure); " +
		"0 = icebox / parked / speculative / won't-do-soon.\n\n")
	b.WriteString("AUTOMATION MODE - the highest autonomy the issue supports:\n" +
		"- headless: an agent can carry it from open issue to merged change with NO human - self-contained " +
		"code/docs/config, clear enough to act on now, no pending design call, no missing access, no destructive " +
		"or externally-visible production step.\n" +
		"- interactive: an agent does the work but must pause at a human checkpoint (a design choice, a " +
		"destructive/irreversible step, a human-only verification).\n" +
		"- consult: a human must decide/design/act first (ambiguous, a product call, missing access).\n\n")
	b.WriteString("Some issues are marked [P0-CANDIDATE] because their text tripped an incident content-net. For " +
		"those ONLY, also judge whether it is an ACTIVE incident / live exposure right now (P0=yes) or merely " +
		"discusses the topic (P0=no).\n\n")
	b.WriteString("ISSUES:\n")
	for _, c := range cands {
		flag := ""
		if c.P0Candidate {
			flag = " [P0-CANDIDATE]"
		}
		body := strings.TrimSpace(backlogTruncate(strings.ReplaceAll(c.Body, "\n", " "), triageBodyLimit))
		fmt.Fprintf(&b, "- #%d%s %q :: %s\n", c.Num, flag, backlogTruncate(c.Title, 120), body)
	}
	b.WriteString("\nFor EACH issue output exactly one line, no other prose:\n" +
		"  #<num> SCORE=<0-3> MODE=<headless|interactive|consult> CONF=<high|low>[ P0=<yes|no>]\n" +
		"Use CONF=high only when you are confident of the MODE; otherwise CONF=low (it fails closed to consult).\n")
	return b.String()
}

var (
	triageLineRE  = regexp.MustCompile(`#(\d+)`)
	triageScoreRE = regexp.MustCompile(`(?i)score\s*[:=]?\s*([0-3])`)
	triageModeRE  = regexp.MustCompile(`(?i)mode\s*[:=]?\s*(headless|interactive|consult)`)
	triageConfRE  = regexp.MustCompile(`(?i)conf(?:idence)?\s*[:=]?\s*(high|low)`)
	triageP0RE    = regexp.MustCompile(`(?i)p0\s*[:=]?\s*(yes|no|true|false)`)
)

// parseTriageVerdicts reads the one-shot's per-issue lines into verdicts keyed by number
// (last wins); a missing field stays zero, so a garbled read degrades to consult. Pure.
func parseTriageVerdicts(read string) map[int]triageVerdict {
	out := map[int]triageVerdict{}
	for _, raw := range strings.Split(read, "\n") {
		line := strings.TrimSpace(raw)
		m := triageLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		v := triageVerdict{}
		if s := triageScoreRE.FindStringSubmatch(line); s != nil {
			v.Score, _ = strconv.Atoi(s[1])
		}
		if md := triageModeRE.FindStringSubmatch(line); md != nil {
			v.Mode = strings.ToLower(md[1])
		}
		if cf := triageConfRE.FindStringSubmatch(line); cf != nil {
			v.Confident = strings.EqualFold(cf[1], "high")
		}
		if p := triageP0RE.FindStringSubmatch(line); p != nil {
			v.P0Confirmed = strings.EqualFold(p[1], "yes") || strings.EqualFold(p[1], "true")
		}
		out[num] = v
	}
	return out
}

// backlogTriage is the startup triage pass (ward#397): label each untriaged open issue's
// tier + mode across the scope. Best effort and fail-closed; no one-shot writes nothing.
func (r *Runner) backlogTriage(ctx context.Context, label string, repos []string, mode containerMode, limit int) {
	bin := mode.agentBinary()
	if _, ok := mode.hostPreflightArgv("probe"); !ok || !hostHasBinary(bin) {
		fmt.Fprintf(os.Stderr, "%s: note: %s self-assessment unavailable; skipping startup triage.\n", label, bin)
		return
	}
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot triage (%v); leaving labels unchanged.\n", label, err)
		return
	}
	cl = cl.withMode(mode)
	for _, repo := range repos {
		r.triageRepo(ctx, label, repo, cl, mode, limit)
	}
}

// triageRepo runs the pass over one repo: fetch, filter to the untriaged, judge in one
// host one-shot, then write the missing tier/mode labels. Best effort.
func (r *Runner) triageRepo(ctx context.Context, label, repo string, cl *forgejoClient, mode containerMode, limit int) {
	owner, name, _ := strings.Cut(repo, "/")
	issues, err := cl.listOpenIssues(ctx, owner, name, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot triage %s (%v); leaving labels unchanged.\n", label, repo, err)
		return
	}
	cands := collectTriageCandidates(issues)
	if len(cands) == 0 {
		fmt.Fprintf(os.Stderr, "%s: triage - %s already fully labeled (%d open); nothing to do.\n", label, repo, len(issues))
		return
	}
	fmt.Fprintf(os.Stderr, "%s: triage - judging %d untriaged issue(s) in %s with %s...\n", label, len(cands), repo, mode.agentBinary())
	verdicts, ok := r.triageJudge(ctx, label, mode, cands)
	if !ok {
		return
	}
	tiers := assignTriageTiers(cands, verdicts)
	r.triageWriteLabels(ctx, label, repo, cl, cands, verdicts, tiers)
}

// triageJudge runs the batched judgment one-shot and parses its verdicts; ok=false on an
// incomplete read, so the caller writes nothing (fail-closed).
func (r *Runner) triageJudge(ctx context.Context, label string, mode containerMode, cands []triageCandidate) (map[int]triageVerdict, bool) {
	argv, ok := mode.hostPreflightArgv(triagePrompt(cands))
	if !ok {
		return nil, false
	}
	tctx, cancel := context.WithTimeout(ctx, directorDecideTimeout)
	defer cancel()
	out, err := r.capturePreflight(tctx, argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: triage judgment read did not complete (%v); leaving labels unchanged.\n", label, err)
		return nil, false
	}
	return parseTriageVerdicts(strings.TrimSpace(string(out))), true
}

// triageWriteLabels adds each candidate's missing tier/mode label; a write failure (an
// org label not yet defined, say) is noted and skipped, never fatal.
func (r *Runner) triageWriteLabels(ctx context.Context, label, repo string, cl *forgejoClient, cands []triageCandidate, verdicts map[int]triageVerdict, tiers map[int]string) {
	owner, name, _ := strings.Cut(repo, "/")
	var promoted int
	for _, c := range cands {
		add := triageLabelsFor(c, verdicts[c.Num], tiers[c.Num])
		if len(add) == 0 {
			continue
		}
		if err := cl.addIssueLabels(ctx, owner, name, c.Num, add); err != nil {
			fmt.Fprintf(os.Stderr, "%s: note: cannot label %s#%d %v (%v); skipping.\n", label, repo, c.Num, add, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s#%d += %s\n", repo, c.Num, strings.Join(add, " "))
		for _, l := range add {
			if l == "headless" {
				promoted++
			}
		}
	}
	fmt.Fprintf(os.Stderr, "%s: triage - %s done (%d issue(s) promoted to headless).\n", label, repo, promoted)
}

// triageLabelsFor builds the labels to add: the computed tier (if still missing) and the
// fail-closed mode (if still missing), so an existing human label is never clobbered.
func triageLabelsFor(c triageCandidate, v triageVerdict, tier string) []string {
	var add []string
	if c.NeedTier && tier != "" {
		add = append(add, tier)
	}
	if c.NeedMode {
		add = append(add, triageMode(v))
	}
	return add
}
