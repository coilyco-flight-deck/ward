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

// agent_log_drain.go is the host-side drain of agent-run observability (ward#363,
// ward#532): before the keep-10 rm, it pulls a container's console + transcript.

// It runs host-side because the reaper runs INSIDE the container with no docker
// socket. The release surface keeps only local artifacts and the redacted view.

// agentLogsSubdir is the per-host archive root under the .ward app dir, sibling
// to audit/ (docs/audit.md) - one directory per drained container run.
const agentLogsSubdir = "agent-logs"

// agentLogsRedactedSubdir is the parallel archive root for the redacted-at-rest view
// (ward#526): a SEPARATE tree the surface binds so raw logs never mount. See docs.
const agentLogsRedactedSubdir = "agent-logs-redacted"

// containerTranscriptDir is where claude writes session jsonl for the agent user;
// the drain `docker cp`s the tree out as a tar and concatenates the jsonl.
const containerTranscriptDir = "/home/ubuntu/.claude/projects"

// drained artifact filenames inside ~/.ward/agent-logs/<slug>/.
const (
	drainConsoleFile    = "console.log"
	drainTranscriptFile = "transcript.jsonl"
	drainMetaFile       = "meta.json"
)

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
// memory and writes the local artifacts plus the redacted sibling tree.
func (r *Runner) drainAgentRun(ctx context.Context, name, dir string) {
	fmt.Fprintf(os.Stderr, "ward container: starting drain of container %s\n", name)

	// Console: docker logs carries both the agent's stdout stream and the reaper's
	// stderr markers; capture combined so the outcome is inferable from one stream.
	console := r.dockerLogsCombined(ctx, name)

	// Transcript: docker cp streams the projects tree as a tar to stdout; pull the
	// jsonl out and concatenate. Held in memory until the local artifacts are written.
	transcript := r.drainTranscript(ctx, name)

	// Meta: safe dims from the inspected env allowlist + the inferred outcome.
	meta := r.buildRunMeta(ctx, name, string(console))

	r.writeDiskArtifacts(name, dir, console, transcript, meta)
	// The redacted view rides the same disk gate: whenever the raw archive lands,
	// its scrubbed sibling lands too, for the director surface mount (ward#526).
	r.writeRedactedArtifacts(name, console, transcript, meta)
	fmt.Fprintf(os.Stderr, "ward container: drained %s (outcome %s)\n", name, meta.Outcome)
}

// writeDiskArtifacts persists console.log + transcript.jsonl + meta.json under dir.
func (r *Runner) writeDiskArtifacts(name, dir string, console, transcript []byte, meta runMeta) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not create %s (%v); skipping disk sink\n", name, dir, err)
		return
	}
	if werr := os.WriteFile(filepath.Join(dir, drainConsoleFile), console, 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write console.log: %v\n", name, werr)
	}
	if len(transcript) > 0 {
		if werr := os.WriteFile(filepath.Join(dir, drainTranscriptFile), transcript, 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write transcript.jsonl: %v\n", name, werr)
		}
	}
	if data, merr := json.MarshalIndent(meta, "", "  "); merr == nil {
		if werr := os.WriteFile(filepath.Join(dir, drainMetaFile), append(data, '\n'), 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write meta.json: %v\n", name, werr)
		}
	}
	fmt.Fprintf(os.Stderr, "ward container: wrote disk artifacts for %s -> %s\n", name, dir)
}

// writeRedactedArtifacts persists the redacted-at-rest view (ward#526) under the
// agent-logs-redacted tree; scrubbers shared with the local redaction path. Best-effort.
func (r *Runner) writeRedactedArtifacts(name string, console, transcript []byte, meta runMeta) {
	dir := filepath.Join(agentLogsRedactedDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not create %s (%v); skipping redacted view\n", name, dir, err)
		return
	}
	if werr := os.WriteFile(filepath.Join(dir, drainConsoleRedactedFile), redactConsole(console), 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: write console.redacted.log: %v\n", name, werr)
	}
	if red := redactedTranscript(transcript); len(red) > 0 {
		if werr := os.WriteFile(filepath.Join(dir, drainTranscriptRedactedFile), red, 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write transcript.redacted.jsonl: %v\n", name, werr)
		}
	}
	if data, merr := json.MarshalIndent(meta, "", "  "); merr == nil {
		if werr := os.WriteFile(filepath.Join(dir, drainMetaFile), append(data, '\n'), 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write redacted meta.json: %v\n", name, werr)
		}
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
// concatenated jsonl; an absent tree (a goose run writes none) returns nil.
func (r *Runner) drainTranscript(ctx context.Context, name string) []byte {
	// `docker cp <c>:<path> -` writes a tar of <path> to stdout. The trailing
	// stderr ("no such file") is discarded; an empty/garbage tar yields nil.
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.dockerCapture(ctx, "cp", name+":"+containerTranscriptDir, "-")
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
