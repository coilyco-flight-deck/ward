package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// telemetryArgCap bounds a redacted arg's length so a pathological command can't
// blow up an indexed attribute (cardinality/size discipline, agent-observability.md).
const telemetryArgCap = 512

// redactionRules is the Warp custom_secret_regex_list ported to RE2 (lookahead-free,
// so verbatim). Source: the upstream Warp template. See docs.
var redactionRules = []*regexp.Regexp{
	// Credential-bearing HTTP headers and key/value responses. These rules
	// intentionally replace the complete field, including an arbitrary value.
	regexp.MustCompile(`(?i)\b(?:authorization|proxy-authorization|private-token|x-api-key|x-auth-token)[ \t]*[:=][ \t]*(?:(?:basic|bearer|token)[ \t]+)?[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(?:password|token|access[_-]?token|api[_-]?key|client[_-]?secret)[ \t]*=[ \t]*[^\s]+`),
	// Public IPv4 (excludes loopback / RFC1918 / link-local; CGNAT kept).
	regexp.MustCompile(`\b(?:(?:[1-9]|1[1-9]|[2-9]\d|1[01]\d|12[0-6]|12[89]|1[3-5]\d|16[0-8]|17[01]|17[3-9]|18\d|19[01]|19[3-9]|2[0-4]\d|25[0-5])\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)|169\.(?:255|25[0-3]|2[0-4]\d|1\d\d|[1-9]?\d)|172\.(?:25[0-5]|2[0-4]\d|1\d\d|3[2-9]|[4-9]\d|1[0-5]|[0-9])|192\.(?:25[0-5]|2[0-4]\d|1[7-9]\d|16[0-7]|1[0-5]\d|169|[1-9]?\d))(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){2}\b`),
	regexp.MustCompile(`\b((([0-9A-Fa-f]{1,4}:){1,6}:)|(([0-9A-Fa-f]{1,4}:){7}))([0-9A-Fa-f]{1,4})\b`), // IPv6
	regexp.MustCompile(`\bxapp-[0-9]+-[A-Za-z0-9_]+-[0-9]+-[a-f0-9]+\b`),                               // Slack App Token
	regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`),                                                   // Google API Key
	regexp.MustCompile(`\b[0-9]+-[0-9A-Za-z_]{32}\.apps\.googleusercontent\.com\b`),                    // Google OAuth ID
	regexp.MustCompile(`\bghp_[A-Za-z0-9_]{36}\b`),                                                     // GitHub classic PAT
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{82}\b`),                                              // GitHub fine-grained PAT
	regexp.MustCompile(`\bgho_[A-Za-z0-9_]{36}\b`),                                                     // GitHub OAuth
	regexp.MustCompile(`\bghu_[A-Za-z0-9_]{36}\b`),                                                     // GitHub user-to-server
	regexp.MustCompile(`\bghs_[A-Za-z0-9_]{36}\b`),                                                     // GitHub server-to-server
	regexp.MustCompile(`\b(?:r|s)k_(test|live)_[0-9a-zA-Z]{24}\b`),                                     // Stripe
	regexp.MustCompile(`\b([a-z0-9-]){1,30}(\.firebaseapp\.com)\b`),                                    // Firebase Auth Domain
	regexp.MustCompile(`\b(ey[a-zA-Z0-9_\-=]{10,}\.){2}[a-zA-Z0-9_\-=]{10,}\b`),                        // JWT
	regexp.MustCompile(`\bsk-ant-api\d{0,2}-[a-zA-Z0-9\-]{80,120}\b`),                                  // Anthropic API Key
	regexp.MustCompile(`\bsk-[a-zA-Z0-9]{48}\b`),                                                       // OpenAI API Key
	regexp.MustCompile(`\bsk-[a-zA-Z0-9\-]{10,100}\b`),                                                 // Generic SK API Key
	regexp.MustCompile(`\bfw_[a-zA-Z0-9]{24}\b`),                                                       // Fireworks API Key
}

const redactionPlaceholder = "[REDACTED]"

var redactionEnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// injectedCredentialEnvNames lists credential-bearing channels Ward recognizes
// by default. Operator-configured names extend it.
var injectedCredentialEnvNames = []string{
	"FORGEJO_TOKEN",
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"GITLAB_TOKEN",
	"WARD_GITHUB_TOKEN",
	"WARD_GITLAB_TOKEN",
	"GITLAB_ACCESS_TOKEN",
	"OAUTH_TOKEN",
	shortcutTokenEnv,
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"OPENAI_API_KEY",
	claudeCredsEnvKey,
	codexAuthEnvKey,
	gooseOllamaHostEnvKey,
	envDispatchBrokerToken,
	envChildBrokerCapability,
}

// secretRedactor combines built-ins, operator RE2 patterns, and exact values.
// Exact values are replaced longest-first to avoid leaving suffixes behind.
type secretRedactor struct {
	patterns []*regexp.Regexp
	literals []string
}

type redactingLineWriter struct {
	mu       sync.Mutex
	target   io.Writer
	redactor secretRedactor
	pending  []byte
}

func (w *redactingLineWriter) Write(body []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, body...)
	for {
		idx := bytes.IndexByte(w.pending, '\n')
		if idx < 0 {
			break
		}
		if _, err := io.WriteString(w.target, w.redactor.redact(string(w.pending[:idx+1]))); err != nil {
			return 0, err
		}
		w.pending = w.pending[idx+1:]
	}
	return len(body), nil
}

func (w *redactingLineWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return nil
	}
	_, err := io.WriteString(w.target, w.redactor.redact(string(w.pending)))
	w.pending = nil
	return err
}

func newSecretRedactor(exactValues, configuredPatterns []string) (secretRedactor, error) {
	patterns := append([]*regexp.Regexp(nil), redactionRules...)
	for i, pattern := range configuredPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return secretRedactor{}, fmt.Errorf("agent.redaction.patterns[%d] must not be empty", i)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return secretRedactor{}, fmt.Errorf("agent.redaction.patterns[%d]: invalid RE2 pattern: %w", i, err)
		}
		patterns = append(patterns, re)
	}
	seen := map[string]bool{}
	literals := make([]string, 0, len(exactValues))
	for _, value := range exactValues {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		literals = append(literals, value)
	}
	sort.Slice(literals, func(i, j int) bool {
		if len(literals[i]) == len(literals[j]) {
			return literals[i] < literals[j]
		}
		return len(literals[i]) > len(literals[j])
	})
	return secretRedactor{patterns: patterns, literals: literals}, nil
}

func validateRedactionConfig(envNames, patterns []string) error {
	for i, name := range envNames {
		name = strings.TrimSpace(name)
		if !redactionEnvNameRE.MatchString(name) {
			return fmt.Errorf("agent.redaction.env-names[%d] %q is not an environment variable name", i, name)
		}
	}
	_, err := newSecretRedactor(nil, patterns)
	return err
}

func loadWardGlobalConfigWithRedactionValidation() (wardGlobalConfig, error) {
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return wardGlobalConfig{}, err
	}
	if err := validateRedactionConfig(cfg.Agent.Redaction.EnvNames, cfg.Agent.Redaction.Patterns); err != nil {
		return wardGlobalConfig{}, err
	}
	return cfg, nil
}

func configuredSecretRedactor(env map[string]string) (secretRedactor, error) {
	cfg, err := loadWardGlobalConfigWithRedactionValidation()
	if err != nil {
		return secretRedactor{}, fmt.Errorf("read agent.redaction config: %w", err)
	}
	names := append([]string(nil), injectedCredentialEnvNames...)
	names = append(names, cfg.Agent.Redaction.EnvNames...)
	values := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		value := ""
		if env != nil {
			value = env[name]
		}
		if value == "" {
			value = os.Getenv(name)
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return newSecretRedactor(values, cfg.Agent.Redaction.Patterns)
}

func (r secretRedactor) redact(s string) string {
	for _, literal := range r.literals {
		s = strings.ReplaceAll(s, literal, redactionPlaceholder)
	}
	for _, re := range r.patterns {
		s = re.ReplaceAllString(s, redactionPlaceholder)
	}
	return s
}

// redactJSONStrings applies one redactor to every string in a JSON-shaped
// artifact without exposing raw encoded strings to regular-expression rewrites.
func redactJSONStrings(input, output any, r secretRedactor) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	value = redactJSONTree(value, r)
	body, err = json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, output)
}

func redactJSONTree(value any, r secretRedactor) any {
	switch typed := value.(type) {
	case string:
		return r.redact(typed)
	case []any:
		for i := range typed {
			typed[i] = redactJSONTree(typed[i], r)
		}
		return typed
	case map[string]any:
		for key, child := range typed {
			typed[key] = redactJSONTree(child, r)
		}
		return typed
	default:
		return value
	}
}

// redactSecrets scrubs every Warp-list secret shape from s. Applied to args
// before they enter an envelope - the last line before export.
func redactSecrets(s string) string {
	r, err := configuredSecretRedactor(nil)
	if err != nil {
		r, _ = newSecretRedactor(nil, nil)
	}
	return r.redact(s)
}

// redactConsole scrubs known secret shapes from a drained console (ward#526): the
// redacted-at-rest console view, reusing the extractor's redactSecrets. No reflow.
func redactConsole(console []byte) []byte {
	r, _ := newSecretRedactor(nil, nil)
	return redactConsoleWith(console, r)
}

func redactConsoleWith(console []byte, r secretRedactor) []byte {
	if len(console) == 0 {
		return nil
	}
	return []byte(r.redact(string(console)))
}

// redactedTranscript renders a drained transcript as one JSON envelope per line
// via the shared redaction path used by the local redacted archive (ward#526).
func redactedTranscript(transcript []byte) []byte {
	r, _ := newSecretRedactor(nil, nil)
	return redactedTranscriptWith(transcript, r)
}

func redactedTranscriptWith(transcript []byte, r secretRedactor) []byte {
	envs := extractEnvelopesWithRedactor(transcript, true, r)
	if len(envs) == 0 {
		return nil
	}
	var out bytes.Buffer
	for _, e := range envs {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// bodyArgKeys are body-shaped tool inputs (file contents, edit payloads): dropped
// from envelopes outright, never redacted-and-kept (docs/agent-observability.md).
var bodyArgKeys = map[string]bool{
	"content":      true,
	"description":  true,
	"instructions": true,
	"message":      true,
	"new_string":   true,
	"old_string":   true,
	"new_str":      true,
	"old_str":      true,
	"patch":        true,
	"prompt":       true,
	"file_text":    true,
	"text":         true,
	"body":         true,
}

// fileArgKeys are input fields naming a path the tool touched.
var fileArgKeys = []string{"file_path", "path", "notebook_path"}

// toolEnvelope is one tool call reduced to call-metadata: name, redacted args,
// cwd, duration, pass/fail, lifecycle step, files touched. No result, no body.
type toolEnvelope struct {
	Tool         string            `json:"tool"`
	Args         map[string]string `json:"args"`
	Cwd          string            `json:"cwd,omitempty"`
	DurationMs   int64             `json:"duration_ms,omitempty"`
	Outcome      string            `json:"outcome"`
	Lifecycle    string            `json:"lifecycle"`
	Files        []string          `json:"files,omitempty"`
	TimeUnixNano int64             `json:"-"`
}

// lifecycle steps a tool call maps to (the run's coarse phase).
const (
	lifecycleClone     = "clone"
	lifecycleMerge     = "merge"
	lifecyclePush      = "push"
	lifecycleImplement = "implement"
)

// transcriptLine is the tolerant shape the extractor reads from each jsonl event;
// unknown fields are ignored, so it survives claude transcript-format drift.
type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Message   struct {
		Content []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
		} `json:"content"`
	} `json:"message"`
}

// extractEnvelopes turns a drained transcript into one envelope per tool call.
func extractEnvelopes(transcript []byte, redact bool) []toolEnvelope { //nolint:gocognit,gocyclo,cyclop
	r, _ := newSecretRedactor(nil, nil)
	return extractEnvelopesWithRedactor(transcript, redact, r)
}

func extractEnvelopesWithRedactor(transcript []byte, redact bool, r secretRedactor) []toolEnvelope { //nolint:gocognit,gocyclo,cyclop
	var envelopes []toolEnvelope
	byID := map[string]int{} // tool_use id -> index in envelopes
	for _, raw := range bytes.Split(transcript, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line transcriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		ts := parseTranscriptTime(line.Timestamp)
		for _, c := range line.Message.Content {
			switch c.Type {
			case "tool_use":
				env := toolEnvelope{
					Tool:         c.Name,
					Cwd:          line.Cwd,
					Outcome:      "success", // until a matching error result flips it
					TimeUnixNano: ts,
				}
				env.Args, env.Files = sanitizeToolInputWithRedactor(c.Input, redact, r)
				if redact {
					env.Tool = r.redact(env.Tool)
					env.Cwd = r.redact(env.Cwd)
				}
				env.Lifecycle = classifyLifecycle(c.Name, env.Args)
				byID[c.ID] = len(envelopes)
				envelopes = append(envelopes, env)
			case "tool_result":
				idx, ok := byID[c.ToolUseID]
				if !ok {
					continue
				}
				if c.IsError {
					envelopes[idx].Outcome = "failure"
				}
				if start := envelopes[idx].TimeUnixNano; start > 0 && ts > start {
					envelopes[idx].DurationMs = (ts - start) / 1e6
				}
			}
		}
	}
	return envelopes
}

// sanitizeToolInputWithRedactor splits a tool's input into scalar args and touched files.
func sanitizeToolInputWithRedactor(input json.RawMessage, redact bool, r secretRedactor) (map[string]string, []string) { //nolint:gocyclo,cyclop
	args := map[string]string{}
	var files []string
	if len(input) == 0 {
		return args, files
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return args, files
	}
	for _, k := range fileArgKeys {
		if p, ok := sanitizedToolScalar(fields[k], redact, r); ok && p != "" {
			files = append(files, p)
		}
	}
	for k, raw := range fields {
		if redact && bodyArgKeys[k] {
			continue // rule 1: off-box, bodies never enter an envelope
		}
		s, ok := sanitizedToolScalar(raw, redact, r)
		if !ok {
			continue // non-scalar (object/array) args are skipped, not flattened
		}
		args[k] = s
	}
	return args, files
}

func sanitizedToolScalar(raw json.RawMessage, redact bool, r secretRedactor) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	if redact {
		value = r.redact(value)
		if len(value) > telemetryArgCap {
			value = value[:telemetryArgCap] + "…"
		}
	}
	return value, true
}

// classifyLifecycle maps a tool call to the run's coarse phase from its command
// or args (git push -> push, merge/rebase -> merge, clone -> clone, else implement).
func classifyLifecycle(_ string, args map[string]string) string {
	cmd := strings.ToLower(args["command"])
	switch {
	case strings.Contains(cmd, "git push"):
		return lifecyclePush
	case strings.Contains(cmd, "git merge"), strings.Contains(cmd, "git rebase"):
		return lifecycleMerge
	case strings.Contains(cmd, "git clone"):
		return lifecycleClone
	default:
		return lifecycleImplement
	}
}

// parseTranscriptTime parses an RFC3339 transcript timestamp to unix nanos; an
// empty or unparseable stamp yields 0 (duration then stays unset).
func parseTranscriptTime(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.UnixNano()
}
