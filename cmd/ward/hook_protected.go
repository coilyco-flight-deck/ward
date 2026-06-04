package main

import (
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/hook"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/hookcfg"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
)

// loadProtectedForCwd walks up from cwd looking for a ward/coily config,
// parses its security: block, and returns the protected-binary list the
// PreToolUse hook engine wants. Best-effort: any failure (no config
// reachable, parse error) returns nil so the hook stays a hint surface
// rather than a fail-closed gate — same posture the rest of runPreToolUse
// takes on malformed payloads. The repocfg.Security → []hook.Protected
// mapping itself lives in cli-guard/hookcfg. See docs/hook.md.
func loadProtectedForCwd(cwd string) []hook.Protected {
	if cwd == "" {
		return nil
	}
	path, err := discoverConfig(cwd)
	if err != nil {
		return nil
	}
	cfg, err := repocfg.Load(path)
	if err != nil {
		return nil
	}
	return hookcfg.ProtectedFor(cfg.Security)
}
