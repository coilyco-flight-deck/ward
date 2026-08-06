package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	osExec "os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

const (
	releaseRecordKindTransaction  = "transaction"
	releasePhaseAccepted          = "accepted"
	releasePhaseLockAcquired      = "lock-acquired"
	releasePhaseStartBound        = "start-bound"
	releasePhasePreflightVerified = "preflight-verified"
	releasePhasePrepared          = "prepared"
	releasePhaseApplying          = "applying"
	releasePhaseApplied           = "applied"
	releasePhaseCandidateVerified = "candidate-verified"
	releasePhasePushing           = "pushing"
	releasePhaseRecovering        = "recovering"
	releasePhaseLockReleased      = "lock-released"
	releasePhaseCleanupNeeded     = "cleanup-needed"
	releasePushAttempts           = 3
	releaseOperationOutputMax     = 64 * 1024
	releaseRecoveryTimeout        = 2 * time.Minute
)

type releaseAttestation struct {
	ApplicationCommit string `json:"application_commit"`
	ArtifactDigest    string `json:"artifact_digest"`
	DeployCommit      string `json:"deploy_commit"`
	EvidenceDigest    string `json:"evidence_digest"`
}

type releaseTransactionEventInput struct {
	CandidateID    string              `json:"candidate_id"`
	AttemptID      string              `json:"attempt_id"`
	Phase          string              `json:"phase"`
	ReasonCode     string              `json:"reason_code"`
	EvidenceDigest string              `json:"evidence_digest"`
	Attestation    *releaseAttestation `json:"attestation,omitempty"`
}

type releaseTransactionEvent struct {
	SchemaVersion  int                 `json:"schema_version"`
	CandidateID    string              `json:"candidate_id"`
	CandidateHash  string              `json:"candidate_hash"`
	AttemptID      string              `json:"attempt_id"`
	Actor          string              `json:"actor"`
	Director       string              `json:"director"`
	CreatedAt      time.Time           `json:"created_at"`
	Phase          string              `json:"phase"`
	ReasonCode     string              `json:"reason_code"`
	EvidenceDigest string              `json:"evidence_digest"`
	Attestation    *releaseAttestation `json:"attestation,omitempty"`
	Correlation    releaseCorrelation  `json:"correlation"`
}

type releaseTransaction struct {
	Candidate      releaseCandidate
	Attempt        releaseAttempt
	Records        []releaseArtifactRecord
	Worktree       string
	PreparedCommit string
}

type releaseTransactionReport struct {
	Result        releaseResultInput
	LockRef       string
	LockOID       string
	LockRetained  bool
	CleanupNeeded bool
}

type releaseTransactionGit interface {
	AcquireLock(context.Context, releaseTransaction) (string, string, bool, error)
	RemoteBranch(context.Context, releaseTransaction) (string, error)
	ValidatePrepared(context.Context, releaseTransaction) error
	PushPrepared(context.Context, releaseTransaction) error
	ReleaseLock(context.Context, releaseTransaction, string, string) error
}

type releaseTransactionOperations interface {
	Apply(context.Context, releaseCandidate, releaseAttestation) (string, error)
	Verify(context.Context, releaseCandidate, string) (releaseAttestation, error)
}

type releaseTransactionJournal interface {
	Record(context.Context, releaseTransactionEventInput) error
}

type releaseTransactionFinalizer interface {
	Finalize(context.Context, releaseResultInput) error
}

func agentReleaseExecuteCommand() *cli.Command {
	return &cli.Command{
		Name:  "execute",
		Usage: "Run the provider-neutral Git CAS transaction for one broker-minted release attempt.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "candidate", Required: true, Usage: "broker-minted candidate id"},
			&cli.StringFlag{Name: "attempt", Required: true, Usage: "broker-minted attempt id"},
			&cli.StringFlag{Name: "worktree", Required: true, Usage: "prepared deploy-repository worktree"},
			&cli.StringFlag{Name: "prepared-commit", Required: true, Usage: "single prepared commit atop the candidate starting revision"},
		},
		Action: runAgentReleaseExecute,
	}
}

func runAgentReleaseExecute(ctx context.Context, c *cli.Command) error {
	candidateID := strings.TrimSpace(c.String("candidate"))
	attemptID := strings.TrimSpace(c.String("attempt"))
	if !dispatchRequestIDPattern.MatchString(candidateID) || !dispatchRequestIDPattern.MatchString(attemptID) {
		return fmt.Errorf("ward agent release execute: candidate and attempt must be broker-minted ids")
	}
	prepared := strings.TrimSpace(c.String("prepared-commit"))
	if !validReleaseCommit(prepared) {
		return fmt.Errorf("ward agent release execute: --prepared-commit must be a full lowercase commit")
	}
	worktree, err := filepath.Abs(strings.TrimSpace(c.String("worktree")))
	if err != nil || strings.TrimSpace(c.String("worktree")) == "" {
		return fmt.Errorf("ward agent release execute: invalid --worktree")
	}
	resp, err := sendAgentReleaseRequest(ctx, dispatchBrokerRequest{Action: dispatchActionReleaseReceive, Release: &releaseBrokerPayload{}})
	if err != nil {
		return err
	}
	candidate, attempt, err := releaseTransactionSelection(resp.ReleaseRecords, candidateID, attemptID)
	if err != nil {
		return fmt.Errorf("ward agent release execute: %w", err)
	}
	tx := releaseTransaction{Candidate: candidate, Attempt: attempt, Records: resp.ReleaseRecords, Worktree: worktree, PreparedCommit: prepared}
	journal := brokerReleaseTransactionJournal{}
	finalizer := brokerReleaseTransactionFinalizer{}
	report, runErr := runReleaseTransaction(ctx, tx, commandReleaseTransactionGit{}, aosguardReleaseOperations{Worktree: worktree}, journal, finalizer)
	if encodeErr := json.NewEncoder(agentCommandWriter(c)).Encode(report); encodeErr != nil && runErr == nil {
		return encodeErr
	}
	if runErr != nil {
		return fmt.Errorf("ward agent release execute: %w", runErr)
	}
	return nil
}

func releaseTransactionSelection(records []releaseArtifactRecord, candidateID, attemptID string) (releaseCandidate, releaseAttempt, error) {
	candidate, attempts, _, err := releaseCandidateState(records, candidateID)
	if err != nil {
		return releaseCandidate{}, releaseAttempt{}, err
	}
	attempt, ok := attempts[attemptID]
	if !ok {
		return releaseCandidate{}, releaseAttempt{}, fmt.Errorf("attempt %s does not belong to candidate %s", attemptID, candidateID)
	}
	return candidate, attempt, nil
}

type brokerReleaseTransactionJournal struct{}

func (brokerReleaseTransactionJournal) Record(ctx context.Context, event releaseTransactionEventInput) error {
	_, err := sendAgentReleaseRequest(ctx, dispatchBrokerRequest{
		Action:  dispatchActionReleaseProgress,
		Release: &releaseBrokerPayload{Transaction: &event},
	})
	return err
}

type brokerReleaseTransactionFinalizer struct{}

func (brokerReleaseTransactionFinalizer) Finalize(ctx context.Context, result releaseResultInput) error {
	_, err := sendAgentReleaseRequest(ctx, dispatchBrokerRequest{
		Action:  dispatchActionReleaseResult,
		Release: &releaseBrokerPayload{Result: &result},
	})
	return err
}

func validateReleaseProgressRequest(req dispatchBrokerRequest) error {
	if req.To != "" || req.After != "" || req.Release.Candidate != nil || req.Release.Result != nil ||
		req.Release.CandidateID != "" || req.Release.StartingDeployCommit != "" || req.Release.Transaction == nil {
		return fmt.Errorf("dispatch broker: release-progress payload shape is invalid")
	}
	return validateReleaseTransactionEventInput(*req.Release.Transaction)
}

func validateReleaseTransactionEventInput(event releaseTransactionEventInput) error {
	if !dispatchRequestIDPattern.MatchString(event.CandidateID) || !dispatchRequestIDPattern.MatchString(event.AttemptID) {
		return fmt.Errorf("dispatch broker: release transaction requires broker-minted candidate and attempt ids")
	}
	if !validReleaseTransactionPhase(event.Phase) || !releaseSymbolPattern.MatchString(event.ReasonCode) || !validApprovalDigest(event.EvidenceDigest) {
		return fmt.Errorf("dispatch broker: release transaction phase, reason, or evidence digest is invalid")
	}
	if event.Attestation != nil {
		return validateReleaseAttestation(*event.Attestation)
	}
	return nil
}

func validReleaseTransactionPhase(phase string) bool {
	switch phase {
	case releasePhaseAccepted, releasePhaseLockAcquired, releasePhaseStartBound,
		releasePhasePreflightVerified, releasePhasePrepared, releasePhaseApplying,
		releasePhaseApplied, releasePhaseCandidateVerified, releasePhasePushing,
		releasePhaseRecovering, releaseOutcomeRestored, releaseOutcomeVerified,
		releaseOutcomeRejected, releaseOutcomeBlocked, releaseOutcomeIndeterminate,
		releasePhaseLockReleased, releasePhaseCleanupNeeded:
		return true
	default:
		return false
	}
}

func validateReleaseAttestation(attestation releaseAttestation) error {
	if !validReleaseCommit(attestation.ApplicationCommit) || !validApprovalDigest(attestation.ArtifactDigest) ||
		!validReleaseCommit(attestation.DeployCommit) || !validApprovalDigest(attestation.EvidenceDigest) {
		return fmt.Errorf("dispatch broker: release attestation requires immutable application, artifact, deploy, and evidence identities")
	}
	return releaseValuesAreSecretSafe(attestation)
}

func recordReleaseTransactionEvent(req dispatchBrokerRequest) ([]releaseArtifactRecord, error) {
	if req.AuthenticatedRole != releaseOpsRole {
		return nil, fmt.Errorf("dispatch broker: only an authenticated Ops peer may record release transaction progress")
	}
	input := *req.Release.Transaction
	records, admission, err := releaseRecordsForCandidate(req.BrokerID, input.CandidateID)
	if err != nil {
		return nil, err
	}
	candidate, attempts, _, err := releaseCandidateState(records, input.CandidateID)
	if err != nil {
		return nil, err
	}
	if candidate.To != req.Requester || admission.PeerID != req.Requester {
		return nil, fmt.Errorf("dispatch broker: release candidate is addressed to Ops peer %s", candidate.To)
	}
	if _, ok := attempts[input.AttemptID]; !ok {
		return nil, fmt.Errorf("dispatch broker: release attempt %s does not belong to candidate %s", input.AttemptID, input.CandidateID)
	}
	prior := releaseTransactionEvents(records, input.AttemptID)
	if existing := matchingReleaseTransactionEventRecord(records, input); existing != nil {
		return []releaseArtifactRecord{*existing}, nil
	}
	if err := validateReleasePhaseTransition(prior, input.Phase); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	event := releaseTransactionEvent{
		SchemaVersion: releaseSchemaVersion, CandidateID: candidate.CandidateID,
		CandidateHash: candidate.ContentHash, AttemptID: input.AttemptID,
		Actor: req.Requester, Director: candidate.From, CreatedAt: now,
		Phase: input.Phase, ReasonCode: input.ReasonCode,
		EvidenceDigest: input.EvidenceDigest, Attestation: input.Attestation,
		Correlation: releaseCorrelation{ClusterID: req.BrokerID, WardRunID: req.Requester, DispatchRequestID: admission.RequestID},
	}
	if err := validateReleaseTransactionAttestation(event, candidate); err != nil {
		return nil, err
	}
	record := releaseArtifactRecord{ID: newDispatchBrokerRequestID(), Kind: releaseRecordKindTransaction, CreatedAt: now, Transaction: &event}
	if err := appendReleaseArtifactRecord(admission, record); err != nil {
		return nil, err
	}
	return []releaseArtifactRecord{record}, nil
}

func releaseTransactionEvents(records []releaseArtifactRecord, attemptID string) []releaseTransactionEvent {
	var events []releaseTransactionEvent
	for _, record := range records {
		if record.Transaction != nil && record.Transaction.AttemptID == attemptID {
			events = append(events, *record.Transaction)
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt.Before(events[j].CreatedAt) })
	return events
}

func validateReleaseTransactionHistory(candidate releaseCandidate, attempts map[string]releaseAttempt, records []releaseArtifactRecord) error {
	byAttempt := make(map[string][]releaseTransactionEvent)
	for _, record := range records {
		if record.Transaction == nil || record.Transaction.CandidateID != candidate.CandidateID {
			continue
		}
		event := *record.Transaction
		if err := validateReleaseTransactionEvent(event, candidate, attempts); err != nil {
			return err
		}
		byAttempt[event.AttemptID] = append(byAttempt[event.AttemptID], event)
	}
	for _, events := range byAttempt {
		sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt.Before(events[j].CreatedAt) })
		var prior []releaseTransactionEvent
		for _, event := range events {
			if err := validateReleasePhaseTransition(prior, event.Phase); err != nil {
				return err
			}
			prior = append(prior, event)
		}
	}
	return nil
}

func validateReleaseTransactionEvent(event releaseTransactionEvent, candidate releaseCandidate, attempts map[string]releaseAttempt) error {
	if event.SchemaVersion != releaseSchemaVersion || event.CreatedAt.IsZero() ||
		event.CandidateID != candidate.CandidateID || event.CandidateHash != candidate.ContentHash ||
		event.Actor != candidate.To || event.Director != candidate.From {
		return fmt.Errorf("dispatch broker: release transaction event does not match its immutable candidate")
	}
	if _, ok := attempts[event.AttemptID]; !ok {
		return fmt.Errorf("dispatch broker: release transaction event references unknown attempt %s", event.AttemptID)
	}
	if event.Correlation.ClusterID != candidate.Correlation.ClusterID || event.Correlation.WardRunID != candidate.To ||
		!dispatchRequestIDPattern.MatchString(event.Correlation.DispatchRequestID) {
		return fmt.Errorf("dispatch broker: release transaction event identity or Ward provenance is invalid")
	}
	if err := validateReleaseTransactionEventInput(releaseTransactionEventInput{
		CandidateID: event.CandidateID, AttemptID: event.AttemptID, Phase: event.Phase,
		ReasonCode: event.ReasonCode, EvidenceDigest: event.EvidenceDigest, Attestation: event.Attestation,
	}); err != nil {
		return err
	}
	return validateReleaseTransactionAttestation(event, candidate)
}

func validateReleaseTransactionAttestation(event releaseTransactionEvent, candidate releaseCandidate) error {
	requiresAttestation := event.Phase == releasePhasePreflightVerified || event.Phase == releasePhaseCandidateVerified ||
		event.Phase == releaseOutcomeVerified || event.Phase == releaseOutcomeRestored
	if requiresAttestation != (event.Attestation != nil) {
		return fmt.Errorf("dispatch broker: release transaction phase %s has an invalid attestation shape", event.Phase)
	}
	if event.Attestation == nil {
		return nil
	}
	attestation := *event.Attestation
	switch event.Phase {
	case releasePhasePreflightVerified, releaseOutcomeRestored:
		if attestation.DeployCommit != candidate.StartingDeployCommit {
			return fmt.Errorf("dispatch broker: release transaction baseline attestation does not match the starting commit")
		}
	case releasePhaseCandidateVerified, releaseOutcomeVerified:
		if attestation.ApplicationCommit != candidate.ApplicationCommit || attestation.ArtifactDigest != candidate.ArtifactDigest {
			return fmt.Errorf("dispatch broker: release transaction candidate attestation does not match the immutable application")
		}
	}
	return nil
}

func matchingReleaseTransactionEventRecord(records []releaseArtifactRecord, input releaseTransactionEventInput) *releaseArtifactRecord {
	for i := len(records) - 1; i >= 0; i-- {
		event := records[i].Transaction
		if event == nil || event.AttemptID != input.AttemptID || event.Phase != input.Phase ||
			event.ReasonCode != input.ReasonCode || event.EvidenceDigest != input.EvidenceDigest || !releaseAttestationsEqual(event.Attestation, input.Attestation) {
			continue
		}
		return &records[i]
	}
	return nil
}

func releaseAttestationsEqual(left, right *releaseAttestation) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateReleasePhaseTransition(prior []releaseTransactionEvent, next string) error {
	previous := ""
	if len(prior) > 0 {
		previous = prior[len(prior)-1].Phase
	}
	allowed := releasePhaseTransitions()[previous]
	if !allowed[next] {
		return fmt.Errorf("dispatch broker: release transaction phase %q cannot follow %q", next, previous)
	}
	return nil
}

func releasePhaseTransitions() map[string]map[string]bool {
	return map[string]map[string]bool{
		"":                            {releasePhaseAccepted: true},
		releasePhaseAccepted:          {releasePhaseLockAcquired: true, releaseOutcomeBlocked: true},
		releasePhaseLockAcquired:      {releasePhaseStartBound: true, releaseOutcomeBlocked: true, releaseOutcomeRejected: true, releaseOutcomeIndeterminate: true},
		releasePhaseStartBound:        {releasePhasePreflightVerified: true, releaseOutcomeRejected: true, releaseOutcomeBlocked: true, releaseOutcomeIndeterminate: true},
		releasePhasePreflightVerified: {releasePhasePrepared: true, releaseOutcomeRejected: true, releaseOutcomeIndeterminate: true},
		releasePhasePrepared:          {releasePhaseApplying: true, releaseOutcomeRejected: true, releaseOutcomeIndeterminate: true},
		releasePhaseApplying:          {releasePhaseApplied: true, releasePhaseRecovering: true, releaseOutcomeVerified: true, releaseOutcomeIndeterminate: true},
		releasePhaseApplied:           {releasePhaseCandidateVerified: true, releasePhaseRecovering: true, releaseOutcomeVerified: true, releaseOutcomeIndeterminate: true},
		releasePhaseCandidateVerified: {releasePhasePushing: true, releasePhaseRecovering: true, releaseOutcomeVerified: true, releaseOutcomeIndeterminate: true},
		releasePhasePushing:           {releaseOutcomeVerified: true, releasePhaseRecovering: true, releaseOutcomeIndeterminate: true},
		releasePhaseRecovering:        {releasePhaseRecovering: true, releaseOutcomeVerified: true, releaseOutcomeRestored: true, releaseOutcomeIndeterminate: true},
		releaseOutcomeVerified:        {releasePhaseLockReleased: true, releasePhaseCleanupNeeded: true},
		releaseOutcomeRestored:        {releasePhaseLockReleased: true, releasePhaseCleanupNeeded: true},
		releaseOutcomeRejected:        {releasePhaseLockReleased: true, releasePhaseCleanupNeeded: true},
		releaseOutcomeBlocked:         {releasePhaseLockReleased: true, releasePhaseCleanupNeeded: true},
		releasePhaseCleanupNeeded:     {releasePhaseCleanupNeeded: true, releasePhaseLockReleased: true},
	}
}

func runReleaseTransaction(ctx context.Context, tx releaseTransaction, git releaseTransactionGit, operations releaseTransactionOperations, journal releaseTransactionJournal, finalizer releaseTransactionFinalizer) (releaseTransactionReport, error) { //nolint:gocyclo,cyclop,gocognit,funlen // the explicit transaction order is the safety contract
	if err := validateReleaseTransaction(tx); err != nil {
		return releaseTransactionReport{}, err
	}
	if report, done, err := resumeTerminalRelease(ctx, tx, git, journal); done {
		return report, err
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseAccepted, "transaction-accepted", nil); err != nil {
		return releaseTransactionReport{}, err
	}
	lockRef, lockOID, conflict, err := git.AcquireLock(ctx, tx)
	if err != nil {
		if lockOID != "" {
			return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "lock-state-unreadable", releaseEvidenceDigest("lock-state-unreadable", lockRef), nil)
		}
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, "", "", releaseOutcomeBlocked, "lock-unavailable", releaseEvidenceDigest("lock-unavailable", tx.Candidate.CandidateID), nil)
	}
	if conflict {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, "", releaseOutcomeBlocked, "environment-locked", releaseEvidenceDigest("environment-locked", lockRef), nil)
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseLockAcquired, "lock-acquired", nil); err != nil {
		_ = git.ReleaseLock(context.Background(), tx, lockRef, lockOID)
		return releaseTransactionReport{}, err
	}
	remote, err := git.RemoteBranch(ctx, tx)
	if err != nil {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeBlocked, "deploy-branch-unreadable", releaseEvidenceDigest("deploy-branch-unreadable", lockRef), nil)
	}
	if remote != tx.Candidate.StartingDeployCommit {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeRejected, "stale-starting-revision", releaseEvidenceDigest("stale-starting-revision", remote), nil)
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseStartBound, "starting-revision-bound", nil); err != nil {
		_ = git.ReleaseLock(context.Background(), tx, lockRef, lockOID)
		return releaseTransactionReport{}, err
	}
	events := releaseTransactionEvents(tx.Records, tx.Attempt.AttemptID)
	if releaseMutationStarted(events) {
		return resumeMutatedRelease(ctx, tx, git, operations, journal, finalizer, lockRef, lockOID, events)
	}
	baseline, err := operations.Verify(ctx, tx.Candidate, tx.Candidate.StartingDeployCommit)
	if err != nil || validateStartingAttestation(baseline, tx.Candidate) != nil {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "preflight-drift", releaseEvidenceDigest("preflight-drift", tx.Candidate.StartingDeployCommit), nil)
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhasePreflightVerified, "starting-state-verified", &baseline); err != nil {
		_ = git.ReleaseLock(context.Background(), tx, lockRef, lockOID)
		return releaseTransactionReport{}, err
	}
	if err := git.ValidatePrepared(ctx, tx); err != nil {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeRejected, "prepared-commit-invalid", releaseEvidenceDigest("prepared-commit-invalid", tx.PreparedCommit), nil)
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhasePrepared, "prepared-commit-validated", nil); err != nil {
		_ = git.ReleaseLock(context.Background(), tx, lockRef, lockOID)
		return releaseTransactionReport{}, err
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseApplying, "candidate-apply-started", nil); err != nil {
		_ = git.ReleaseLock(context.Background(), tx, lockRef, lockOID)
		return releaseTransactionReport{}, err
	}
	desired := candidateReleaseAttestation(tx)
	applyEvidence, applyErr := operations.Apply(ctx, tx.Candidate, desired)
	if applyErr != nil || ctx.Err() != nil {
		return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "apply-failed", applyEvidence)
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseApplied, "candidate-applied", nil); err != nil {
		return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "journal-failed-after-apply", releaseEvidenceDigest("journal-failed-after-apply"))
	}
	attestation, verifyErr := operations.Verify(ctx, tx.Candidate, tx.PreparedCommit)
	if verifyErr != nil || validateCandidateAttestation(attestation, tx) != nil {
		return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "candidate-verification-failed", releaseEvidenceDigest("candidate-verification-failed"))
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseCandidateVerified, "candidate-state-verified", &attestation); err != nil {
		return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "journal-failed-after-verification", attestation.EvidenceDigest)
	}
	if err := recordReleasePhase(ctx, journal, tx, releasePhasePushing, "deploy-commit-push-started", nil); err != nil {
		return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "journal-failed-before-push", attestation.EvidenceDigest)
	}
	return pushVerifiedRelease(ctx, tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, attestation)
}

func validateReleaseTransaction(tx releaseTransaction) error {
	if err := validateReleaseCandidate(tx.Candidate); err != nil {
		return err
	}
	if err := validateReleaseAttempt(tx.Attempt, tx.Candidate); err != nil {
		return err
	}
	if !validReleaseCommit(tx.PreparedCommit) || strings.TrimSpace(tx.Worktree) == "" {
		return fmt.Errorf("release transaction requires a prepared commit and worktree")
	}
	return nil
}

func recordReleasePhase(ctx context.Context, journal releaseTransactionJournal, tx releaseTransaction, phase, reason string, attestation *releaseAttestation) error {
	evidence := releaseEvidenceDigest(tx.Candidate.CandidateID, tx.Attempt.AttemptID, phase, reason)
	if attestation != nil {
		evidence = attestation.EvidenceDigest
	}
	return journal.Record(ctx, releaseTransactionEventInput{
		CandidateID: tx.Candidate.CandidateID, AttemptID: tx.Attempt.AttemptID,
		Phase: phase, ReasonCode: reason, EvidenceDigest: evidence, Attestation: attestation,
	})
}

func pushVerifiedRelease(ctx context.Context, tx releaseTransaction, git releaseTransactionGit, operations releaseTransactionOperations, journal releaseTransactionJournal, finalizer releaseTransactionFinalizer, lockRef, lockOID string, baseline, attestation releaseAttestation) (releaseTransactionReport, error) {
	for attempt := 0; attempt < releasePushAttempts; attempt++ {
		pushErr := git.PushPrepared(ctx, tx)
		remote, readErr := git.RemoteBranch(ctx, tx)
		if readErr != nil {
			return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "push-state-unreadable", releaseEvidenceDigest("push-state-unreadable"), nil)
		}
		if remote == tx.PreparedCommit {
			return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeVerified, "candidate-live-and-recorded", attestation.EvidenceDigest, &attestation)
		}
		if remote != tx.Candidate.StartingDeployCommit {
			return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "push-cas-diverged", releaseEvidenceDigest("push-cas-diverged", remote), nil)
		}
		if pushErr == nil {
			continue
		}
	}
	return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "push-retries-exhausted", attestation.EvidenceDigest)
}

func recoverReleaseTransaction(tx releaseTransaction, git releaseTransactionGit, operations releaseTransactionOperations, journal releaseTransactionJournal, finalizer releaseTransactionFinalizer, lockRef, lockOID string, baseline releaseAttestation, reason, evidence string) (releaseTransactionReport, error) {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), releaseRecoveryTimeout)
	defer cancel()
	if evidence == "" {
		evidence = releaseEvidenceDigest(reason)
	}
	if err := recordReleasePhase(recoveryCtx, journal, tx, releasePhaseRecovering, reason, nil); err != nil {
		return releaseTransactionReport{LockRef: lockRef, LockOID: lockOID, LockRetained: true}, err
	}
	if _, err := operations.Apply(recoveryCtx, tx.Candidate, baseline); err != nil {
		return completeReleaseTransaction(recoveryCtx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "restore-apply-failed", releaseEvidenceDigest("restore-apply-failed", evidence), nil)
	}
	restored, err := operations.Verify(recoveryCtx, tx.Candidate, tx.Candidate.StartingDeployCommit)
	if err != nil || !releaseAttestationEqual(restored, baseline) {
		return completeReleaseTransaction(recoveryCtx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "restore-verification-failed", releaseEvidenceDigest("restore-verification-failed", evidence), nil)
	}
	return completeReleaseTransaction(recoveryCtx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeRestored, "starting-state-restored", restored.EvidenceDigest, &restored)
}

func completeReleaseTransaction(ctx context.Context, tx releaseTransaction, git releaseTransactionGit, journal releaseTransactionJournal, finalizer releaseTransactionFinalizer, lockRef, lockOID, classification, reason, evidence string, attestation *releaseAttestation) (releaseTransactionReport, error) {
	if evidence == "" {
		evidence = releaseEvidenceDigest(classification, reason)
	}
	result := releaseResultInput{
		CandidateID: tx.Candidate.CandidateID, AttemptID: tx.Attempt.AttemptID,
		Classification: classification, ReasonCode: reason, EvidenceDigests: []string{evidence},
	}
	if classification == releaseOutcomeVerified {
		result.DeployCommit = tx.PreparedCommit
	}
	if classification == releaseOutcomeRestored {
		result.RestoredCommit = tx.Candidate.StartingDeployCommit
	}
	report := releaseTransactionReport{Result: result, LockRef: lockRef, LockOID: lockOID, LockRetained: lockOID != ""}
	if err := recordReleasePhase(ctx, journal, tx, classification, reason, attestation); err != nil {
		return report, err
	}
	if err := finalizer.Finalize(ctx, result); err != nil {
		return report, err
	}
	if classification == releaseOutcomeIndeterminate || lockOID == "" {
		return report, nil
	}
	if err := git.ReleaseLock(ctx, tx, lockRef, lockOID); err != nil {
		report.CleanupNeeded = true
		if journalErr := recordReleasePhase(ctx, journal, tx, releasePhaseCleanupNeeded, "lock-release-failed", nil); journalErr != nil {
			return report, fmt.Errorf("release lock cleanup and journal failed")
		}
		return report, fmt.Errorf("release lock cleanup failed")
	}
	report.LockRetained = false
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseLockReleased, "lock-released", nil); err != nil {
		return report, err
	}
	return report, nil
}

func resumeTerminalRelease(ctx context.Context, tx releaseTransaction, git releaseTransactionGit, journal releaseTransactionJournal) (releaseTransactionReport, bool, error) {
	result, ok := releaseResultForAttempt(tx.Records, tx.Attempt.AttemptID)
	if !ok {
		return releaseTransactionReport{}, false, nil
	}
	report := releaseTransactionReport{Result: releaseResultInput{
		CandidateID: result.CandidateID, AttemptID: result.AttemptID,
		Classification: result.Classification, ReasonCode: result.ReasonCode,
		EvidenceDigests: result.EvidenceDigests, DeployCommit: result.DeployCommit, RestoredCommit: result.RestoredCommit,
	}, LockRetained: result.Classification == releaseOutcomeIndeterminate}
	if result.Classification == releaseOutcomeIndeterminate || releaseAttemptHasPhase(tx.Records, tx.Attempt.AttemptID, releasePhaseLockReleased) {
		return report, true, nil
	}
	if !releaseAttemptHasPhase(tx.Records, tx.Attempt.AttemptID, releasePhaseLockAcquired) {
		return report, true, nil
	}
	lockRef, lockOID, conflict, err := git.AcquireLock(ctx, tx)
	if err != nil {
		return report, true, err
	}
	if conflict {
		report.CleanupNeeded = true
		return report, true, fmt.Errorf("release lock is owned by another attempt")
	}
	report.LockRef, report.LockOID, report.LockRetained = lockRef, lockOID, true
	if err := git.ReleaseLock(ctx, tx, lockRef, lockOID); err != nil {
		report.CleanupNeeded = true
		_ = recordReleasePhase(ctx, journal, tx, releasePhaseCleanupNeeded, "lock-release-failed", nil)
		return report, true, err
	}
	report.LockRetained = false
	if err := recordReleasePhase(ctx, journal, tx, releasePhaseLockReleased, "lock-released", nil); err != nil {
		return report, true, err
	}
	return report, true, nil
}

func resumeMutatedRelease(ctx context.Context, tx releaseTransaction, git releaseTransactionGit, operations releaseTransactionOperations, journal releaseTransactionJournal, finalizer releaseTransactionFinalizer, lockRef, lockOID string, events []releaseTransactionEvent) (releaseTransactionReport, error) {
	baseline, ok := releasePhaseAttestation(events, releasePhasePreflightVerified)
	if !ok {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "resume-baseline-missing", releaseEvidenceDigest("resume-baseline-missing"), nil)
	}
	remote, err := git.RemoteBranch(ctx, tx)
	if err != nil {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "resume-remote-unreadable", releaseEvidenceDigest("resume-remote-unreadable"), nil)
	}
	if remote == tx.PreparedCommit {
		attestation, verifyErr := operations.Verify(ctx, tx.Candidate, tx.PreparedCommit)
		if verifyErr != nil || validateCandidateAttestation(attestation, tx) != nil {
			return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "resume-candidate-unverified", releaseEvidenceDigest("resume-candidate-unverified"), nil)
		}
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeVerified, "candidate-live-and-recorded", attestation.EvidenceDigest, &attestation)
	}
	if remote != tx.Candidate.StartingDeployCommit {
		return completeReleaseTransaction(ctx, tx, git, journal, finalizer, lockRef, lockOID, releaseOutcomeIndeterminate, "resume-deploy-state-diverged", releaseEvidenceDigest("resume-deploy-state-diverged", remote), nil)
	}
	return recoverReleaseTransaction(tx, git, operations, journal, finalizer, lockRef, lockOID, baseline, "resumed-after-mutation", releaseEvidenceDigest("resumed-after-mutation"))
}

func releaseResultForAttempt(records []releaseArtifactRecord, attemptID string) (releaseResult, bool) {
	for _, record := range records {
		if record.Result != nil && record.Result.AttemptID == attemptID {
			return *record.Result, true
		}
	}
	return releaseResult{}, false
}

func validateReleaseResultTransaction(records []releaseArtifactRecord, input releaseResultInput) error {
	if input.Classification != releaseOutcomeVerified && input.Classification != releaseOutcomeRestored {
		return nil
	}
	events := releaseTransactionEvents(records, input.AttemptID)
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if event.Phase != input.Classification {
			continue
		}
		if event.Attestation == nil || event.ReasonCode != input.ReasonCode ||
			!stringSliceContains(input.EvidenceDigests, event.EvidenceDigest) {
			break
		}
		if input.Classification == releaseOutcomeVerified && event.Attestation.DeployCommit != input.DeployCommit {
			break
		}
		if input.Classification == releaseOutcomeRestored && event.Attestation.DeployCommit != input.RestoredCommit {
			break
		}
		return nil
	}
	return fmt.Errorf("dispatch broker: %s result requires its matching transaction outcome and attestation", input.Classification)
}

func releaseAttemptHasPhase(records []releaseArtifactRecord, attemptID, phase string) bool {
	for _, record := range records {
		if record.Transaction != nil && record.Transaction.AttemptID == attemptID && record.Transaction.Phase == phase {
			return true
		}
	}
	return false
}

func releaseMutationStarted(events []releaseTransactionEvent) bool {
	for _, event := range events {
		switch event.Phase {
		case releasePhaseApplying, releasePhaseApplied, releasePhaseCandidateVerified, releasePhasePushing, releasePhaseRecovering:
			return true
		}
	}
	return false
}

func releasePhaseAttestation(events []releaseTransactionEvent, phase string) (releaseAttestation, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Phase == phase && events[i].Attestation != nil {
			return *events[i].Attestation, true
		}
	}
	return releaseAttestation{}, false
}

func validateStartingAttestation(attestation releaseAttestation, candidate releaseCandidate) error {
	if err := validateReleaseAttestation(attestation); err != nil {
		return err
	}
	if attestation.DeployCommit != candidate.StartingDeployCommit {
		return fmt.Errorf("starting environment attestation does not match the starting deploy commit")
	}
	return nil
}

func validateCandidateAttestation(attestation releaseAttestation, tx releaseTransaction) error {
	if err := validateReleaseAttestation(attestation); err != nil {
		return err
	}
	if attestation.ApplicationCommit != tx.Candidate.ApplicationCommit || attestation.ArtifactDigest != tx.Candidate.ArtifactDigest || attestation.DeployCommit != tx.PreparedCommit {
		return fmt.Errorf("candidate environment attestation does not match the immutable release")
	}
	return nil
}

func candidateReleaseAttestation(tx releaseTransaction) releaseAttestation {
	return releaseAttestation{
		ApplicationCommit: tx.Candidate.ApplicationCommit,
		ArtifactDigest:    tx.Candidate.ArtifactDigest,
		DeployCommit:      tx.PreparedCommit,
		EvidenceDigest:    releaseEvidenceDigest("candidate-desired", tx.Candidate.ContentHash, tx.PreparedCommit),
	}
}

func releaseAttestationEqual(left, right releaseAttestation) bool {
	return left.ApplicationCommit == right.ApplicationCommit && left.ArtifactDigest == right.ArtifactDigest && left.DeployCommit == right.DeployCommit
}

func releaseEvidenceDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(digest[:])
}

type commandReleaseTransactionGit struct{}

func (commandReleaseTransactionGit) AcquireLock(ctx context.Context, tx releaseTransaction) (string, string, bool, error) {
	lockRef := releaseTransactionLockRef(tx.Candidate)
	lockBody := strings.Join([]string{"ward-release-lock-v1", tx.Candidate.CandidateID, tx.Attempt.AttemptID, tx.Candidate.ContentHash}, "\n") + "\n"
	lockOID, err := releaseGitCaptureInput(ctx, tx.Worktree, lockBody, "hash-object", "-w", "--stdin")
	if err != nil || !validReleaseCommit(lockOID) {
		return lockRef, "", false, fmt.Errorf("create release lock object")
	}
	existing, err := releaseRemoteRef(ctx, tx.Worktree, lockRef)
	if err != nil {
		return lockRef, lockOID, false, err
	}
	if existing == lockOID {
		return lockRef, lockOID, false, nil
	}
	if existing != "" {
		return lockRef, lockOID, true, nil
	}
	if err := releaseGitRun(ctx, tx.Worktree, "push", "--porcelain", "origin", lockOID+":"+lockRef); err != nil {
		existing, readErr := releaseRemoteRef(ctx, tx.Worktree, lockRef)
		if readErr != nil {
			return lockRef, lockOID, false, readErr
		}
		if existing == lockOID {
			return lockRef, lockOID, false, nil
		}
		if existing != "" {
			return lockRef, lockOID, true, nil
		}
		return lockRef, "", false, fmt.Errorf("create remote release lock")
	}
	return lockRef, lockOID, false, nil
}

func (commandReleaseTransactionGit) RemoteBranch(ctx context.Context, tx releaseTransaction) (string, error) {
	return releaseRemoteRef(ctx, tx.Worktree, "refs/heads/"+tx.Candidate.DeployBranch)
}

func (commandReleaseTransactionGit) ValidatePrepared(ctx context.Context, tx releaseTransaction) error {
	if err := validateReleaseDeployOrigin(ctx, tx); err != nil {
		return fmt.Errorf("prepared deploy worktree origin does not match the candidate: %w", err)
	}
	if err := validateReleasePreparedWorktree(ctx, tx); err != nil {
		return err
	}
	if err := validateReleasePreparedCommitShape(ctx, tx); err != nil {
		return err
	}
	return validateReleasePreparedProvenance(ctx, tx)
}

func validateReleasePreparedWorktree(ctx context.Context, tx releaseTransaction) error {
	status, err := releaseGitCapture(ctx, tx.Worktree, "status", "--porcelain")
	if err != nil || status != "" {
		return fmt.Errorf("prepared deploy worktree is not clean")
	}
	head, err := releaseGitCapture(ctx, tx.Worktree, "rev-parse", "HEAD")
	if err != nil || head != tx.PreparedCommit {
		return fmt.Errorf("prepared deploy worktree is not at the prepared commit")
	}
	return nil
}

func validateReleasePreparedCommitShape(ctx context.Context, tx releaseTransaction) error {
	parents, err := releaseGitCapture(ctx, tx.Worktree, "rev-list", "--parents", "-n", "1", tx.PreparedCommit)
	if err != nil {
		return fmt.Errorf("read prepared deploy commit parents")
	}
	parts := strings.Fields(parents)
	if len(parts) != 2 || parts[0] != tx.PreparedCommit || parts[1] != tx.Candidate.StartingDeployCommit {
		return fmt.Errorf("prepared deploy commit must be exactly one commit atop the candidate starting revision")
	}
	changed, err := releaseGitCapture(ctx, tx.Worktree, "diff-tree", "--no-commit-id", "--name-only", "-r", tx.PreparedCommit)
	if err != nil || strings.TrimSpace(changed) == "" {
		return fmt.Errorf("prepared deploy commit must contain a deploy-state change")
	}
	return nil
}

func validateReleasePreparedProvenance(ctx context.Context, tx releaseTransaction) error {
	message, err := releaseGitCapture(ctx, tx.Worktree, "show", "-s", "--format=%B", tx.PreparedCommit)
	if err != nil {
		return fmt.Errorf("read prepared deploy commit provenance")
	}
	for key, expected := range releasePreparedCommitTrailers(tx.Candidate) {
		if !commitMessageHasExactTrailer(message, key, expected) {
			return fmt.Errorf("prepared deploy commit is missing exact %s provenance", key)
		}
	}
	return nil
}

func validateReleaseDeployOrigin(ctx context.Context, tx releaseTransaction) error {
	server, err := url.Parse(forgejoBaseURL)
	if err != nil || (server.Scheme != "http" && server.Scheme != "https") || server.Hostname() == "" {
		return fmt.Errorf("configured Forgejo base is not an absolute HTTP(S) URL")
	}
	origin, err := releaseGitCapture(ctx, tx.Worktree, "config", "--get", "remote.origin.url")
	if err != nil {
		return fmt.Errorf("read raw origin remote")
	}
	originHost, originPath, err := splitGitRemote(origin)
	if err != nil {
		return err
	}
	if !strings.EqualFold(server.Hostname(), originHost) {
		return fmt.Errorf("origin host does not match Ward's canonical Forgejo")
	}
	if originPath != cleanRepoPath(path.Join(server.Path, tx.Candidate.DeployRepository)) {
		return fmt.Errorf("origin repository does not match deploy_repository")
	}
	return nil
}

func (commandReleaseTransactionGit) PushPrepared(ctx context.Context, tx releaseTransaction) error {
	branchRef := "refs/heads/" + tx.Candidate.DeployBranch
	lease := "--force-with-lease=" + branchRef + ":" + tx.Candidate.StartingDeployCommit
	return releaseGitRun(ctx, tx.Worktree, "push", "--porcelain", lease, "origin", tx.PreparedCommit+":"+branchRef)
}

func (commandReleaseTransactionGit) ReleaseLock(ctx context.Context, tx releaseTransaction, lockRef, lockOID string) error {
	existing, err := releaseRemoteRef(ctx, tx.Worktree, lockRef)
	if err != nil {
		return err
	}
	if existing == "" {
		return nil
	}
	if existing != lockOID {
		return fmt.Errorf("release lock ownership changed")
	}
	lease := "--force-with-lease=" + lockRef + ":" + lockOID
	return releaseGitRun(ctx, tx.Worktree, "push", "--porcelain", lease, "origin", ":"+lockRef)
}

func releaseTransactionLockRef(candidate releaseCandidate) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{candidate.DeployRepository, candidate.DeployBranch, candidate.Environment}, "\x00")))
	return "refs/ward/release-locks/" + hex.EncodeToString(digest[:])
}

func releaseRemoteRef(ctx context.Context, worktree, ref string) (string, error) {
	out, err := releaseGitCapture(ctx, worktree, "ls-remote", "--refs", "origin", ref)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != ref || !validReleaseCommit(fields[0]) {
		return "", fmt.Errorf("read remote ref %s", ref)
	}
	return fields[0], nil
}

func releasePreparedCommitTrailers(candidate releaseCandidate) map[string]string {
	return map[string]string{
		"Ward-Application-Revision": candidate.ApplicationCommit,
		"Ward-Artifact-Digest":      candidate.ArtifactDigest,
		"Ward-Environment":          candidate.Environment,
		"Ward-Run-ID":               candidate.Correlation.WardRunID,
		"Ward-Originating-Ticket":   candidate.OriginatingTicket,
		"Ward-Release-Candidate":    candidate.CandidateID,
	}
}

func commitMessageHasExactTrailer(message, key, value string) bool {
	want := key + ": " + value
	for _, line := range strings.Split(strings.TrimSpace(message), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func releaseGitCapture(ctx context.Context, worktree string, args ...string) (string, error) {
	return releaseGitCaptureInput(ctx, worktree, "", args...)
}

func releaseGitCaptureInput(ctx context.Context, worktree, input string, args ...string) (string, error) {
	cmd := osExec.CommandContext(ctx, "git", append([]string{"-C", worktree}, args...)...) // #nosec G204 -- fixed git binary with validated refs and caller-selected worktree
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git release transaction step failed")
	}
	return strings.TrimSpace(stdout.String()), nil
}

func releaseGitRun(ctx context.Context, worktree string, args ...string) error {
	_, err := releaseGitCapture(ctx, worktree, args...)
	return err
}

type aosguardReleaseOperations struct {
	Worktree string
}

func (ops aosguardReleaseOperations) Apply(ctx context.Context, candidate releaseCandidate, desired releaseAttestation) (string, error) {
	stdout, stderr, err := ops.run(ctx, candidate.DeployOperation, candidate, desired)
	evidence := releaseEvidenceDigest("apply", stdout, stderr)
	if err != nil {
		return evidence, fmt.Errorf("guarded deploy operation failed")
	}
	return evidence, nil
}

func (ops aosguardReleaseOperations) Verify(ctx context.Context, candidate releaseCandidate, desiredDeployCommit string) (releaseAttestation, error) {
	desired := releaseAttestation{DeployCommit: desiredDeployCommit}
	stdout, stderr, err := ops.run(ctx, candidate.VerifyOperation, candidate, desired)
	if err != nil {
		return releaseAttestation{}, fmt.Errorf("guarded verify operation failed (%s)", releaseEvidenceDigest("verify-error", stdout, stderr))
	}
	var attestation releaseAttestation
	decoder := json.NewDecoder(strings.NewReader(stdout))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(&attestation); decodeErr != nil {
		return releaseAttestation{}, fmt.Errorf("guarded verify operation returned an invalid typed attestation")
	}
	if decodeErr := decoder.Decode(&struct{}{}); decodeErr != io.EOF {
		return releaseAttestation{}, fmt.Errorf("guarded verify operation returned more than one typed attestation")
	}
	if err := validateReleaseAttestation(attestation); err != nil {
		return releaseAttestation{}, err
	}
	return attestation, nil
}

func (ops aosguardReleaseOperations) run(ctx context.Context, operation string, candidate releaseCandidate, desired releaseAttestation) (string, string, error) {
	area, verbName, err := releaseOperationParts(operation)
	if err != nil {
		return "", "", err
	}
	cmd := osExec.CommandContext(ctx, "aosguard", "ops", area, verbName) // #nosec G204 -- validated symbolic operation id; AOSguard independently authorizes the operation
	cmd.Dir = ops.Worktree
	cmd.Env = append(os.Environ(), releaseOperationEnv(candidate, desired)...)
	stdout := &boundedReleaseBuffer{limit: releaseOperationOutputMax}
	stderr := &boundedReleaseBuffer{limit: releaseOperationOutputMax}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return stdout.String(), stderr.String(), fmt.Errorf("guarded operation output exceeded %d bytes", releaseOperationOutputMax)
	}
	return stdout.String(), stderr.String(), runErr
}

func releaseOperationParts(operation string) (string, string, error) {
	parts := strings.Split(operation, ".")
	if len(parts) != 2 || !releaseOperationComponent(parts[0]) || !releaseOperationComponent(parts[1]) {
		return "", "", fmt.Errorf("release operation %q must be area.verb", operation)
	}
	return parts[0], parts[1], nil
}

func releaseOperationComponent(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' || len(value) > 63 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func releaseOperationEnv(candidate releaseCandidate, desired releaseAttestation) []string {
	return []string{
		"WARD_RELEASE_CANDIDATE_ID=" + candidate.CandidateID,
		"WARD_RELEASE_APPLICATION_REPOSITORY=" + candidate.ApplicationRepository,
		"WARD_RELEASE_APPLICATION_COMMIT=" + firstNonEmpty(desired.ApplicationCommit, candidate.ApplicationCommit),
		"WARD_RELEASE_ARTIFACT_DIGEST=" + firstNonEmpty(desired.ArtifactDigest, candidate.ArtifactDigest),
		"WARD_RELEASE_ENVIRONMENT=" + candidate.Environment,
		"WARD_RELEASE_DEPLOY_REPOSITORY=" + candidate.DeployRepository,
		"WARD_RELEASE_DEPLOY_BRANCH=" + candidate.DeployBranch,
		"WARD_RELEASE_DEPLOY_COMMIT=" + desired.DeployCommit,
		"WARD_RELEASE_ORIGINATING_TICKET=" + candidate.OriginatingTicket,
	}
}

type boundedReleaseBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedReleaseBuffer) Write(body []byte) (int, error) {
	original := len(body)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(body) > remaining {
		body = body[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(body)
	return original, nil
}

func (b *boundedReleaseBuffer) String() string {
	return b.buffer.String()
}
