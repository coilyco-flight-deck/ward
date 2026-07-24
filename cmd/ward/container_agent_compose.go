package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentComposeManifest = "manifest.json"

func agentComposeDispatchGuard(nested bool, plan upPlan) error {
	if nested && plan.AgentComposeBundle != "" {
		return fmt.Errorf("ward container: --agent-compose-bundle cannot be forwarded from inside a container because Docker cannot preserve its read-only host bind; launch this bundle-backed run from the host")
	}
	return nil
}

// resolveAgentComposeBundle validates the host-controlled handoff without
// parsing the bundle. Agent-compose remains the only schema and policy reader.
func resolveAgentComposeBundle(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve --agent-compose-bundle %q: %w", raw, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve --agent-compose-bundle %q: %w", raw, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect --agent-compose-bundle %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--agent-compose-bundle %q is not a directory", resolved)
	}
	manifest := filepath.Join(resolved, agentComposeManifest)
	manifestInfo, err := os.Stat(manifest)
	if err != nil {
		return "", fmt.Errorf("--agent-compose-bundle %q has no readable %s: %w", resolved, agentComposeManifest, err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return "", fmt.Errorf("--agent-compose-bundle %q has a non-regular %s", resolved, agentComposeManifest)
	}
	f, err := os.Open(manifest) // #nosec G304 -- explicit operator-selected bundle.
	if err != nil {
		return "", fmt.Errorf("--agent-compose-bundle %q has no readable %s: %w", resolved, agentComposeManifest, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close --agent-compose-bundle manifest %q: %w", manifest, err)
	}
	return resolved, nil
}

// agentComposeInstructionRel is the provider's documented home-scope load-point
// registry. This is the one cross-repo compatibility seam Ward must agree with.
func agentComposeInstructionRel(mode string) (string, error) {
	switch containerMode(mode) {
	case modeClaude:
		return filepath.Join(".claude", "CLAUDE.md"), nil
	case modeCodex:
		return filepath.Join(".codex", "AGENTS.md"), nil
	case modeGoose:
		return filepath.Join(".config", "goose", ".goosehints"), nil
	case modeOpencode:
		return filepath.Join(".config", "opencode", "AGENTS.md"), nil
	default:
		return "", fmt.Errorf("agent-compose has no home projection layout for harness %q", mode)
	}
}

var runAgentCompose = func(ctx context.Context, r *Runner, args ...string) error {
	return r.Runner.Exec(ctx, "agent-compose", args...)
}

// projectAgentComposeHome projects a verified opaque bundle through its provider,
// then composes Ward authority into the load point without reading the bundle.
func (r *Runner) projectAgentComposeHome(ctx context.Context, e bootstrapEnv) error {
	bundle := strings.TrimSpace(e.AgentComposeBundle)
	if bundle == "" {
		return nil
	}
	instructionRel, err := agentComposeInstructionRel(e.Mode)
	if err != nil {
		return err
	}
	if err := runAgentCompose(ctx, r, "verify", bundle); err != nil {
		return fmt.Errorf("verify agent-compose bundle at %s: %w", bundle, err)
	}

	// Remove only composeContext's known Ward-owned projection so agent-compose
	// can apply its foreign-file safety rules to the rest of HOME.
	instruction := filepath.Join(e.AgentHome, instructionRel)
	if containerMode(e.Mode) != modeOpencode {
		if err := removeWardContextProjection(instruction, filepath.Join(e.AgentHome, "AGENTS.md")); err != nil {
			return err
		}
	}
	if err := runAgentCompose(ctx, r,
		"project", bundle,
		"--layout", e.Mode,
		"--scope", "home",
		"--target", e.AgentHome,
	); err != nil {
		return fmt.Errorf("project agent-compose bundle at %s into %s home: %w", bundle, e.Mode, err)
	}
	if err := appendWardAuthorityContext(instruction, filepath.Join(e.AgentHome, "AGENTS.md")); err != nil {
		return err
	}
	blog("agent-compose bundle projected: %s -> %s (%s)", bundle, e.AgentHome, e.Mode)
	return nil
}

func removeWardContextProjection(instruction, authority string) error {
	info, err := os.Lstat(instruction)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Ward context projection %s: %w", instruction, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		projected, readErr := os.ReadFile(instruction) // #nosec G304 -- fixed path under agent HOME.
		if readErr != nil {
			return fmt.Errorf("read Ward context projection %s: %w", instruction, readErr)
		}
		base, readErr := os.ReadFile(authority) // #nosec G304 -- fixed path under agent HOME.
		if readErr != nil {
			return fmt.Errorf("read Ward authority context %s: %w", authority, readErr)
		}
		if !bytes.Equal(projected, base) {
			return fmt.Errorf("refusing to replace foreign harness context at %s", instruction)
		}
	}
	if err := os.Remove(instruction); err != nil {
		return fmt.Errorf("remove Ward context projection %s: %w", instruction, err)
	}
	return nil
}

func appendWardAuthorityContext(instruction, authority string) error {
	projected, err := os.ReadFile(instruction) // #nosec G304 -- provider-selected fixed load point.
	if err != nil {
		return fmt.Errorf("read projected agent-compose context %s: %w", instruction, err)
	}
	ward, err := os.ReadFile(authority) // #nosec G304 -- fixed path under agent HOME.
	if err != nil {
		return fmt.Errorf("read Ward authority context %s: %w", authority, err)
	}
	merged := make([]byte, 0, len(projected)+len(ward)+64)
	merged = append(merged, bytes.TrimSpace(projected)...)
	merged = append(merged, []byte("\n\n---\n\n## Ward container authority context\n\n")...)
	merged = append(merged, bytes.TrimSpace(ward)...)
	merged = append(merged, '\n')
	if err := os.WriteFile(instruction, merged, 0o644); err != nil { // #nosec G306 -- context, not a secret.
		return fmt.Errorf("compose Ward authority into %s: %w", instruction, err)
	}
	return nil
}
