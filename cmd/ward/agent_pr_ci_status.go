package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// prCIStatus is the agent-shaped PR/CI snapshot used by status, wait, and log
// follow-up.
type prCIStatus struct {
	Repo string `json:"repo"`
	PR   struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		State     string `json:"state"`
		Draft     bool   `json:"draft"`
		Mergeable bool   `json:"mergeable"`
	} `json:"pr"`
	Base struct {
		Ref               string   `json:"ref"`
		RequiredContexts  []string `json:"required_contexts,omitempty"`
		RequiredAvailable bool     `json:"required_available"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Status struct {
		Combined            string    `json:"combined"`
		Required            string    `json:"required"`
		RequiredAvailable   bool      `json:"required_available"`
		LatestRunConclusion string    `json:"latest_run_conclusion"`
		Terminal            bool      `json:"terminal"`
		ObservedAt          time.Time `json:"observed_at"`
		PinnedHead          string    `json:"pinned_head,omitempty"`
		HeadMismatch        bool      `json:"head_mismatch,omitempty"`
	} `json:"status"`
	Contexts        []prCIStatusContext `json:"contexts"`
	FailingContexts []string            `json:"failing_contexts,omitempty"`
	PendingContexts []string            `json:"pending_contexts,omitempty"`
	LatestRuns      []prCIStatusRun     `json:"latest_runs,omitempty"`
	LogHooks        []prCILogHook       `json:"log_hooks,omitempty"`
	Repair          *prCIRepair         `json:"repair,omitempty"`
	NextAction      string              `json:"next_action"`
}

type prCIStatusContext struct {
	Name        string       `json:"name"`
	State       string       `json:"state"`
	Required    bool         `json:"required"`
	Description string       `json:"description,omitempty"`
	TargetURL   string       `json:"target_url,omitempty"`
	RunID       int64        `json:"run_id,omitempty"`
	JobName     string       `json:"job_name,omitempty"`
	Attempt     int          `json:"attempt,omitempty"`
	Available   bool         `json:"available"`
	LogHook     *prCILogHook `json:"log_hook,omitempty"`
}

type prCIStatusRun struct {
	ID         int64  `json:"id"`
	Index      int64  `json:"index_in_repo"`
	WorkflowID string `json:"workflow_id"`
	Status     string `json:"status"`
	Event      string `json:"event"`
	CommitSHA  string `json:"commit_sha"`
	URL        string `json:"url"`
	Title      string `json:"title"`
}

type prCILogHook struct {
	Capability string `json:"capability"`
	Available  bool   `json:"available"`
	Repo       string `json:"repo"`
	RunID      int64  `json:"run_id,omitempty"`
	Context    string `json:"context,omitempty"`
	JobName    string `json:"job_name,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	URL        string `json:"url,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type prCIRepair struct {
	Bucket string `json:"bucket"`
	Note   string `json:"note"`
}

var prCILogTargetRE = regexp.MustCompile(`(?i)/actions/runs/(\d+)/jobs/(\d+)(?:/attempt/(\d+))?`)

func buildPRCIStatus(ctx context.Context, cl *forgejoClient, owner, repo string, index int, pinnedHead string) (*prCIStatus, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return nil, err
	}
	head := pr.HeadSHA()
	if head == "" {
		return nil, fmt.Errorf("pr status: %s/%s#%d did not expose a head SHA", owner, repo, index)
	}
	combined, err := cl.GetCommitCombinedStatus(ctx, owner, repo, head)
	if err != nil {
		return nil, err
	}
	branch, berr := cl.GetBranch(ctx, owner, repo, strings.TrimSpace(pr.Base.Ref))
	statusChecks := []string{}
	requiredAvailable := false
	if berr == nil && branch != nil {
		statusChecks = normalizeDirectorRequiredContexts(branch.StatusCheckContexts)
		requiredAvailable = len(statusChecks) > 0
	}
	runs, _ := cl.ListActionRuns(ctx, owner, repo, 20)
	latestRuns := prCIStatusRunsForHead(head, runs)
	latestRunConclusion := "unknown"
	if len(latestRuns) > 0 {
		latestRunConclusion = statusValue(latestRuns[0].Status)
	}
	st := &prCIStatus{Repo: owner + "/" + repo}
	st.PR.Number = pr.Number
	st.PR.Title = strings.TrimSpace(pr.Title)
	st.PR.URL = strings.TrimSpace(pr.HTMLURL)
	st.PR.State = statusValue(pr.State)
	st.PR.Draft = pr.Draft
	st.PR.Mergeable = pr.Mergeable
	st.Base.Ref = strings.TrimSpace(pr.Base.Ref)
	st.Base.RequiredContexts = append(st.Base.RequiredContexts, statusChecks...)
	st.Base.RequiredAvailable = requiredAvailable
	st.Head.Ref = strings.TrimSpace(pr.Head.Ref)
	st.Head.SHA = head
	st.Status.Combined = statusValue(combined.State)
	st.Status.RequiredAvailable = requiredAvailable
	st.Status.LatestRunConclusion = latestRunConclusion
	st.Status.ObservedAt = time.Now().UTC()
	st.Status.PinnedHead = strings.TrimSpace(pinnedHead)
	st.Status.HeadMismatch = st.Status.PinnedHead != "" && !strings.EqualFold(st.Status.PinnedHead, head)
	st.Contexts = prCIStatusContexts(combined.Statuses, statusChecks, latestRuns, owner, repo)
	st.FailingContexts, st.PendingContexts = prCIStatusBuckets(st.Contexts)
	st.LatestRuns = latestRuns
	st.LogHooks = prCIStatusLogHooks(st)
	st.Status.Required, st.Status.Terminal = prCIStatusRequiredAndTerminal(st)
	st.Repair = prCIStatusRepair(ctx, cl, owner, repo, pr, head, st.Status.Combined)
	st.NextAction = prCIStatusNextAction(st)
	return st, nil
}

func prCIStatusContexts(statuses []forgejoCommitStatus, required []string, runs []prCIStatusRun, owner, repo string) []prCIStatusContext {
	requiredSet := map[string]bool{}
	for _, ctx := range required {
		requiredSet[normalizeCIName(ctx)] = true
	}
	seen := map[string]bool{}
	out := make([]prCIStatusContext, 0, len(statuses))
	for _, st := range statuses {
		name := strings.TrimSpace(st.Context)
		norm := normalizeCIName(name)
		seen[norm] = true
		ctx := prCIStatusContext{
			Name:        name,
			State:       statusValue(st.EffectiveState()),
			Required:    requiredSet[norm],
			Description: strings.TrimSpace(st.Description),
			TargetURL:   strings.TrimSpace(st.TargetURL),
		}
		if run, ok := prCIStatusMatchRun(norm, runs); ok {
			ctx.RunID = run.ID
			ctx.JobName = prCIWorkflowJobName(run.WorkflowID)
			if hook, hok := prCILogHookFromStatus(owner, repo, ctx, run, ctx.TargetURL); hok {
				ctx.Available = hook.Available
				ctx.LogHook = &hook
			}
		} else if hook, hok := prCILogHookFromStatus(owner, repo, ctx, prCIStatusRun{}, ctx.TargetURL); hok {
			ctx.Available = hook.Available
			ctx.LogHook = &hook
		}
		out = append(out, ctx)
	}
	for _, requiredName := range required {
		norm := normalizeCIName(requiredName)
		if norm == "" || seen[norm] {
			continue
		}
		out = append(out, prCIStatusContext{
			Name:      strings.TrimSpace(requiredName),
			State:     string(prCIStatePending),
			Required:  true,
			Available: false,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required && !out[j].Required
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func prCIStatusBuckets(contexts []prCIStatusContext) (failing, pending []string) {
	for _, ctx := range contexts {
		switch prCIStatusClassify(ctx.State) {
		case prCIStateSuccess:
		case prCIStateFailure, prCIStateError, prCIStateCancelled, prCIStateSkipped:
			failing = append(failing, ctx.Name)
		case prCIStatePending, prCIStateUnknown:
			pending = append(pending, ctx.Name)
		}
	}
	sort.Strings(failing)
	sort.Strings(pending)
	return failing, pending
}

type prCIStatusClass string

const (
	prCIStateSuccess   prCIStatusClass = "success"
	prCIStatePending   prCIStatusClass = "pending"
	prCIStateFailure   prCIStatusClass = "failure"
	prCIStateError     prCIStatusClass = "error"
	prCIStateCancelled prCIStatusClass = "cancelled"
	prCIStateSkipped   prCIStatusClass = "skipped"
	prCIStateUnknown   prCIStatusClass = "unknown"
)

func prCIStatusClassify(state string) prCIStatusClass {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success":
		return prCIStateSuccess
	case "pending", "running", "queued", "waiting":
		return prCIStatePending
	case "failure":
		return prCIStateFailure
	case "error":
		return prCIStateError
	case "cancelled", "canceled":
		return prCIStateCancelled
	case "skipped":
		return prCIStateSkipped
	case "":
		return prCIStatePending
	default:
		return prCIStateUnknown
	}
}

func prCIStatusRequiredAndTerminal(st *prCIStatus) (string, bool) {
	if st == nil {
		return "unknown", false
	}
	if len(st.Base.RequiredContexts) == 0 {
		req := statusValue(st.Status.Combined)
		return req, prCIStatusTerminal(req)
	}
	byName := map[string]prCIStatusClass{}
	for _, ctx := range st.Contexts {
		byName[normalizeCIName(ctx.Name)] = prCIStatusClassify(ctx.State)
	}
	var sawPending bool
	for _, required := range st.Base.RequiredContexts {
		class := byName[normalizeCIName(required)]
		switch class {
		case prCIStateSuccess:
			continue
		case prCIStatePending, prCIStateUnknown:
			sawPending = true
		case prCIStateSkipped, prCIStateFailure, prCIStateError, prCIStateCancelled:
			return string(prCIStateFailure), true
		}
	}
	if sawPending {
		return string(prCIStatePending), false
	}
	return string(prCIStateSuccess), true
}

func prCIStatusTerminal(state string) bool {
	switch prCIStatusClassify(state) {
	case prCIStateSuccess, prCIStateFailure, prCIStateError, prCIStateCancelled, prCIStateSkipped:
		return true
	case prCIStatePending, prCIStateUnknown:
		return false
	default:
		return false
	}
}

func prCIStatusLogHooks(st *prCIStatus) []prCILogHook {
	if st == nil {
		return nil
	}
	hooks := make([]prCILogHook, 0, len(st.Contexts))
	for _, ctx := range st.Contexts {
		hook := prCILogHook{
			Capability: "ci.log.read",
			Repo:       st.Repo,
			Context:    ctx.Name,
			Available:  false,
			URL:        ctx.TargetURL,
		}
		if ctx.LogHook != nil {
			hook = *ctx.LogHook
		} else if ctx.RunID > 0 {
			hook.RunID = ctx.RunID
			hook.JobName = ctx.JobName
		}
		if hook.Capability == "" {
			hook.Capability = "ci.log.read"
		}
		hooks = append(hooks, hook)
	}
	return hooks
}

func prCIStatusNextAction(st *prCIStatus) string {
	if st == nil {
		return "blocked"
	}
	if st.Status.HeadMismatch {
		return "rebase_or_refresh"
	}
	switch statusValue(st.Status.Required) {
	case "success":
		if strings.EqualFold(st.PR.State, "open") {
			return "merge"
		}
		return "none"
	case "failure", "error", "cancelled", "skipped":
		if prCIStatusHasAvailableHook(st) {
			return "fetch_logs"
		}
		return "repair_pr"
	case "pending", "unknown":
		return "wait"
	default:
		return "wait"
	}
}

func prCIStatusHasAvailableHook(st *prCIStatus) bool {
	for _, hook := range st.LogHooks {
		if hook.Available {
			return true
		}
	}
	return false
}

func prCIStatusRepair(ctx context.Context, cl prRepairForgejoClassifier, owner, repo string, pr *forgejoPullRequest, head string, combined string) *prCIRepair {
	if cl == nil || pr == nil {
		return nil
	}
	assessment, err := classifyForgejoPRRepair(ctx, cl, owner, repo, agentPullRequestContext{
		State:        strings.TrimSpace(pr.State),
		Title:        strings.TrimSpace(pr.Title),
		Body:         strings.TrimSpace(pr.Body),
		URL:          strings.TrimSpace(pr.HTMLURL),
		HeadSHA:      strings.TrimSpace(head),
		HeadRef:      strings.TrimSpace(pr.Head.Ref),
		BaseRef:      strings.TrimSpace(pr.Base.Ref),
		Mergeability: boolString(pr.Mergeable),
	})
	if err != nil || assessment.Bucket == "" || statusValue(combined) == "success" {
		return nil
	}
	return &prCIRepair{Bucket: string(assessment.Bucket), Note: assessment.Note}
}

func prCIStatusMarshalJSON(st *prCIStatus) (string, error) {
	if st == nil {
		return "", fmt.Errorf("pr status: empty snapshot")
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func prCIStatusText(st *prCIStatus) string {
	if st == nil {
		return ""
	}
	var b strings.Builder
	prCIStatusAppendLine(&b, "%s head %s combined=%s required=%s latest=%s next=%s\n",
		st.Repo, shortHead(st.Head.SHA), statusValue(st.Status.Combined), statusValue(st.Status.Required), statusValue(st.Status.LatestRunConclusion), st.NextAction)
	prCIStatusAppendLine(&b, "  pr=%d state=%s draft=%t mergeable=%t base=%s required=%s\n",
		st.PR.Number, statusValue(st.PR.State), st.PR.Draft, st.PR.Mergeable, emptyDefault(st.Base.Ref, "(unknown)"), strings.Join(st.Base.RequiredContexts, ","))
	prCIStatusAppendLine(&b, "  combined status: %s\n", statusValue(st.Status.Combined))
	prCIStatusAppendContextLines(&b, st.Contexts)
	if len(st.Base.RequiredContexts) > 0 {
		prCIStatusAppendLine(&b, "  required on %s: %s\n", emptyDefault(st.Base.Ref, "(unknown)"), strings.Join(st.Base.RequiredContexts, ", "))
	}
	if len(st.FailingContexts) > 0 {
		prCIStatusAppendLine(&b, "  failing=%s\n", strings.Join(st.FailingContexts, ","))
	}
	if len(st.PendingContexts) > 0 {
		prCIStatusAppendLine(&b, "  pending=%s\n", strings.Join(st.PendingContexts, ","))
	}
	prCIStatusAppendLatestRunLine(&b, st.LatestRuns)
	prCIStatusAppendHookLines(&b, st.LogHooks)
	if st.Status.HeadMismatch {
		prCIStatusAppendLine(&b, "  head_mismatch: pinned=%s observed=%s\n", shortHead(st.Status.PinnedHead), shortHead(st.Head.SHA))
	}
	prCIStatusAppendRepairLine(&b, st.Repair)
	return b.String()
}

func shortHead(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 12 {
		return sha[:12]
	}
	if sha == "" {
		return "(unknown)"
	}
	return sha
}

func normalizeCIName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = filepath.Base(s)
	s = strings.TrimSuffix(s, ".yml")
	s = strings.TrimSuffix(s, ".yaml")
	return s
}

func prCIStatusRunsForHead(head string, runs []forgejoActionRun) []prCIStatusRun {
	head = strings.TrimSpace(head)
	out := make([]prCIStatusRun, 0, len(runs))
	for _, run := range runs {
		if strings.TrimSpace(run.CommitSHA) != head {
			continue
		}
		out = append(out, prCIStatusRun{
			ID:         run.ID,
			Index:      run.IndexInRepo,
			WorkflowID: strings.TrimSpace(run.WorkflowID),
			Status:     statusValue(run.Status),
			Event:      strings.TrimSpace(run.Event),
			CommitSHA:  strings.TrimSpace(run.CommitSHA),
			URL:        strings.TrimSpace(run.HTMLURL),
			Title:      strings.TrimSpace(run.Title),
		})
	}
	return out
}

func prCIStatusMatchRun(contextName string, runs []prCIStatusRun) (prCIStatusRun, bool) {
	contextName = normalizeCIName(contextName)
	for _, run := range runs {
		if prCIWorkflowMatchesContext(run.WorkflowID, contextName) {
			return run, true
		}
	}
	return prCIStatusRun{}, false
}

func prCIWorkflowMatchesContext(workflowID, contextName string) bool {
	if contextName == "" || workflowID == "" {
		return false
	}
	workflowName := normalizeCIName(workflowID)
	if workflowName == contextName {
		return true
	}
	workflowName = strings.TrimSuffix(workflowName, ".yml")
	return workflowName == contextName
}

func prCIWorkflowJobName(workflowID string) string {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return ""
	}
	return filepath.Base(workflowID)
}

func prCILogHookFromStatus(owner, repo string, ctx prCIStatusContext, run prCIStatusRun, targetURL string) (prCILogHook, bool) {
	hook := prCILogHook{
		Capability: "ci.log.read",
		Repo:       owner + "/" + repo,
		Context:    ctx.Name,
		URL:        strings.TrimSpace(targetURL),
	}
	if hook.URL == "" && strings.TrimSpace(run.URL) != "" {
		hook.URL = strings.TrimSpace(run.URL)
	}
	if run.ID > 0 {
		hook.RunID = run.ID
	}
	if run.WorkflowID != "" {
		hook.JobName = prCIWorkflowJobName(run.WorkflowID)
	}
	if parsed, ok := prCILogHookParse(targetURL); ok {
		hook.RunID = parsed.RunID
		hook.JobName = parsed.JobName
		hook.Attempt = parsed.Attempt
		hook.Available = true
		hook.URL = parsed.URL
		return hook, true
	}
	if parsed, ok := prCILogHookParse(run.URL); ok {
		hook.RunID = parsed.RunID
		hook.JobName = parsed.JobName
		hook.Attempt = parsed.Attempt
		hook.Available = true
		hook.URL = parsed.URL
		return hook, true
	}
	if run.ID > 0 {
		hook.Available = false
		hook.Reason = "target URL did not expose a run/job/attempt path"
		return hook, true
	}
	if ctx.TargetURL != "" {
		hook.Available = false
		hook.Reason = "no matching run id was found for this context"
		return hook, true
	}
	return hook, false
}

func prCILogHookParse(raw string) (prCILogHook, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return prCILogHook{}, false
	}
	m := prCILogTargetRE.FindStringSubmatch(raw)
	if m == nil {
		return prCILogHook{}, false
	}
	runID, _ := strconv.ParseInt(m[1], 10, 64)
	jobName := m[2]
	attempt := 1
	if m[3] != "" {
		if n, err := strconv.Atoi(m[3]); err == nil && n > 0 {
			attempt = n
		}
	}
	return prCILogHook{
		Capability: "ci.log.read",
		Available:  true,
		RunID:      runID,
		JobName:    jobName,
		Attempt:    attempt,
		URL:        raw,
	}, true
}

func prCIStatusSelectLogHook(st *prCIStatus, contextName string) (prCILogHook, error) {
	if st == nil {
		return prCILogHook{}, fmt.Errorf("pr logs: no status snapshot available")
	}
	if contextName = normalizeCIName(contextName); contextName != "" {
		if hook, ok := prCIStatusLogHookForContext(st.LogHooks, contextName); ok {
			return hook, prCIHookError(hook)
		}
		return prCILogHook{}, fmt.Errorf("pr logs: no log hook found for context %q", contextName)
	}
	if hook, ok := prCIStatusRequiredLogHook(st.Contexts); ok {
		return hook, prCIHookError(hook)
	}
	if hook, ok := prCIStatusAnyLogHook(st.Contexts); ok {
		return hook, prCIHookError(hook)
	}
	if hook, ok := prCIStatusAvailableHook(st.LogHooks); ok {
		return hook, nil
	}
	if len(st.LogHooks) > 0 {
		return st.LogHooks[0], prCIHookError(st.LogHooks[0])
	}
	return prCILogHook{}, fmt.Errorf("pr logs: no log hooks available")
}

func prCIStatusLogHookForContext(hooks []prCILogHook, contextName string) (prCILogHook, bool) {
	for _, hook := range hooks {
		if normalizeCIName(hook.Context) == contextName {
			return hook, true
		}
	}
	return prCILogHook{}, false
}

func prCIStatusRequiredLogHook(contexts []prCIStatusContext) (prCILogHook, bool) {
	for _, ctx := range contexts {
		if !ctx.Required || prCIStatusClassify(ctx.State) == prCIStateSuccess || ctx.LogHook == nil {
			continue
		}
		return *ctx.LogHook, true
	}
	return prCILogHook{}, false
}

func prCIStatusAnyLogHook(contexts []prCIStatusContext) (prCILogHook, bool) {
	for _, ctx := range contexts {
		if prCIStatusClassify(ctx.State) == prCIStateSuccess || ctx.LogHook == nil {
			continue
		}
		return *ctx.LogHook, true
	}
	return prCILogHook{}, false
}

func prCIStatusAvailableHook(hooks []prCILogHook) (prCILogHook, bool) {
	for _, hook := range hooks {
		if hook.Available {
			return hook, true
		}
	}
	return prCILogHook{}, false
}

func prCIHookError(hook prCILogHook) error {
	if hook.Available {
		return nil
	}
	if strings.TrimSpace(hook.Reason) != "" {
		return fmt.Errorf("%s", strings.TrimSpace(hook.Reason))
	}
	return fmt.Errorf("log hook unavailable")
}

func prCIStatusReadOnlyWaitLine(st *prCIStatus) string {
	if st == nil {
		return ""
	}
	var pending string
	if len(st.PendingContexts) > 0 {
		pending = strings.Join(st.PendingContexts, ",")
	}
	var runs string
	if len(st.LatestRuns) > 0 {
		ids := make([]string, 0, len(st.LatestRuns))
		for _, run := range st.LatestRuns {
			ids = append(ids, strconv.FormatInt(run.ID, 10))
		}
		runs = strings.Join(ids, ",")
	}
	return fmt.Sprintf("%s head %s required=%s pending=%s latest_runs=%s next=%s",
		st.Repo, shortHead(st.Head.SHA), statusValue(st.Status.Required), emptyDefault(pending, "-"), emptyDefault(runs, "-"), st.NextAction)
}

func prCIStatusAppendLine(b *strings.Builder, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}

func prCIStatusAppendContextLines(b *strings.Builder, contexts []prCIStatusContext) {
	for _, ctx := range contexts {
		prCIStatusAppendLine(b, "  %s = %s\n", ctx.Name, statusValue(ctx.State))
	}
}

func prCIStatusAppendHookLines(b *strings.Builder, hooks []prCILogHook) {
	for _, hook := range hooks {
		if hook.Available {
			prCIStatusAppendLine(b, "  log: %s context=%s run=%d job=%s attempt=%d\n", hook.Capability, hook.Context, hook.RunID, emptyDefault(hook.JobName, "(unknown)"), hook.Attempt)
			continue
		}
		prCIStatusAppendLine(b, "  log: %s context=%s unavailable (%s)\n", hook.Capability, hook.Context, emptyDefault(hook.Reason, "not enough data"))
	}
}

func prCIStatusAppendLatestRunLine(b *strings.Builder, runs []prCIStatusRun) {
	if len(runs) == 0 {
		return
	}
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, strconv.FormatInt(run.ID, 10))
	}
	prCIStatusAppendLine(b, "  latest_runs=%s\n", strings.Join(ids, ","))
}

func prCIStatusAppendRepairLine(b *strings.Builder, repair *prCIRepair) {
	if repair == nil {
		return
	}
	prCIStatusAppendLine(b, "  repair: %s - %s\n", repair.Bucket, repair.Note)
}

func statusValue(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "unknown"
	}
	return s
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func prCIStatusJSONOrText(st *prCIStatus, jsonOut bool) (string, error) {
	if jsonOut {
		return prCIStatusMarshalJSON(st)
	}
	return prCIStatusText(st), nil
}

func prCIStatusFetch(ctx context.Context, cl *forgejoClient, owner, repo string, index int, pinnedHead string, jsonOut bool) (string, error) {
	st, err := buildPRCIStatus(ctx, cl, owner, repo, index, pinnedHead)
	if err != nil {
		return "", err
	}
	body, err := prCIStatusJSONOrText(st, jsonOut)
	if err != nil {
		return "", err
	}
	return body, nil
}
