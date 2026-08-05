package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
)

const (
	dispatchJournalsSubdir     = "dispatch-requests"
	dispatchJournalVersion     = 2
	envDispatchJournalDir      = "WARD_DISPATCH_JOURNAL_DIR"
	envDispatchRequestID       = "WARD_DISPATCH_REQUEST_ID"
	envDispatchJournalPath     = "WARD_DISPATCH_JOURNAL_PATH"
	envDispatchResumeContainer = "WARD_DISPATCH_RESUME_CONTAINER"
	labelDispatchRequest       = "ward.dispatch-request-id"
	dispatchPhaseAccepted      = "accepted"
	dispatchPhaseRecovering    = "recovering"
	dispatchPhasePreflight     = "preflight"
	dispatchPhaseReserved      = "reserved"
	dispatchPhaseCreating      = "creating"
	dispatchPhaseCreated       = "created"
	dispatchPhasePrepared      = "prepared"
	dispatchPhaseStarting      = "starting"
	dispatchPhaseVisible       = "visible"
	dispatchPhaseTerminal      = "terminal"
	dispatchOutcomeInProgress  = "in-progress"
	dispatchOutcomeLaunched    = "launched"
	dispatchOutcomeFailed      = "failed"
	dispatchOutcomeInterrupted = "interrupted"
)

var dispatchRequestLocks sync.Map

type dispatchRequestJournal struct {
	Version        int                         `json:"version"`
	RequestID      string                      `json:"request_id"`
	Fingerprint    string                      `json:"fingerprint"`
	Request        dispatchBrokerRequest       `json:"request"`
	Paths          dispatchArtifactPaths       `json:"paths"`
	BrokerID       string                      `json:"broker_id,omitempty"`
	Repo           string                      `json:"repo,omitempty"`
	Issue          string                      `json:"issue,omitempty"`
	Ref            string                      `json:"ref,omitempty"`
	Role           string                      `json:"role,omitempty"`
	Harness        string                      `json:"harness,omitempty"`
	Workflow       string                      `json:"workflow,omitempty"`
	State          string                      `json:"state"`
	LastTransition dispatchLifecycleTransition `json:"last_transition"`
	TerminalReason string                      `json:"terminal_reason,omitempty"`
	Phase          string                      `json:"phase"`
	Outcome        string                      `json:"outcome"`
	ContainerID    string                      `json:"container_id,omitempty"`
	Error          string                      `json:"error,omitempty"`
	AcceptedAt     time.Time                   `json:"accepted_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
}

type dispatchContainerState struct {
	ID    string
	State string
	Name  string
}

type dispatchReconcileDecision struct {
	ResumeContainer string
	TerminalOutcome string
	Err             error
}

func dispatchRequestLock(requestID string) *sync.Mutex {
	value, _ := dispatchRequestLocks.LoadOrStore(requestID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func dispatchJournalPath(requestID string) (string, error) {
	if !dispatchRequestIDPattern.MatchString(requestID) {
		return "", fmt.Errorf("dispatch broker: invalid request id %q", requestID)
	}
	dir := strings.TrimSpace(os.Getenv(envDispatchJournalDir))
	if dir == "" {
		global, err := config.GlobalDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(global, dispatchJournalsSubdir)
	}
	return filepath.Join(dir, requestID+".json"), nil
}

func dispatchRequestFingerprint(req dispatchBrokerRequest) (string, error) {
	req.Token = ""
	req.RequestID = ""
	req.JournalPath = ""
	req.ResumeContainer = ""
	req.Recovery = false
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func acceptDispatchLaunch(req dispatchBrokerRequest) (dispatchArtifactPaths, *dispatchArtifactLog, string, bool, error) {
	if !dispatchRequestIDPattern.MatchString(req.RequestID) {
		return dispatchArtifactPaths{}, nil, "", false, fmt.Errorf("dispatch broker: invalid request id %q", req.RequestID)
	}
	lock := dispatchRequestLock(req.RequestID)
	lock.Lock()
	defer lock.Unlock()
	path, err := dispatchJournalPath(req.RequestID)
	if err != nil {
		return dispatchArtifactPaths{}, nil, "", false, err
	}
	fingerprint, err := dispatchRequestFingerprint(req)
	if err != nil {
		return dispatchArtifactPaths{}, nil, "", false, fmt.Errorf("dispatch broker: fingerprint request: %w", err)
	}
	if existing, readErr := readDispatchJournal(path); readErr == nil {
		if existing.Fingerprint != fingerprint {
			err := fmt.Errorf("dispatch broker: request id %s was already accepted with different launch arguments", req.RequestID)
			return existing.Paths, nil, path, true, err
		}
		return existing.Paths, nil, path, true, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return dispatchArtifactPaths{}, nil, path, false, fmt.Errorf("dispatch broker: read request journal: %w", readErr)
	}
	paths, logf, err := openDispatchArtifact(req, time.Now(), req.RequestID)
	if err != nil {
		return dispatchArtifactPaths{}, nil, path, false, fmt.Errorf("dispatch broker: open run log: %w", err)
	}
	writeDispatchArtifactInitial(paths, req)
	persistedReq := req
	persistedReq.Token = ""
	journal := dispatchRequestJournal{
		Version:     dispatchJournalVersion,
		RequestID:   req.RequestID,
		Fingerprint: fingerprint,
		Request:     persistedReq,
		Paths:       paths,
		Phase:       dispatchPhaseAccepted,
		Outcome:     dispatchOutcomeInProgress,
		AcceptedAt:  paths.CreatedAt,
		UpdatedAt:   paths.CreatedAt,
	}
	initializeDispatchLifecycle(&journal, req, paths)
	if err := createDispatchJournal(path, journal); err != nil {
		_ = logf.Close()
		return paths, nil, path, false, fmt.Errorf("dispatch broker: persist accepted request: %w", err)
	}
	return paths, logf, path, false, nil
}

func readDispatchJournal(path string) (dispatchRequestJournal, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- Ward-derived path under ~/.ward
	if err != nil {
		return dispatchRequestJournal{}, err
	}
	var journal dispatchRequestJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return dispatchRequestJournal{}, err
	}
	return journal, nil
}

func createDispatchJournal(path string, journal dispatchRequestJournal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- Ward-derived path under ~/.ward
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(append(body, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func writeDispatchJournal(path string, journal dispatchRequestJournal) error {
	journal.UpdatedAt = time.Now().UTC()
	body, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func updateDispatchJournal(path, phase, containerID, outcome string, launchErr error) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	requestID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	lock := dispatchRequestLock(requestID)
	lock.Lock()
	defer lock.Unlock()
	journal, err := readDispatchJournal(path)
	if err != nil {
		return err
	}
	if phase != "" {
		journal.Phase = phase
	}
	if containerID != "" {
		journal.ContainerID = containerID
	}
	if outcome != "" {
		journal.Outcome = outcome
	}
	if launchErr != nil {
		journal.Error = redactSecrets(firstLine(launchErr.Error()))
	}
	advanceDispatchLifecycle(&journal, phase, outcome, launchErr)
	if err := writeDispatchJournal(path, journal); err != nil {
		return err
	}
	writeDispatchLifecycleArtifact(journal)
	return nil
}

func checkpointDispatchJournal(phase, containerID string) error {
	return updateDispatchJournal(
		strings.TrimSpace(os.Getenv(envDispatchJournalPath)),
		phase,
		containerID,
		dispatchOutcomeInProgress,
		nil,
	)
}

func (r *Runner) reconcileDispatchJournals(ctx context.Context, brokerID string) error {
	dir, err := dispatchJournalDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if rerr := r.reconcileDispatchJournalEntry(ctx, dir, brokerID, entry); rerr != nil {
			return rerr
		}
	}
	return nil
}

func (r *Runner) reconcileDispatchJournalEntry(ctx context.Context, dir, brokerID string, entry os.DirEntry) error {
	if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
		return nil
	}
	path := filepath.Join(dir, entry.Name())
	journal, err := readDispatchJournal(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := ensureDispatchLifecycleMigrated(path, &journal); err != nil {
		return err
	}
	if dispatchLifecycleTerminal(journal.State) {
		return nil
	}
	if strings.TrimSpace(firstNonEmptyList(journal.BrokerID, journal.Request.BrokerID)) != strings.TrimSpace(brokerID) {
		return nil
	}
	return r.reconcileDispatchJournal(ctx, path, journal)
}

func ensureDispatchLifecycleMigrated(path string, journal *dispatchRequestJournal) error {
	needsMigration := journal.Version < dispatchJournalVersion || journal.State == ""
	migrateDispatchLifecycle(journal)
	if !needsMigration {
		return nil
	}
	if err := writeDispatchJournal(path, *journal); err != nil {
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	writeDispatchLifecycleArtifact(*journal)
	return nil
}

func (r *Runner) reconcileDispatchJournal(ctx context.Context, path string, journal dispatchRequestJournal) error {
	containers, err := r.dispatchRequestContainers(ctx, journal.RequestID)
	if err != nil {
		return fmt.Errorf("reconcile %s: query containers: %w", journal.RequestID, err)
	}
	decision := classifyDispatchReconcile(journal, containers)
	if decision.Err != nil && decision.TerminalOutcome == "" {
		return decision.Err
	}
	if decision.TerminalOutcome != "" {
		containerID := ""
		if len(containers) == 1 {
			containerID = containers[0].ID
		}
		return updateDispatchJournal(path, dispatchPhaseTerminal, containerID, decision.TerminalOutcome, decision.Err)
	}
	return r.resumeDispatchJournal(ctx, path, journal, decision.ResumeContainer)
}

func classifyDispatchReconcile(journal dispatchRequestJournal, containers []dispatchContainerState) dispatchReconcileDecision {
	if len(containers) > 1 {
		return dispatchReconcileDecision{Err: fmt.Errorf("reconcile %s: invariant violation: %d containers carry %s=%s",
			journal.RequestID, len(containers), labelDispatchRequest, journal.RequestID)}
	}
	if len(containers) == 0 {
		switch journal.Phase {
		case dispatchPhaseAccepted, dispatchPhaseRecovering, dispatchPhasePreflight, dispatchPhaseReserved, dispatchPhaseCreating:
			return dispatchReconcileDecision{}
		default:
			err := fmt.Errorf("accepted request %s lost its container after phase %s, refusing ambiguous replay",
				journal.RequestID, journal.Phase)
			return dispatchReconcileDecision{TerminalOutcome: dispatchOutcomeInterrupted, Err: err}
		}
	}
	container := containers[0]
	switch container.State {
	case "created":
		return dispatchReconcileDecision{ResumeContainer: container.ID}
	case "running", "restarting", "paused":
		return dispatchReconcileDecision{TerminalOutcome: dispatchOutcomeLaunched}
	case "exited", "dead":
		err := fmt.Errorf("accepted request %s has terminal container %s in state %s",
			journal.RequestID, emptyDefault(container.Name, container.ID), container.State)
		return dispatchReconcileDecision{TerminalOutcome: dispatchOutcomeFailed, Err: err}
	default:
		return dispatchReconcileDecision{Err: fmt.Errorf("reconcile %s: unsupported Docker state %q", journal.RequestID, container.State)}
	}
}

func (r *Runner) dispatchRequestContainers(ctx context.Context, requestID string) ([]dispatchContainerState, error) {
	out, err := r.dockerCapture(ctx, "ps", "-a",
		"--filter", "label="+labelDispatchRequest+"="+requestID,
		"--format", "{{.ID}}\t{{.State}}\t{{.Names}}")
	if err != nil {
		return nil, err
	}
	var containers []dispatchContainerState
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected docker ps row %q", line)
		}
		containers = append(containers, dispatchContainerState{
			ID:    strings.TrimSpace(parts[0]),
			State: strings.TrimSpace(parts[1]),
			Name:  strings.TrimSpace(parts[2]),
		})
	}
	return containers, nil
}

func (r *Runner) resumeDispatchJournal(ctx context.Context, path string, journal dispatchRequestJournal, containerID string) error {
	if err := updateDispatchJournal(path, dispatchPhaseRecovering, containerID, dispatchOutcomeInProgress, nil); err != nil {
		return err
	}
	logf, err := openDispatchArtifactLog(journal.Paths.ConsolePath)
	if err != nil {
		return err
	}
	req := journal.Request
	req.JournalPath = path
	req.ResumeContainer = containerID
	req.Recovery = true
	started := make(chan struct{})
	go r.handleHostDispatchBrokerLaunch(ctx, req, journal.Paths, logf, func() {}, nil, started)
	select {
	case <-started:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
