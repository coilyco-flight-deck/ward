package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDispatchLifecyclePersistsSafeCorrelationAndDropsRecoveryPayload(t *testing.T) {
	accepted := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	secretPrompt := "fix this with secret prompt material"
	req := dispatchBrokerRequest{
		RequestID: strings.Repeat("a", 32), BrokerID: "codex-ab45", Role: roleEngineer,
		Argv: []string{"engineer", "coilyco-flight-deck/ward#1618", "--harness", "codex", "--workflow", "merge-remote-main", "--details", secretPrompt},
	}
	paths := newDispatchArtifactPaths(req, accepted, req.RequestID)
	journal := dispatchRequestJournal{RequestID: req.RequestID, Request: req, Paths: paths, AcceptedAt: accepted, UpdatedAt: accepted}
	initializeDispatchLifecycle(&journal, req, paths)
	if journal.State != dispatchStateAccepted || journal.Repo != "coilyco-flight-deck/ward" || journal.Issue != "1618" || journal.Workflow != "merge-remote-main" {
		t.Fatalf("accepted lifecycle = %#v", journal)
	}
	advanceDispatchLifecycle(&journal, dispatchPhaseVisible, dispatchOutcomeInProgress, nil)
	if journal.State != dispatchStateRunning || len(journal.Request.Argv) != 0 {
		t.Fatalf("running lifecycle retained recovery payload: %#v", journal)
	}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secretPrompt) {
		t.Fatalf("terminal-safe journal retained prompt body: %s", body)
	}
}

func TestDispatchLifecycleClassifiesRefusalFailureAndDrainOutcome(t *testing.T) {
	tests := []struct {
		name    string
		phase   string
		outcome string
		err     error
		want    string
	}{
		{name: "launching", phase: dispatchPhasePreflight, outcome: dispatchOutcomeInProgress, want: dispatchStateLaunching},
		{name: "refusal", phase: dispatchPhaseTerminal, outcome: dispatchOutcomeFailed, err: errors.New("capacity is full"), want: dispatchStateBlocked},
		{name: "launch failure", phase: dispatchPhaseTerminal, outcome: dispatchOutcomeFailed, err: errors.New("docker create failed"), want: dispatchStateFailed},
		{name: "restart orphan", phase: dispatchPhaseStarting, outcome: dispatchOutcomeInterrupted, want: dispatchStateInterrupted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := dispatchLifecycleForUpdate(tc.phase, tc.outcome, tc.err)
			if got != tc.want {
				t.Fatalf("state = %q, want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct{ normalized, want string }{
		{"landed-main", dispatchStateCompleted}, {"blocked", dispatchStateBlocked}, {"failed", dispatchStateFailed},
	} {
		if got, _ := dispatchTerminalStateForDrain(tc.normalized, nil); got != tc.want {
			t.Fatalf("drain %q = %q, want %q", tc.normalized, got, tc.want)
		}
	}
	if got, _ := dispatchTerminalStateForDrain("landed-main", errors.New("scrub failed")); got != dispatchStateCleanupNeeded {
		t.Fatalf("failed drain = %q, want cleanup-needed", got)
	}
}

func TestDispatchLifecycleTerminalStateCannotRegressToRunning(t *testing.T) {
	journal := dispatchRequestJournal{State: dispatchStateCompleted, Outcome: dispatchStateCompleted}
	advanceDispatchLifecycle(&journal, dispatchPhaseTerminal, dispatchOutcomeLaunched, nil)
	if journal.State != dispatchStateCompleted {
		t.Fatalf("terminal lifecycle regressed to %q", journal.State)
	}
}

func TestDispatchLifecyclePruneSelectsOnlyOldTerminalRecords(t *testing.T) {
	cutoff := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	old := cutoff.Add(-time.Hour)
	recent := cutoff.Add(time.Hour)
	journals := []dispatchRequestJournal{
		{RequestID: "completed", State: dispatchStateCompleted, UpdatedAt: old},
		{RequestID: "blocked", State: dispatchStateBlocked, UpdatedAt: old},
		{RequestID: "cleanup", State: dispatchStateCleanupNeeded, UpdatedAt: old},
		{RequestID: "running", State: dispatchStateRunning, UpdatedAt: old},
		{RequestID: "recent", State: dispatchStateFailed, UpdatedAt: recent},
	}
	got := dispatchLifecyclePruneCandidates(journals, cutoff)
	if len(got) != 2 || got[0].RequestID != "completed" || got[1].RequestID != "blocked" {
		t.Fatalf("prune candidates = %#v", got)
	}
}

func TestDispatchLifecycleHumanAndJSONAgreeOnStateOutcomeAndNextAction(t *testing.T) {
	record := dispatchLifecycleRecord{
		RequestID: strings.Repeat("b", 32), State: dispatchStateInterrupted,
		Outcome: dispatchOutcomeInterrupted, NextAction: "inspect orphan evidence",
		LastTransition: dispatchLifecycleTransition{To: dispatchStateInterrupted, At: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC), ReasonCode: "orphaned-after-starting"},
	}
	human := renderDispatchLifecycleHuman([]dispatchLifecycleRecord{record})
	for _, want := range []string{"state: interrupted", "inspect orphan evidence", "orphaned-after-starting"} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output missing %q: %s", want, human)
		}
	}
	payload := dispatchLifecycleJSON([]dispatchLifecycleRecord{record})
	if len(payload.Requests) != 1 || payload.Requests[0].State != record.State || payload.Requests[0].Outcome != record.Outcome || payload.Requests[0].NextAction != record.NextAction {
		t.Fatalf("json lifecycle = %#v", payload)
	}
}

func TestBrokerLifecycleReadSurvivesOriginatingTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envDispatchJournalDir, dir)
	requestID := strings.Repeat("c", 32)
	artifactDir := t.TempDir()
	consolePath := filepath.Join(artifactDir, dispatchArtifactConsoleFile)
	if err := os.WriteFile(consolePath, []byte("safe dispatch evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := dispatchRequestJournal{
		Version: dispatchJournalVersion, RequestID: requestID, State: dispatchStateRunning,
		Outcome: dispatchOutcomeLaunched, Phase: dispatchPhaseTerminal,
		Repo: "coilyco-flight-deck/ward", Issue: "1618", Ref: "coilyco-flight-deck/ward#1618",
		Paths:      dispatchArtifactPaths{RequestID: requestID, Dir: artifactDir, ConsolePath: consolePath},
		AcceptedAt: time.Now().UTC().Add(-time.Minute), UpdatedAt: time.Now().UTC(),
		LastTransition: dispatchLifecycleTransition{To: dispatchStateRunning, At: time.Now().UTC(), ReasonCode: "container-visible"},
	}
	path, err := dispatchJournalPath(requestID)
	if err != nil {
		t.Fatal(err)
	}
	if err := createDispatchJournal(path, journal); err != nil {
		t.Fatal(err)
	}

	server, client := net.Pipe()
	go func() {
		defer server.Close()
		newRunner().runDispatchBrokerLifecycleRead(server, dispatchBrokerRequest{Action: dispatchActionLifecycleStatus, Target: requestID, Format: "json"})
	}()
	dec := json.NewDecoder(client)
	var response dispatchBrokerResponse
	if err := dec.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatalf("broker response = %#v", response)
	}
	body, err := io.ReadAll(io.MultiReader(dec.Buffered(), client))
	if err != nil {
		t.Fatal(err)
	}
	var payload dispatchLifecycleListJSON
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Requests) != 1 || payload.Requests[0].RequestID != requestID || payload.Requests[0].State != dispatchStateRunning {
		t.Fatalf("broker lifecycle payload = %#v", payload)
	}

	source, err := newRunner().resolveAgentLogsSource(t.Context(), requestID, 0, false, agentLogsResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != agentLogSourceFile || source.Path != consolePath {
		t.Fatalf("request-id log source = %#v", source)
	}
}
