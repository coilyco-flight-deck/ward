package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
)

const (
	releaseSchemaVersion        = 1
	releaseOpsRole              = "ops"
	releaseArtifactFile         = "release.jsonl"
	releaseArtifactMaxBytes     = 64 * 1024
	releaseEvidenceMax          = 128
	releaseRepositoryMaxBytes   = 255
	releaseRecordKindCandidate  = "candidate"
	releaseRecordKindAttempt    = "attempt"
	releaseRecordKindResult     = "result"
	releaseOutcomeVerified      = "verified"
	releaseOutcomeRejected      = "rejected"
	releaseOutcomeRestored      = "restored"
	releaseOutcomeBlocked       = "blocked"
	releaseOutcomeIndeterminate = "indeterminate"
)

var (
	releaseCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	releaseSymbolPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,127}$`)
	releaseArtifactMu    sync.Mutex
	releaseContractMu    sync.Mutex
)

type releaseCandidateInput struct {
	ApplicationRepository string `json:"application_repository"`
	ApplicationCommit     string `json:"application_commit"`
	ArtifactDigest        string `json:"artifact_digest"`
	Environment           string `json:"environment"`
	DeployRepository      string `json:"deploy_repository"`
	StartingDeployCommit  string `json:"starting_deploy_commit"`
	OriginatingTicket     string `json:"originating_ticket"`
	DeployOperation       string `json:"deploy_operation"`
	VerifyOperation       string `json:"verify_operation"`
}

type releaseCorrelation struct {
	ClusterID         string `json:"cluster_id"`
	WardRunID         string `json:"ward_run_id"`
	DispatchRequestID string `json:"dispatch_request_id,omitempty"`
}

type releaseCandidate struct {
	SchemaVersion         int                `json:"schema_version"`
	CandidateID           string             `json:"candidate_id"`
	ContentHash           string             `json:"content_hash"`
	From                  string             `json:"from"`
	To                    string             `json:"to"`
	CreatedAt             time.Time          `json:"created_at"`
	ApplicationRepository string             `json:"application_repository"`
	ApplicationCommit     string             `json:"application_commit"`
	ArtifactDigest        string             `json:"artifact_digest"`
	Environment           string             `json:"environment"`
	DeployRepository      string             `json:"deploy_repository"`
	StartingDeployCommit  string             `json:"starting_deploy_commit"`
	OriginatingTicket     string             `json:"originating_ticket"`
	DeployOperation       string             `json:"deploy_operation"`
	VerifyOperation       string             `json:"verify_operation"`
	Correlation           releaseCorrelation `json:"correlation"`
}

type releaseAttempt struct {
	SchemaVersion int       `json:"schema_version"`
	AttemptID     string    `json:"attempt_id"`
	CandidateID   string    `json:"candidate_id"`
	CandidateHash string    `json:"candidate_hash"`
	RequestedBy   string    `json:"requested_by"`
	To            string    `json:"to"`
	CreatedAt     time.Time `json:"created_at"`
}

type releaseResultInput struct {
	CandidateID     string   `json:"candidate_id"`
	AttemptID       string   `json:"attempt_id"`
	Classification  string   `json:"classification"`
	ReasonCode      string   `json:"reason_code"`
	EvidenceDigests []string `json:"evidence_digests"`
	DeployCommit    string   `json:"deploy_commit,omitempty"`
	RestoredCommit  string   `json:"restored_commit,omitempty"`
}

type releaseResult struct {
	SchemaVersion   int                `json:"schema_version"`
	CandidateID     string             `json:"candidate_id"`
	CandidateHash   string             `json:"candidate_hash"`
	AttemptID       string             `json:"attempt_id"`
	From            string             `json:"from"`
	To              string             `json:"to"`
	CreatedAt       time.Time          `json:"created_at"`
	Classification  string             `json:"classification"`
	ReasonCode      string             `json:"reason_code"`
	EvidenceDigests []string           `json:"evidence_digests"`
	DeployCommit    string             `json:"deploy_commit,omitempty"`
	RestoredCommit  string             `json:"restored_commit,omitempty"`
	Correlation     releaseCorrelation `json:"correlation"`
}

type releaseArtifactRecord struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	CreatedAt time.Time         `json:"created_at"`
	Candidate *releaseCandidate `json:"candidate,omitempty"`
	Attempt   *releaseAttempt   `json:"attempt,omitempty"`
	Result    *releaseResult    `json:"result,omitempty"`
}

type releaseBrokerPayload struct {
	Candidate            *releaseCandidateInput `json:"candidate,omitempty"`
	Result               *releaseResultInput    `json:"result,omitempty"`
	CandidateID          string                 `json:"candidate_id,omitempty"`
	StartingDeployCommit string                 `json:"starting_deploy_commit,omitempty"`
}

func agentReleaseCommand() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "Exchange a typed, provider-neutral release contract between Director and one Ops peer.",
		Commands: []*cli.Command{
			{
				Name:  "candidate",
				Usage: "Send one immutable release candidate to an exact broker-minted Ops peer.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "to", Required: true, Usage: "exact broker-minted Ops peer id"},
					&cli.StringFlag{Name: "file", Required: true, Usage: "candidate JSON file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					var input releaseCandidateInput
					if err := decodeReleaseFile(c.String("file"), &input); err != nil {
						return fmt.Errorf("ward agent release candidate: %w", err)
					}
					return runAgentReleaseRequest(ctx, c, dispatchBrokerRequest{
						Action:  dispatchActionReleaseCandidate,
						To:      strings.TrimSpace(c.String("to")),
						Release: &releaseBrokerPayload{Candidate: &input},
					})
				},
			},
			{
				Name:  "retry",
				Usage: "Mint a new attempt for an unchanged retryable candidate.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "candidate", Required: true, Usage: "broker-minted candidate id"},
					&cli.StringFlag{Name: "starting-deploy-commit", Required: true, Usage: "currently observed full deploy-repository commit"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return runAgentReleaseRequest(ctx, c, dispatchBrokerRequest{
						Action: dispatchActionReleaseRetry,
						Release: &releaseBrokerPayload{
							CandidateID:          strings.TrimSpace(c.String("candidate")),
							StartingDeployCommit: strings.TrimSpace(c.String("starting-deploy-commit")),
						},
					})
				},
			},
			{
				Name:  "result",
				Usage: "Return one typed terminal result for a release attempt.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Required: true, Usage: "result JSON file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					var input releaseResultInput
					if err := decodeReleaseFile(c.String("file"), &input); err != nil {
						return fmt.Errorf("ward agent release result: %w", err)
					}
					return runAgentReleaseRequest(ctx, c, dispatchBrokerRequest{
						Action:  dispatchActionReleaseResult,
						Release: &releaseBrokerPayload{Result: &input},
					})
				},
			},
			{
				Name:  "receive",
				Usage: "Read typed release records addressed to the authenticated caller.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "after", Usage: "return records after this record id"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return runAgentReleaseRequest(ctx, c, dispatchBrokerRequest{
						Action:  dispatchActionReleaseReceive,
						After:   strings.TrimSpace(c.String("after")),
						Release: &releaseBrokerPayload{},
					})
				},
			},
		},
	}
}

func runAgentReleaseRequest(ctx context.Context, c *cli.Command, req dispatchBrokerRequest) error {
	resp, err := sendAgentReleaseRequest(ctx, req)
	if err != nil {
		return err
	}
	return json.NewEncoder(agentCommandWriter(c)).Encode(resp.ReleaseRecords)
}

func decodeReleaseFile(path string, target any) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("--file is empty")
	}
	file, err := os.Open(path) // #nosec G304 -- explicit caller-selected input file.
	if err != nil {
		return fmt.Errorf("read --file %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, releaseArtifactMaxBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode --file %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode --file %q: expected one JSON object", path)
	}
	return nil
}

func sendAgentReleaseRequest(ctx context.Context, req dispatchBrokerRequest) (dispatchBrokerResponse, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	token := strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
	if addr == "" || token == "" {
		return dispatchBrokerResponse{}, fmt.Errorf("ward agent release: no dispatch broker capability is available")
	}
	req.Token = token
	conn, err := dialDispatchBroker(ctx, addr)
	if err != nil {
		return dispatchBrokerResponse{}, dispatchBrokerDialDiagnostic(addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return dispatchBrokerResponse{}, fmt.Errorf("ward agent release: send request: %w", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return dispatchBrokerResponse{}, fmt.Errorf("ward agent release: read response: %w", err)
	}
	if !resp.OK {
		return resp, fmt.Errorf("ward agent release: %s", resp.Error)
	}
	return resp, nil
}

func validateDispatchBrokerRelease(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 || req.Target != "" || req.Message != "" || req.Conversation != "" {
		return fmt.Errorf("dispatch broker: release actions carry no launch, log, or message payload")
	}
	if req.Release == nil {
		return fmt.Errorf("dispatch broker: release payload is required")
	}
	switch dispatchAction(req.Action) {
	case dispatchActionReleaseCandidate:
		return validateReleaseCandidateRequest(req)
	case dispatchActionReleaseRetry:
		return validateReleaseRetryRequest(req)
	case dispatchActionReleaseResult:
		return validateReleaseResultRequest(req)
	case dispatchActionReleaseReceive:
		return validateReleaseReceiveRequest(req)
	default:
		return fmt.Errorf("dispatch broker: unsupported release action %q", req.Action)
	}
}

func validateReleaseCandidateRequest(req dispatchBrokerRequest) error {
	if req.Release.Candidate == nil || req.Release.Result != nil || req.Release.CandidateID != "" || req.Release.StartingDeployCommit != "" || req.After != "" {
		return fmt.Errorf("dispatch broker: release-candidate payload shape is invalid")
	}
	if !validDispatchAgentID(req.To) || req.To == "*" {
		return fmt.Errorf("dispatch broker: release candidate requires one exact peer recipient")
	}
	return validateReleaseCandidateInput(*req.Release.Candidate)
}

func validateReleaseRetryRequest(req dispatchBrokerRequest) error {
	if req.To != "" || req.After != "" || req.Release.Candidate != nil || req.Release.Result != nil ||
		!dispatchRequestIDPattern.MatchString(req.Release.CandidateID) || !validReleaseCommit(req.Release.StartingDeployCommit) {
		return fmt.Errorf("dispatch broker: release-retry requires a candidate id and full starting deploy commit")
	}
	return nil
}

func validateReleaseResultRequest(req dispatchBrokerRequest) error {
	if req.To != "" || req.After != "" || req.Release.Candidate != nil || req.Release.Result == nil || req.Release.CandidateID != "" || req.Release.StartingDeployCommit != "" {
		return fmt.Errorf("dispatch broker: release-result payload shape is invalid")
	}
	return validateReleaseResultInput(*req.Release.Result)
}

func validateReleaseReceiveRequest(req dispatchBrokerRequest) error {
	if req.To != "" || req.Release.Candidate != nil || req.Release.Result != nil || req.Release.CandidateID != "" || req.Release.StartingDeployCommit != "" {
		return fmt.Errorf("dispatch broker: release-receive carries only an after filter")
	}
	if req.After != "" && !dispatchRequestIDPattern.MatchString(req.After) {
		return fmt.Errorf("dispatch broker: invalid after release record id %q", req.After)
	}
	return nil
}

func validateReleaseCandidateInput(input releaseCandidateInput) error {
	if err := validateReleaseCandidateRepositories(input); err != nil {
		return err
	}
	if err := validateReleaseCandidateRevisions(input); err != nil {
		return err
	}
	if err := validateReleaseCandidateSymbols(input); err != nil {
		return err
	}
	return releaseValuesAreSecretSafe(input)
}

func validateReleaseCandidateRepositories(input releaseCandidateInput) error {
	if err := validateReleaseRepository("application_repository", input.ApplicationRepository); err != nil {
		return err
	}
	if err := validateReleaseRepository("deploy_repository", input.DeployRepository); err != nil {
		return err
	}
	ref, err := parseAgentIssueRef(input.OriginatingTicket)
	if err != nil || ref.String() != input.OriginatingTicket || ref.Owner == "" || ref.Repo == "" {
		return fmt.Errorf("dispatch broker: release candidate originating_ticket must be canonical owner/repo#N")
	}
	return nil
}

func validateReleaseCandidateRevisions(input releaseCandidateInput) error {
	if !validReleaseCommit(input.ApplicationCommit) {
		return fmt.Errorf("dispatch broker: release candidate application_commit must be a full lowercase commit")
	}
	if !validApprovalDigest(input.ArtifactDigest) {
		return fmt.Errorf("dispatch broker: release candidate artifact_digest must be a sha256 digest")
	}
	if !releaseSymbolPattern.MatchString(input.Environment) {
		return fmt.Errorf("dispatch broker: release candidate environment must be a safe symbolic id")
	}
	if !validReleaseCommit(input.StartingDeployCommit) {
		return fmt.Errorf("dispatch broker: release candidate starting_deploy_commit must be a full lowercase commit")
	}
	return nil
}

func validateReleaseCandidateSymbols(input releaseCandidateInput) error {
	if err := validateReleaseOperation("deploy_operation", input.DeployOperation); err != nil {
		return err
	}
	return validateReleaseOperation("verify_operation", input.VerifyOperation)
}

func validateReleaseRepository(label, repo string) error {
	if len(repo) > releaseRepositoryMaxBytes {
		return fmt.Errorf("dispatch broker: release candidate %s exceeds %d bytes", label, releaseRepositoryMaxBytes)
	}
	parsed, err := parseRepoRef(repo)
	if err != nil || parsed.slug() != repo {
		return fmt.Errorf("dispatch broker: release candidate %s must be canonical owner/repo", label)
	}
	return nil
}

func validateReleaseOperation(label, operation string) error {
	if !releaseSymbolPattern.MatchString(operation) {
		return fmt.Errorf("dispatch broker: release candidate %s must be a safe symbolic id", label)
	}
	return nil
}

func validateReleaseCandidate(candidate releaseCandidate) error {
	if err := validateReleaseCandidateEnvelope(candidate); err != nil {
		return err
	}
	if err := validateReleaseCandidateInput(releaseCandidateInput{
		ApplicationRepository: candidate.ApplicationRepository,
		ApplicationCommit:     candidate.ApplicationCommit,
		ArtifactDigest:        candidate.ArtifactDigest,
		Environment:           candidate.Environment,
		DeployRepository:      candidate.DeployRepository,
		StartingDeployCommit:  candidate.StartingDeployCommit,
		OriginatingTicket:     candidate.OriginatingTicket,
		DeployOperation:       candidate.DeployOperation,
		VerifyOperation:       candidate.VerifyOperation,
	}); err != nil {
		return err
	}
	digest, err := releaseCandidateContentHash(candidate)
	if err != nil || digest != candidate.ContentHash {
		return fmt.Errorf("dispatch broker: release candidate content hash does not match its immutable content")
	}
	return nil
}

func validateReleaseCandidateEnvelope(candidate releaseCandidate) error {
	if candidate.SchemaVersion != releaseSchemaVersion || !dispatchRequestIDPattern.MatchString(candidate.CandidateID) ||
		!validDispatchAgentID(candidate.From) || !validDispatchAgentID(candidate.To) || candidate.CreatedAt.IsZero() {
		return fmt.Errorf("dispatch broker: release candidate envelope is incomplete")
	}
	if !validClusterID(candidate.Correlation.ClusterID) || !validDispatchAgentID(candidate.Correlation.WardRunID) {
		return fmt.Errorf("dispatch broker: release candidate Ward provenance is missing")
	}
	if candidate.Correlation.DispatchRequestID != "" && !dispatchRequestIDPattern.MatchString(candidate.Correlation.DispatchRequestID) {
		return fmt.Errorf("dispatch broker: release candidate dispatch provenance is invalid")
	}
	return nil
}

func validateReleaseResultInput(input releaseResultInput) error {
	if !dispatchRequestIDPattern.MatchString(input.CandidateID) || !dispatchRequestIDPattern.MatchString(input.AttemptID) {
		return fmt.Errorf("dispatch broker: release result requires broker-minted candidate and attempt ids")
	}
	if !validReleaseOutcome(input.Classification) {
		return fmt.Errorf("dispatch broker: invalid release classification %q", input.Classification)
	}
	if !releaseSymbolPattern.MatchString(input.ReasonCode) {
		return fmt.Errorf("dispatch broker: release result reason_code must be a safe symbolic id")
	}
	if len(input.EvidenceDigests) == 0 {
		return fmt.Errorf("dispatch broker: release result requires at least one evidence digest")
	}
	if len(input.EvidenceDigests) > releaseEvidenceMax {
		return fmt.Errorf("dispatch broker: release result exceeds %d evidence digests", releaseEvidenceMax)
	}
	for _, digest := range input.EvidenceDigests {
		if !validApprovalDigest(digest) {
			return fmt.Errorf("dispatch broker: release result evidence digests must be sha256 digests")
		}
	}
	if err := validateReleaseResultCommitShape(input); err != nil {
		return err
	}
	return releaseValuesAreSecretSafe(input)
}

func validateReleaseResultCommitShape(input releaseResultInput) error {
	switch input.Classification {
	case releaseOutcomeVerified:
		if !validReleaseCommit(input.DeployCommit) || input.RestoredCommit != "" {
			return fmt.Errorf("dispatch broker: verified release result requires one full deploy commit and no restored commit")
		}
	case releaseOutcomeRestored:
		if input.DeployCommit != "" || !validReleaseCommit(input.RestoredCommit) {
			return fmt.Errorf("dispatch broker: restored release result requires the prior commit and no new deploy commit")
		}
	default:
		if input.DeployCommit != "" || input.RestoredCommit != "" {
			return fmt.Errorf("dispatch broker: non-verified release result cannot carry a deploy-state commit")
		}
	}
	return nil
}

func validReleaseCommit(value string) bool {
	return releaseCommitPattern.MatchString(value)
}

func validReleaseOutcome(value string) bool {
	switch value {
	case releaseOutcomeVerified, releaseOutcomeRejected, releaseOutcomeRestored, releaseOutcomeBlocked, releaseOutcomeIndeterminate:
		return true
	default:
		return false
	}
}

func releaseValuesAreSecretSafe(value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	redactor, err := configuredSecretRedactor(nil)
	if err != nil {
		return fmt.Errorf("dispatch broker: build release artifact redactor: %w", err)
	}
	if redactor.redact(string(body)) != string(body) {
		return fmt.Errorf("dispatch broker: release contract contains a configured secret-bearing value")
	}
	return nil
}

func releaseCandidateContentHash(candidate releaseCandidate) (string, error) {
	candidate.ContentHash = ""
	body, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (r *Runner) runDispatchBrokerRelease(conn net.Conn, req dispatchBrokerRequest) {
	if err := validateDispatchBrokerRelease(req); err != nil {
		writeDispatchBrokerReleaseResponse(conn, nil, err)
		return
	}
	var records []releaseArtifactRecord
	var err error
	if dispatchAction(req.Action) != dispatchActionReleaseReceive {
		releaseContractMu.Lock()
		defer releaseContractMu.Unlock()
	}
	switch dispatchAction(req.Action) {
	case dispatchActionReleaseCandidate:
		records, err = createReleaseCandidate(req)
	case dispatchActionReleaseRetry:
		records, err = retryReleaseCandidate(req)
	case dispatchActionReleaseResult:
		records, err = recordReleaseResult(req)
	case dispatchActionReleaseReceive:
		records, err = receiveReleaseRecords(req)
	}
	writeDispatchBrokerReleaseResponse(conn, records, err)
}

func writeDispatchBrokerReleaseResponse(conn net.Conn, records []releaseArtifactRecord, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, ReleaseRecords: records}
	if err != nil {
		resp.Error = err.Error()
	}
	if body, marshalErr := json.Marshal(resp); marshalErr == nil {
		_, _ = conn.Write(body)
	}
}

func createReleaseCandidate(req dispatchBrokerRequest) ([]releaseArtifactRecord, error) {
	if req.AuthenticatedRole != roleDirector {
		return nil, fmt.Errorf("dispatch broker: only an authenticated Director may create a release candidate")
	}
	admission, err := releaseOpsAdmission(req.BrokerID, req.To)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	input := *req.Release.Candidate
	candidate := releaseCandidate{
		SchemaVersion:         releaseSchemaVersion,
		CandidateID:           newDispatchBrokerRequestID(),
		From:                  req.Requester,
		To:                    admission.PeerID,
		CreatedAt:             now,
		ApplicationRepository: input.ApplicationRepository,
		ApplicationCommit:     input.ApplicationCommit,
		ArtifactDigest:        input.ArtifactDigest,
		Environment:           input.Environment,
		DeployRepository:      input.DeployRepository,
		StartingDeployCommit:  input.StartingDeployCommit,
		OriginatingTicket:     input.OriginatingTicket,
		DeployOperation:       input.DeployOperation,
		VerifyOperation:       input.VerifyOperation,
		Correlation:           releaseCorrelation{ClusterID: req.BrokerID, WardRunID: req.Requester},
	}
	candidate.Correlation.DispatchRequestID = releaseRequesterDispatchID(req.BrokerID, req.Requester)
	candidate.ContentHash, err = releaseCandidateContentHash(candidate)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseCandidate(candidate); err != nil {
		return nil, err
	}
	attempt := newReleaseAttempt(candidate, req.Requester, now)
	record := releaseArtifactRecord{
		ID: newDispatchBrokerRequestID(), Kind: releaseRecordKindCandidate, CreatedAt: now,
		Candidate: &candidate, Attempt: &attempt,
	}
	if err := appendReleaseArtifactRecord(admission, record); err != nil {
		return nil, err
	}
	return []releaseArtifactRecord{record}, nil
}

func retryReleaseCandidate(req dispatchBrokerRequest) ([]releaseArtifactRecord, error) {
	if req.AuthenticatedRole != roleDirector {
		return nil, fmt.Errorf("dispatch broker: only an authenticated Director may retry a release candidate")
	}
	records, admission, err := releaseRecordsForCandidate(req.BrokerID, req.Release.CandidateID)
	if err != nil {
		return nil, err
	}
	candidate, attempts, results, err := releaseCandidateState(records, req.Release.CandidateID)
	if err != nil {
		return nil, err
	}
	if candidate.From != req.Requester {
		return nil, fmt.Errorf("dispatch broker: release candidate belongs to Director %s", candidate.From)
	}
	activeAdmission, err := releaseOpsAdmission(req.BrokerID, candidate.To)
	if err != nil {
		return nil, err
	}
	if activeAdmission.RequestID != admission.RequestID {
		return nil, fmt.Errorf("dispatch broker: release candidate Ops admission changed")
	}
	if candidate.StartingDeployCommit != req.Release.StartingDeployCommit {
		return nil, fmt.Errorf("dispatch broker: starting deploy revision changed, create a new release candidate")
	}
	latest := latestReleaseAttempt(attempts)
	result, ok := results[latest.AttemptID]
	if !ok {
		return nil, fmt.Errorf("dispatch broker: release attempt %s has no terminal result", latest.AttemptID)
	}
	if result.Classification == releaseOutcomeIndeterminate {
		return nil, fmt.Errorf("dispatch broker: indeterminate release blocks automated retry pending Ops reconciliation")
	}
	if result.Classification == releaseOutcomeVerified {
		return nil, fmt.Errorf("dispatch broker: verified release candidates cannot be retried")
	}
	now := time.Now().UTC()
	attempt := newReleaseAttempt(candidate, req.Requester, now)
	record := releaseArtifactRecord{ID: newDispatchBrokerRequestID(), Kind: releaseRecordKindAttempt, CreatedAt: now, Attempt: &attempt}
	if err := appendReleaseArtifactRecord(admission, record); err != nil {
		return nil, err
	}
	return []releaseArtifactRecord{record}, nil
}

func recordReleaseResult(req dispatchBrokerRequest) ([]releaseArtifactRecord, error) {
	if req.AuthenticatedRole != releaseOpsRole {
		return nil, fmt.Errorf("dispatch broker: only an authenticated Ops peer may return a release result")
	}
	input := *req.Release.Result
	records, admission, err := releaseRecordsForCandidate(req.BrokerID, input.CandidateID)
	if err != nil {
		return nil, err
	}
	candidate, attempts, results, err := releaseCandidateState(records, input.CandidateID)
	if err != nil {
		return nil, err
	}
	if candidate.To != req.Requester || admission.PeerID != req.Requester {
		return nil, fmt.Errorf("dispatch broker: release candidate is addressed to Ops peer %s", candidate.To)
	}
	attempt, ok := attempts[input.AttemptID]
	if !ok {
		return nil, fmt.Errorf("dispatch broker: release attempt %s does not belong to candidate %s", input.AttemptID, input.CandidateID)
	}
	if _, exists := results[input.AttemptID]; exists {
		return nil, fmt.Errorf("dispatch broker: release attempt %s already has a terminal result", input.AttemptID)
	}
	if input.Classification == releaseOutcomeRestored && input.RestoredCommit != candidate.StartingDeployCommit {
		return nil, fmt.Errorf("dispatch broker: restored result must identify the unchanged starting deploy commit")
	}
	now := time.Now().UTC()
	result := releaseResult{
		SchemaVersion:   releaseSchemaVersion,
		CandidateID:     candidate.CandidateID,
		CandidateHash:   candidate.ContentHash,
		AttemptID:       attempt.AttemptID,
		From:            req.Requester,
		To:              candidate.From,
		CreatedAt:       now,
		Classification:  input.Classification,
		ReasonCode:      input.ReasonCode,
		EvidenceDigests: append([]string(nil), input.EvidenceDigests...),
		DeployCommit:    input.DeployCommit,
		RestoredCommit:  input.RestoredCommit,
		Correlation: releaseCorrelation{
			ClusterID:         req.BrokerID,
			WardRunID:         req.Requester,
			DispatchRequestID: admission.RequestID,
		},
	}
	if err := releaseValuesAreSecretSafe(result); err != nil {
		return nil, err
	}
	record := releaseArtifactRecord{ID: newDispatchBrokerRequestID(), Kind: releaseRecordKindResult, CreatedAt: now, Result: &result}
	if err := appendReleaseArtifactRecord(admission, record); err != nil {
		return nil, err
	}
	return []releaseArtifactRecord{record}, nil
}

func receiveReleaseRecords(req dispatchBrokerRequest) ([]releaseArtifactRecord, error) {
	records, err := readAllReleaseArtifactRecords(req.BrokerID)
	if err != nil {
		return nil, err
	}
	filtered := make([]releaseArtifactRecord, 0)
	pastAfter := req.After == ""
	for _, record := range records {
		if !pastAfter {
			pastAfter = record.ID == req.After
			continue
		}
		if releaseRecordAddressedTo(record, req.Requester) {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func releaseRecordAddressedTo(record releaseArtifactRecord, requester string) bool {
	if record.Candidate != nil && record.Candidate.To == requester {
		return true
	}
	if record.Attempt != nil && record.Attempt.To == requester {
		return true
	}
	return record.Result != nil && record.Result.To == requester
}

func newReleaseAttempt(candidate releaseCandidate, requester string, now time.Time) releaseAttempt {
	return releaseAttempt{
		SchemaVersion: releaseSchemaVersion,
		AttemptID:     newDispatchBrokerRequestID(),
		CandidateID:   candidate.CandidateID,
		CandidateHash: candidate.ContentHash,
		RequestedBy:   requester,
		To:            candidate.To,
		CreatedAt:     now,
	}
}

func releaseOpsAdmission(clusterID, peerID string) (dispatchPeerAdmission, error) {
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return dispatchPeerAdmission{}, err
	}
	for _, admission := range admissions {
		if admission.PeerID != peerID {
			continue
		}
		if admission.Role != releaseOpsRole || (admission.Status != dispatchPeerStatusActive && admission.Status != dispatchPeerStatusAdmitted) {
			return dispatchPeerAdmission{}, fmt.Errorf("dispatch broker: release candidate target %s is not an active Ops peer", peerID)
		}
		return admission, nil
	}
	return dispatchPeerAdmission{}, fmt.Errorf("dispatch broker: release candidate target %s is not a broker-admitted peer", peerID)
}

func releaseRequesterDispatchID(clusterID, requester string) string {
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return ""
	}
	for _, admission := range admissions {
		if admission.PeerID == requester {
			return admission.RequestID
		}
	}
	return ""
}

func releaseRecordsForCandidate(clusterID, candidateID string) ([]releaseArtifactRecord, dispatchPeerAdmission, error) {
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return nil, dispatchPeerAdmission{}, err
	}
	for _, admission := range admissions {
		records, readErr := readReleaseArtifactRecords(admission)
		if readErr != nil {
			return nil, dispatchPeerAdmission{}, readErr
		}
		for _, record := range records {
			if record.Candidate != nil && record.Candidate.CandidateID == candidateID {
				return records, admission, nil
			}
		}
	}
	return nil, dispatchPeerAdmission{}, fmt.Errorf("dispatch broker: release candidate %s was not found in cluster %s", candidateID, clusterID)
}

func readAllReleaseArtifactRecords(clusterID string) ([]releaseArtifactRecord, error) {
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return nil, err
	}
	var records []releaseArtifactRecord
	seen := map[string]bool{}
	for _, admission := range admissions {
		if seen[admission.RequestID] {
			continue
		}
		seen[admission.RequestID] = true
		part, readErr := readReleaseArtifactRecords(admission)
		if readErr != nil {
			return nil, readErr
		}
		records = append(records, part...)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records, nil
}

func releaseCandidateState(records []releaseArtifactRecord, candidateID string) (releaseCandidate, map[string]releaseAttempt, map[string]releaseResult, error) {
	candidate, attempts, results, err := collectReleaseCandidateState(records, candidateID)
	if err != nil {
		return releaseCandidate{}, nil, nil, err
	}
	if err := validateReleaseCandidateState(candidate, attempts, results); err != nil {
		return releaseCandidate{}, nil, nil, err
	}
	return candidate, attempts, results, nil
}

func collectReleaseCandidateState(records []releaseArtifactRecord, candidateID string) (releaseCandidate, map[string]releaseAttempt, map[string]releaseResult, error) {
	var candidate releaseCandidate
	attempts := map[string]releaseAttempt{}
	results := map[string]releaseResult{}
	for _, record := range records {
		if record.Candidate != nil && record.Candidate.CandidateID == candidateID {
			if candidate.CandidateID != "" {
				return releaseCandidate{}, nil, nil, fmt.Errorf("dispatch broker: duplicate release candidate %s", candidateID)
			}
			candidate = *record.Candidate
		}
		if record.Attempt != nil && record.Attempt.CandidateID == candidateID {
			attempts[record.Attempt.AttemptID] = *record.Attempt
		}
		if record.Result != nil && record.Result.CandidateID == candidateID {
			results[record.Result.AttemptID] = *record.Result
		}
	}
	if candidate.CandidateID == "" {
		return releaseCandidate{}, nil, nil, fmt.Errorf("dispatch broker: release candidate %s is missing", candidateID)
	}
	return candidate, attempts, results, nil
}

func validateReleaseCandidateState(candidate releaseCandidate, attempts map[string]releaseAttempt, results map[string]releaseResult) error {
	if err := validateReleaseCandidate(candidate); err != nil {
		return err
	}
	if len(attempts) == 0 {
		return fmt.Errorf("dispatch broker: release candidate %s has no attempt", candidate.CandidateID)
	}
	for _, attempt := range attempts {
		if err := validateReleaseAttempt(attempt, candidate); err != nil {
			return err
		}
	}
	for _, result := range results {
		attempt, ok := attempts[result.AttemptID]
		if !ok {
			return fmt.Errorf("dispatch broker: release result references unknown attempt %s", result.AttemptID)
		}
		if err := validateReleaseResult(result, candidate, attempt); err != nil {
			return err
		}
	}
	return nil
}

func validateReleaseAttempt(attempt releaseAttempt, candidate releaseCandidate) error {
	if attempt.SchemaVersion != releaseSchemaVersion || !dispatchRequestIDPattern.MatchString(attempt.AttemptID) || attempt.CreatedAt.IsZero() {
		return fmt.Errorf("dispatch broker: release attempt envelope is incomplete")
	}
	if attempt.CandidateID != candidate.CandidateID || attempt.CandidateHash != candidate.ContentHash ||
		attempt.RequestedBy != candidate.From || attempt.To != candidate.To {
		return fmt.Errorf("dispatch broker: release attempt does not match its immutable candidate")
	}
	return nil
}

func validateReleaseResult(result releaseResult, candidate releaseCandidate, attempt releaseAttempt) error {
	if err := validateReleaseResultEnvelope(result, candidate, attempt); err != nil {
		return err
	}
	if err := validateReleaseResultInput(releaseResultInput{
		CandidateID: result.CandidateID, AttemptID: result.AttemptID,
		Classification: result.Classification, ReasonCode: result.ReasonCode,
		EvidenceDigests: result.EvidenceDigests, DeployCommit: result.DeployCommit, RestoredCommit: result.RestoredCommit,
	}); err != nil {
		return err
	}
	if result.Classification == releaseOutcomeRestored && result.RestoredCommit != candidate.StartingDeployCommit {
		return fmt.Errorf("dispatch broker: restored result does not identify the candidate's starting deploy commit")
	}
	return nil
}

func validateReleaseResultEnvelope(result releaseResult, candidate releaseCandidate, attempt releaseAttempt) error {
	if result.SchemaVersion != releaseSchemaVersion || result.CreatedAt.IsZero() ||
		result.CandidateID != candidate.CandidateID || result.CandidateHash != candidate.ContentHash || result.AttemptID != attempt.AttemptID {
		return fmt.Errorf("dispatch broker: release result does not match its immutable candidate and attempt")
	}
	if result.From != candidate.To || result.To != candidate.From ||
		result.Correlation.ClusterID != candidate.Correlation.ClusterID || result.Correlation.WardRunID != candidate.To ||
		!dispatchRequestIDPattern.MatchString(result.Correlation.DispatchRequestID) {
		return fmt.Errorf("dispatch broker: release result identity or Ward provenance is invalid")
	}
	return nil
}

func latestReleaseAttempt(attempts map[string]releaseAttempt) releaseAttempt {
	var latest releaseAttempt
	for _, attempt := range attempts {
		if latest.AttemptID == "" || attempt.CreatedAt.After(latest.CreatedAt) {
			latest = attempt
		}
	}
	return latest
}

func appendReleaseArtifactRecord(admission dispatchPeerAdmission, record releaseArtifactRecord) error {
	releaseArtifactMu.Lock()
	defer releaseArtifactMu.Unlock()
	path, err := releaseArtifactPath(admission)
	if err != nil {
		return err
	}
	if err := releaseValuesAreSecretSafe(record); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("dispatch broker: create release artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- validated Ward dispatch artifact path.
	if err != nil {
		return fmt.Errorf("dispatch broker: open release artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		return fmt.Errorf("dispatch broker: append release artifact: %w", err)
	}
	return file.Sync()
}

func readReleaseArtifactRecords(admission dispatchPeerAdmission) ([]releaseArtifactRecord, error) {
	releaseArtifactMu.Lock()
	defer releaseArtifactMu.Unlock()
	path, err := releaseArtifactPath(admission)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path) // #nosec G304 -- validated Ward dispatch artifact path.
	if errors.Is(err, os.ErrNotExist) {
		return []releaseArtifactRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dispatch broker: open release artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), releaseArtifactMaxBytes)
	var records []releaseArtifactRecord
	for scanner.Scan() {
		var record releaseArtifactRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("dispatch broker: decode release artifact: %w", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dispatch broker: read release artifact: %w", err)
	}
	return records, nil
}

func releaseArtifactPath(admission dispatchPeerAdmission) (string, error) {
	path, err := dispatchJournalPath(admission.RequestID)
	if err != nil {
		return "", err
	}
	journal, err := readDispatchJournal(path)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: read Ops peer dispatch journal: %w", err)
	}
	dir, err := validatedDispatchLifecycleArtifactDir(journal.Paths.Dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, releaseArtifactFile), nil
}

func releaseArtifactSourceForName(name string, tail int, follow bool) (agentLogSource, error) {
	if follow {
		return agentLogSource{}, fmt.Errorf("ward agent logs: --follow is not supported for release artifacts")
	}
	clusterID := currentDispatchClusterID()
	if !validClusterID(clusterID) {
		return agentLogSource{}, fmt.Errorf("ward agent logs: release artifacts require a current broker cluster")
	}
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return agentLogSource{}, err
	}
	for _, admission := range admissions {
		if admission.PeerID != name {
			continue
		}
		path, pathErr := releaseArtifactPath(admission)
		if pathErr != nil {
			return agentLogSource{}, pathErr
		}
		if _, statErr := os.Stat(path); statErr != nil {
			if os.IsNotExist(statErr) {
				return agentLogSource{}, fmt.Errorf("peer %s has no release artifact", name)
			}
			return agentLogSource{}, statErr
		}
		return agentLogSource{Kind: agentLogSourceFile, Label: "release artifact path", Path: path, Tail: tail}, nil
	}
	return agentLogSource{}, fmt.Errorf("dispatch broker: no peer admission matches %q", name)
}

func releaseArtifactSourceForRef(ref agentIssueRef, tail int, follow bool) (agentLogSource, error) {
	if follow {
		return agentLogSource{}, fmt.Errorf("ward agent logs: --follow is not supported for release artifacts")
	}
	clusterID := currentDispatchClusterID()
	if !validClusterID(clusterID) {
		return agentLogSource{}, fmt.Errorf("ward agent logs: release artifacts require a current broker cluster")
	}
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return agentLogSource{}, err
	}
	var selected string
	var selectedAt time.Time
	for _, admission := range admissions {
		path, createdAt, matched, matchErr := releaseArtifactCandidateForRef(admission, ref)
		if matchErr != nil {
			return agentLogSource{}, matchErr
		}
		if matched && (selected == "" || createdAt.After(selectedAt)) {
			selected = path
			selectedAt = createdAt
		}
	}
	if selected == "" {
		return agentLogSource{}, fmt.Errorf("dispatch broker: no release artifact matches %q", ref.String())
	}
	return agentLogSource{Kind: agentLogSourceFile, Label: "release artifact path", Path: selected, Tail: tail}, nil
}

func releaseArtifactCandidateForRef(admission dispatchPeerAdmission, ref agentIssueRef) (string, time.Time, bool, error) {
	records, err := readReleaseArtifactRecords(admission)
	if err != nil {
		return "", time.Time{}, false, err
	}
	var latest time.Time
	for _, record := range records {
		if record.Candidate != nil && record.Candidate.OriginatingTicket == ref.String() && record.CreatedAt.After(latest) {
			latest = record.CreatedAt
		}
	}
	if latest.IsZero() {
		return "", time.Time{}, false, nil
	}
	path, err := releaseArtifactPath(admission)
	return path, latest, err == nil, err
}
