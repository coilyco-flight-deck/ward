package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// agent_telemetry.go is slice 2 of agent-run observability (ward#363): a drained
// transcript -> one redacted envelope per tool call -> SigNoz OTLP logs.

// Redaction is enforced here, upstream of the sink: bodies dropped, args scrubbed
// through the Warp regex list. Export defaults OFF. See docs/agent-observability.md.

// envTelemetryEnabled is the opt-in gate. Unset or anything but "1" keeps the
// OTLP export OFF (the always-on host drain is unaffected).
const envTelemetryEnabled = "WARD_AGENT_TELEMETRY"

// envTelemetryEndpoint overrides the OTLP/HTTP logs endpoint. The default is the
// cross-cluster ser8 collector (the drain runs host-side, off-cluster); ward#363.
const envTelemetryEndpoint = "WARD_AGENT_TELEMETRY_ENDPOINT"

const defaultTelemetryEndpoint = "http://ser8:4318/v1/logs"

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

// extractEnvelopes parses a drained transcript into one envelope per tool call.
// Pure (testable); a tool_result only sets the matched call's pass/fail+duration.
func extractEnvelopes(transcript []byte) []toolEnvelope {
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
				env.Args, env.Files = sanitizeToolInput(c.Input)
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

// sanitizeToolInput splits a tool's input into redacted scalar args (bodies dropped)
// and the files it touched; every kept value is secret-redacted + length-capped.
func sanitizeToolInput(input json.RawMessage) (map[string]string, []string) {
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
		if bodyArgKeys[k] {
			continue // rule 1: bodies never enter an envelope
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue // non-scalar (object/array) args are skipped, not flattened
		}
		s = redactSecrets(s)
		if len(s) > telemetryArgCap {
			s = s[:telemetryArgCap] + "…"
		}
		args[k] = s
	}
	return args, files
}

// classifyLifecycle maps a tool call to the run's coarse phase from its command
// or args (git push -> push, merge/rebase -> merge, clone -> clone, else implement).
func classifyLifecycle(tool string, args map[string]string) string {
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

// maybeShipTelemetry ships the redacted envelopes to SigNoz only when
// WARD_AGENT_TELEMETRY=1 (default-OFF); best-effort, a failure never aborts the sweep.
func (r *Runner) maybeShipTelemetry(ctx context.Context, transcript []byte, meta runMeta) {
	if os.Getenv(envTelemetryEnabled) != "1" {
		return
	}
	if len(transcript) == 0 {
		return
	}
	envelopes := extractEnvelopes(transcript)
	if len(envelopes) == 0 {
		return
	}
	endpoint := os.Getenv(envTelemetryEndpoint)
	if endpoint == "" {
		endpoint = defaultTelemetryEndpoint
	}
	if err := shipEnvelopes(ctx, endpoint, envelopes, meta); err != nil {
		fmt.Fprintf(os.Stderr, "ward container: telemetry export to %s failed (%v); host drain is unaffected\n", endpoint, err)
		return
	}
	fmt.Fprintf(os.Stderr, "ward container: exported %d redacted tool envelope(s) for %s to %s\n", len(envelopes), meta.Container, endpoint)
}

// shipEnvelopes POSTs the OTLP/HTTP logs payload to the collector. Bounded
// timeout; a non-2xx is an error so the caller can log it.
func shipEnvelopes(ctx context.Context, endpoint string, envelopes []toolEnvelope, meta runMeta) error {
	payload, err := otlpLogsPayload(envelopes, meta)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("collector returned %s", resp.Status)
	}
	return nil
}

// --- OTLP/HTTP logs JSON (one logRecord per envelope) ------------------------

type otlpAttr struct {
	Key   string        `json:"key"`
	Value otlpAttrValue `json:"value"`
}

type otlpAttrValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *int64  `json:"intValue,omitempty"`
}

func otlpStr(k, v string) otlpAttr { return otlpAttr{Key: k, Value: otlpAttrValue{StringValue: &v}} }
func otlpInt(k string, v int64) otlpAttr {
	return otlpAttr{Key: k, Value: otlpAttrValue{IntValue: &v}}
}

// otlpLogsPayload renders the envelopes as an OTLP/HTTP logs request: bounded
// names become attributes, unbounded ids stay in the body (log-schema.md). Pure.
func otlpLogsPayload(envelopes []toolEnvelope, meta runMeta) ([]byte, error) {
	resourceAttrs := []otlpAttr{otlpStr("service.name", "ward-agent")}
	if meta.Repo != "" {
		resourceAttrs = append(resourceAttrs, otlpStr("repo", meta.Repo))
	}
	if meta.Driver != "" {
		resourceAttrs = append(resourceAttrs, otlpStr("actor", meta.Driver))
	}
	if meta.Container != "" {
		resourceAttrs = append(resourceAttrs, otlpStr("container", meta.Container))
	}

	records := make([]map[string]any, 0, len(envelopes))
	for _, e := range envelopes {
		attrs := []otlpAttr{
			otlpStr("verb", e.Tool),
			otlpStr("outcome", e.Outcome),
			otlpStr("lifecycle", e.Lifecycle),
		}
		if e.DurationMs > 0 {
			attrs = append(attrs, otlpInt("duration_ms", e.DurationMs))
		}
		if meta.Issue != "" {
			attrs = append(attrs, otlpStr("issue", meta.Issue))
		}
		rec := map[string]any{
			"severityText": "info",
			"body":         map[string]any{"stringValue": envelopeBody(e)},
			"attributes":   attrs,
		}
		if e.TimeUnixNano > 0 {
			rec["timeUnixNano"] = strconv.FormatInt(e.TimeUnixNano, 10)
		}
		records = append(records, rec)
	}

	doc := map[string]any{
		"resourceLogs": []map[string]any{{
			"resource":  map[string]any{"attributes": resourceAttrs},
			"scopeLogs": []map[string]any{{"logRecords": records}},
		}},
	}
	return json.Marshal(doc)
}

// envelopeBody renders the unbounded, human-readable line kept in the log body
// (the redacted args + touched files - never an indexed attribute; ward#363).
func envelopeBody(e toolEnvelope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] %s", e.Tool, e.Lifecycle, e.Outcome)
	if e.Cwd != "" {
		fmt.Fprintf(&b, " cwd=%s", e.Cwd)
	}
	for _, k := range sortedArgKeys(e.Args) {
		fmt.Fprintf(&b, " %s=%q", k, e.Args[k])
	}
	if len(e.Files) > 0 {
		fmt.Fprintf(&b, " files=%s", strings.Join(e.Files, ","))
	}
	return b.String()
}

// sortedArgKeys keeps the body rendering deterministic.
func sortedArgKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
