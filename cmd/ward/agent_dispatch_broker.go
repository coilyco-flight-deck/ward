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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/version"
	"github.com/urfave/cli/v3"
)

// agent_dispatch_broker.go wires ward#378: director surfaces ask host ward to
// launch sibling engineer/advisor runs. TCP transport + token gate: ward#391.

const (
	// envDispatchBrokerAddr carries host.docker.internal:<port> the surface dials;
	// envDispatchBrokerToken the per-launch nonce it echoes back (ward#391).
	envDispatchBrokerAddr  = "WARD_DISPATCH_BROKER_ADDR"
	envDispatchBrokerToken = "WARD_DISPATCH_BROKER_TOKEN"

	// dispatchBrokerListenAddr binds an ephemeral TCP port on all interfaces so the
	// container reaches it via the docker gateway; the token is the access control.
	dispatchBrokerListenAddr = "0.0.0.0:0"
)

var errDispatchBrokerUnavailable = errors.New("dispatch broker unavailable")

// dispatchBrokerLaunchMu serializes the process-global env override while the host
// broker kicks off a forwarded run so one launch cannot leak into another.
var dispatchBrokerLaunchMu sync.Mutex

// dispatchBrokerLaunch runs the validated request's role-specific launch.
// Tests override this hook to keep the broker path fast and observable.
var dispatchBrokerLaunch = func(ctx context.Context, req dispatchBrokerRequest) error {
	switch req.Role {
	case "engineer":
		return agentEngineerCommand().Run(ctx, req.Argv)
	case "advisor":
		return agentAdvisorCommand().Run(ctx, req.Argv)
	case "qa":
		return agentQACommand().Run(ctx, req.Argv)
	default:
		return fmt.Errorf("role %q is not dispatchable", req.Role)
	}
}

const (
	// dispatchActionLaunch is the default action: launch a sibling engineer/advisor
	// run. An empty Action normalizes to it, keeping older launch requests byte-compatible.
	dispatchActionLaunch = "launch"
	// dispatchActionStop is the targeted control action (ward#627): docker-stop one
	// running engineer named by Target - stop-only, engineer-only, no launch argv.
	dispatchActionStop = "stop"
	// dispatchActionList is the read-only control action: list running engineers.
	dispatchActionList = "list"
	// dispatchActionLogs streams one engineer's logs back to the requester.
	dispatchActionLogs = "logs"
)

type dispatchBrokerRequest struct {
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
	// Token is the per-launch shared secret the surface echoes back so the host
	// broker authenticates the dial (the TCP port has no socket file perms).
	Token string `json:"token,omitempty"`
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
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
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
// holding one file per forwarded run, sibling to the drained-container archives.
const dispatchLogsSubdir = "dispatch"

// startHostDispatchBroker serves validated dispatch requests until ctx ends. It
// returns the host:port the container dials + the token it must echo (ward#391).
func (r *Runner) startHostDispatchBroker(ctx context.Context, requester string) (addr, token string, cleanup func(), err error) {
	token, err = newDispatchBrokerToken()
	if err != nil {
		return "", "", func() {}, fmt.Errorf("ward dispatch broker: mint token: %w", err)
	}
	// Bind all interfaces: the container reaches the host via the docker gateway,
	// and loopback is unreachable from the LinuxKit VM. The token guards it.
	ln, err := net.Listen("tcp", dispatchBrokerListenAddr) //nolint:gosec // gateway-reachable bind, guarded by the per-launch token
	if err != nil {
		return "", "", func() {}, fmt.Errorf("ward dispatch broker: listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr = fmt.Sprintf("%s:%d", containerHostGateway, port)
	go r.serveHostDispatchBroker(ctx, ln, requester, token)
	return addr, token, func() {}, nil
}

// newDispatchBrokerToken mints a 256-bit hex nonce as the per-launch shared
// secret guarding the TCP transport (no socket file perm to lean on).
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
	defer func() { _ = conn.Close() }()
	var req dispatchBrokerRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeDispatchBrokerResponse(conn, "", fmt.Errorf("decode request: %w", err))
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		writeDispatchBrokerResponse(conn, "", errors.New("dispatch broker: token rejected"))
		return
	}
	if req.Requester == "" {
		req.Requester = requester
	}
	if dispatchAction(req.Action) == dispatchActionLogs {
		r.runDispatchBrokerLogs(ctx, conn, req)
		return
	}
	if dispatchAction(req.Action) == dispatchActionList {
		r.runDispatchBrokerList(ctx, conn, req)
		return
	}
	logPath, err := r.startHostDispatchBrokerRequest(ctx, req)
	writeDispatchBrokerResponse(conn, logPath, err)
}

func writeDispatchBrokerResponse(conn net.Conn, logPath string, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, LogPath: logPath}
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

// startHostDispatchBrokerRequest launches the validated request in the background
// and returns once the launch has started so the broker can ack the surface promptly.
func (r *Runner) startHostDispatchBrokerRequest(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	if err := validateDispatchBrokerRequest(req); err != nil {
		return "", err
	}
	if dispatchAction(req.Action) == dispatchActionStop {
		return r.runDispatchBrokerStop(ctx, req)
	}
	var lock *sync.Mutex
	if ref, err := parseAgentIssueRef(req.Argv[1]); err == nil {
		lock = dispatchRefLock(ref.String())
	}
	logf, logPath, err := openDispatchLog(req, time.Now())
	if err != nil {
		return "", fmt.Errorf("dispatch broker: open run log: %w", err)
	}
	_, _ = fmt.Fprintf(logf, "ward dispatch broker: %s requested `ward agent %s`\n",
		emptyDefault(req.Requester, "unknown-container"), redactDispatchBrokerArgv(req.Argv))
	if v := dispatchBrokerWardVersion(req.Argv); v != "" {
		_, _ = fmt.Fprintf(logf, "ward dispatch broker: effective ward version for this launch: %s\n", v)
	}
	ref := ""
	if len(req.Argv) >= 2 {
		ref = req.Argv[1]
	}
	_, _ = fmt.Fprintf(logf, "ward dispatch broker: this log captures the host wrapper only; in-container engineer console/reap logs drain separately and are readable with `ward agent logs %s`\n",
		ref)
	restore := redirectStdioToLog(logf)
	started := make(chan struct{})
	go func() {
		restored := false
		defer func() {
			if !restored {
				restore()
			}
			_ = logf.Close()
			dispatchStdioRestoreHook()
		}()
		if lock != nil {
			lock.Lock()
			defer lock.Unlock()
		}
		if err := withBrokerForwardingDisabled(func() error {
			close(started)
			return dispatchBrokerLaunch(ctx, req)
		}); err != nil {
			restore()
			restored = true
			dispatchFailedDispatchLaunchStartHook()
			r.commentDispatchLaunchError(ctx, req, logPath, err)
			return
		}
		fmt.Fprintf(os.Stderr, "ward dispatch broker: launch completed\n")
	}()
	select {
	case <-started:
		return logPath, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
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
// issue comment path after the host broker has restored its stdio.
func (r *Runner) commentDispatchLaunchError(ctx context.Context, req dispatchBrokerRequest, logPath string, launchErr error) {
	if isEngineerCapacityError(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: launch deferred: %v\n", launchErr)
		r.commentDeferredDispatchLaunch(ctx, req, logPath, launchErr)
		return
	}
	if isReleaseAssetsNotReadyError(launchErr) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: launch deferred: %v\n", launchErr)
		r.commentDeferredReleaseAssetsLaunch(ctx, req, logPath, launchErr)
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

// dispatchBrokerRequestMode resolves the requested harness for a forwarded dispatch.
func dispatchBrokerRequestMode(req dispatchBrokerRequest) containerMode {
	for i := 0; i+1 < len(req.Argv); i++ {
		switch req.Argv[i] {
		case "--harness", "--agent", "--driver":
			if mode, err := parseMode(req.Argv[i+1]); err == nil {
				return mode
			}
		}
	}
	return currentAgentMode()
}

// commentFailedDispatch writes the visible failure comment and clears the stale
// reservation signal after a forwarded launch never became a running engineer.
func (r *Runner) commentFailedDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	container := emptyDefault(req.Requester, "unknown-container")
	if req.Role == roleEngineer {
		container = issueScopedContainerName(req.Role, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	r.stopFailedDispatchContainer(ctx, mode, ref, req.Role, container)
	body := dispatchLaunchFailureCommentBody(mode, container, req, logPath, launchErr)
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not comment failed dispatch on %s: %v\n", ref, err)
		return
	}
	if err := cl.unlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not unlock issue %s after failed dispatch: %v\n", ref, err)
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: released failed dispatch reservation on %s\n", ref)
}

// commentDeferredDispatch writes the visible deferred comment and clears the stale
// reservation after a forwarded launch hit the global engineer cap.
func (r *Runner) commentDeferredDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	container := emptyDefault(req.Requester, "unknown-container")
	if req.Role == roleEngineer {
		container = issueScopedContainerName(req.Role, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	r.stopFailedDispatchContainer(ctx, mode, ref, req.Role, container)
	body := dispatchLaunchDeferredCommentBody(mode, container, req, logPath, launchErr)
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not comment deferred dispatch on %s: %v\n", ref, err)
		return
	}
	if err := cl.unlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not unlock issue %s after deferred dispatch: %v\n", ref, err)
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: released deferred dispatch reservation on %s\n", ref)
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

// commentDeferredReleaseAssetsDispatch writes the release-assets-not-ready comment.
// It clears the stale reservation after a published release still lacks the asset.
func (r *Runner) commentDeferredReleaseAssetsDispatch(ctx context.Context, cl Tracker, mode containerMode, ref agentIssueRef, req dispatchBrokerRequest, logPath string, launchErr error) {
	container := emptyDefault(req.Requester, "unknown-container")
	if req.Role == roleEngineer {
		container = issueScopedContainerName(req.Role, mode, targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref.Number)
	}
	body := dispatchLaunchReleaseAssetsDeferredCommentBody(mode, container, req, logPath, launchErr)
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not comment deferred release-assets-not-ready dispatch on %s: %v\n", ref, err)
		return
	}
	if err := cl.unlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: could not unlock issue %s after deferred release-assets-not-ready dispatch: %v\n", ref, err)
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: released deferred release-assets-not-ready reservation on %s\n", ref)
}

// dispatchLaunchFailureCommentBody supersedes the stale reservation with a visible
// failure comment and the retry shape an operator needs.
func dispatchLaunchFailureCommentBody(mode containerMode, container string, req dispatchBrokerRequest, logPath string, launchErr error) string {
	attempted := redactDispatchBrokerArgv(req.Argv)
	logDetail := "unavailable"
	if strings.TrimSpace(logPath) != "" {
		logDetail = logPath
	}
	detail := fmt.Sprintf(
		"This forwarded dispatch failed after the issue was already reserved.\n\n"+
			"Attempted harness: `%s`\n"+
			"Attempted run: `ward agent %s`\n"+
			"Container: `%s`\n"+
			"Container created: no running engineer was observed.\n"+
			"Host log: `%s`\n"+
			"Failure: `%s`\n\n"+
			"Retry: choose another harness if the first one is down, or rerun with `--force` if the reservation is stale.",
		mode, attempted, container, logDetail, firstLine(launchErr.Error()))
	return agentReservationReleaseMarker + "\n" + agentNeedsRedispatchMarker + "\n" +
		collapsedIssueComment("WARD-DISPATCH: failed ❌", "failure details", detail)
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
		collapsedIssueComment("WARD-DISPATCH: deferred ⏸", "deferred details", detail)
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
		collapsedIssueComment("WARD-DISPATCH: deferred ⏸", "release-assets-not-ready details", detail)
}

// withBrokerForwardingDisabled temporarily clears the read-only surface markers so
// the host-side launch does not re-enter the broker and deadlock on itself.
func withBrokerForwardingDisabled(fn func() error) error {
	dispatchBrokerLaunchMu.Lock()
	defer dispatchBrokerLaunchMu.Unlock()

	type savedEnv struct {
		value string
		set   bool
	}
	saved := map[string]savedEnv{}
	for _, key := range []string{"WARD_READONLY", envDispatchBrokerAddr, envDispatchBrokerToken} {
		if v, ok := os.LookupEnv(key); ok {
			saved[key] = savedEnv{value: v, set: true}
		}
		_ = os.Unsetenv(key)
	}
	defer func() {
		for _, key := range []string{"WARD_READONLY", envDispatchBrokerAddr, envDispatchBrokerToken} {
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
		return "", err
	}
	// Graceful stop, the exact verb reap uses (agent_reap.go): no rm, no kill, no exec.
	if serr := r.dockerExec(ctx, "stop", name); serr != nil {
		return "", fmt.Errorf("dispatch broker: docker stop %s: %w", name, serr)
	}
	return name, nil
}

// resolveEngineerStopTarget maps a stop target to one running engineer, fail-closed
// on role (ward#627): owner/repo#N matches by label, else it is a container name.
func (r *Runner) resolveEngineerStopTarget(ctx context.Context, target string) (string, error) {
	// owner/repo#N: match by the engineer identity labels (ward#364). The role filter
	// is engineer-only, and selectSingleStopTarget refuses zero / more-than-one.
	if ref, err := parseAgentIssueRef(target); err == nil && ref.Owner != "" && ref.Repo != "" {
		name, serr := selectSingleStopTarget(target, r.runningEngineersForIssue(ctx, ref))
		if serr != nil {
			return "", serr
		}
		return r.guardEngineerStop(ctx, name)
	}
	// Otherwise a container name: it must be a running container, and its role is
	// re-checked fail-closed below (never an advisor/director/session).
	if !r.containerRunning(ctx, target) {
		return "", fmt.Errorf("dispatch broker: no running container named %q to stop", target)
	}
	return r.guardEngineerStop(ctx, target)
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
			"stop only targets %s (advisor/director/session are never stopped)", name, role, roleEngineer)
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

// openDispatchLog creates ~/.ward/agent-logs/dispatch and opens the per-dispatch
// log file for req, stamped at now so re-dispatches of the same ref don't collide.
func openDispatchLog(req dispatchBrokerRequest, now time.Time) (*os.File, string, error) {
	dir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, dispatchLogName(req, now))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) // #nosec G304 -- ward-derived path under ~/.ward
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

// dispatchLogName builds a filesystem-safe per-dispatch basename: a UTC stamp (sortable,
// distinct re-dispatches) plus a requester + ref slug (attributable). Pure, for testing.
func dispatchLogName(req dispatchBrokerRequest, now time.Time) string {
	ref := ""
	if len(req.Argv) >= 2 {
		ref = req.Argv[1]
	}
	slug := config.SanitizeSlug(emptyDefault(req.Requester, "unknown") + "-" + ref)
	return fmt.Sprintf("%s-%s.log", now.UTC().Format("20060102T150405Z"), slug)
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
	case dispatchActionStop:
		return validateDispatchBrokerStop(req)
	case dispatchActionList:
		return validateDispatchBrokerList(req)
	case dispatchActionLogs:
		return validateDispatchBrokerLogs(req)
	case dispatchActionLaunch:
		return validateDispatchBrokerLaunch(req)
	default:
		return fmt.Errorf("dispatch broker: action %q refused (allowed: launch, stop, list, logs)", req.Action)
	}
}

// dispatchStopTargetRe bounds a stop's container-name target to docker's own
// name grammar, so a non-issue-ref target can only be a plausible container name.
var dispatchStopTargetRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

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

// validateDispatchBrokerLogs checks the logs shape: a target, no argv, and a
// non-negative tail. follow is just a request hint.
func validateDispatchBrokerLogs(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 {
		return fmt.Errorf("dispatch broker: logs takes no launch argv, got %v", req.Argv)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return fmt.Errorf("dispatch broker: logs requires a target (owner/repo#N or a container name)")
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
// an engineer/advisor role, an argv led by that role, and an issue ref (ward#378).
func validateDispatchBrokerLaunch(req dispatchBrokerRequest) error {
	if req.Target != "" {
		return fmt.Errorf("dispatch broker: launch takes no stop target, got %q", req.Target)
	}
	if req.Role != "engineer" && req.Role != "advisor" && req.Role != "qa" {
		return fmt.Errorf("dispatch broker: role %q refused (allowed: engineer, advisor, qa)", req.Role)
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

// runDispatchBrokerLogs resolves one engineer run to a live docker source or a
// drained archive, then streams its logs back over the request connection.
func (r *Runner) runDispatchBrokerLogs(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
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
		writeDispatchBrokerResponse(conn, "", err)
		return
	}
	writeDispatchBrokerResponse(conn, "", nil)
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
	// --config is repeatable on both roles (ward#616); --harness, its equal --agent
	// spelling, and the pre-#660 --driver alias stay approved for skew-safety (ward#660).
	valueFlags := map[string]bool{"--harness": true, "--agent": true, "--driver": true, "--config": true}
	boolFlags := map[string]bool{"--print": true}
	if role == "engineer" {
		valueFlags["--workflow"] = true
		valueFlags["--details"] = true
		for _, f := range []string{"--image", "--tag", "--ward-version", "--branch", "--repo", "--tailnet-mode"} {
			valueFlags[f] = true
		}
		for _, f := range []string{"--aws", "--tailnet", "--no-pull", "--force", "--skip-preflight", "--no-preflight", "--skip-review", "--no-review-gate"} {
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
	case "advisor":
		return brokerAdvisorArgv(c, mode, ref), true
	case "qa":
		return brokerQaArgv(c, mode, ref), true
	default:
		return nil, false
	}
}

// maybeForwardAgentDispatchToHostBroker is the in-container ref-mode gate.
// It only runs inside a read-only director surface with a broker socket.
func (r *Runner) maybeForwardAgentDispatchToHostBroker(ctx context.Context, c *cli.Command, role string, mode containerMode) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	argv, ok := brokerDispatchArgvForRole(ctx, r, c, role, mode)
	if !ok {
		return false, nil
	}
	req := dispatchBrokerRequest{
		Role:      role,
		Argv:      argv,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	if role == "advisor" {
		if err := fireAndForgetDispatchBrokerRequest(ctx, addr, req); err != nil {
			return true, err
		}
		displayArgv := redactDispatchBrokerArgv(argv)
		fmt.Fprintf(os.Stderr, "ward dispatch broker: forwarded `ward agent %s` to host ward\n", displayArgv)
		return true, nil
	}
	logPath, err := sendDispatchBrokerRequest(ctx, addr, req)
	if err != nil {
		if logPath != "" {
			return true, fmt.Errorf("%w (dispatch log: %s)", err, logPath)
		}
		return true, err
	}
	fmt.Fprintln(os.Stderr, dispatchBrokerForwardedLine(argv, logPath))
	return true, nil
}

// fireAndForgetDispatchBrokerRequest sends one dispatch request.
func fireAndForgetDispatchBrokerRequest(ctx context.Context, addr string, req dispatchBrokerRequest) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("%w: the host dispatch broker did not answer at %s "+
			"(WARD_DISPATCH_BROKER_ADDR, TCP over the docker gateway - see ward#382): %w",
			errDispatchBrokerUnavailable, addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("dispatch broker: send request: %w", err)
	}
	return nil
}

// forwardFreeformEngineerLaunchToHostBroker forwards the launch after a freshly
// filed freeform engineer issue on read-only surfaces.
func (r *Runner) forwardFreeformEngineerLaunchToHostBroker(ctx context.Context, c *cli.Command, mode containerMode, ref agentIssueRef) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	req := dispatchBrokerRequest{
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
	base := fmt.Sprintf("ward dispatch broker: forwarded `ward agent %s` to host ward", displayArgv)
	if v := dispatchBrokerWardVersion(argv); v != "" {
		base += fmt.Sprintf(" (effective ward %s)", v)
	}
	if path := strings.TrimSpace(logPath); path != "" {
		return fmt.Sprintf("%s (run output on the host at %s)", base, path)
	}
	ref := ""
	if len(argv) >= 2 {
		ref = strings.TrimSpace(argv[1])
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
// Explicit --harness/--agent/--driver wins; otherwise inherit WARD_AGENT/WARD_MODE.
func brokerDispatchHarness(c *cli.Command, fallback containerMode) containerMode {
	if c.IsSet("harness") || c.IsSet("agent") || c.IsSet("driver") {
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
	argv := []string{"engineer", ref.String(), "--harness", string(brokerDispatchHarness(c, mode))}
	argv = appendBrokerContainerFlags(argv, c)
	if c.IsSet("workflow") {
		if wf := strings.TrimSpace(c.String("workflow")); wf != "" {
			argv = append(argv, "--workflow", wf)
		}
	}
	if details := strings.TrimSpace(c.String("details")); details != "" {
		argv = append(argv, "--details", details)
	}
	if c.Bool("force") {
		argv = append(argv, "--force")
	}
	if c.Bool("skip-preflight") {
		argv = append(argv, "--skip-preflight")
	}
	if c.Bool("skip-review") || c.Bool("no-review-gate") {
		argv = append(argv, "--skip-review")
	}
	if c.Bool("print") {
		argv = append(argv, "--print")
	}
	return argv
}

func brokerAdvisorArgv(c *cli.Command, mode containerMode, ref agentIssueRef) []string {
	argv := []string{"advisor", ref.String(), "--harness", string(brokerDispatchHarness(c, mode))}
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

func appendBrokerContainerFlags(argv []string, c *cli.Command) []string {
	for _, name := range []string{"image", "tag", "branch", "tailnet-mode"} {
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
	for _, name := range []string{"aws", "tailnet", "no-pull"} {
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
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		// Papercut #1 (ward#382): fail loud - name the transport + addr so an
		// unreachable host dispatch broker never reads as a bare dial error.
		return "", fmt.Errorf("%w: the host dispatch broker did not answer at %s "+
			"(WARD_DISPATCH_BROKER_ADDR, TCP over the docker gateway - see ward#382): %w",
			errDispatchBrokerUnavailable, addr, err)
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
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("%w: the host dispatch broker did not answer at %s "+
			"(WARD_DISPATCH_BROKER_ADDR, TCP over the docker gateway - see ward#382): %w",
			errDispatchBrokerUnavailable, addr, err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("dispatch broker: send request: %w", err)
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
		return "", ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return "", fmt.Errorf("dispatch broker: read response from %s: %w", addr, result.err)
		}
		if !result.resp.OK {
			if isCredentialBrokerReply(result.resp.Error) {
				return "", fmt.Errorf("%w: %s answered as the credential broker, not the dispatch broker "+
					"(WARD_DISPATCH_BROKER_ADDR points at the wrong broker - see ward#382)",
					errDispatchBrokerUnavailable, addr)
			}
			return result.resp.LogPath, fmt.Errorf("dispatch broker: %s", result.resp.Error)
		}
		return result.resp.LogPath, nil
	}
}

// sendDispatchBrokerLogsRequest sends a logs request and returns the source + body
// stream for the caller to relay.
func sendDispatchBrokerLogsRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (string, io.ReadCloser, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("%w: the host dispatch broker did not answer at %s "+
			"(WARD_DISPATCH_BROKER_ADDR, TCP over the docker gateway - see ward#382): %w",
			errDispatchBrokerUnavailable, addr, err)
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
