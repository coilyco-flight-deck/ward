package main

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/shell"
)

// TestSweepActionsDrainPrecedesRemove is the load-bearing ordering assertion: every
// drain precedes the `docker rm` (ward#363), and the FULL exited set drains (ward#510).
func TestSweepActionsDrainPrecedesRemove(t *testing.T) {
	// Five exited, the oldest two past keep: every exited run is drained, only the
	// two stale ones are removed.
	exited := []string{"ward-a", "ward-b", "ward-c", "ward-d", "ward-e"}
	stale := []string{"ward-d", "ward-e"}
	actions := sweepActions(exited, stale, "/base")

	removeIdx := -1
	var drained []string
	for i, a := range actions {
		switch a.Op {
		case sweepRemove:
			if removeIdx != -1 {
				t.Fatalf("expected exactly one remove action, got a second at %d", i)
			}
			removeIdx = i
			if strings.Join(a.Names, ",") != strings.Join(stale, ",") {
				t.Errorf("remove action names = %v, want the stale subset %v", a.Names, stale)
			}
		case sweepDrain:
			drained = append(drained, a.Container)
			if removeIdx != -1 {
				t.Errorf("drain of %s at index %d follows the remove at %d (must precede it)", a.Container, i, removeIdx)
			}
			wantDir := filepath.Join("/base", a.Container)
			if a.Dir != wantDir {
				t.Errorf("drain dir = %q, want %q", a.Dir, wantDir)
			}
		default:
			t.Errorf("unexpected op %q", a.Op)
		}
	}
	if removeIdx == -1 {
		t.Fatal("no remove action emitted")
	}
	if strings.Join(drained, ",") != strings.Join(exited, ",") {
		t.Errorf("drained %v, want every EXITED container %v (ward#510)", drained, exited)
	}
}

// TestSweepActionsDrainsWithoutEviction is the ward#510 core: exited runs still
// inside the keep window (nothing stale) are drained, and no remove is planned.
func TestSweepActionsDrainsWithoutEviction(t *testing.T) {
	exited := []string{"ward-a", "ward-b"}
	actions := sweepActions(exited, nil, "/base")
	if len(actions) != len(exited) {
		t.Fatalf("got %d actions, want %d drains and no remove", len(actions), len(exited))
	}
	for i, a := range actions {
		if a.Op != sweepDrain {
			t.Errorf("action %d op = %q, want a drain (no eviction when nothing is stale)", i, a.Op)
		}
	}
}

// TestResolveSinkMode covers the fixed local disk default.
func TestResolveSinkMode(t *testing.T) {
	if defaultSinkMode != sinkDisk {
		t.Error("default sink must keep the local disk archive")
	}
	if got := resolveSinkMode(); got != defaultSinkMode {
		t.Errorf("resolveSinkMode() = %q, want %q", got, defaultSinkMode)
	}
}

func TestSweepActionsEmpty(t *testing.T) {
	if got := sweepActions(nil, nil, "/base"); got != nil {
		t.Errorf("sweepActions(nil, nil) = %v, want nil", got)
	}
}

// TestDrainMarkerIdempotency covers the ward#510 duplicate-drain guard: the marker
// round-trips (write, observe, clear) so a removed name drains fresh on reuse.
func TestDrainMarkerIdempotency(t *testing.T) {
	base := t.TempDir()
	const name = "engineer-claude-ward-510"

	if alreadyDrained(base, name) {
		t.Fatal("a never-drained container must not read as drained")
	}
	markDrained(base, name)
	if !alreadyDrained(base, name) {
		t.Fatal("after markDrained the container must read as drained (the sweep skips it)")
	}
	// The sentinel lives under the hidden .drained subdir, not as a run artifact.
	if got := drainMarkerPath(base, name); got != filepath.Join(base, drainedMarkerSubdir, name) {
		t.Errorf("drainMarkerPath = %q, want it under %s/", got, drainedMarkerSubdir)
	}
	// Removal clears the marker so a reused deterministic name drains fresh.
	clearDrainMarker(base, name)
	if alreadyDrained(base, name) {
		t.Fatal("after clearDrainMarker the reused name must drain fresh, not be skipped")
	}
}

// TestDrainAgentRunIdempotentSkipsMarked pins that a pre-marked run pulls NO docker:
// a second drain is a pure sentinel check, never a re-pull to disk (ward#510).
func TestDrainAgentRunIdempotentSkipsMarked(t *testing.T) {
	base := t.TempDir()
	const name = "ward-already-drained"
	markDrained(base, name)

	// A docker that errors on every call: if the skip path called docker at all, the
	// disk sink would try to run it.
	r := fakeDockerRunner(t, "", 1)
	r.drainAgentRunIdempotent(context.Background(), name, base)

	// The skip must not have created the per-run disk dir (no drain ran).
	if _, err := os.Stat(filepath.Join(base, name)); err == nil {
		t.Error("an already-drained run must not be re-drained to disk")
	}
}

// TestWriteSecretSafeArtifacts proves arbitrary exact values and body-shaped
// tool inputs never reach the one canonical archive.
func TestWriteSecretSafeArtifacts(t *testing.T) {
	setTestHome(t, t.TempDir())
	name := "ward-agent-redact-test"
	secret := "synthetic-arbitrary-credential-value"
	console := []byte("boot\nleaked " + secret + " here\ndone\n")
	transcript := []byte(`{"type":"assistant","timestamp":"2026-06-26T02:00:00Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/workspace/ward/x.go","content":"secret ` + secret + ` body"}}]}}`)
	meta := runMeta{Container: name, Repo: "o/r", Issue: "526", Outcome: outcomePushedMain}
	redactor, err := newSecretRedactor([]string{secret}, nil)
	if err != nil {
		t.Fatal(err)
	}

	r := &Runner{}
	dir := filepath.Join(agentLogsDir(), name)
	if err := r.writeDiskArtifacts(name, dir, redactConsoleWith(console, redactor), redactedTranscriptWith(transcript, redactor), meta, nil); err != nil {
		t.Fatal(err)
	}

	con, err := os.ReadFile(filepath.Join(dir, drainConsoleFile))
	if err != nil {
		t.Fatalf("read console.log: %v", err)
	}
	if strings.Contains(string(con), secret) || !strings.Contains(string(con), redactionPlaceholder) {
		t.Errorf("safe console leaked or failed to mark a credential: %q", con)
	}
	tr, err := os.ReadFile(filepath.Join(dir, drainTranscriptFile))
	if err != nil {
		t.Fatalf("read transcript.jsonl: %v", err)
	}
	if strings.Contains(string(tr), secret) || strings.Contains(string(tr), "\"content\"") {
		t.Errorf("redacted transcript leaked a body/token: %q", tr)
	}
	if _, err := os.ReadFile(filepath.Join(dir, drainMetaFile)); err != nil {
		t.Errorf("read meta.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(historicalRawAgentLogsDir(), name)); !os.IsNotExist(err) {
		t.Errorf("safe write must not create the historical raw archive; stat err = %v", err)
	}
}

// TestDrainWaiterArgv pins the detached waiter re-enters ward at the hidden leaf.
func TestDrainWaiterArgv(t *testing.T) {
	got := drainWaiterArgv("engineer-claude-ward-510")
	want := []string{"container", "drain-exit", "engineer-claude-ward-510"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("drainWaiterArgv = %v, want %v", got, want)
	}
}

// TestPickMetaEnvAllowlistOnly is the meta.json security boundary: Config.Env also
// carries --env-file secrets, so only allowlisted dims may be copied out.
func TestPickMetaEnvAllowlistOnly(t *testing.T) {
	env := []string{
		"WARD_TARGET_REPO=coilyco-flight-deck/ward",
		"WARD_MODE=claude",
		"WARD_TARGET_ISSUE=363",
		"FORGEJO_TOKEN=secrettoken123",               // MUST NOT leak
		"WARD_CLAUDE_CREDS_B64=eyJzZWNyZXQiOiJ4In0=", // MUST NOT leak
		"PATH=/usr/bin",                              // not allowlisted
	}
	got := pickMetaEnv(env, metaEnvAllow)

	if got["WARD_TARGET_REPO"] != "coilyco-flight-deck/ward" {
		t.Errorf("WARD_TARGET_REPO = %q, want the repo slug", got["WARD_TARGET_REPO"])
	}
	if got["WARD_MODE"] != "claude" || got["WARD_TARGET_ISSUE"] != "363" {
		t.Errorf("allowlisted dims missing: %v", got)
	}
	for k := range got {
		if k == "FORGEJO_TOKEN" || k == "WARD_CLAUDE_CREDS_B64" || k == "PATH" {
			t.Fatalf("pickMetaEnv leaked a non-allowlisted key %q", k)
		}
	}
	// And no value should carry the secret material regardless of key.
	for k, v := range got {
		if strings.Contains(v, "secrettoken123") || strings.Contains(v, "eyJzZWNyZXQ") {
			t.Fatalf("secret material leaked through %q = %q", k, v)
		}
	}
}

func TestNormalizeRunSummaryOutcome(t *testing.T) {
	cases := []struct {
		name           string
		meta           runMeta
		env            map[string]string
		signals        runSummarySignals
		wantOutcome    string
		wantConfidence string
	}{
		{
			name:           "landed-main",
			meta:           runMeta{Launched: true, TranscriptPresent: true, Outcome: outcomePushedMain},
			env:            map[string]string{"WARD_WORKFLOW": "merge-remote-main"},
			wantOutcome:    "landed-main",
			wantConfidence: "high",
		},
		{
			name: "pr-green",
			meta: runMeta{Launched: true, TranscriptPresent: true, Outcome: outcomeNothing},
			env:  map[string]string{"WARD_WORKFLOW": string(workflowPullRequest)},
			signals: runSummarySignals{
				WardOutcomeComment: true,
				ChecksGreen:        true,
			},
			wantOutcome:    "pr-green",
			wantConfidence: "high",
		},
		{
			name: "blocked",
			meta: runMeta{Launched: true, TranscriptPresent: true, Outcome: outcomeUnknown},
			signals: runSummarySignals{
				WardOutcomeComment: true,
				LatestOutcome:      backlogOutcome{Status: "blocked", Text: "blocked: reviewer request"},
			},
			wantOutcome:    "blocked",
			wantConfidence: "high",
		},
		{
			name: "salvage",
			meta: runMeta{Launched: true, TranscriptPresent: true, Outcome: outcomeSalvage},
			signals: runSummarySignals{
				PreservedBranch: "ward-salvage/ward-abc123",
			},
			wantOutcome:    "preserved-salvage-branch",
			wantConfidence: "high",
		},
		{
			name:           "prelaunch-no-transcript",
			meta:           runMeta{Launched: false, TranscriptPresent: false, Outcome: outcomeUnknown},
			wantOutcome:    "prelaunch-failure",
			wantConfidence: "high",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotOutcome, gotConfidence := normalizeRunSummaryOutcome(tc.meta, tc.env, tc.signals)
			if gotOutcome != tc.wantOutcome {
				t.Fatalf("normalized outcome = %q, want %q", gotOutcome, tc.wantOutcome)
			}
			if gotConfidence != tc.wantConfidence {
				t.Fatalf("outcome confidence = %q, want %q", gotConfidence, tc.wantConfidence)
			}
		})
	}
}

func TestWriteDiskArtifactsAddsRunSummaryFooter(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	name := "engineer-codex-ward-526"
	dir := filepath.Join(t.TempDir(), "archive")
	console := []byte("boot\nward container reap: landed on main\n")
	meta := runMeta{
		Container: name,
		Outcome:   outcomePushedMain,
		Summary: runSummary{
			SchemaVersion:     runSummarySchemaVersion,
			Workflow:          string(workflowPullRequest),
			NormalizedOutcome: "pr-green",
			Artifacts: runSummaryArtifacts{
				Transcript: filepath.Join(agentLogsDir(), name, drainTranscriptFile),
			},
		},
	}

	r := &Runner{}
	if err := r.writeDiskArtifacts(name, dir, console, nil, meta, nil); err != nil {
		t.Fatal(err)
	}

	rawConsole, err := os.ReadFile(filepath.Join(dir, drainConsoleFile))
	if err != nil {
		t.Fatalf("read console.log: %v", err)
	}
	if !strings.Contains(string(rawConsole), "WARD-RUN-SUMMARY: outcome=pr-green") {
		t.Fatalf("console.log missing structured footer:\n%s", rawConsole)
	}
	rawMeta, err := os.ReadFile(filepath.Join(dir, drainMetaFile))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	if !strings.Contains(string(rawMeta), `"summary"`) || !strings.Contains(string(rawMeta), `"schema_version": 1`) {
		t.Fatalf("meta.json missing summary block:\n%s", rawMeta)
	}
}

func dockerInspectStateStubDir(t *testing.T, stateJSON, envJSON string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .State}}' ]; then\n" +
		"  printf '%s' " + shellQuote(stateJSON) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .Config.Env}}' ]; then\n" +
		"  printf '%s' " + shellQuote(envJSON) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return dir
}

func TestBuildRunMetaRecordsOOMKilled(t *testing.T) {
	dir := dockerInspectStateStubDir(t,
		`{"OOMKilled":true,"ExitCode":0}`,
		`["WARD_TARGET_REPO=coilyco-flight-deck/ward","WARD_TARGET_ISSUE=883","WARD_MODE=codex","WARD_BRANCH=issue-883","WARD_AGENT_LAUNCHED=1"]`,
	)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: shell.PathResolver}}
	meta := r.buildRunMeta(t.Context(), "director-codex-vg94", "agent exited non-zero (signal: killed; docker state: OOMKilled=true); reaping anyway", []byte(`{"type":"assistant","timestamp":"2026-07-15T01:00:00Z"}`+"\n"))
	if !meta.OOMKilled {
		t.Fatal("buildRunMeta must carry Docker OOMKilled=true into meta.json")
	}
	if meta.Container != "director-codex-vg94" || meta.Repo != "coilyco-flight-deck/ward" || meta.Outcome != outcomeUnknown {
		t.Fatalf("unexpected meta record: %+v", meta)
	}
}

func TestBuildRunMetaDerivesLaunchedFromReapLifecycle(t *testing.T) {
	dir := dockerInspectStateStubDir(t,
		`{"OOMKilled":false,"ExitCode":0}`,
		`["WARD_TARGET_REPO=coilyco-flight-deck/ward","WARD_TARGET_ISSUE=1543","WARD_MODE=codex","WARD_BRANCH=issue-1543"]`,
	)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: shell.PathResolver}}
	console := "WARD-REAP: start container=engineer-codex-ward-1543 repo=coilyco-flight-deck/ward issue=1543 readOnly=false extraRepos=0 launched=true\n" +
		"WARDED_WORKFLOW: dispatch-failed is source text, not lifecycle evidence\n"
	meta := r.buildRunMeta(t.Context(), "engineer-codex-ward-1543", console, []byte(`{"type":"assistant","timestamp":"2026-07-15T01:00:00Z"}`+"\n"))
	if !meta.Launched {
		t.Fatal("buildRunMeta must derive launched=true from the WARD-REAP lifecycle marker")
	}
	for _, ev := range meta.Friction {
		if ev.Category == "prelaunch-failure" {
			t.Fatalf("source text was classified as prelaunch friction: %+v", meta.Friction)
		}
	}
}

func TestCollectFrictionEvents(t *testing.T) {
	cases := []struct {
		name      string
		meta      runMeta
		console   string
		wantCats  []string
		wantStage []string
	}{
		{
			name:    "clean landed run",
			meta:    runMeta{Driver: string(modeCodex), Launched: true, TranscriptPresent: true},
			console: "ward container reap: landed on main\n",
		},
		{
			name:    "green pr boundary",
			meta:    runMeta{Driver: string(modeClaude), Launched: true, TranscriptPresent: true},
			console: "WARD-REAP: nothing to reap (workflow pull-request boundary reached with a clean local tree. The reaper did not verify remote branch, pull request, or CI state)\n",
		},
		{
			name:      "no transcript prelaunch failure",
			meta:      runMeta{Driver: string(modeCodex), Launched: false, TranscriptPresent: false},
			console:   "ward container reap: released issue reservation on #123 (container exited pre-launch, did no work)\n",
			wantCats:  []string{"missing-transcript", "prelaunch-failure"},
			wantStage: []string{"drain", "reap"},
		},
		{
			name:      "preserved salvage branch",
			meta:      runMeta{Driver: string(modeClaude), Launched: true, TranscriptPresent: true},
			console:   "ward container reap: preserved work on ward-salvage/ward-abc123 (merge conflict integrating onto main)\n",
			wantCats:  []string{"salvage-noise"},
			wantStage: []string{"reap"},
		},
		{
			name:      "unrelated extra repo residual preservation",
			meta:      runMeta{Driver: string(modeClaude), Launched: true, TranscriptPresent: true},
			console:   "ward container reap: preserved un-landed granted-repo work on ward-salvage/umbra-abc123 (coilyco-flight-deck/umbra)\n",
			wantCats:  []string{"extra-repo-preservation"},
			wantStage: []string{"reap"},
		},
		{
			name:    "source text does not make prelaunch failure",
			meta:    runMeta{Driver: string(modeCodex), Launched: false, TranscriptPresent: true},
			console: "const status = \"dispatch-failed\" // launch failed in source text\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectFrictionEvents(tc.meta, tc.console)
			if len(tc.wantCats) == 0 {
				if len(got) != 0 {
					t.Fatalf("collectFrictionEvents(%s) = %+v, want no events", tc.name, got)
				}
				return
			}
			if len(got) != len(tc.wantCats) {
				t.Fatalf("collectFrictionEvents(%s) = %+v, want %d event(s)", tc.name, got, len(tc.wantCats))
			}
			for i, ev := range got {
				if ev.Category != tc.wantCats[i] {
					t.Errorf("event %d category = %q, want %q", i, ev.Category, tc.wantCats[i])
				}
				if ev.Stage != tc.wantStage[i] {
					t.Errorf("event %d stage = %q, want %q", i, ev.Stage, tc.wantStage[i])
				}
				if ev.Fingerprint == "" || ev.Evidence == "" {
					t.Errorf("event %d missing fingerprint/evidence: %+v", i, ev)
				}
			}
		})
	}
}

func TestClassifyReapOutcome(t *testing.T) {
	cases := []struct {
		name    string
		console string
		want    string
	}{
		{"landed", "ward container reap: landed on main\n", outcomePushedMain},
		{"salvage-branch", "ward container reap: preserved work on ward-salvage/ward-abc (merge conflict)\n", outcomeSalvage},
		{"salvage-prefix", "preserved un-landed granted-repo work on ward-salvage/x\n", outcomeSalvage},
		{"nothing", "WARD-REAP: nothing to reap (tree clean, HEAD on origin/main)\n", outcomeNothing},
		{"empty", "", outcomeUnknown},
		{"noise", "some unrelated docker output\n", outcomeUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyReapOutcome(c.console); got != c.want {
				t.Errorf("classifyReapOutcome(%q) = %q, want %q", c.console, got, c.want)
			}
		})
	}
}

// TestExtractTranscriptFromTar asserts the docker-cp tar walk pulls only the
// jsonl session files and concatenates them line-complete.
func TestExtractTranscriptFromTar(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := []struct {
		name, body string
	}{
		{"projects/enc/session-a.jsonl", `{"type":"assistant"}` + "\n" + `{"type":"user"}`}, // no trailing newline
		{"projects/enc/notes.txt", "ignore me"},
		{"projects/enc/session-b.jsonl", `{"type":"result"}` + "\n"},
	}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Typeflag: tar.TypeReg, Size: int64(len(f.body)), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	body, paths := extractTranscriptArchiveFromTar(buf.Bytes())
	got := string(body)
	if strings.Contains(got, "ignore me") {
		t.Errorf("non-jsonl member leaked into transcript: %q", got)
	}
	for _, want := range []string{`{"type":"assistant"}`, `{"type":"user"}`, `{"type":"result"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript missing %q; got:\n%s", want, got)
		}
	}
	// Each session file must be newline-terminated so the concatenation stays one
	// JSON event per line.
	for _, ln := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			t.Errorf("empty line in concatenated transcript:\n%s", got)
		}
	}
	if gotPaths, wantPaths := strings.Join(paths, ","), "enc/session-a.jsonl,enc/session-b.jsonl"; gotPaths != wantPaths {
		t.Errorf("transcript paths = %q, want %q", gotPaths, wantPaths)
	}
}

func TestDrainTranscriptUsesHarnessTranscriptTree(t *testing.T) {
	cases := []struct {
		name        string
		container   string
		wantSuffix  string
		wantContent string
	}{
		{
			name:        "claude",
			container:   "engineer-claude-ward-883",
			wantSuffix:  ".claude/projects",
			wantContent: `{"type":"assistant","text":"claude progress"}` + "\n",
		},
		{
			name:        "codex",
			container:   "engineer-codex-ward-883",
			wantSuffix:  ".codex/sessions",
			wantContent: `{"type":"assistant","text":"codex progress"}` + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tarBytes := liveTranscriptTar(t, map[string]string{
				"sessions/session-a.jsonl": tc.wantContent,
			})
			r := fakeAgentLogsDockerRunner(t, "", "", tarBytes, tc.wantSuffix)
			got := r.drainTranscript(t.Context(), tc.container)
			if !strings.Contains(string(got), tc.wantContent) {
				t.Fatalf("drainTranscript(%q) = %q, want transcript content", tc.container, got)
			}
		})
	}
}

func TestExtractTranscriptFromTarGarbage(t *testing.T) {
	if got := extractTranscriptFromTar([]byte("not a tar at all")); len(got) != 0 {
		t.Errorf("garbage tar yielded %q, want empty", got)
	}
}
