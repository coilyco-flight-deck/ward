package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/coilysiren/cli-guard/repocfg"
)

// errNoConfig is returned when no agent-guard config can be found by
// walking up from cwd.
var errNoConfig = errors.New("no .agent-guard/agent-guard.yaml or .coily/coily.yaml reachable from cwd")

// configCandidate names the per-level filenames agent-guard accepts. The
// canonical home is .agent-guard/agent-guard.yaml. .coily/coily.yaml is
// honored so repos already using coily's allowlist do not have to rename
// the file to adopt agent-guard. The format is the cli-guard repocfg
// format in both cases.
type configCandidate struct {
	dir  string
	file string
}

var configCandidates = []configCandidate{
	{dir: ".agent-guard", file: "agent-guard.yaml"},
	{dir: ".coily", file: "coily.yaml"},
}

// discoverConfig walks up from start looking for the first reachable
// agent-guard or coily allowlist. Returns the absolute path on success
// or errNoConfig if nothing is reachable.
func discoverConfig(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("abs %s: %w", start, err)
	}
	for {
		for _, cand := range configCandidates {
			path := filepath.Join(dir, cand.dir, cand.file)
			info, statErr := os.Stat(path)
			if statErr == nil && !info.IsDir() {
				return path, nil
			}
			if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
				return "", fmt.Errorf("stat %s: %w", path, statErr)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNoConfig
		}
		dir = parent
	}
}

// loadDefault discovers the config from cwd and parses it via cli-guard's
// repocfg loader (which runs every argv token through the shell-
// metacharacter policy check).
func loadDefault() (*repocfg.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	path, err := discoverConfig(cwd)
	if err != nil {
		return nil, err
	}
	return repocfg.Load(path)
}
