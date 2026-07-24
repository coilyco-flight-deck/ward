package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/urfave/cli/v3"
)

// TestFleetToRosterJSONProjectsEveryField pins the stable JSON projection: every
// field lands under its key, and the -1 sentinel + empty argv modes survive.
func TestFleetToRosterJSONProjectsEveryField(t *testing.T) {
	in := fleetconfig.Fleet{
		SchemaVersion: 2,
		Defaults: fleetconfig.Defaults{
			Agent:       "claude",
			Attribution: fleetconfig.Attribution{Name: "n", Email: "e"},
		},
		Agents: []fleetconfig.Agent{
			{
				Name: "codex", Binary: "codex", ContextLevel: -1,
				Stream: "none", Auth: "codex-file", Model: "gpt", Endpoint: "ep",
				Provider: "p", ReasoningEffort: "low", Verbosity: "low",
				Argv: fleetconfig.Argv{Headless: []string{"codex", "exec"}},
			},
		},
	}
	got := fleetToRosterJSON(in)
	if got.SchemaVersion != 2 || got.Defaults.Agent != "claude" ||
		got.Defaults.Attribution.Name != "n" || got.Defaults.Attribution.Email != "e" {
		t.Fatalf("defaults/version not projected: %+v", got)
	}
	if len(got.Agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(got.Agents))
	}
	a := got.Agents[0]
	if a.Name != "codex" || a.Binary != "codex" || a.ContextLevel != -1 ||
		a.Stream != "none" || a.Auth != "codex-file" || a.Model != "gpt" ||
		a.Endpoint != "ep" || a.Provider != "p" || a.ReasoningEffort != "low" ||
		a.Verbosity != "low" {
		t.Errorf("agent fields not projected verbatim: %+v", a)
	}
	if len(a.Argv.Headless) != 2 || a.Argv.Preflight != nil || a.Argv.Interactive != nil {
		t.Errorf("argv not projected verbatim: %+v", a.Argv)
	}
}

// runAgentsList runs `ward agents list <args>` against a captured Writer and
// returns stdout, exercising the surface end to end (parse -> project -> marshal).
func runAgentsList(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := &cli.Command{
		Name:     "ward",
		Writer:   &out,
		Commands: []*cli.Command{agentsCommand()},
	}
	full := append([]string{"ward", "agents", "list"}, args...)
	if err := root.Run(context.Background(), full); err != nil {
		t.Fatalf("run %v: %v", full, err)
	}
	return out.String()
}

// TestAgentsListJSONMatchesEmbeddedFleet asserts `--json` emits valid JSON that
// equals the effective fleet's projection - the read surface aos consumes.
func TestAgentsListJSONMatchesEmbeddedFleet(t *testing.T) {
	fleet, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig: %v", err)
	}
	var got agentsRosterJSON
	if err := json.Unmarshal([]byte(runAgentsList(t, "--json")), &got); err != nil {
		t.Fatalf("emitted --json is not valid JSON: %v", err)
	}
	want := fleetToRosterJSON(fleet)
	gotBytes, _ := json.Marshal(got)
	wantBytes, _ := json.Marshal(want)
	if string(gotBytes) != string(wantBytes) {
		t.Errorf("--json drifted from the embedded fleet projection\n got: %s\nwant: %s", gotBytes, wantBytes)
	}
	if got.SchemaVersion != fleetconfig.SchemaVersion || len(got.Agents) == 0 {
		t.Errorf("roster looks empty/wrong: version=%d agents=%d", got.SchemaVersion, len(got.Agents))
	}
}

func TestAgentsListJSONIgnoresSelectedBundleDefaults(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+writeSelectedBundleFixture(t))
	var got agentsRosterJSON
	if err := json.Unmarshal([]byte(runAgentsList(t, "--json")), &got); err != nil {
		t.Fatalf("emitted --json is not valid JSON: %v", err)
	}
	if got.Defaults.Agent != string(defaultAgentMode()) {
		t.Fatalf("selected bundle default agent = %q, want baked %q", got.Defaults.Agent, defaultAgentMode())
	}
}

// TestAgentsListTableDefault asserts the bare `list` prints the human table (not
// JSON), keeping the machine surface behind the explicit --json flag.
func TestAgentsListTableDefault(t *testing.T) {
	out := runAgentsList(t)
	if json.Valid([]byte(out)) {
		t.Errorf("default output should be the human table, not JSON: %q", out)
	}
	if !bytes.Contains([]byte(out), []byte("ward fleet roster")) {
		t.Errorf("table output missing header: %q", out)
	}
}
