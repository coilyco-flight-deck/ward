package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

func TestRunAgentLogsWithoutTargetStreamsCurrentComposeGroup(t *testing.T) {
	r := fakeComposeGroupLogsDockerRunner(t, "ward-director-ward-codex", []string{
		"ward-director-ward-codex-broker",
		"director-codex-ward-1567",
	})
	var stdout, stderr bytes.Buffer
	r.Runner.Stdout = &stdout
	r.Runner.Stderr = &stderr
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-ward-1567")

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs without target: %v", err)
	}

	if got := stderr.String(); !strings.Contains(got, "compose project ward-director-ward-codex (2 containers) --tail 100") {
		t.Fatalf("stderr = %q, want compose project source with default tail", got)
	}
	for _, want := range []string{
		"===== ward agent logs: director-codex-ward-1567 =====",
		"logs --tail 100 director-codex-ward-1567",
		"===== ward agent logs: ward-director-ward-codex-broker =====",
		"logs --tail 100 ward-director-ward-codex-broker",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunAgentLogsExplicitTargetKeepsExistingTailDefault(t *testing.T) {
	r := fakeAgentLogsDockerRunner(t, "engineer-claude-ward-692\n", "explicit-target\n", nil, "")
	var stdout, stderr bytes.Buffer
	r.Runner.Stdout = &stdout
	r.Runner.Stderr = &stderr

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", "coilyco-flight-deck/ward#692"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs explicit target: %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, "docker logs engineer-claude-ward-692") || strings.Contains(got, "--tail 100") {
		t.Fatalf("stderr = %q, want explicit target without the group tail default", got)
	}
	if got := stdout.String(); got != "explicit-target\n" {
		t.Fatalf("stdout = %q, want explicit docker logs body", got)
	}
}

func TestRunAgentLogsFreshSilentContainerReturnsBoundedStatus(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1594}
	container := "engineer-codex-ward-1594"
	reservedAt := time.Now().UTC().Add(-2 * time.Minute)
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("reservation path: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: container,
		At:        reservedAt,
	}); err != nil {
		t.Fatalf("write reservation: %v", err)
	}
	dispatchDir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dispatchDir, 0o755); err != nil {
		t.Fatalf("mkdir dispatch logs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dispatchDir, "20260728T085230Z-director-codex-ward-1594.log"),
		[]byte("ward dispatch broker: director-codex-ward requested `ward agent engineer coilyco-flight-deck/ward#1594 --harness codex`\n"+
			"ward dispatch broker: request id: abc\n"+
			"launch start: creating engineer container\n"), 0o644); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}

	r := fakeAgentLogsDockerRunner(t, container+"\n", "", nil, ".codex/sessions")
	var stdout, stderr bytes.Buffer
	r.Runner.Stdout = &stdout
	r.Runner.Stderr = &stderr

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", ref.String(), "--tail", "12"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs: %v", err)
	}

	got := stdout.String()
	for _, want := range []string{
		"ward agent logs: docker logs engineer-codex-ward-1594 --tail 12 had no readable bytes",
		"container: engineer-codex-ward-1594",
		"ref: coilyco-flight-deck/ward#1594",
		"phase: container starting",
		"reservation age:",
		"transcript: no readable live transcript exists yet",
		"/home/ubuntu/.ward/.codex/sessions",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout missing %q\n%s", want, got)
		}
	}
}

func TestLiveTranscriptSourceTimesOutPromptly(t *testing.T) {
	orig := agentLogsLiveTranscriptTimeout
	agentLogsLiveTranscriptTimeout = 10 * time.Millisecond
	t.Cleanup(func() { agentLogsLiveTranscriptTimeout = orig })

	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = cp ]; then\n" +
		"  sleep 1\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	r := &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}

	start := time.Now()
	bodyBytes, timedOut := r.liveTranscriptSource(t.Context(), "engineer-codex-ward-1594", "/home/ubuntu/.ward/.codex/sessions")
	if !timedOut {
		t.Fatal("liveTranscriptSource did not report the bounded read timeout")
	}
	if len(bodyBytes) != 0 {
		t.Fatalf("transcript body = %q, want empty on timeout", string(bodyBytes))
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("liveTranscriptSource took %s, want a prompt bounded return", elapsed)
	}
}

func TestResolveAgentLogsSourceFallsBackToDispatchLog(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1033}
	dispatchDir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dispatchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dispatch logs: %v", err)
	}
	logPath := filepath.Join(dispatchDir, "20260710T101500Z-director-box-coilyco-flight-deck-ward-1033.log")
	if err := os.WriteFile(logPath, []byte(
		"ward dispatch broker: director-box requested `ward agent engineer coilyco-flight-deck/ward#1033 --harness codex`\n"+
			"WARD-DISPATCH: failed ❌\n"+
			"Failure: `pre-flight NO-GO`\n"), 0o644); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	source, err := r.resolveAgentLogsSourceForIssue(t.Context(), ref, 0, false, agentLogsResolveOptions{})
	if err != nil {
		t.Fatalf("resolveAgentLogsSourceForIssue: %v", err)
	}
	if source.Kind != agentLogSourceFile {
		t.Fatalf("source kind = %q, want %q", source.Kind, agentLogSourceFile)
	}
	if source.Path != logPath {
		t.Fatalf("source path = %q, want %q", source.Path, logPath)
	}
	body, err := os.ReadFile(source.Path)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("dispatch log fallback returned an empty file")
	}
}

func TestRunAgentLogsPrefersCompletedArchiveForExitedContainer(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1543}
	container := "engineer-codex-ward-1543"
	archiveDir := filepath.Join(agentLogsDir(), container)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: container, Repo: ref.repoSlug(), Issue: "1543", Outcome: outcomeNothing}
	if err := writeJSONAtomic(filepath.Join(archiveDir, drainMetaFile), meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, drainConsoleFile), []byte("drained console\nWARD-RUN-SUMMARY: outcome=pr-green meta=meta.json transcript=none\n"), 0o644); err != nil {
		t.Fatalf("write console: %v", err)
	}

	r := fakeAgentLogsDockerRunnerWithState(t, container+"\n", "docker console\n", "exited")
	var stdout, stderr bytes.Buffer
	r.Runner.Stdout = &stdout
	r.Runner.Stderr = &stderr

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", ref.String()})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "WARD-RUN-SUMMARY") || strings.Contains(got, "docker console") {
		t.Fatalf("stdout = %q, want drained archive with summary instead of docker logs", got)
	}
	if got := stderr.String(); !strings.Contains(got, "archive path") {
		t.Fatalf("stderr = %q, want archive source", got)
	}
}

func TestRunAgentLogsArtifactMetaReturnsDrainedMeta(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1543}
	dir := filepath.Join(agentLogsDir(), "engineer-codex-ward-1543")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: "engineer-codex-ward-1543", Repo: ref.repoSlug(), Issue: "1543", Driver: string(modeCodex), Outcome: outcomeNothing}
	if err := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	var stdout bytes.Buffer
	r.Runner.Stdout = &stdout

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", ref.String(), "--artifact", "meta"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, `"container": "engineer-codex-ward-1543"`) || !strings.Contains(got, `"repo": "coilyco-flight-deck/ward"`) {
		t.Fatalf("meta output = %q", got)
	}
}

func TestRunAgentLogsArtifactTranscriptFallsBackToSafeSummary(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1543}
	dir := filepath.Join(agentLogsRedactedDir(), "engineer-codex-ward-1543")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: "engineer-codex-ward-1543", Repo: ref.repoSlug(), Issue: "1543", Driver: string(modeCodex), TranscriptPresent: true, Outcome: outcomeNothing}
	if err := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	var stdout bytes.Buffer
	r.Runner.Stdout = &stdout

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", ref.String(), "--artifact", "transcript"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs: %v", err)
	}
	for _, want := range []string{`"artifact":"transcript"`, `"status":"unavailable"`, `"container":"engineer-codex-ward-1543"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("transcript fallback = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunAgentLogsArtifactDispatchPrefersRedactedDispatchOverContainer(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1543}
	writeDispatchArtifactFixture(t, ref, dispatchArtifactMeta{
		RequestID: "req-dispatch",
		Role:      roleEngineer,
		Ref:       ref.String(),
		Repo:      ref.repoSlug(),
		Issue:     "1543",
		Outcome:   "launched",
	}, "redacted dispatch body\n", true)

	r := fakeAgentLogsDockerRunner(t, "engineer-codex-ward-1543\n", "engineer console\n", nil, "")
	var stdout bytes.Buffer
	r.Runner.Stdout = &stdout

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", ref.String(), "--artifact", "dispatch"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs: %v", err)
	}
	if got := stdout.String(); got != "redacted dispatch body\n" {
		t.Fatalf("dispatch output = %q, want redacted dispatch artifact", got)
	}
}

func TestRunAgentLogsArtifactFrictionCleanRunHasEmptyEvents(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1600}
	dir := filepath.Join(agentLogsDir(), "engineer-codex-ward-1600")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: "engineer-codex-ward-1600", Repo: ref.repoSlug(), Issue: "1600", Driver: string(modeCodex), Launched: true, TranscriptPresent: true, Outcome: outcomePushedMain}
	if err := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, drainConsoleFile), []byte("ward container reap: landed on main\n"), 0o644); err != nil {
		t.Fatalf("write console: %v", err)
	}

	report := runFrictionArtifactForTest(t, ref)
	if report.SchemaVersion != frictionReportSchemaVersion {
		t.Fatalf("schema version = %d", report.SchemaVersion)
	}
	if len(report.Events) != 0 {
		t.Fatalf("events = %+v, want explicit empty events", report.Events)
	}
}

func TestRunAgentLogsArtifactFrictionClassifiesRecoveredBrokerEvents(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1601}
	dir := filepath.Join(agentLogsDir(), "engineer-codex-ward-1601")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: "engineer-codex-ward-1601", Repo: ref.repoSlug(), Issue: "1601", Driver: string(modeCodex), Launched: true, TranscriptPresent: true, Outcome: outcomeNothing}
	if err := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, drainConsoleFile), []byte("WARD-REAP: nothing to reap (pull-request boundary)\n"), 0o644); err != nil {
		t.Fatalf("write console: %v", err)
	}
	writeDispatchArtifactFixture(t, ref, dispatchArtifactMeta{
		RequestID: "req-recovered",
		Role:      roleEngineer,
		Ref:       ref.String(),
		Repo:      ref.repoSlug(),
		Issue:     "1601",
		Outcome:   "launched",
	}, "fatal: reference is not a tree\nward: generated mount degraded; continuing\nward agent codex: image pull failed (denied); trying the local image\n", false)

	report := runFrictionArtifactForTest(t, ref)
	for _, want := range []string{"stale-config-ref", "generated-mount-degradation", "image-pull-fallback"} {
		if !frictionReportHasCategory(report, want) {
			t.Fatalf("report missing %q: %+v", want, report.Events)
		}
	}
	if frictionReportHasCategory(report, "terminal-launch-failure") {
		t.Fatalf("recovered fallback was classified fatal: %+v", report.Events)
	}
}

func TestRunAgentLogsArtifactFrictionClassifiesFatalDispatch(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1602}
	writeDispatchArtifactFixture(t, ref, dispatchArtifactMeta{
		RequestID:  "req-fatal",
		Role:       roleEngineer,
		Ref:        ref.String(),
		Repo:       ref.repoSlug(),
		Issue:      "1602",
		Outcome:    "failed-before-container",
		ErrorClass: "launch-failure",
		Error:      "docker create failed",
	}, "docker create failed\n", false)

	report := runFrictionArtifactForTest(t, ref)
	if !frictionReportHasCategory(report, "terminal-launch-failure") {
		t.Fatalf("report missing terminal launch failure: %+v", report.Events)
	}
}

func TestResolveAgentLogsSourceForIssueIncludesDirectorContainer(t *testing.T) {
	r := fakeDirectorIssueLogRunner(t, "director-codex-ward-1033")
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1033}

	source, err := r.resolveAgentLogsSourceForIssue(t.Context(), ref, 7, false, agentLogsResolveOptions{})
	if err != nil {
		t.Fatalf("resolveAgentLogsSourceForIssue: %v", err)
	}
	if source.Kind != agentLogSourceDocker {
		t.Fatalf("source kind = %q, want %q", source.Kind, agentLogSourceDocker)
	}
	if source.Container != "director-codex-ward-1033" {
		t.Fatalf("source container = %q, want director container", source.Container)
	}
}

func TestResolveAgentLogsSourceForIssueFallsBackToRedactedDirectorArchive(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1301}
	dir := filepath.Join(agentLogsRedactedDir(), "director-codex-ward-1301")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir redacted archive: %v", err)
	}
	meta := runMeta{Container: "director-codex-ward-1301", Repo: ref.repoSlug(), Issue: "1301", Outcome: outcomePushedMain}
	if err := writeJSONAtomic(filepath.Join(dir, drainMetaFile), meta); err != nil {
		t.Fatalf("write redacted meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, drainConsoleRedactedFile), []byte("director on disk\n"), 0o644); err != nil {
		t.Fatalf("write redacted console: %v", err)
	}

	source, err := fakeDirectorIssueLogRunner(t, "").resolveAgentLogsSourceForIssue(t.Context(), ref, 0, false, agentLogsResolveOptions{})
	if err != nil {
		t.Fatalf("resolveAgentLogsSourceForIssue: %v", err)
	}
	if source.Kind != agentLogSourceFile {
		t.Fatalf("source kind = %q, want %q", source.Kind, agentLogSourceFile)
	}
	if source.Path != filepath.Join(dir, drainConsoleRedactedFile) {
		t.Fatalf("source path = %q, want %q", source.Path, filepath.Join(dir, drainConsoleRedactedFile))
	}
	if !strings.Contains(source.String(), "archive path") {
		t.Fatalf("source string = %q, want archive path label", source.String())
	}
}

func fakeDirectorIssueLogRunner(t *testing.T, visibleName string) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ] && [ \"$2\" = -a ]; then\n" +
		"  if [ -n " + shellQuote(visibleName) + " ]; then\n" +
		"    printf '%s\\n' " + shellQuote(visibleName) + "\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{index .Config.Labels \"ward.role\"}}' ]; then\n" +
		"  printf '%s\\n' director\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .Config.Env}}' ]; then\n" +
		"  printf '%s\\n' '[\"WARD_AGENT_HOME=/home/ubuntu/.ward\"]'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func fakeAgentLogsDockerRunnerWithState(t *testing.T, psOut, logsOut, status string) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ] && [ \"$2\" = -a ]; then\n" +
		"  printf '%s' " + shellQuote(psOut) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{index .Config.Labels \"ward.role\"}}' ]; then\n" +
		"  printf '%s\\n' engineer\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .Config.Env}}' ]; then\n" +
		"  printf '%s' " + shellQuote(`["WARD_AGENT_HOME=/home/ubuntu/.ward"]`) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .State}}' ]; then\n" +
		"  printf '%s' " + shellQuote(`{"Status":"`+status+`"}`) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = logs ]; then\n" +
		"  printf '%s' " + shellQuote(logsOut) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func writeDispatchArtifactFixture(t *testing.T, ref agentIssueRef, meta dispatchArtifactMeta, body string, redactedOnly bool) {
	t.Helper()
	if meta.Ref == "" {
		meta.Ref = ref.String()
	}
	if meta.Repo == "" {
		meta.Repo = ref.repoSlug()
	}
	if meta.Issue == "" {
		meta.Issue = strconv.Itoa(ref.Number)
	}
	if meta.CreatedAt == "" {
		meta.CreatedAt = "2026-07-28T00:00:00Z"
	}
	if meta.RequestID == "" {
		meta.RequestID = "req-fixture"
	}
	roots := []string{agentLogsRedactedDir()}
	if !redactedOnly {
		roots = append(roots, agentLogsDir())
	}
	for _, root := range roots {
		dir := filepath.Join(root, dispatchArtifactsSubdir, meta.RequestID+"-director-codex-ward-"+meta.Issue)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir dispatch fixture: %v", err)
		}
		consoleName := dispatchArtifactConsoleFile
		if strings.Contains(filepath.ToSlash(root), agentLogsRedactedSubdir) {
			consoleName = dispatchArtifactRedactedConsole
		}
		if err := os.WriteFile(filepath.Join(dir, consoleName), []byte(body), 0o644); err != nil {
			t.Fatalf("write dispatch console: %v", err)
		}
		if err := writeJSONAtomic(filepath.Join(dir, dispatchArtifactMetaFile), meta); err != nil {
			t.Fatalf("write dispatch meta: %v", err)
		}
	}
}

func runFrictionArtifactForTest(t *testing.T, ref agentIssueRef) frictionReport {
	t.Helper()
	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	var stdout bytes.Buffer
	r.Runner.Stdout = &stdout
	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", ref.String(), "--artifact", "friction"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs friction: %v", err)
	}
	var report frictionReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal friction report %q: %v", stdout.String(), err)
	}
	if report.Events == nil {
		t.Fatal("friction report events must be an explicit empty array when clean")
	}
	return report
}

func frictionReportHasCategory(report frictionReport, category string) bool {
	for _, ev := range report.Events {
		if ev.Category == category {
			return true
		}
	}
	return false
}

func fakeComposeGroupLogsDockerRunner(t *testing.T, project string, names []string) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{index .Config.Labels \"com.docker.compose.project\"}}' ]; then\n" +
		"  printf '%s\\n' " + shellQuote(project) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .Config.Env}}' ]; then\n" +
		"  printf '%s' " + shellQuote(`[]`) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = ps ] && [ \"$2\" = -a ]; then\n"
	for _, name := range names {
		body += "  printf '%s\\n' " + shellQuote(name) + "\n"
	}
	body += "  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = logs ]; then\n" +
		"  printf '%s\\n' \"$*\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}
