package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const skillUsageSchemaVersion = 1

// skillUsageArtifact is secret-free run telemetry written beside meta.json;
// it excludes prompts, inputs, commands, and results. See docs/agent-observability.md.
type skillUsageArtifact struct {
	SchemaVersion int               `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Container     string            `json:"container"`
	Role          string            `json:"role"`
	Harness       string            `json:"harness"`
	Repo          string            `json:"repo"`
	IssueRef      string            `json:"issue_ref"`
	Workflow      string            `json:"workflow"`
	WardVersion   string            `json:"ward_version"`
	Skills        []skillUsageEntry `json:"skills"`
}

type skillUsageEntry struct {
	SkillName string `json:"skill_name"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

type skillUsageEvent struct {
	Name string
	Seen time.Time
}

// skillUsageTranscriptLine tolerates current Claude and Codex formats; unknown
// fields stay ignored so format growth cannot copy bodies into the artifact.
type skillUsageTranscriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Payload struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"payload"`
}

var (
	// Codex skill use is observable as execution input reading skills/<name>/SKILL.md;
	// matching is confined to tool-call inputs, never messages or tool outputs.
	skillFilePathRE = regexp.MustCompile("(?i)(?:^|[/\\\\])skills[/\\\\][^[:space:]\"'`]+[/\\\\]SKILL\\.md")
	skillNameRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

func buildSkillUsageArtifact(meta runMeta, transcript []byte) *skillUsageArtifact {
	events := extractSkillUsageEvents(transcript)
	if len(events) == 0 {
		return nil
	}
	env := meta.env
	return &skillUsageArtifact{
		SchemaVersion: skillUsageSchemaVersion,
		RunID:         firstNonEmpty(strings.TrimSpace(env["WARD_RUN_ID"]), strings.TrimSpace(meta.Container)),
		Container:     strings.TrimSpace(meta.Container),
		Role:          firstNonEmpty(strings.TrimSpace(env["WARD_ROLE"]), roleFromContainerName(meta.Container)),
		Harness:       skillUsageHarness(meta),
		Repo:          strings.TrimSpace(meta.Repo),
		IssueRef:      skillUsageIssueRef(meta),
		Workflow:      skillUsageWorkflow(meta),
		WardVersion:   strings.TrimSpace(env["WARD_VERSION"]),
		Skills:        aggregateSkillUsage(events),
	}
}

func aggregateSkillUsage(events []skillUsageEvent) []skillUsageEntry {
	type aggregate struct {
		count int
		first time.Time
		last  time.Time
	}
	byName := make(map[string]aggregate, len(events))
	for _, event := range events {
		agg := byName[event.Name]
		agg.count++
		if !event.Seen.IsZero() {
			if agg.first.IsZero() || event.Seen.Before(agg.first) {
				agg.first = event.Seen
			}
			if agg.last.IsZero() || event.Seen.After(agg.last) {
				agg.last = event.Seen
			}
		}
		byName[event.Name] = agg
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	skills := make([]skillUsageEntry, 0, len(names))
	for _, name := range names {
		agg := byName[name]
		skills = append(skills, skillUsageEntry{
			SkillName: name,
			Count:     agg.count,
			FirstSeen: formatSummaryTime(agg.first),
			LastSeen:  formatSummaryTime(agg.last),
		})
	}
	return skills
}

func skillUsageIssueRef(meta runMeta) string {
	env := meta.env
	issueRef := strings.TrimSpace(env["WARD_ISSUE_REF"])
	if issueRef == "" && strings.TrimSpace(meta.Repo) != "" && strings.TrimSpace(meta.Issue) != "" {
		issueRef = strings.TrimSpace(meta.Repo) + "#" + strings.TrimSpace(meta.Issue)
	}
	return issueRef
}

func skillUsageWorkflow(meta runMeta) string {
	env := meta.env
	workflow := firstNonEmpty(strings.TrimSpace(env["WARD_WORKFLOW"]), strings.TrimSpace(meta.Summary.Workflow))
	if workflow == "" {
		workflow = string(workflowDirectToMain)
	}
	return workflow
}

func skillUsageHarness(meta runMeta) string {
	env := meta.env
	return firstNonEmptyList(
		strings.TrimSpace(env["WARD_HARNESS"]),
		strings.TrimSpace(env["WARD_MODE"]),
		strings.TrimSpace(meta.Driver),
		string(containerModeFromContainerName(meta.Container)),
	)
}

func extractSkillUsageEvents(transcript []byte) []skillUsageEvent {
	var events []skillUsageEvent
	for _, raw := range bytes.Split(transcript, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line skillUsageTranscriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		seen := parseSummaryTime(line.Timestamp)
		events = append(events, claudeSkillUsageEvents(line, seen)...)
		events = append(events, codexSkillUsageEvents(line, seen)...)
	}
	return events
}

func claudeSkillUsageEvents(line skillUsageTranscriptLine, seen time.Time) []skillUsageEvent {
	var events []skillUsageEvent
	for _, content := range line.Message.Content {
		if content.Type != "tool_use" || !strings.EqualFold(content.Name, "Skill") {
			continue
		}
		if name := skillNameFromClaudeInput(content.Input); name != "" {
			events = append(events, skillUsageEvent{Name: name, Seen: seen})
		}
	}
	return events
}

func codexSkillUsageEvents(line skillUsageTranscriptLine, seen time.Time) []skillUsageEvent {
	if line.Type != "response_item" ||
		(line.Payload.Type != "custom_tool_call" && line.Payload.Type != "function_call") ||
		!isCodexExecutionTool(line.Payload.Name) {
		return nil
	}
	input := rawJSONString(line.Payload.Input)
	if input == "" {
		input = rawJSONString(line.Payload.Arguments)
	}
	names := skillNamesFromCodexToolInput(input)
	events := make([]skillUsageEvent, 0, len(names))
	for _, name := range names {
		events = append(events, skillUsageEvent{Name: name, Seen: seen})
	}
	return events
}

func skillNameFromClaudeInput(input json.RawMessage) string {
	var fields map[string]json.RawMessage
	if len(input) == 0 || json.Unmarshal(input, &fields) != nil {
		return ""
	}
	for _, key := range []string{"skill", "name"} {
		var name string
		if raw, ok := fields[key]; ok && json.Unmarshal(raw, &name) == nil {
			if normalized := normalizeSkillName(name); normalized != "" {
				return normalized
			}
		}
	}
	return ""
}

func isCodexExecutionTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "exec", "exec_command":
		return true
	default:
		return false
	}
}

func rawJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return string(raw)
}

func skillNamesFromCodexToolInput(input string) []string {
	seen := map[string]bool{}
	var names []string
	for _, match := range skillFilePathRE.FindAllString(input, -1) {
		parts := strings.FieldsFunc(strings.ReplaceAll(match, `\`, "/"), func(r rune) bool {
			return r == '/'
		})
		if len(parts) < 2 {
			continue
		}
		name := normalizeSkillName(parts[len(parts)-2])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeSkillName(name string) string {
	name = strings.TrimSpace(name)
	if !skillNameRE.MatchString(name) || redactSecrets(name) != name {
		return ""
	}
	return name
}

func roleFromContainerName(name string) string {
	role, _, _ := strings.Cut(strings.TrimSpace(name), "-")
	switch role {
	case roleEngineer, roleDirector, roleQA:
		return role
	default:
		return ""
	}
}

// writeSkillUsageArtifact atomically writes observed usage and removes any stale
// artifact when a deterministic run directory is reused without skill events.
func writeSkillUsageArtifact(container, dir string, usage *skillUsageArtifact) error {
	artifactPath := filepath.Join(dir, drainSkillUsageFile)
	if usage == nil || len(usage.Skills) == 0 {
		if err := os.Remove(artifactPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("ward container: drain %s: remove stale %s: %w", container, drainSkillUsageFile, err)
		}
		return nil
	}
	if err := writeJSONAtomic(artifactPath, usage); err != nil {
		return fmt.Errorf("ward container: drain %s: write %s: %w", container, drainSkillUsageFile, err)
	}
	return nil
}
