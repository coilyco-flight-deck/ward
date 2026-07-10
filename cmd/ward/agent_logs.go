package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
		return r.forwardAgentLogsToHostBroker(ctx, addr, arg, tail, follow)
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
		return agentLogSource{Kind: agentLogSourceDocker, Container: name, TranscriptTree: containerTranscriptDir, Tail: tail, Follow: follow}, nil
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
	return agentLogSource{}, fmt.Errorf("dispatch broker: no engineer log source matches %q", ref.String())
}

// resolveAgentLogsSourceForName resolves a container name to a live engineer
// container or a drained archive for that exact name.
func (r *Runner) resolveAgentLogsSourceForName(ctx context.Context, name string, tail int, follow bool) (agentLogSource, error) {
	if strings.TrimSpace(name) == "" {
		return agentLogSource{}, fmt.Errorf("container name is empty")
	}
	if r.containerPresent(ctx, name) {
		role, err := r.containerRoleLabel(ctx, name)
		if err != nil {
			return agentLogSource{}, fmt.Errorf("dispatch broker: refusing to read %q: could not read its %s label (%w) - "+
				"fail-closed, only %s containers are readable", name, labelRole, err, roleEngineer)
		}
		if err := stopTargetGuard(name, role); err != nil {
			return agentLogSource{}, err
		}
		return agentLogSource{Kind: agentLogSourceDocker, Container: name, TranscriptTree: containerTranscriptDir, Tail: tail, Follow: follow}, nil
	}
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
	return agentLogSource{}, fmt.Errorf("dispatch broker: no engineer log source matches %q", name)
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
	if len(bytes.TrimSpace(out)) == 0 && strings.TrimSpace(source.TranscriptTree) != "" {
		transcript := r.liveTranscriptSource(ctx, source.Container, source.TranscriptTree)
		if len(transcript) > 0 {
			if source.Tail > 0 {
				transcript = tailBytes(transcript, source.Tail)
			}
			_, _ = fmt.Fprintf(w, "ward agent logs: docker logs empty; using live transcript tree from %s\n", source.TranscriptTree)
			_, err = w.Write(transcript)
			return err
		}
		_, _ = fmt.Fprintf(w, "ward agent logs: docker logs empty; live transcript tree at %s is empty\n", source.TranscriptTree)
		return nil
	}
	_, err = w.Write(out)
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
		return agentLogSource{Kind: agentLogSourceFile, Path: filepath.Join(candidates[0], consoleName), Tail: tail, Follow: follow}, nil
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
		return agentLogSource{Kind: agentLogSourceFile, Path: filepath.Join(candidates[0], consoleName), Tail: tail, Follow: follow}, nil
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
