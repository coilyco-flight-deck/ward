package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
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

const (
	// dispatchActionLaunch is the default action: launch a sibling engineer/advisor
	// run. An empty Action normalizes to it, keeping older launch requests byte-compatible.
	dispatchActionLaunch = "launch"
	// dispatchActionStop is the targeted control action (ward#627): docker-stop one
	// running engineer named by Target - stop-only, engineer-only, no launch argv.
	dispatchActionStop = "stop"
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
	Requester string `json:"requester,omitempty"`
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
	// LogPath is the host path the served run's stdout/stderr were redirected to,
	// so the requesting surface can name it without any bytes hitting the TTY (ward#389).
	LogPath string `json:"log_path,omitempty"`
}

// advisorDispatchRunner runs the advisor ref-mode work. Tests replace it so the broker
// can prove it returns before the long research/comment pass finishes.
var advisorDispatchRunner = func(ctx context.Context, argv []string) error {
	return agentAdvisorCommand().Run(ctx, argv)
}

// dispatchStdioMu serializes the process-global os.Stdout/os.Stderr swap that keeps
// a served run's deploy output off the shared read-only TUI (ward#389).
var dispatchStdioMu sync.Mutex

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
	logPath, err := r.runHostDispatchBrokerRequest(ctx, req)
	writeDispatchBrokerResponse(conn, logPath, err)
}

func writeDispatchBrokerResponse(conn net.Conn, logPath string, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, LogPath: logPath}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// runHostDispatchBrokerRequest serves one validated run in-process, redirecting its
// deploy output to a per-dispatch log so it can't corrupt the surface TUI (ward#389).
func (r *Runner) runHostDispatchBrokerRequest(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	if err := validateDispatchBrokerRequest(req); err != nil {
		return "", err
	}
	// Stop is a targeted control action, not a launch: it resolves + docker-stops one
	// engineer, so it takes no dispatch log, no stdio redirect, and no ref lock (ward#627).
	if dispatchAction(req.Action) == dispatchActionStop {
		return r.runDispatchBrokerStop(ctx, req)
	}
	switch req.Role {
	case "engineer":
		// Serialize on the ref so two same-N dispatches can't both reserve + spin:
		// the second waits, then its reservation check sees the first's hold (ward#600).
		if ref, err := parseAgentIssueRef(req.Argv[1]); err == nil {
			lock := dispatchRefLock(ref.String())
			lock.Lock()
			defer lock.Unlock()
		}
		logf, logPath, err := openDispatchLog(req, time.Now())
		if err != nil {
			// Fail loud rather than fall back to the TTY: a broken log dir must not
			// silently reroute the flood back onto the corrupted surface (ward#389).
			return "", fmt.Errorf("dispatch broker: open run log: %w", err)
		}
		defer func() { _ = logf.Close() }()
		restore := redirectStdioToLog(logf)
		defer restore()

		_, _ = fmt.Fprintf(logf, "ward dispatch broker: %s requested `ward agent %s`\n",
			emptyDefault(req.Requester, "unknown-container"), strings.Join(req.Argv, " "))
		return logPath, agentEngineerCommand().Run(ctx, req.Argv)
	case "advisor":
		return r.runHostDispatchBrokerAdvisor(ctx, req)
	default:
		return "", fmt.Errorf("role %q is not dispatchable", req.Role)
	}
}

// runHostDispatchBrokerAdvisor launches advisor ref-mode in the background.
// That returns a prompt dispatch result instead of watching the full pass inline.
func (r *Runner) runHostDispatchBrokerAdvisor(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return "", err
	}
	lock := dispatchRefLock(ref.String())
	lock.Lock()

	logf, logPath, err := openDispatchLog(req, time.Now())
	if err != nil {
		lock.Unlock()
		// Fail loud rather than fall back to the TTY: a broken log dir must not
		// silently reroute the launch back onto the corrupted surface (ward#389).
		return "", fmt.Errorf("dispatch broker: open run log: %w", err)
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				_, _ = fmt.Fprintf(logf, "ward dispatch broker: advisor dispatch panicked: %v\n", rec)
			}
		}()
		defer lock.Unlock()
		defer func() { _ = logf.Close() }()
		restore := redirectStdioToLog(logf)
		defer restore()

		_, _ = fmt.Fprintf(logf, "ward dispatch broker: %s requested `ward agent %s`\n",
			emptyDefault(req.Requester, "unknown-container"), strings.Join(req.Argv, " "))
		if err := advisorDispatchRunner(ctx, req.Argv); err != nil {
			_, _ = fmt.Fprintf(logf, "ward dispatch broker: advisor dispatch finished with error: %v\n", err)
		}
	}()
	return logPath, nil
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

// selectSingleStopTarget picks exactly one engineer from a match set, refusing on
// zero or more than one (ambiguous) with the candidates listed, not a guess (ward#627).
func selectSingleStopTarget(target string, names []string) (string, error) {
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", fmt.Errorf("dispatch broker: no running engineer container matches %q - nothing to stop", target)
	default:
		return "", fmt.Errorf("dispatch broker: %q matches %d running engineer containers (%s) - "+
			"refusing to guess; stop one by its container name", target, len(names), strings.Join(names, ", "))
	}
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
	case dispatchActionLaunch:
		return validateDispatchBrokerLaunch(req)
	default:
		return fmt.Errorf("dispatch broker: action %q refused (allowed: launch, stop)", req.Action)
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

// validateDispatchBrokerLaunch is the launch-request shape (the original narrow API):
// an engineer/advisor role, an argv led by that role, and an issue ref (ward#378).
func validateDispatchBrokerLaunch(req dispatchBrokerRequest) error {
	if req.Target != "" {
		return fmt.Errorf("dispatch broker: launch takes no stop target, got %q", req.Target)
	}
	if req.Role != "engineer" && req.Role != "advisor" {
		return fmt.Errorf("dispatch broker: role %q refused (allowed: engineer, advisor)", req.Role)
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

func validateDispatchBrokerArgv(role string, tail []string) error {
	// --config is repeatable on both roles (ward#616); --harness, its equal --agent
	// spelling, and the pre-#660 --driver alias stay approved for skew-safety (ward#660).
	valueFlags := map[string]bool{"--harness": true, "--agent": true, "--driver": true, "--config": true}
	boolFlags := map[string]bool{"--print": true}
	if role == "engineer" {
		for _, f := range []string{"--image", "--tag", "--ward-version", "--branch", "--repo", "--tailnet-mode"} {
			valueFlags[f] = true
		}
		for _, f := range []string{"--aws", "--tailnet", "--no-pull", "--force", "--skip-preflight", "--no-preflight", "--skip-review", "--no-review-gate"} {
			boolFlags[f] = true
		}
		return validateDispatchBrokerFlags(role, tail, valueFlags, boolFlags, false)
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

// maybeForwardAgentDispatchToHostBroker is the in-container ref-mode gate.
// It only runs inside a read-only director surface with a broker socket.
func (r *Runner) maybeForwardAgentDispatchToHostBroker(ctx context.Context, c *cli.Command, role string, mode containerMode) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	var argv []string
	switch role {
	case "engineer":
		ref, ok := r.brokerDispatchRef(ctx, c.Args().First())
		if !ok {
			return false, nil
		}
		argv = brokerEngineerArgv(c, mode, ref)
	case "advisor":
		ref, ok := r.brokerDispatchRef(ctx, c.Args().First())
		if !ok {
			return false, nil
		}
		argv = brokerAdvisorArgv(c, mode, ref)
	default:
		return false, nil
	}
	req := dispatchBrokerRequest{
		Role:      role,
		Argv:      argv,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	logPath, err := sendDispatchBrokerRequest(ctx, addr, req)
	if err != nil {
		return true, err
	}
	// This line is captured as tool output by the surface agent, not written to the
	// raw TTY, so naming the host-side run log here is safe and aids discovery.
	if logPath != "" {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: forwarded `ward agent %s` to host ward (run output on the host at %s)\n",
			strings.Join(argv, " "), logPath)
	} else {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: forwarded `ward agent %s` to host ward\n", strings.Join(argv, " "))
	}
	return true, nil
}

func (r *Runner) brokerDispatchRef(ctx context.Context, arg string) (agentIssueRef, bool) {
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return agentIssueRef{}, false
	}
	return ref, true
}

func brokerEngineerArgv(c *cli.Command, mode containerMode, ref agentIssueRef) []string {
	argv := []string{"engineer", ref.String(), "--harness", string(mode)}
	argv = appendBrokerContainerFlags(argv, c)
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
	argv := []string{"advisor", ref.String(), "--harness", string(mode)}
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
	for _, name := range []string{"image", "tag", "ward-version", "branch", "tailnet-mode"} {
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
	return argv
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

// isCredentialBrokerReply spots the credential broker's protocol-version refusal:
// the dispatch client reached cmd/ward/broker.go, not the dispatch broker (ward#382).
func isCredentialBrokerReply(msg string) bool {
	return strings.Contains(msg, "unsupported protocol version")
}
