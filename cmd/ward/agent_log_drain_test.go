package main

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestResolveSinkMode covers the env override + the local-exclusive default.
func TestResolveSinkMode(t *testing.T) {
	cases := []struct {
		set  string
		want sinkMode
	}{
		{"", defaultSinkMode},
		{"signoz", sinkSignoz},
		{"disk", sinkDisk},
		{"both", sinkBoth},
		{"DISK", sinkDisk},           // case-insensitive
		{"  both  ", sinkBoth},       // trimmed
		{"garbage", defaultSinkMode}, // unrecognized falls back, never fails
	}
	for _, c := range cases {
		t.Setenv(envSinkMode, c.set)
		if got := resolveSinkMode(); got != c.want {
			t.Errorf("resolveSinkMode with %q = %q, want %q", c.set, got, c.want)
		}
	}
	// The default must be signoz-exclusive: no disk, yes signoz.
	if defaultSinkMode.wantsDisk() {
		t.Error("default sink writes to disk; ward#532 requires signoz-exclusive by default")
	}
	if !defaultSinkMode.wantsSignoz() {
		t.Error("default sink does not ship to signoz")
	}
	// both is the only mode that does both.
	if !sinkBoth.wantsDisk() || !sinkBoth.wantsSignoz() {
		t.Error("both mode must do disk AND signoz")
	}
	if sinkDisk.wantsSignoz() || sinkSignoz.wantsDisk() {
		t.Error("disk/signoz modes must be exclusive")
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
	// disk sink would try to run it. Route to disk so a drain WOULD hit docker.
	t.Setenv(envSinkMode, string(sinkDisk))
	r := fakeDockerRunner(t, "", 1)
	r.drainAgentRunIdempotent(context.Background(), name, base)

	// The skip must not have created the per-run disk dir (no drain ran).
	if _, err := os.Stat(filepath.Join(base, name)); err == nil {
		t.Error("an already-drained run must not be re-drained to disk")
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

func TestClassifyReapOutcome(t *testing.T) {
	cases := []struct {
		name    string
		console string
		want    string
	}{
		{"landed", "ward container reap: landed on main\n", outcomePushedMain},
		{"salvage-branch", "ward container reap: preserved work on ward-salvage/ward-abc (merge conflict)\n", outcomeSalvage},
		{"salvage-prefix", "preserved un-landed granted-repo work on ward-salvage/x\n", outcomeSalvage},
		{"nothing", "ward container reap: nothing to reap (tree clean, HEAD on origin/main)\n", outcomeNothing},
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

	got := string(extractTranscriptFromTar(buf.Bytes()))
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
}

func TestExtractTranscriptFromTarGarbage(t *testing.T) {
	if got := extractTranscriptFromTar([]byte("not a tar at all")); len(got) != 0 {
		t.Errorf("garbage tar yielded %q, want empty", got)
	}
}
