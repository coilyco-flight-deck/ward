package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

const wardInstructionMarker = "<!-- ward-owned-harness-instruction -->"

// composeContext writes one regular instruction file directly to the selected
// harness's native load point. It never creates a shared home-level doctrine.
func (r *Runner) composeContext(e bootstrapEnv) error {
	agent := lookupAgent(containerMode(e.Mode))
	projection := agent.Record().Projection
	if err := validateHarnessProjection(agent); err != nil {
		return err
	}
	buf := append([]byte(containerDoctrine), '\n')
	level, _ := strconv.Atoi(e.ContextLevel)
	if level > 0 {
		if extra, source, ok := selectedInstructionSource(e.ContextSrc, projection.InstructionSources); ok {
			buf = appendSection(buf, extra)
			blog("selected %s instruction source %s", e.Mode, source)
		} else {
			diagnostic := fmt.Sprintf("## Ward context diagnostic\n\nNo compatible %s instruction source was present. Accepted source: %s. Ward used only its compiled container doctrine.\n",
				e.Mode, strings.Join(projection.InstructionSources, ", "))
			buf = appendSection(buf, []byte(diagnostic))
			blog("no compatible %s instruction source under %s; accepted: %s", e.Mode, e.ContextSrc, strings.Join(projection.InstructionSources, ", "))
		}
	} else {
		diagnostic := fmt.Sprintf("## Ward context diagnostic\n\nRepository instruction input is disabled for %s at context level 0. Ward used only its compiled container doctrine.\n", e.Mode)
		buf = appendSection(buf, []byte(diagnostic))
	}
	if block := substrateInventoryBlock(e.SubstrateDest); block != "" {
		buf = append(buf, []byte(block)...)
	}
	buf = append(buf, interactiveIntroductionContext(e)...)
	if e.ReadOnly {
		buf = append(buf, []byte(readOnlyContextBlock)...)
	}
	buf = append(buf, agentIdentityContext(e)...)
	dest := filepath.Join(e.AgentHome, filepath.FromSlash(projection.InstructionPath))
	if err := writeWardInstruction(e.AgentHome, dest, buf); err != nil {
		return err
	}
	blog("composed %s context (level %s%s) at %s", e.Mode, e.ContextLevel, readOnlyTag(e.ReadOnly), dest)
	return nil
}

func selectedInstructionSource(root string, sources []string) ([]byte, string, bool) {
	if !isDir(root) {
		return nil, "", false
	}
	for _, source := range sources {
		if body, ok := readFileIf(filepath.Join(root, filepath.FromSlash(source))); ok {
			return body, source, true
		}
	}
	return nil, "", false
}

func appendSection(dst, section []byte) []byte {
	dst = append(dst, []byte("\n\n---\n\n")...)
	return append(dst, bytes.TrimSpace(section)...)
}

func validateHarnessProjection(agent agentsapi.Agent) error {
	p := agent.Record().Projection
	if len(p.InstructionSources) == 0 || p.InstructionPath == "" || p.SkillsPath == "" || len(p.OwnershipPaths) == 0 {
		return fmt.Errorf("harness %q has an incomplete private-home projection", agent.Name())
	}
	all := append([]string{}, p.InstructionSources...)
	all = append(all, p.InstructionPath, p.SkillsPath)
	all = append(all, p.ConfigPaths...)
	all = append(all, p.CredentialPaths...)
	all = append(all, p.PermissionPaths...)
	all = append(all, p.OnboardingPaths...)
	all = append(all, p.StatePaths...)
	all = append(all, p.OwnershipPaths...)
	for _, rel := range all {
		if !safeProjectionPath(rel) {
			return fmt.Errorf("harness %q declares unsafe projection path %q", agent.Name(), rel)
		}
	}
	for _, check := range []struct {
		name  string
		paths []string
		has   bool
	}{
		{name: "credential", paths: p.CredentialPaths, has: implementsCredentialProvider(agent)},
		{name: "config", paths: p.ConfigPaths, has: implementsConfigComposer(agent)},
		{name: "permission", paths: p.PermissionPaths, has: implementsPermissionComposer(agent)},
		{name: "onboarding", paths: p.OnboardingPaths, has: implementsOnboardingSeeder(agent)},
	} {
		if check.has != (len(check.paths) > 0) {
			return fmt.Errorf("harness %q %s capability and projection paths disagree", agent.Name(), check.name)
		}
	}
	owned := append([]string{p.InstructionPath, p.SkillsPath}, p.ConfigPaths...)
	owned = append(owned, p.CredentialPaths...)
	owned = append(owned, p.PermissionPaths...)
	owned = append(owned, p.OnboardingPaths...)
	owned = append(owned, p.StatePaths...)
	for _, rel := range owned {
		if !projectionPathOwned(rel, p.OwnershipPaths) {
			return fmt.Errorf("harness %q projection path %q has no ownership root", agent.Name(), rel)
		}
	}
	return nil
}

func projectionPathOwned(rel string, roots []string) bool {
	rel = filepath.ToSlash(rel)
	for _, root := range roots {
		root = strings.TrimSuffix(filepath.ToSlash(root), "/")
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func safeProjectionPath(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || filepath.Clean(rel) != filepath.FromSlash(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(filepath.ToSlash(rel), "../")
}

func implementsCredentialProvider(agent agentsapi.Agent) bool {
	_, ok := agent.(agentsapi.CredentialProvider)
	return ok
}

func implementsConfigComposer(agent agentsapi.Agent) bool {
	_, ok := agent.(agentsapi.ConfigComposer)
	return ok
}

func implementsPermissionComposer(agent agentsapi.Agent) bool {
	_, ok := agent.(agentsapi.PermissionComposer)
	return ok
}

func implementsOnboardingSeeder(agent agentsapi.Agent) bool {
	_, ok := agent.(agentsapi.OnboardingSeeder)
	return ok
}

// writeWardInstruction atomically replaces only a prior Ward projection. A
// pre-existing foreign file or link fails closed.
func writeWardInstruction(home, dest string, body []byte) error {
	if err := ensureProjectionDestination(home, dest); err != nil {
		return err
	}
	if err := inspectExistingWardInstruction(dest); err != nil {
		return err
	}
	data := make([]byte, 0, len(wardInstructionMarker)+len(body)+4)
	data = append(data, wardInstructionMarker...)
	data = append(data, []byte("\n\n")...)
	data = append(data, bytes.TrimSpace(body)...)
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".ward-instruction-*")
	if err != nil {
		return fmt.Errorf("create temporary harness instruction: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("install harness instruction %s: %w", dest, err)
	}
	return nil
}

func inspectExistingWardInstruction(dest string) error {
	info, err := os.Lstat(dest)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect harness instruction %s: %w", dest, err)
	case info.Mode()&os.ModeSymlink != 0:
		target, readErr := os.Readlink(dest)
		if readErr == nil && target == filepath.Join("..", "AGENTS.md") {
			return nil
		}
		return fmt.Errorf("refusing to replace foreign harness instruction at %s", dest)
	case !info.Mode().IsRegular():
		return fmt.Errorf("refusing to replace foreign harness instruction at %s", dest)
	}
	existing, err := os.ReadFile(dest) // #nosec G304 -- selected path under the private agent home.
	if err != nil {
		return fmt.Errorf("read harness instruction %s: %w", dest, err)
	}
	if !bytes.HasPrefix(existing, []byte(wardInstructionMarker)) {
		return fmt.Errorf("refusing to replace foreign harness instruction at %s", dest)
	}
	return nil
}

func ensureProjectionDestination(home, dest string) error {
	rel, err := filepath.Rel(home, dest)
	if err != nil || !safeProjectionPath(rel) {
		return fmt.Errorf("harness instruction %s escapes private home %s", dest, home)
	}
	current := home
	for _, part := range strings.Split(filepath.Dir(rel), string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		switch {
		case os.IsNotExist(statErr):
			if mkErr := os.Mkdir(current, 0o755); mkErr != nil {
				return fmt.Errorf("create harness instruction directory %s: %w", current, mkErr)
			}
		case statErr != nil:
			return fmt.Errorf("inspect harness instruction directory %s: %w", current, statErr)
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return fmt.Errorf("refusing foreign harness instruction directory at %s", current)
		}
	}
	return nil
}
