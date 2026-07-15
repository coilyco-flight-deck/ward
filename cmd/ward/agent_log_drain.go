package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
)

// agent_log_drain.go is the host-side drain of agent-run observability (ward#363,
// ward#532): before the keep-10 rm, it pulls a container's console + transcript.

// It runs host-side because the reaper runs INSIDE the container with no docker
// socket. A selectable SINK routes the drain. See docs/agent-observability.md.

// agentLogsSubdir is the per-host archive root under the .ward app dir, sibling
// to audit/ (docs/audit.md) - one directory per drained container run.
const agentLogsSubdir = "agent-logs"

// agentLogsRedactedSubdir is the parallel archive root for the redacted-at-rest view
// (ward#526): a SEPARATE tree the surface binds so raw logs never mount. See docs.
const agentLogsRedactedSubdir = "agent-logs-redacted"

// containerTranscriptDir returns the harness live transcript tree path rooted
// at the agent home, with a legacy fallback for older containers.
func containerTranscriptDir(agentHome string, mode containerMode) string {
	if strings.TrimSpace(agentHome) == "" {
		agentHome = "/home/ubuntu"
	}
	switch mode {
	case modeClaude:
		return filepath.Join(agentHome, ".claude", "projects")
	case modeCodex:
		return filepath.Join(agentHome, ".codex", "sessions")
	case modeOpencode, modeGoose:
		return ""
	default:
		return ""
	}
}

func (r *Runner) containerTranscriptTree(ctx context.Context, name string) string {
	mode := containerModeFromContainerName(name)
	if mode == "" {
		return ""
	}
	return containerTranscriptDir(r.containerAgentHome(ctx, name), mode)
}

func (r *Runner) containerAgentHome(ctx context.Context, name string) string {
	env := r.inspectContainerEnv(ctx, name)
	return strings.TrimSpace(env["WARD_AGENT_HOME"])
}

// drained artifact filenames inside ~/.ward/agent-logs/<slug>/.
const (
	drainConsoleFile    = "console.log"
	drainTranscriptFile = "transcript.jsonl"
	drainMetaFile       = "meta.json"
)

const runSummarySchemaVersion = 1

type runSummary struct {
	SchemaVersion     int                 `json:"schema_version"`
	Workflow          string              `json:"workflow"`
	StartedAt         string              `json:"started_at,omitempty"`
	EndedAt           string              `json:"ended_at,omitempty"`
	DurationSeconds   float64             `json:"duration_seconds,omitempty"`
	RawReapOutcome    string              `json:"raw_reap_outcome"`
	NormalizedOutcome string              `json:"normalized_outcome"`
	OutcomeConfidence string              `json:"outcome_confidence"`
	Artifacts         runSummaryArtifacts `json:"artifacts"`
	Git               runSummaryGit       `json:"git"`
	Reap              runSummaryReap      `json:"reap"`
	Signals           runSummarySignals   `json:"signals"`
}

type runSummaryArtifacts struct {
	ConsoleLog string `json:"console_log,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

type runSummaryGit struct {
	Head         string `json:"head,omitempty"`
	PushedBranch string `json:"pushed_branch,omitempty"`
	PushedMain   string `json:"pushed_main,omitempty"`
	PR           string `json:"pr,omitempty"`
}

type runSummaryReap struct {
	Started                bool   `json:"started"`
	PreservedBranch        string `json:"preserved_branch,omitempty"`
	StandaloneSalvageIssue string `json:"standalone_salvage_issue,omitempty"`
	Reason                 string `json:"reason,omitempty"`
}

type runSummarySignals struct {
	WardOutcomeComment bool `json:"ward_outcome_comment"`
	ChecksGreen        bool `json:"checks_green"`
	TranscriptPresent  bool `json:"transcript_present"`

	LatestOutcome          backlogOutcome `json:"-"`
	PreservedBranch        string         `json:"-"`
	StandaloneSalvageIssue string         `json:"-"`
	ReapReason             string         `json:"-"`
}

// redacted-view artifact filenames inside ~/.ward/agent-logs-redacted/<slug>/ (ward#526).
// meta.json is already secret-free, so it is copied over under drainMetaFile verbatim.
const (
	drainConsoleRedactedFile    = "console.redacted.log"
	drainTranscriptRedactedFile = "transcript.redacted.jsonl"
)

// drainedMarkerSubdir holds one zero-byte sentinel per drained container so a
// second drain is a cheap no-op - the exit-waiter/sweep idempotency boundary (ward#510).
const drainedMarkerSubdir = ".drained"

// drainMarkerPath is the sentinel path for one container's drain. Pure.
func drainMarkerPath(baseDir, name string) string {
	return filepath.Join(baseDir, drainedMarkerSubdir, name)
}

// alreadyDrained reports whether name's drain sentinel exists under baseDir.
func alreadyDrained(baseDir, name string) bool {
	_, err := os.Stat(drainMarkerPath(baseDir, name))
	return err == nil
}

// markDrained writes name's drain sentinel (best-effort; a write failure only
// costs a redundant re-drain later, never correctness).
func markDrained(baseDir, name string) {
	p := drainMarkerPath(baseDir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, nil, 0o644)
}

// clearDrainMarker drops name's sentinel on removal, so a reused deterministic
// name drains fresh rather than being skipped by the dead run's marker (ward#510).
func clearDrainMarker(baseDir, name string) {
	_ = os.Remove(drainMarkerPath(baseDir, name))
}

// sinkMode labels the requested sink. The release surface always writes the local
// disk archive.
type sinkMode string

const (
	sinkDisk sinkMode = "disk"
)

// defaultSinkMode keeps the local archive by default.
const defaultSinkMode = sinkDisk

// resolveSinkMode returns the fixed sink. The drain itself always writes the
// local disk archive.
func resolveSinkMode() sinkMode {
	return defaultSinkMode
}

// metaEnvAllow is the strict allowlist of container env keys copied into meta.json.
// Config.Env also carries --env-file secrets, so only these known-safe dims ride.
var metaEnvAllow = []string{
	"WARD_TARGET_REPO",
	"WARD_TARGET_OWNER",
	"WARD_TARGET_NAME",
	"WARD_TARGET_ISSUE",
	"WARD_CONTAINER_UP",
	"WARD_FORGE",
	"WARD_RUN_ID",
	"WARD_HARNESS",
	"WARD_ROLE",
	"WARD_MODE",
	"WARD_BRANCH",
	"WARD_ISSUE_REF",
	"WARD_WORKFLOW",
	"WARD_CONTEXT_LEVEL",
	"WARD_VERSION",
	"WARD_THREAD_ID",
	"WARD_AGENT_HOME",
	"WARD_AGENT_LAUNCHED",
}

// run outcome strings recorded in meta.json, inferred from the reaper's console
// markers (the reaper logs these on every teardown path; container_reap.go).
const (
	outcomePushedMain = "pushed-to-main"
	outcomeSalvage    = "ward-salvage"
	outcomeNothing    = "nothing-to-reap"
	outcomeUnknown    = "unknown"
)

// reapOutcomeValues is the complete meta.json `outcome` enum, in classifyReapOutcome
// order; the drift-test source of truth for docs/agent-dispatch-contract.md (ward#485).
var reapOutcomeValues = []string{
	outcomePushedMain, // clean integration + push to main
	outcomeSalvage,    // conflict / scan finding / rejected or auth-failed push
	outcomeNothing,    // the tree was already clean or the workflow boundary was reached
	outcomeUnknown,    // no reaper marker matched (crash, external stop, abort)
}

// agentLogsDir resolves the host archive root: the .ward app dir under $HOME,
// falling back to $TMPDIR when $HOME is unset (mirrors config.CacheDir).
func agentLogsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, config.AppDir(), agentLogsSubdir)
}

// agentLogsDisplayDir renders the host archive root in the container log stream
// using the stable tilde path the operator can apply on the host.
func agentLogsDisplayDir(name string) string {
	return filepath.Join("~", config.AppDir(), agentLogsSubdir, name)
}

// agentLogsRedactedDir resolves the parallel redacted archive root (ward#526),
// resolved the same way as agentLogsDir so the two trees sit side by side.
func agentLogsRedactedDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, config.AppDir(), agentLogsRedactedSubdir)
}

// sweepAction is one ordered host-side teardown step: drain a single container, or
// remove the whole set. Every drain must precede the rm (ward#363).
type sweepAction struct {
	// Op is "drain" or "remove".
	Op string
	// Container is the single container to drain (Op == "drain").
	Container string
	// Dir is the per-run archive directory for a drain (baseDir/<container>).
	Dir string
	// Names is the full stale set to remove (Op == "remove").
	Names []string
}

const (
	sweepDrain  = "drain"
	sweepRemove = "remove"
)

// sweepActions is the pure plan: drain EVERY exited container into baseDir/<name>,
// THEN remove the stale (past keep-10) subset (ward#363 ordering, ward#510 full-set).
func sweepActions(exited, stale []string, baseDir string) []sweepAction {
	if len(exited) == 0 && len(stale) == 0 {
		return nil
	}
	actions := make([]sweepAction, 0, len(exited)+1)
	for _, name := range exited {
		actions = append(actions, sweepAction{
			Op:        sweepDrain,
			Container: name,
			Dir:       filepath.Join(baseDir, name),
		})
	}
	if len(stale) > 0 {
		actions = append(actions, sweepAction{Op: sweepRemove, Names: stale})
	}
	return actions
}

// drainStaleContainers runs the sweep plan: drain every exited container
// idempotently, then `docker rm` the stale subset and clear its markers (ward#510).
func (r *Runner) drainStaleContainers(ctx context.Context, exited, stale []string) error {
	baseDir := agentLogsDir()
	for _, a := range sweepActions(exited, stale, baseDir) {
		switch a.Op {
		case sweepDrain:
			r.drainAgentRunIdempotent(ctx, a.Container, baseDir)
		case sweepRemove:
			fmt.Fprintf(os.Stderr, "ward container: removing containers %v\n", a.Names)
			rmErr := r.dockerExec(ctx, dockerRmArgv(a.Names)...)
			for _, name := range a.Names {
				clearDrainMarker(baseDir, name)
			}
			return rmErr
		}
	}
	return nil
}

// drainAgentRunIdempotent drains name once: a drain sentinel makes a repeat a cheap
// no-op, so the exit waiter and the later keep-10 sweep never double-pull (ward#510).
func (r *Runner) drainAgentRunIdempotent(ctx context.Context, name, baseDir string) {
	if alreadyDrained(baseDir, name) {
		fmt.Fprintf(os.Stderr, "ward container: drain of %s skipped (already drained)\n", name)
		return
	}
	r.drainAgentRun(ctx, name, filepath.Join(baseDir, name))
	markDrained(baseDir, name)
}

// drainAgentRun pulls one exited container's console + transcript + meta into
// memory and routes them to the resolved sink; disk is written only if asked.
func (r *Runner) drainAgentRun(ctx context.Context, name, dir string) {
	mode := resolveSinkMode()
	fmt.Fprintf(os.Stderr, "ward container: starting drain of container %s (sink %s)\n", name, mode)

	// Console: docker logs carries both the agent's stdout stream and the reaper's
	// stderr markers; capture combined so the outcome is inferable from one stream.
	console := r.dockerLogsCombined(ctx, name)

	// Transcript: docker cp streams the projects tree as a tar to stdout; pull the
	// jsonl out and concatenate. Held in memory - hits disk only if the sink asks.
	transcript := r.drainTranscript(ctx, name)

	// Meta: safe dims from the inspected env allowlist + the inferred outcome.
	meta := r.buildRunMeta(ctx, name, string(console), transcript)

	r.writeDiskArtifacts(name, dir, console, transcript, meta)
	// The redacted view rides the same disk gate. When the raw archive lands, its
	// scrubbed sibling lands too, for the director surface mount (ward#526).
	r.writeRedactedArtifacts(name, console, transcript, meta)
	if containerModeFromContainerName(name) == modeClaude {
		r.writeClaudeToolFailureRecords(name, meta, transcript)
	}
	fmt.Fprintf(os.Stderr, "ward container: drained %s (sink %s, outcome %s)\n", name, mode, meta.Outcome)
}

// writeDiskArtifacts persists console.log + transcript.jsonl + meta.json under
// dir - today's artifacts, now gated behind the disk / both modes. Best-effort.
func (r *Runner) writeDiskArtifacts(name, dir string, console, transcript []byte, meta runMeta) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not create %s (%v); skipping disk sink\n", name, dir, err)
		return
	}
	console = appendRunSummaryFooter(console, meta.Summary)
	if werr := writeBytesAtomic(filepath.Join(dir, drainConsoleFile), console); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write console.log: %v\n", name, werr)
	}
	if len(transcript) > 0 {
		if werr := writeBytesAtomic(filepath.Join(dir, drainTranscriptFile), transcript); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write transcript.jsonl: %v\n", name, werr)
		}
	}
	if werr := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write meta.json: %v\n", name, werr)
	}
	fmt.Fprintf(os.Stderr, "ward container: wrote disk artifacts for %s -> %s\n", name, dir)
}

// writeRedactedArtifacts persists the redacted-at-rest view (ward#526) under the
// agent-logs-redacted tree; the same scrubbers feed the redacted transcript view.
func (r *Runner) writeRedactedArtifacts(name string, console, transcript []byte, meta runMeta) {
	dir := filepath.Join(agentLogsRedactedDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not create %s (%v); skipping redacted view\n", name, dir, err)
		return
	}
	console = appendRunSummaryFooter(console, meta.Summary)
	if werr := writeBytesAtomic(filepath.Join(dir, drainConsoleRedactedFile), redactConsole(console)); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write console.redacted.log: %v\n", name, werr)
	}
	if red := redactedTranscript(transcript); len(red) > 0 {
		if werr := writeBytesAtomic(filepath.Join(dir, drainTranscriptRedactedFile), red); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write transcript.redacted.jsonl: %v\n", name, werr)
		}
	}
	if werr := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write redacted meta.json: %v\n", name, werr)
	}
	fmt.Fprintf(os.Stderr, "ward container: wrote redacted view for %s -> %s\n", name, dir)
}

// dockerLogsCombined captures `docker logs <name>` with stdout+stderr merged into
// one buffer so the reaper's stderr markers survive alongside the agent stream.
func (r *Runner) dockerLogsCombined(ctx context.Context, name string) []byte {
	var buf bytes.Buffer
	prevOut, prevErr := r.Runner.Stdout, r.Runner.Stderr
	r.Runner.Stdout, r.Runner.Stderr = &buf, &buf
	_ = r.dockerExec(ctx, "logs", name)
	r.Runner.Stdout, r.Runner.Stderr = prevOut, prevErr
	return buf.Bytes()
}

// drainTranscript `docker cp`s the transcript tree out as a tar and returns the
// concatenated jsonl. An absent tree (goose/opencode/unknown) returns nil.
func (r *Runner) drainTranscript(ctx context.Context, name string) []byte {
	tree := r.containerTranscriptTree(ctx, name)
	if strings.TrimSpace(tree) == "" {
		return nil
	}
	// `docker cp <c>:<path> -` writes a tar of <path> to stdout. The trailing
	// stderr ("no such file") is discarded; an empty/garbage tar yields nil.
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.dockerCapture(ctx, "cp", name+":"+tree, "-")
	r.Runner.Stderr = prevErr
	if err != nil || len(out) == 0 {
		return nil
	}
	return extractTranscriptFromTar(out)
}

func containerModeFromContainerName(name string) containerMode {
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 2 {
		return ""
	}
	mode := containerMode(parts[1])
	switch mode {
	case modeClaude, modeCodex, modeOpencode, modeGoose:
		return mode
	default:
		return ""
	}
}

// extractTranscriptFromTar concatenates the *.jsonl members of a tar stream in
// archive order. Pure, so the tar walk is unit-testable without docker.
func extractTranscriptFromTar(tarBytes []byte) []byte {
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	var out bytes.Buffer
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".jsonl") {
			continue
		}
		if _, cerr := io.Copy(&out, tr); cerr != nil { // #nosec G110 -- bounded by the tar member size
			break
		}
		if out.Len() > 0 && out.Bytes()[out.Len()-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// runMeta is the small, secret-free record drained alongside console + transcript:
// who the run was (env allowlist), how the reaper resolved it, and friction facts.
type runMeta struct {
	Container         string          `json:"container"`
	Repo              string          `json:"repo,omitempty"`
	Issue             string          `json:"issue,omitempty"`
	Driver            string          `json:"driver,omitempty"`
	Branch            string          `json:"branch,omitempty"`
	Launched          bool            `json:"launched,omitempty"`
	TranscriptPresent bool            `json:"transcript_present,omitempty"`
	OOMKilled         bool            `json:"OOMKilled,omitempty"`
	Outcome           string          `json:"outcome"`
	Summary           runSummary      `json:"summary"`
	Friction          []frictionEvent `json:"friction,omitempty"`
}

// buildRunMeta assembles the meta record from the container's inspected env
// (allowlisted), the inspected Docker state, and the reaper's console markers.
func (r *Runner) buildRunMeta(ctx context.Context, name, console string, transcript []byte) runMeta {
	env := r.inspectContainerEnv(ctx, name)
	state, ok := r.inspectContainerState(ctx, name)
	meta := runMeta{
		Container:         name,
		Repo:              env["WARD_TARGET_REPO"],
		Issue:             env["WARD_TARGET_ISSUE"],
		Driver:            env["WARD_MODE"],
		Branch:            env["WARD_BRANCH"],
		Launched:          env["WARD_AGENT_LAUNCHED"] == "1",
		TranscriptPresent: len(bytes.TrimSpace(transcript)) > 0,
		OOMKilled:         ok && state.OOMKilled,
		Outcome:           classifyReapOutcome(console),
	}
	meta.Summary = r.buildRunSummary(ctx, name, env, state, meta, console, transcript)
	meta.Friction = collectFrictionEvents(meta, console)
	return meta
}

// inspectContainerEnv reads the container's Config.Env and returns ONLY the
// allowlisted WARD_* dims (never the --env-file secrets that also live there).
func (r *Runner) inspectContainerEnv(ctx context.Context, name string) map[string]string {
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.dockerCapture(ctx, "inspect", "--format", "{{json .Config.Env}}", name)
	r.Runner.Stderr = prevErr
	if err != nil {
		return map[string]string{}
	}
	var env []string
	if jerr := json.Unmarshal(bytes.TrimSpace(out), &env); jerr != nil {
		return map[string]string{}
	}
	return pickMetaEnv(env, metaEnvAllow)
}

// dockerContainerState is the inspect-time Docker state subset used for the OOM
// breadcrumb. The JSON tags mirror `docker inspect --format {{json .State}}`.
type dockerContainerState struct {
	OOMKilled  bool   `json:"OOMKilled"`
	StartedAt  string `json:"StartedAt"`
	FinishedAt string `json:"FinishedAt"`
}

// inspectContainerState reads the container's inspected Docker state and returns
// only the OOM breadcrumb. A read or parse failure is best-effort false.
func (r *Runner) inspectContainerState(ctx context.Context, name string) (dockerContainerState, bool) {
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.dockerCapture(ctx, "inspect", "--format", "{{json .State}}", name)
	r.Runner.Stderr = prevErr
	if err != nil {
		return dockerContainerState{}, false
	}
	var state dockerContainerState
	if jerr := json.Unmarshal(bytes.TrimSpace(out), &state); jerr != nil {
		return dockerContainerState{}, false
	}
	return state, true
}

func (r *Runner) buildRunSummary(ctx context.Context, name string, env map[string]string, state dockerContainerState, meta runMeta, console string, transcript []byte) runSummary {
	startedAt := parseSummaryTime(firstNonEmpty(strings.TrimSpace(state.StartedAt), strings.TrimSpace(env["WARD_CONTAINER_UP"])))
	endedAt := parseSummaryTime(strings.TrimSpace(state.FinishedAt))
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	if startedAt.IsZero() {
		startedAt = endedAt
	}
	signals := r.loadRunSummarySignals(ctx, env, startedAt, console, transcript)
	signals.TranscriptPresent = meta.TranscriptPresent
	signals.PreservedBranch = parsePreservedBranch(console)
	signals.StandaloneSalvageIssue = parseStandaloneSalvageIssue(env, console)
	signals.ReapReason = parseReapReason(meta, console, signals)
	normalized, confidence := normalizeRunSummaryOutcome(meta, env, signals)
	summary := runSummary{
		SchemaVersion:     runSummarySchemaVersion,
		Workflow:          string(workflowMode(strings.TrimSpace(env["WARD_WORKFLOW"])).orDefault()),
		StartedAt:         formatSummaryTime(startedAt),
		EndedAt:           formatSummaryTime(endedAt),
		DurationSeconds:   endedAt.Sub(startedAt).Seconds(),
		RawReapOutcome:    meta.Outcome,
		NormalizedOutcome: normalized,
		OutcomeConfidence: confidence,
		Artifacts: runSummaryArtifacts{
			ConsoleLog: filepath.Join(agentLogsDir(), name, drainConsoleFile),
		},
		Git: runSummaryGit{
			Head:         strings.TrimSpace(meta.Branch),
			PushedBranch: signals.PreservedBranch,
			PR:           strings.TrimSpace(signals.LatestOutcome.PRURL),
		},
		Reap: runSummaryReap{
			Started:                strings.Contains(console, "WARD-REAP: start") || strings.Contains(console, "ward container reap: "),
			PreservedBranch:        signals.PreservedBranch,
			StandaloneSalvageIssue: signals.StandaloneSalvageIssue,
			Reason:                 signals.ReapReason,
		},
		Signals: runSummarySignals{
			WardOutcomeComment: signals.WardOutcomeComment,
			ChecksGreen:        signals.ChecksGreen,
			TranscriptPresent:  meta.TranscriptPresent,
		},
	}
	if meta.TranscriptPresent {
		summary.Artifacts.Transcript = filepath.Join(agentLogsDir(), name, drainTranscriptFile)
	}
	if summary.Git.PushedBranch == "" && summary.NormalizedOutcome != "landed-main" {
		summary.Git.PushedBranch = strings.TrimSpace(meta.Branch)
	}
	if summary.Git.PushedMain == "" && summary.NormalizedOutcome == "landed-main" {
		summary.Git.PushedMain = "main"
	}
	if summary.Reap.Reason == "" {
		summary.Reap.Reason = summary.RawReapOutcome
	}
	return summary
}

func (r *Runner) loadRunSummarySignals(ctx context.Context, env map[string]string, afterAt time.Time, console string, transcript []byte) runSummarySignals {
	signals := runSummarySignals{
		TranscriptPresent: len(bytes.TrimSpace(transcript)) > 0,
	}
	if strings.Contains(strings.ToLower(console), "required ci checks are green") {
		signals.ChecksGreen = true
	}
	issueNum, err := strconv.Atoi(strings.TrimSpace(env["WARD_TARGET_ISSUE"]))
	if err != nil || issueNum <= 0 {
		return signals
	}
	owner := strings.TrimSpace(env["WARD_TARGET_OWNER"])
	repo := strings.TrimSpace(env["WARD_TARGET_NAME"])
	if owner == "" || repo == "" {
		return signals
	}
	mode := containerMode(strings.TrimSpace(env["WARD_MODE"]))
	tracker := trackerFromForge(parseForge(env["WARD_FORGE"]))
	cl, cerr := r.hostTrackerClient(ctx, tracker, mode)
	if cerr != nil {
		return signals
	}
	comments, cerr := cl.ListIssueComments(ctx, owner, repo, issueNum)
	if cerr != nil {
		return signals
	}
	if comment, ok := latestBacklogOutcomeCommentAfter(comments, afterAt); ok {
		outcome, found := backlogOutcomeOfComment(comment.Body)
		if !found {
			return signals
		}
		signals.WardOutcomeComment = true
		signals.LatestOutcome = outcome
		switch strings.ToLower(strings.TrimSpace(outcome.Status)) {
		case "submitted", "merge-ready", "done":
			signals.ChecksGreen = true
		}
	}
	return signals
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeBytesAtomic(path, data)
}

func writeBytesAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func appendRunSummaryFooter(console []byte, summary runSummary) []byte {
	if strings.TrimSpace(summary.NormalizedOutcome) == "" {
		return console
	}
	transcriptPath := summary.Artifacts.Transcript
	if strings.TrimSpace(transcriptPath) == "" {
		transcriptPath = "none"
	}
	footer := fmt.Sprintf("WARD-RUN-SUMMARY: outcome=%s meta=meta.json transcript=%s\n", summary.NormalizedOutcome, transcriptPath)
	if len(console) == 0 {
		return []byte(footer)
	}
	out := append([]byte(strings.TrimRight(string(console), "\n")), '\n')
	out = append(out, []byte(footer)...)
	return out
}

func parseSummaryTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func formatSummaryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

//nolint:gocyclo,cyclop
func normalizeRunSummaryOutcome(meta runMeta, env map[string]string, signals runSummarySignals) (string, string) {
	workflow := workflowMode(strings.TrimSpace(env["WARD_WORKFLOW"])).orDefault()
	switch {
	case !meta.Launched && !meta.TranscriptPresent:
		return "prelaunch-failure", "high"
	case strings.EqualFold(meta.Outcome, outcomePushedMain):
		return "landed-main", "high"
	case strings.EqualFold(meta.Outcome, outcomeSalvage):
		if strings.TrimSpace(signals.StandaloneSalvageIssue) != "" {
			return "standalone-salvage-branch", "high"
		}
		return "preserved-salvage-branch", "high"
	case signals.WardOutcomeComment && signals.ChecksGreen && (workflow == workflowPullRequest || workflow == workflowPullRequestAndMerge || workflow == workflowRemoteBranchOnly):
		return "pr-green", "high"
	case strings.EqualFold(strings.TrimSpace(signals.LatestOutcome.Status), "blocked"):
		return "blocked", "high"
	case strings.EqualFold(strings.TrimSpace(signals.LatestOutcome.Status), "failed"):
		return "failed", "high"
	case meta.Outcome == outcomeNothing && signals.ChecksGreen:
		return "pr-green", "medium"
	case meta.Outcome == outcomeNothing && strings.Contains(strings.ToLower(string(workflow)), "pull-request"):
		return "pr-green", "medium"
	case meta.Outcome == outcomeUnknown && !meta.TranscriptPresent:
		return "prelaunch-failure", "medium"
	default:
		return "unknown", "low"
	}
}

var (
	preservedBranchRE        = regexp.MustCompile(`(?i)ward container reap: preserved(?: un-landed granted-repo work)? on ([^ ]+) \((.*)\)$`)
	standaloneSalvageIssueRE = regexp.MustCompile(`(?i)ward container reap: filed standalone salvage issue #([0-9]+)$`)
	nothingToReapRE          = regexp.MustCompile(`(?i)WARD-REAP: nothing to reap \((.*)\)$`)
)

func parsePreservedBranch(console string) string {
	for _, line := range strings.Split(console, "\n") {
		line = strings.TrimSpace(line)
		if m := preservedBranchRE.FindStringSubmatch(line); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func parseStandaloneSalvageIssue(env map[string]string, console string) string {
	owner := strings.TrimSpace(env["WARD_TARGET_OWNER"])
	repo := strings.TrimSpace(env["WARD_TARGET_NAME"])
	for _, line := range strings.Split(console, "\n") {
		line = strings.TrimSpace(line)
		if m := standaloneSalvageIssueRE.FindStringSubmatch(line); m != nil {
			if owner != "" && repo != "" {
				return fmt.Sprintf("%s/%s#%s", owner, repo, m[1])
			}
			return "#" + m[1]
		}
	}
	return ""
}

//nolint:gocyclo,cyclop
func parseReapReason(meta runMeta, console string, signals runSummarySignals) string {
	if trimmed := strings.TrimSpace(signals.ReapReason); trimmed != "" {
		return trimmed
	}
	for _, line := range strings.Split(console, "\n") {
		line = strings.TrimSpace(line)
		if m := nothingToReapRE.FindStringSubmatch(line); m != nil {
			return strings.TrimSpace(m[1])
		}
		switch {
		case strings.Contains(strings.ToLower(line), "landed on main"):
			return "landed on main"
		case strings.Contains(strings.ToLower(line), "preserved work on"):
			if m := preservedBranchRE.FindStringSubmatch(line); len(m) > 2 {
				return strings.TrimSpace(m[2])
			}
		case strings.Contains(strings.ToLower(line), "preserved un-landed granted-repo work on"):
			if m := preservedBranchRE.FindStringSubmatch(line); len(m) > 2 {
				return strings.TrimSpace(m[2])
			}
		case strings.Contains(strings.ToLower(line), "filed standalone salvage issue"):
			return line
		}
	}
	if !meta.Launched && !meta.TranscriptPresent {
		return "prelaunch failure"
	}
	if trimmed := strings.TrimSpace(signals.LatestOutcome.Text); trimmed != "" {
		return trimmed
	}
	return ""
}

// pickMetaEnv selects only allowlisted keys from a docker `KEY=VALUE` env slice.
// The allowlist is the security boundary: co-resident secrets never match.
func pickMetaEnv(env, allow []string) map[string]string {
	want := make(map[string]bool, len(allow))
	for _, k := range allow {
		want[k] = true
	}
	out := make(map[string]string, len(allow))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && want[k] {
			out[k] = v
		}
	}
	return out
}

// classifyReapOutcome maps the reaper's console markers to a run outcome; the
// markers are mutually exclusive (container_reap.go), so first-match-wins.
func classifyReapOutcome(console string) string {
	switch {
	case strings.Contains(console, "landed on main"):
		return outcomePushedMain
	case strings.Contains(console, salvageBranchPrefix),
		strings.Contains(console, "preserved work on"),
		strings.Contains(console, "preserved un-landed"):
		return outcomeSalvage
	case strings.Contains(console, "nothing to reap"):
		return outcomeNothing
	default:
		return outcomeUnknown
	}
}
