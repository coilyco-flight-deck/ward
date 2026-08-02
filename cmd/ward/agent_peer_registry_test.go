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
	go writeDispatchBrokerLaunchResponse(server, req, "/tmp/dispatch", nil)
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ClusterID != req.BrokerID || resp.AgentID != req.AgentID || resp.RequestID != req.RequestID {
		t.Fatalf("launch response = %#v", resp)
	}
}

func TestBrokerAdmitsHostMountedPeerBeforeContainerLaunch(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv(envDispatchJournalDir, t.TempDir())
	t.Setenv(envContainerService, dispatchBrokerService)
	t.Setenv(envDispatchBrokerID, "codex-ab45")
	t.Setenv(envDispatchBrokerToken, "master-capability")
	original := dispatchPeerIDSuffix
	t.Cleanup(func() { dispatchPeerIDSuffix = original })
	dispatchPeerIDSuffix = func() string { return "cd67" }
	requestID := newDispatchBrokerRequestID()

	response, err := admitHostMountedDispatchPeer("critic", requestID, "")
	if err != nil {
		t.Fatal(err)
	}
	if response.ClusterID != "codex-ab45" || response.PeerID != "critic-cd67" ||
		response.RequestID != requestID || response.Capability == "" {
		t.Fatalf("host-mounted admission = %#v", response)
	}
	journalPath, err := dispatchJournalPath(requestID)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := readDispatchJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Request.AgentID != response.PeerID || journal.Phase != dispatchPhaseAccepted {
		t.Fatalf("accepted journal = %#v", journal)
	}
	if err := finishHostMountedDispatchPeer("critic", requestID, response.PeerID, dispatchPeerStatusActive, ""); err != nil {
		t.Fatal(err)
	}
	journal, err = readDispatchJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Outcome != dispatchOutcomeLaunched {
		t.Fatalf("terminal journal = %#v", journal)
	}
	active, err := activeDispatchPeers("codex-ab45")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].PeerID != response.PeerID {
		t.Fatalf("active peer registry = %#v", active)
	}
	if err := retireDispatchPeer("codex-ab45", response.PeerID); err != nil {
		t.Fatal(err)
	}
	active, err = activeDispatchPeers("codex-ab45")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("stopped peer remained active: %#v", active)
	}
	journal, err = readDispatchJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Outcome != dispatchOutcomeInterrupted {
		t.Fatalf("stopped journal = %#v", journal)
	}
}

func TestPeerContainerLookupUsesExactIdentityAndClusterLabels(t *testing.T) {
	got := peerContainerListArgs("critic-cd67", "codex-ab45", true)
	want := []string{
		"ps", "-a", "--format", "{{.Names}}",
		"--filter", "label=" + containerLabel,
		"--filter", "label=" + labelPeer + "=critic-cd67",
		"--filter", "label=" + labelCluster + "=codex-ab45",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("peer lookup argv = %#v, want %#v", got, want)
	}
}

func TestPeerLabelExposesBrokerMintedIdentity(t *testing.T) {
	plan := upPlan{Role: "critic", Mode: modeCodex, ClusterID: "codex-ab45", AgentID: "critic-ab45"}
	labels := labelsMap(plan.labels())
	if labels[labelCluster] != "codex-ab45" || labels[labelPeer] != "critic-ab45" || labels[labelRole] != "critic" {
		t.Fatalf("peer labels = %#v", labels)
	}
}
