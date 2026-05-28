package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/coilysiren/cli-guard/repocfg"
)

// errNoConfig is returned when no ward config can be found by
// walking up from cwd.
var errNoConfig = errors.New("no .ward/ward.yaml or .coily/coily.yaml reachable from cwd")

// configCandidate names the per-level filenames ward accepts.
// See docs/config-discovery.md.
type configCandidate struct {
	dir  string
	file string
}

var configCandidates = []configCandidate{
	{dir: ".ward", file: "ward.yaml"},
	{dir: ".coily", file: "coily.yaml"},
}

// discoverConfig walks up from start to find an allowlist. See docs/config-discovery.md.
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

// loadDefault discovers and parses the config from cwd. See docs/config-discovery.md.
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
