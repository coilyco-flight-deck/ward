package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type releaseGitFixture struct {
	root     string
	remote   string
	tx       releaseTransaction
	baseline releaseAttestation
}

type fixtureReleaseJournal struct {
	events []releaseTransactionEventInput
}

func (journal *fixtureReleaseJournal) Record(_ context.Context, event releaseTransactionEventInput) error {
	journal.events = append(journal.events, event)
	return nil
}

type fixtureReleaseFinalizer struct {
	results []releaseResultInput
}

func (finalizer *fixtureReleaseFinalizer) Finalize(_ context.Context, result releaseResultInput) error {
	finalizer.results = append(finalizer.results, result)
	return nil
}

type fixtureReleaseOperations struct {
	current             releaseAttestation
	applyCalls          int
	verifyCalls         int
	failCandidateApply  bool
	failCandidateVerify bool
}

func (operations *fixtureReleaseOperations) Apply(_ context.Context, _ releaseCandidate, desired releaseAttestation) (string, error) {
	operations.applyCalls++
	if operations.failCandidateApply && operations.applyCalls == 1 {
		operations.current.ApplicationCommit = desired.ApplicationCommit
		return releaseEvidenceDigest("partial-apply"), errors.New("synthetic partial apply failure")
	}
	operations.current = desired
	operations.current.EvidenceDigest = releaseEvidenceDigest("apply", desired.DeployCommit)
	return operations.current.EvidenceDigest, nil
}

func (operations *fixtureReleaseOperations) Verify(_ context.Context, _ releaseCandidate, desiredDeployCommit string) (releaseAttestation, error) {
	operations.verifyCalls++
	if operations.failCandidateVerify && desiredDeployCommit != operations.current.DeployCommit {
		return releaseAttestation{}, errors.New("synthetic candidate verification failure")
	}
	if operations.failCandidateVerify && desiredDeployCommit == operations.current.DeployCommit && operations.applyCalls == 1 {
		return releaseAttestation{}, errors.New("synthetic candidate verification failure")
	}
	attestation := operations.current
	attestation.EvidenceDigest = releaseEvidenceDigest("verify", desiredDeployCommit,
		attestation.ApplicationCommit, attestation.ArtifactDigest, attestation.DeployCommit)
	return attestation, nil
}

type releasePushFailureGit struct {
	commandReleaseTransactionGit
	mode        string
	pushCalls   int
	foreignWork string
	foreignOID  string
}

func (git *releasePushFailureGit) PushPrepared(ctx context.Context, tx releaseTransaction) error {
	git.pushCalls++
	switch git.mode {
	case "ambiguous-success":
		if err := git.commandReleaseTransactionGit.PushPrepared(ctx, tx); err != nil {
			return err
		}
		return errors.New("synthetic lost push response")
	case "diverged":
		branchRef := "refs/heads/" + tx.Candidate.DeployBranch
		if err := releaseGitRun(ctx, git.foreignWork, "push", "--porcelain", "--force-with-lease="+branchRef+":"+tx.Candidate.StartingDeployCommit,
			"origin", git.foreignOID+":"+branchRef); err != nil {
			return err
		}
		return errors.New("synthetic divergent push failure")
	default:
		return errors.New("synthetic push failure")
	}
}

type releaseUnlockFailureGit struct {
	commandReleaseTransactionGit
}

func (*releaseUnlockFailureGit) ReleaseLock(context.Context, releaseTransaction, string, string) error {
	return errors.New("synthetic lock release failure")
}

func TestReleaseTransactionSuccessPushesOneVerifiedCommit(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	journal := &fixtureReleaseJournal{}
	finalizer := &fixtureReleaseFinalizer{}
	operations := &fixtureReleaseOperations{current: fixture.baseline}

	report, err := runReleaseTransaction(context.Background(), fixture.tx, commandReleaseTransactionGit{}, operations, journal, finalizer)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeVerified || report.Result.DeployCommit != fixture.tx.PreparedCommit {
		t.Fatalf("result = %#v", report.Result)
	}
	if report.LockRetained || report.CleanupNeeded {
		t.Fatalf("report retained lock: %#v", report)
	}
	assertReleaseRemote(t, fixture, fixture.tx.PreparedCommit)
	if count := releaseTestGitOutput(t, fixture.tx.Worktree, "rev-list", "--count", fixture.tx.Candidate.StartingDeployCommit+"..origin/main"); count != "1" {
		t.Fatalf("remote deploy commit count = %s", count)
	}
	assertReleaseLock(t, fixture, "")
	assertReleasePhases(t, journal,
		releasePhaseAccepted, releasePhaseLockAcquired, releasePhaseStartBound,
		releasePhasePreflightVerified, releasePhasePrepared, releasePhaseApplying,
		releasePhaseApplied, releasePhaseCandidateVerified, releasePhasePushing,
		releaseOutcomeVerified, releasePhaseLockReleased,
	)
	if len(finalizer.results) != 1 || finalizer.results[0].Classification != report.Result.Classification ||
		finalizer.results[0].DeployCommit != report.Result.DeployCommit {
		t.Fatalf("finalized results = %#v", finalizer.results)
	}
}

func TestReleaseTransactionConcurrentAttemptIsBlocked(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	git := commandReleaseTransactionGit{}
	lockRef, lockOID, conflict, err := git.AcquireLock(context.Background(), fixture.tx)
	if err != nil || conflict {
		t.Fatalf("hold first lock: conflict=%t err=%v", conflict, err)
	}
	t.Cleanup(func() { _ = git.ReleaseLock(context.Background(), fixture.tx, lockRef, lockOID) })

	concurrent := fixture.tx
	concurrent.Attempt.AttemptID = strings.Repeat("c", 32)
	journal := &fixtureReleaseJournal{}
	finalizer := &fixtureReleaseFinalizer{}
	operations := &fixtureReleaseOperations{current: fixture.baseline}
	report, err := runReleaseTransaction(context.Background(), concurrent, git, operations, journal, finalizer)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeBlocked || report.Result.ReasonCode != "environment-locked" {
		t.Fatalf("result = %#v", report.Result)
	}
	if operations.applyCalls != 0 {
		t.Fatalf("concurrent attempt applied %d times", operations.applyCalls)
	}
	assertReleaseRemote(t, fixture, fixture.tx.Candidate.StartingDeployCommit)
	assertReleaseLock(t, fixture, lockOID)
}

func TestReleaseTransactionRejectsStaleStartBeforeMutation(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	_, foreignOID := prepareForeignReleaseCommit(t, fixture, true)
	journal := &fixtureReleaseJournal{}
	finalizer := &fixtureReleaseFinalizer{}
	operations := &fixtureReleaseOperations{current: fixture.baseline}

	report, err := runReleaseTransaction(context.Background(), fixture.tx, commandReleaseTransactionGit{}, operations, journal, finalizer)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeRejected || report.Result.ReasonCode != "stale-starting-revision" {
		t.Fatalf("result = %#v", report.Result)
	}
	if operations.applyCalls != 0 || operations.verifyCalls != 0 {
		t.Fatalf("stale attempt reached operations: apply=%d verify=%d", operations.applyCalls, operations.verifyCalls)
	}
	assertReleaseRemote(t, fixture, foreignOID)
	assertReleaseLock(t, fixture, "")
}

func TestReleaseTransactionRestoresAfterPartialApply(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	journal := &fixtureReleaseJournal{}
	finalizer := &fixtureReleaseFinalizer{}
	operations := &fixtureReleaseOperations{current: fixture.baseline, failCandidateApply: true}

	report, err := runReleaseTransaction(context.Background(), fixture.tx, commandReleaseTransactionGit{}, operations, journal, finalizer)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeRestored || report.Result.RestoredCommit != fixture.tx.Candidate.StartingDeployCommit {
		t.Fatalf("result = %#v", report.Result)
	}
	if !releaseAttestationEqual(operations.current, fixture.baseline) || operations.applyCalls != 2 {
		t.Fatalf("environment = %#v, apply calls = %d", operations.current, operations.applyCalls)
	}
	assertReleaseRemote(t, fixture, fixture.tx.Candidate.StartingDeployCommit)
	assertReleaseLock(t, fixture, "")
}

func TestReleaseTransactionRestoresAfterVerificationFailure(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	journal := &fixtureReleaseJournal{}
	finalizer := &fixtureReleaseFinalizer{}
	operations := &fixtureReleaseOperations{current: fixture.baseline, failCandidateVerify: true}

	report, err := runReleaseTransaction(context.Background(), fixture.tx, commandReleaseTransactionGit{}, operations, journal, finalizer)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeRestored {
		t.Fatalf("result = %#v", report.Result)
	}
	if !releaseAttestationEqual(operations.current, fixture.baseline) {
		t.Fatalf("environment = %#v", operations.current)
	}
	assertReleaseRemote(t, fixture, fixture.tx.Candidate.StartingDeployCommit)
}

func TestReleaseTransactionPushFailuresRestoreOrReconcile(t *testing.T) {
	t.Run("retries exhausted", func(t *testing.T) {
		fixture := newReleaseGitFixture(t)
		git := &releasePushFailureGit{mode: "fail"}
		operations := &fixtureReleaseOperations{current: fixture.baseline}
		report, err := runReleaseTransaction(context.Background(), fixture.tx, git, operations,
			&fixtureReleaseJournal{}, &fixtureReleaseFinalizer{})
		if err != nil {
			t.Fatal(err)
		}
		if report.Result.Classification != releaseOutcomeRestored || git.pushCalls != releasePushAttempts {
			t.Fatalf("report = %#v, push calls = %d", report, git.pushCalls)
		}
		if !releaseAttestationEqual(operations.current, fixture.baseline) {
			t.Fatalf("environment = %#v", operations.current)
		}
		assertReleaseRemote(t, fixture, fixture.tx.Candidate.StartingDeployCommit)
		assertReleaseLock(t, fixture, "")
	})

	t.Run("ambiguous response", func(t *testing.T) {
		fixture := newReleaseGitFixture(t)
		git := &releasePushFailureGit{mode: "ambiguous-success"}
		operations := &fixtureReleaseOperations{current: fixture.baseline}
		report, err := runReleaseTransaction(context.Background(), fixture.tx, git, operations,
			&fixtureReleaseJournal{}, &fixtureReleaseFinalizer{})
		if err != nil {
			t.Fatal(err)
		}
		if report.Result.Classification != releaseOutcomeVerified || git.pushCalls != 1 {
			t.Fatalf("report = %#v, push calls = %d", report, git.pushCalls)
		}
		assertReleaseRemote(t, fixture, fixture.tx.PreparedCommit)
		assertReleaseLock(t, fixture, "")
	})

	t.Run("remote diverged", func(t *testing.T) {
		fixture := newReleaseGitFixture(t)
		foreignWork, foreignOID := prepareForeignReleaseCommit(t, fixture, false)
		git := &releasePushFailureGit{mode: "diverged", foreignWork: foreignWork, foreignOID: foreignOID}
		operations := &fixtureReleaseOperations{current: fixture.baseline}
		report, err := runReleaseTransaction(context.Background(), fixture.tx, git, operations,
			&fixtureReleaseJournal{}, &fixtureReleaseFinalizer{})
		if err != nil {
			t.Fatal(err)
		}
		if report.Result.Classification != releaseOutcomeIndeterminate || !report.LockRetained {
			t.Fatalf("report = %#v", report)
		}
		assertReleaseRemote(t, fixture, foreignOID)
		assertReleaseLock(t, fixture, report.LockOID)
	})
}

func TestReleaseTransactionRollbackCreatesANewVerifiedCommit(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	operations := &fixtureReleaseOperations{current: fixture.baseline}
	first, err := runReleaseTransaction(context.Background(), fixture.tx, commandReleaseTransactionGit{}, operations,
		&fixtureReleaseJournal{}, &fixtureReleaseFinalizer{})
	if err != nil || first.Result.Classification != releaseOutcomeVerified {
		t.Fatalf("forward release = %#v, err=%v", first, err)
	}

	rollback := fixture.tx
	rollback.Candidate.CandidateID = strings.Repeat("d", 32)
	rollback.Candidate.ApplicationCommit = fixture.baseline.ApplicationCommit
	rollback.Candidate.ArtifactDigest = fixture.baseline.ArtifactDigest
	rollback.Candidate.StartingDeployCommit = fixture.tx.PreparedCommit
	rollback.Candidate.OriginatingTicket = "acme/app#43"
	rollback.Candidate.CreatedAt = rollback.Candidate.CreatedAt.Add(time.Minute)
	rollback.Candidate.ContentHash = ""
	rollback.Candidate.ContentHash, err = releaseCandidateContentHash(rollback.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	rollback.Attempt = releaseAttempt{
		SchemaVersion: releaseSchemaVersion, AttemptID: strings.Repeat("e", 32),
		CandidateID: rollback.Candidate.CandidateID, CandidateHash: rollback.Candidate.ContentHash,
		RequestedBy: rollback.Candidate.From, To: rollback.Candidate.To, CreatedAt: rollback.Candidate.CreatedAt,
	}
	rollback.PreparedCommit = prepareReleaseCommit(t, rollback.Worktree, rollback.Candidate, "baseline\n")

	report, err := runReleaseTransaction(context.Background(), rollback, commandReleaseTransactionGit{}, operations,
		&fixtureReleaseJournal{}, &fixtureReleaseFinalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeVerified || report.Result.DeployCommit != rollback.PreparedCommit {
		t.Fatalf("rollback result = %#v", report.Result)
	}
	if parent := releaseTestGitOutput(t, rollback.Worktree, "rev-parse", rollback.PreparedCommit+"^"); parent != fixture.tx.PreparedCommit {
		t.Fatalf("rollback parent = %s", parent)
	}
	if count := releaseTestGitOutput(t, rollback.Worktree, "rev-list", "--count", fixture.tx.Candidate.StartingDeployCommit+"..origin/main"); count != "2" {
		t.Fatalf("rollback history count = %s", count)
	}
}

func TestReleaseTransactionRetainsLockWhenCleanupFails(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	git := &releaseUnlockFailureGit{}
	journal := &fixtureReleaseJournal{}
	finalizer := &fixtureReleaseFinalizer{}
	operations := &fixtureReleaseOperations{current: fixture.baseline}

	report, err := runReleaseTransaction(context.Background(), fixture.tx, git, operations, journal, finalizer)
	if err == nil || !report.CleanupNeeded || !report.LockRetained {
		t.Fatalf("report = %#v, err=%v", report, err)
	}
	if report.Result.Classification != releaseOutcomeVerified || len(finalizer.results) != 1 {
		t.Fatalf("result = %#v, finalized = %#v", report.Result, finalizer.results)
	}
	assertReleaseLock(t, fixture, report.LockOID)
	if journal.events[len(journal.events)-1].Phase != releasePhaseCleanupNeeded {
		t.Fatalf("last phase = %#v", journal.events[len(journal.events)-1])
	}
}

func TestReleaseTransactionRestartRecoversJournaledMutation(t *testing.T) {
	fixture := newReleaseGitFixture(t)
	git := commandReleaseTransactionGit{}
	lockRef, lockOID, conflict, err := git.AcquireLock(context.Background(), fixture.tx)
	if err != nil || conflict {
		t.Fatalf("hold restart lock: conflict=%t err=%v", conflict, err)
	}
	partial := fixture.baseline
	partial.ApplicationCommit = fixture.tx.Candidate.ApplicationCommit
	operations := &fixtureReleaseOperations{current: partial}
	fixture.tx.Records = releaseRestartRecords(fixture.tx, fixture.baseline)

	report, err := runReleaseTransaction(context.Background(), fixture.tx, git, operations,
		&fixtureReleaseJournal{}, &fixtureReleaseFinalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Classification != releaseOutcomeRestored || !releaseAttestationEqual(operations.current, fixture.baseline) {
		t.Fatalf("report = %#v, environment = %#v", report, operations.current)
	}
	assertReleaseRemote(t, fixture, fixture.tx.Candidate.StartingDeployCommit)
	assertReleaseLock(t, fixture, "")
	if lockRef == "" || lockOID == "" {
		t.Fatal("restart fixture did not create a remote lock")
	}
}

func TestReleaseBrokerTransactionProgressIsOrderedAndIdempotent(t *testing.T) {
	admission := setupReleaseArtifactFixture(t)
	records, err := createReleaseCandidate(dispatchBrokerRequest{
		AuthenticatedRole: roleDirector, Requester: "director-one", BrokerID: admission.ClusterID,
		To: admission.PeerID, Release: &releaseBrokerPayload{Candidate: ptrReleaseCandidateFixture()},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, attempt := records[0].Candidate, records[0].Attempt
	progress := func(phase string) ([]releaseArtifactRecord, error) {
		return recordReleaseTransactionEvent(dispatchBrokerRequest{
			AuthenticatedRole: releaseOpsRole, Requester: admission.PeerID, BrokerID: admission.ClusterID,
			Release: &releaseBrokerPayload{Transaction: &releaseTransactionEventInput{
				CandidateID: candidate.CandidateID, AttemptID: attempt.AttemptID,
				Phase: phase, ReasonCode: "fixture-progress", EvidenceDigest: releaseEvidenceDigest("fixture", phase),
			}},
		})
	}
	accepted, err := progress(releasePhaseAccepted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := progress(releasePhaseLockAcquired); err != nil {
		t.Fatal(err)
	}
	duplicate, err := progress(releasePhaseAccepted)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate[0].ID != accepted[0].ID {
		t.Fatalf("duplicate progress created a different record: %s != %s", duplicate[0].ID, accepted[0].ID)
	}
	if _, err := progress(releasePhasePrepared); err == nil || !strings.Contains(err.Error(), "cannot follow") {
		t.Fatalf("out-of-order progress error = %v", err)
	}
	persisted, err := readReleaseArtifactRecords(admission)
	if err != nil {
		t.Fatal(err)
	}
	if events := releaseTransactionEvents(persisted, attempt.AttemptID); len(events) != 2 {
		t.Fatalf("persisted transaction events = %#v", events)
	}
}

func ptrReleaseCandidateFixture() *releaseCandidateInput {
	fixture := releaseCandidateFixture()
	return &fixture
}

func newReleaseGitFixture(t *testing.T) releaseGitFixture {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "deploy.git")
	worktree := filepath.Join(root, "deploy-work")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "clone", remote, worktree)
	runGit(t, worktree, "config", "user.name", "Ward Release Fixture")
	runGit(t, worktree, "config", "user.email", "ward-release@example.invalid")
	runGit(t, worktree, "switch", "-c", "main")
	writeReleaseState(t, worktree, "baseline\n")
	runGit(t, worktree, "add", "deploy-state.txt")
	runGit(t, worktree, "commit", "-m", "seed deploy state")
	runGit(t, worktree, "push", "-u", "origin", "main")
	starting := mustGitRev(t, worktree, "HEAD")
	canonicalRemote := forgejoBaseURL + "/acme/deploy.git"
	runGit(t, worktree, "config", "url."+remote+".insteadOf", canonicalRemote)
	runGit(t, worktree, "remote", "set-url", "origin", canonicalRemote)

	candidate := releaseCandidateFromFixture(t)
	candidate.StartingDeployCommit = starting
	candidate.ContentHash = ""
	var err error
	candidate.ContentHash, err = releaseCandidateContentHash(candidate)
	if err != nil {
		t.Fatal(err)
	}
	attempt := releaseAttempt{
		SchemaVersion: releaseSchemaVersion, AttemptID: strings.Repeat("b", 32),
		CandidateID: candidate.CandidateID, CandidateHash: candidate.ContentHash,
		RequestedBy: candidate.From, To: candidate.To, CreatedAt: candidate.CreatedAt,
	}
	prepared := prepareReleaseCommit(t, worktree, candidate, "candidate\n")
	baseline := releaseAttestation{
		ApplicationCommit: releaseCommit("f"), ArtifactDigest: releaseDigest("e"),
		DeployCommit: starting, EvidenceDigest: releaseEvidenceDigest("baseline", starting),
	}
	return releaseGitFixture{
		root: root, remote: remote, baseline: baseline,
		tx: releaseTransaction{Candidate: candidate, Attempt: attempt, Worktree: worktree, PreparedCommit: prepared},
	}
}

func prepareReleaseCommit(t *testing.T, worktree string, candidate releaseCandidate, state string) string {
	t.Helper()
	writeReleaseState(t, worktree, state)
	runGit(t, worktree, "add", "deploy-state.txt")
	message := "deploy immutable application revision\n\n"
	for key, value := range releasePreparedCommitTrailers(candidate) {
		message += key + ": " + value + "\n"
	}
	runGit(t, worktree, "commit", "-m", message)
	return mustGitRev(t, worktree, "HEAD")
}

func prepareForeignReleaseCommit(t *testing.T, fixture releaseGitFixture, push bool) (string, string) {
	t.Helper()
	worktree := filepath.Join(fixture.root, fmt.Sprintf("foreign-%d", time.Now().UnixNano()))
	runGit(t, fixture.root, "clone", fixture.remote, worktree)
	runGit(t, worktree, "config", "user.name", "Foreign Release Fixture")
	runGit(t, worktree, "config", "user.email", "foreign-release@example.invalid")
	runGit(t, worktree, "switch", "main")
	writeReleaseState(t, worktree, "foreign\n")
	runGit(t, worktree, "add", "deploy-state.txt")
	runGit(t, worktree, "commit", "-m", "foreign deploy state")
	oid := mustGitRev(t, worktree, "HEAD")
	if push {
		runGit(t, worktree, "push", "origin", "HEAD:main")
	}
	return worktree, oid
}

func writeReleaseState(t *testing.T, worktree, state string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktree, "deploy-state.txt"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
}

func releaseTestGitOutput(t *testing.T, worktree string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", worktree}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func assertReleaseRemote(t *testing.T, fixture releaseGitFixture, expected string) {
	t.Helper()
	actual, err := releaseRemoteRef(context.Background(), fixture.tx.Worktree, "refs/heads/"+fixture.tx.Candidate.DeployBranch)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("remote branch = %s, want %s", actual, expected)
	}
}

func assertReleaseLock(t *testing.T, fixture releaseGitFixture, expected string) {
	t.Helper()
	actual, err := releaseRemoteRef(context.Background(), fixture.tx.Worktree, releaseTransactionLockRef(fixture.tx.Candidate))
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("remote lock = %s, want %s", actual, expected)
	}
}

func assertReleasePhases(t *testing.T, journal *fixtureReleaseJournal, expected ...string) {
	t.Helper()
	actual := make([]string, 0, len(journal.events))
	for _, event := range journal.events {
		actual = append(actual, event.Phase)
	}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("phases = %v, want %v", actual, expected)
	}
}

func releaseRestartRecords(tx releaseTransaction, baseline releaseAttestation) []releaseArtifactRecord {
	phases := []string{
		releasePhaseAccepted, releasePhaseLockAcquired, releasePhaseStartBound,
		releasePhasePreflightVerified, releasePhasePrepared, releasePhaseApplying,
	}
	records := make([]releaseArtifactRecord, 0, len(phases))
	for index, phase := range phases {
		event := releaseTransactionEvent{
			SchemaVersion: releaseSchemaVersion, CandidateID: tx.Candidate.CandidateID,
			CandidateHash: tx.Candidate.ContentHash, AttemptID: tx.Attempt.AttemptID,
			Actor: tx.Candidate.To, Director: tx.Candidate.From,
			CreatedAt: tx.Candidate.CreatedAt.Add(time.Duration(index) * time.Second),
			Phase:     phase, ReasonCode: "restart-fixture", EvidenceDigest: releaseEvidenceDigest("restart", phase),
			Correlation: releaseCorrelation{ClusterID: tx.Candidate.Correlation.ClusterID,
				WardRunID: tx.Candidate.To, DispatchRequestID: strings.Repeat("c", 32)},
		}
		if phase == releasePhasePreflightVerified {
			event.Attestation = &baseline
			event.EvidenceDigest = baseline.EvidenceDigest
		}
		records = append(records, releaseArtifactRecord{
			ID: strings.Repeat(fmt.Sprintf("%x", index+1), 32), Kind: releaseRecordKindTransaction,
			CreatedAt: event.CreatedAt, Transaction: &event,
		})
	}
	return records
}
