package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

func TestAgentRunningEngineerFromInspectIncludesReservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(wardConfigRefEnv, "")

	now := time.Date(2026, 7, 9, 22, 45, 0, 0, time.UTC)
	ref := agentIssueRef{Owner: "coilyco-gaming", Repo: "factory-game-v3", Number: 18}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Container: "engineer-codex-factory-game-v3-18",
		Branch:    "issue-18",
		Host:      "kais-macbook-pro-2.local",
		At:        now.Add(-17 * time.Minute),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}

	snap := agentDockerInspectContainer{Name: "/engineer-codex-factory-game-v3-18"}
	snap.Config.Labels = map[string]string{
		labelDriver: string(modeCodex),
		labelRepo:   "factory-game-v3",
		labelIssue:  "18",
	}
	snap.Config.Env = []string{
		"WARD_TARGET_OWNER=coilyco-gaming",
		"WARD_TARGET_NAME=factory-game-v3",
		"WARD_TARGET_REPO=coilyco-gaming/factory-game-v3",
		"WARD_TARGET_ISSUE=18",
		"WARD_BRANCH=issue-18",
		"WARD_MODE=codex",
	}
	snap.State.Status = "running"
	snap.State.StartedAt = now.Add(-16 * time.Minute).Format(time.RFC3339Nano)

	row := agentRunningEngineerFromInspect(now, snap)
	if row.Container != "engineer-codex-factory-game-v3-18" {
		t.Fatalf("container = %q", row.Container)
	}
	if row.Role != roleEngineer {
		t.Fatalf("role = %q", row.Role)
	}
	if row.Ref != "coilyco-gaming/factory-game-v3#18" {
		t.Fatalf("ref = %q", row.Ref)
	}
	if row.Repo != "coilyco-gaming/factory-game-v3" {
		t.Fatalf("repo = %q", row.Repo)
	}
	if row.Harness != string(modeCodex) {
		t.Fatalf("harness = %q", row.Harness)
	}
	if row.Host != "kais-macbook-pro-2.local" {
		t.Fatalf("host = %q", row.Host)
	}
	if row.Branch != "issue-18" {
		t.Fatalf("branch = %q", row.Branch)
	}
	if row.Age.Round(time.Minute) != 17*time.Minute {
		t.Fatalf("age = %s", row.Age)
	}
	if row.Status != "running" {
		t.Fatalf("status = %q", row.Status)
	}
	if row.ExecutionLimit != 90*time.Minute {
		t.Fatalf("execution limit = %s, want 90m", row.ExecutionLimit)
	}
	if row.Phase != agentLaunchPhaseRunning {
		t.Fatalf("phase = %q", row.Phase)
	}
	if row.ReservedAt.IsZero() || row.StartedAt.IsZero() {
		t.Fatalf("expected reservation and start timestamps, got reserved=%v started=%v", row.ReservedAt, row.StartedAt)
	}

	j := row.toJSON()
	if j.Ref != row.Ref || j.Repo != row.Repo || j.Harness != row.Harness {
		t.Fatalf("json projection drifted: %+v", j)
	}
	buf, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if !strings.Contains(string(buf), `"engineer-codex-factory-game-v3-18"`) {
		t.Fatalf("json output missing container: %s", buf)
	}

	payload := agentListJSONFromRows([]agentRunningEngineer{row}, bakedSmartDefaults().engineerContainerLimit-11)
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	if payload.LaunchIntents != 0 {
		t.Fatalf("launch_intents = %d, want 0", payload.LaunchIntents)
	}
	if payload.CleanupNeeded != 0 || payload.FailedBefore != 0 {
		t.Fatalf("unexpected excluded counts: cleanup=%d failed=%d", payload.CleanupNeeded, payload.FailedBefore)
	}
	if payload.Limit == nil || *payload.Limit != engineerContainerLimitDefault() {
		t.Fatalf("limit = %v, want %d", payload.Limit, engineerContainerLimitDefault())
	}
	if payload.Remaining == nil || *payload.Remaining != engineerContainerLimitDefault()-1 {
		t.Fatalf("remaining = %v, want %d", payload.Remaining, engineerContainerLimitDefault()-1)
	}
	if payload.AtCapacity == nil || *payload.AtCapacity {
		t.Fatalf("at_capacity = %v, want false", payload.AtCapacity)
	}
	if len(payload.Engineers) != 1 || payload.Engineers[0].ExecutionLimit != "1h30m0s" {
		t.Fatalf("engineer budget JSON = %+v", payload.Engineers)
	}
	if !strings.Contains(payload.Engineers[0].BudgetRemaining, "remaining of 90m limit") {
		t.Fatalf("engineer budget remaining = %q", payload.Engineers[0].BudgetRemaining)
	}
	jbuf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal payload json: %v", err)
	}
	for _, want := range []string{
		`"count": 1`,
		`"limit": 12`,
		`"remaining": 11`,
		`"at_capacity": false`,
		`"execution_limit": "1h30m0s"`,
		`"budget_remaining": "73m remaining of 90m limit"`,
		`"phase": "container running"`,
	} {
		if !strings.Contains(string(jbuf), want) {
			t.Fatalf("payload json missing %q:\n%s", want, jbuf)
		}
	}

	human := renderAgentListHuman([]agentRunningEngineer{row}, bakedSmartDefaults().engineerContainerLimit-11)
	for _, want := range []string{
		"ward agent: active engineer launches (1/12, 11 slots free)",
		"coilyco-gaming/factory-game-v3#18",
		"kais-macbook-pro-2.local",
		"issue-18",
		"running",
		"budget:    73m remaining of 90m limit",
		"phase:     container running",
		"container running",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output missing %q:\n%s", want, human)
		}
	}
}

func TestReservationCachePrunesArchivedDirectorRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1301}
	dir := filepath.Join(agentLogsDir(), "director-codex-ward-1301")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir archived director run: %v", err)
	}
	meta := runMeta{
		Container: "director-codex-ward-1301",
		Repo:      ref.repoSlug(),
		Issue:     strconv.Itoa(ref.Number),
		Outcome:   outcomePushedMain,
	}
	if err := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); err != nil {
		t.Fatalf("write archived director meta: %v", err)
	}

	r := &Runner{}
	if !r.reservationCacheShouldPruneArchivedRun(ref) {
		t.Fatal("director archive should supersede the reservation cache the same way an engineer archive does")
	}
}

func TestAgentListIncludesReservedLaunchPhase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1033}
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	srv := issueThreadAuthorityServer(t, []issueThreadAuthorityRow{{
		Number: 1033,
		Title:  "reserved launch",
		Body:   "body",
		Comments: []issueComment{
			reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-1033", "director-box", now.Add(-time.Second), "", nil), now.Add(-time.Second)),
		},
	}})
	forgejoBaseURL = srv.URL
	resPath, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(resPath, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1033",
		Branch:    "issue-1033",
		Host:      "director-box",
		At:        now.Add(-time.Second),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	dispatchDir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dispatchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dispatch logs: %v", err)
	}
	logPath := filepath.Join(dispatchDir, "20260710T101500Z-director-box-coilyco-flight-deck-ward-1033.log")
	if err := os.WriteFile(logPath, []byte(
		"ward dispatch broker: director-box requested `ward agent engineer coilyco-flight-deck/ward#1033 --harness codex`\n"+
			"ward dispatch broker: launch plan ready for coilyco-flight-deck/ward#1033 (container=engineer-codex-ward-1033 branch=issue-1033 readOnly=true tailnet=false/false)\n"+
			"ward dispatch broker: wrote launch env file for coilyco-flight-deck/ward#1033\n"), 0o644); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Ref != ref.String() {
		t.Fatalf("ref = %q, want %q", row.Ref, ref.String())
	}
	if row.Phase != agentLaunchPhaseStarting {
		t.Fatalf("phase = %q, want %q", row.Phase, agentLaunchPhaseStarting)
	}
	if row.Status != "starting" {
		t.Fatalf("status = %q, want starting", row.Status)
	}
	payload := agentListJSONFromRows(rows)
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1 active engineer launch", payload.Count)
	}
	if payload.LaunchIntents != 1 {
		t.Fatalf("launch_intents = %d, want 1", payload.LaunchIntents)
	}
	if payload.CleanupNeeded != 0 || payload.FailedBefore != 0 {
		t.Fatalf("unexpected excluded counts: cleanup=%d failed=%d", payload.CleanupNeeded, payload.FailedBefore)
	}
	if row.ReservedAt.IsZero() || !row.ReservedAt.Equal(now.Add(-time.Second)) {
		t.Fatalf("reserved_at = %v, want %v", row.ReservedAt, now.Add(-time.Second))
	}
	human := renderAgentListHuman(rows)
	for _, want := range []string{
		"ward agent: active engineer launches (1/12, 11 slots free) + 1 launch intent pending",
		ref.String(),
		"phase:     container starting",
		"status:    starting",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("reserved-launch human output missing %q:\n%s", want, human)
		}
	}
}

func TestAgentListMarksStalePrelaunchLaunchesCleanupNeeded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origTimeout := dispatchBrokerVisibilityTimeout
	dispatchBrokerVisibilityTimeout = 25 * time.Millisecond
	t.Cleanup(func() { dispatchBrokerVisibilityTimeout = origTimeout })

	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1034}
	resPath, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(resPath, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1034",
		Branch:    "issue-1034",
		Host:      "director-box",
		At:        now.Add(-time.Second),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	dispatchDir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dispatchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dispatch logs: %v", err)
	}
	logPath := filepath.Join(dispatchDir, "20260710T101500Z-director-box-coilyco-flight-deck-ward-1034.log")
	if err := os.WriteFile(logPath, []byte(
		"ward dispatch broker: director-box requested `ward agent engineer coilyco-flight-deck/ward#1034 --harness codex`\n"+
			"ward dispatch broker: launch plan ready for coilyco-flight-deck/ward#1034 (container=engineer-codex-ward-1034 branch=issue-1034 readOnly=true tailnet=false/false)\n"+
			"ward dispatch broker: wrote launch env file for coilyco-flight-deck/ward#1034\n"), 0o644); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want stale prelaunch launch to stay visible for cleanup", len(rows))
	}
	row := rows[0]
	if row.Status != agentLaunchStatusCleanup {
		t.Fatalf("status = %q, want %q", row.Status, agentLaunchStatusCleanup)
	}
	if row.Phase != agentLaunchPhaseStarting {
		t.Fatalf("phase = %q, want %q", row.Phase, agentLaunchPhaseStarting)
	}
	payload := agentListJSONFromRows(rows)
	if payload.Count != 0 || payload.LaunchIntents != 0 {
		t.Fatalf("payload counts = active=%d intents=%d, want 0/0", payload.Count, payload.LaunchIntents)
	}
	if payload.CleanupNeeded != 1 || payload.FailedBefore != 0 {
		t.Fatalf("payload excluded counts = cleanup=%d failed=%d, want 1/0", payload.CleanupNeeded, payload.FailedBefore)
	}
	human := renderAgentListHuman(rows)
	for _, want := range []string{
		"1 cleanup-needed record",
		"phase:     container starting",
		"status:    cleanup-needed",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("stale launch human output missing %q:\n%s", want, human)
		}
	}
}

func TestAgentListKeepsFailedBeforeStartRowsVisibleButExcluded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1035}
	resPath, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(resPath, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1035",
		Branch:    "issue-1035",
		Host:      "director-box",
		At:        now.Add(-time.Second),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	dispatchDir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dispatchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dispatch logs: %v", err)
	}
	logPath := filepath.Join(dispatchDir, "20260710T101500Z-director-box-coilyco-flight-deck-ward-1035.log")
	if err := os.WriteFile(logPath, []byte(
		"ward dispatch broker: director-box requested `ward agent engineer coilyco-flight-deck/ward#1035 --harness codex`\n"+
			"WARDED_WORKFLOW: dispatch-failed\n"), 0o644); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want failed-before-start launch to stay visible for cleanup", len(rows))
	}
	row := rows[0]
	if row.Status != "failed" {
		t.Fatalf("status = %q, want failed", row.Status)
	}
	if row.Phase != agentLaunchPhaseFailed {
		t.Fatalf("phase = %q, want %q", row.Phase, agentLaunchPhaseFailed)
	}
	payload := agentListJSONFromRows(rows)
	if payload.Count != 0 || payload.LaunchIntents != 0 {
		t.Fatalf("payload counts = active=%d intents=%d, want 0/0", payload.Count, payload.LaunchIntents)
	}
	if payload.CleanupNeeded != 0 || payload.FailedBefore != 1 {
		t.Fatalf("payload excluded counts = cleanup=%d failed=%d, want 0/1", payload.CleanupNeeded, payload.FailedBefore)
	}
	human := renderAgentListHuman(rows)
	for _, want := range []string{
		"1 failed-before-start record",
		"phase:     failed before container start",
		"status:    failed",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("failed launch human output missing %q:\n%s", want, human)
		}
	}
}

func TestAgentListPrunesFailedBeforeStartRowAfterSuccessfulDrain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1037}
	resPath, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(resPath, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1037",
		Branch:    "issue-1037",
		Host:      "director-box",
		At:        now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	paths, logf, err := openDispatchArtifact(dispatchBrokerRequest{
		Requester: "director-box",
		Role:      roleEngineer,
		Argv:      []string{"engineer", ref.String(), "--harness", string(modeCodex)},
	}, now.Add(-2*time.Minute), "req-1037")
	if err != nil {
		t.Fatalf("openDispatchArtifact: %v", err)
	}
	if _, err := fmt.Fprintln(logf, "WARDED_WORKFLOW: dispatch-failed"); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}
	if err := logf.Close(); err != nil {
		t.Fatalf("close dispatch log: %v", err)
	}
	finalizeDispatchArtifact(paths, dispatchBrokerRequest{
		Requester: "director-box",
		Role:      roleEngineer,
		Argv:      []string{"engineer", ref.String(), "--harness", string(modeCodex)},
	}, paths.ConsolePath, fmt.Errorf("launch failed before container start"))

	archiveDir := filepath.Join(agentLogsDir(), "engineer-codex-ward-1037")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("MkdirAll archive dir: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(archiveDir, drainMetaFile), runMeta{
		Container: "engineer-codex-ward-1037",
		Repo:      ref.repoSlug(),
		Issue:     strconv.Itoa(ref.Number),
		Outcome:   outcomePushedMain,
		Summary: runSummary{
			NormalizedOutcome: "landed-main",
		},
	}); err != nil {
		t.Fatalf("write archive meta: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want successful drain to prune the stale launch row", len(rows))
	}
	if _, ok, err := readAgentReservation(resPath); err != nil {
		t.Fatalf("readAgentReservation: %v", err)
	} else if ok {
		t.Fatal("successful archived run should clear the stale reservation cache entry")
	}
	report := r.dispatchHealthSnapshot(t.Context(), []string{ref.repoSlug()}, directorMaxParallelDefault())
	if report.FailedBefore != 0 {
		t.Fatalf("failed-before-start = %d, want 0 after successful drain", report.FailedBefore)
	}
	for _, signal := range report.Signals {
		if signal == "stale-records" {
			t.Fatalf("dispatch-health still surfaced stale-records: %+v", report.Signals)
		}
	}
}

func TestAgentListMarksPartialLaunchWhenReservationMarkerIsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1036}
	srv := issueThreadAuthorityServer(t, []issueThreadAuthorityRow{{
		Number: 1036,
		Title:  "missing marker",
		Body:   "body",
		Comments: []issueComment{
			{Body: "just a normal comment", CreatedAt: now.Add(-time.Second)},
		},
	}})
	t.Setenv("WARD_FORGEJO_BASE", srv.URL)
	resPath, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(resPath, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1036",
		Branch:    "issue-1036",
		Host:      "director-box",
		At:        now.Add(-time.Second),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	r := fakeEngineerRunningDockerRunner(t, now.Add(-time.Second).Format(time.RFC3339Nano))
	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 running engineer", len(rows))
	}
	row := rows[0]
	if row.Status != agentLaunchStatusPartial {
		t.Fatalf("status = %q, want %q", row.Status, agentLaunchStatusPartial)
	}
	if row.Remediation == "" {
		t.Fatal("partial launch row should carry remediation")
	}
	for _, want := range []string{
		ref.String(),
		"engineer-codex-ward-1036",
		"reservation-held marker",
	} {
		if !strings.Contains(row.Remediation, want) {
			t.Fatalf("remediation missing %q: %s", want, row.Remediation)
		}
	}
	payload := agentListJSONFromRows(rows)
	if payload.PartialLaunch != 1 || payload.Count != 1 {
		t.Fatalf("payload counts = partial=%d count=%d, want 1/1", payload.PartialLaunch, payload.Count)
	}
	human := renderAgentListHuman(rows)
	for _, want := range []string{
		"1 partial launch",
		ref.repoSlug(),
		strconv.Itoa(ref.Number),
		"engineer-codex-ward-1036",
		"remediation: issue " + ref.String(),
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output missing %q:\n%s", want, human)
		}
	}
}

func TestAgentListPrunesStaleReservationCacheEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := issueThreadAuthorityServer(t, []issueThreadAuthorityRow{{
		Number: 1035,
		Comments: []issueComment{
			{Body: "just a normal comment", CreatedAt: time.Now().UTC()},
		},
	}})
	t.Setenv("WARD_FORGEJO_BASE", server.URL)
	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1035}
	resPath, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(resPath, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1035",
		Branch:    "issue-1035",
		Host:      "director-box",
		At:        now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}

	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want stale reservation cache to be pruned", len(rows))
	}
	if _, ok, _ := readAgentReservation(resPath); ok {
		t.Fatal("stale reservation cache entry should be removed")
	}
}

func TestClearAgentReservationCacheDirRecreatesDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := agentReservationCacheDir()
	if err != nil {
		t.Fatalf("agentReservationCacheDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll reservation cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.lock"), []byte("lock"), 0o600); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}
	if err := clearAgentReservationCacheDir(); err != nil {
		t.Fatalf("clearAgentReservationCacheDir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir reservation cache: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("reservation cache directory should be recreated empty, got %d entries", len(entries))
	}
}

func TestAgentListRecreatesMissingReservationCacheDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := agentReservationCacheDir()
	if err != nil {
		t.Fatalf("agentReservationCacheDir: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove reservation cache dir: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("reservation cache dir should be missing before list, got err=%v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	rows, err := r.agentListRows(t.Context())
	if err != nil {
		t.Fatalf("agentListRows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 after recreating empty cache dir", len(rows))
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("reservation cache dir should be recreated, got %v", err)
	}
}

func TestDispatchLaunchPhaseFromLog(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "accepted",
			body: "ward dispatch broker: director requested `ward agent engineer coilyco-flight-deck/ward#1 --harness codex`\n",
			want: agentLaunchPhaseQueued,
		},
		{
			name: "preflight",
			body: "ward dispatch broker: director requested `ward agent engineer coilyco-flight-deck/ward#1 --harness codex`\nward agent: preflight start for coilyco-flight-deck/ward#1 via codex\n",
			want: agentLaunchPhasePreflight,
		},
		{
			name: "starting",
			body: "ward dispatch broker: director requested `ward agent engineer coilyco-flight-deck/ward#1 --harness codex`\nward agent: wrote launch env file for coilyco-flight-deck/ward#1\n",
			want: agentLaunchPhaseStarting,
		},
		{
			name: "failed",
			body: "ward dispatch broker: director requested `ward agent engineer coilyco-flight-deck/ward#1 --harness codex`\nWARDED_WORKFLOW: dispatch-failed\n",
			want: agentLaunchPhaseFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, ok := dispatchLaunchPhaseFromLog(tc.body)
			if !ok || got != tc.want {
				t.Fatalf("dispatchLaunchPhaseFromLog = %q (ok=%v), want %q", got, ok, tc.want)
			}
		})
	}
}

func fakeEngineerRunningDockerRunner(t *testing.T, startedAt string) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ]; then\n" +
		"  printf '%s\\n' engineer-codex-ward-1036\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = engineer-codex-ward-1036 ]; then\n" +
		"  cat <<'JSON'\n" +
		"[{\"Name\":\"/engineer-codex-ward-1036\",\"Config\":{\"Labels\":{\"ward\":\"true\",\"ward.role\":\"engineer\",\"ward.driver\":\"codex\",\"ward.repo\":\"coilyco-flight-deck/ward\",\"ward.issue\":\"1036\"},\"Env\":[\"WARD_TARGET_OWNER=coilyco-flight-deck\",\"WARD_TARGET_NAME=ward\",\"WARD_TARGET_REPO=coilyco-flight-deck/ward\",\"WARD_TARGET_ISSUE=1036\",\"WARD_BRANCH=issue-1036\",\"WARD_MODE=codex\"]},\"State\":{\"Status\":\"running\",\"StartedAt\":\"" + startedAt + "\"}}]\n" +
		"JSON\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write fake docker: %v", err)
	}
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func TestFormatAgentListCapacityNotesUnavailableSource(t *testing.T) {
	limit := 12
	remaining := 1
	atCapacity := false
	got := formatAgentListCapacity(agentListCapacity{
		Count:       11,
		Limit:       &limit,
		Remaining:   &remaining,
		AtCapacity:  &atCapacity,
		Unavailable: true,
	})
	if !strings.Contains(got, "capacity source unavailable through broker") {
		t.Fatalf("formatted capacity missing unavailable note: %q", got)
	}
}
