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

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_logs.go wires `ward agent logs`: a director surface can read one engineer
// run's logs through the dispatch broker, or host-side when no broker is present.

// agentLogSourceKind labels the source the broker resolved.
type agentLogSourceKind string

const (
	agentLogSourceDocker agentLogSourceKind = "docker"
	agentLogSourceFile   agentLogSourceKind = "file"
)

// agentLogSource is the resolved log source the host prints and streams from.
type agentLogSource struct {
	Kind           agentLogSourceKind
	Container      string
	Path           string
	TranscriptTree string
	ArchiveMeta    runMeta
	Tail           int
	Follow         bool
}

func (s agentLogSource) String() string {
	switch s.Kind {
	case agentLogSourceDocker:
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
	case agentLogSourceFile:
		if strings.TrimSpace(s.ArchiveMeta.Outcome) != "" {
			return fmt.Sprintf("%s (outcome %s)", s.Path, s.ArchiveMeta.Outcome)
		}
		return s.Path
	default:
		return ""
	}
}

// agentLogsCommand builds `ward agent logs <ref>`: the director-surface log read.
func agentLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Read one engineer's run logs through the dispatch broker - director-surface, source-reported, tail/follow aware.",
		ArgsUsage: "<owner/repo#N | #N | container-name>",
		Description: `logs reads one engineer run's console output through the host dispatch broker when a
director surface is attached, or host-side otherwise. It resolves the target the same
way ` + "`ward agent stop`" + ` does: issue ref or container name. When a live engineer
container is present it prefers ` + "`docker logs`" + `, and when that container has been
removed it falls back to the drained host archive at ~/.ward/agent-logs/<container>/console.log
or the redacted sibling. The chosen source is printed before the body streams.

  ward agent logs coilyco-flight-deck/ward#692
  ward agent logs engineer-goose-ward-692
  ward agent logs coilyco-flight-deck/ward#692 --tail 200
  ward agent logs coilyco-flight-deck/ward#692 --follow`,
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "tail", Usage: "show the last N lines when reading logs (0 means all lines)"},
			&cli.BoolFlag{Name: "follow", Usage: "follow live docker logs instead of taking a snapshot"},
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

// runAgentLogs resolves the target and streams its logs either directly or through
// the host dispatch broker.
func (r *Runner) runAgentLogs(ctx context.Context, c *cli.Command) error {
	arg := strings.TrimSpace(c.Args().First())
	if arg == "" {
		return fmt.Errorf("ward agent logs: a target is required: owner/repo#N, a bare #N, or a container name")
	}
	tail := c.Int("tail")
	if tail < 0 {
		return fmt.Errorf("ward agent logs: --tail must be >= 0")
	}
	follow := c.Bool("follow")

	if addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)); addr != "" && os.Getenv("WARD_READONLY") == "1" {
		if err := r.forwardAgentLogsToHostBroker(ctx, addr, arg, tail, follow); err != nil {
			if !errors.Is(err, errDispatchBrokerUnavailable) {
				return err
			}
		} else {
			return nil
		}
	}
	source, err := r.resolveAgentLogsSource(ctx, arg, tail, follow)
	if err != nil {
		return fmt.Errorf("ward agent logs: %w", err)
	}
	writef(r.Runner.Stderr, "ward agent logs: using %s\n", source.String())
	return r.streamAgentLogsSource(ctx, source, r.Runner.Stdout)
}

// forwardAgentLogsToHostBroker forwards the read to host ward, then relays the body.
func (r *Runner) forwardAgentLogsToHostBroker(ctx context.Context, addr, target string, tail int, follow bool) error {
	req := dispatchBrokerRequest{
		Action:    dispatchActionLogs,
		Target:    target,
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

// resolveAgentLogsSource resolves the target to a live engineer container or a
// drained log file, preferring the live Docker path when available.
func (r *Runner) resolveAgentLogsSource(ctx context.Context, target string, tail int, follow bool) (agentLogSource, error) {
	if ref, err := parseAgentIssueRef(target); err == nil && ref.Owner != "" && ref.Repo != "" {
		return r.resolveAgentLogsSourceForIssue(ctx, ref, tail, follow)
	}
	return r.resolveAgentLogsSourceForName(ctx, target, tail, follow)
}

// resolveAgentLogsSourceForIssue resolves a carried issue to one engineer container
// when possible, else to a drained archive for that same issue.
func (r *Runner) resolveAgentLogsSourceForIssue(ctx context.Context, ref agentIssueRef, tail int, follow bool) (agentLogSource, error) {
	if names := r.allEngineerContainersForIssue(ctx, ref); len(names) > 0 {
		name, err := selectSingleEngineerTarget("read", ref.String(), names)
		if err != nil {
			return agentLogSource{}, err
		}
		return agentLogSource{Kind: agentLogSourceDocker, Container: name, TranscriptTree: containerTranscriptDir(containerModeFromContainerName(name)), Tail: tail, Follow: follow}, nil
	}
	if src, err := findArchivedAgentLogSourceByIssue(ref, tail, follow, agentLogsDir(), drainConsoleFile); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
		}
		return src, nil
	}
	if src, err := findArchivedAgentLogSourceByIssue(ref, tail, follow, agentLogsRedactedDir(), drainConsoleRedactedFile); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
		}
		return src, nil
	}
	if path, ok, err := latestDispatchConsolePathForRef(ref); err != nil {
		return agentLogSource{}, err
	} else if ok {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %s", ref)
		}
		return agentLogSource{Kind: agentLogSourceFile, Path: path}, nil
	}
	return agentLogSource{}, fmt.Errorf("dispatch broker: no engineer log source matches %q", ref.String())
}

func (r *Runner) resolveAgentLogsSourceForName(ctx context.Context, name string, tail int, follow bool) (agentLogSource, error) {
	if strings.TrimSpace(name) == "" {
		return agentLogSource{}, fmt.Errorf("container name is empty")
	}
	if r.containerPresent(ctx, name) {
		return r.resolveAgentLogsSourceForRunningName(ctx, name, tail, follow)
	}
	if src, err := r.resolveArchivedAgentLogSourceForName(name, tail, follow); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		return src, nil
	}
	if src, err := r.resolveDispatchArtifactSourceForName(name, tail, follow); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		return src, nil
	}
	return agentLogSource{}, fmt.Errorf("dispatch broker: no engineer log source matches %q", name)
}

func (r *Runner) resolveAgentLogsSourceForRunningName(ctx context.Context, name string, tail int, follow bool) (agentLogSource, error) {
	role, err := r.containerRoleLabel(ctx, name)
	if err != nil {
		return agentLogSource{}, fmt.Errorf("dispatch broker: refusing to read %q: could not read its %s label (%w) - "+
			"fail-closed, only %s containers are readable", name, labelRole, err, roleEngineer)
	}
	if err := stopTargetGuard(name, role); err != nil {
		return agentLogSource{}, err
	}
	return agentLogSource{Kind: agentLogSourceDocker, Container: name, TranscriptTree: containerTranscriptDir(containerModeFromContainerName(name)), Tail: tail, Follow: follow}, nil
}

func (r *Runner) resolveArchivedAgentLogSourceForName(name string, tail int, follow bool) (agentLogSource, error) {
	if src, err := findArchivedAgentLogSourceByName(name, tail, follow, agentLogsDir(), drainConsoleFile); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %q", name)
		}
		return src, nil
	}
	if src, err := findArchivedAgentLogSourceByName(name, tail, follow, agentLogsRedactedDir(), drainConsoleRedactedFile); err != nil {
		return agentLogSource{}, err
	} else if src.Kind != "" {
		if follow {
			return agentLogSource{}, fmt.Errorf("ward agent logs: --follow requires a live docker container for %q", name)
		}
		return src, nil
	}
	return agentLogSource{}, nil
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
	return agentLogSource{Kind: agentLogSourceFile, Path: paths.ConsolePath, ArchiveMeta: runMeta{Container: meta.RequestID, Repo: meta.Repo, Issue: meta.Issue, Driver: meta.Harness, Branch: meta.Ref, Outcome: meta.Outcome}, Tail: tail, Follow: follow}, nil
}

// streamAgentLogsSource writes the chosen source to w. Live docker sources follow
// when requested, while archive sources are snapshot-only.
func (r *Runner) streamAgentLogsSource(ctx context.Context, source agentLogSource, w io.Writer) error {
	switch source.Kind {
	case agentLogSourceDocker:
		return r.streamDockerAgentLogsSource(ctx, source, w)
	case agentLogSourceFile:
		return r.streamFileAgentLogsSource(source, w)
	default:
		return fmt.Errorf("ward agent logs: unknown log source kind %q", source.Kind)
	}
}

func (r *Runner) streamDockerAgentLogsSource(ctx context.Context, source agentLogSource, w io.Writer) error {
	argv := []string{"logs"}
	if source.Tail > 0 {
		argv = append(argv, "--tail", strconv.Itoa(source.Tail))
	}
	if source.Follow {
		argv = append(argv, "--follow")
	}
	argv = append(argv, source.Container)
	if source.Follow {
		prevOut, prevErr := r.Runner.Stdout, r.Runner.Stderr
		r.Runner.Stdout, r.Runner.Stderr = w, w
		defer func() {
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
	_, err = w.Write(out)
	return err
}

func (r *Runner) streamEmptyDockerAgentLogsSource(ctx context.Context, source agentLogSource, w io.Writer) error {
	tree := strings.TrimSpace(source.TranscriptTree)
	if tree == "" {
		_, _ = fmt.Fprintf(w, "ward agent logs: docker logs empty; no readable live transcript tree for %s\n", source.Container)
		return nil
	}
	transcript := r.liveTranscriptSource(ctx, source.Container, tree)
	if len(transcript) == 0 {
		_, _ = fmt.Fprintf(w, "ward agent logs: docker logs empty; live transcript tree at %s is empty\n", tree)
		return nil
	}
	if source.Tail > 0 {
		transcript = tailBytes(transcript, source.Tail)
	}
	_, _ = fmt.Fprintf(w, "ward agent logs: docker logs empty; using live transcript tree from %s\n", tree)
	_, err := w.Write(transcript)
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
	_, err = w.Write(b)
	return err
}

// liveTranscriptSource pulls the current transcript tree from a live engineer
// container and returns the concatenated jsonl the drain would eventually archive.
func (r *Runner) liveTranscriptSource(ctx context.Context, name, transcriptTree string) []byte {
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.dockerCapture(ctx, "cp", name+":"+transcriptTree, "-")
	r.Runner.Stderr = prevErr
	if err != nil || len(out) == 0 {
		return nil
	}
	return extractTranscriptFromTar(out)
}

// allEngineerContainersForIssue lists every live or exited engineer container that
// matches ref's repo + issue, so the read path can fail loud on ambiguity.
func (r *Runner) allEngineerContainersForIssue(ctx context.Context, ref agentIssueRef) []string {
	out, err := r.dockerCapture(ctx, "ps", "-a", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRole+"="+roleEngineer,
		"--filter", "label="+labelRepo+"="+ref.repoSlug(),
		"--filter", fmt.Sprintf("label=%s=%d", labelIssue, ref.Number))
	if err != nil {
		return nil
	}
	return parseExitedContainerNames(string(out))
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

// findArchivedAgentLogSourceByIssue scans the archive tree for a drained engineer
// run whose meta.json matches the given issue.
func findArchivedAgentLogSourceByIssue(ref agentIssueRef, tail int, follow bool, baseDir, consoleName string) (agentLogSource, error) {
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
		meta, ok := readRunMeta(metaPath)
		if !ok || meta.Repo != ref.repoSlug() || meta.Issue != strconv.Itoa(ref.Number) || !strings.HasPrefix(name, roleEngineer+"-") {
			continue
		}
		candidates = append(candidates, filepath.Join(baseDir, name))
	}
	switch len(candidates) {
	case 0:
		return agentLogSource{}, nil
	case 1:
		meta, _ := readRunMeta(filepath.Join(candidates[0], drainMetaFile))
		return agentLogSource{Kind: agentLogSourceFile, Path: filepath.Join(candidates[0], consoleName), ArchiveMeta: meta, Tail: tail, Follow: follow}, nil
	default:
		sort.Strings(candidates)
		return agentLogSource{}, fmt.Errorf("dispatch broker: %q matches %d drained engineer log directories (%s) - refusing to guess; read one by its container name",
			ref.String(), len(candidates), strings.Join(candidates, ", "))
	}
}

// findArchivedAgentLogSourceByName looks up one exact drained container archive.
func findArchivedAgentLogSourceByName(name string, tail int, follow bool, baseDir, consoleName string) (agentLogSource, error) {
	match := func(container string) bool {
		return container == name
	}
	return findArchivedAgentLogSource(name, baseDir, consoleName, tail, follow, match)
}

// findArchivedAgentLogSource scans one archive tree and returns the single matching
// source, preferring the raw console.log naming for the source line.
func findArchivedAgentLogSource(target, baseDir, consoleName string, tail int, follow bool, match func(string) bool) (agentLogSource, error) {
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
		candidates = append(candidates, filepath.Join(baseDir, name))
	}
	switch len(candidates) {
	case 0:
		return agentLogSource{}, nil
	case 1:
		meta, _ := readRunMeta(filepath.Join(candidates[0], drainMetaFile))
		return agentLogSource{Kind: agentLogSourceFile, Path: filepath.Join(candidates[0], consoleName), ArchiveMeta: meta, Tail: tail, Follow: follow}, nil
	default:
		sort.Strings(candidates)
		return agentLogSource{}, fmt.Errorf("dispatch broker: %q matches %d drained engineer log directories (%s) - refusing to guess; read one by its container name",
			target, len(candidates), strings.Join(candidates, ", "))
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
	return latestDispatchArtifactPathsIn(agentLogsRedactedDir(), dispatchArtifactRedactedConsole, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Ref == ref.String() || (meta.Repo == ref.repoSlug() && meta.Issue == strconv.Itoa(ref.Number))
	})
}

// latestDispatchArtifactPathsForRole resolves the newest broker dispatch artifact
// directory for a target role.
func latestDispatchArtifactPathsForRole(role string) (dispatchArtifactPaths, bool, error) {
	role = strings.TrimSpace(role)
	paths, ok, err := latestDispatchArtifactPathsIn(agentLogsDir(), dispatchArtifactConsoleFile, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Role == role || meta.RequesterRole == role
	})
	if err != nil || ok {
		return paths, ok, err
	}
	return latestDispatchArtifactPathsIn(agentLogsRedactedDir(), dispatchArtifactRedactedConsole, func(meta dispatchArtifactMeta, _ string) bool {
		return meta.Role == role || meta.RequesterRole == role
	})
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
		paths := dispatchArtifactPaths{
			Dir:         base,
			ConsolePath: filepath.Join(base, consoleName),
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
