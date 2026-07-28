package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	osExec "os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/version"
	"github.com/urfave/cli/v3"
)

// Director surfaces ask this independently supervised service to launch sibling
// engineer and QA runs over their Compose network (ward#378, ward#1562).

const (
	// envDispatchBrokerAddr carries the broker endpoint the surface dials.
	// envDispatchBrokerToken is the per-stack nonce it echoes back.
	envDispatchBrokerAddr  = "WARD_DISPATCH_BROKER_ADDR"
	envDispatchBrokerToken = "WARD_DISPATCH_BROKER_TOKEN"
)

var errDispatchBrokerUnavailable = errors.New("dispatch broker unavailable")

type dispatchBrokerDiagnosticKind string

const (
	dispatchBrokerDiagnosticBrokerUnreachable dispatchBrokerDiagnosticKind = "broker-unreachable"
	dispatchBrokerDiagnosticBrokerTimeout     dispatchBrokerDiagnosticKind = "broker-timeout"
)

// dispatchBrokerDiagnostic carries the operator-facing classification for a
// broker contact failure.
type dispatchBrokerDiagnostic struct {
	kind        dispatchBrokerDiagnosticKind
	addrSource  string
	addr        string
	connection  string
	remediation string
}

func (d *dispatchBrokerDiagnostic) Error() string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ward dispatch broker: %s\n", d.kind)
	fmt.Fprintf(&b, "address source: %s=%s\n", d.addrSource, d.addr)
	if conn := strings.TrimSpace(d.connection); conn != "" {
		fmt.Fprintf(&b, "connection: %s\n", conn)
	}
	if remediation := strings.TrimSpace(d.remediation); remediation != "" {
		fmt.Fprintf(&b, "remediation: %s", remediation)
	}
	return strings.TrimSpace(b.String())
}

func (d *dispatchBrokerDiagnostic) Unwrap() error {
	return errDispatchBrokerUnavailable
}

// dispatchBrokerDialContext is a test hook for the broker dial path.
var dispatchBrokerDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// dispatchBrokerProbeTimeout bounds the reachability check we use before
// choosing the brokered path on a read-only surface.
const dispatchBrokerProbeTimeout = 250 * time.Millisecond

// dispatchBrokerLaunchMu serializes the process-global env override while the host
// broker kicks off a forwarded run so one launch cannot leak into another.
var dispatchBrokerLaunchMu sync.Mutex

// dispatchBrokerLaunch runs the validated request's role-specific launch.
// Tests override this hook to keep the broker path fast and observable.
var dispatchBrokerLaunch = func(ctx context.Context, req dispatchBrokerRequest) error {
	switch req.Role {
	case "engineer":
		return agentEngineerCommand().Run(ctx, req.Argv)
	case "qa":
		return agentQACommand().Run(ctx, req.Argv)
	default:
		return fmt.Errorf("role %q is not dispatchable", req.Role)
	}
}

// dispatchBrokerVisibilityTimeout bounds the post-launch wait for a forwarded
// engineer run to show up in the director-facing list.
var dispatchBrokerVisibilityTimeout = 10 * time.Second

// dispatchBrokerVisibilityPoll is the cadence for checking whether a forwarded
// engineer launch is visible yet.
var dispatchBrokerVisibilityPoll = 100 * time.Millisecond

const (
	// dispatchActionLaunch is the default action: launch a sibling engineer/QA
	// run. An empty Action normalizes to it, keeping older launch requests byte-compatible.
	dispatchActionLaunch = "launch"
	// dispatchActionStop stops one engineer or clears one confirmed stale issue-ref
	// launch record. The action accepts no launch argv (ward#627, ward#1502).
	dispatchActionStop = "stop"
	// dispatchActionList is the read-only control action: list running engineers.
	dispatchActionList = "list"
	// dispatchActionLogs streams one engineer's logs back to the requester.
	dispatchActionLogs = "logs"
	// dispatchActionPing proves that the broker protocol, token, and listener are
	// live. Compose uses it for the broker service health check (ward#1562).
	dispatchActionPing = "ping"
	// dispatchActionPRStatus reads one PR head's combined CI status natively (ward#1067).
	dispatchActionPRStatus = "pr-status"
	// dispatchActionPRLogs reads one PR CI log stream through the native status hook.
	dispatchActionPRLogs = "pr-logs"
	// dispatchActionPRMerge merges one PR through ward's compiled client, gated by
	// the embedded role x workflow permission table (ward#1067).
	dispatchActionPRMerge = "pr-merge"
	// dispatchActionPRClose closes one PR through ward's compiled client.
	dispatchActionPRClose = "pr-close"
	// dispatchActionPRReopen reopens one PR through ward's compiled client.
	dispatchActionPRReopen = "pr-reopen"
	// dispatchActionPRRecover diagnoses a closed-unmerged PR through ward's compiled client.
	dispatchActionPRRecover = "pr-recover"
	// dispatchActionCIRuns lists a repo's Actions runs with conclusions (ward#1067).
	dispatchActionCIRuns = "ci-runs"
	// dispatchActionCIRerun reruns one Actions run natively (ward#1067).
	dispatchActionCIRerun = "ci-rerun"
)

const staleLaunchCleanupResultPrefix = "stale-launch-cleared:"

type staleEngineerLaunchError struct {
	hold stalePrelaunchReservation
}

func (e *staleEngineerLaunchError) Error() string {
	return fmt.Sprintf("dispatch broker: %q is a cleanup-needed launch record: no running container exists and the launch-confirmation TTL elapsed", e.hold.Ref())
}

// prWorkflowDispatchActions is the ward#1067 action set: PR-workflow verbs the
// broker serves natively, host-side, on ward's compiled Forgejo client.
var prWorkflowDispatchActions = map[string]bool{
	dispatchActionPRStatus:  true,
	dispatchActionPRLogs:    true,
	dispatchActionPRMerge:   true,
	dispatchActionPRClose:   true,
	dispatchActionPRReopen:  true,
	dispatchActionPRRecover: true,
	dispatchActionCIRuns:    true,
	dispatchActionCIRerun:   true,
}

type dispatchBrokerRequest struct {
	// RequestID is minted by the caller before a launch crosses the broker
	// boundary. It is the idempotency key for a lost response or reconnect.
	RequestID string `json:"request_id,omitempty"`
	// Action discriminates a launch (default/empty) from a stop control action
	// (ward#627); an empty value is treated as launch for back-compat.
	Action string   `json:"action,omitempty"`
	Role   string   `json:"role"`
	Argv   []string `json:"argv"`
	// Target names the stop action's container: owner/repo#N (resolved by labels) or
	// a container name. Empty on a launch request (ward#627).
	Target    string `json:"target,omitempty"`
	Format    string `json:"format,omitempty"`
	Requester string `json:"requester,omitempty"`
	Tail      int    `json:"tail,omitempty"`
	Follow    bool   `json:"follow,omitempty"`
	// Preview asks the stop action to resolve the target without stopping it.
	Preview bool `json:"preview,omitempty"`
	// RunID names the Actions run a ci-rerun acts on (ward#1067).
	RunID int64 `json:"run_id,omitempty"`
	// Limit caps a ci-runs read (ward#1067).
	Limit int `json:"limit,omitempty"`
	// MergeStyle names the Forgejo merge style for pr-merge requests.
	MergeStyle string `json:"merge_style,omitempty"`
	// Reason carries the required close reason for pr-close requests.
	Reason string `json:"reason,omitempty"`
	// Supersedes carries the superseding issue/PR ref for pr-close requests.
	Supersedes string `json:"supersedes,omitempty"`
	// Context names a PR status/log context for follow-up requests.
	Context string `json:"context,omitempty"`
	// Head pins a PR status request to a specific head SHA when wait wants fail-fast.
	Head string `json:"head,omitempty"`
	// Token is the per-launch shared secret the surface echoes back so the host
	// broker authenticates the dial (the TCP port has no socket file perms).
	Token string `json:"token,omitempty"`
	// BrokerID is stamped by the accepting service so only that Compose project
	// reconciles the request after a restart.
	BrokerID string `json:"broker_id,omitempty"`
	// JournalPath, ResumeContainer, and Recovery are broker-local recovery state.
	// They never cross the protocol or enter the token-stripped request journal.
	JournalPath     string `json:"-"`
	ResumeContainer string `json:"-"`
	Recovery        bool   `json:"-"`
}

// dispatchAction normalizes the request's action, defaulting an empty value to
// launch so back-compat launch requests (no Action field) still route (ward#627).
func dispatchAction(a string) string {
	if strings.TrimSpace(a) == "" {
		return dispatchActionLaunch
	}
	return a
}

type dispatchBrokerResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Phase     string `json:"phase,omitempty"`
	// Source names the log source the host used for a logs request.
	Source string `json:"source,omitempty"`
	// LogPath is the host path the served run's stdout/stderr were redirected to,
	// so the requesting surface can name it without any bytes hitting the TTY (ward#389).
	LogPath string `json:"log_path,omitempty"`
}

// dispatchStdioMu serializes the process-global os.Stdout/os.Stderr swap that keeps
// a served run's deploy output off the shared read-only TUI (ward#389).
var dispatchStdioMu sync.Mutex

// dispatchStdioRestoreHook is a test hook that fires after a detached launch has
// restored process stdio and closed its dispatch log.
var dispatchStdioRestoreHook = func() {}

// dispatchFailedDispatchLaunchHook lets tests bypass the slow real failure-comment
// recovery path when they only care about the broker's structured response.
var dispatchFailedDispatchLaunchHook = func(dispatchBrokerRequest, string, error) bool { return false }

// dispatchFailedDispatchLaunchStartHook lets tests wait until the detached failure
// branch has definitely entered its recovery path.
var dispatchFailedDispatchLaunchStartHook = func() {}

// dispatchRefLocks holds one mutex per issue ref so the broker serializes same-ref
// dispatches before any container starts (ward#600, docs/agent-reservation.md).
var dispatchRefLocks sync.Map // ref string -> *sync.Mutex

// dispatchRefLock returns the shared mutex for ref, creating it on first use.
func dispatchRefLock(ref string) *sync.Mutex {
	m, _ := dispatchRefLocks.LoadOrStore(ref, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// dispatchLogsSubdir is the per-host dir under ~/.ward/agent-logs (agentLogsDir)
// that groups one directory-backed artifact per forwarded request.
const dispatchLogsSubdir = "dispatch"

// newDispatchBrokerToken mints a 256-bit hex nonce as the per-launch shared
// secret guarding the Compose-network TCP transport.
func newDispatchBrokerToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (r *Runner) serveHostDispatchBroker(ctx context.Context, ln net.Listener, requester, token string) {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Fprintf(os.Stderr, "ward dispatch broker: accept: %v\n", err)
			continue
		}
		go r.handleHostDispatchBrokerConn(ctx, conn, requester, token)
	}
}

func (r *Runner) handleHostDispatchBrokerConn(ctx context.Context, conn net.Conn, requester, token string) {
	defer func() {
		if p := recover(); p != nil {
			err := dispatchBrokerPanicError("request handler", p)
			fmt.Fprintf(os.Stderr, "ward dispatch broker: %v\n", err)
			writeDispatchBrokerResponse(conn, "", "", "", err)
		}
		_ = conn.Close()
	}()
	var req dispatchBrokerRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeDispatchBrokerResponse(conn, "", "", "", fmt.Errorf("decode request: %w", err))
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		writeDispatchBrokerResponse(conn, "", "", "", errors.New("dispatch broker: token rejected"))
		return
	}
	if req.Requester == "" {
		req.Requester = requester
	}
	req.BrokerID = strings.TrimSpace(os.Getenv(envDispatchBrokerID))
	if dispatchAction(req.Action) == dispatchActionPing {
		writeDispatchBrokerResponse(conn, "", "", "", nil)
		return
	}
	if dispatchAction(req.Action) == dispatchActionLogs {
		r.runDispatchBrokerLogs(ctx, conn, req)
		return
	}
	if dispatchAction(req.Action) == dispatchActionList {
		r.runDispatchBrokerList(ctx, conn, req)
		return
	}
	if prWorkflowDispatchActions[dispatchAction(req.Action)] {
		r.runDispatchBrokerPRWorkflow(ctx, conn, req)
		return
	}
	if dispatchAction(req.Action) == dispatchActionLaunch && strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = newDispatchBrokerRequestID()
	}
	logPath, err := r.startHostDispatchBrokerRequest(ctx, req)
	writeDispatchBrokerResponse(conn, logPath, req.RequestID, dispatchPhaseAccepted, err)
}

func writeDispatchBrokerResponse(conn net.Conn, logPath, requestID, phase string, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, LogPath: logPath, RequestID: requestID, Phase: phase}
	if err != nil {
		resp.Error = err.Error()
	}
	if data, merr := json.Marshal(resp); merr == nil {
		_, _ = conn.Write(data)
	}
}

// writeDispatchBrokerLogsResponse writes the response header for a logs request.
func writeDispatchBrokerLogsResponse(conn net.Conn, source string, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, Source: source}
	if err != nil {
		resp.Error = err.Error()
	}
	if data, merr := json.Marshal(resp); merr == nil {
		_, _ = conn.Write(data)
	}
}

// startHostDispatchBrokerRequest returns after the broker-owned Ward launch starts.
// Later milestones remain asynchronous and are recorded in the dispatch artifact.
func (r *Runner) startHostDispatchBrokerRequest(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	if err := validateDispatchBrokerRequest(req); err != nil {
		return "", err
	}
	if dispatchAction(req.Action) == dispatchActionStop {
		if req.Preview {
			return r.runDispatchBrokerStopPreview(ctx, req)
		}
		return r.runDispatchBrokerStop(ctx, req)
	}
	var lock *sync.Mutex
	if ref, err := parseAgentIssueRef(req.Argv[1]); err == nil {
		lock = dispatchRefLock(ref.String())
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = newDispatchBrokerRequestID()
	}
	paths, logf, journalPath, existing, err := acceptDispatchLaunch(req)
	if err != nil || existing {
		return paths.ConsolePath, err
	}
	req.JournalPath = journalPath
	logDispatchDecision(logf, "broker", "request-accepted", "request_id=%s action=%s role=%s requester=%s argv=%s",
		paths.RequestID, dispatchAction(req.Action), req.Role, emptyDefault(req.Requester, "unknown-container"), redactDispatchBrokerArgv(req.Argv))
	_, _ = fmt.Fprintf(logf, "ward dispatch broker: request id: %s\n", paths.RequestID)
	_, _ = fmt.Fprintf(logf, "ward dispatch broker: %s requested `ward agent %s`\n",
		emptyDefault(req.Requester, "unknown-container"), redactDispatchBrokerArgv(req.Argv))
	if v := dispatchBrokerWardVersion(req.Argv); v != "" {
		_, _ = fmt.Fprintf(logf, "ward dispatch broker: effective ward version for this launch: %s\n", v)
	}
	ref := ""
	if len(req.Argv) >= 2 {
		ref = req.Argv[1]
	}
	_, _ = fmt.Fprintf(logf, "ward dispatch broker: this log captures the broker wrapper only; in-container engineer console/reap logs drain separately and are readable with `ward agent logs %s`\n",
		ref)
	_, _ = fmt.Fprintf(logf, "ward dispatch broker: broker accepted launch request (request id %s; artifact %s)\n", paths.RequestID, paths.Dir)
	restore := func() {}
	if !dispatchBrokerServiceMode() {
		logDispatchDecision(logf, "broker", "stdio-routing", "mode=in-process redirect=artifact")
		restore = redirectStdioToLog(logf)
	} else {
		logDispatchDecision(logf, "broker", "stdio-routing", "mode=service child_stdout=artifact")
	}
	started := make(chan struct{})
	go r.handleHostDispatchBrokerLaunch(ctx, req, paths, logf, restore, lock, started)
	return waitForDispatchBrokerLaunchStart(ctx, paths.ConsolePath, started)
}

//nolint:funlen,gocognit,gocyclo,cyclop // lifecycle branches
func (r *Runner) handleHostDispatchBrokerLaunch(ctx context.Context, req dispatchBrokerRequest, paths dispatchArtifactPaths, logf *os.File, restore func(), lock *sync.Mutex, started chan struct{}) {
	restored := false
	finalized := false
	resultErr := error(nil)
	defer func() {
		if p := recover(); p != nil {
			resultErr = dispatchBrokerPanicError("launch worker", p)
			if !restored {
				restore()
				restored = true
			}
			// Persist the terminal artifact before announcing the failure, so its
			// durable summary cannot still say in-progress during recovery.
			if !finalized {
				finalizeDispatchArtifact(paths, req, paths.ConsolePath, resultErr)
				finalized = true
			}
			dispatchFailedDispatchLaunchStartHook()
			r.commentDispatchLaunchError(ctx, req, paths.ConsolePath, resultErr)
		}
		if !restored {
			restore()
		}
		if !finalized {
			finalizeDispatchArtifact(paths, req, paths.ConsolePath, resultErr)
		}
		outcome := dispatchOutcomeLaunched
		if resultErr != nil {
			outcome = dispatchOutcomeFailed
		}
		if err := updateDispatchJournal(req.JournalPath, dispatchPhaseTerminal, "", outcome, resultErr); err != nil {
			_, _ = fmt.Fprintf(logf, "ward dispatch broker: persist terminal request state: %v\n", err)
		}
		_ = logf.Close()
		dispatchStdioRestoreHook()
	}()
	// This is the detach boundary; later milestones remain asynchronous.
	logDispatchDecision(logf, "broker", "detach-boundary", "response=accepted before container visibility and harness startup")
	fmt.Fprintln(os.Stderr, "ward dispatch broker: broker Ward launch started; response detaches before container visibility and engineer harness start")
	close(started)
	if lock != nil {
		logDispatchDecision(logf, "broker", "ref-lock", "serializing same-issue launch for %s", emptyDefault(argRef(req.Argv), "(unknown-ref)"))
		lock.Lock()
		defer lock.Unlock()
		logDispatchDecision(logf, "broker", "ref-lock-acquired", "same-issue launch lock acquired")
	} else {
		logDispatchDecision(logf, "broker", "ref-lock", "skipped: request ref did not parse")
	}
	logDispatchDecision(logf, "broker", "phase", "persisting %s", dispatchPhasePreflight)
	if err := updateDispatchJournal(req.JournalPath, dispatchPhasePreflight, "", dispatchOutcomeInProgress, nil); err != nil {
		resultErr = fmt.Errorf("dispatch broker: persist preflight phase: %w", err)
		return
	}
	logDispatchDecision(logf, "broker", "backpressure-open-pr", "checking broker-time repo queue gate")
	if err := r.dispatchBrokerOpenPRBackpressureCheck(ctx, req, agentCmdline(dispatchBrokerRequestMode(req), req.Role)); err != nil {
		logDispatchDecision(logf, "broker", "backpressure-open-pr", "deferred: %s", firstLine(err.Error()))
		resultErr = err
		r.finishDispatchBrokerLaunchFailure(ctx, req, paths, restore, &restored, &finalized, err, true)
		return
	}
	logDispatchDecision(logf, "broker", "backpressure-open-pr", "passed")
	launchCtx := withDispatchLaunchReservationTracking(ctx)
	var launchErr error
	if dispatchBrokerServiceMode() {
		logDispatchDecision(logf, "broker", "launch-runner", "mode=service child process")
		launchErr = runDispatchBrokerChild(launchCtx, req, logf)
	} else {
		logDispatchDecision(logf, "broker", "launch-runner", "mode=in-process broker forwarding disabled")
		launchErr = withBrokerForwardingDisabled(req, func() error {
			return dispatchBrokerLaunch(launchCtx, req)
		})
	}
	if launchErr != nil {
		err := launchErr
		if isPartialLaunchError(err) {
			logDispatchDecision(logf, "broker", "launch-result", "partial launch: %s", firstLine(err.Error()))
			resultErr = err
			r.finishDispatchBrokerLaunchFailure(ctx, req, paths, restore, &restored, &finalized, err, false)
			return
		}
		logDispatchDecision(logf, "broker", "launch-result", "failed before confirmed visibility: %s", firstLine(err.Error()))
		resultErr = err
		r.finishDispatchBrokerLaunchFailure(ctx, req, paths, restore, &restored, &finalized, err, true)
		return
	}
	logDispatchDecision(logf, "broker", "launch-result", "host launch command returned; checking visibility")
	if err := r.maybeHandleDispatchBrokerEngineerVisibility(ctx, req, paths, logf, restore, &restored, &finalized); err != nil {
		resultErr = err
		return
	}
	fmt.Fprintln(os.Stderr, "ward dispatch broker: broker launch completed; container visibility was confirmed, but engineer harness startup remains in-container")
	if err := updateDispatchJournal(req.JournalPath, dispatchPhaseVisible, "", dispatchOutcomeInProgress, nil); err != nil {
		resultErr = fmt.Errorf("dispatch broker: persist visible phase: %w", err)
		return
	}
	finalizeDispatchArtifact(paths, req, paths.ConsolePath, nil)
	finalized = true
}

func dispatchBrokerServiceMode() bool {
	return strings.TrimSpace(os.Getenv(envContainerService)) == dispatchBrokerService
}

func runDispatchBrokerChild(ctx context.Context, req dispatchBrokerRequest, logf io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("dispatch broker: resolve Ward executable: %w", err)
	}
	argv := append([]string{"agent"}, req.Argv...)
	if req.Recovery && req.Role == roleEngineer && !stringSliceContains(argv, "--override-reservation") {
		argv = append(argv, "--override-reservation")
	}
	cmd := osExec.CommandContext(ctx, executable, argv...)
	cmd.Stdin = nil
	cmd.Stdout = logf
	cmd.Stderr = logf
	env := removeEnvKeys(os.Environ(),
		"WARD_READONLY",
		envDispatchBrokerAddr,
		envDispatchBrokerToken,
		envContainerService,
		envDispatchBrokerListen,
		envDispatchBrokerRequester,
		envDispatchBrokerID,
	)
	env = append(env,
		envDispatchRequestID+"="+req.RequestID,
		envDispatchJournalPath+"="+req.JournalPath,
	)
	if req.ResumeContainer != "" {
		env = append(env, envDispatchResumeContainer+"="+req.ResumeContainer)
	}
	cmd.Env = env
	return cmd.Run()
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func removeEnvKeys(env []string, keys ...string) []string {
	remove := make(map[string]bool, len(keys))
	for _, key := range keys {
		remove[key] = true
	}
	out := make([]string, 0, len(env))
	for _, line := range env {
		key, _, _ := strings.Cut(line, "=")
		if !remove[key] {
			out = append(out, line)
		}
	}
	return out
}

func (r *Runner) maybeHandleDispatchBrokerEngineerVisibility(ctx context.Context, req dispatchBrokerRequest, paths dispatchArtifactPaths, logf io.Writer, restore func(), restored, finalized *bool) error {
	if dispatchAction(req.Action) != dispatchActionLaunch || req.Role != roleEngineer {
		logDispatchDecision(logf, "broker", "visibility", "skipped for action=%s role=%s", dispatchAction(req.Action), req.Role)
		return nil
	}
	if err := r.waitForDispatchBrokerEngineerVisibility(ctx, req, logf); err != nil {
		r.finishDispatchBrokerLaunchFailure(ctx, req, paths, restore, restored, finalized, err, true)
		return err
	}
	return nil
}

func (r *Runner) finishDispatchBrokerLaunchFailure(ctx context.Context, req dispatchBrokerRequest, paths dispatchArtifactPaths, restore func(), restored, finalized *bool, err error, notify bool) {
	if !*restored {
		restore()
		*restored = true
	}
	// Finalize the durable failure record before visible recovery work, so its
	// summary cannot say in-progress during recovery.
	finalizeDispatchArtifact(paths, req, paths.ConsolePath, err)
	*finalized = true
	if notify {
		dispatchFailedDispatchLaunchStartHook()
		r.commentDispatchLaunchError(ctx, req, paths.ConsolePath, err)
	}
}

func dispatchBrokerPanicError(stage string, p any) error {
	msg := strings.ToLower(fmt.Sprint(p))
	if strings.Contains(msg, "exit status 125") || (strings.Contains(msg, "container name") && strings.Contains(msg, "already in use")) {
		return fmt.Errorf("dispatch broker: %s request failure: %v", stage, p)
	}
	return fmt.Errorf("dispatch broker: %s panicked: %v", stage, p)
}

func waitForDispatchBrokerLaunchStart(ctx context.Context, logPath string, started <-chan struct{}) (string, error) {
	select {
	case <-started:
		return logPath, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// waitForDispatchBrokerEngineerVisibility polls until the forwarded engineer is
// visible in the director-facing list or the confirmation window expires.
func (r *Runner) waitForDispatchBrokerEngineerVisibility(ctx context.Context, req dispatchBrokerRequest, logf io.Writer) error {
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return err
	}
	logDispatchDecision(logf, "broker", "visibility", "polling for %s up to %s", ref, dispatchBrokerVisibilityTimeout)
	deadlineCtx, cancel := context.WithTimeout(ctx, dispatchBrokerVisibilityTimeout)
	defer cancel()
	ticker := time.NewTicker(dispatchBrokerVisibilityPoll)
	defer ticker.Stop()
	polls := 0
	for {
		polls++
		visible, err := r.dispatchBrokerEngineerVisible(deadlineCtx, ref)
		if err != nil {
			logDispatchDecision(logf, "broker", "visibility", "error after %d poll(s): %s", polls, firstLine(err.Error()))
			releaseDispatchLaunchReservation(ref)
			return fmt.Errorf(
				"dispatch broker: launch accepted but could not confirm engineer visibility; "+
					"inspect with `ward agent list` from the director surface: %w", err)
		}
		if visible {
			logDispatchDecision(logf, "broker", "visibility", "confirmed after %d poll(s)", polls)
			forgetDispatchLaunchReservationRelease(ref)
			return nil
		}
		if polls == 1 {
			logDispatchDecision(logf, "broker", "visibility", "not visible on first poll; suppressing repeated unchanged polls")
		}
		select {
		case <-deadlineCtx.Done():
			logDispatchDecision(logf, "broker", "visibility", "timed out after %d poll(s)", polls)
			releaseDispatchLaunchReservation(ref)
			return fmt.Errorf(
				"dispatch broker: launch accepted but the forwarded engineer never became visible; " +
					"inspect with `ward agent list` from the director surface")
		case <-ticker.C:
		}
	}
}

// dispatchBrokerEngineerVisible checks whether the expected engineer is visible
// in the host's running-engineer list.
func (r *Runner) dispatchBrokerEngineerVisible(ctx context.Context, ref agentIssueRef) (bool, error) {
	out, err := r.dockerCapture(ctx, "ps", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRole+"="+roleEngineer,
		"--filter", "label="+labelRepo+"="+ref.repoSlug(),
		"--filter", fmt.Sprintf("label=%s=%d", labelIssue, ref.Number))
	if err != nil {
		return false, fmt.Errorf("dispatch broker: check engineer visibility for %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// commentFailedDispatchLaunch posts the failure comment with a detached timeout.
func (r *Runner) commentFailedDispatchLaunch(ctx context.Context, req dispatchBrokerRequest, logPath string, launchErr error) {
	if dispatchFailedDispatchLaunchHook(req, logPath, launchErr) {
		return
	}
	if r == nil || r.Runner == nil {
		return
	}
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return
	}
	if r == nil || r.Runner == nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: skipping failure comment for %s: no shell runner available\n", ref)
		return
	}
	mode := dispatchBrokerRequestMode(req)
	commentCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	cl, cerr := r.hostTrackerClient(commentCtx, ref.trackerOrDefault(), mode)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not build issue client to comment failed dispatch on %s: %v\n", ref, cerr)
		return
	}
	r.commentFailedDispatch(commentCtx, cl, mode, ref, req, logPath, launchErr)
}

// commentDispatchLaunchError routes a launch refusal to the deferred or failed
// issue comment path after the broker worker has restored its stdio.
func (r *Runner) commentDispatchLaunchError(ctx context.Context, req dispatchBrokerRequest, logPath string, launchErr error) {
	if isPartialLaunchError(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: %v\n", launchErr)
		return
	}
	if isEngineerCapacityError(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: %s\n", engineerCapacityBackpressureSummary(launchErr))
		return
	}
	if isOpenPRBackpressureError(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: launch deferred: %v\n", launchErr)
		r.commentDeferredDispatchLaunch(ctx, req, logPath, launchErr)
		return
	}
	if isReleaseAssetsNotReadyError(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: launch deferred: %v\n", launchErr)
		r.commentDeferredReleaseAssetsLaunch(ctx, req, logPath, launchErr)
		return
	}
	if isReservationConflict(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: launch deferred: %v\n", launchErr)
		r.commentReservationConflictLaunch(ctx, req, logPath, launchErr)
		return
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: launch failed: %v\n", launchErr)
	r.commentFailedDispatchLaunch(ctx, req, logPath, launchErr)
}

// commentDeferredDispatchLaunch posts the capacity backpressure comment and clears
// the stale reservation after a forwarded launch was queued instead of started.
func (r *Runner) commentDeferredDispatchLaunch(ctx context.Context, req dispatchBrokerRequest, logPath string, launchErr error) {
	if r == nil || r.Runner == nil {
		return
	}
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return
	}
	mode := dispatchBrokerRequestMode(req)
	commentCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	cl, cerr := r.hostTrackerClient(commentCtx, ref.trackerOrDefault(), mode)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not build issue client to comment deferred dispatch on %s: %v\n", ref, cerr)
		return
	}
	r.commentDeferredDispatch(commentCtx, cl, mode, ref, req, logPath, launchErr)
}

// commentDeferredReleaseAssetsLaunch posts the release-assets-not-ready comment.
// It clears the stale reservation when the selected release's asset is missing.
func (r *Runner) commentDeferredReleaseAssetsLaunch(ctx context.Context, req dispatchBrokerRequest, logPath string, launchErr error) {
	if r == nil || r.Runner == nil {
		return
	}
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return
	}
	mode := dispatchBrokerRequestMode(req)
	commentCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	cl, cerr := r.hostTrackerClient(commentCtx, ref.trackerOrDefault(), mode)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not build issue client to comment deferred dispatch on %s: %v\n", ref, cerr)
		return
	}
	r.commentDeferredReleaseAssetsDispatch(commentCtx, cl, mode, ref, req, logPath, launchErr)
}

// commentReservationConflictLaunch posts the reservation-collision comment after a
// forwarded launch refused because another run still holds the issue (ward#1149, docs).
func (r *Runner) commentReservationConflictLaunch(ctx context.Context, req dispatchBrokerRequest, logPath string, launchErr error) {
	if r == nil || r.Runner == nil {
		return
	}
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return
	}
	mode := dispatchBrokerRequestMode(req)
	commentCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	cl, cerr := r.hostTrackerClient(commentCtx, ref.trackerOrDefault(), mode)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not build issue client to comment reservation-collision dispatch on %s: %v\n", ref, cerr)
		return
	}
	r.commentReservationConflictDispatch(commentCtx, cl, mode, ref, req, logPath, launchErr)
}

// commentReservationConflictDispatch writes the reservation-collision deferred
// comment: no container stop, no unlock, no release marker (ward#1149).
func (r *Runner) commentReservationConflictDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	body := dispatchLaunchReservationConflictCommentBody(mode, req, logPath, launchErr)
	if err := cl.CommentIssue(ctx, ref.Owner, ref.Repo, ref.Number, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not comment reservation-collision dispatch on %s: %v\n", ref, err)
		return
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: deferred dispatch on %s behind the live reservation (ward#1149)\n", ref)
}

// dispatchBrokerRequestMode resolves the requested harness for a forwarded dispatch.
func dispatchBrokerRequestMode(req dispatchBrokerRequest) containerMode {
	for i := 0; i+1 < len(req.Argv); i++ {
		switch req.Argv[i] {
		case "--harness", "--agent":
			if mode, err := parseMode(req.Argv[i+1]); err == nil {
				return mode
			}
		}
	}
	return currentAgentMode()
}

// commentFailedDispatch clears the stale reservation signal after a forwarded
// launch never became a running engineer, leaving the telemetry on the host log.
func (r *Runner) commentFailedDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	container := emptyDefault(req.Requester, "unknown-container")
	if req.Role == roleEngineer {
		container = issueScopedContainerName(req.Role, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	if !isDockerNameConflictError(launchErr) {
		r.stopFailedDispatchContainer(ctx, mode, ref, req.Role, container)
	}
	if err := cl.UnlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not unlock issue %s after failed dispatch: %v\n", ref, err)
	}
	deleteTransientWorkflowComments(ctx, cl, ref, time.Now().UTC())
	fmt.Fprintf(os.Stderr, "ward dispatch broker: released failed dispatch reservation on %s\n", ref)
	fmt.Fprintf(os.Stderr, "ward dispatch broker: failed dispatch telemetry stayed on the host log for %s (%s: %s)\n", ref, logPath, firstLine(launchErr.Error()))
}

// commentDeferredDispatch clears the stale reservation after a forwarded launch
// hit the global engineer cap, leaving the telemetry on the host log.
func (r *Runner) commentDeferredDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	container := emptyDefault(req.Requester, "unknown-container")
	if req.Role == roleEngineer {
		container = issueScopedContainerName(req.Role, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	r.stopFailedDispatchContainer(ctx, mode, ref, req.Role, container)
	if err := cl.UnlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not unlock issue %s after deferred dispatch: %v\n", ref, err)
	}
	deleteTransientWorkflowComments(ctx, cl, ref, time.Now().UTC())
	fmt.Fprintf(os.Stderr, "ward dispatch broker: released deferred dispatch reservation on %s\n", ref)
	fmt.Fprintf(os.Stderr, "ward dispatch broker: deferred dispatch telemetry stayed on the host log for %s (%s: %s)\n", ref, logPath, firstLine(launchErr.Error()))
}

// stopFailedDispatchContainer best-effort stops the attempted engineer container
// when a forwarded dispatch failed after the reservation decision was made.
func (r *Runner) stopFailedDispatchContainer(ctx context.Context, mode containerMode, ref agentIssueRef, role, container string) {
	if role != roleEngineer || r == nil || r.Runner == nil {
		return
	}
	name := strings.TrimSpace(container)
	if name == "" {
		name = issueScopedContainerName(roleEngineer, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	if !r.containerRunning(ctx, name) {
		return
	}
	if err := r.dockerExec(ctx, "stop", name); err != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not stop failed dispatch container %s: %v\n", name, err)
		return
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: stopped failed dispatch container %s\n", name)
}

// isDockerNameConflictError spots Docker's duplicate-name refusal.
// The live container already owns the name, so the failure handler must not stop it.
func isDockerNameConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "container name") && strings.Contains(msg, "already in use")
}

// commentDeferredReleaseAssetsDispatch clears the stale reservation after a
// published release still lacks the asset, leaving the telemetry on the host log.
func (r *Runner) commentDeferredReleaseAssetsDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	container := emptyDefault(req.Requester, "unknown-container")
	if req.Role == roleEngineer {
		container = issueScopedContainerName(req.Role, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	if err := cl.UnlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not unlock issue %s after deferred release-assets-not-ready dispatch: %v\n", ref, err)
	}
	deleteTransientWorkflowComments(ctx, cl, ref, time.Now().UTC())
	fmt.Fprintf(os.Stderr, "ward dispatch broker: released deferred release-assets-not-ready reservation on %s (%s)\n", ref, container)
	fmt.Fprintf(os.Stderr, "ward dispatch broker: release-assets telemetry stayed on the host log for %s (%s: %s)\n", ref, logPath, firstLine(launchErr.Error()))
}

// dispatchLaunchDeferredCommentBody supersedes the stale reservation with a visible
// deferred comment and the retry shape an operator needs when the cap is full.
func dispatchLaunchDeferredCommentBody(mode containerMode, container string, req dispatchBrokerRequest, logPath string, launchErr error) string {
	attempted := redactDispatchBrokerArgv(req.Argv)
	logDetail := "unavailable"
	if strings.TrimSpace(logPath) != "" {
		logDetail = logPath
	}
	detail := fmt.Sprintf(
		"This forwarded dispatch was deferred after the issue was already reserved.\n\n"+
			"Attempted harness: `%s`\n"+
			"Attempted run: `ward agent %s`\n"+
			"Container: `%s`\n"+
			"Container created: no running engineer was observed.\n"+
			"Host log: `%s`\n"+
			"Capacity: `%s`\n\n"+
			"Retry: the issue stays queued and the director will try again when a slot opens.",
		mode, attempted, container, logDetail, firstLine(launchErr.Error()))
	return agentReservationReleaseMarker + "\n" + agentNeedsRedispatchMarker + "\n" +
		collapsedIssueComment(workflowDispatchDeferredVisible(), "deferred details", detail)
}

// dispatchLaunchReservationConflictCommentBody renders the collision deferral: the
// needs-redispatch marker WITHOUT a release marker - the hold is the live run's (#1149).
func dispatchLaunchReservationConflictCommentBody(mode containerMode, req dispatchBrokerRequest, logPath string, launchErr error) string {
	attempted := redactDispatchBrokerArgv(req.Argv)
	logDetail := "unavailable"
	if strings.TrimSpace(logPath) != "" {
		logDetail = logPath
	}
	detail := fmt.Sprintf(
		"This forwarded dispatch collided with a reservation another run still holds.\n\n"+
			"Attempted harness: `%s`\n"+
			"Attempted run: `ward agent %s`\n"+
			"Host log: `%s`\n"+
			"Collision: `%s`\n\n"+
			"Nothing new is running for this dispatch, and the live run's hold is untouched. "+
			"Retry: a `ward agent director` heartbeat sweeps this marker and redispatches once the hold "+
			"releases - the run's terminal `WARD-WORKFLOW` outcome supersedes its reservation (ward#1149). A manual "+
			"retry can pass `--override-reservation` if the collision is genuinely stale.",
		mode, attempted, logDetail, firstLine(launchErr.Error()))
	return agentNeedsRedispatchMarker + "\n" +
		collapsedIssueComment(workflowDispatchDeferredVisible(), "reservation-collision details", detail)
}

// dispatchLaunchReleaseAssetsDeferredCommentBody renders the deferred comment.
// It explains that the selected release was visible before its bootstrap asset.
func dispatchLaunchReleaseAssetsDeferredCommentBody(mode containerMode, container string, req dispatchBrokerRequest, logPath string, launchErr error) string {
	attempted := redactDispatchBrokerArgv(req.Argv)
	logDetail := "unavailable"
	if strings.TrimSpace(logPath) != "" {
		logDetail = logPath
	}
	detail := fmt.Sprintf(
		"This forwarded dispatch was deferred because the selected release was visible before its bootstrap asset was ready.\n\n"+
			"Attempted harness: `%s`\n"+
			"Attempted run: `ward agent %s`\n"+
			"Container: `%s`\n"+
			"Container created: no running engineer was observed.\n"+
			"Host log: `%s`\n"+
			"Release assets: `%s`\n\n"+
			"Retry: the issue stays queued until the release publishes the missing platform assets, then the director can try again.",
		mode, attempted, container, logDetail, firstLine(launchErr.Error()))
	return agentReservationReleaseMarker + "\n" + agentNeedsRedispatchMarker + "\n" +
		collapsedIssueComment(workflowDispatchDeferredVisible(), "release-assets-not-ready details", detail)
}

// withBrokerForwardingDisabled temporarily clears the read-only surface markers so
// the host-side launch does not re-enter the broker and deadlock on itself.
func withBrokerForwardingDisabled(req dispatchBrokerRequest, fn func() error) error {
	dispatchBrokerLaunchMu.Lock()
	defer dispatchBrokerLaunchMu.Unlock()

	type savedEnv struct {
		value string
		set   bool
	}
	saved := map[string]savedEnv{}
	cleared := []string{"WARD_READONLY", envDispatchBrokerAddr, envDispatchBrokerToken}
	requestKeys := []string{envDispatchRequestID, envDispatchJournalPath, envDispatchResumeContainer}
	for _, key := range append(append([]string{}, cleared...), requestKeys...) {
		if v, ok := os.LookupEnv(key); ok {
			saved[key] = savedEnv{value: v, set: true}
		}
	}
	for _, key := range cleared {
		_ = os.Unsetenv(key)
	}
	if req.RequestID != "" {
		_ = os.Setenv(envDispatchRequestID, req.RequestID)
	}
	if req.JournalPath != "" {
		_ = os.Setenv(envDispatchJournalPath, req.JournalPath)
	}
	if req.ResumeContainer != "" {
		_ = os.Setenv(envDispatchResumeContainer, req.ResumeContainer)
	}
	defer func() {
		for _, key := range append(append([]string{}, cleared...), requestKeys...) {
			if v, ok := saved[key]; ok && v.set {
				_ = os.Setenv(key, v.value)
				continue
			}
			_ = os.Unsetenv(key)
		}
	}()
	return fn()
}

// runDispatchBrokerStop resolves the stop target to one running engineer and
// docker-stops it (ward#627); returns the stopped name (see docs/agent-stop.md).
func (r *Runner) runDispatchBrokerStop(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	name, err := r.resolveEngineerStopTarget(ctx, strings.TrimSpace(req.Target))
	if err != nil {
		var stale *staleEngineerLaunchError
		if !errors.As(err, &stale) {
			return "", err
		}
		cl, cerr := r.hostTrackerClient(ctx, stale.hold.Ref().trackerOrDefault(), stale.hold.Mode())
		if cerr != nil {
			return "", fmt.Errorf("dispatch broker: build tracker client to clear stale launch %s: %w", stale.hold.Ref(), cerr)
		}
		if !clearStalePrelaunchReservation(ctx, cl, "ward agent stop", stale.hold) {
			return "", fmt.Errorf("dispatch broker: stale launch %s could not be cleared; the reservation cache remains for diagnosis", stale.hold.Ref())
		}
		return staleLaunchCleanupResultPrefix + stale.hold.Ref().String(), nil
	}
	// Graceful stop, the exact verb reap uses (agent_reap.go): no rm, no kill, no exec.
	if serr := r.dockerExec(ctx, "stop", name); serr != nil {
		return "", fmt.Errorf("dispatch broker: docker stop %s: %w", name, serr)
	}
	return name, nil
}

// runDispatchBrokerStopPreview resolves the stop target but leaves the container
// running. It uses the same stoppability criteria as a real stop.
func (r *Runner) runDispatchBrokerStopPreview(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	name, err := r.resolveEngineerStopTarget(ctx, strings.TrimSpace(req.Target))
	if err == nil {
		return name, nil
	}
	var stale *staleEngineerLaunchError
	if errors.As(err, &stale) {
		return staleLaunchCleanupResultPrefix + stale.hold.Ref().String(), nil
	}
	return "", err
}

// resolveEngineerStopTarget maps a stop target to one running engineer, fail-closed
// on role (ward#627): owner/repo#N matches by label, else it is a container name.
func (r *Runner) resolveEngineerStopTarget(ctx context.Context, target string) (string, error) {
	// owner/repo#N: match by the engineer identity labels (ward#364). The role filter
	// is engineer-only, and selectSingleStopTarget refuses zero / more-than-one.
	if ref, err := parseAgentIssueRef(target); err == nil && ref.Owner != "" && ref.Repo != "" {
		return r.resolveEngineerStopRef(ctx, target, ref)
	}
	// Otherwise a container name: it must be a running container, and its role is
	// re-checked fail-closed below (never a director/session).
	if !r.containerRunning(ctx, target) {
		return "", fmt.Errorf("dispatch broker: no running container named %q to stop", target)
	}
	return r.guardEngineerStop(ctx, target)
}

func (r *Runner) resolveEngineerStopRef(ctx context.Context, target string, ref agentIssueRef) (string, error) {
	running := r.runningEngineersForIssue(ctx, ref)
	name, err := selectSingleStopTarget(target, running)
	if err == nil {
		return r.guardEngineerStop(ctx, name)
	}
	if len(running) != 0 {
		return "", err
	}
	res, ok, holdErr := r.reservedEngineerHold(ref)
	if holdErr != nil || !ok {
		return "", err
	}
	if reservationFresh(res.At, time.Now().UTC(), agentLaunchConfirmationTTL()) {
		return "", fmt.Errorf("dispatch broker: %q is a fresh launch intent with no visible container yet; wait for the %s launch-confirmation window before cleanup", ref, conciseDuration(agentLaunchConfirmationTTL()))
	}
	path, pathErr := agentReservationPath(ref)
	if pathErr != nil {
		return "", fmt.Errorf("dispatch broker: resolve stale launch cache for %s: %w", ref, pathErr)
	}
	return "", &staleEngineerLaunchError{hold: stalePrelaunchReservation{Path: path, Reservation: *res}}
}

// reservedEngineerHold reports whether a ref still has a local launch-intent
// reservation, even when no running engineer container is visible yet.
func (r *Runner) reservedEngineerHold(ref agentIssueRef) (*agentReservation, bool, error) {
	path, err := agentReservationPath(ref)
	if err != nil {
		return nil, false, err
	}
	res, ok, err := readAgentReservation(path)
	if err != nil || !ok || res == nil {
		return nil, false, err
	}
	if res.Owner != ref.Owner || res.Repo != ref.Repo || res.Number != ref.Number {
		return nil, false, nil
	}
	return res, true, nil
}

// guardEngineerStop reads a resolved container's ward.role and refuses unless it is
// engineer (ward#627); an unreadable label fails closed rather than stopping blind.
func (r *Runner) guardEngineerStop(ctx context.Context, name string) (string, error) {
	role, err := r.containerRoleLabel(ctx, name)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: refusing to stop %q: could not read its %s label (%w) - "+
			"fail-closed, only %s containers are stoppable", name, labelRole, err, roleEngineer)
	}
	if gerr := stopTargetGuard(name, role); gerr != nil {
		return "", gerr
	}
	return name, nil
}

// containerRoleLabel reads a container's ward.role label via docker inspect; an
// empty result means the label is absent (a non-ward or unlabeled container).
func (r *Runner) containerRoleLabel(ctx context.Context, name string) (string, error) {
	out, err := r.dockerCapture(ctx, "inspect",
		"--format", `{{index .Config.Labels "`+labelRole+`"}}`, name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// runningEngineersForIssue lists the running engineer containers carrying ref's
// repo + issue, AND-combined with ward=true + ward.role=engineer (ward#364, #627).
func (r *Runner) runningEngineersForIssue(ctx context.Context, ref agentIssueRef) []string {
	out, err := r.dockerCapture(ctx, "ps", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRole+"="+roleEngineer,
		"--filter", "label="+labelRepo+"="+ref.repoSlug(),
		"--filter", fmt.Sprintf("label=%s=%d", labelIssue, ref.Number))
	if err != nil {
		return nil
	}
	// parseExitedContainerNames is a plain non-blank-line splitter (name is historical).
	return parseExitedContainerNames(string(out))
}

// stopTargetGuard enforces the engineer-only stop rule (ward#627): only a
// ward.role=engineer is stoppable; any other role, or an empty one, is refused.
func stopTargetGuard(name, role string) error {
	switch role = strings.TrimSpace(role); role {
	case roleEngineer:
		return nil
	case "":
		return fmt.Errorf("dispatch broker: refusing to stop %q: its %s label is empty or unreadable - "+
			"fail-closed, only %s containers are stoppable", name, labelRole, roleEngineer)
	default:
		return fmt.Errorf("dispatch broker: refusing to stop %q: it is a %q container, not an engineer - "+
			"stop only targets %s (director/session are never stopped)", name, role, roleEngineer)
	}
}

// selectSingleEngineerTarget picks exactly one engineer from a match set, refusing
// on zero or more than one (ambiguous) with the candidates listed, not a guess.
func selectSingleEngineerTarget(action, target string, names []string) (string, error) {
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", fmt.Errorf("dispatch broker: no running engineer container matches %q - nothing to %s", target, action)
	default:
		return "", fmt.Errorf("dispatch broker: %q matches %d running engineer containers (%s) - "+
			"refusing to guess; %s one by its container name", target, len(names), strings.Join(names, ", "), action)
	}
}

// selectSingleStopTarget keeps the ward#627 stop wording stable.
func selectSingleStopTarget(target string, names []string) (string, error) {
	return selectSingleEngineerTarget("stop", target, names)
}

// redirectStdioToLog swaps process os.Stdout/os.Stderr to logf for one served run (read
// at run time by its newRunner + subprocesses), serialized by dispatchStdioMu (ward#389).
func redirectStdioToLog(logf *os.File) func() {
	dispatchStdioMu.Lock()
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = logf, logf
	return func() {
		os.Stdout, os.Stderr = prevOut, prevErr
		dispatchStdioMu.Unlock()
	}
}

func validateDispatchBrokerRequest(req dispatchBrokerRequest) error {
	switch dispatchAction(req.Action) {
	case dispatchActionPing:
		return nil
	case dispatchActionStop:
		return validateDispatchBrokerStop(req)
	case dispatchActionList:
		return validateDispatchBrokerList(req)
	case dispatchActionLogs:
		return validateDispatchBrokerLogs(req)
	case dispatchActionLaunch:
		return validateDispatchBrokerLaunch(req)
	default:
		return fmt.Errorf("dispatch broker: action %q refused (allowed: launch, stop, list, logs, ping)", req.Action)
	}
}

// dispatchStopTargetRe bounds a stop's container-name target to docker's own
// name grammar, so a non-issue-ref target can only be a plausible container name.
var (
	dispatchStopTargetRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	dispatchRequestIDPattern = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
)

// validateDispatchBrokerStop checks the stop shape (ward#627): a non-empty target,
// no launch argv, no flags, target an issue ref or a bare container name.
func validateDispatchBrokerStop(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 {
		return fmt.Errorf("dispatch broker: stop takes no launch argv, got %v", req.Argv)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return fmt.Errorf("dispatch broker: stop requires a target (owner/repo#N or a container name)")
	}
	if strings.ContainsRune(target, '\x00') {
		return fmt.Errorf("dispatch broker: stop target contains NUL")
	}
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("dispatch broker: stop target %q must not be a flag", target)
	}
	// A target is either a parseable issue ref (resolved by labels host-side) or a
	// bare container name; a URL/path or metacharacter-bearing string is neither.
	if _, err := parseAgentIssueRef(target); err != nil && !dispatchStopTargetRe.MatchString(target) {
		return fmt.Errorf("dispatch broker: stop target %q is neither an issue ref (owner/repo#N) nor a container name", target)
	}
	return nil
}

// validateDispatchBrokerLogs checks the logs shape: optional target, no argv, and
// a non-negative tail. An empty target means the current Compose group.
func validateDispatchBrokerLogs(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 {
		return fmt.Errorf("dispatch broker: logs takes no launch argv, got %v", req.Argv)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		if req.Tail < 0 {
			return fmt.Errorf("dispatch broker: logs tail must be >= 0")
		}
		return nil
	}
	if strings.ContainsRune(target, '\x00') {
		return fmt.Errorf("dispatch broker: logs target contains NUL")
	}
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("dispatch broker: logs target %q must not be a flag", target)
	}
	if _, err := parseAgentIssueRef(target); err != nil && !dispatchStopTargetRe.MatchString(target) {
		return fmt.Errorf("dispatch broker: logs target %q is neither an issue ref (owner/repo#N) nor a container name", target)
	}
	if req.Tail < 0 {
		return fmt.Errorf("dispatch broker: logs tail must be >= 0")
	}
	return nil
}

// validateDispatchBrokerList checks the list shape: no target, no argv, and a
// known output format.
func validateDispatchBrokerList(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 {
		return fmt.Errorf("dispatch broker: list takes no launch argv, got %v", req.Argv)
	}
	if strings.TrimSpace(req.Target) != "" {
		return fmt.Errorf("dispatch broker: list takes no target, got %q", req.Target)
	}
	switch strings.TrimSpace(req.Format) {
	case "", "json", "text":
	default:
		return fmt.Errorf("dispatch broker: list format %q refused (allowed: text, json)", req.Format)
	}
	return nil
}

// validateDispatchBrokerLaunch is the launch-request shape (the original narrow API):
// an engineer/QA role, an argv led by that role, and an issue ref (ward#378).
func validateDispatchBrokerLaunch(req dispatchBrokerRequest) error {
	if req.Target != "" {
		return fmt.Errorf("dispatch broker: launch takes no stop target, got %q", req.Target)
	}
	if req.Role != "engineer" && req.Role != "qa" {
		return fmt.Errorf("dispatch broker: role %q refused (allowed: engineer, qa)", req.Role)
	}
	if len(req.Argv) == 0 || req.Argv[0] != req.Role {
		return fmt.Errorf("dispatch broker: argv must begin with role %q", req.Role)
	}
	if len(req.Argv) < 2 {
		return fmt.Errorf("dispatch broker: missing issue ref")
	}
	for _, arg := range req.Argv {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("dispatch broker: argv contains NUL")
		}
	}
	if _, err := parseAgentIssueRef(req.Argv[1]); err != nil {
		return fmt.Errorf("dispatch broker: %s dispatch requires an issue ref, got %q", req.Role, req.Argv[1])
	}
	return validateDispatchBrokerArgv(req.Role, req.Argv[2:])
}

// runDispatchBrokerLogs resolves one explicit run, or the current Compose group
// when no target is supplied, then streams logs back over the request connection.
func (r *Runner) runDispatchBrokerLogs(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
	if strings.TrimSpace(req.Target) == "" {
		group, err := r.resolveCurrentComposeGroupLogs(ctx, req.Requester, req.Tail, req.Follow)
		if err != nil {
			writeDispatchBrokerLogsResponse(conn, "", err)
			return
		}
		writeDispatchBrokerLogsResponse(conn, group.String(), nil)
		if err := r.streamAgentLogsGroup(ctx, group, conn); err != nil {
			fmt.Fprintf(os.Stderr, "ward dispatch broker: logs stream failed: %v\n", err)
		}
		return
	}
	source, err := r.resolveDispatchBrokerLogsSource(ctx, req)
	if err != nil {
		writeDispatchBrokerLogsResponse(conn, "", err)
		return
	}
	writeDispatchBrokerLogsResponse(conn, source.String(), nil)
	if err := r.streamAgentLogsSource(ctx, source, conn); err != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: logs stream failed: %v\n", err)
	}
}

// runDispatchBrokerList serves the read-only engineer list back over the broker.
func (r *Runner) runDispatchBrokerList(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
	body, err := r.renderAgentList(ctx, strings.TrimSpace(req.Format) == "json")
	if err != nil {
		writeDispatchBrokerResponse(conn, "", "", "", err)
		return
	}
	writeDispatchBrokerResponse(conn, "", "", "", nil)
	_, _ = io.WriteString(conn, body)
}

// resolveDispatchBrokerLogsSource resolves the request target to one readable
// engineer log source.
func (r *Runner) resolveDispatchBrokerLogsSource(ctx context.Context, req dispatchBrokerRequest) (agentLogSource, error) {
	if ref, err := parseAgentIssueRef(req.Target); err == nil && ref.Owner != "" && ref.Repo != "" {
		return r.resolveAgentLogsSourceForIssue(ctx, ref, req.Tail, req.Follow)
	}
	return r.resolveAgentLogsSourceForName(ctx, req.Target, req.Tail, req.Follow)
}

func validateDispatchBrokerArgv(role string, tail []string) error {
	// --config is repeatable on both roles (ward#616); --harness and its equal
	// --agent spelling stay approved for skew-safety (ward#660).
	valueFlags := map[string]bool{"--harness": true, "--agent": true, "--config": true}
	boolFlags := map[string]bool{"--print": true}
	if role == "engineer" {
		valueFlags["--workflow"] = true
		valueFlags["--details"] = true
		for _, f := range []string{"--image", "--tag", "--ward-version", "--branch", "--repo"} {
			valueFlags[f] = true
		}
		for _, f := range []string{"--no-pull", "--override-reservation", "--override-capacity", "--skip-preflight", "--no-preflight", "--skip-smoke-test", "--skip-review", "--no-review-gate", "--pr"} {
			boolFlags[f] = true
		}
		return validateDispatchBrokerFlags(role, tail, valueFlags, boolFlags, false)
	}
	if role == "qa" {
		valueFlags["--family"] = true
	}
	valueFlags["--thoroughness"] = true
	valueFlags["--depth"] = true
	return validateDispatchBrokerFlags(role, tail, valueFlags, boolFlags, true)
}

func validateDispatchBrokerFlags(role string, tail []string, valueFlags, boolFlags map[string]bool, allowPrompt bool) error {
	for i := 0; i < len(tail); i++ {
		arg := tail[i]
		if !strings.HasPrefix(arg, "-") {
			if allowPrompt {
				return nil
			}
			return fmt.Errorf("dispatch broker: %s argument %q refused after issue ref", role, arg)
		}
		if valueFlags[arg] {
			i++
			if i >= len(tail) || tail[i] == "" {
				return fmt.Errorf("dispatch broker: %s flag %s needs a value", role, arg)
			}
			continue
		}
		if boolFlags[arg] {
			continue
		}
		return fmt.Errorf("dispatch broker: %s flag %s is not approved", role, arg)
	}
	return nil
}

func emptyDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func brokerDispatchArgvForRole(ctx context.Context, r *Runner, c *cli.Command, role string, mode containerMode) ([]string, bool) {
	ref, ok := r.brokerDispatchRef(ctx, c.Args().First())
	if !ok {
		return nil, false
	}
	switch role {
	case "engineer":
		return brokerEngineerArgv(c, mode, ref), true
	case "qa":
		return brokerQaArgv(c, mode, ref), true
	default:
		return nil, false
	}
}

// maybeForwardAgentDispatchToHostBroker is the in-container ref-mode gate.
// It only runs inside a read-only director surface with a broker endpoint.
func (r *Runner) maybeForwardAgentDispatchToHostBroker(ctx context.Context, c *cli.Command, role string, mode containerMode) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	if err := probeHostDispatchBroker(ctx, addr); err != nil {
		return true, err
	}
	argv, ok := brokerDispatchArgvForRole(ctx, r, c, role, mode)
	if !ok {
		return false, nil
	}
	req := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(),
		Role:      role,
		Argv:      argv,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	logPath, err := sendDispatchBrokerLaunchRequest(ctx, addr, req)
	if err != nil {
		if logPath != "" {
			return true, fmt.Errorf("%w (dispatch log: %s)", err, logPath)
		}
		return true, err
	}
	fmt.Fprintln(os.Stderr, dispatchBrokerForwardedLine(argv, logPath))
	return true, nil
}

// forwardFreeformEngineerLaunchToHostBroker forwards the launch after a freshly
// filed freeform engineer issue on read-only surfaces.
func (r *Runner) forwardFreeformEngineerLaunchToHostBroker(ctx context.Context, c *cli.Command, mode containerMode, ref agentIssueRef) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	if err := probeHostDispatchBroker(ctx, addr); err != nil {
		return true, err
	}
	req := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(),
		Role:      "engineer",
		Argv:      brokerEngineerArgv(c, mode, ref),
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	logPath, err := sendDispatchBrokerLaunchRequest(ctx, addr, req)
	if err != nil {
		if logPath != "" {
			return true, fmt.Errorf("%w (dispatch log: %s)", err, logPath)
		}
		return true, err
	}
	fmt.Fprintln(os.Stderr, dispatchBrokerForwardedLine(req.Argv, logPath))
	return true, nil
}

// dispatchBrokerForwardedLine renders the stable text for a forwarded launch.
// If logPath is missing, it falls back to a deterministic lookup command.
func dispatchBrokerForwardedLine(argv []string, logPath string) string {
	displayArgv := redactDispatchBrokerArgv(argv)
	base := fmt.Sprintf("ward dispatch broker: accepted `ward agent %s`; broker Ward launch started", displayArgv)
	if v := dispatchBrokerWardVersion(argv); v != "" {
		base += fmt.Sprintf(" (effective ward %s)", v)
	}
	base += " (container visibility and engineer harness startup are pending)"
	ref := ""
	if len(argv) >= 2 {
		ref = strings.TrimSpace(argv[1])
	}
	if path := strings.TrimSpace(logPath); path != "" {
		if ref != "" {
			return fmt.Sprintf("%s (broker artifact %s; inspect with `ward agent logs %s`)", base, path, ref)
		}
		return fmt.Sprintf("%s (broker artifact %s)", base, path)
	}
	if ref != "" {
		return fmt.Sprintf("%s (dispatch log path unavailable yet; inspect later with `ward agent logs %s`)", base, ref)
	}
	return fmt.Sprintf("%s (dispatch log path unavailable yet; no lookup command could be derived)", base)
}

// dispatchBrokerWardVersion extracts the version the brokered launch will carry.
func dispatchBrokerWardVersion(argv []string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--ward-version" {
			if v := strings.TrimSpace(argv[i+1]); v != "" {
				return v
			}
		}
	}
	if version.LooksReleased(Version) {
		return Version
	}
	return ""
}

// brokerDispatchHarness returns the harness to forward into a sibling dispatch.
// Explicit --harness/--agent wins; otherwise inherit WARD_AGENT/WARD_MODE.
func brokerDispatchHarness(c *cli.Command, fallback containerMode) containerMode {
	if c.IsSet("harness") || c.IsSet("agent") {
		return fallback
	}
	return currentAgentMode()
}

func (r *Runner) brokerDispatchRef(ctx context.Context, arg string) (agentIssueRef, bool) {
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return agentIssueRef{}, false
	}
	return ref, true
}

func brokerEngineerArgv(c *cli.Command, mode containerMode, ref agentIssueRef) []string {
	// Preserve the tracker-qualified URL; compact refs can be re-resolved under
	// checkout policy and silently turn a Forgejo ticket into a GitHub lookup.
	argv := []string{"engineer", ref.url(), "--harness", string(brokerDispatchHarness(c, mode))}
	argv = appendBrokerContainerFlags(argv, c)
	argv = appendBrokerLocalHarnessConfig(argv, c, mode)
	if c.IsSet("workflow") {
		if wf := strings.TrimSpace(c.String("workflow")); wf != "" {
			argv = append(argv, "--workflow", wf)
		}
	}
	if details := strings.TrimSpace(c.String("details")); details != "" {
		argv = append(argv, "--details", details)
	}
	// Forward each --override-* spelling as typed (ward#1045); capacity never rides
	// on reservation.
	if c.Bool("override-reservation") {
		argv = append(argv, "--override-reservation")
	}
	if c.Bool("override-capacity") {
		argv = append(argv, "--override-capacity")
	}
	if c.Bool("pr") {
		argv = append(argv, "--pr")
	}
	if c.Bool("skip-preflight") {
		argv = append(argv, "--skip-preflight")
	}
	if smokeTestSkipped(c) {
		argv = append(argv, "--skip-smoke-test")
	}
	if c.Bool("skip-review") || c.Bool("no-review-gate") {
		argv = append(argv, "--skip-review")
	}
	if c.Bool("print") {
		argv = append(argv, "--print")
	}
	return argv
}

func brokerQaArgv(c *cli.Command, mode containerMode, ref agentIssueRef) []string {
	argv := []string{"qa", ref.String(), "--harness", string(brokerDispatchHarness(c, mode))}
	if family := strings.TrimSpace(c.String("family")); family != "" {
		argv = append(argv, "--family", family)
	}
	if lvl := strings.TrimSpace(c.String("thoroughness")); lvl != "" {
		argv = append(argv, "--thoroughness", lvl)
	}
	argv = appendBrokerConfigFlags(argv, c)
	if c.Bool("print") {
		argv = append(argv, "--print")
	}
	argv = append(argv, c.Args().Tail()...)
	return argv
}

// brokerWardVersion resolves the ward version a brokered launch should carry.
// Explicit pins win; released callers otherwise forward their current release.
func brokerWardVersion(c *cli.Command) string {
	if v := strings.TrimSpace(c.String("ward-version")); v != "" {
		return v
	}
	if version.LooksReleased(Version) {
		return Version
	}
	return ""
}

// appendBrokerConfigFlags forwards each repeatable --config override to the host-side
// dispatch argv (ward#616); the host re-parses + validates it via parseConfigOverrides.
func appendBrokerConfigFlags(argv []string, c *cli.Command) []string {
	for _, cfg := range c.StringSlice("config") {
		if cfg = strings.TrimSpace(cfg); cfg != "" {
			argv = append(argv, "--config", cfg)
		}
	}
	return argv
}

// appendBrokerLocalHarnessConfig makes inherited deployment config explicit at
// the broker boundary. A request-local --config spelling keeps precedence.
func appendBrokerLocalHarnessConfig(argv []string, c *cli.Command, mode containerMode) []string {
	var pairs [][2]string
	switch mode {
	case modeGoose:
		pairs = [][2]string{{"agent.goose.model", "WARD_GOOSE_MODEL"}}
	case modeOpencode:
		pairs = [][2]string{
			{"agent.opencode.model", "WARD_OPENCODE_MODEL"},
			{"agent.opencode.endpoint", "WARD_OLLAMA_URL"},
		}
	case modeClaude, modeCodex:
		return argv
	}
	for _, pair := range pairs {
		if configOverridePresent(c, pair[0]) {
			continue
		}
		if value := strings.TrimSpace(os.Getenv(pair[1])); value != "" {
			argv = append(argv, "--config", pair[0]+"="+value)
		}
	}
	return argv
}

func configOverridePresent(c *cli.Command, path string) bool {
	for _, raw := range c.StringSlice("config") {
		got, _, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && strings.TrimSpace(got) == path {
			return true
		}
	}
	return false
}

func appendBrokerContainerFlags(argv []string, c *cli.Command) []string {
	for _, name := range []string{"image", "tag", "branch"} {
		if v := strings.TrimSpace(c.String(name)); c.IsSet(name) && v != "" {
			argv = append(argv, "--"+name, v)
		}
	}
	for _, repo := range extraRepoGrant(c) {
		if repo = strings.TrimSpace(repo); repo != "" {
			argv = append(argv, "--repo", repo)
		}
	}
	argv = appendBrokerConfigFlags(argv, c)
	for _, name := range []string{"no-pull"} {
		if c.Bool(name) {
			argv = append(argv, "--"+name)
		}
	}
	if v := brokerWardVersion(c); v != "" {
		argv = append(argv, "--ward-version", v)
	}
	return argv
}

func redactDispatchBrokerArgv(argv []string) string {
	return redactSecrets(strings.Join(argv, " "))
}

func sendDispatchBrokerRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (string, error) {
	conn, err := dialDispatchBroker(ctx, addr)
	if err != nil {
		return "", dispatchBrokerDialDiagnostic(addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", fmt.Errorf("dispatch broker: send request: %w", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", fmt.Errorf("dispatch broker: read response from %s: %w", addr, err)
	}
	if !resp.OK {
		// Papercut #2 (ward#382): the credential broker answers a dispatch dial with a
		// protocol-version refusal - surface it as a "wrong broker" hint, not a bare string.
		if isCredentialBrokerReply(resp.Error) {
			return "", fmt.Errorf("%w: %s answered as the credential broker, not the dispatch broker "+
				"(WARD_DISPATCH_BROKER_ADDR points at the wrong broker - see ward#382)",
				errDispatchBrokerUnavailable, addr)
		}
		return resp.LogPath, fmt.Errorf("dispatch broker: %s", resp.Error)
	}
	return resp.LogPath, nil
}

func sendDispatchBrokerLaunchRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (string, error) {
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = newDispatchBrokerRequestID()
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		logPath, retry, err := sendDispatchBrokerLaunchAttempt(ctx, addr, req)
		if err == nil || !retry || ctx.Err() != nil {
			return logPath, err
		}
		lastErr = err
	}
	return "", lastErr
}

func sendDispatchBrokerLaunchAttempt(ctx context.Context, addr string, req dispatchBrokerRequest) (string, bool, error) {
	conn, err := dialDispatchBroker(ctx, addr)
	if err != nil {
		return "", false, dispatchBrokerDialDiagnostic(addr, err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		_ = conn.Close()
		return "", false, fmt.Errorf("dispatch broker: send request: %w", err)
	}
	type responseResult struct {
		resp dispatchBrokerResponse
		err  error
	}
	ch := make(chan responseResult, 1)
	go func() {
		defer func() { _ = conn.Close() }()
		var resp dispatchBrokerResponse
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			ch <- responseResult{err: err}
			return
		}
		ch <- responseResult{resp: resp}
	}()
	select {
	case <-ctx.Done():
		_ = conn.Close()
		return "", false, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return "", true, launchDispatchBrokerResponseErr(addr, result.err)
		}
		if !result.resp.OK {
			if isCredentialBrokerReply(result.resp.Error) {
				return "", false, fmt.Errorf("%w: %s answered as the credential broker, not the dispatch broker "+
					"(WARD_DISPATCH_BROKER_ADDR points at the wrong broker - see ward#382)",
					errDispatchBrokerUnavailable, addr)
			}
			return result.resp.LogPath, false, fmt.Errorf("dispatch broker: %s", result.resp.Error)
		}
		return result.resp.LogPath, false, nil
	}
}

func launchDispatchBrokerResponseErr(addr string, err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("dispatch broker: host-side command exited before writing a response from %s: %w", addr, err)
	}
	return fmt.Errorf("dispatch broker: read response from %s: %w", addr, err)
}

// sendDispatchBrokerLogsRequest sends a logs request and returns the source + body
// stream for the caller to relay.
func sendDispatchBrokerLogsRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (string, io.ReadCloser, error) {
	conn, err := dialDispatchBroker(ctx, addr)
	if err != nil {
		return "", nil, dispatchBrokerDialDiagnostic(addr, err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("dispatch broker: send request: %w", err)
	}
	dec := json.NewDecoder(conn)
	var resp dispatchBrokerResponse
	if err := dec.Decode(&resp); err != nil {
		_ = conn.Close()
		return "", nil, fmt.Errorf("dispatch broker: read response from %s: %w", addr, err)
	}
	if !resp.OK {
		_ = conn.Close()
		if isCredentialBrokerReply(resp.Error) {
			return "", nil, fmt.Errorf("%w: %s answered as the credential broker, not the dispatch broker "+
				"(WARD_DISPATCH_BROKER_ADDR points at the wrong broker - see ward#382)",
				errDispatchBrokerUnavailable, addr)
		}
		return "", nil, fmt.Errorf("dispatch broker: %s", resp.Error)
	}
	return resp.Source, &connReadCloser{
		Reader: skipLeadingSeparator(io.MultiReader(dec.Buffered(), conn)),
		CloseFn: func() error {
			return conn.Close()
		},
	}, nil
}

// probeHostDispatchBroker probes whether the advertised dispatch broker accepts
// connections before we choose the brokered path.
func probeHostDispatchBroker(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, dispatchBrokerProbeTimeout)
	defer cancel()
	conn, err := dialDispatchBroker(probeCtx, addr)
	if err != nil {
		return dispatchBrokerDialDiagnostic(addr, err)
	}
	_ = conn.Close()
	return nil
}

func dispatchBrokerDialDiagnostic(addr string, err error) error {
	kind := dispatchBrokerDiagnosticBrokerUnreachable
	if isDialTimeoutError(err) {
		kind = dispatchBrokerDiagnosticBrokerTimeout
	}
	return &dispatchBrokerDiagnostic{
		kind:        kind,
		addrSource:  envDispatchBrokerAddr,
		addr:        addr,
		connection:  dispatchBrokerConnectionText(kind, err),
		remediation: dispatchBrokerRemediationText(),
	}
}

func dispatchBrokerConnectionText(kind dispatchBrokerDiagnosticKind, err error) string {
	switch kind {
	case dispatchBrokerDiagnosticBrokerUnreachable:
		if err == nil {
			return ""
		}
		return err.Error()
	case dispatchBrokerDiagnosticBrokerTimeout:
		return fmt.Sprintf("timed out dialing broker after %s", dispatchBrokerProbeTimeout)
	default:
		if err == nil {
			return ""
		}
		return err.Error()
	}
}

func dispatchBrokerRemediationText() string {
	return "retry after the broker service restarts; if it stays unavailable, exit this director surface and start a fresh `warded director ...` stack."
}

func isDialTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// isCredentialBrokerReply spots the credential broker's protocol-version refusal:
// the dispatch client reached cmd/ward/broker.go, not the dispatch broker (ward#382).
func isCredentialBrokerReply(msg string) bool {
	return strings.Contains(msg, "unsupported protocol version")
}

type connReadCloser struct {
	io.Reader
	CloseFn func() error
}

func (c *connReadCloser) Close() error {
	if c.CloseFn != nil {
		return c.CloseFn()
	}
	return nil
}

// skipLeadingSeparator drops one broker framing line break from a streamed body.
func skipLeadingSeparator(r io.Reader) io.Reader {
	return &separatorSkippingReader{r: bufio.NewReader(r)}
}

type separatorSkippingReader struct {
	r       *bufio.Reader
	skipped bool
}

func (s *separatorSkippingReader) Read(p []byte) (int, error) {
	if !s.skipped {
		s.skipped = true
		s.skipLeadingSeparator()
	}
	return s.r.Read(p)
}

func (s *separatorSkippingReader) skipLeadingSeparator() {
	for {
		b, err := s.r.Peek(1)
		if err != nil {
			return
		}
		if b[0] == '\n' {
			_, _ = s.r.ReadByte()
			continue
		}
		if b[0] == '\r' {
			_, _ = s.r.ReadByte()
			s.skipOptionalLineFeed()
			continue
		}
		return
	}
}

func (s *separatorSkippingReader) skipOptionalLineFeed() {
	next, err := s.r.Peek(1)
	if err == nil && next[0] == '\n' {
		_, _ = s.r.ReadByte()
	}
}
