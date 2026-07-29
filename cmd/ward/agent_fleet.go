package main

import "fmt"

// launchAgent contains mechanics Ward must know to invoke a harness. Policy
// inputs stay outside this typed adapter.
type launchAgent struct {
	Name         string
	Binary       string
	ContextLevel int
	Stream       string
	Auth         string
	Model        string
	Endpoint     string
	Provider     string
	Effort       string
	Verbosity    string
	Argv         agentArgv
}

type launchAttribution struct {
	Name  string
	Email string
}

type launchConfig struct {
	DefaultAgent string
	Attribution  launchAttribution
	Agents       []launchAgent
}

var frontierAgentOrder = []string{"claude", "codex", "opencode", "goose"}

// builtInLaunchConfig describes invocation mechanics. Empty policy fields
// defer to environment or explicit --config inputs.
func builtInLaunchConfig() launchConfig {
	return launchConfig{
		DefaultAgent: string(modeClaude),
		Attribution: launchAttribution{
			Name:  "example-bot",
			Email: "bot@example.com",
		},
		Agents: []launchAgent{
			{
				Name: string(modeClaude), Binary: "claude", ContextLevel: 2,
				Stream: "stream-json", Auth: "claude-keychain",
				Argv: agentArgv{
					Preflight:   []string{"claude", "-p"},
					Headless:    []string{"claude", "-p", "--verbose", "--output-format", "stream-json"},
					Interactive: []string{"claude"},
				},
			},
			{
				Name: string(modeCodex), Binary: "codex", ContextLevel: 1,
				Stream: "none", Auth: "codex-file",
				Argv: agentArgv{
					Headless:    []string{"codex", "exec"},
					Interactive: []string{"codex"},
				},
			},
			{
				Name: string(modeOpencode), Binary: "opencode", ContextLevel: 0,
				Stream: "none", Auth: "none",
				Argv: agentArgv{
					Headless:    []string{"opencode", "run"},
					Interactive: []string{"opencode"},
				},
			},
			{
				Name: string(modeGoose), Binary: "goose", ContextLevel: 2,
				Stream: "none", Auth: "ollama",
				Argv: agentArgv{
					Preflight:   []string{"goose", "run", "-t"},
					Headless:    []string{"goose", "run", "--no-session", "-t"},
					Interactive: []string{"goose", "session"},
				},
			},
		},
	}
}

func frontierAgentNames() []string {
	out := make([]string, len(frontierAgentOrder))
	copy(out, frontierAgentOrder)
	return out
}

func launchAgentByName(cfg launchConfig, name string) launchAgent {
	for _, agent := range cfg.Agents {
		if agent.Name == name {
			return agent
		}
	}
	return launchAgent{}
}

func validateLaunchConfig(cfg launchConfig) error {
	if cfg.DefaultAgent == "" {
		return fmt.Errorf("launch config: no default agent")
	}
	if launchAgentByName(cfg, cfg.DefaultAgent).Name == "" {
		return fmt.Errorf("launch config: default agent %q is not registered", cfg.DefaultAgent)
	}
	seen := map[string]bool{}
	for i, agent := range cfg.Agents {
		if agent.Name == "" {
			return fmt.Errorf("launch config: agent %d has no name", i)
		}
		if seen[agent.Name] {
			return fmt.Errorf("launch config: duplicate agent %q", agent.Name)
		}
		seen[agent.Name] = true
		if agent.Binary == "" {
			return fmt.Errorf("launch config: agent %q has no binary", agent.Name)
		}
		if agent.ContextLevel < 0 || agent.ContextLevel > 2 {
			return fmt.Errorf("launch config: agent %q context level %d out of range 0..2", agent.Name, agent.ContextLevel)
		}
		if len(agent.Argv.Headless) == 0 || agent.Argv.Headless[0] != agent.Binary {
			return fmt.Errorf("launch config: agent %q has invalid headless argv", agent.Name)
		}
	}
	return nil
}

func loadLaunchConfig() (launchConfig, error) {
	cfg := builtInLaunchConfig()
	prefs, err := loadOperatorPreferences()
	if err != nil {
		return launchConfig{}, err
	}
	if prefs.DefaultHarness != "" {
		cfg.DefaultAgent = prefs.DefaultHarness
	}
	if err := validateLaunchConfig(cfg); err != nil {
		return launchConfig{}, err
	}
	return cfg, nil
}
