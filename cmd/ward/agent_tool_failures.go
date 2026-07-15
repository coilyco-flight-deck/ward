package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	toolFailuresCacheApp  = "agentic-os"
	toolFailuresCacheTail = "tool-failures"
	toolFailuresSource    = "claude.transcript"
	toolFailuresSchema    = "tool_failure"
	toolFailuresDetail    = "tool result marked error"
	toolFailureExcerptCap = 240
)

const toolFailuresClaudeHarness = string(modeClaude)

type toolFailureRecord struct {
	Ts            int64  `json:"ts,omitempty"`
	Fingerprint   string `json:"fingerprint"`
	FailureClass  string `json:"failure_class"`
	Harness       string `json:"harness"`
	Repo          string `json:"repo"`
	Source        string `json:"source,omitempty"`
	SchemaTitle   string `json:"schema_title,omitempty"`
	Tool          string `json:"tool,omitempty"`
	Detail        string `json:"detail,omitempty"`
	StderrExcerpt string `json:"stderr_excerpt,omitempty"`
	Expected      bool   `json:"expected,omitempty"`
}

type claudeFailureTranscriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Message   struct {
		Content []claudeFailureTranscriptContent `json:"content"`
	} `json:"message"`
}

type claudeFailureTranscriptContent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

type pendingClaudeFailure struct {
	Tool      string
	Args      map[string]string
	StartedAt int64
}

func toolFailuresDir() string {
	if cache := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); cache != "" {
		return filepath.Join(cache, toolFailuresCacheApp, toolFailuresCacheTail)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".cache", toolFailuresCacheApp, toolFailuresCacheTail)
}

func toolFailureBufferPath(repo string) string {
	return filepath.Join(toolFailuresDir(), repoSlugFromFullSlug(repo)+".jsonl")
}

func repoSlugFromFullSlug(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "unknown"
	}
	if tail, ok := strings.CutSuffix(repo, ".git"); ok {
		repo = tail
	}
	if idx := strings.LastIndex(repo, "/"); idx >= 0 && idx+1 < len(repo) {
		return repo[idx+1:]
	}
	return repo
}

func (r *Runner) writeClaudeToolFailureRecords(container string, meta runMeta, transcript []byte) {
	records := extractClaudeToolFailureRecords(transcript, meta)
	if len(records) == 0 {
		return
	}
	path := toolFailureBufferPath(meta.Repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not create tool-failure buffer dir %s (%v)\n", container, filepath.Dir(path), err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container: drain %s: could not append tool-failure buffer %s (%v)\n", container, path, err)
		return
	}
	defer func() { _ = f.Close() }()
	for _, rec := range records {
		data, merr := json.Marshal(rec)
		if merr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: marshal tool-failure record (%v)\n", container, merr)
			continue
		}
		if _, werr := f.Write(append(data, '\n')); werr != nil {
			fmt.Fprintf(os.Stderr, "ward container: drain %s: write tool-failure buffer %s (%v)\n", container, path, werr)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "ward container: wrote %d tool-failure record(s) for %s -> %s\n", len(records), container, path)
}

func extractClaudeToolFailureRecords(transcript []byte, meta runMeta) []toolFailureRecord {
	if strings.TrimSpace(string(transcript)) == "" {
		return nil
	}
	var records []toolFailureRecord
	pendingByID := map[string]pendingClaudeFailure{}
	repo := repoSlugFromFullSlug(meta.Repo)
	if repo == "" {
		repo = "unknown"
	}
	for _, raw := range strings.Split(string(transcript), "\n") {
		line, ok := parseClaudeFailureTranscriptLine(raw)
		if !ok {
			continue
		}
		ts := parseTranscriptTime(line.Timestamp)
		for _, c := range line.Message.Content {
			recordClaudeToolFailureUse(pendingByID, c, ts)
			recordClaudeToolFailureResult(&records, pendingByID, c, repo, ts)
		}
	}
	return records
}

func parseClaudeFailureTranscriptLine(raw string) (claudeFailureTranscriptLine, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return claudeFailureTranscriptLine{}, false
	}
	var line claudeFailureTranscriptLine
	if err := json.Unmarshal([]byte(raw), &line); err != nil {
		return claudeFailureTranscriptLine{}, false
	}
	return line, true
}

func recordClaudeToolFailureUse(pendingByID map[string]pendingClaudeFailure, c claudeFailureTranscriptContent, ts int64) {
	if c.Type != "tool_use" || c.ID == "" {
		return
	}
	args, _ := sanitizeToolInput(c.Input, true)
	pendingByID[c.ID] = pendingClaudeFailure{
		Tool:      c.Name,
		Args:      args,
		StartedAt: ts,
	}
}

func recordClaudeToolFailureResult(records *[]toolFailureRecord, pendingByID map[string]pendingClaudeFailure, c claudeFailureTranscriptContent, repo string, ts int64) {
	if c.Type != "tool_result" || !c.IsError {
		return
	}
	pending, ok := pendingByID[c.ToolUseID]
	if !ok {
		return
	}
	delete(pendingByID, c.ToolUseID)
	excerpt := scrubToolFailureExcerpt(extractTranscriptExcerpt(c.Content))
	class := classifyClaudeToolFailure(pending.Tool, excerpt, pending.Args)
	*records = append(*records, toolFailureRecord{
		Ts:            firstNonZero(ts, pending.StartedAt),
		Fingerprint:   toolFailureFingerprint(repo, toolFailuresClaudeHarness, class, pending.Tool, pending.Args),
		FailureClass:  class,
		Harness:       toolFailuresClaudeHarness,
		Repo:          repo,
		Source:        toolFailuresSource,
		SchemaTitle:   toolFailuresSchema,
		Tool:          pending.Tool,
		Detail:        toolFailuresDetail,
		StderrExcerpt: excerpt,
	})
}

func firstNonZero(v, fallback int64) int64 {
	if v != 0 {
		return v
	}
	return fallback
}

func toolFailureFingerprint(repo, harness, failureClass, tool string, args map[string]string) string {
	h := sha256.New()
	writeFingerprintPart := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	writeFingerprintPart(repo)
	writeFingerprintPart(harness)
	writeFingerprintPart(failureClass)
	writeFingerprintPart(tool)
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeFingerprintPart(k)
		writeFingerprintPart(args[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func classifyClaudeToolFailure(tool, excerpt string, args map[string]string) string {
	hay := strings.ToLower(strings.Join([]string{tool, excerpt, args["command"], args["path"], args["file_path"]}, "\n"))
	switch {
	case strings.Contains(hay, "permission denied"), strings.Contains(hay, "operation not permitted"):
		return "permission_denied"
	case strings.Contains(hay, "no such file"), strings.Contains(hay, "not found"), strings.Contains(hay, "couldn't read"):
		return "missing_path"
	case strings.Contains(hay, "exit code"), strings.Contains(hay, "non-zero"), strings.Contains(hay, "returned 1"), strings.Contains(hay, "command failed"):
		return "nonzero_exit"
	case strings.TrimSpace(tool) == "":
		return "tool_error"
	default:
		if strings.EqualFold(tool, "Bash") && excerpt == "" {
			return "nonzero_exit"
		}
		return "tool_error"
	}
}

func extractTranscriptExcerpt(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	var parts []string
	collectTranscriptStrings(v, &parts)
	if len(parts) == 0 {
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	return strings.Join(parts, " ")
}

func collectTranscriptStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, item := range x {
			collectTranscriptStrings(item, out)
		}
	case map[string]any:
		for _, item := range x {
			collectTranscriptStrings(item, out)
		}
	}
}

func scrubToolFailureExcerpt(s string) string {
	s = redactSecrets(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= toolFailureExcerptCap {
		return s
	}
	return string(r[:toolFailureExcerptCap]) + "…"
}
