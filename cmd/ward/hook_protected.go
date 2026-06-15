package main

import (
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/hook"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/hookcfg"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
)

// loadProtectedForCwd walks up from cwd for a ward/coily config and returns the
// protected-binary list for the hook engine. Best-effort: failures return nil.
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
