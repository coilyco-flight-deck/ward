package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentRunningEngineerFromInspectIncludesReservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

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

	payload := agentListJSONFromRows([]agentRunningEngineer{row})
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	if payload.LaunchIntents != 0 {
		t.Fatalf("launch_intents = %d, want 0", payload.LaunchIntents)
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

	human := renderAgentListHuman([]agentRunningEngineer{row})
	for _, want := range []string{
		"ward agent: running engineers (1/12, 11 slots free)",
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

func TestAgentListIncludesReservedLaunchPhase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1033}
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
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0 running engineers", payload.Count)
	}
	if payload.LaunchIntents != 1 {
		t.Fatalf("launch_intents = %d, want 1", payload.LaunchIntents)
	}
	if row.ReservedAt.IsZero() || !row.ReservedAt.Equal(now.Add(-time.Second)) {
		t.Fatalf("reserved_at = %v, want %v", row.ReservedAt, now.Add(-time.Second))
	}
	human := renderAgentListHuman(rows)
	for _, want := range []string{
		"ward agent: running engineers (0/12, 12 slots free) + 1 launch intent pending",
		ref.String(),
		"phase:     container starting",
		"status:    starting",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("reserved-launch human output missing %q:\n%s", want, human)
		}
	}
}

func TestAgentListDropsStalePrelaunchLaunches(t *testing.T) {
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
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want stale prelaunch launch to drop out of the active count", len(rows))
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
