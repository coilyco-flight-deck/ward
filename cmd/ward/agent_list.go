package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_list.go wires `ward agent list` (alias `ps`): the director-surface read
// path for running engineer containers.

const agentListSchemaVersion = 1

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
	Limit         *int                 `json:"limit"`
	Remaining     *int                 `json:"remaining"`
	AtCapacity    *bool                `json:"at_capacity"`
	Engineers     []agentListJSONEntry `json:"engineers"`
}

// agentListJSONEntry is one running engineer container.
type agentListJSONEntry struct {
	Container  string `json:"container"`
	Harness    string `json:"harness"`
	Repo       string `json:"repo"`
	Issue      string `json:"issue"`
	Ref        string `json:"ref"`
	Branch     string `json:"branch"`
	Host       string `json:"host"`
	ReservedAt string `json:"reserved_at"`
	StartedAt  string `json:"started_at"`
	Age        string `json:"age"`
	Status     string `json:"status"`
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
		Usage:   "List running engineer containers through the dispatch broker - director-surface, read-only, issue-aware.",
		Description: "list prints the running engineer containers that carry ward labels.\n" +
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
	if addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)); addr != "" && os.Getenv("WARD_READONLY") == "1" {
		return r.forwardAgentListToHostBroker(ctx, addr, c.Bool("json"))
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
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: the host dispatch broker did not answer at %s "+
			"(WARD_DISPATCH_BROKER_ADDR, TCP over the docker gateway - see ward#382): %w",
			errDispatchBrokerUnavailable, addr, err)
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
	rows, err := r.runningEngineerRows(ctx)
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

// runningEngineerRows gathers the live engineer list from docker + reservation data.
func (r *Runner) runningEngineerRows(ctx context.Context) ([]agentRunningEngineer, error) {
	names, err := r.runningEngineerContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running engineer containers: %w", err)
	}
	if len(names) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	rows := make([]agentRunningEngineer, 0, len(names))
	for _, name := range names {
		row, err := r.runningEngineerRow(ctx, now, name)
		if err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type agentRunningEngineer struct {
	Container  string
	Harness    string
	Repo       string
	Issue      string
	Ref        string
	Branch     string
	Host       string
	ReservedAt time.Time
	StartedAt  time.Time
	Age        time.Duration
	Status     string
}

func agentListJSONFromRows(rows []agentRunningEngineer) agentListJSON {
	capacity := agentListCapacityForCount(len(rows))
	payload := agentListJSON{
		SchemaVersion: agentListSchemaVersion,
		Count:         capacity.Count,
		Limit:         capacity.Limit,
		Remaining:     capacity.Remaining,
		AtCapacity:    capacity.AtCapacity,
		Engineers:     make([]agentListJSONEntry, 0, len(rows)),
	}
	for _, row := range rows {
		payload.Engineers = append(payload.Engineers, row.toJSON())
	}
	return payload
}

func (r agentRunningEngineer) toJSON() agentListJSONEntry {
	return agentListJSONEntry{
		Container:  r.Container,
		Harness:    r.Harness,
		Repo:       r.Repo,
		Issue:      r.Issue,
		Ref:        r.Ref,
		Branch:     r.Branch,
		Host:       r.Host,
		ReservedAt: formatJSONTime(r.ReservedAt),
		StartedAt:  formatJSONTime(r.StartedAt),
		Age:        formatDuration(r.Age),
		Status:     r.Status,
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
		Harness:    harness,
		Repo:       repo,
		Issue:      "",
		Ref:        "",
		Branch:     branch,
		Host:       host,
		ReservedAt: reservedAt,
		StartedAt:  startedAt,
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
	capacity := agentListCapacityForCount(len(rows))
	var b strings.Builder
	fmt.Fprintf(&b, "ward agent: running engineer containers %s\n", formatAgentListCapacity(capacity))
	if len(rows) == 0 {
		b.WriteString("\n  no running engineer containers.\n")
		return b.String()
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "\n  %s\n", emptyDefault(row.Container, "(unknown container)"))
		fmt.Fprintf(&b, "    ref:       %s\n", emptyDefault(row.Ref, "-"))
		fmt.Fprintf(&b, "    harness:   %s\n", emptyDefault(row.Harness, "-"))
		fmt.Fprintf(&b, "    repo:      %s\n", emptyDefault(row.Repo, "-"))
		fmt.Fprintf(&b, "    issue:     %s\n", emptyDefault(row.Issue, "-"))
		fmt.Fprintf(&b, "    branch:    %s\n", emptyDefault(row.Branch, "-"))
		fmt.Fprintf(&b, "    host:      %s\n", emptyDefault(row.Host, "-"))
		fmt.Fprintf(&b, "    reserved:  %s\n", emptyDefault(formatJSONTime(row.ReservedAt), "-"))
		fmt.Fprintf(&b, "    started:   %s\n", emptyDefault(formatJSONTime(row.StartedAt), "-"))
		fmt.Fprintf(&b, "    age:       %s\n", emptyDefault(formatDuration(row.Age), "-"))
		fmt.Fprintf(&b, "    status:    %s\n", emptyDefault(row.Status, "-"))
	}
	return b.String()
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
