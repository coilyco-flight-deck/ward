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
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
)

// agent_log_drain.go is slice 1 of agent-run observability (ward#363): a host-side
// drain of an exited container's console + transcript + meta before the keep-10 rm.

// It runs host-side because the reaper runs INSIDE the container with no docker
// socket. Always-on, best-effort. See docs/agent-observability.md.

// agentLogsSubdir is the per-host archive root under the .ward app dir, sibling
// to audit/ (docs/audit.md) - one directory per drained container run.
const agentLogsSubdir = "agent-logs"

// containerTranscriptDir is where claude writes session jsonl for the agent user;
// the drain `docker cp`s the tree out as a tar and concatenates the jsonl.
const containerTranscriptDir = "/home/ubuntu/.claude/projects"

// drained artifact filenames inside ~/.ward/agent-logs/<slug>/.
const (
	drainConsoleFile    = "console.log"
	drainTranscriptFile = "transcript.jsonl"
	drainMetaFile       = "meta.json"
)

// metaEnvAllow is the strict allowlist of container env keys copied into meta.json.
// Config.Env also carries --env-file secrets, so only these known-safe dims ride.
var metaEnvAllow = []string{
	"WARD_TARGET_REPO",
	"WARD_TARGET_OWNER",
	"WARD_TARGET_NAME",
	"WARD_TARGET_ISSUE",
	"WARD_MODE",
	"WARD_BRANCH",
}

// run outcome strings recorded in meta.json, inferred from the reaper's console
// markers (the reaper logs these on every teardown path; container_reap.go).
const (
	outcomePushedMain = "pushed-to-main"
	outcomeSalvage    = "ward-salvage"
	outcomeNothing    = "nothing-to-reap"
	outcomeUnknown    = "unknown"
)

// agentLogsDir resolves the host archive root: the .ward app dir under $HOME,
// falling back to $TMPDIR when $HOME is unset (mirrors config.CacheDir).
func agentLogsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, config.AppDir(), agentLogsSubdir)
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

// sweepActions is the pure plan: drain each stale container into baseDir/<name>,
// THEN remove them all. A test asserts no remove precedes a drain (ward#363).
func sweepActions(stale []string, baseDir string) []sweepAction {
	if len(stale) == 0 {
		return nil
	}
	actions := make([]sweepAction, 0, len(stale)+1)
	for _, name := range stale {
		actions = append(actions, sweepAction{
			Op:        sweepDrain,
			Container: name,
			Dir:       filepath.Join(baseDir, name),
		})
	}
	return append(actions, sweepAction{Op: sweepRemove, Names: stale})
}

// drainStaleContainers executes the sweep plan: drain each container (best-effort)
// to the host archive, then `docker rm` the set, returning only the rm error.
func (r *Runner) drainStaleContainers(ctx context.Context, stale []string) error {
	baseDir := agentLogsDir()
	for _, a := range sweepActions(stale, baseDir) {
		switch a.Op {
		case sweepDrain:
			r.drainAgentRun(ctx, a.Container, a.Dir)
		case sweepRemove:
			return r.Runner.Exec(ctx, "docker", dockerRmArgv(a.Names)...)
		}
	}
	return nil
}

// drainAgentRun copies one exited container's console + transcript + meta.json to
// dir, best-effort, then ships the envelope stream if WARD_AGENT_TELEMETRY=1.
func (r *Runner) drainAgentRun(ctx context.Context, name, dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not create %s (%v); skipping\n", name, dir, err)
		return
	}

	// Console: docker logs carries both the agent's stdout stream and the reaper's
	// stderr markers; capture combined so the outcome is inferable from one file.
	console := r.dockerLogsCombined(ctx, name)
	if werr := os.WriteFile(filepath.Join(dir, drainConsoleFile), console, 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write console.log: %v\n", name, werr)
	}

	// Transcript: docker cp streams the projects tree as a tar to stdout; pull the
	// jsonl session files out of it and concatenate (each line is one event).
	transcript := r.drainTranscript(ctx, name)
	if len(transcript) > 0 {
		if werr := os.WriteFile(filepath.Join(dir, drainTranscriptFile), transcript, 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write transcript.jsonl: %v\n", name, werr)
		}
	}

	// Meta: safe dims from the inspected env allowlist + the inferred outcome.
	meta := r.buildRunMeta(ctx, name, string(console))
	if data, merr := json.MarshalIndent(meta, "", "  "); merr == nil {
		if werr := os.WriteFile(filepath.Join(dir, drainMetaFile), append(data, '\n'), 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write meta.json: %v\n", name, werr)
		}
	}
	fmt.Fprintf(os.Stderr, "ward container: drained %s -> %s (outcome %s)\n", name, dir, meta.Outcome)

	// Slice 2 (ward#363): the external envelope export is opt-in and default-OFF;
	// the host drain above is always-on. Only the OTLP push is gated.
	r.maybeShipTelemetry(ctx, transcript, meta)
}

// dockerLogsCombined captures `docker logs <name>` with stdout+stderr merged into
// one buffer so the reaper's stderr markers survive alongside the agent stream.
func (r *Runner) dockerLogsCombined(ctx context.Context, name string) []byte {
	var buf bytes.Buffer
	prevOut, prevErr := r.Runner.Stdout, r.Runner.Stderr
	r.Runner.Stdout, r.Runner.Stderr = &buf, &buf
	_ = r.Runner.Exec(ctx, "docker", "logs", name)
	r.Runner.Stdout, r.Runner.Stderr = prevOut, prevErr
	return buf.Bytes()
}

// drainTranscript `docker cp`s the transcript tree out as a tar and returns the
// concatenated jsonl; an absent tree (a goose run writes none) returns nil.
func (r *Runner) drainTranscript(ctx context.Context, name string) []byte {
	// `docker cp <c>:<path> -` writes a tar of <path> to stdout. The trailing
	// stderr ("no such file") is discarded; an empty/garbage tar yields nil.
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.Runner.Capture(ctx, "docker", "cp", name+":"+containerTranscriptDir, "-")
	r.Runner.Stderr = prevErr
	if err != nil || len(out) == 0 {
		return nil
	}
	return extractTranscriptFromTar(out)
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
// who the run was (env allowlist) and how the reaper resolved it (console markers).
type runMeta struct {
	Container string `json:"container"`
	Repo      string `json:"repo,omitempty"`
	Issue     string `json:"issue,omitempty"`
	Driver    string `json:"driver,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Outcome   string `json:"outcome"`
}

// buildRunMeta assembles the meta record from the container's inspected env
// (allowlisted) and the reaper's console markers.
func (r *Runner) buildRunMeta(ctx context.Context, name, console string) runMeta {
	env := r.inspectContainerEnv(ctx, name)
	return runMeta{
		Container: name,
		Repo:      env["WARD_TARGET_REPO"],
		Issue:     env["WARD_TARGET_ISSUE"],
		Driver:    env["WARD_MODE"],
		Branch:    env["WARD_BRANCH"],
		Outcome:   classifyReapOutcome(console),
	}
}

// inspectContainerEnv reads the container's Config.Env and returns ONLY the
// allowlisted WARD_* dims (never the --env-file secrets that also live there).
func (r *Runner) inspectContainerEnv(ctx context.Context, name string) map[string]string {
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.Runner.Capture(ctx, "docker", "inspect", "--format", "{{json .Config.Env}}", name)
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
