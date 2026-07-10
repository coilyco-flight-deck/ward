package main

import (
	"encoding/json"
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

	defs := bakedSmartDefaults()
	payload := agentListJSONFromRows([]agentRunningEngineer{row}, defs)
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
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
	if len(payload.Engineers) != 1 || payload.Engineers[0].ExecutionLimit != "90m" {
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
		`"execution_limit": "90m"`,
	} {
		if !strings.Contains(string(jbuf), want) {
			t.Fatalf("payload json missing %q:\n%s", want, jbuf)
		}
	}

	human := renderAgentListHuman([]agentRunningEngineer{row}, defs)
	for _, want := range []string{
		"ward agent: running engineer containers (1/12, 11 slots free)",
		"coilyco-gaming/factory-game-v3#18",
		"kais-macbook-pro-2.local",
		"issue-18",
		"running",
		"budget:    73m remaining of 90m limit",
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output missing %q:\n%s", want, human)
		}
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
