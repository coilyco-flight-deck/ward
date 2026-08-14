package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/config"
	"github.com/urfave/cli/v3"
)

const (
	dispatchStateQueued        = "queued"
	dispatchStateAccepted      = "accepted"
	dispatchStateLaunching     = "launching"
	dispatchStateRunning       = "running"
	dispatchStateCleanupNeeded = "cleanup-needed"
	dispatchStateCompleted     = "completed"
	dispatchStateBlocked       = "blocked"
	dispatchStateFailed        = "failed"
	dispatchStateInterrupted   = "interrupted"
)

type dispatchLifecycleTransition struct {
	From       string    `json:"from,omitempty"`
	To         string    `json:"to"`
	At         time.Time `json:"at"`
	ReasonCode string    `json:"reason_code,omitempty"`
}

type dispatchLifecycleRecord struct {
	RequestID       string                      `json:"request_id"`
	State           string                      `json:"state"`
	Outcome         string                      `json:"outcome"`
	NextAction      string                      `json:"next_action,omitempty"`
	Repo            string                      `json:"repo,omitempty"`
	Issue           string                      `json:"issue,omitempty"`
	Ref             string                      `json:"ref,omitempty"`
	Role            string                      `json:"role,omitempty"`
	Harness         string                      `json:"harness,omitempty"`
	Workflow        string                      `json:"workflow,omitempty"`
	ClusterID       string                      `json:"cluster_id,omitempty"`
	ContainerID     string                      `json:"container_id,omitempty"`
	AcceptedAt      time.Time                   `json:"accepted_at"`
	UpdatedAt       time.Time                   `json:"updated_at"`
	LastTransition  dispatchLifecycleTransition `json:"last_transition"`
	TerminalReason  string                      `json:"terminal_reason,omitempty"`
	DiagnosticPhase string                      `json:"diagnostic_phase,omitempty"`
	LogPath         string                      `json:"log_path,omitempty"`
}

func initializeDispatchLifecycle(journal *dispatchRequestJournal, req dispatchBrokerRequest, paths dispatchArtifactPaths) {
	journal.BrokerID = strings.TrimSpace(req.BrokerID)
	journal.Repo = strings.TrimSpace(paths.Repo)
	journal.Issue = strings.TrimSpace(paths.Issue)
	journal.Ref = strings.TrimSpace(paths.TargetRef)
	journal.Role = strings.TrimSpace(req.Role)
	journal.Harness = strings.TrimSpace(paths.Harness)
	journal.Workflow = dispatchRequestFlag(req.Argv, "--workflow")
	if journal.Workflow == "" && journal.Role == roleEngineer {
		journal.Workflow = string(defaultWorkflow)
	}
	journal.State = dispatchStateAccepted
	journal.LastTransition = dispatchLifecycleTransition{To: dispatchStateAccepted, At: journal.AcceptedAt.UTC(), ReasonCode: "broker-accepted"}
}

func dispatchRequestFlag(argv []string, name string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == name {
			return strings.TrimSpace(argv[i+1])
		}
	}
	return ""
}

func migrateDispatchLifecycle(journal *dispatchRequestJournal) {
	if journal.State != "" {
		return
	}
	journal.Version = dispatchJournalVersion
	initializeDispatchLifecycle(journal, journal.Request, journal.Paths)
	state, reason := dispatchLifecycleForUpdate(journal.Phase, journal.Outcome, nil)
	if state != "" && state != journal.State {
		journal.LastTransition = dispatchLifecycleTransition{From: journal.State, To: state, At: journal.UpdatedAt.UTC(), ReasonCode: reason}
		journal.State = state
	}
	if journal.State == dispatchStateRunning || dispatchLifecycleTerminal(journal.State) || journal.State == dispatchStateCleanupNeeded {
		clearDispatchRecoveryPayload(&journal.Request)
	}
}

func advanceDispatchLifecycle(journal *dispatchRequestJournal, phase, outcome string, launchErr error) {
	migrateDispatchLifecycle(journal)
	if dispatchLifecycleTerminal(journal.State) || journal.State == dispatchStateCleanupNeeded {
		return
	}
	state, reason := dispatchLifecycleForUpdate(phase, outcome, launchErr)
	if state == "" || state == journal.State {
		return
	}
	now := time.Now().UTC()
	journal.LastTransition = dispatchLifecycleTransition{From: journal.State, To: state, At: now, ReasonCode: reason}
	journal.State = state
	if dispatchLifecycleTerminal(state) || state == dispatchStateCleanupNeeded {
		journal.TerminalReason = reason
	}
	if state == dispatchStateRunning || dispatchLifecycleTerminal(state) || state == dispatchStateCleanupNeeded {
		clearDispatchRecoveryPayload(&journal.Request)
	}
}

func dispatchLifecycleForUpdate(phase, outcome string, updateErr error) (string, string) {
	switch outcome {
	case dispatchOutcomeInterrupted:
		return dispatchStateInterrupted, "orphaned-after-" + emptyDefault(phase, "unknown")
	case dispatchOutcomeFailed:
		if dispatchFailureIsBlocked(updateErr) {
			return dispatchStateBlocked, "launch-refused"
		}
		return dispatchStateFailed, "launch-failed"
	case dispatchOutcomeLaunched:
		return dispatchStateRunning, "container-visible"
	}
	switch phase {
	case dispatchPhaseAccepted:
		return dispatchStateAccepted, "broker-accepted"
	case dispatchPhaseRecovering, dispatchPhasePreflight, dispatchPhaseReserved,
		dispatchPhaseCreating, dispatchPhaseCreated, dispatchPhasePrepared, dispatchPhaseStarting:
		return dispatchStateLaunching, "launch-progress"
	case dispatchPhaseVisible:
		return dispatchStateRunning, "container-visible"
	}
	return "", ""
}

func dispatchFailureIsBlocked(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"capacity", "already reserved", "backpressure", "no-go", "not ready", "refusing", "blocked"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func clearDispatchRecoveryPayload(req *dispatchBrokerRequest) {
	req.Argv = nil
	req.Token = ""
	req.Forgejo = nil
	req.Message = ""
	req.Reason = ""
	req.Supersedes = ""
	req.Context = ""
	req.Head = ""
}

func dispatchLifecycleTerminal(state string) bool {
	switch state {
	case dispatchStateCompleted, dispatchStateBlocked, dispatchStateFailed, dispatchStateInterrupted:
		return true
	default:
		return false
	}
}

func dispatchLifecycleNextAction(state, requestID string) string {
	switch state {
	case dispatchStateQueued:
		return "wait for scheduling or cancel the queued intent"
	case dispatchStateAccepted, dispatchStateLaunching:
		return "inspect dispatch logs for request " + requestID
	case dispatchStateRunning:
		return "follow run logs for request " + requestID
	case dispatchStateCleanupNeeded:
		return "repair secret-safe drain or container cleanup for request " + requestID
	case dispatchStateBlocked:
		return "resolve the refusal and dispatch a new request"
	case dispatchStateFailed:
		return "inspect the terminal reason, then dispatch a new request if appropriate"
	case dispatchStateInterrupted:
		return "inspect orphan evidence, then dispatch a new request if appropriate"
	default:
		return ""
	}
}

func dispatchLifecycleRecordFromJournal(j dispatchRequestJournal) dispatchLifecycleRecord {
	migrateDispatchLifecycle(&j)
	return dispatchLifecycleRecord{
		RequestID: j.RequestID, State: j.State, Outcome: j.Outcome,
		NextAction: dispatchLifecycleNextAction(j.State, j.RequestID), Repo: j.Repo,
		Issue: j.Issue, Ref: j.Ref, Role: j.Role, Harness: j.Harness, Workflow: j.Workflow,
		ClusterID: j.BrokerID, ContainerID: j.ContainerID, AcceptedAt: j.AcceptedAt,
		UpdatedAt: j.UpdatedAt, LastTransition: j.LastTransition,
		TerminalReason: j.TerminalReason, DiagnosticPhase: j.Phase, LogPath: j.Paths.ConsolePath,
	}
}

func dispatchJournalDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(envDispatchJournalDir)); dir != "" {
		return dir, nil
	}
	global, err := config.GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(global, dispatchJournalsSubdir), nil
}

func readAllDispatchJournals() ([]dispatchRequestJournal, error) {
	dir, err := dispatchJournalDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []dispatchRequestJournal{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]dispatchRequestJournal, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		journal, err := readDispatchJournal(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read dispatch lifecycle %s: %w", entry.Name(), err)
		}
		migrateDispatchLifecycle(&journal)
		items = append(items, journal)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func dispatchCommand() *cli.Command {
	return &cli.Command{Name: "dispatch", Usage: "Inspect and explicitly prune durable broker request lifecycles.", Commands: []*cli.Command{
		dispatchListCommand(), dispatchStatusCommand(), dispatchPruneCommand(),
	}}
}

func dispatchListCommand() *cli.Command {
	return &cli.Command{Name: "list", Usage: "List retained broker request lifecycles.", Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "emit JSON"}}, Action: func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{Name: "agent.dispatch.list", SkipPolicy: true, Action: func(context.Context, *cli.Command) error {
			if addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)); addr != "" {
				return r.forwardDispatchLifecycleRead(ctx, addr, dispatchActionLifecycleList, "", c.Bool("json"))
			}
			journals, err := readAllDispatchJournals()
			if err != nil {
				return err
			}
			records := make([]dispatchLifecycleRecord, 0, len(journals))
			for _, journal := range journals {
				records = append(records, dispatchLifecycleRecordFromJournal(journal))
			}
			return writeDispatchLifecycleRecords(c, records)
		}}, r.Audit)(ctx, c)
	}}
}

func dispatchStatusCommand() *cli.Command {
	return &cli.Command{Name: "status", Usage: "Show one retained broker request lifecycle.", ArgsUsage: "<request-id>", Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "emit JSON"}}, Action: func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{Name: "agent.dispatch.status", SkipPolicy: true, Action: func(context.Context, *cli.Command) error {
			requestID := strings.TrimSpace(c.Args().First())
			if addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)); addr != "" {
				return r.forwardDispatchLifecycleRead(ctx, addr, dispatchActionLifecycleStatus, requestID, c.Bool("json"))
			}
			path, err := dispatchJournalPath(requestID)
			if err != nil {
				return err
			}
			journal, err := readDispatchJournal(path)
			if err != nil {
				return fmt.Errorf("dispatch request %s: %w", requestID, err)
			}
			return writeDispatchLifecycleRecords(c, []dispatchLifecycleRecord{dispatchLifecycleRecordFromJournal(journal)})
		}}, r.Audit)(ctx, c)
	}}
}

func (r *Runner) forwardDispatchLifecycleRead(ctx context.Context, addr, action, requestID string, jsonOut bool) error {
	req := dispatchBrokerRequest{
		Action: action, Target: requestID,
		Format:    map[bool]string{true: "json", false: "text"}[jsonOut],
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	body, err := sendDispatchBrokerListRequest(ctx, addr, req)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	_, err = io.Copy(r.Runner.Stdout, body)
	return err
}

func (r *Runner) runDispatchBrokerLifecycleRead(conn net.Conn, req dispatchBrokerRequest) {
	journals, err := readAllDispatchJournals()
	if err != nil {
		writeDispatchBrokerResponse(conn, err)
		return
	}
	if dispatchAction(req.Action) == dispatchActionLifecycleStatus {
		requestID := strings.TrimSpace(req.Target)
		if !dispatchRequestIDPattern.MatchString(requestID) {
			writeDispatchBrokerResponse(conn, fmt.Errorf("dispatch broker: invalid request id %q", requestID))
			return
		}
		filtered := journals[:0]
		for _, journal := range journals {
			if journal.RequestID == requestID {
				filtered = append(filtered, journal)
			}
		}
		journals = filtered
		if len(journals) == 0 {
			err = fmt.Errorf("dispatch request %s: %w", requestID, os.ErrNotExist)
		}
	}
	if err != nil {
		writeDispatchBrokerResponse(conn, err)
		return
	}
	writeDispatchBrokerResponse(conn, nil)
	records := make([]dispatchLifecycleRecord, 0, len(journals))
	for _, journal := range journals {
		records = append(records, dispatchLifecycleRecordFromJournal(journal))
	}
	if strings.TrimSpace(req.Format) == "json" {
		payload, _ := json.MarshalIndent(dispatchLifecycleJSON(records), "", "  ")
		_, _ = conn.Write(append(payload, '\n'))
	} else {
		_, _ = io.WriteString(conn, renderDispatchLifecycleHuman(records))
	}
}

func dispatchPruneCommand() *cli.Command {
	return &cli.Command{Name: "prune", Usage: "Select terminal request summaries older than 30d, deleting only with --confirm.", Flags: []cli.Flag{
		&cli.DurationFlag{Name: "older-than", Value: 30 * 24 * time.Hour}, &cli.BoolFlag{Name: "confirm"}, &cli.BoolFlag{Name: "json", Usage: "emit JSON"},
	}, Action: func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{Name: "agent.dispatch.prune", SkipPolicy: true, Action: func(context.Context, *cli.Command) error {
			return runDispatchLifecyclePrune(c)
		}}, r.Audit)(ctx, c)
	}}
}

func writeDispatchLifecycleRecords(c *cli.Command, records []dispatchLifecycleRecord) error {
	w := agentCommandWriter(c)
	if c.Bool("json") {
		return json.NewEncoder(w).Encode(dispatchLifecycleJSON(records))
	}
	_, err := fmt.Fprint(w, renderDispatchLifecycleHuman(records))
	return err
}

type dispatchLifecycleListJSON struct {
	SchemaVersion int                       `json:"schema_version"`
	Requests      []dispatchLifecycleRecord `json:"requests"`
}

func dispatchLifecycleJSON(records []dispatchLifecycleRecord) dispatchLifecycleListJSON {
	return dispatchLifecycleListJSON{SchemaVersion: dispatchJournalVersion, Requests: records}
}

func renderDispatchLifecycleHuman(records []dispatchLifecycleRecord) string {
	var b strings.Builder
	if len(records) == 0 {
		return "state: none\nnext action: none\n"
	}
	for i, record := range records {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "state: %s\n", record.State)
		fmt.Fprintf(&b, "request: %s\n", record.RequestID)
		fmt.Fprintf(&b, "target: %s\n", emptyDefault(record.Ref, emptyDefault(record.Repo, "-")))
		fmt.Fprintf(&b, "role: %s  harness: %s  workflow: %s\n", emptyDefault(record.Role, "-"), emptyDefault(record.Harness, "-"), emptyDefault(record.Workflow, "-"))
		fmt.Fprintf(&b, "last transition: %s at %s (%s)\n", record.LastTransition.To, formatJSONTime(record.LastTransition.At), emptyDefault(record.LastTransition.ReasonCode, "unspecified"))
		if record.TerminalReason != "" {
			fmt.Fprintf(&b, "terminal reason: %s\n", record.TerminalReason)
		}
		fmt.Fprintf(&b, "next action: %s\n", emptyDefault(record.NextAction, "none"))
	}
	return b.String()
}

func runDispatchLifecyclePrune(c *cli.Command) error {
	olderThan := c.Duration("older-than")
	if olderThan < 0 {
		return fmt.Errorf("ward agent dispatch prune: --older-than must be non-negative")
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	journals, err := readAllDispatchJournals()
	if err != nil {
		return err
	}
	selectedJournals := dispatchLifecyclePruneCandidates(journals, cutoff)
	selected := make([]dispatchLifecycleRecord, 0, len(selectedJournals))
	for _, journal := range selectedJournals {
		selected = append(selected, dispatchLifecycleRecordFromJournal(journal))
		if c.Bool("confirm") {
			if err := pruneDispatchLifecycleRecord(journal); err != nil {
				return err
			}
		}
	}
	if c.Bool("json") {
		return json.NewEncoder(agentCommandWriter(c)).Encode(map[string]any{"confirmed": c.Bool("confirm"), "count": len(selected), "requests": selected})
	}
	verb := "would prune"
	if c.Bool("confirm") {
		verb = "pruned"
	}
	_, err = fmt.Fprintf(agentCommandWriter(c), "%s %d terminal dispatch request(s) older than %s\n", verb, len(selected), olderThan)
	return err
}

func pruneDispatchLifecycleRecord(journal dispatchRequestJournal) error {
	path, err := dispatchJournalPath(journal.RequestID)
	if err != nil {
		return err
	}
	artifactDir := ""
	if journal.Paths.Dir != "" {
		artifactDir, err = validatedDispatchLifecycleArtifactDir(journal.Paths.Dir)
		if err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if artifactDir == "" {
		return nil
	}
	return os.RemoveAll(artifactDir)
}

func validatedDispatchLifecycleArtifactDir(dir string) (string, error) {
	root, err := filepath.Abs(filepath.Join(agentLogsDir(), dispatchArtifactsSubdir))
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("ward agent dispatch prune: refusing artifact path outside %s: %s", root, target)
	}
	return target, nil
}

func dispatchLifecyclePruneCandidates(journals []dispatchRequestJournal, cutoff time.Time) []dispatchRequestJournal {
	selected := make([]dispatchRequestJournal, 0)
	for _, journal := range journals {
		migrateDispatchLifecycle(&journal)
		if dispatchLifecycleTerminal(journal.State) && !journal.UpdatedAt.After(cutoff) {
			selected = append(selected, journal)
		}
	}
	return selected
}

func writeDispatchLifecycleArtifact(journal dispatchRequestJournal) {
	if strings.TrimSpace(journal.Paths.MetaPath) == "" {
		return
	}
	body, err := os.ReadFile(journal.Paths.MetaPath) // #nosec G304 -- Ward-owned safe artifact path
	if err != nil {
		return
	}
	var meta dispatchArtifactMeta
	if json.Unmarshal(body, &meta) != nil {
		return
	}
	meta.State = journal.State
	meta.LastTransition = journal.LastTransition
	meta.TerminalReason = journal.TerminalReason
	writeDispatchArtifactJSON(journal.Paths.MetaPath, meta)
}

func updateDispatchLifecycleFromDrain(requestID, normalized string, drainErr error) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	path, err := dispatchJournalPath(requestID)
	if err != nil {
		return err
	}
	lock := dispatchRequestLock(requestID)
	lock.Lock()
	defer lock.Unlock()
	journal, err := readDispatchJournal(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	migrateDispatchLifecycle(&journal)
	state, reason := dispatchTerminalStateForDrain(normalized, drainErr)
	from := journal.State
	journal.State = state
	journal.TerminalReason = reason
	journal.LastTransition = dispatchLifecycleTransition{From: from, To: state, At: time.Now().UTC(), ReasonCode: reason}
	if state == dispatchStateCleanupNeeded {
		journal.Outcome = dispatchStateCleanupNeeded
	} else {
		journal.Outcome = state
	}
	journal.Phase = dispatchPhaseTerminal
	clearDispatchRecoveryPayload(&journal.Request)
	if drainErr != nil {
		journal.Error = redactSecrets(firstLine(drainErr.Error()))
	}
	if err := writeDispatchJournal(path, journal); err != nil {
		return err
	}
	writeDispatchLifecycleArtifact(journal)
	return nil
}

func dispatchTerminalStateForDrain(normalized string, drainErr error) (string, string) {
	if drainErr != nil {
		return dispatchStateCleanupNeeded, "secret-safe-drain-failed"
	}
	switch strings.TrimSpace(normalized) {
	case "blocked":
		return dispatchStateBlocked, "run-blocked"
	case "failed", "prelaunch-failure":
		return dispatchStateFailed, "run-failed"
	default:
		return dispatchStateCompleted, "run-finished"
	}
}
