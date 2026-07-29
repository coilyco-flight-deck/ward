package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSkillUsageArtifactFromClaudeTranscript(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"assistant","timestamp":"2026-07-29T16:08:00Z","message":{"content":[{"type":"tool_use","id":"skill-1","name":"Skill","input":{"skill":"repo-ward","args":"prompt body ghp_1234567890abcdefghijklmnopqrstuvwxyz"}}]}}`,
		`{"type":"user","timestamp":"2026-07-29T16:08:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"skill-1","content":"private skill result body"}]}}`,
		`{"type":"assistant","timestamp":"2026-07-29T16:09:00Z","message":{"content":[{"type":"tool_use","id":"skill-2","name":"Skill","input":{"skill":"openai-docs","args":"another private body"}},{"type":"tool_use","id":"skill-3","name":"Skill","input":{"skill":"repo-ward"}},{"type":"tool_use","id":"skill-4","name":"Skill","input":{"skill":"ghp_1234567890abcdefghijklmnopqrstuvwxyz"}}]}}`,
	}, "\n")
	meta := runMeta{
		Container: "engineer-claude-ward-873",
		Repo:      "coilyco-flight-deck/ward",
		Issue:     "873",
		Driver:    "claude",
		Summary:   runSummary{Workflow: "merge-remote-main"},
		env: map[string]string{
			"WARD_RUN_ID":    "run-873-a",
			"WARD_ROLE":      "engineer",
			"WARD_HARNESS":   "claude",
			"WARD_ISSUE_REF": "coilyco-flight-deck/ward#873",
			"WARD_WORKFLOW":  "merge-remote-main",
			"WARD_VERSION":   "v0.856.0",
			"FORGEJO_TOKEN":  "must-not-serialize",
		},
	}

	got := buildSkillUsageArtifact(meta, []byte(transcript))
	if got == nil {
		t.Fatal("Claude Skill tool calls produced no skill-usage artifact")
	}
	if got.SchemaVersion != skillUsageSchemaVersion ||
		got.RunID != "run-873-a" ||
		got.Container != meta.Container ||
		got.Role != "engineer" ||
		got.Harness != "claude" ||
		got.Repo != meta.Repo ||
		got.IssueRef != "coilyco-flight-deck/ward#873" ||
		got.Workflow != "merge-remote-main" ||
		got.WardVersion != "v0.856.0" {
		t.Fatalf("unexpected run dimensions: %+v", got)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("got %d skill rows, want 2: %+v", len(got.Skills), got.Skills)
	}
	if got.Skills[0].SkillName != "openai-docs" || got.Skills[0].Count != 1 {
		t.Fatalf("first sorted skill row = %+v, want openai-docs count 1", got.Skills[0])
	}
	repoWard := got.Skills[1]
	if repoWard.SkillName != "repo-ward" || repoWard.Count != 2 {
		t.Fatalf("repo-ward row = %+v, want count 2", repoWard)
	}
	if repoWard.FirstSeen != "2026-07-29T16:08:00Z" || repoWard.LastSeen != "2026-07-29T16:09:00Z" {
		t.Fatalf("repo-ward observation window = %q..%q", repoWard.FirstSeen, repoWard.LastSeen)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"prompt body",
		"private skill result body",
		"another private body",
		"ghp_",
		"must-not-serialize",
		`"args"`,
		`"content"`,
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("skill-usage artifact copied forbidden transcript content %q: %s", forbidden, encoded)
		}
	}
}

func TestExtractSkillUsageEventsFromCodexTranscript(t *testing.T) {
	transcript := strings.Join([]string{
		`{"timestamp":"2026-07-29T16:07:12Z","type":"session_meta","payload":{"base_instructions":"mentioning /tmp/skills/not-observed/SKILL.md is not a tool call"}}`,
		`{"timestamp":"2026-07-29T16:07:13Z","type":"event_msg","payload":{"type":"user_message","message":"please read /tmp/skills/also-not-observed/SKILL.md"}}`,
		`{"timestamp":"2026-07-29T16:08:00.125Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"const r = await tools.exec_command({\"cmd\":\"sed -n '1,240p' /workspace/ward/.agents/skills/repo-ward/SKILL.md && sed -n '1,240p' /workspace/ward/.agents/skills/repo-ward/SKILL.md\"});"}}`,
		`{"timestamp":"2026-07-29T16:08:01Z","type":"response_item","payload":{"type":"custom_tool_call_output","output":"full skill body and ghp_1234567890abcdefghijklmnopqrstuvwxyz"}}`,
		`{"timestamp":"2026-07-29T16:09:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"sed -n '1,240p' /home/ubuntu/.codex/skills/.system/openai-docs/SKILL.md\"}"}}`,
	}, "\n")

	events := extractSkillUsageEvents([]byte(transcript))
	if len(events) != 2 {
		t.Fatalf("got %d Codex skill events, want 2: %+v", len(events), events)
	}
	if events[0].Name != "repo-ward" || events[0].Seen.Format("2006-01-02T15:04:05.000Z07:00") != "2026-07-29T16:08:00.125Z" {
		t.Fatalf("first Codex event = %+v", events[0])
	}
	if events[1].Name != "openai-docs" || events[1].Seen.Format("2006-01-02T15:04:05Z07:00") != "2026-07-29T16:09:00Z" {
		t.Fatalf("second Codex event = %+v", events[1])
	}
}

func TestSkillUsageArtifactWrittenToBothDrainViewsAndRemovedWhenEmpty(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	name := "engineer-codex-ward-873"
	rawDir := filepath.Join(agentLogsDir(), name)
	transcript := []byte(`{"timestamp":"2026-07-29T16:08:00Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"await tools.exec_command({\"cmd\":\"cat /workspace/ward/.agents/skills/repo-ward/SKILL.md\"})"}}` + "\n")
	meta := runMeta{
		Container: name,
		Repo:      "coilyco-flight-deck/ward",
		Issue:     "873",
		Driver:    "codex",
		Outcome:   outcomePushedMain,
		Summary:   runSummary{Workflow: "merge-remote-main"},
		env: map[string]string{
			"WARD_ROLE":    "engineer",
			"WARD_HARNESS": "codex",
			"WARD_VERSION": "v0.856.0",
		},
	}
	usage := buildSkillUsageArtifact(meta, transcript)
	if usage == nil {
		t.Fatal("Codex transcript produced no skill usage")
	}
	meta.Summary.Artifacts.SkillUsage = filepath.Join(rawDir, drainSkillUsageFile)

	r := &Runner{}
	r.writeDiskArtifacts(name, rawDir, nil, transcript, meta, usage)
	r.writeRedactedArtifacts(name, nil, transcript, meta, usage)

	rawPath := filepath.Join(rawDir, drainSkillUsageFile)
	redactedPath := filepath.Join(agentLogsRedactedDir(), name, drainSkillUsageFile)
	rawArtifact, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw skill usage: %v", err)
	}
	redactedArtifact, err := os.ReadFile(redactedPath)
	if err != nil {
		t.Fatalf("read redacted skill usage: %v", err)
	}
	if string(rawArtifact) != string(redactedArtifact) {
		t.Fatalf("raw and redacted skill-usage artifacts differ:\nraw: %s\nredacted: %s", rawArtifact, redactedArtifact)
	}
	if strings.Contains(string(rawArtifact), "cat /workspace") || strings.Contains(string(rawArtifact), "ghp_") {
		t.Fatalf("skill-usage artifact copied command/body content: %s", rawArtifact)
	}

	meta.Summary.Artifacts.SkillUsage = ""
	r.writeDiskArtifacts(name, rawDir, nil, nil, meta, nil)
	r.writeRedactedArtifacts(name, nil, nil, meta, nil)
	for _, path := range []string{rawPath, redactedPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("empty usage must leave no artifact at %s; stat err = %v", path, err)
		}
	}
}

func TestBuildSkillUsageArtifactAbsentWithoutObservedSkills(t *testing.T) {
	transcript := []byte(`{"timestamp":"2026-07-29T16:08:00Z","type":"event_msg","payload":{"type":"user_message","message":"use repo-ward"}}` + "\n")
	if got := buildSkillUsageArtifact(runMeta{Container: "engineer-codex-ward-873"}, transcript); got != nil {
		t.Fatalf("non-tool transcript produced skill usage: %+v", got)
	}
}
