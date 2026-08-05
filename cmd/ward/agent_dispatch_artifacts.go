package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
)

const (
	dispatchArtifactsSubdir         = "dispatch"
	dispatchArtifactConsoleFile     = "console.log"
	dispatchArtifactRedactedConsole = "console.redacted.log"
	dispatchArtifactMetaFile        = "meta.json"
	dispatchArtifactSummaryFile     = "summary.md"
)

// dispatchArtifactPaths describes the one secret-safe artifact tree for a
// broker dispatch request.
type dispatchArtifactPaths struct {
	RequestID     string
	Dir           string
	ConsolePath   string
	MetaPath      string
	SummaryPath   string
	CreatedAt     time.Time
	RequesterRole string
	RequesterMode containerMode
	TargetRole    string
	TargetRef     string
	Repo          string
	Issue         string
	Harness       string
	WardVersion   string
}

// dispatchArtifactMeta is the safe join record written beside the console log.
type dispatchArtifactMeta struct {
	RequestID     string `json:"request_id"`
	Action        string `json:"action"`
	Requester     string `json:"requester,omitempty"`
	RequesterRole string `json:"requester_role,omitempty"`
	RequesterMode string `json:"requester_mode,omitempty"`
	Role          string `json:"role"`
	Ref           string `json:"ref,omitempty"`
	Repo          string `json:"repo,omitempty"`
	Issue         string `json:"issue,omitempty"`
	Harness       string `json:"harness,omitempty"`
	WardVersion   string `json:"ward_version,omitempty"`
	CreatedAt     string `json:"created_at"`
	CompletedAt   string `json:"completed_at,omitempty"`
	Outcome       string `json:"outcome"`
	ErrorClass    string `json:"error_class,omitempty"`
	Error         string `json:"error,omitempty"`
	RedactedArgv  string `json:"redacted_argv,omitempty"`
	LogPath       string `json:"log_path,omitempty"`
	SummaryPath   string `json:"summary_path,omitempty"`
}

func newDispatchArtifactPaths(req dispatchBrokerRequest, now time.Time, requestID string) dispatchArtifactPaths {
	requester := strings.TrimSpace(req.Requester)
	requesterRole := containerNameRole(requester)
	requesterMode := containerModeFromContainerName(requester)
	harness := dispatchBrokerRequestHarness(req)
	wardVersion := dispatchBrokerWardVersion(req.Argv)
	slug := config.SanitizeSlug(emptyDefault(requester, "unknown") + "-" + emptyDefault(argRef(req.Argv), "missing-ref"))
	if requestID == "" {
		requestID = newDispatchBrokerRequestID()
	}
	name := fmt.Sprintf("%s-%s", requestID, slug)
	dir := filepath.Join(agentLogsDir(), dispatchArtifactsSubdir, name)
	return dispatchArtifactPaths{
		RequestID:     requestID,
		Dir:           dir,
		ConsolePath:   filepath.Join(dir, dispatchArtifactConsoleFile),
		MetaPath:      filepath.Join(dir, dispatchArtifactMetaFile),
		SummaryPath:   filepath.Join(dir, dispatchArtifactSummaryFile),
		CreatedAt:     now.UTC(),
		RequesterRole: requesterRole,
		RequesterMode: requesterMode,
		TargetRole:    req.Role,
		TargetRef:     emptyDefault(argRef(req.Argv), ""),
		Repo:          emptyDefault(argRepo(req.Argv), ""),
		Issue:         emptyDefault(argIssue(req.Argv), ""),
		Harness:       harness,
		WardVersion:   wardVersion,
	}
}

type dispatchArtifactLog struct {
	mu       sync.Mutex
	file     *os.File
	redactor secretRedactor
	pending  []byte
	err      error
}

func (l *dispatchArtifactLog) Write(body []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return 0, l.err
	}
	l.pending = append(l.pending, body...)
	for {
		idx := bytes.IndexByte(l.pending, '\n')
		if idx < 0 {
			break
		}
		line := l.redactor.redact(string(l.pending[:idx+1]))
		if _, err := l.file.WriteString(line); err != nil {
			l.err = err
			return 0, err
		}
		l.pending = l.pending[idx+1:]
	}
	return len(body), nil
}

func (l *dispatchArtifactLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) > 0 && l.err == nil {
		_, l.err = l.file.WriteString(l.redactor.redact(string(l.pending)))
		l.pending = nil
	}
	closeErr := l.file.Close()
	if l.err != nil {
		return l.err
	}
	return closeErr
}

func openDispatchArtifactLog(path string) (*dispatchArtifactLog, error) {
	redactor, err := configuredSecretRedactor(nil)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- Ward-derived safe archive path
	if err != nil {
		return nil, err
	}
	return &dispatchArtifactLog{file: file, redactor: redactor}, nil
}

func openDispatchArtifact(req dispatchBrokerRequest, now time.Time, requestID string) (dispatchArtifactPaths, *dispatchArtifactLog, error) {
	paths := newDispatchArtifactPaths(req, now, requestID)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return dispatchArtifactPaths{}, nil, err
	}
	logf, err := openDispatchArtifactLog(paths.ConsolePath)
	if err != nil {
		return dispatchArtifactPaths{}, nil, err
	}
	return paths, logf, nil
}

func writeDispatchArtifactInitial(paths dispatchArtifactPaths, req dispatchBrokerRequest) {
	meta := dispatchArtifactMeta{
		RequestID:     paths.RequestID,
		Action:        dispatchAction(req.Action),
		Requester:     strings.TrimSpace(req.Requester),
		RequesterRole: paths.RequesterRole,
		RequesterMode: string(paths.RequesterMode),
		Role:          paths.TargetRole,
		Ref:           paths.TargetRef,
		Repo:          paths.Repo,
		Issue:         paths.Issue,
		Harness:       paths.Harness,
		WardVersion:   paths.WardVersion,
		CreatedAt:     paths.CreatedAt.Format(time.RFC3339Nano),
		Outcome:       "in-progress",
		RedactedArgv:  redactDispatchBrokerArgv(req.Argv),
	}
	writeDispatchArtifactJSON(paths.MetaPath, meta)
	writeDispatchArtifactSummary(paths.SummaryPath, summarizeDispatchArtifact(meta, ""))
}

func finalizeDispatchArtifact(paths dispatchArtifactPaths, req dispatchBrokerRequest, logPath string, launchErr error) {
	meta := dispatchArtifactMeta{
		RequestID:     paths.RequestID,
		Action:        dispatchAction(req.Action),
		Requester:     strings.TrimSpace(req.Requester),
		RequesterRole: paths.RequesterRole,
		RequesterMode: string(paths.RequesterMode),
		Role:          paths.TargetRole,
		Ref:           paths.TargetRef,
		Repo:          paths.Repo,
		Issue:         paths.Issue,
		Harness:       paths.Harness,
		WardVersion:   paths.WardVersion,
		CreatedAt:     paths.CreatedAt.Format(time.RFC3339Nano),
		CompletedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Outcome:       dispatchArtifactOutcome(launchErr),
		RedactedArgv:  redactDispatchBrokerArgv(req.Argv),
		LogPath:       logPath,
		SummaryPath:   paths.SummaryPath,
	}
	if launchErr != nil {
		meta.ErrorClass = dispatchArtifactErrorClass(launchErr)
		redactor, _ := configuredSecretRedactor(nil)
		meta.Error = redactor.redact(firstLine(launchErr.Error()))
	}
	tail := dispatchArtifactTail(logPath)
	writeDispatchArtifactJSON(paths.MetaPath, meta)
	writeDispatchArtifactSummary(paths.SummaryPath, summarizeDispatchArtifact(meta, tail))
}

func writeDispatchArtifactJSON(path string, meta dispatchArtifactMeta) {
	redactor, err := configuredSecretRedactor(nil)
	if err != nil {
		return
	}
	var safe dispatchArtifactMeta
	if err := redactJSONStrings(meta, &safe, redactor); err != nil {
		return
	}
	if data, err := json.MarshalIndent(safe, "", "  "); err == nil {
		_ = os.WriteFile(path, append(data, '\n'), 0o600) // #nosec G306 -- ward-derived path under ~/.ward
	}
}

func writeDispatchArtifactSummary(path, summary string) {
	redactor, err := configuredSecretRedactor(nil)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(redactor.redact(summary)), 0o600) // #nosec G306 -- ward-derived path under ~/.ward
}

func summarizeDispatchArtifact(meta dispatchArtifactMeta, tail string) string {
	var b strings.Builder
	for _, field := range []struct{ key, value string }{
		{"request_id", meta.RequestID},
		{"action", meta.Action},
		{"requester", meta.Requester},
		{"requester_role", meta.RequesterRole},
		{"requester_mode", meta.RequesterMode},
		{"target_role", meta.Role},
		{"ref", meta.Ref},
		{"repo", meta.Repo},
		{"issue", meta.Issue},
		{"harness", meta.Harness},
		{"ward_version", meta.WardVersion},
		{"created_at", meta.CreatedAt},
		{"completed_at", meta.CompletedAt},
		{"outcome", meta.Outcome},
		{"error_class", meta.ErrorClass},
		{"error", meta.Error},
		{"argv", meta.RedactedArgv},
		{"log_path", meta.LogPath},
	} {
		appendDispatchSummaryLine(&b, field.key, field.value)
	}
	appendDispatchSummaryBlock(&b, "output_tail", tail)
	return b.String()
}

func appendDispatchSummaryLine(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, value)
}

func appendDispatchSummaryBlock(b *strings.Builder, heading, body string) {
	if trimmed := strings.TrimSpace(body); trimmed != "" {
		if heading != "" {
			b.WriteString("\n")
			b.WriteString(heading)
			b.WriteString(":\n")
		} else {
			b.WriteString("\n")
		}
		b.WriteString(trimmed)
		if !strings.HasSuffix(trimmed, "\n") {
			b.WriteByte('\n')
		}
	}
}

func dispatchArtifactTail(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	b, err := os.ReadFile(path) // #nosec G304 -- ward-derived path under ~/.ward
	if err != nil {
		return ""
	}
	redactor, _ := configuredSecretRedactor(nil)
	return redactor.redact(string(tailBytes(b, 200)))
}

func dispatchArtifactOutcome(err error) string {
	if err == nil {
		return "launched"
	}
	switch {
	case isOpenPRBackpressureError(err):
		return "deferred-open-pr"
	case isReleaseAssetsNotReadyError(err):
		return "deferred-release-assets-not-ready"
	case isReservationConflict(err):
		return "deferred-reservation-conflict"
	case isEngineerCapacityError(err):
		return "deferred-capacity"
	case isPartialLaunchError(err):
		return "partial-launch"
	default:
		return "failed-before-container"
	}
}

func dispatchArtifactErrorClass(err error) string {
	switch {
	case isOpenPRBackpressureError(err):
		return "open-pr-backpressure"
	case isReleaseAssetsNotReadyError(err):
		return "release-assets-not-ready"
	case isReservationConflict(err):
		return "reservation-conflict"
	case isEngineerCapacityError(err):
		return "engineer-capacity"
	case isPartialLaunchError(err):
		return "partial-launch"
	default:
		return "launch-failure"
	}
}

func dispatchArtifactIndexableRole(role string) string {
	switch strings.TrimSpace(role) {
	case roleEngineer, roleQA, roleDirector:
		return strings.TrimSpace(role)
	default:
		return ""
	}
}

func containerNameRole(name string) string {
	switch parts := strings.SplitN(strings.TrimSpace(name), "-", 2); len(parts) {
	case 0:
		return ""
	case 1:
		return dispatchArtifactIndexableRole(parts[0])
	default:
		return dispatchArtifactIndexableRole(parts[0])
	}
}

func dispatchBrokerRequestHarness(req dispatchBrokerRequest) string {
	for i := 0; i+1 < len(req.Argv); i++ {
		switch req.Argv[i] {
		case "--harness", "--agent":
			return strings.TrimSpace(req.Argv[i+1])
		}
	}
	return ""
}

func argRef(argv []string) string {
	if len(argv) >= 2 {
		return strings.TrimSpace(argv[1])
	}
	return ""
}

func argRepo(argv []string) string {
	if ref, err := parseAgentIssueRef(argRef(argv)); err == nil {
		return ref.repoSlug()
	}
	return ""
}

func argIssue(argv []string) string {
	if ref, err := parseAgentIssueRef(argRef(argv)); err == nil {
		return fmt.Sprintf("%d", ref.Number)
	}
	return ""
}

func newDispatchBrokerRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

// openDispatchLog preserves the old test seam while the artifact now lives in a
// directory-backed dispatch tree.
func openDispatchLog(req dispatchBrokerRequest, now time.Time) (*dispatchArtifactLog, string, error) {
	paths, logf, err := openDispatchArtifact(req, now, newDispatchBrokerRequestID())
	if err != nil {
		return nil, "", err
	}
	return logf, paths.ConsolePath, nil
}

// dispatchLogName preserves the old basename helper for tests and lookups that
// still refer to the historic flat filename shape.
func dispatchLogName(req dispatchBrokerRequest, now time.Time) string {
	ref := ""
	if len(req.Argv) >= 2 {
		ref = req.Argv[1]
	}
	slug := config.SanitizeSlug(emptyDefault(req.Requester, "unknown") + "-" + ref)
	return fmt.Sprintf("%s-%s.log", now.UTC().Format("20060102T150405Z"), slug)
}
