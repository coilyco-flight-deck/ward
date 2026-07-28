package main

import (
	"os"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/urfave/cli/v3"
)

// agentRosterDocPath is the committed page relative to this test's cmd/ward dir.
const agentRosterDocPath = "../../" + agentRosterDoc

// TestAgentRosterDocMatches fails when the committed docs/agent-roster.md drifts from
// the code roster's regenerated markdown - mirrors TestOpsAssetsMatchWardKDL (ward#348).
func TestAgentRosterDocMatches(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+writeBundleFixture(t))
	want, err := agentRosterMarkdown()
	if err != nil {
		t.Fatalf("agentRosterMarkdown: %v", err)
	}
	got, err := os.ReadFile(agentRosterDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentRosterDocPath, err)
	}
	if want != string(got) {
		t.Errorf("%s has drifted from the code roster; regenerate it with `%s`", agentRosterDoc, agentRosterRegenHint)
	}
}

// TestAgentRosterCommandRegistered asserts `roster` mounts under the agent umbrella
// so `ward agent roster` resolves.
func TestAgentRosterCommandRegistered(t *testing.T) {
	if commandNamed(agentCommand().Commands, "roster") == nil {
		t.Fatalf("agent umbrella missing the roster command; got %v", commandNames(agentCommand().Commands))
	}
}

// TestEmbeddedAgentRoleCatalogDefinesShippedRoster proves the embedded role KDL
// parses to the shipped role roster.
func TestEmbeddedAgentRoleCatalogDefinesShippedRoster(t *testing.T) {
	cat := mustEmbeddedAgentRoleCatalog()
	for _, role := range []string{"engineer", "director", "qa"} {
		def, ok := cat.Definitions[role]
		if !ok {
			t.Fatalf("embedded catalog missing role %q", role)
		}
		if strings.TrimSpace(def.Tagline) == "" || strings.TrimSpace(def.Modes) == "" || strings.TrimSpace(def.DefaultHarness) == "" {
			t.Fatalf("embedded catalog role %q missing required shipped fields: %+v", role, def)
		}
	}
	rows, err := agentRosterRowsFromDefinitions(agentCommand().Commands, cat.Definitions)
	if err != nil {
		t.Fatalf("agentRosterRowsFromDefinitions: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Role] = true
		if strings.TrimSpace(r.Tagline) == "" || strings.TrimSpace(r.Capabilities) == "" || strings.TrimSpace(r.Modes) == "" {
			t.Errorf("role %q has an empty tagline, capabilities, or modes column", r.Role)
		}
	}
	for _, role := range []string{"engineer", "director", "qa"} {
		if !got[role] {
			t.Errorf("roster missing the %q role; got %v", role, rosterRoleNames(rows))
		}
	}
	// roster itself is a meta verb, never a roster entry.
	if got["roster"] {
		t.Error("roster listed itself as a role; it must be skipped as a meta command")
	}
}

// TestAgentRosterRowsRejectsUndescribedRole asserts a registered role with no
// descriptor is a hard error, not a silent omission (ward#348).
func TestAgentRosterRowsRejectsUndescribedRole(t *testing.T) {
	cmds := []*cli.Command{
		{Name: "engineer"},
		{Name: "newcomer"}, // no resolved role definition entry
	}
	defs := embeddedAgentRoleDefinitions()
	delete(defs, "newcomer")
	if _, err := agentRosterRowsFromDefinitions(cmds, defs); err == nil {
		t.Fatal("agentRosterRowsFromDefinitions accepted a role with no descriptor; want an error")
	}
}

// TestAgentRoleDefinitionsFromFleetAppliesOverlay proves a config-defined overlay
// changes the effective roster without any Go registration change.
func TestAgentRoleDefinitionsFromFleetAppliesOverlay(t *testing.T) {
	base := embeddedAgentRoleDefinitions()
	fleet := fleetconfig.Fleet{
		Defaults: fleetconfig.Defaults{Agent: "claude"},
		Roles: []fleetconfig.Role{
			{
				Name: roleDirector,
				AgentConfig: map[string]fleetconfig.RoleAgentOverride{
					"claude": {
						Model:           "director-preview",
						DisplayName:     "fabled director",
						Pronouns:        "she",
						ReasoningEffort: "high",
					},
				},
			},
		},
	}
	defs, err := agentRoleDefinitionsFromFleet(fleet)
	if err != nil {
		t.Fatalf("agentRoleDefinitionsFromFleet: %v", err)
	}
	rows, err := agentRosterRowsFromDefinitions(agentCommand().Commands, defs)
	if err != nil {
		t.Fatalf("agentRosterRowsFromDefinitions: %v", err)
	}
	var directorRow *agentRosterRow
	for i := range rows {
		if rows[i].Role == roleDirector {
			directorRow = &rows[i]
			break
		}
	}
	if directorRow == nil {
		t.Fatal("director row missing from resolved roster")
	}
	if !strings.Contains(directorRow.Modes, "Role overlays: claude{model=director-preview, name=fabled director, pronouns=she, reasoning-effort=high}.") {
		t.Fatalf("director row modes did not include the config overlay summary: %q", directorRow.Modes)
	}
	if defs[roleDirector].DefaultHarness != "claude" {
		t.Fatalf("director default harness = %q, want fleet default claude", defs[roleDirector].DefaultHarness)
	}
	if defs[roleDirector].Tagline != base[roleDirector].Tagline {
		t.Fatalf("director tagline changed by overlay: got %q want %q", defs[roleDirector].Tagline, base[roleDirector].Tagline)
	}
	if defs[roleDirector].Capabilities.String() != base[roleDirector].Capabilities.String() {
		t.Fatalf("director capabilities changed by overlay: got %q want %q", defs[roleDirector].Capabilities.String(), base[roleDirector].Capabilities.String())
	}
	if defs[roleDirector].Modes != base[roleDirector].Modes {
		t.Fatalf("director modes changed by overlay: got %q want %q", defs[roleDirector].Modes, base[roleDirector].Modes)
	}
}

func TestAgentRoleDefinitionsIgnoreOperatorBundleCatalog(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+writeSelectedBundleFixture(t))
	defs, err := agentRoleDefinitions()
	if err != nil {
		t.Fatalf("agentRoleDefinitions: %v", err)
	}
	for _, role := range []string{roleEngineer, roleDirector, "qa"} {
		if got := defs[role].DefaultHarness; got != string(modeClaude) {
			t.Fatalf("role %q default harness = %q, want baked %q", role, got, modeClaude)
		}
	}
}

// TestAgentRosterMarkdownShape sanity-checks the generated body: the doc_goal
// front-matter, the generated-by header, and a flat bullet per role linking its doc.
func TestAgentRosterMarkdownShape(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+writeBundleFixture(t))
	md, err := agentRosterMarkdown()
	if err != nil {
		t.Fatalf("agentRosterMarkdown: %v", err)
	}
	for _, want := range []string{
		"---\ndoc_goal: ",
		"# ward agent: the role roster",
		"ward agent roster --markdown",
		"- [`warded engineer`](agent-engineer.md) - Implements a ticket end to end. Capabilities: read + engineering. Modes: ",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("generated roster body missing %q", want)
		}
	}
	if !strings.HasSuffix(md, "\n") {
		t.Error("generated roster body should end in a newline")
	}
}

// TestAgentRosterDefaultPrintsRoster asserts the truly-empty `warded` path renders the
// generated role roster + launch hint, not the CLI flag dump (ward#360).
func TestAgentRosterDefaultPrintsRoster(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+writeBundleFixture(t))
	var buf strings.Builder
	cmd := agentCommand()
	cmd.Writer = &buf
	if err := agentRosterDefault(cmd); err != nil {
		t.Fatalf("agentRosterDefault: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"the startup-role roster",
		"warded engineer - Implements a ticket end to end.",
		"capabilities: read + engineering",
		"warded qa - Inspects a candidate and posts a structured verdict comment.",
		"ward agent roster", // the launch-hint footer
	} {
		if !strings.Contains(out, want) {
			t.Errorf("bare-warded roster output missing %q; got:\n%s", want, out)
		}
	}
	// The whole point of ward#360: this is the roster, not the flag wall.
	for _, unwanted := range []string{"GLOBAL OPTIONS", "--harness"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("bare-warded output leaked the CLI flag dump (%q); got:\n%s", unwanted, out)
		}
	}
}

func rosterRoleNames(rows []agentRosterRow) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Role)
	}
	return names
}
