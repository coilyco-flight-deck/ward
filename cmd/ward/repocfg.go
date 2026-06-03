package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
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

// preParseConfigFlag scans args for the root-level --config flag before
// urfave/cli sees them. Needed because execCommand builds its subtree from
// the loaded config at init time. Recognises --config=<path>, --config <path>,
// and the deprecated --config-path forms; stops at -- or the first positional.
//
// Returns "" if the flag is absent. Env-var precedence is applied at
// resolveConfigPath, not here.
func preParseConfigFlag(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return ""
		}
		switch {
		case a == "--config":
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		case len(a) > len("--config=") && a[:len("--config=")] == "--config=":
			return a[len("--config="):]
		case len(a) > 0 && a[0] != '-':
			return ""
		}
	}
	return ""
}

// resolveConfigPath picks the config path using the explicit > env > walk-up
// precedence. See docs/config-discovery.md.
//
// explicit is the value of --config (empty if unset). env is the value of
// $WARD_CONFIG (empty if unset). cwd is the directory the walk-up starts from.
//
// An explicit or env-supplied path is returned verbatim (made absolute) without
// stat-ing; the eventual repocfg.Load call is the canonical existence check and
// produces a clearer error than a duplicate stat here.
func resolveConfigPath(explicit, env, cwd string) (string, error) {
	switch {
	case explicit != "":
		return filepath.Abs(explicit)
	case env != "":
		return filepath.Abs(env)
	default:
		return discoverConfig(cwd)
	}
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

// loadDefault resolves the config path via resolveConfigPath and parses it.
// See docs/config-discovery.md.
func loadDefault() (*repocfg.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	path, err := resolveConfigPath(explicitConfigPath(), os.Getenv("WARD_CONFIG"), cwd)
	if err != nil {
		return nil, err
	}
	return repocfg.Load(path)
}
