package main

import (
	"encoding/json"
	"net"
	"reflect"
	"testing"
)

func TestBrokerMintsDistinctDurablePeerIDs(t *testing.T) {
	setTestHome(t, t.TempDir())
	original := dispatchPeerIDSuffix
	t.Cleanup(func() { dispatchPeerIDSuffix = original })
	suffixes := []string{"ab45", "ab45", "cd67"}
	dispatchPeerIDSuffix = func() string {
		value := suffixes[0]
		suffixes = suffixes[1:]
		return value
	}

	first := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(), BrokerID: "codex-ab45", Role: "critic",
		Argv: []string{"run", "--role", "critic", "review this"},
	}
	created, err := admitDispatchPeer(&first)
	if err != nil || !created {
		t.Fatalf("first admission = %t, %v", created, err)
	}
	if first.AgentID != "critic-ab45" || !reflect.DeepEqual(first.Argv[:3], []string{"run", "--agent-id", "critic-ab45"}) {
		t.Fatalf("first admission = %#v", first)
	}

	second := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(), BrokerID: "codex-ab45", Role: "critic",
		Argv: []string{"run", "--role", "critic", "review that"},
	}
	created, err = admitDispatchPeer(&second)
	if err != nil || !created {
		t.Fatalf("second admission = %t, %v", created, err)
	}
	if second.AgentID != "critic-cd67" || second.AgentID == first.AgentID {
		t.Fatalf("peer ids = %q, %q", first.AgentID, second.AgentID)
	}

	retry := dispatchBrokerRequest{
		RequestID: first.RequestID, BrokerID: first.BrokerID, Role: first.Role,
		Argv: []string{"run", "--role", "critic", "review this"},
	}
	created, err = admitDispatchPeer(&retry)
	if err != nil || created || retry.AgentID != first.AgentID {
		t.Fatalf("retry admission = %#v, created %t, err %v", retry, created, err)
	}
}

func TestFailedPeerAdmissionLeavesNoActivePhantom(t *testing.T) {
	setTestHome(t, t.TempDir())
	req := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(), BrokerID: "codex-ab45", Role: "critic",
		Argv: []string{"run", "--role", "critic", "review this"},
	}
	if _, err := admitDispatchPeer(&req); err != nil {
		t.Fatal(err)
	}
	if err := updateDispatchPeerStatus(req, dispatchPeerStatusFailed); err != nil {
		t.Fatal(err)
	}
	active, err := activeDispatchPeers(req.BrokerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active peer registry contains failed admission: %#v", active)
	}
}

func TestAcceptedJournalPersistsBrokerMintedPeerBeforeLaunch(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv(envDispatchJournalDir, t.TempDir())
	req := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(), BrokerID: "codex-ab45", Role: "critic",
		Argv: []string{"run", "--role", "critic", "review this"},
	}
	if _, err := admitDispatchPeer(&req); err != nil {
		t.Fatal(err)
	}
	paths, logf, journalPath, existing, err := acceptDispatchLaunch(req)
	if err != nil || existing {
		t.Fatalf("acceptDispatchLaunch = %#v, %t, %v", paths, existing, err)
	}
	if err := logf.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := readDispatchJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Request.AgentID != req.AgentID || journal.Request.AgentID == "" {
		t.Fatalf("journal request identity = %#v", journal.Request)
	}
}

func TestLaunchResponseExposesClusterAndPeerIDs(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	req := dispatchBrokerRequest{RequestID: newDispatchBrokerRequestID(), BrokerID: "codex-ab45", AgentID: "critic-ab45"}
	go writeDispatchBrokerLaunchResponse(server, req, "/tmp/dispatch", dispatchPhaseAccepted, nil)
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ClusterID != req.BrokerID || resp.AgentID != req.AgentID || resp.RequestID != req.RequestID {
		t.Fatalf("launch response = %#v", resp)
	}
}

func TestPeerLabelExposesBrokerMintedIdentity(t *testing.T) {
	plan := upPlan{Role: "critic", Mode: modeCodex, ClusterID: "codex-ab45", AgentID: "critic-ab45"}
	labels := labelsMap(plan.labels())
	if labels[labelCluster] != "codex-ab45" || labels[labelPeer] != "critic-ab45" || labels[labelRole] != "critic" {
		t.Fatalf("peer labels = %#v", labels)
	}
}
