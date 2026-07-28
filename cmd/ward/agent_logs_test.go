package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	source, err := r.resolveAgentLogsSourceForIssue(t.Context(), ref, 0, false)
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

func TestResolveAgentLogsSourceForIssueIncludesDirectorContainer(t *testing.T) {
	r := fakeDirectorIssueLogRunner(t, "director-codex-ward-1033")
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1033}

	source, err := r.resolveAgentLogsSourceForIssue(t.Context(), ref, 7, false)
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

	source, err := fakeDirectorIssueLogRunner(t, "").resolveAgentLogsSourceForIssue(t.Context(), ref, 0, false)
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
