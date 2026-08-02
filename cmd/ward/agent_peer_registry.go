package main

import (
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
	dispatchPeerAdmissionsSubdir = "peer-admissions"
	dispatchPeerStatusAdmitted   = "admitted"
	dispatchPeerStatusActive     = "active"
	dispatchPeerStatusFailed     = "failed"
	dispatchPeerMintAttempts     = 32
)

type dispatchPeerAdmission struct {
	ClusterID string    `json:"cluster_id"`
	RequestID string    `json:"request_id"`
	PeerID    string    `json:"peer_id"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Admitted  time.Time `json:"admitted_at"`
	Updated   time.Time `json:"updated_at"`
}

var dispatchPeerAdmissionsMu sync.Mutex
var dispatchPeerIDSuffix = dictatableID

func admitDispatchPeer(req *dispatchBrokerRequest) (bool, error) {
	if req == nil || dispatchAction(req.Action) != dispatchActionLaunch ||
		req.Role == roleEngineer || req.Role == roleQA {
		return false, nil
	}
	dispatchPeerAdmissionsMu.Lock()
	defer dispatchPeerAdmissionsMu.Unlock()

	admissions, path, err := readDispatchPeerAdmissions(req.BrokerID)
	if err != nil {
		return false, err
	}
	for _, admission := range admissions {
		if admission.RequestID == req.RequestID {
			if admission.Role != req.Role {
				return false, fmt.Errorf("dispatch broker: request id %s was already admitted for role %s", req.RequestID, admission.Role)
			}
			req.AgentID = admission.PeerID
			bindDispatchPeerID(req)
			return false, nil
		}
	}

	peerID := strings.TrimSpace(req.AgentID)
	if peerID == "" {
		peerID, err = mintDispatchPeerID(req.Role, admissions)
		if err != nil {
			return false, err
		}
	} else if dispatchPeerIDInUse(peerID, admissions) {
		return false, fmt.Errorf("dispatch broker: peer id %s is already active in cluster %s", peerID, req.BrokerID)
	}
	req.AgentID = peerID
	bindDispatchPeerID(req)
	now := time.Now().UTC()
	admissions = append(admissions, dispatchPeerAdmission{
		ClusterID: req.BrokerID,
		RequestID: req.RequestID,
		PeerID:    peerID,
		Role:      req.Role,
		Status:    dispatchPeerStatusAdmitted,
		Admitted:  now,
		Updated:   now,
	})
	if err := writeDispatchPeerAdmissions(path, admissions); err != nil {
		return false, err
	}
	return true, nil
}

func mintDispatchPeerID(role string, admissions []dispatchPeerAdmission) (string, error) {
	for attempt := 0; attempt < dispatchPeerMintAttempts; attempt++ {
		candidate := role + "-" + dispatchPeerIDSuffix()
		if !dispatchPeerIDInUse(candidate, admissions) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("dispatch broker: could not mint a distinct peer id for role %s after %d attempts", role, dispatchPeerMintAttempts)
}

func dispatchPeerIDInUse(peerID string, admissions []dispatchPeerAdmission) bool {
	for _, admission := range admissions {
		if admission.PeerID == peerID && admission.Status != dispatchPeerStatusFailed {
			return true
		}
	}
	return false
}

func bindDispatchPeerID(req *dispatchBrokerRequest) {
	if req == nil || req.AgentID == "" || len(req.Argv) == 0 || req.Argv[0] != "run" {
		return
	}
	for i, arg := range req.Argv {
		if arg == "--agent-id" && i+1 < len(req.Argv) {
			req.Argv[i+1] = req.AgentID
			return
		}
	}
	req.Argv = append(req.Argv[:1], append([]string{"--agent-id", req.AgentID}, req.Argv[1:]...)...)
}

func updateDispatchPeerStatus(req dispatchBrokerRequest, status string) error {
	if req.AgentID == "" || req.BrokerID == "" || req.Role == roleEngineer || req.Role == roleQA {
		return nil
	}
	dispatchPeerAdmissionsMu.Lock()
	defer dispatchPeerAdmissionsMu.Unlock()
	admissions, path, err := readDispatchPeerAdmissions(req.BrokerID)
	if err != nil {
		return err
	}
	for i := range admissions {
		if admissions[i].RequestID == req.RequestID {
			admissions[i].Status = status
			admissions[i].Updated = time.Now().UTC()
			return writeDispatchPeerAdmissions(path, admissions)
		}
	}
	return fmt.Errorf("dispatch broker: peer admission for request %s is missing", req.RequestID)
}

func activeDispatchPeers(clusterID string) ([]dispatchPeerAdmission, error) {
	dispatchPeerAdmissionsMu.Lock()
	defer dispatchPeerAdmissionsMu.Unlock()
	admissions, _, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return nil, err
	}
	active := make([]dispatchPeerAdmission, 0, len(admissions))
	for _, admission := range admissions {
		if admission.Status == dispatchPeerStatusActive || admission.Status == dispatchPeerStatusAdmitted {
			active = append(active, admission)
		}
	}
	return active, nil
}

func reconcileDispatchPeerAdmissions(clusterID string) error {
	dispatchPeerAdmissionsMu.Lock()
	defer dispatchPeerAdmissionsMu.Unlock()
	admissions, path, err := readDispatchPeerAdmissions(clusterID)
	if err != nil {
		return err
	}
	changed := false
	for i := range admissions {
		if admissions[i].Status == dispatchPeerStatusFailed {
			continue
		}
		journalPath, pathErr := dispatchJournalPath(admissions[i].RequestID)
		if pathErr != nil {
			return pathErr
		}
		journal, readErr := readDispatchJournal(journalPath)
		switch {
		case errors.Is(readErr, os.ErrNotExist):
			admissions[i].Status = dispatchPeerStatusFailed
			changed = true
		case readErr != nil:
			return fmt.Errorf("dispatch broker: reconcile peer %s: %w", admissions[i].PeerID, readErr)
		case journal.Outcome == dispatchOutcomeFailed || journal.Outcome == dispatchOutcomeInterrupted:
			admissions[i].Status = dispatchPeerStatusFailed
			changed = true
		case journal.Phase == dispatchPhaseVisible || journal.Outcome == dispatchOutcomeLaunched:
			admissions[i].Status = dispatchPeerStatusActive
			changed = true
		}
		if changed {
			admissions[i].Updated = time.Now().UTC()
		}
	}
	if !changed {
		return nil
	}
	return writeDispatchPeerAdmissions(path, admissions)
}

func dispatchPeerAdmissionsPath(clusterID string) (string, error) {
	if !validClusterID(clusterID) {
		return "", fmt.Errorf("dispatch broker: invalid peer registry cluster id %q", clusterID)
	}
	global, err := config.GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(global, dispatchPeerAdmissionsSubdir, clusterID+".json"), nil
}

func readDispatchPeerAdmissions(clusterID string) ([]dispatchPeerAdmission, string, error) {
	path, err := dispatchPeerAdmissionsPath(clusterID)
	if err != nil {
		return nil, "", err
	}
	body, err := os.ReadFile(path) // #nosec G304 -- validated Ward-owned cluster path.
	if errors.Is(err, os.ErrNotExist) {
		return []dispatchPeerAdmission{}, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	var admissions []dispatchPeerAdmission
	if err := json.Unmarshal(body, &admissions); err != nil {
		return nil, path, fmt.Errorf("dispatch broker: decode peer registry: %w", err)
	}
	return admissions, path, nil
}

func writeDispatchPeerAdmissions(path string, admissions []dispatchPeerAdmission) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(admissions, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(body, '\n'))
}
