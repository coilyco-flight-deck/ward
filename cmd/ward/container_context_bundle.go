package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	contextBundleManifestName = "context-bundle.json"
	contextBundleFormat       = "ward.context-bundle.v1"
)

type contextBundleManifest struct {
	Format string `json:"format"`
	Role   string `json:"role"`
	Agent  string `json:"agent"`
}

type resolvedContextBundle struct {
	Root     string
	HasTools bool
}

type contextBundleFile struct {
	Rel  string
	Mode os.FileMode
	Body []byte
}

type inspectedContextBundle struct {
	Home     []contextBundleFile
	HasTools bool
}

type contextBundleWalkState struct {
	root           *os.Root
	rootPath       string
	agent          string
	instructionRel string
	skillsRel      string
	home           []contextBundleFile
	hasHome        bool
	hasInstruction bool
	hasTools       bool
}

func contextBundleDispatchGuard(nested bool, plan upPlan) error {
	if nested && plan.ContextBundle != "" {
		return fmt.Errorf("ward container: --context-bundle cannot be forwarded from inside a container because Docker cannot preserve its read-only host bind; launch this bundle-backed run from the host")
	}
	return nil
}

// resolveContextBundle validates Ward's narrow, provider-neutral handoff before
// Docker starts. The strict manifest has no authority or capability fields.
func resolveContextBundle(raw, role string, mode containerMode) (resolvedContextBundle, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return resolvedContextBundle{}, nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return resolvedContextBundle{}, fmt.Errorf("resolve --context-bundle %q: %w", raw, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return resolvedContextBundle{}, fmt.Errorf("resolve --context-bundle %q: %w", raw, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return resolvedContextBundle{}, fmt.Errorf("inspect --context-bundle %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return resolvedContextBundle{}, fmt.Errorf("--context-bundle %q is not a directory", resolved)
	}
	inspected, err := inspectContextBundle(resolved, role, string(mode))
	if err != nil {
		return resolvedContextBundle{}, fmt.Errorf("validate --context-bundle %q: %w", resolved, err)
	}
	return resolvedContextBundle{Root: resolved, HasTools: inspected.HasTools}, nil
}

func inspectContextBundle(root, role, agent string) (inspectedContextBundle, error) {
	scopedRoot, err := os.OpenRoot(root)
	if err != nil {
		return inspectedContextBundle{}, fmt.Errorf("open context bundle root: %w", err)
	}
	defer func() { _ = scopedRoot.Close() }()

	if err := validateContextBundleManifest(scopedRoot, role, agent); err != nil {
		return inspectedContextBundle{}, err
	}

	instructionRel, skillsRel, err := contextBundleLayout(agent)
	if err != nil {
		return inspectedContextBundle{}, err
	}
	state := contextBundleWalkState{
		root: scopedRoot, rootPath: root, agent: agent,
		instructionRel: instructionRel, skillsRel: skillsRel,
	}
	if err := filepath.WalkDir(root, state.inspectEntry); err != nil {
		return inspectedContextBundle{}, err
	}
	if !state.hasHome {
		return inspectedContextBundle{}, fmt.Errorf("missing home directory")
	}
	if !state.hasInstruction {
		return inspectedContextBundle{}, fmt.Errorf("missing selected %s instruction file home/%s", agent, instructionRel)
	}
	return inspectedContextBundle{Home: state.home, HasTools: state.hasTools}, nil
}

func validateContextBundleManifest(root *os.Root, role, agent string) error {
	manifestInfo, err := root.Lstat(contextBundleManifestName)
	if err != nil {
		return fmt.Errorf("missing readable %s: %w", contextBundleManifestName, err)
	}
	if manifestInfo.Mode()&os.ModeSymlink != 0 || !manifestInfo.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file, not a link or special file", contextBundleManifestName)
	}
	manifestFile, err := root.Open(contextBundleManifestName)
	if err != nil {
		return fmt.Errorf("open %s: %w", contextBundleManifestName, err)
	}
	defer func() { _ = manifestFile.Close() }()

	var manifest contextBundleManifest
	decoder := json.NewDecoder(io.LimitReader(manifestFile, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("decode %s: %w", contextBundleManifestName, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("decode %s: %w", contextBundleManifestName, err)
		}
		return fmt.Errorf("decode %s: multiple JSON values", contextBundleManifestName)
	}
	if manifest.Format != contextBundleFormat {
		return fmt.Errorf("%s format is %q, want %q", contextBundleManifestName, manifest.Format, contextBundleFormat)
	}
	if manifest.Role != role {
		return fmt.Errorf("%s role is %q, selected Ward role is %q", contextBundleManifestName, manifest.Role, role)
	}
	if manifest.Agent != agent {
		return fmt.Errorf("%s agent is %q, selected Ward agent is %q", contextBundleManifestName, manifest.Agent, agent)
	}
	return nil
}

func (s *contextBundleWalkState) inspectEntry(full string, entry os.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	rel, err := filepath.Rel(s.rootPath, full)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link", rel)
	}
	if !strings.Contains(rel, "/") {
		return s.inspectTopLevelEntry(rel, entry, info)
	}
	switch {
	case strings.HasPrefix(rel, "home/"):
		return s.inspectHomeEntry(rel, entry, info)
	case strings.HasPrefix(rel, "bin/"):
		return s.inspectToolEntry(rel, entry, info)
	default:
		return fmt.Errorf("unexpected bundle path %q", rel)
	}
}

func (s *contextBundleWalkState) inspectTopLevelEntry(rel string, entry os.DirEntry, info os.FileInfo) error {
	switch rel {
	case contextBundleManifestName:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		return nil
	case "home":
		if !entry.IsDir() {
			return fmt.Errorf("home is not a directory")
		}
		s.hasHome = true
		return nil
	case "bin":
		if !entry.IsDir() {
			return fmt.Errorf("bin is not a directory")
		}
		return nil
	default:
		return fmt.Errorf("unexpected bundle path %q", rel)
	}
}

func (s *contextBundleWalkState) inspectHomeEntry(rel string, entry os.DirEntry, info os.FileInfo) error {
	sub := strings.TrimPrefix(rel, "home/")
	if entry.IsDir() {
		if !contextBundleHomeDirAllowed(sub, s.instructionRel, s.skillsRel) {
			return fmt.Errorf("home path %q is outside the selected %s layout", sub, s.agent)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("home path %q is not a regular file", sub)
	}
	if sub != s.instructionRel && !strings.HasPrefix(sub, s.skillsRel+"/") {
		return fmt.Errorf("home path %q is outside the selected %s instruction and skill roots", sub, s.agent)
	}
	body, err := readContextBundleRootFile(s.root, rel)
	if err != nil {
		return fmt.Errorf("read home path %q: %w", sub, err)
	}
	s.home = append(s.home, contextBundleFile{
		Rel: filepath.FromSlash(sub), Mode: info.Mode().Perm(), Body: body,
	})
	if sub == s.instructionRel {
		s.hasInstruction = true
	}
	return nil
}

func (s *contextBundleWalkState) inspectToolEntry(rel string, entry os.DirEntry, info os.FileInfo) error {
	if entry.IsDir() {
		return fmt.Errorf("tool path %q must be directly under bin", rel)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("tool path %q is not a regular file", rel)
	}
	name := strings.TrimPrefix(rel, "bin/")
	if strings.Contains(name, "/") || !safeContextToolName(name) {
		return fmt.Errorf("tool path %q has an unsafe name", rel)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("tool path %q is not executable", rel)
	}
	s.hasTools = true
	return nil
}

func readContextBundleRootFile(root *os.Root, rel string) ([]byte, error) {
	file, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}

func contextBundleLayout(mode string) (instruction, skills string, err error) {
	switch containerMode(mode) {
	case modeClaude:
		return ".claude/CLAUDE.md", ".claude/skills", nil
	case modeCodex:
		return ".codex/AGENTS.md", ".agents/skills", nil
	case modeGoose:
		return ".config/goose/.goosehints", ".agents/skills", nil
	case modeOpencode:
		return ".config/opencode/AGENTS.md", ".agents/skills", nil
	default:
		return "", "", fmt.Errorf("ward has no context-bundle home layout for agent %q", mode)
	}
}

func contextBundleHomeDirAllowed(rel, instruction, skills string) bool {
	rel = path.Clean(filepath.ToSlash(rel))
	return rel == skills ||
		strings.HasPrefix(rel, skills+"/") ||
		strings.HasPrefix(instruction, rel+"/") ||
		strings.HasPrefix(skills, rel+"/")
}

func safeContextToolName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-", r) {
			return false
		}
	}
	return true
}

// projectContextBundleHome revalidates the read-only mount, copies its narrow
// home projection, then composes Ward authority into the harness load point.
func (r *Runner) projectContextBundleHome(e bootstrapEnv) error {
	bundle := strings.TrimSpace(e.ContextBundle)
	if bundle == "" {
		return nil
	}
	inspected, err := inspectContextBundle(bundle, e.Role, e.Mode)
	if err != nil {
		return fmt.Errorf("validate mounted context bundle at %s: %w", bundle, err)
	}
	instructionRel, _, err := contextBundleLayout(e.Mode)
	if err != nil {
		return err
	}

	// Remove only composeContext's known Ward-owned projection before installing
	// the selected instruction file. Any foreign replacement still fails closed.
	instruction := filepath.Join(e.AgentHome, filepath.FromSlash(instructionRel))
	if containerMode(e.Mode) != modeOpencode {
		if err := removeWardContextProjection(instruction, filepath.Join(e.AgentHome, "AGENTS.md")); err != nil {
			return err
		}
	}
	for _, file := range inspected.Home {
		dest := filepath.Join(e.AgentHome, file.Rel)
		if err := writeContextBundleFile(dest, file); err != nil {
			return err
		}
	}
	if err := appendWardAuthorityContext(instruction, filepath.Join(e.AgentHome, "AGENTS.md")); err != nil {
		return err
	}
	blog("context bundle projected: %s -> %s (%s/%s)", bundle, e.AgentHome, e.Role, e.Mode)
	return nil
}

func writeContextBundleFile(dest string, file contextBundleFile) error {
	if info, err := os.Lstat(dest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace foreign context path at %s", dest)
		}
		existing, err := os.ReadFile(dest) // #nosec G304 -- validated destination under agent HOME.
		if err != nil {
			return fmt.Errorf("read existing context path %s: %w", dest, err)
		}
		if !bytes.Equal(existing, file.Body) {
			return fmt.Errorf("refusing to replace foreign context path at %s", dest)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect context destination %s: %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create context destination directory for %s: %w", dest, err)
	}
	if err := os.WriteFile(dest, file.Body, file.Mode); err != nil {
		return fmt.Errorf("write context path %s: %w", dest, err)
	}
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
	projected, err := os.ReadFile(instruction) // #nosec G304 -- selected fixed load point.
	if err != nil {
		return fmt.Errorf("read projected context bundle instruction %s: %w", instruction, err)
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
