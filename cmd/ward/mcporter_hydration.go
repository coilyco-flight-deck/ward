package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

// mcporterHydrationScript is the canonical merge helper, mounted read-only into
// warded containers via the /substrate agentic-os-kai reference repo.
var mcporterHydrationScript = "/substrate/agentic-os-kai/scripts/merge-mcporter.py"

// containerMcporterHydrateCommand is the hidden `ward container mcporter-hydrate`:
// a first-class startup capability that materializes ~/.mcporter/mcporter.json.
func containerMcporterHydrateCommand() *cli.Command {
	return &cli.Command{
		Name:            "mcporter-hydrate",
		Hidden:          true,
		Usage:           "Hydrate ~/.mcporter/mcporter.json from the mounted agentic-os-kai bundle (image-internal; ward#672).",
		SkipFlagParsing: true,
		Action: func(ctx context.Context, _ *cli.Command) error {
			home := homeDir()
			if home == "" {
				return fmt.Errorf("mcporter hydration: could not resolve HOME")
			}
			return newRunner().hydrateMcporter(ctx, home)
		},
	}
}

// hydrateMcporter runs the canonical merge helper against the mounted
// agentic-os-kai bundle, writing the merged inventory into home.
func (r *Runner) hydrateMcporter(ctx context.Context, home string) error {
	return r.hydrateMcporterFrom(ctx, home, mcporterHydrationScript)
}

// hydrateMcporterFrom runs the canonical merge helper from a specific script
// path, which keeps the startup path testable without mutating /substrate.
func (r *Runner) hydrateMcporterFrom(ctx context.Context, home, script string) error {
	home = strings.TrimSpace(home)
	script = strings.TrimSpace(script)
	if home == "" {
		return fmt.Errorf("mcporter hydration: missing home directory")
	}
	if script == "" {
		return fmt.Errorf("mcporter hydration: missing merge helper path")
	}
	if !fileExists(script) {
		return fmt.Errorf("mcporter hydration: missing canonical merge helper at %s", script)
	}
	if !commandExists("python3") {
		return fmt.Errorf("mcporter hydration: python3 is not available in this container")
	}
	cmd := exec.CommandContext(ctx, "python3", script)
	cmd.Env = mcporterHydrationEnv(home)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mcporter hydration: merge helper failed: %w", err)
	}
	blog("mcporter hydrated at %s", filepath.Join(home, ".mcporter", "mcporter.json"))
	return nil
}

// mcporterHydrationEnv runs the merge helper with the agent's home bound as HOME
// and the container's shared /workspace root available for path rewrites.
func mcporterHydrationEnv(home string) []string {
	env := envWithOverrides(os.Environ(),
		"HOME", home,
		"COILYSIREN_PROJECTS", "/workspace",
	)
	if guide := strings.TrimSpace(os.Getenv("WORK_MCP_GUIDE")); guide != "" && fileExists(guide) {
		env = envWithOverrides(env, "WORK_MCP_GUIDE", guide)
	}
	return env
}

// envWithOverrides returns env with the supplied key/value pairs replacing any
// existing entries. Odd-length input is ignored after the last full pair.
func envWithOverrides(base []string, kv ...string) []string {
	if len(kv) < 2 {
		return append([]string{}, base...)
	}
	out := append([]string{}, base...)
	for i := 0; i+1 < len(kv); i += 2 {
		key, val := kv[i], kv[i+1]
		prefix := key + "="
		replaced := false
		for j, existing := range out {
			if strings.HasPrefix(existing, prefix) {
				out[j] = prefix + val
				replaced = true
			}
		}
		if !replaced {
			out = append(out, prefix+val)
		}
	}
	return out
}
