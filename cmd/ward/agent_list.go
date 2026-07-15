package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_list.go wires `ward agent list` (alias `ps`): the director-surface read
// path for running engineers plus launch intents.

const agentListSchemaVersion = 5

const (
	agentLaunchPhaseQueued    = "broker accepted / queued"
	agentLaunchPhasePreflight = "pre-flight running"
	agentLaunchPhaseStarting  = "container starting"
	agentLaunchPhaseRunning   = "container running"
	agentLaunchPhaseFailed    = "failed before container start"
	agentLaunchStatusCleanup  = "cleanup-needed"
	agentLaunchStatusPartial  = "partial-launch"
)

// agentLaunchConfirmationTTL is the short lease for the prelaunch state machine:
// a reservation that never becomes visible does not get the full reservation TTL.
func agentLaunchConfirmationTTL() time.Duration { return dispatchBrokerVisibilityTimeout }

type agentListCapacity struct {
	Count       int
	Limit       *int
	Remaining   *int
	AtCapacity  *bool
	Unavailable bool
}

// agentListJSON is the stable machine shape `ward agent list --json` emits.
type agentListJSON struct {
	SchemaVersion int                  `json:"schema_version"`
	GeneratedAt   string               `json:"generated_at"`
	Count         int                  `json:"count"`
	PartialLaunch int                  `json:"partial_launch"`
	LaunchIntents int                  `json:"launch_intents"`
	CleanupNeeded int                  `json:"cleanup_needed"`
	FailedBefore  int                  `json:"failed_before_start"`
	Limit         *int                 `json:"limit"`
	Remaining     *int                 `json:"remaining"`
	AtCapacity    *bool                `json:"at_capacity"`
	Engineers     []agentListJSONEntry `json:"engineers"`
}

// agentListJSONEntry is one active engineer launch row.
type agentListJSONEntry struct {
	Container       string `json:"container"`
	Role            string `json:"role"`
	Harness         string `json:"harness"`
	Repo            string `json:"repo"`
	Issue           string `json:"issue"`
	Ref             string `json:"ref"`
	Branch          string `json:"branch"`
	Host            string `json:"host"`
	ReservedAt      string `json:"reserved_at"`
	StartedAt       string `json:"started_at"`
	Age             string `json:"age"`
	ExecutionLimit  string `json:"execution_limit"`
	BudgetRemaining string `json:"budget_remaining"`
	BudgetExpiresAt string `json:"budget_expires_at"`
	Phase           string `json:"phase"`
	Status          string `json:"status"`
	Remediation     string `json:"remediation,omitempty"`
}

type agentDockerInspectContainer struct {
	Name   string `json:"Name"`
	Config struct {
		Labels map[string]string `json:"Labels"`
		Env    []string          `json:"Env"`
	} `json:"Config"`
	State struct {
		Status    string `json:"Status"`
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
}

// agentListCommand builds `ward agent list [--json]` and its `ps` alias.
func agentListCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ps"},
		Usage:   "List running engineers and launch intents through the dispatch broker - director-surface, read-only, issue-aware.",
		Description: "list prints the running engineer containers that carry ward labels, plus launch intents before their container appears.\n" +
			"The host side uses the same label-backed broker path as `ward agent stop --print` " +
			"to resolve a single engineer, but here it reports the whole active set instead of " +
			"one target. `--json` emits a stable machine-readable schema. The `ps` alias " +
			"is kept as the docker-shaped spelling.\n\n" +
			"  ward agent list\n" +
			"  ward agent ps\n" +
			"  ward agent list --json",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "emit the stable JSON schema instead of the human table"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.list",
				SkipPolicy: true, // reads docker state only; no repo tree to gate
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runAgentList(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentList renders the running engineers locally or forwards the read through
// the director broker when the surface has one.
func (r *Runner) runAgentList(ctx context.Context, c *cli.Command) error {
	if addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)); addr != "" {
		if handled, err := r.runAgentListThroughBroker(ctx, addr, c.Bool("json")); handled || err != nil {
			return err
		}
	}
	body, err := r.renderAgentList(ctx, c.Bool("json"))
	if err != nil {
		return fmt.Errorf("ward agent list: %w", err)
	}
	w := c.Root().Writer
	if w == nil {
		w = os.Stdout
	}
	_, err = io.WriteString(w, body)
	return err
}

func (r *Runner) runAgentListThroughBroker(ctx context.Context, addr string, jsonOut bool) (bool, error) {
	if os.Getenv("WARD_READONLY") == "1" {
		if err := probeHostDispatchBroker(ctx, addr); err != nil {
			return true, err
		}
		return true, r.forwardAgentListToHostBroker(ctx, addr, jsonOut)
	}
	if err := r.forwardAgentListToHostBroker(ctx, addr, jsonOut); err != nil {
		if errors.Is(err, errDispatchBrokerUnavailable) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

// forwardAgentListToHostBroker forwards the list read and relays the rendered body.
func (r *Runner) forwardAgentListToHostBroker(ctx context.Context, addr string, jsonOut bool) error {
	req := dispatchBrokerRequest{
		Action:    dispatchActionList,
		Format:    map[bool]string{true: "json", false: "text"}[jsonOut],
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	body, err := sendDispatchBrokerListRequest(ctx, addr, req)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if _, err := io.Copy(r.Runner.Stdout, body); err != nil {
		return fmt.Errorf("ward agent list: relay host output: %w", err)
	}
	return nil
}

// sendDispatchBrokerListRequest sends a list request and returns the body stream.
func sendDispatchBrokerListRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (io.ReadCloser, error) {
	conn, err := dispatchBrokerDialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, dispatchBrokerDialDiagnostic(addr, err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("dispatch broker: send request: %w", err)
	}
	dec := json.NewDecoder(conn)
	var resp dispatchBrokerResponse
	if err := dec.Decode(&resp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("dispatch broker: read response from %s: %w", addr, err)
	}
	if !resp.OK {
		_ = conn.Close()
		if isCredentialBrokerReply(resp.Error) {
			return nil, fmt.Errorf("%w: %s answered as the credential broker, not the dispatch broker "+
				"(WARD_DISPATCH_BROKER_ADDR points at the wrong broker - see ward#382)",
				errDispatchBrokerUnavailable, addr)
		}
		return nil, fmt.Errorf("dispatch broker: %s", resp.Error)
	}
	return &connReadCloser{
		Reader: skipLeadingSeparator(io.MultiReader(dec.Buffered(), conn)),
		CloseFn: func() error {
			return conn.Close()
		},
	}, nil
}

// renderAgentList renders the active engineer runs in either human or JSON form.
func (r *Runner) renderAgentList(ctx context.Context, jsonOut bool) (string, error) {
	rows, err := r.agentListRows(ctx)
	if err != nil {
		return "", err
	}
	if jsonOut {
		payload := agentListJSONFromRows(rows)
		payload.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
		buf, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		return string(buf) + "\n", nil
	}
	return renderAgentListHuman(rows), nil
}

// agentListRows gathers the live engineer list and reservation-backed launches.
// Cleanup-needed and failed-before-start records stay visible but do not count.
func (r *Runner) agentListRows(ctx context.Context) ([]agentRunningEngineer, error) {
	names, err := r.runningEngineerContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running engineer containers: %w", err)
	}
	now := time.Now().UTC()
	rows := make([]agentRunningEngineer, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		row, err := r.runningEngineerRow(ctx, now, name)
		if err != nil {
			continue
		}
		row.Phase = agentLaunchPhaseRunning
		row.Status = emptyDefault(row.Status, "running")
		if row.Ref != "" {
			seen[row.Ref] = true
		}
		rows = append(rows, row)
	}
	pending, err := r.reservedEngineerRows(ctx, now, seen)
	if err != nil {
		return nil, err
	}
	rows = append(rows, pending...)
	rows = r.annotatePartialLaunchRows(ctx, now, rows)
	return rows, nil
}

type agentRunningEngineer struct {
	Container      string
	Role           string
	Harness        string
	Repo           string
	Issue          string
	Ref            string
	Branch         string
	Host           string
	ReservedAt     time.Time
	StartedAt      time.Time
	Age            time.Duration
	ExecutionLimit time.Duration
	Phase          string
	Status         string
	Remediation    string
}

type agentLaunchInventory struct {
	Running         int
	PartialLaunch   int
	LaunchIntents   int
	CleanupNeeded   int
	FailedBefore    int
	Count           int
	Limit           *int
	Remaining       *int
	AtCapacity      *bool
	CapacityUnknown bool
}

func agentListJSONFromRows(rows []agentRunningEngineer) agentListJSON {
	inv := agentLaunchInventoryFromRows(rows)
	payload := agentListJSON{
		SchemaVersion: agentListSchemaVersion,
		Count:         inv.Count,
		PartialLaunch: inv.PartialLaunch,
		LaunchIntents: inv.LaunchIntents,
		CleanupNeeded: inv.CleanupNeeded,
		FailedBefore:  inv.FailedBefore,
		Limit:         inv.Limit,
		Remaining:     inv.Remaining,
		AtCapacity:    inv.AtCapacity,
		Engineers:     make([]agentListJSONEntry, 0, len(rows)),
	}
	for _, row := range rows {
		payload.Engineers = append(payload.Engineers, row.toJSON())
	}
	return payload
}

func (r agentRunningEngineer) toJSON() agentListJSONEntry {
	limit := r.ExecutionLimit
	expiresAt := time.Time{}
	if limit > 0 {
		expiresAt = agentRunningEngineerAgeBase(r.ReservedAt, r.StartedAt).Add(limit)
	}
	return agentListJSONEntry{
		Container:       r.Container,
		Role:            r.Role,
		Harness:         r.Harness,
		Repo:            r.Repo,
		Issue:           r.Issue,
		Ref:             r.Ref,
		Branch:          r.Branch,
		Host:            r.Host,
		ReservedAt:      formatJSONTime(r.ReservedAt),
		StartedAt:       formatJSONTime(r.StartedAt),
		Age:             formatDuration(r.Age),
		ExecutionLimit:  formatDuration(limit),
		BudgetRemaining: agentRunBudgetSummary(r.Role, r.Age),
		BudgetExpiresAt: formatJSONTime(expiresAt),
		Phase:           emptyDefault(r.Phase, "-"),
		Status:          r.Status,
		Remediation:     r.Remediation,
	}
}

func agentListCapacityForCount(count int) agentListCapacity {
	defs, err := currentSmartDefaultsWithError()
	limit := defs.engineerContainerLimit
	capacity := agentListCapacity{Count: count}
	if err != nil {
		capacity.Unavailable = true
	}
	if limit <= 0 {
		capacity.Unavailable = true
		return capacity
	}
	capacity.Limit = &limit
	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}
	capacity.Remaining = &remaining
	atCapacity := count >= limit
	capacity.AtCapacity = &atCapacity
	return capacity
}

type agentLaunchRowKind string

const (
	agentLaunchRowRunning       agentLaunchRowKind = "running"
	agentLaunchRowActiveIntent  agentLaunchRowKind = "active-intent"
	agentLaunchRowCleanupNeeded agentLaunchRowKind = "cleanup-needed"
	agentLaunchRowFailedBefore  agentLaunchRowKind = "failed-before-start"
)

func agentLaunchRowClass(row agentRunningEngineer) agentLaunchRowKind {
	switch {
	case row.Phase == agentLaunchPhaseRunning:
		return agentLaunchRowRunning
	case row.Phase == agentLaunchPhaseFailed || strings.EqualFold(strings.TrimSpace(row.Status), "failed"):
		return agentLaunchRowFailedBefore
	case strings.EqualFold(strings.TrimSpace(row.Status), agentLaunchStatusCleanup):
		return agentLaunchRowCleanupNeeded
	default:
		return agentLaunchRowActiveIntent
	}
}

func (r *Runner) annotatePartialLaunchRows(ctx context.Context, now time.Time, rows []agentRunningEngineer) []agentRunningEngineer {
	for i := range rows {
		if rows[i].Phase != agentLaunchPhaseRunning {
			continue
		}
		partial, remediation, err := r.partialLaunchState(ctx, rows[i], now)
		if err != nil || !partial {
			continue
		}
		rows[i].Status = agentLaunchStatusPartial
		rows[i].Remediation = remediation
	}
	return rows
}

func (r *Runner) partialLaunchState(ctx context.Context, row agentRunningEngineer, now time.Time) (bool, string, error) {
	ref, err := parseAgentIssueRef(row.Ref)
	if err != nil {
		return false, "", nil //nolint:nilerr // non-issue rows cannot be partial-launch records
	}
	if ref.Owner == "" || ref.Repo == "" || ref.Number <= 0 {
		return false, "", nil
	}
	cl, cerr := r.hostTrackerClient(ctx, ref.trackerOrDefault(), containerMode(strings.TrimSpace(row.Harness)))
	if cerr != nil {
		return false, "", cerr
	}
	comments, cerr := cl.ListIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if cerr != nil {
		return false, "", cerr
	}
	if _, held := freshReservationComment(comments, now, agentReservationTTL()); held {
		return false, "", nil
	}
	return true, partialLaunchRemediation(row), nil
}

func partialLaunchRemediation(row agentRunningEngineer) string {
	return fmt.Sprintf("issue %s is missing the reservation-held marker; re-post the reservation comment or stop and re-dispatch %s", emptyDefault(row.Ref, "the run"), emptyDefault(row.Container, "the container"))
}

func agentLaunchInventoryFromRows(rows []agentRunningEngineer) agentLaunchInventory {
	inv := agentLaunchInventory{}
	for _, row := range rows {
		switch agentLaunchRowClass(row) {
		case agentLaunchRowRunning:
			inv.Running++
			inv.Count++
			if strings.EqualFold(strings.TrimSpace(row.Status), agentLaunchStatusPartial) {
				inv.PartialLaunch++
			}
		case agentLaunchRowActiveIntent:
			inv.LaunchIntents++
			inv.Count++
		case agentLaunchRowCleanupNeeded:
			inv.CleanupNeeded++
		case agentLaunchRowFailedBefore:
			inv.FailedBefore++
		}
	}
	capacity := agentListCapacityForCount(inv.Count)
	inv.Limit = capacity.Limit
	inv.Remaining = capacity.Remaining
	inv.AtCapacity = capacity.AtCapacity
	inv.CapacityUnknown = capacity.Unavailable
	return inv
}

func agentLaunchInventoryFromRowsWithScope(rows []agentRunningEngineer, scope map[string]bool) agentLaunchInventory {
	if len(scope) == 0 {
		return agentLaunchInventoryFromRows(rows)
	}
	filtered := make([]agentRunningEngineer, 0, len(rows))
	for _, row := range rows {
		if scope[row.Repo] {
			filtered = append(filtered, row)
		}
	}
	return agentLaunchInventoryFromRows(filtered)
}

func (r *Runner) reservedEngineerRows(ctx context.Context, now time.Time, seen map[string]bool) ([]agentRunningEngineer, error) {
	_ = ctx
	dir, err := agentReservationCacheDir()
	if err != nil {
		return nil, err
	}
	entries, err := readAgentReservationCacheEntries(dir)
	if err != nil {
		return nil, err
	}
	rows := make([]agentRunningEngineer, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if row, ok := activeReservedEngineerRow(ctx, r, path, now, seen); ok {
			rows = append(rows, row)
			seen[row.Ref] = true
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		switch {
		case rows[i].ReservedAt.Equal(rows[j].ReservedAt):
			return rows[i].Ref < rows[j].Ref
		case rows[i].ReservedAt.IsZero():
			return false
		case rows[j].ReservedAt.IsZero():
			return true
		default:
			return rows[i].ReservedAt.After(rows[j].ReservedAt)
		}
	})
	return rows, nil
}

func readAgentReservationCacheEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return nil, fmt.Errorf("restore reservation cache dir: %w", mkErr)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("list reserved engineer launches: %w", err)
	}
	return entries, nil
}

func activeReservedEngineerRow(ctx context.Context, r *Runner, path string, now time.Time, seen map[string]bool) (agentRunningEngineer, bool) {
	res, ok, err := readAgentReservation(path)
	if err != nil || !ok || res == nil {
		return agentRunningEngineer{}, false
	}
	ref := agentIssueRef{Owner: res.Owner, Repo: res.Repo, Number: res.Number}
	if ref.Owner == "" || ref.Repo == "" || ref.Number <= 0 {
		return agentRunningEngineer{}, false
	}
	if seen[ref.String()] || seen[strings.TrimSpace(res.Container)] {
		return agentRunningEngineer{}, false
	}
	row, ok := reservationCacheEngineerRow(ctx, r, path, ref, res, now)
	if !ok {
		return agentRunningEngineer{}, false
	}
	return row, true
}

func reservationCacheEngineerRow(ctx context.Context, r *Runner, path string, ref agentIssueRef, res *agentReservation, now time.Time) (agentRunningEngineer, bool) {
	phase, status, phaseOK := dispatchLaunchPhaseForReservation(ref)
	row := reservedEngineerRowFromReservation(ref, res, now, phase, status, phaseOK)
	if phaseOK && phase == agentLaunchPhaseFailed {
		row.Status = "failed"
		return row, true
	}
	if !reservationLaunchFresh(res.At, now) {
		if phaseOK {
			row.Status = agentLaunchStatusCleanup
			return row, true
		}
		cl, cerr := r.hostTrackerClient(ctx, ref.trackerOrDefault(), containerMode(strings.TrimSpace(res.Mode)))
		if cerr != nil {
			return row, true
		}
		held, cerr := reservationCacheRowHeld(ctx, cl, ref, now)
		if cerr != nil {
			return row, true
		}
		if !held {
			_ = removeAgentReservationArtifacts(path)
			return agentRunningEngineer{}, false
		}
		row.Status = agentLaunchStatusCleanup
		return row, true
	}
	cl, cerr := r.hostTrackerClient(ctx, ref.trackerOrDefault(), containerMode(strings.TrimSpace(res.Mode)))
	if cerr != nil {
		return row, true
	}
	held, cerr := reservationCacheRowHeld(ctx, cl, ref, now)
	if cerr != nil {
		return row, true
	}
	if !held {
		_ = removeAgentReservationArtifacts(path)
		return agentRunningEngineer{}, false
	}
	return row, true
}

func reservationCacheRowHeld(ctx context.Context, cl Tracker, ref agentIssueRef, now time.Time) (bool, error) {
	comments, err := cl.ListIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return false, err
	}
	_, held := freshReservationComment(comments, now, agentReservationTTL())
	return held, nil
}

func reservationLaunchFresh(at, now time.Time) bool {
	return reservationFresh(at, now, agentLaunchConfirmationTTL())
}

func reservedEngineerRowFromReservation(ref agentIssueRef, res *agentReservation, now time.Time, phase, status string, phaseOK bool) agentRunningEngineer {
	row := agentRunningEngineer{
		Container:  emptyDefault(strings.TrimSpace(res.Container), "(reserved)"),
		Role:       roleEngineer,
		Harness:    emptyDefault(strings.TrimSpace(res.Mode), "-"),
		Repo:       ref.repoSlug(),
		Issue:      strconv.Itoa(ref.Number),
		Ref:        ref.String(),
		Branch:     strings.TrimSpace(res.Branch),
		Host:       strings.TrimSpace(res.Host),
		ReservedAt: res.At,
		Age:        now.Sub(res.At),
		Phase:      agentLaunchPhaseQueued,
		Status:     "reserved",
	}
	if phaseOK {
		row.Phase = phase
		row.Status = status
	}
	if limit, ok := agentRoleExecutionLimit(roleEngineer); ok {
		row.ExecutionLimit = limit
	}
	return row
}

func dispatchLaunchPhaseForReservation(ref agentIssueRef) (phase, status string, ok bool) {
	body, found, err := latestDispatchLogBodyForRef(ref)
	if err != nil || !found {
		return "", "", false
	}
	return dispatchLaunchPhaseFromLog(body)
}

func dispatchLaunchFailedBody(lower string) bool {
	return strings.Contains(lower, "launch failed") ||
		strings.Contains(lower, "warded_workflow: dispatch-failed") ||
		strings.Contains(lower, "ward-dispatch: failed") ||
		strings.Contains(lower, "pre-flight no-go") ||
		strings.Contains(lower, "wrong-repo") ||
		strings.Contains(lower, "released failed dispatch reservation") ||
		strings.Contains(lower, "released deferred dispatch reservation") ||
		strings.Contains(lower, "released deferred release-assets-not-ready reservation")
}

func dispatchLaunchStartingBody(lower string) bool {
	return strings.Contains(lower, "wrote launch env file") || strings.Contains(lower, "launch start:")
}

func dispatchLaunchPreflightBody(lower string) bool {
	return strings.Contains(lower, "preflight start for") ||
		strings.Contains(lower, "pre-flight - asking") ||
		strings.Contains(lower, "launch plan ready") ||
		strings.Contains(lower, "pulling ")
}

func dispatchLaunchQueuedBody(lower string) bool {
	return strings.Contains(lower, "reservation acquired for")
}

func dispatchLaunchPhaseFromLog(body string) (phase, status string, ok bool) {
	lower := strings.ToLower(body)
	switch {
	case dispatchLaunchFailedBody(lower):
		return agentLaunchPhaseFailed, "failed", true
	case dispatchLaunchStartingBody(lower):
		return agentLaunchPhaseStarting, "starting", true
	case dispatchLaunchPreflightBody(lower):
		return agentLaunchPhasePreflight, "reserved", true
	case dispatchLaunchQueuedBody(lower):
		return agentLaunchPhaseQueued, "reserved", true
	default:
		return agentLaunchPhaseQueued, "reserved", true
	}
}

func latestDispatchLogBodyForRef(ref agentIssueRef) (string, bool, error) {
	path, found, err := latestDispatchConsolePathForRef(ref)
	if err != nil || !found {
		return "", false, err
	}
	b, err := os.ReadFile(path) // #nosec G304 -- ward-derived log path under ~/.ward
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

func (r *Runner) runningEngineerRow(ctx context.Context, now time.Time, name string) (agentRunningEngineer, error) {
	out, err := r.dockerCapture(ctx, "inspect", name)
	if err != nil {
		return agentRunningEngineer{}, err
	}
	var snaps []agentDockerInspectContainer
	if err := json.Unmarshal(out, &snaps); err != nil {
		return agentRunningEngineer{}, err
	}
	if len(snaps) != 1 {
		return agentRunningEngineer{}, fmt.Errorf("dock inspect %q returned %d objects", name, len(snaps))
	}
	return agentRunningEngineerFromInspect(now, snaps[0]), nil
}

func agentRunningEngineerFromInspect(now time.Time, snap agentDockerInspectContainer) agentRunningEngineer {
	name := strings.TrimPrefix(strings.TrimSpace(snap.Name), "/")
	env := agentEnvMap(snap.Config.Env)
	labels := snap.Config.Labels
	role := firstNonEmptyList(strings.TrimSpace(labels[labelRole]), strings.TrimSpace(env["WARD_ROLE"]))
	if role == "" {
		role = roleEngineer
	}
	harness := firstNonEmptyList(strings.TrimSpace(labels[labelDriver]), strings.TrimSpace(env["WARD_MODE"]))
	repoSlug := firstNonEmptyList(strings.TrimSpace(labels[labelRepo]), strings.TrimSpace(env["WARD_TARGET_REPO"]))
	owner := firstNonEmptyList(strings.TrimSpace(env["WARD_TARGET_OWNER"]), strings.TrimSpace(env["WARD_OWNER"]))
	repoName := firstNonEmptyList(strings.TrimSpace(env["WARD_TARGET_NAME"]), repoNameFromSlug(repoSlug))
	issueNum, _ := strconv.Atoi(strings.TrimSpace(firstNonEmptyList(strings.TrimSpace(labels[labelIssue]), strings.TrimSpace(env["WARD_TARGET_ISSUE"]))))
	branch := strings.TrimSpace(env["WARD_BRANCH"])
	status := strings.TrimSpace(snap.State.Status)
	startedAt, _ := parseDockerInspectTime(snap.State.StartedAt)
	reservedAt, host, branch := agentRunningEngineerReservationDetails(owner, repoName, issueNum, branch)
	repo := repoSlug
	if owner != "" && repoName != "" {
		repo = fmt.Sprintf("%s/%s", owner, repoName)
	}
	out := agentRunningEngineer{
		Container:  name,
		Role:       role,
		Harness:    harness,
		Repo:       repo,
		Issue:      "",
		Ref:        "",
		Branch:     branch,
		Host:       host,
		ReservedAt: reservedAt,
		StartedAt:  startedAt,
		Phase:      agentLaunchPhaseRunning,
		Status:     status,
	}
	if issueNum > 0 {
		out.Issue = strconv.Itoa(issueNum)
	}
	if owner != "" && repoName != "" && issueNum > 0 {
		out.Ref = fmt.Sprintf("%s/%s#%d", owner, repoName, issueNum)
	}
	if ageAt := agentRunningEngineerAgeBase(reservedAt, startedAt); !ageAt.IsZero() {
		out.Age = now.Sub(ageAt)
	}
	if limit, ok := agentRoleExecutionLimit(role); ok {
		out.ExecutionLimit = limit
	}
	return out
}

func agentRunningEngineerReservationDetails(owner, repoName string, issueNum int, branch string) (time.Time, string, string) {
	reservedAt := time.Time{}
	host := ""
	if issueNum > 0 && owner != "" && repoName != "" {
		if res, ok, _ := readAgentReservationMust(agentIssueRef{Owner: owner, Repo: repoName, Number: issueNum}); ok {
			reservedAt = res.At
			host = strings.TrimSpace(res.Host)
			if branch == "" {
				branch = strings.TrimSpace(res.Branch)
			}
		}
	}
	return reservedAt, host, branch
}

func agentRunningEngineerAgeBase(reservedAt, startedAt time.Time) time.Time {
	if !reservedAt.IsZero() {
		return reservedAt
	}
	return startedAt
}

func readAgentReservationMust(ref agentIssueRef) (*agentReservation, bool, error) {
	path, err := agentReservationPath(ref)
	if err != nil {
		return nil, false, err
	}
	return readAgentReservation(path)
}

func agentEnvMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, line := range env {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}

func repoNameFromSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	if _, name, ok := strings.Cut(slug, "/"); ok {
		return name
	}
	return slug
}

func firstNonEmptyList(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func formatJSONTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.Round(time.Second).String()
}

func renderAgentListHuman(rows []agentRunningEngineer) string {
	inv := agentLaunchInventoryFromRows(rows)
	var b strings.Builder
	appendAgentListHeader(&b, inv)
	b.WriteString("\n")
	if len(rows) == 0 {
		b.WriteString("\n  no active engineer launches.\n")
		return b.String()
	}
	for _, row := range rows {
		appendAgentListRow(&b, row)
	}
	return b.String()
}

func appendAgentListHeader(b *strings.Builder, inv agentLaunchInventory) {
	capacity := agentListCapacity{Count: inv.Count, Limit: inv.Limit, Remaining: inv.Remaining, AtCapacity: inv.AtCapacity, Unavailable: inv.CapacityUnknown}
	fmt.Fprintf(b, "ward agent: active engineer launches %s", formatAgentListCapacity(capacity))
	appendAgentListLaunchIntents(b, inv.LaunchIntents)
	appendAgentListCleanupNeeded(b, inv.CleanupNeeded)
	appendAgentListPartialLaunch(b, inv.PartialLaunch)
	appendAgentListFailedBefore(b, inv.FailedBefore)
}

func appendAgentListLaunchIntents(b *strings.Builder, count int) {
	if count <= 0 {
		return
	}
	noun := "launch intents"
	if count == 1 {
		noun = "launch intent"
	}
	fmt.Fprintf(b, " + %d %s pending", count, noun)
}

func appendAgentListCleanupNeeded(b *strings.Builder, count int) {
	if count <= 0 {
		return
	}
	noun := "records"
	if count == 1 {
		noun = "record"
	}
	fmt.Fprintf(b, " + %d cleanup-needed %s", count, noun)
}

func appendAgentListPartialLaunch(b *strings.Builder, count int) {
	if count <= 0 {
		return
	}
	noun := "launches"
	if count == 1 {
		noun = "launch"
	}
	fmt.Fprintf(b, " + %d partial %s", count, noun)
}

func appendAgentListFailedBefore(b *strings.Builder, count int) {
	if count <= 0 {
		return
	}
	noun := "records"
	if count == 1 {
		noun = "record"
	}
	fmt.Fprintf(b, " + %d failed-before-start %s", count, noun)
}

func appendAgentListRow(b *strings.Builder, row agentRunningEngineer) {
	fmt.Fprintf(b, "\n  %s\n", emptyDefault(row.Container, "(unknown container)"))
	fmt.Fprintf(b, "    ref:       %s\n", emptyDefault(row.Ref, "-"))
	fmt.Fprintf(b, "    harness:   %s\n", emptyDefault(row.Harness, "-"))
	fmt.Fprintf(b, "    repo:      %s\n", emptyDefault(row.Repo, "-"))
	fmt.Fprintf(b, "    issue:     %s\n", emptyDefault(row.Issue, "-"))
	fmt.Fprintf(b, "    branch:    %s\n", emptyDefault(row.Branch, "-"))
	fmt.Fprintf(b, "    host:      %s\n", emptyDefault(row.Host, "-"))
	fmt.Fprintf(b, "    reserved:  %s\n", emptyDefault(formatJSONTime(row.ReservedAt), "-"))
	fmt.Fprintf(b, "    started:   %s\n", emptyDefault(formatJSONTime(row.StartedAt), "-"))
	fmt.Fprintf(b, "    age:       %s\n", emptyDefault(formatDuration(row.Age), "-"))
	if budget := agentRunBudgetSummary(row.Role, row.Age); budget != "" {
		fmt.Fprintf(b, "    budget:    %s\n", budget)
	}
	fmt.Fprintf(b, "    phase:     %s\n", emptyDefault(row.Phase, "-"))
	fmt.Fprintf(b, "    status:    %s\n", emptyDefault(row.Status, "-"))
	if strings.TrimSpace(row.Remediation) != "" {
		fmt.Fprintf(b, "    remediation: %s\n", row.Remediation)
	}
}

func formatAgentListCapacity(capacity agentListCapacity) string {
	note := ""
	if capacity.Unavailable {
		note = ", capacity source unavailable through broker"
	}
	if capacity.Limit == nil {
		return fmt.Sprintf("(%d, capacity unavailable through broker)", capacity.Count)
	}
	if capacity.Remaining == nil || capacity.AtCapacity == nil {
		return fmt.Sprintf("(%d/%d)%s", capacity.Count, *capacity.Limit, note)
	}
	if *capacity.AtCapacity {
		return fmt.Sprintf("(%d/%d, at capacity)%s", capacity.Count, *capacity.Limit, note)
	}
	if *capacity.Remaining == 1 {
		return fmt.Sprintf("(%d/%d, 1 slot free)%s", capacity.Count, *capacity.Limit, note)
	}
	return fmt.Sprintf("(%d/%d, %d slots free)%s", capacity.Count, *capacity.Limit, *capacity.Remaining, note)
}
