package main

// agent_adapter.go projects the effective fleet roster onto the launcher's adapter
// shape. ward#419 dropped the YAML mirror; see docs/agent-adapter-manifest.md.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// The manifest reader owns the concrete harness names and the retired alias.
// The rest of core treats containerMode as an opaque string validated here.
const (
	modeClaude   containerMode = "claude"
	modeCodex    containerMode = "codex"
	modeOpencode containerMode = "opencode"
	modeGoose    containerMode = "goose"
)

// agentAdapterSchemaVersion is the manifest schema this build understands.
const agentAdapterSchemaVersion = 1

// agentArgv holds the argv prefixes for the three ways ward invokes an agent;
// the prompt (preflight) or seed (headless/interactive) is appended by the caller.
type agentArgv struct {
	Preflight   []string
	Headless    []string
	Interactive []string
}

// agentAdapter is one agent's full divergence record, replacing the per-mode
// Go switches and bash cases. See docs/agent-adapter-manifest.md for the schema.
type agentAdapter struct {
	Name         string
	Binary       string
	ContextLevel int
	Stream       string
	Auth         string
	ContextFiles []string
	Argv         agentArgv
}

// contextFiles returns the runtime doctrine load points this adapter needs.
func (a agentAdapter) contextFiles() []string {
	if len(a.ContextFiles) != 0 {
		out := make([]string, len(a.ContextFiles))
		copy(out, a.ContextFiles)
		return out
	}
	out := []string{filepath.Join(".claude", "CLAUDE.md"), filepath.Join(".codex", "AGENTS.md")}
	if a.Name == string(modeGoose) {
		out = append(out, filepath.Join(".config", "goose", ".goosehints"))
	}
	return out
}

// preflightArgv returns the host one-shot argv with the prompt appended, plus
// whether one exists. It mirrors containerMode.hostPreflightArgv (ward#152).
func (a agentAdapter) preflightArgv(prompt string) ([]string, bool) {
	if len(a.Argv.Preflight) == 0 {
		return nil, false
	}
	argv := make([]string, 0, len(a.Argv.Preflight)+1)
	argv = append(argv, a.Argv.Preflight...)
	argv = append(argv, prompt)
	return argv, true
}

// launchArgv returns the in-container argv for the selected posture. The prompt
// seed is appended by the caller.
func (a agentAdapter) launchArgv(headless, ask bool, model string, seed []string) ([]string, bool) { //nolint:cyclop
	if ask && len(a.Argv.Preflight) > 0 {
		argv := a.launchSeededArgv(a.Argv.Preflight, model, seed, true)
		if a.Name == string(modeGoose) {
			argv = gooseHeadlessArgv(argv)
		}
		return argv, false
	}
	if headless || ask {
		argv := append([]string{}, a.Argv.Headless...)
		if a.Name == string(modeGoose) {
			argv = gooseHeadlessArgv(argv)
		}
		return a.launchSeededArgv(argv, model, seed, true), a.Stream == "stream-json"
	}
	argv := append([]string{}, a.Argv.Interactive...)
	if model != "" && a.Binary == string(modeClaude) {
		argv = append(argv, "--model", model)
	}
	switch a.Name {
	case string(modeClaude), string(modeCodex):
		argv = append(argv, seed...)
	}
	return argv, false
}

func (a agentAdapter) launchSeededArgv(argv []string, model string, seed []string, appendSeed bool) []string {
	if model != "" && a.Binary == string(modeClaude) {
		argv = append(argv, "--model", model)
	}
	if appendSeed {
		argv = append(argv, seed...)
	}
	return argv
}

// gooseHeadlessArgv ensures Goose headless runs are sessionless and keep the
// `goose run --no-session -t` subcommand shape that exits after the final turn.
func gooseHeadlessArgv(argv []string) []string {
	if len(argv) < 2 || argv[0] != "goose" || argv[1] != "run" || slices.Contains(argv, "--no-session") {
		return argv
	}
	out := make([]string, 0, len(argv)+1)
	out = append(out, argv[:2]...)
	out = append(out, "--no-session")
	out = append(out, argv[2:]...)
	return out
}

// agentManifest is the parsed manifest: a schema version plus the agent records.
type agentManifest struct {
	SchemaVersion int
	Agents        []agentAdapter
}

// adapter looks an agent up by name (the --mode value).
func (m agentManifest) adapter(name string) (agentAdapter, bool) {
	for _, a := range m.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return agentAdapter{}, false
}

var (
	agentManifestOnce sync.Once
	agentManifestVal  agentManifest
	agentManifestErr  error
)

// cachedAgentManifest loads the effective adapter manifest once per process.
func cachedAgentManifest() (agentManifest, error) {
	agentManifestOnce.Do(func() {
		agentManifestVal, agentManifestErr = loadAgentManifest()
	})
	return agentManifestVal, agentManifestErr
}

// mustAgentAdapter resolves a mode to its manifest adapter. parseMode validates
// the mode first, so this is the runtime projection the launcher uses.
func mustAgentAdapter(mode containerMode) agentAdapter {
	m, err := cachedAgentManifest()
	if err != nil {
		panic(err)
	}
	a, ok := m.adapter(string(mode))
	if !ok {
		panic(fmt.Errorf("agent-adapter manifest: no adapter for %q", mode))
	}
	return a
}

// defaultAgentMode returns the manifest's default mode, falling back to the
// first roster entry when the bundle omits an explicit default.
func defaultAgentMode() containerMode {
	fleet, err := currentFleetConfigWithError()
	if err == nil {
		if agent := strings.TrimSpace(fleet.Defaults.Agent); agent != "" {
			return containerMode(agent)
		}
	}
	if fleet, err := bakedProfileProvider().Fleet(); err == nil {
		if agent := strings.TrimSpace(fleet.Defaults.Agent); agent != "" {
			return containerMode(agent)
		}
	}
	if names := frontierAgentNames(); len(names) > 0 {
		return containerMode(names[0])
	}
	return modeClaude
}

// selectedAgentMode resolves the active bundle's default harness, falling back
// to the baked default when the selected bundle omits one.
func selectedAgentMode() (containerMode, error) {
	fleet, err := currentFleetConfigWithError()
	if err != nil {
		return "", err
	}
	if agent := strings.TrimSpace(fleet.Defaults.Agent); agent != "" {
		return containerMode(agent), nil
	}
	if ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv)); ref != "" {
		if baked, berr := bakedProfileProvider().Fleet(); berr == nil {
			if agent := strings.TrimSpace(baked.Defaults.Agent); agent != "" {
				return containerMode(agent), nil
			}
		}
	}
	if names := frontierAgentNames(); len(names) > 0 {
		return containerMode(names[0]), nil
	}
	return modeClaude, nil
}

// loadAgentManifest builds the manifest from the effective dialect-2 fleet config
// (fleet.go) - the sole source since ward#419 deleted the agent-adapters.yaml mirror.
func loadAgentManifest() (agentManifest, error) {
	f, err := loadFleetConfig()
	if err != nil {
		return agentManifest{}, fmt.Errorf("agent-adapter manifest (from fleet): %w", err)
	}
	m := fleetToAgentManifest(f)
	if err := validateAgentManifest(m); err != nil {
		return agentManifest{}, err
	}
	return m, nil
}

// fleetToAgentManifest projects a parsed fleet roster onto the adapter shape the
// launcher reads (binary/context-level/stream/auth/argv); model/endpoint go direct.
func fleetToAgentManifest(f fleetconfig.Fleet) agentManifest {
	m := agentManifest{SchemaVersion: agentAdapterSchemaVersion}
	for _, a := range f.Agents {
		m.Agents = append(m.Agents, agentAdapter{
			Name:         a.Name,
			Binary:       a.Binary,
			ContextLevel: a.ContextLevel,
			Stream:       a.Stream,
			Auth:         a.Auth,
			ContextFiles: contextFilesForAdapter(a.Name),
			Argv: agentArgv{
				Preflight:   a.Argv.Preflight,
				Headless:    a.Argv.Headless,
				Interactive: a.Argv.Interactive,
			},
		})
	}
	return m
}

// contextFilesForAdapter centralizes the per-agent doctrine load-point fanout.
func contextFilesForAdapter(name string) []string {
	files := []string{filepath.Join(".claude", "CLAUDE.md"), filepath.Join(".codex", "AGENTS.md")}
	if name == string(modeGoose) {
		files = append(files, filepath.Join(".config", "goose", ".goosehints"))
	}
	return files
}

// validateAgentManifest enforces the schema on the projected fleet roster, so a
// malformed embed fails loud at load instead of driving the wrong binary.
func validateAgentManifest(m agentManifest) error {
	if m.SchemaVersion != agentAdapterSchemaVersion {
		return fmt.Errorf("agent-adapter manifest: schemaVersion %d, want %d", m.SchemaVersion, agentAdapterSchemaVersion)
	}
	if len(m.Agents) == 0 {
		return fmt.Errorf("agent-adapter manifest: no agents defined")
	}
	seen := map[string]bool{}
	for i, a := range m.Agents {
		if a.Name == "" {
			return fmt.Errorf("agent-adapter manifest: agent %d has no name", i)
		}
		if seen[a.Name] {
			return fmt.Errorf("agent-adapter manifest: duplicate agent %q", a.Name)
		}
		seen[a.Name] = true
		if a.Binary == "" {
			return fmt.Errorf("agent-adapter manifest: agent %q has no binary", a.Name)
		}
		if a.ContextLevel < 0 || a.ContextLevel > 2 {
			return fmt.Errorf("agent-adapter manifest: agent %q contextLevel %d out of range 0..2", a.Name, a.ContextLevel)
		}
		if len(a.Argv.Headless) == 0 {
			return fmt.Errorf("agent-adapter manifest: agent %q has no headless argv", a.Name)
		}
		if a.Argv.Headless[0] != a.Binary {
			return fmt.Errorf("agent-adapter manifest: agent %q headless argv starts with %q, not its binary %q", a.Name, a.Argv.Headless[0], a.Binary)
		}
	}
	return nil
}
