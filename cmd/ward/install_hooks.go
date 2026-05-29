package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

// installHooksCommand returns the `install-hooks` subcommand. See docs/install-hooks.md.
func installHooksCommand() *cli.Command {
	return &cli.Command{
		Name:  "install-hooks",
		Usage: "Idempotently register the ward PreToolUse hook in .claude/settings.json.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Print the proposed write to stdout instead of writing.",
			},
			&cli.BoolFlag{
				Name:  "check",
				Usage: "Exit non-zero if the hook entry is not present. No writes.",
			},
			&cli.StringFlag{
				Name:  "path",
				Usage: "Explicit path to .claude/settings.json (default: <git-toplevel>/.claude/settings.json).",
			},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			return runInstallHooks(installHooksArgs{
				explicitPath: c.String("path"),
				dryRun:       c.Bool("dry-run"),
				check:        c.Bool("check"),
			}, os.Stdout)
		},
	}
}

type installHooksArgs struct {
	explicitPath string
	dryRun       bool
	check        bool
}

const (
	wantedMatcher = "Bash"
	wantedCommand = "ward hook pre-tool-use"
	wantedType    = "command"
)

func runInstallHooks(args installHooksArgs, out *os.File) error {
	target, err := resolveSettingsPath(args.explicitPath)
	if err != nil {
		return err
	}

	existing, err := loadSettings(target)
	if err != nil {
		return err
	}

	present, merged := ensureHook(existing)

	if args.check {
		if present {
			_, _ = fmt.Fprintf(out, "ward install-hooks: hook present at %s\n", target)
			return nil
		}
		return cli.Exit(fmt.Sprintf("ward install-hooks: hook not registered at %s", target), 1)
	}

	if present && !args.dryRun {
		_, _ = fmt.Fprintf(out, "ward install-hooks: hook already registered at %s, nothing to do\n", target)
		return nil
	}

	rendered, err := marshalSettings(merged)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if args.dryRun {
		_, _ = fmt.Fprintf(out, "ward install-hooks: would write to %s:\n", target)
		_, _ = out.Write(rendered)
		if !strings.HasSuffix(string(rendered), "\n") {
			_, _ = fmt.Fprintln(out)
		}
		return nil
	}

	if err := writeSettings(target, rendered); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	_, _ = fmt.Fprintf(out, "ward install-hooks: registered hook in %s\n", target)
	return nil
}

// resolveSettingsPath returns absolute settings.json path. See docs/install-hooks.md.
func resolveSettingsPath(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("abs %s: %w", explicit, err)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	top, err := gitToplevel(cwd)
	if err != nil {
		return "", fmt.Errorf("auto-detect failed (cwd is not in a git repo); pass --path to specify settings.json directly: %w", err)
	}
	return filepath.Join(top, ".claude", "settings.json"), nil
}

// gitToplevel runs `git rev-parse --show-toplevel` rooted at dir.
// Returns the absolute repo root path.
func gitToplevel(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// loadSettings reads settings.json. Missing -> empty map. See docs/install-hooks.md.
func loadSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- caller-controlled target path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w (refusing to overwrite a malformed settings.json)", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// ensureHook returns (present, merged) after ensuring the hook entry exists.
// See docs/install-hooks.md for merge rules.
func ensureHook(in map[string]any) (bool, map[string]any) {
	out := cloneMap(in)
	hooks, _ := out["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	preToolUse, _ := hooks["PreToolUse"].([]any)

	wanted := map[string]any{"type": wantedType, "command": wantedCommand}
	bashEntryIdx := -1
	for i, raw := range preToolUse {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if matcher, _ := entry["matcher"].(string); matcher == wantedMatcher {
			bashEntryIdx = i
			inner, _ := entry["hooks"].([]any)
			for _, h := range inner {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				if cmd, _ := hm["command"].(string); cmd == wantedCommand {
					return true, out
				}
			}
		}
	}

	if bashEntryIdx >= 0 {
		entry := preToolUse[bashEntryIdx].(map[string]any)
		inner, _ := entry["hooks"].([]any)
		inner = append(inner, wanted)
		entry["hooks"] = inner
		preToolUse[bashEntryIdx] = entry
	} else {
		newEntry := map[string]any{
			"matcher": wantedMatcher,
			"hooks":   []any{wanted},
		}
		preToolUse = append(preToolUse, newEntry)
	}
	hooks["PreToolUse"] = preToolUse
	out["hooks"] = hooks
	return false, out
}

// cloneMap shallow-clones a map[string]any. See docs/install-hooks.md.
func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch tv := v.(type) {
		case map[string]any:
			out[k] = cloneMap(tv)
		case []any:
			cp := make([]any, len(tv))
			for i, item := range tv {
				if itemMap, ok := item.(map[string]any); ok {
					cp[i] = cloneMap(itemMap)
				} else {
					cp[i] = item
				}
			}
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// marshalSettings emits two-space JSON with trailing newline. See docs/install-hooks.md.
func marshalSettings(m map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return data, nil
}

// writeSettings writes data to path atomically (tempfile + rename).
// Creates the parent directory if missing.
func writeSettings(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".settings.json.*.tmp")
	if err != nil {
		return fmt.Errorf("tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
