package main

import (
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/hook"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
)

// loadProtectedForCwd walks up from cwd looking for a ward/coily config,
// parses its security: block, and returns the protected-binary list the
// PreToolUse hook engine wants. Best-effort: any failure (no config
// reachable, parse error) returns nil so the hook stays a hint surface
// rather than a fail-closed gate — same posture the rest of runPreToolUse
// takes on malformed payloads. See docs/hook.md.
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
	return protectedFor(cfg.Security)
}

// protectedFor maps a parsed repocfg.Security block into the hook engine's
// Protected shape. Merges two sources:
//
//   - Every protected_binaries entry becomes a Protected; the engine
//     handles basename-aware matching against bare / absolute / relative
//     spellings.
//   - Every hooks.deny_bare_binaries entry that is NOT already covered by
//     a protected_binaries entry becomes a hint-only Protected (no
//     wrappers), so a downstream config can deny additional bare
//     invocations without declaring the full protected schema.
//
// Hint precedence: hooks.route_hints[name] when set, else engine
// synthesizes from Wrappers, else a bare deny.
func protectedFor(sec repocfg.Security) []hook.Protected {
	if len(sec.ProtectedBinaries) == 0 && len(sec.Hooks.DenyBareBinaries) == 0 {
		return nil
	}
	covered := make(map[string]bool, len(sec.ProtectedBinaries))
	out := make([]hook.Protected, 0, len(sec.ProtectedBinaries)+len(sec.Hooks.DenyBareBinaries))
	for _, pb := range sec.ProtectedBinaries {
		if pb.Name == "" {
			continue
		}
		covered[pb.Name] = true
		out = append(out, hook.Protected{
			Name:     pb.Name,
			Hint:     sec.Hooks.RouteHints[pb.Name],
			Wrappers: pb.AllowedWrappers,
		})
	}
	for _, name := range sec.Hooks.DenyBareBinaries {
		if name == "" || covered[name] {
			continue
		}
		out = append(out, hook.Protected{
			Name: name,
			Hint: sec.Hooks.RouteHints[name],
		})
	}
	return out
}
