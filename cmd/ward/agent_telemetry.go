package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// telemetryArgCap bounds a redacted arg's length so a pathological command can't
// blow up an indexed attribute (cardinality/size discipline, log-schema.md).
const telemetryArgCap = 512

// redactionRules is the Warp custom_secret_regex_list ported to RE2 (lookahead-free,
// so verbatim). Source: agentic-os/warp/templates/settings.toml.tmpl. See docs.
var redactionRules = []*regexp.Regexp{
	// Public IPv4 (excludes loopback / RFC1918 / link-local; CGNAT kept).
	regexp.MustCompile(`\b(?:(?:[1-9]|1[1-9]|[2-9]\d|1[01]\d|12[0-6]|12[89]|1[3-5]\d|16[0-8]|17[01]|17[3-9]|18\d|19[01]|19[3-9]|2[0-4]\d|25[0-5])\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)|169\.(?:255|25[0-3]|2[0-4]\d|1\d\d|[1-9]?\d)|172\.(?:25[0-5]|2[0-4]\d|1\d\d|3[2-9]|[4-9]\d|1[0-5]|[0-9])|192\.(?:25[0-5]|2[0-4]\d|1[7-9]\d|16[0-7]|1[0-5]\d|169|[1-9]?\d))(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){2}\b`),
	regexp.MustCompile(`\b((([0-9A-Fa-f]{1,4}:){1,6}:)|(([0-9A-Fa-f]{1,4}:){7}))([0-9A-Fa-f]{1,4})\b`), // IPv6
	regexp.MustCompile(`\bxapp-[0-9]+-[A-Za-z0-9_]+-[0-9]+-[a-f0-9]+\b`),                               // Slack App Token
	regexp.MustCompile(`\b(AKIA|A3T|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{12,}\b`),               // AWS Access ID
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

// redactSecrets scrubs every Warp-list secret shape from s. Applied to args
// before they enter an envelope - the last line before export.
func redactSecrets(s string) string {
	for _, re := range redactionRules {
		s = re.ReplaceAllString(s, redactionPlaceholder)
	}
	return s
}

// redactConsole scrubs known secret shapes from a drained console (ward#526): the
// redacted-at-rest console view, reusing the extractor's redactSecrets. No reflow.
func redactConsole(console []byte) []byte {
	if len(console) == 0 {
		return nil
	}
	return []byte(redactSecrets(string(console)))
}

// redactedTranscript renders a drained transcript as one JSON envelope per line
// via the shared redaction path used by the local redacted archive (ward#526).
func redactedTranscript(transcript []byte) []byte {
	envs := extractEnvelopes(transcript, true)
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
	"content":    true,
	"new_string": true,
	"old_string": true,
	"new_str":    true,
	"old_str":    true,
	"file_text":  true,
	"text":       true,
	"body":       true,
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
				env.Args, env.Files = sanitizeToolInput(c.Input, redact)
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

// sanitizeToolInput splits a tool's input into scalar args and touched files.
func sanitizeToolInput(input json.RawMessage, redact bool) (map[string]string, []string) { //nolint:gocyclo,cyclop
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
		if raw, ok := fields[k]; ok {
			var p string
			if json.Unmarshal(raw, &p) == nil && p != "" {
				files = append(files, p)
			}
		}
	}
	for k, raw := range fields {
		if redact && bodyArgKeys[k] {
			continue // rule 1: off-box, bodies never enter an envelope
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue // non-scalar (object/array) args are skipped, not flattened
		}
		if redact {
			s = redactSecrets(s)
			if len(s) > telemetryArgCap {
				s = s[:telemetryArgCap] + "…"
			}
		}
		args[k] = s
	}
	return args, files
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
