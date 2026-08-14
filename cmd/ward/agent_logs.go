package main

import (
	"bytes"
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

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_logs.go wires `ward agent logs`: a director surface can read one engineer
// run's logs through the dispatch broker, or host-side when no broker is present.

// agentLogSourceKind labels the source the broker resolved.
type agentLogSourceKind string

const (
	agentLogSourceDocker agentLogSourceKind = "docker"
	agentLogSourceFile   agentLogSourceKind = "file"
	agentLogSourceInline agentLogSourceKind = "inline"
)

const (
	agentLogsDefaultGroupTail = 100
	composeProjectLabel       = "com.docker.compose.project"
)

type agentLogArtifact string

const (
	agentLogArtifactConsole    agentLogArtifact = "console"
	agentLogArtifactTranscript agentLogArtifact = "transcript"
	agentLogArtifactMeta       agentLogArtifact = "meta"
	agentLogArtifactFriction   agentLogArtifact = "friction"
	agentLogArtifactDispatch   agentLogArtifact = "dispatch"
	agentLogArtifactRelease    agentLogArtifact = "release"
)

type agentLogsResolveOptions struct {
	Artifact agentLogArtifact
}

func parseAgentLogArtifact(s string) (agentLogArtifact, error) {
	switch agentLogArtifact(strings.ToLower(strings.TrimSpace(s))) {
	case "", agentLogArtifactConsole:
		return agentLogArtifactConsole, nil
	case agentLogArtifactTranscript:
		return agentLogArtifactTranscript, nil
	case agentLogArtifactMeta:
		return agentLogArtifactMeta, nil
	case agentLogArtifactFriction:
		return agentLogArtifactFriction, nil
	case agentLogArtifactDispatch:
		return agentLogArtifactDispatch, nil
	case agentLogArtifactRelease:
		return agentLogArtifactRelease, nil
	default:
		return "", fmt.Errorf("unknown --artifact %q (allowed: console, transcript, meta, friction, dispatch, release)", s)
	}
}

func (o agentLogsResolveOptions) artifact() agentLogArtifact {
	if o.Artifact == "" {
		return agentLogArtifactConsole
	}
	return o.Artifact
}

// agentLogsLiveTranscriptTimeout bounds best-effort live transcript reads so
// silent fresh containers explain state instead of waiting on an absent tree.
var agentLogsLiveTranscriptTimeout = 2 * time.Second

// agentLogSource is the resolved log source the host prints and streams from.
type agentLogSource struct {
	Kind           agentLogSourceKind
	Label          string
	Container      string
	Ref            string
	Phase          string
	Path           string
	Body           []byte
	TranscriptTree string
	ArchiveMeta    runMeta
	ReservedAt     time.Time
	StartedAt      time.Time
	StateStatus    string
	Tail           int
	Follow         bool
}

type agentLogGroupSource struct {
	Project string
	Sources []agentLogSource
	Tail    int
}

func (s agentLogGroupSource) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "compose project %s (%d containers)", s.Project, len(s.Sources))
	if s.Tail > 0 {
		fmt.Fprintf(&b, " --tail %d", s.Tail)
	}
	return b.String()
}

func (s agentLogSource) String() string {
	switch s.Kind {
	case agentLogSourceDocker:
		return s.dockerString()
	case agentLogSourceFile:
		return s.fileString()
	case agentLogSourceInline:
		return s.inlineString()
	default:
		return ""
	}
}

func (s agentLogSource) dockerString() string {
	var b strings.Builder
	b.WriteString("docker logs ")
	b.WriteString(s.Container)
	if s.Tail > 0 {
		fmt.Fprintf(&b, " --tail %d", s.Tail)
	}
	if s.Follow {
		b.WriteString(" --follow")
	}
	return b.String()
}

func (s agentLogSource) fileString() string {
	var b strings.Builder
	label := strings.TrimSpace(s.Label)
	path := strings.TrimSpace(s.Path)
	switch {
	case label != "" && path != "":
		b.WriteString(label)
		b.WriteString(" ")
		b.WriteString(s.Path)
	case label != "":
		b.WriteString(label)
	default:
		b.WriteString(s.Path)
	}
	if outcome := strings.TrimSpace(s.ArchiveMeta.Outcome); outcome != "" {
		fmt.Fprintf(&b, " (outcome %s)", outcome)
	}
	return appendAgentLogTailFollow(&b, s.Tail, s.Follow)
}

func (s agentLogSource) inlineString() string {
	label := strings.TrimSpace(s.Label)
	if label == "" {
		label = "generated artifact"
	}
	var b strings.Builder
	b.WriteString(label)
	return appendAgentLogTailFollow(&b, s.Tail, s.Follow)
}

func appendAgentLogTailFollow(b *strings.Builder, tail int, follow bool) string {
	if tail > 0 {
		fmt.Fprintf(b, " --tail %d", tail)
	}
	if follow {
		b.WriteString(" --follow")
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// agentLogsCommand builds `ward agent logs <ref>`: the director-surface log read.
func agentLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Read one engineer's run logs through the dispatch broker - director-surface, source-reported, tail/follow aware.",
		ArgsUsage: "[owner/repo#N | #N | container-name]",
		Description: `logs reads one engineer run artifact through the supervised dispatch broker when a
director surface is attached, or host-side otherwise. It resolves the target the same
way ` + "`ward agent stop`" + ` does: issue ref or container name. When a live engineer
container is running it prefers ` + "`docker logs`" + `, and when an exited container has a
completed archive it prefers the drained secret-safe host archive at ~/.ward/agent-logs-redacted/<container>/.
Use --artifact to read console, transcript, meta, friction, the secret-safe dispatch
wrapper, or a typed release sidecar. With no target, it discovers the current Ward director Compose
project and prints the last 100 lines for every container in that group. The chosen
source is printed before the body streams.

  ward agent logs
  ward agent logs coilyco-flight-deck/ward#692
  ward agent logs coilyco-flight-deck/ward#692 --artifact meta
  ward agent logs coilyco-flight-deck/ward#692 --artifact friction
  ward agent logs coilyco-flight-deck/ward#692 --artifact dispatch
  ward agent logs <dispatch-request-id> --artifact release
  ward agent logs engineer-goose-ward-692
  ward agent logs coilyco-flight-deck/ward#692 --tail 200
  ward agent logs coilyco-flight-deck/ward#692 --follow`,
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "tail", Usage: "show the last N lines when reading logs (0 means all lines)"},
			&cli.BoolFlag{Name: "follow", Usage: "follow live docker logs instead of taking a snapshot"},
			&cli.StringFlag{Name: "artifact", Usage: "artifact to read: console, transcript, meta, friction, dispatch, release", Value: string(agentLogArtifactConsole)},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.logs",
				SkipPolicy: true, // reads docker state / archived logs only; no repo tree to gate
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runAgentLogs(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentLogs resolves the target and streams its logs directly or through the
// supervised dispatch broker.
func (r *Runner) runAgentLogs(ctx context.Context, c *cli.Command) error {
	req, err := agentLogsRequestFromCommand(c)
	if err != nil {
		return fmt.Errorf("ward agent logs: %w", err)
	}
	if addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)); addr != "" && os.Getenv("WARD_READONLY") == "1" {
		if err := r.forwardAgentLogsToHostBroker(ctx, addr, req.Target, req.Tail, req.Follow, req.Artifact); err != nil {
			if !errors.Is(err, errDispatchBrokerUnavailable) {
				return err
			}
		} else {
			return nil
		}
	}
	if req.Target == "" {
		return r.runAgentLogsForCurrentComposeGroup(ctx, req.Tail, req.Follow, strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")))
	}
	source, err := r.resolveAgentLogsSource(ctx, req.Target, req.Tail, req.Follow, agentLogsResolveOptions{Artifact: req.Artifact})
	if err != nil {
		return fmt.Errorf("ward agent logs: %w", err)
	}
	writef(r.Runner.Stderr, "ward agent logs: using %s\n", source.String())
	return r.streamAgentLogsSource(ctx, source, r.Runner.Stdout)
}

type agentLogsRequest struct {
	Target   string
	Tail     int
	Follow   bool
	Artifact agentLogArtifact
}

func agentLogsRequestFromCommand(c *cli.Command) (agentLogsRequest, error) {
	req := agentLogsRequest{
		Target: strings.TrimSpace(c.Args().First()),
		Tail:   c.Int("tail"),
		Follow: c.Bool("follow"),
	}
	if req.Tail < 0 {
		return agentLogsRequest{}, fmt.Errorf("--tail must be >= 0")
	}
	artifact, err := parseAgentLogArtifact(c.String("artifact"))
	if err != nil {
		return agentLogsRequest{}, err
	}
	req.Artifact = artifact
	if req.Artifact != agentLogArtifactConsole && req.Target == "" {
		return agentLogsRequest{}, fmt.Errorf("--artifact %s requires an explicit issue ref or container name", req.Artifact)
	}
	if req.Follow && req.Artifact != agentLogArtifactConsole {
		return agentLogsRequest{}, fmt.Errorf("--follow is only supported for --artifact console")
	}
	if req.Target == "" && !c.IsSet("tail") {
		req.Tail = agentLogsDefaultGroupTail
	}
	return req, nil
}

// forwardAgentLogsToHostBroker forwards the read to host ward, then relays the body.
func (r *Runner) forwardAgentLogsToHostBroker(ctx context.Context, addr, target string, tail int, follow bool, artifact agentLogArtifact) error {
	req := dispatchBrokerRequest{
		Action:    dispatchActionLogs,
		Target:    target,
		Artifact:  string(artifact),
		Tail:      tail,
		Follow:    follow,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	source, body, err := sendDispatchBrokerLogsRequest(ctx, addr, req)
	if err != nil {
		return err
	}
	defer func() {
		_ = body.Close()
	}()
	writef(r.Runner.Stderr, "ward agent logs: using %s\n", source)
	if _, err := io.Copy(r.Runner.Stdout, body); err != nil {
		return fmt.Errorf("ward agent logs: relay host output: %w", err)
	}
	return nil
}

func (r *Runner) runAgentLogsForCurrentComposeGroup(ctx context.Context, tail int, follow bool, requester string) error {
	group, err := r.resolveCurrentComposeGroupLogs(ctx, requester, tail, follow)
	if err != nil {
		return fmt.Errorf("ward agent logs: %w", err)
	}
	writef(r.Runner.Stderr, "ward agent logs: using %s\n", group.String())
	return r.streamAgentLogsGroup(ctx, group, r.Runner.Stdout)
}

// resolveAgentLogsSource resolves the target to a live agent container or a
// drained log file, preferring the live Docker path when available.
func (r *Runner) resolveAgentLogsSource(ctx context.Context, target string, tail int, follow bool, opts agentLogsResolveOptions) (agentLogSource, error) {
	if source, matched, err := resolveAgentLogsSourceForRequestID(target, tail, follow, opts); matched || err != nil {
		return source, err
	}
	if ref, err := parseAgentIssueRef(target); err == nil && ref.Owner != "" && ref.Repo != "" {
		return r.resolveAgentLogsSourceForIssue(ctx, ref, tail, follow, opts)
	}
	if validDispatchAgentID(target) {
		names, err := r.containersForPeerID(ctx, target, currentDispatchClusterID(), true)
		if err != nil {
			return agentLogSource{}, fmt.Errorf("resolve peer id %q: %w", target, err)
		}
		if len(names) > 0 {
			name, err := selectSinglePeerTarget("read", target, names)
			if err != nil {
				return agentLogSource{}, err
			}
			return r.resolveAgentLogsSourceForName(ctx, name, tail, follow, opts)
		}
	}
	return r.resolveAgentLogsSourceForName(ctx, target, tail, follow, opts)
}

func resolveAgentLogsSourceForRequestID(target string, tail int, follow bool, opts agentLogsResolveOptions) (agentLogSource, bool, error) {
	if !dispatchRequestIDPattern.MatchString(target) {
		return agentLogSource{}, false, nil
	}
	path, err := dispatchJournalPath(target)
	if err != nil {
		return agentLogSource{}, true, err
	}
	journal, err := readDispatchJournal(path)
	if errors.Is(err, os.ErrNotExist) {
		return agentLogSource{}, false, nil
	}
	if err != nil {
		return agentLogSource{}, true, err
	}
	if opts.artifact() == agentLogArtifactRelease {
		dir, pathErr := validatedDispatchLifecycleArtifactDir(journal.Paths.Dir)
		if pathErr != nil {
			return agentLogSource{}, true, pathErr
		}
		path := filepath.Join(dir, releaseArtifactFile)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return agentLogSource{}, true, fmt.Errorf("dispatch request %s has no release artifact", target)
			}
			return agentLogSource{}, true, err
		}
		return agentLogSource{Kind: agentLogSourceFile, Label: "release artifact path", Path: path, Tail: tail, Follow: follow}, true, nil
	}
	if opts.artifact() != agentLogArtifactConsole && opts.artifact() != agentLogArtifactDispatch {
		return agentLogSource{}, true, fmt.Errorf("request ids support console, dispatch, or release artifacts")
	}
	return agentLogSource{Kind: agentLogSourceFile, Path: journal.Paths.ConsolePath, Tail: tail, Follow: follow}, true, nil
}

// resolveAgentLogsSourceForIssue resolves a carried issue to one live agent container
// when possible, else to a drained archive for that same issue.
func (r *Runner) resolveAgentLogsSourceForIssue(ctx context.Context, ref agentIssueRef, tail int, follow bool, opts agentLogsResolveOptions) (agentLogSource, error) {
	switch opts.artifact() {
	case agentLogArtifactConsole:
		return r.resolveAgentConsoleSourceForIssue(ctx, ref, tail, follow)
	case agentLogArtifactTranscript:
		return archivedTranscriptSourceByIssue(ref, tail, follow)
	case agentLogArtifactMeta:
		return findArchivedAgentArtifactSourceByIssue(ref, tail, follow, agentLogArtifactMeta)
	case agentLogArtifactFriction:
		return r.frictionReportSourceForIssue(ref)
	case agentLogArtifactDispatch:
		return dispatchArtifactSourceForRef(ref, tail, follow)
	case agentLogArtifactRelease:
		return releaseArtifactSourceForRef(ref, tail, follow)
	default:
		return agentLogSource{}, fmt.Errorf("unknown artifact %q", opts.artifact())
	}
}

func (r *Runner) resolveAgentConsoleSourceForIssue(ctx context.Context, ref agentIssueRef, tail int, follow bool) (agentLogSource, error) {
	if names := r.allAgentContainersForIssue(ctx, ref); len(names) > 0 {
		return r.resolveAgentConsoleSourceForContainers(ctx, ref, names, tail, follow)
	}
	if src, err := findArchivedAgentArtifactSourceByIssue(ref, tail, follow, agentLogArtifactConsole); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
		}
		return src, nil
	}
	if src, err := dispatchArtifactSourceForRef(ref, tail, follow); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		return src, nil
	}
	return agentLogSource{}, fmt.Errorf("dispatch broker: no agent log source matches %q", ref.String())
}

func (r *Runner) resolveAgentConsoleSourceForContainers(ctx context.Context, ref agentIssueRef, names []string, tail int, follow bool) (agentLogSource, error) {
	live, terminal := r.partitionAgentLogContainers(ctx, names)
	if len(live) > 0 {
		name, err := selectSingleLogTarget("read", ref.String(), live)
		if err != nil {
			return agentLogSource{}, err
		}
		return r.dockerAgentLogSource(ctx, name, ref, tail, follow), nil
	}
	src, err := findArchivedAgentArtifactSourceByIssue(ref, tail, follow, agentLogArtifactConsole)
	if err != nil || src.Kind != "" {
		return src, err
	}
	if len(terminal) == 0 {
		return agentLogSource{}, nil
	}
	name, err := selectSingleLogTarget("read", ref.String(), terminal)
	if err != nil {
		return agentLogSource{}, err
	}
	return r.dockerAgentLogSource(ctx, name, ref, tail, follow), nil
}

func (r *Runner) resolveAgentLogsSourceForName(ctx context.Context, name string, tail int, follow bool, opts agentLogsResolveOptions) (agentLogSource, error) {
	if strings.TrimSpace(name) == "" {
		return agentLogSource{}, fmt.Errorf("container name is empty")
	}
	if opts.artifact() != agentLogArtifactConsole {
		return r.resolveAgentNamedArtifactSource(name, tail, follow, opts)
	}
	if r.containerPresent(ctx, name) && r.containerLogDockerPreferred(ctx, name) {
		return r.resolveAgentLogsSourceForRunningName(ctx, name, tail, follow)
	}
	if src, err := r.resolveArchivedAgentLogSourceForName(name, tail, follow); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		return src, nil
	}
	if r.containerPresent(ctx, name) {
		return r.resolveAgentLogsSourceForRunningName(ctx, name, tail, follow)
	}
	if src, err := r.resolveDispatchArtifactSourceForName(name, tail, follow); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		return src, nil
	}
	return agentLogSource{}, fmt.Errorf("dispatch broker: no agent log source matches %q", name)
}

func (r *Runner) resolveAgentNamedArtifactSource(name string, tail int, follow bool, opts agentLogsResolveOptions) (agentLogSource, error) {
	switch opts.artifact() {
	case agentLogArtifactConsole:
		return agentLogSource{}, fmt.Errorf("console artifact should resolve through the console path")
	case agentLogArtifactTranscript:
		return archivedTranscriptSourceByName(name, tail, follow)
	case agentLogArtifactMeta:
		return findArchivedAgentArtifactSourceByName(name, tail, follow, agentLogArtifactMeta)
	case agentLogArtifactFriction:
		return r.frictionReportSourceForName(name)
	case agentLogArtifactDispatch:
		return r.resolveDispatchArtifactSourceForName(name, tail, follow)
	case agentLogArtifactRelease:
		return releaseArtifactSourceForName(name, tail, follow)
	default:
		return agentLogSource{}, fmt.Errorf("unknown artifact %q", opts.artifact())
	}
}

func (r *Runner) resolveAgentLogsSourceForRunningName(ctx context.Context, name string, tail int, follow bool) (agentLogSource, error) {
	role, err := r.containerRoleLabel(ctx, name)
	if err != nil {
		return agentLogSource{}, fmt.Errorf("dispatch broker: refusing to read %q: could not read its %s label (%w) - "+
			"fail-closed, only %s and %s containers are readable", name, labelRole, err, roleEngineer, roleDirector)
	}
	switch role {
	case roleEngineer, roleDirector:
		// readable
	default:
		peerID, peerErr := r.containerPeerLabel(ctx, name)
		if peerErr != nil || !validComposedRole(role) || peerID == "" {
			return agentLogSource{}, fmt.Errorf("dispatch broker: refusing to read %q: it is a %q container without a readable peer identity - "+
				"logs target %s, %s, and broker-admitted peers", name, role, roleEngineer, roleDirector)
		}
	}
	return r.dockerAgentLogSource(ctx, name, agentIssueRef{}, tail, follow), nil
}

func (r *Runner) resolveCurrentComposeGroupLogs(ctx context.Context, requester string, tail int, follow bool) (agentLogGroupSource, error) {
	if follow {
		return agentLogGroupSource{}, fmt.Errorf("--follow requires an explicit target")
	}
	project, err := r.currentComposeProject(ctx, requester)
	if err != nil {
		return agentLogGroupSource{}, err
	}
	names, err := r.composeProjectContainerNames(ctx, project)
	if err != nil {
		return agentLogGroupSource{}, err
	}
	if len(names) == 0 {
		return agentLogGroupSource{}, fmt.Errorf("compose project %q has no containers", project)
	}
	sources := make([]agentLogSource, 0, len(names))
	for _, name := range names {
		sources = append(sources, r.dockerAgentLogSource(ctx, name, agentIssueRef{}, tail, false))
	}
	return agentLogGroupSource{Project: project, Sources: sources, Tail: tail}, nil
}

func (r *Runner) dockerAgentLogSource(ctx context.Context, name string, ref agentIssueRef, tail int, follow bool) agentLogSource {
	source := agentLogSource{
		Kind:           agentLogSourceDocker,
		Container:      name,
		TranscriptTree: r.containerTranscriptTree(ctx, name),
		Tail:           tail,
		Follow:         follow,
	}
	if state, ok := r.inspectContainerState(ctx, name); ok {
		source.StateStatus = strings.TrimSpace(state.Status)
		source.StartedAt = parseSummaryTime(strings.TrimSpace(state.StartedAt))
		source.Phase = agentLogPhaseFromDockerState(source.StateStatus)
	}
	if ref.Owner == "" || ref.Repo == "" || ref.Number <= 0 {
		return source
	}
	source.Ref = ref.String()
	if phase, _, ok := dispatchLaunchPhaseForReservation(ref); ok {
		source.Phase = phase
	}
	if res, ok, _ := readAgentReservationMust(ref); ok && res != nil {
		source.ReservedAt = res.At
		if strings.TrimSpace(source.Container) == "" {
			source.Container = strings.TrimSpace(res.Container)
		}
	}
	return source
}

func agentLogPhaseFromDockerState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return ""
	case "running":
		return agentLaunchPhaseRunning
	case "created", "restarting", "paused":
		return agentLaunchPhaseStarting
	default:
		return "container " + strings.ToLower(strings.TrimSpace(status))
	}
}

func (r *Runner) currentComposeProject(ctx context.Context, requester string) (string, error) {
	if project := strings.TrimSpace(os.Getenv(envDispatchBrokerID)); project != "" {
		return project, nil
	}
	for _, name := range []string{requester, strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME"))} {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out, err := r.dockerCapture(ctx, "inspect", "--format", `{{index .Config.Labels "`+composeProjectLabel+`"}}`, name)
		if err != nil {
			continue
		}
		if project := strings.TrimSpace(string(out)); project != "" && project != "<no value>" {
			return project, nil
		}
	}
	return "", fmt.Errorf("no current compose group found from ward/director context")
}

func (r *Runner) composeProjectContainerNames(ctx context.Context, project string) ([]string, error) {
	out, err := r.dockerCapture(ctx, "ps", "-a", "--format", "{{.Names}}",
		"--filter", "label="+composeProjectLabel+"="+project)
	if err != nil {
		return nil, err
	}
	names := parseExitedContainerNames(string(out))
	sort.Strings(names)
	return names, nil
}

func selectSingleLogTarget(action, target string, names []string) (string, error) {
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", fmt.Errorf("dispatch broker: no running agent container matches %q - nothing to %s", target, action)
	default:
		return "", fmt.Errorf("dispatch broker: %q matches %d running agent containers (%s) - refusing to guess; %s one by its container name",
			target, len(names), strings.Join(names, ", "), action)
	}
}

func (r *Runner) resolveArchivedAgentLogSourceForName(name string, tail int, follow bool) (agentLogSource, error) {
	return findArchivedAgentArtifactSourceByName(name, tail, follow, agentLogArtifactConsole)
}

func (r *Runner) partitionAgentLogContainers(ctx context.Context, names []string) (live, terminal []string) {
	for _, name := range names {
		if r.containerLogDockerPreferred(ctx, name) {
			live = append(live, name)
		} else {
			terminal = append(terminal, name)
		}
	}
	return live, terminal
}

func (r *Runner) containerLogDockerPreferred(ctx context.Context, name string) bool {
	state, ok := r.inspectContainerState(ctx, name)
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(state.Status)) {
	case "exited", "dead", "removing":
		return false
	default:
		return true
	}
}

func findArchivedAgentArtifactSourceByIssue(ref agentIssueRef, tail int, follow bool, artifact agentLogArtifact) (agentLogSource, error) {
	canonicalName, legacySafeName, label, err := archivedAgentArtifactFile(artifact)
	if err != nil {
		return agentLogSource{}, err
	}
	if src, err := findArchivedAgentLogSourceByIssue(ref, tail, follow, agentLogsDir(), canonicalName, label); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
		}
		return src, nil
	}
	// Compatibility read for safe archives written before #1582. Both names live
	// in the one canonical safe root. The retired raw root is never consulted.
	if src, err := findArchivedAgentLogSourceByIssue(ref, tail, follow, agentLogsDir(), legacySafeName, label); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
		}
		return src, nil
	}
	return agentLogSource{}, nil
}

func findArchivedAgentArtifactSourceByName(name string, tail int, follow bool, artifact agentLogArtifact) (agentLogSource, error) {
	canonicalName, legacySafeName, label, err := archivedAgentArtifactFile(artifact)
	if err != nil {
		return agentLogSource{}, err
	}
	if src, err := findArchivedAgentLogSourceByName(name, tail, follow, agentLogsDir(), canonicalName, label); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %q", name)
		}
		return src, nil
	}
	if src, err := findArchivedAgentLogSourceByName(name, tail, follow, agentLogsDir(), legacySafeName, label); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %q", name)
		}
		return src, nil
	}
	return agentLogSource{}, nil
}

func archivedTranscriptSourceByIssue(ref agentIssueRef, tail int, follow bool) (agentLogSource, error) {
	src, err := findArchivedAgentArtifactSourceByIssue(ref, tail, follow, agentLogArtifactTranscript)
	if err != nil || src.Kind != "" {
		return src, err
	}
	metaSrc, err := findArchivedAgentArtifactSourceByIssue(ref, 0, false, agentLogArtifactMeta)
	if err != nil || metaSrc.Kind == "" {
		return agentLogSource{}, err
	}
	meta, ok := readRunMeta(metaSrc.Path)
	if !ok || !meta.TranscriptPresent {
		return agentLogSource{}, nil
	}
	return transcriptUnavailableSource(meta, tail), nil
}

func archivedTranscriptSourceByName(name string, tail int, follow bool) (agentLogSource, error) {
	src, err := findArchivedAgentArtifactSourceByName(name, tail, follow, agentLogArtifactTranscript)
	if err != nil || src.Kind != "" {
		return src, err
	}
	metaSrc, err := findArchivedAgentArtifactSourceByName(name, 0, false, agentLogArtifactMeta)
	if err != nil || metaSrc.Kind == "" {
		return agentLogSource{}, err
	}
	meta, ok := readRunMeta(metaSrc.Path)
	if !ok || !meta.TranscriptPresent {
		return agentLogSource{}, nil
	}
	return transcriptUnavailableSource(meta, tail), nil
}

func transcriptUnavailableSource(meta runMeta, tail int) agentLogSource {
	body, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"artifact":       "transcript",
		"status":         "unavailable",
		"container":      meta.Container,
		"repo":           meta.Repo,
		"issue":          meta.Issue,
		"reason":         "transcript_present=true but no transcript artifact is available",
	})
	body = append(body, '\n')
	return agentLogSource{Kind: agentLogSourceInline, Label: "transcript summary", Body: body, Tail: tail}
}

func archivedAgentArtifactFile(artifact agentLogArtifact) (canonicalName, legacySafeName, label string, err error) {
	switch artifact {
	case agentLogArtifactConsole:
		return drainConsoleFile, drainConsoleRedactedFile, "archive path", nil
	case agentLogArtifactTranscript:
		return drainTranscriptFile, drainTranscriptRedactedFile, "transcript path", nil
	case agentLogArtifactMeta:
		return drainMetaFile, drainMetaFile, "meta path", nil
	case agentLogArtifactFriction, agentLogArtifactDispatch, agentLogArtifactRelease:
		return "", "", "", fmt.Errorf("artifact %q is not a drained file artifact", artifact)
	default:
		return "", "", "", fmt.Errorf("artifact %q is not a drained file artifact", artifact)
	}
}

func (r *Runner) resolveDispatchArtifactSourceForName(name string, tail int, follow bool) (agentLogSource, error) {
	role := dispatchArtifactIndexableRole(name)
	if role == "" {
		return agentLogSource{}, nil
	}
	paths, ok, err := latestDispatchArtifactPathsForRole(role)
	if err != nil {
		return agentLogSource{}, err
	}
	if !ok {
		return agentLogSource{}, nil
	}
	if follow {
		return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %q", name)
	}
	meta, _ := readDispatchArtifactMeta(paths.MetaPath)
	label := "dispatch log path"
	return agentLogSource{Kind: agentLogSourceFile, Label: label, Path: paths.ConsolePath, ArchiveMeta: runMeta{Container: meta.RequestID, Repo: meta.Repo, Issue: meta.Issue, Driver: meta.Harness, Branch: meta.Ref, Outcome: meta.Outcome}, Tail: tail, Follow: follow}, nil
}

// streamAgentLogsSource writes the chosen source to w. Live docker sources follow
// when requested, while archive sources are snapshot-only.
func (r *Runner) streamAgentLogsSource(ctx context.Context, source agentLogSource, w io.Writer) error {
	switch source.Kind {
	case agentLogSourceDocker:
		return r.streamDockerAgentLogsSource(ctx, source, w)
	case agentLogSourceFile:
		return r.streamFileAgentLogsSource(source, w)
	case agentLogSourceInline:
		return streamInlineAgentLogsSource(source, w)
	default:
		return fmt.Errorf("ward agent logs: unknown log source kind %q", source.Kind)
	}
}

func (r *Runner) streamAgentLogsGroup(ctx context.Context, group agentLogGroupSource, w io.Writer) error {
	for i, source := range group.Sources {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "===== ward agent logs: %s =====\n", source.Container); err != nil {
			return err
		}
		if err := r.streamAgentLogsSource(ctx, source, w); err != nil {
			return fmt.Errorf("%s: %w", source.Container, err)
		}
	}
	return nil
}

func (r *Runner) streamDockerAgentLogsSource(ctx context.Context, source agentLogSource, w io.Writer) error {
	redactor, err := configuredSecretRedactor(r.inspectContainerEnvAll(ctx, source.Container))
	if err != nil {
		return fmt.Errorf("ward agent logs: build secret-safe redactor: %w", err)
	}
	argv := []string{"logs"}
	if source.Tail > 0 {
		argv = append(argv, "--tail", strconv.Itoa(source.Tail))
	}
	if source.Follow {
		argv = append(argv, "--follow")
	}
	argv = append(argv, source.Container)
	if source.Follow {
		safe := &redactingLineWriter{target: w, redactor: redactor}
		prevOut, prevErr := r.Runner.Stdout, r.Runner.Stderr
		r.Runner.Stdout, r.Runner.Stderr = safe, safe
		defer func() {
			_ = safe.Flush()
			r.Runner.Stdout, r.Runner.Stderr = prevOut, prevErr
		}()
		return r.dockerExec(ctx, argv...)
	}
	out, err := r.dockerCapture(ctx, argv...)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return r.streamEmptyDockerAgentLogsSource(ctx, source, w)
	}
	_, err = w.Write(redactConsoleWith(out, redactor))
	return err
}

func (r *Runner) streamEmptyDockerAgentLogsSource(ctx context.Context, source agentLogSource, w io.Writer) error {
	tree := strings.TrimSpace(source.TranscriptTree)
	if tree == "" {
		r.writeEmptyLiveAgentLogStatus(w, source, "(unknown)", false)
		return nil
	}
	transcript, timedOut := r.liveTranscriptSource(ctx, source.Container, tree)
	if len(transcript) == 0 {
		r.writeEmptyLiveAgentLogStatus(w, source, tree, timedOut)
		return nil
	}
	redactor, err := configuredSecretRedactor(r.inspectContainerEnvAll(ctx, source.Container))
	if err != nil {
		return fmt.Errorf("ward agent logs: build secret-safe redactor: %w", err)
	}
	transcript = redactedTranscriptWith(transcript, redactor)
	if len(transcript) == 0 {
		r.writeEmptyLiveAgentLogStatus(w, source, tree, timedOut)
		return nil
	}
	if source.Tail > 0 {
		transcript = tailBytes(transcript, source.Tail)
	}
	_, _ = fmt.Fprintf(w, "ward agent logs: %s had no readable bytes; using live transcript tree from %s\n", source.String(), tree)
	_, err = w.Write(transcript)
	return err
}

func (r *Runner) streamFileAgentLogsSource(source agentLogSource, w io.Writer) error {
	b, err := os.ReadFile(source.Path) // #nosec G304 -- ward-derived archive path under ~/.ward
	if err != nil {
		return err
	}
	if source.Tail > 0 {
		b = tailBytes(b, source.Tail)
	}
	if len(b) == 0 {
		_, _ = fmt.Fprintf(w, "ward agent logs: %s has no readable bytes\n", source.String())
		return nil
	}
	_, err = w.Write(b)
	return err
}

func streamInlineAgentLogsSource(source agentLogSource, w io.Writer) error {
	b := source.Body
	if source.Tail > 0 {
		b = tailBytes(b, source.Tail)
	}
	if len(b) == 0 {
		_, _ = fmt.Fprintf(w, "ward agent logs: %s has no readable bytes\n", source.String())
		return nil
	}
	_, err := w.Write(b)
	return err
}

func (r *Runner) writeEmptyLiveAgentLogStatus(w io.Writer, source agentLogSource, tree string, transcriptTimedOut bool) {
	_, _ = fmt.Fprintf(w, "ward agent logs: %s had no readable bytes\n", source.String())
	if container := strings.TrimSpace(source.Container); container != "" {
		_, _ = fmt.Fprintf(w, "container: %s\n", container)
	}
	if ref := strings.TrimSpace(source.Ref); ref != "" {
		_, _ = fmt.Fprintf(w, "ref: %s\n", ref)
	}
	if phase := strings.TrimSpace(source.Phase); phase != "" {
		_, _ = fmt.Fprintf(w, "phase: %s\n", phase)
	}
	now := time.Now().UTC()
	switch {
	case !source.ReservedAt.IsZero():
		_, _ = fmt.Fprintf(w, "reservation age: %s (reserved at %s)\n",
			conciseDuration(now.Sub(source.ReservedAt)), source.ReservedAt.UTC().Format(time.RFC3339))
	case !source.StartedAt.IsZero():
		_, _ = fmt.Fprintf(w, "start age: %s (started at %s)\n",
			conciseDuration(now.Sub(source.StartedAt)), source.StartedAt.UTC().Format(time.RFC3339))
	}
	suffix := ""
	if transcriptTimedOut && agentLogsLiveTranscriptTimeout > 0 {
		suffix = fmt.Sprintf("; read timed out after %s", conciseDuration(agentLogsLiveTranscriptTimeout))
	}
	_, _ = fmt.Fprintf(w, "transcript: no readable live transcript exists yet (checked %s%s)\n", tree, suffix)
}

// liveTranscriptSource pulls the current transcript tree from a live engineer.
// The bool reports whether the bounded read timed out.
func (r *Runner) liveTranscriptSource(ctx context.Context, name, transcriptTree string) ([]byte, bool) {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type transcriptReadResult struct {
		out []byte
		err error
	}
	done := make(chan transcriptReadResult, 1)
	go func() {
		rr := *r
		if r.Runner != nil {
			inner := *r.Runner
			inner.Stderr = io.Discard
			rr.Runner = &inner
		}
		out, err := rr.dockerCapture(readCtx, "cp", name+":"+transcriptTree, "-")
		done <- transcriptReadResult{out: out, err: err}
	}()

	if agentLogsLiveTranscriptTimeout <= 0 {
		res := <-done
		if res.err != nil || len(res.out) == 0 {
			return nil, false
		}
		return extractTranscriptFromTar(res.out), false
	}

	timer := time.NewTimer(agentLogsLiveTranscriptTimeout)
	defer timer.Stop()
	select {
	case res := <-done:
		timedOut := errors.Is(readCtx.Err(), context.DeadlineExceeded) || errors.Is(res.err, context.DeadlineExceeded)
		if res.err != nil || len(res.out) == 0 {
			return nil, timedOut
		}
		return extractTranscriptFromTar(res.out), timedOut
	case <-timer.C:
		cancel()
		return nil, true
	case <-ctx.Done():
		cancel()
		return nil, errors.Is(ctx.Err(), context.DeadlineExceeded)
	}
}

// allAgentContainersForIssue lists every live or exited readable agent container that
// matches ref's repo + issue, so the read path can fail loud on ambiguity.
func (r *Runner) allAgentContainersForIssue(ctx context.Context, ref agentIssueRef) []string {
	out, err := r.dockerCapture(ctx, "ps", "-a", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRepo+"="+ref.repoSlug(),
		"--filter", fmt.Sprintf("label=%s=%d", labelIssue, ref.Number))
	if err != nil {
		return nil
	}
	names := parseExitedContainerNames(string(out))
	if len(names) == 0 {
		return nil
	}
	matches := make([]string, 0, len(names))
	for _, name := range names {
		role, err := r.containerRoleLabel(ctx, name)
		if err != nil {
			continue
		}
		switch role {
		case roleEngineer, roleDirector:
			matches = append(matches, name)
		}
	}
	return matches
}

// containerPresent reports whether the named container exists in Docker, regardless
// of whether it is running or exited.
func (r *Runner) containerPresent(ctx context.Context, name string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	out, err := r.dockerCapture(ctx, "ps", "-a",
		"--filter", "name=^"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// findArchivedAgentLogSourceByIssue scans the archive tree for a drained readable agent
// run whose meta.json matches the given issue.
func findArchivedAgentLogSourceByIssue(ref agentIssueRef, tail int, follow bool, baseDir, consoleName, label string) (agentLogSource, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return agentLogSource{}, nil
		}
		return agentLogSource{}, err
	}
	var candidates []string
	for _, entry := range entries {
		dir, ok := archivedAgentLogIssueCandidate(baseDir, consoleName, entry, ref)
		if !ok {
			continue
		}
		candidates = append(candidates, dir)
	}
	switch len(candidates) {
	case 0:
		return agentLogSource{}, nil
	case 1:
		meta, _ := readRunMeta(filepath.Join(candidates[0], drainMetaFile))
		return agentLogSource{Kind: agentLogSourceFile, Label: label, Path: filepath.Join(candidates[0], consoleName), ArchiveMeta: meta, Tail: tail, Follow: follow}, nil
	default:
		sort.Strings(candidates)
		return agentLogSource{}, fmt.Errorf("dispatch broker: %q matches %d drained agent log directories (%s) - refusing to guess; read one by its container name",
			ref.String(), len(candidates), strings.Join(candidates, ", "))
	}
}

func archivedAgentLogIssueCandidate(baseDir, consoleName string, entry os.DirEntry, ref agentIssueRef) (string, bool) {
	if entry == nil || !entry.IsDir() {
		return "", false
	}
	name := entry.Name()
	metaPath := filepath.Join(baseDir, name, drainMetaFile)
	meta, ok := readRunMeta(metaPath)
	if !ok || meta.Repo != ref.repoSlug() || meta.Issue != strconv.Itoa(ref.Number) || !archivedAgentLogNameHasReadableRole(name) {
		return "", false
	}
	if _, statErr := os.Stat(filepath.Join(baseDir, name, consoleName)); statErr != nil {
		return "", false
	}
	return filepath.Join(baseDir, name), true
}

// findArchivedAgentLogSourceByName looks up one exact drained container archive.
func findArchivedAgentLogSourceByName(name string, tail int, follow bool, baseDir, consoleName, label string) (agentLogSource, error) {
	match := func(container string) bool {
		return container == name
	}
	return findArchivedAgentLogSource(name, baseDir, consoleName, label, tail, follow, match)
}

// findArchivedAgentLogSource scans one archive tree and returns the single matching
// source, preferring the raw console.log naming for the source line.
func findArchivedAgentLogSource(target, baseDir, consoleName, label string, tail int, follow bool, match func(string) bool) (agentLogSource, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return agentLogSource{}, nil
		}
		return agentLogSource{}, err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		metaPath := filepath.Join(baseDir, name, drainMetaFile)
		if _, ok := readRunMeta(metaPath); !ok || !match(name) {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(baseDir, name, consoleName)); statErr != nil {
			continue
		}
		candidates = append(candidates, filepath.Join(baseDir, name))
	}
	switch len(candidates) {
	case 0:
		return agentLogSource{}, nil
	case 1:
		meta, _ := readRunMeta(filepath.Join(candidates[0], drainMetaFile))
		return agentLogSource{Kind: agentLogSourceFile, Label: label, Path: filepath.Join(candidates[0], consoleName), ArchiveMeta: meta, Tail: tail, Follow: follow}, nil
	default:
		sort.Strings(candidates)
		return agentLogSource{}, fmt.Errorf("dispatch broker: %q matches %d drained agent log directories (%s) - refusing to guess; read one by its container name",
			target, len(candidates), strings.Join(candidates, ", "))
	}
}

func archivedAgentLogNameHasReadableRole(name string) bool {
	return strings.HasPrefix(name, roleEngineer+"-") || strings.HasPrefix(name, roleDirector+"-")
}

const frictionReportSchemaVersion = 1

type frictionReport struct {
	SchemaVersion     int             `json:"schema_version"`
	Container         string          `json:"container,omitempty"`
	Repo              string          `json:"repo,omitempty"`
	Issue             string          `json:"issue,omitempty"`
	Driver            string          `json:"driver,omitempty"`
	Branch            string          `json:"branch,omitempty"`
	Outcome           string          `json:"outcome,omitempty"`
	NormalizedOutcome string          `json:"normalized_outcome,omitempty"`
	DispatchRequestID string          `json:"dispatch_request_id,omitempty"`
	DispatchOutcome   string          `json:"dispatch_outcome,omitempty"`
	Events            []frictionEvent `json:"events"`
}

func (r *Runner) frictionReportSourceForIssue(ref agentIssueRef) (agentLogSource, error) {
	metaSrc, err := findArchivedAgentArtifactSourceByIssue(ref, 0, false, agentLogArtifactMeta)
	if err != nil {
		return agentLogSource{}, err
	}
	meta, events := runMetaAndFrictionFromSource(metaSrc)
	paths, dispatchMeta, dispatchEvents, _ := dispatchFrictionForRef(ref)
	if meta.Container == "" && dispatchMeta.RequestID == "" {
		return agentLogSource{}, fmt.Errorf("dispatch broker: no agent friction source matches %q", ref.String())
	}
	if meta.Repo == "" {
		meta.Repo = ref.repoSlug()
	}
	if meta.Issue == "" {
		meta.Issue = strconv.Itoa(ref.Number)
	}
	events = append(events, dispatchEvents...)
	report := frictionReportFrom(meta, dispatchMeta, events)
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return agentLogSource{}, err
	}
	body = append(body, '\n')
	label := "friction report"
	if paths.Dir != "" {
		label += " with dispatch path " + paths.Dir
	}
	return agentLogSource{Kind: agentLogSourceInline, Label: label, Body: body}, nil
}

func (r *Runner) frictionReportSourceForName(name string) (agentLogSource, error) {
	metaSrc, err := findArchivedAgentArtifactSourceByName(name, 0, false, agentLogArtifactMeta)
	if err != nil {
		return agentLogSource{}, err
	}
	meta, events := runMetaAndFrictionFromSource(metaSrc)
	if meta.Container == "" {
		return agentLogSource{}, fmt.Errorf("dispatch broker: no agent friction source matches %q", name)
	}
	report := frictionReportFrom(meta, dispatchArtifactMeta{}, events)
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return agentLogSource{}, err
	}
	body = append(body, '\n')
	return agentLogSource{Kind: agentLogSourceInline, Label: "friction report", Body: body}, nil
}

func runMetaAndFrictionFromSource(src agentLogSource) (runMeta, []frictionEvent) {
	if src.Kind == "" || strings.TrimSpace(src.Path) == "" {
		return runMeta{}, nil
	}
	meta, ok := readRunMeta(src.Path)
	if !ok {
		return runMeta{}, nil
	}
	events := append([]frictionEvent(nil), meta.Friction...)
	if len(events) == 0 {
		if console := archivedConsoleBesideMeta(src.Path); console != "" {
			if b, err := os.ReadFile(console); err == nil { // #nosec G304 -- path is derived from a selected ward archive meta path
				events = collectFrictionEvents(meta, string(b))
			}
		}
	}
	return meta, events
}

func archivedConsoleBesideMeta(metaPath string) string {
	dir := filepath.Dir(metaPath)
	canonical := filepath.Join(dir, drainConsoleFile)
	if _, err := os.Stat(canonical); err == nil {
		return canonical
	}
	return filepath.Join(dir, drainConsoleRedactedFile)
}

func dispatchFrictionForRef(ref agentIssueRef) (dispatchArtifactPaths, dispatchArtifactMeta, []frictionEvent, bool) {
	paths, ok, err := latestDispatchArtifactPathsForRef(ref)
	if err != nil || !ok {
		return dispatchArtifactPaths{}, dispatchArtifactMeta{}, nil, false
	}
	metaPath := paths.MetaPath
	if metaPath == "" {
		metaPath = filepath.Join(paths.Dir, dispatchArtifactMetaFile)
	}
	meta, _ := readDispatchArtifactMeta(metaPath)
	consolePath := paths.ConsolePath
	var console string
	if b, err := os.ReadFile(consolePath); err == nil { // #nosec G304 -- path is derived from a selected ward dispatch artifact
		console = string(b)
	}
	return paths, meta, collectDispatchFrictionEvents(meta, console), true
}

func frictionReportFrom(meta runMeta, dispatch dispatchArtifactMeta, events []frictionEvent) frictionReport {
	return frictionReport{
		SchemaVersion:     frictionReportSchemaVersion,
		Container:         meta.Container,
		Repo:              meta.Repo,
		Issue:             meta.Issue,
		Driver:            meta.Driver,
		Branch:            meta.Branch,
		Outcome:           meta.Outcome,
		NormalizedOutcome: strings.TrimSpace(meta.Summary.NormalizedOutcome),
		DispatchRequestID: strings.TrimSpace(dispatch.RequestID),
		DispatchOutcome:   strings.TrimSpace(dispatch.Outcome),
		Events:            append([]frictionEvent{}, events...),
	}
}

// readRunMeta reads the drained meta.json and parses the secret-free record.
func readRunMeta(path string) (runMeta, bool) {
	b, err := os.ReadFile(path) // #nosec G304 -- caller supplies an archive path under ~/.ward
	if err != nil {
		return runMeta{}, false
	}
	var meta runMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return runMeta{}, false
	}
	return meta, true
}

// latestArchivedAgentRunMetaForIssue resolves the newest drained readable-agent run
// for an issue so callers can tell when a reservation has already been superseded.
func latestArchivedAgentRunMetaForIssue(ref agentIssueRef) (runMeta, bool, error) {
	return latestArchivedAgentRunMetaIn(agentLogsDir(), ref)
}

func latestArchivedAgentRunMetaIn(root string, ref agentIssueRef) (runMeta, bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return runMeta{}, false, nil
		}
		return runMeta{}, false, err
	}
	var best runMeta
	var bestMod time.Time
	for _, entry := range entries {
		meta, mod, ok := archivedAgentRunMetaCandidate(root, entry, ref)
		if !ok {
			continue
		}
		if bestMod.IsZero() || mod.After(bestMod) {
			best = meta
			bestMod = mod
		}
	}
	if bestMod.IsZero() {
		return runMeta{}, false, nil
	}
	return best, true, nil
}

func archivedAgentRunMetaCandidate(root string, entry os.DirEntry, ref agentIssueRef) (runMeta, time.Time, bool) {
	if entry == nil || !entry.IsDir() {
		return runMeta{}, time.Time{}, false
	}
	name := entry.Name()
	if !archivedAgentLogNameHasReadableRole(name) {
		return runMeta{}, time.Time{}, false
	}
	metaPath := filepath.Join(root, name, drainMetaFile)
	meta, ok := readRunMeta(metaPath)
	if !ok || meta.Repo != ref.repoSlug() || meta.Issue != strconv.Itoa(ref.Number) {
		return runMeta{}, time.Time{}, false
	}
	info, err := entry.Info()
	if err != nil {
		return runMeta{}, time.Time{}, false
	}
	return meta, info.ModTime(), true
}

// latestDispatchConsolePathForRef resolves the newest broker dispatch artifact
// for an issue ref to its console log path.
func latestDispatchConsolePathForRef(ref agentIssueRef) (string, bool, error) {
	paths, ok, err := latestDispatchArtifactPathsForRef(ref)
	if err != nil || !ok {
		return "", ok, err
	}
	return paths.ConsolePath, true, nil
}

// latestDispatchArtifactPathsForRef resolves the newest broker dispatch artifact
// directory for an issue ref.
func latestDispatchArtifactPathsForRef(ref agentIssueRef) (dispatchArtifactPaths, bool, error) {
	paths, ok, err := latestDispatchArtifactPathsIn(agentLogsDir(), dispatchArtifactConsoleFile, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Ref == ref.String() || (meta.Repo == ref.repoSlug() && meta.Issue == strconv.Itoa(ref.Number))
	})
	if err != nil || ok {
		return paths, ok, err
	}
	return latestDispatchArtifactPathsIn(agentLogsDir(), dispatchArtifactRedactedConsole, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Ref == ref.String() || (meta.Repo == ref.repoSlug() && meta.Issue == strconv.Itoa(ref.Number))
	})
}

func latestDispatchArtifactPathsForRole(role string) (dispatchArtifactPaths, bool, error) {
	role = strings.TrimSpace(role)
	paths, ok, err := latestDispatchArtifactPathsIn(agentLogsDir(), dispatchArtifactConsoleFile, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Role == role || meta.RequesterRole == role
	})
	if err != nil || ok {
		return paths, ok, err
	}
	return latestDispatchArtifactPathsIn(agentLogsDir(), dispatchArtifactRedactedConsole, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Role == role || meta.RequesterRole == role
	})
}

func dispatchArtifactSourceForRef(ref agentIssueRef, tail int, follow bool) (agentLogSource, error) {
	paths, ok, err := latestDispatchArtifactPathsForRef(ref)
	if err != nil {
		return agentLogSource{}, err
	}
	if !ok {
		return agentLogSource{}, nil
	}
	if follow {
		return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
	}
	metaPath := paths.MetaPath
	if metaPath == "" {
		metaPath = filepath.Join(paths.Dir, dispatchArtifactMetaFile)
	}
	meta, _ := readDispatchArtifactMeta(metaPath)
	return agentLogSource{
		Kind:  agentLogSourceFile,
		Label: "dispatch log path",
		Path:  paths.ConsolePath,
		ArchiveMeta: runMeta{
			Container: meta.RequestID,
			Repo:      meta.Repo,
			Issue:     meta.Issue,
			Driver:    meta.Harness,
			Branch:    meta.Ref,
			Outcome:   meta.Outcome,
		},
		Tail:   tail,
		Follow: follow,
	}, nil
}

func latestDispatchArtifactPathsIn(root, consoleName string, pred func(dispatchArtifactMeta, string) bool) (dispatchArtifactPaths, bool, error) {
	best, ok, err := latestDispatchArtifactPathsInDirs(root, consoleName, pred)
	if err != nil || ok {
		return best, ok, err
	}
	return latestDispatchArtifactPathsInLegacy(root, pred)
}

func latestDispatchArtifactPathsInDirs(root, consoleName string, pred func(dispatchArtifactMeta, string) bool) (dispatchArtifactPaths, bool, error) {
	dir := filepath.Join(root, dispatchArtifactsSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return dispatchArtifactPaths{}, false, nil
		}
		return dispatchArtifactPaths{}, false, err
	}
	type candidate struct {
		paths dispatchArtifactPaths
		mod   time.Time
	}
	var best candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		base := filepath.Join(dir, entry.Name())
		meta, ok := readDispatchArtifactMeta(filepath.Join(base, dispatchArtifactMetaFile))
		if !ok || !pred(meta, base) {
			continue
		}
		consolePath := filepath.Join(base, consoleName)
		if _, err := os.Stat(consolePath); err != nil {
			continue
		}
		paths := dispatchArtifactPaths{
			Dir:         base,
			ConsolePath: consolePath,
			MetaPath:    filepath.Join(base, dispatchArtifactMetaFile),
			SummaryPath: filepath.Join(base, dispatchArtifactSummaryFile),
		}
		if best.paths.Dir == "" || info.ModTime().After(best.mod) {
			best = candidate{paths: paths, mod: info.ModTime()}
		}
	}
	if best.paths.Dir == "" {
		return dispatchArtifactPaths{}, false, nil
	}
	return best.paths, true, nil
}

func latestDispatchArtifactPathsInLegacy(root string, pred func(dispatchArtifactMeta, string) bool) (dispatchArtifactPaths, bool, error) {
	dir := filepath.Join(root, dispatchArtifactsSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return dispatchArtifactPaths{}, false, nil
		}
		return dispatchArtifactPaths{}, false, err
	}
	type candidate struct {
		paths dispatchArtifactPaths
		mod   time.Time
	}
	var best candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		meta, ok := readLegacyDispatchArtifactMeta(path)
		if !ok || !pred(meta, path) {
			continue
		}
		paths := dispatchArtifactPaths{Dir: filepath.Dir(path), ConsolePath: path}
		if best.paths.Dir == "" || info.ModTime().After(best.mod) {
			best = candidate{paths: paths, mod: info.ModTime()}
		}
	}
	if best.paths.Dir == "" {
		return dispatchArtifactPaths{}, false, nil
	}
	return best.paths, true, nil
}

// readDispatchArtifactMeta reads the broker dispatch meta.json record.
func readDispatchArtifactMeta(path string) (dispatchArtifactMeta, bool) {
	b, err := os.ReadFile(path) // #nosec G304 -- caller supplies a ward-derived path under ~/.ward
	if err != nil {
		return dispatchArtifactMeta{}, false
	}
	var meta dispatchArtifactMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return dispatchArtifactMeta{}, false
	}
	return meta, true
}

func readLegacyDispatchArtifactMeta(path string) (dispatchArtifactMeta, bool) {
	b, err := os.ReadFile(path) // #nosec G304 -- caller supplies a ward-derived path under ~/.ward
	if err != nil {
		return dispatchArtifactMeta{}, false
	}
	body := string(b)
	requester := dispatchLogRequester(body)
	ref := dispatchLogRef(body)
	role := dispatchLogRole(body)
	meta := dispatchArtifactMeta{
		Requester:     requester,
		RequesterRole: containerNameRole(requester),
		Role:          role,
		Ref:           ref,
	}
	if parsed, err := parseAgentIssueRef(ref); err == nil {
		meta.Repo = parsed.repoSlug()
		meta.Issue = strconv.Itoa(parsed.Number)
	}
	return meta, role != "" || ref != ""
}

func dispatchLogRequester(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " requested `ward agent ") {
			continue
		}
		start := strings.Index(line, "ward dispatch broker: ")
		if start < 0 {
			continue
		}
		rest := line[start+len("ward dispatch broker: "):]
		mid := strings.Index(rest, " requested `ward agent ")
		if mid < 0 {
			continue
		}
		return strings.TrimSpace(rest[:mid])
	}
	return ""
}

func dispatchLogRole(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "requested `ward agent ") {
			continue
		}
		start := strings.Index(line, "requested `ward agent ")
		if start < 0 {
			continue
		}
		rest := line[start+len("requested `ward agent "):]
		end := strings.Index(rest, "`")
		if end < 0 {
			continue
		}
		argv := strings.Fields(rest[:end])
		if len(argv) > 0 {
			return argv[0]
		}
	}
	return ""
}

func dispatchLogRef(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "requested `ward agent ") {
			continue
		}
		start := strings.Index(line, "requested `ward agent ")
		if start < 0 {
			continue
		}
		rest := line[start+len("requested `ward agent "):]
		end := strings.Index(rest, "`")
		if end < 0 {
			continue
		}
		argv := strings.Fields(rest[:end])
		if len(argv) < 2 {
			continue
		}
		if ref, err := parseAgentIssueRef(argv[1]); err == nil {
			return ref.String()
		}
	}
	return ""
}

// tailBytes keeps only the last n lines of data.
func tailBytes(data []byte, n int) []byte {
	if n <= 0 {
		return data
	}
	trimmed := bytes.TrimSuffix(data, []byte{'\n'})
	lines := bytes.Split(trimmed, []byte{'\n'})
	if len(lines) <= n {
		return data
	}
	start := len(lines) - n
	var out bytes.Buffer
	for i := start; i < len(lines); i++ {
		if i > start {
			out.WriteByte('\n')
		}
		out.Write(lines[i])
	}
	if len(data) > 0 && data[len(data)-1] == '\n' {
		out.WriteByte('\n')
	}
	return out.Bytes()
}
