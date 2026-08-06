package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReleaseCandidateValidationRequiresImmutableSafeInputs(t *testing.T) {
	valid := releaseCandidateFixture()
	tests := []struct {
		name   string
		mutate func(*releaseCandidateInput)
	}{
		{name: "abbreviated app commit", mutate: func(v *releaseCandidateInput) { v.ApplicationCommit = "abc123" }},
		{name: "uppercase deploy commit", mutate: func(v *releaseCandidateInput) { v.StartingDeployCommit = strings.ToUpper(releaseCommit("a")) }},
		{name: "mutable artifact", mutate: func(v *releaseCandidateInput) { v.ArtifactDigest = "latest" }},
		{name: "repository URL", mutate: func(v *releaseCandidateInput) { v.ApplicationRepository = "https://example.test/acme/app.git" }},
		{name: "noncanonical ticket", mutate: func(v *releaseCandidateInput) { v.OriginatingTicket = "https://forgejo.example/acme/app/issues/42" }},
		{name: "operation argv", mutate: func(v *releaseCandidateInput) { v.DeployOperation = "deploy --force" }},
		{name: "operation path", mutate: func(v *releaseCandidateInput) { v.VerifyOperation = "./verify" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := valid
			test.mutate(&got)
			if err := validateReleaseCandidateInput(got); err == nil {
				t.Fatalf("accepted invalid candidate: %#v", got)
			}
		})
	}
	if err := validateReleaseCandidateInput(valid); err != nil {
		t.Fatalf("valid candidate: %v", err)
	}
}

func TestReleaseCandidateEnvelopeRequiresProvenanceAndMatchingHash(t *testing.T) {
	candidate := releaseCandidateFromFixture(t)
	if err := validateReleaseCandidate(candidate); err != nil {
		t.Fatalf("valid candidate envelope: %v", err)
	}

	missing := candidate
	missing.Correlation.WardRunID = ""
	if err := validateReleaseCandidate(missing); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("missing provenance error = %v", err)
	}

	mutated := candidate
	mutated.ApplicationCommit = releaseCommit("d")
	if err := validateReleaseCandidate(mutated); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("mutated candidate error = %v", err)
	}
}

func TestReleaseCandidateAndResultSerializeRoundTrip(t *testing.T) {
	candidate := releaseCandidateFromFixture(t)
	result := releaseResult{
		SchemaVersion:   releaseSchemaVersion,
		CandidateID:     candidate.CandidateID,
		CandidateHash:   candidate.ContentHash,
		AttemptID:       strings.Repeat("3", 32),
		From:            "ops-one",
		To:              candidate.From,
		CreatedAt:       time.Date(2026, time.August, 5, 12, 1, 0, 0, time.UTC),
		Classification:  releaseOutcomeVerified,
		ReasonCode:      "health-checks-passed",
		EvidenceDigests: []string{releaseDigest("4")},
		DeployCommit:    releaseCommit("5"),
		Correlation:     releaseCorrelation{ClusterID: "codex-ab45", WardRunID: "ops-one", DispatchRequestID: strings.Repeat("6", 32)},
	}
	record := releaseArtifactRecord{
		ID: strings.Repeat("7", 32), Kind: releaseRecordKindResult, CreatedAt: result.CreatedAt, Result: &result,
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded releaseArtifactRecord
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Result == nil || decoded.Result.CandidateHash != candidate.ContentHash || decoded.Result.DeployCommit != result.DeployCommit {
		t.Fatalf("round trip = %#v", decoded)
	}
}

func TestReleaseResultValidationCoversEveryTerminalClassification(t *testing.T) {
	base := releaseResultInput{
		CandidateID:     strings.Repeat("1", 32),
		AttemptID:       strings.Repeat("2", 32),
		ReasonCode:      "synthetic-evidence",
		EvidenceDigests: []string{releaseDigest("3")},
	}
	tests := []struct {
		classification string
		deployCommit   string
		restoredCommit string
	}{
		{classification: releaseOutcomeVerified, deployCommit: releaseCommit("4")},
		{classification: releaseOutcomeRejected},
		{classification: releaseOutcomeRestored, restoredCommit: releaseCommit("5")},
		{classification: releaseOutcomeBlocked},
		{classification: releaseOutcomeIndeterminate},
	}
	for _, test := range tests {
		t.Run(test.classification, func(t *testing.T) {
			input := base
			input.Classification = test.classification
			input.DeployCommit = test.deployCommit
			input.RestoredCommit = test.restoredCommit
			if err := validateReleaseResultInput(input); err != nil {
				t.Fatalf("valid %s result: %v", test.classification, err)
			}
		})
	}

	for _, classification := range []string{releaseOutcomeRejected, releaseOutcomeRestored, releaseOutcomeBlocked, releaseOutcomeIndeterminate} {
		input := base
		input.Classification = classification
		input.DeployCommit = releaseCommit("6")
		if err := validateReleaseResultInput(input); err == nil {
			t.Fatalf("%s result claimed a new deploy commit", classification)
		}
	}
}

func TestReleaseBrokerFlowPersistsCandidateAndEveryTerminalResult(t *testing.T) {
	for _, classification := range []string{
		releaseOutcomeVerified,
		releaseOutcomeRejected,
		releaseOutcomeRestored,
		releaseOutcomeBlocked,
		releaseOutcomeIndeterminate,
	} {
		t.Run(classification, func(t *testing.T) {
			admission := setupReleaseArtifactFixture(t)
			input := releaseCandidateFixture()
			candidateRecords, err := createReleaseCandidate(dispatchBrokerRequest{
				AuthenticatedRole: roleDirector,
				Requester:         "director-one",
				BrokerID:          admission.ClusterID,
				To:                admission.PeerID,
				Release:           &releaseBrokerPayload{Candidate: &input},
			})
			if err != nil {
				t.Fatal(err)
			}
			candidate := candidateRecords[0].Candidate
			attempt := candidateRecords[0].Attempt
			if candidate == nil || attempt == nil || candidate.To != admission.PeerID || candidate.From != "director-one" {
				t.Fatalf("candidate record = %#v", candidateRecords)
			}
			resultInput := releaseResultInput{
				CandidateID:     candidate.CandidateID,
				AttemptID:       attempt.AttemptID,
				Classification:  classification,
				ReasonCode:      "synthetic-evidence",
				EvidenceDigests: []string{releaseDigest("8")},
			}
			switch classification {
			case releaseOutcomeVerified:
				resultInput.DeployCommit = releaseCommit("9")
			case releaseOutcomeRestored:
				resultInput.RestoredCommit = candidate.StartingDeployCommit
			}
			resultRecords, err := recordReleaseResult(dispatchBrokerRequest{
				AuthenticatedRole: releaseOpsRole,
				Requester:         admission.PeerID,
				BrokerID:          admission.ClusterID,
				Release:           &releaseBrokerPayload{Result: &resultInput},
			})
			if err != nil {
				t.Fatal(err)
			}
			result := resultRecords[0].Result
			if result == nil || result.To != "director-one" || result.CandidateHash != candidate.ContentHash {
				t.Fatalf("result record = %#v", resultRecords)
			}
			if classification != releaseOutcomeVerified && result.DeployCommit != "" {
				t.Fatalf("%s result persisted deploy commit %q", classification, result.DeployCommit)
			}

			persisted, err := readReleaseArtifactRecords(admission)
			if err != nil {
				t.Fatal(err)
			}
			if len(persisted) != 2 || persisted[0].Candidate == nil || persisted[1].Result == nil {
				t.Fatalf("persisted records = %#v", persisted)
			}
		})
	}
}

func TestReleaseBrokerRejectsWrongRolesAndPeer(t *testing.T) {
	admission := setupReleaseArtifactFixture(t)
	input := releaseCandidateFixture()
	if _, err := createReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: "engineer", Requester: "engineer-one", BrokerID: admission.ClusterID,
		To: admission.PeerID, Release: &releaseBrokerPayload{Candidate: &input},
	}); err == nil {
		t.Fatal("non-Director created a candidate")
	}
	if _, err := createReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		To: "ops-unadmitted", Release: &releaseBrokerPayload{Candidate: &input},
	}); err == nil {
		t.Fatal("Director targeted an unadmitted Ops identity")
	}

	records, err := createReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		To: admission.PeerID, Release: &releaseBrokerPayload{Candidate: &input},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := releaseResultInput{
		CandidateID:     records[0].Candidate.CandidateID,
		AttemptID:       records[0].Attempt.AttemptID,
		Classification:  releaseOutcomeBlocked,
		ReasonCode:      "synthetic-block",
		EvidenceDigests: []string{releaseDigest("a")},
	}
	if _, err := recordReleaseResult(dispatchBrokerRequest{
		AuthenticatedRole: releaseOpsRole, Requester: "ops-other", BrokerID: admission.ClusterID,
		Release: &releaseBrokerPayload{Result: &result},
	}); err == nil {
		t.Fatal("wrong Ops peer returned a result")
	}
}

func TestReleaseBrokerAuthenticatedRoundTrip(t *testing.T) {
	admission := setupReleaseArtifactFixture(t)
	t.Setenv(envDispatchBrokerID, admission.ClusterID)
	t.Setenv("WARD_ROLE", roleDirector)
	const master = "synthetic-master-capability"
	input := releaseCandidateFixture()
	candidateResponse := releaseBrokerRoundTrip(t, master, "director-one", dispatchBrokerRequest{
		Action:  dispatchActionReleaseCandidate,
		To:      admission.PeerID,
		Release: &releaseBrokerPayload{Candidate: &input},
	})
	if !candidateResponse.OK || len(candidateResponse.ReleaseRecords) != 1 {
		t.Fatalf("candidate response = %#v", candidateResponse)
	}
	candidate := candidateResponse.ReleaseRecords[0].Candidate
	attempt := candidateResponse.ReleaseRecords[0].Attempt
	resultInput := releaseResultInput{
		CandidateID:     candidate.CandidateID,
		AttemptID:       attempt.AttemptID,
		Classification:  releaseOutcomeBlocked,
		ReasonCode:      "synthetic-block",
		EvidenceDigests: []string{releaseDigest("d")},
	}
	opsCapability := dispatchBrokerAgentCapability(master, admission.PeerID, releaseOpsRole)
	resultResponse := releaseBrokerRoundTrip(t, opsCapability, "director-one", dispatchBrokerRequest{
		Action:  dispatchActionReleaseResult,
		Release: &releaseBrokerPayload{Result: &resultInput},
	})
	if !resultResponse.OK || len(resultResponse.ReleaseRecords) != 1 || resultResponse.ReleaseRecords[0].Result.From != admission.PeerID {
		t.Fatalf("result response = %#v", resultResponse)
	}
}

func TestReleaseRetryRequiresSameRevisionAndRetryableOutcome(t *testing.T) {
	admission := setupReleaseArtifactFixture(t)
	input := releaseCandidateFixture()
	records, err := createReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		To: admission.PeerID, Release: &releaseBrokerPayload{Candidate: &input},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate := records[0].Candidate
	attempt := records[0].Attempt
	blocked := releaseResultInput{
		CandidateID:     candidate.CandidateID,
		AttemptID:       attempt.AttemptID,
		Classification:  releaseOutcomeBlocked,
		ReasonCode:      "registry-unavailable",
		EvidenceDigests: []string{releaseDigest("b")},
	}
	if _, err := recordReleaseResult(dispatchBrokerRequest{
		AuthenticatedRole: releaseOpsRole, Requester: admission.PeerID, BrokerID: admission.ClusterID,
		Release: &releaseBrokerPayload{Result: &blocked},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := retryReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		Release: &releaseBrokerPayload{CandidateID: candidate.CandidateID, StartingDeployCommit: releaseCommit("c")},
	}); err == nil || !strings.Contains(err.Error(), "revision changed") {
		t.Fatalf("changed revision retry error = %v", err)
	}
	retry, err := retryReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		Release: &releaseBrokerPayload{CandidateID: candidate.CandidateID, StartingDeployCommit: candidate.StartingDeployCommit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry[0].Attempt == nil || retry[0].Attempt.AttemptID == attempt.AttemptID {
		t.Fatalf("retry = %#v", retry)
	}
}

func TestReleaseArtifactIsReadableThroughDispatchRequestLogs(t *testing.T) {
	admission := setupReleaseArtifactFixture(t)
	input := releaseCandidateFixture()
	if _, err := createReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		To: admission.PeerID, Release: &releaseBrokerPayload{Candidate: &input},
	}); err != nil {
		t.Fatal(err)
	}
	source, matched, err := resolveAgentLogsSourceForRequestID(admission.RequestID, 0, false, agentLogsResolveOptions{Artifact: agentLogArtifactRelease})
	if err != nil {
		t.Fatal(err)
	}
	if !matched || source.Path == "" || filepath.Base(source.Path) != releaseArtifactFile {
		t.Fatalf("release log source = %#v, matched=%t", source, matched)
	}
}

func releaseCandidateFixture() releaseCandidateInput {
	return releaseCandidateInput{
		ApplicationRepository: "acme/app",
		ApplicationCommit:     releaseCommit("1"),
		ArtifactDigest:        releaseDigest("2"),
		Environment:           "production",
		DeployRepository:      "acme/deploy",
		StartingDeployCommit:  releaseCommit("3"),
		OriginatingTicket:     "acme/app#42",
		DeployOperation:       "release.deploy",
		VerifyOperation:       "release.verify",
	}
}

func releaseBrokerRoundTrip(t *testing.T, token, requester string, req dispatchBrokerRequest) dispatchBrokerResponse {
	t.Helper()
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&Runner{}).handleHostDispatchBrokerConn(context.Background(), server, requester, "synthetic-master-capability")
	}()
	req.Token = token
	if err := json.NewEncoder(client).Encode(req); err != nil {
		t.Fatal(err)
	}
	var response dispatchBrokerResponse
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	<-done
	return response
}

func releaseCandidateFromFixture(t *testing.T) releaseCandidate {
	t.Helper()
	input := releaseCandidateFixture()
	candidate := releaseCandidate{
		SchemaVersion:         releaseSchemaVersion,
		CandidateID:           strings.Repeat("a", 32),
		From:                  "director-one",
		To:                    "ops-one",
		CreatedAt:             time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		ApplicationRepository: input.ApplicationRepository,
		ApplicationCommit:     input.ApplicationCommit,
		ArtifactDigest:        input.ArtifactDigest,
		Environment:           input.Environment,
		DeployRepository:      input.DeployRepository,
		StartingDeployCommit:  input.StartingDeployCommit,
		OriginatingTicket:     input.OriginatingTicket,
		DeployOperation:       input.DeployOperation,
		VerifyOperation:       input.VerifyOperation,
		Correlation:           releaseCorrelation{ClusterID: "codex-ab45", WardRunID: "director-one"},
	}
	var err error
	candidate.ContentHash, err = releaseCandidateContentHash(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func setupReleaseArtifactFixture(t *testing.T) dispatchPeerAdmission {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	admission := dispatchPeerAdmission{
		ClusterID: "codex-ab45",
		RequestID: strings.Repeat("c", 32),
		PeerID:    "ops-one",
		Role:      releaseOpsRole,
		Status:    dispatchPeerStatusActive,
		Admitted:  time.Now().UTC(),
		Updated:   time.Now().UTC(),
	}
	path, err := dispatchPeerAdmissionsPath(admission.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDispatchPeerAdmissions(path, []dispatchPeerAdmission{admission}); err != nil {
		t.Fatal(err)
	}
	artifactDir := filepath.Join(agentLogsDir(), dispatchArtifactsSubdir, admission.RequestID+"-ops")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journalPath, err := dispatchJournalPath(admission.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if err := createDispatchJournal(journalPath, dispatchRequestJournal{
		Version:    dispatchJournalVersion,
		RequestID:  admission.RequestID,
		Paths:      dispatchArtifactPaths{RequestID: admission.RequestID, Dir: artifactDir},
		BrokerID:   admission.ClusterID,
		AcceptedAt: time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return admission
}

func releaseCommit(fill string) string {
	return strings.Repeat(fill, 40)
}

func releaseDigest(fill string) string {
	return "sha256:" + strings.Repeat(fill, 64)
}
