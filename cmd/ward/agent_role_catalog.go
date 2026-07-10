package main

import (
	"fmt"
	"strings"
	"sync"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	kdl "github.com/calico32/kdl-go"
)

// agent_role_catalog.go owns the embedded shipped role-definition bundle. The
// KDL source is ward-owned product data, not a fleet-config overlay.

type agentRoleCatalog struct {
	Order       []string
	Definitions map[string]agentRoleDefinition
}

var agentRoleCatalogCache struct {
	sync.Once
	catalog agentRoleCatalog
	err     error
}

func loadEmbeddedAgentRoleCatalog() (agentRoleCatalog, error) {
	b, err := bakedAssets.ReadFile(roleDefinitionsGeneratedKDLPath)
	if err != nil {
		return agentRoleCatalog{}, fmt.Errorf("read agent role catalog %s: %w", roleDefinitionsGeneratedKDLPath, err)
	}
	return parseAgentRoleCatalog(b)
}

func cachedEmbeddedAgentRoleCatalog() (agentRoleCatalog, error) {
	agentRoleCatalogCache.Do(func() {
		agentRoleCatalogCache.catalog, agentRoleCatalogCache.err = loadEmbeddedAgentRoleCatalog()
	})
	return agentRoleCatalogCache.catalog, agentRoleCatalogCache.err
}

func cachedBuiltInAgentRoleCatalog() (agentRoleCatalog, error) {
	return cachedEmbeddedAgentRoleCatalog()
}

func mustEmbeddedAgentRoleCatalog() agentRoleCatalog {
	cat, err := cachedEmbeddedAgentRoleCatalog()
	if err != nil {
		panic(err)
	}
	return cat
}

func cloneAgentRoleDefinition(def agentRoleDefinition) agentRoleDefinition {
	out := def
	out.Capabilities = def.Capabilities.clone()
	out.Guardfiles = cloneGuardfiles(def.Guardfiles)
	out.AgentOverlays = cloneRoleOverlays(def.AgentOverlays)
	if len(def.MergeAuthority) > 0 {
		out.MergeAuthority = append([]workflowMode(nil), def.MergeAuthority...)
	}
	return out
}

func cloneAgentRoleDefinitionMap(in map[string]agentRoleDefinition) map[string]agentRoleDefinition {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]agentRoleDefinition, len(in))
	for name, def := range in {
		out[name] = cloneAgentRoleDefinition(def)
	}
	return out
}

func embeddedAgentRoleDefinitions() map[string]agentRoleDefinition {
	return cloneAgentRoleDefinitionMap(mustEmbeddedAgentRoleCatalog().Definitions)
}

func embeddedAgentRoleDefinitionOrder() []string {
	order := mustEmbeddedAgentRoleCatalog().Order
	if len(order) == 0 {
		return nil
	}
	out := make([]string, len(order))
	copy(out, order)
	return out
}

func builtInAgentRoleDefinitionOrder() []string {
	return embeddedAgentRoleDefinitionOrder()
}

func parseAgentRoleCatalog(src []byte) (agentRoleCatalog, error) { //nolint:gocyclo,cyclop,funlen
	doc, err := kdl.ParseString(string(src))
	if err != nil {
		return agentRoleCatalog{}, fmt.Errorf("agent role catalog: parse KDL: %w", err)
	}
	cat := agentRoleCatalog{Definitions: map[string]agentRoleDefinition{}}
	seenCatalog := false
	for _, n := range doc.Nodes {
		if n.Name() != "agent-roles" {
			return agentRoleCatalog{}, unknownAgentRoleCatalogNode("top-level", n.Name(), "agent-roles")
		}
		if seenCatalog {
			return agentRoleCatalog{}, fmt.Errorf("agent role catalog: duplicate top-level `agent-roles` block (fail-closed)")
		}
		seenCatalog = true
		if len(n.Arguments()) != 0 {
			return agentRoleCatalog{}, fmt.Errorf("agent role catalog: `agent-roles` takes no arguments (fail-closed)")
		}
		if len(n.Properties()) != 0 {
			return agentRoleCatalog{}, fmt.Errorf("agent role catalog: `agent-roles` takes no properties (fail-closed)")
		}
		seenRoles := map[string]bool{}
		for _, c := range n.Children().Nodes {
			def, err := parseAgentRoleDefinitionNode(c)
			if err != nil {
				return agentRoleCatalog{}, err
			}
			if seenRoles[def.Name] {
				return agentRoleCatalog{}, fmt.Errorf("agent role catalog: duplicate role %q (fail-closed)", def.Name)
			}
			seenRoles[def.Name] = true
			cat.Order = append(cat.Order, def.Name)
			cat.Definitions[def.Name] = def
		}
	}
	if !seenCatalog {
		return agentRoleCatalog{}, fmt.Errorf("agent role catalog: missing top-level `agent-roles` block (fail-closed)")
	}
	if len(cat.Definitions) == 0 {
		return agentRoleCatalog{}, fmt.Errorf("agent role catalog: no roles defined (fail-closed)")
	}
	return cat, nil
}

func parseAgentRoleDefinitionNode(n *kdl.Node) (agentRoleDefinition, error) {
	if n.Name() != "role" {
		return agentRoleDefinition{}, unknownAgentRoleCatalogNode("agent-roles body", n.Name(), "role")
	}
	name, err := agentRoleStringArg(n, "agent-roles > role")
	if err != nil {
		return agentRoleDefinition{}, err
	}
	if len(n.Properties()) != 0 {
		return agentRoleDefinition{}, fmt.Errorf("agent role catalog: agent-roles > role %q takes no properties (fail-closed)", name)
	}
	def := agentRoleDefinition{
		Name:           name,
		DefaultHarness: "",
		AgentOverlays:  map[string]fleetconfig.RoleAgentOverride{},
	}
	for _, c := range n.Children().Nodes {
		handler, ok := agentRoleDefinitionChildParsers[c.Name()]
		if !ok {
			return agentRoleDefinition{}, unknownAgentRoleCatalogNode("agent-roles > role "+name, c.Name(), "tagline | capabilities | modes | default-harness | posture | overlay | merge-authority")
		}
		if err := handler(&def, c, name); err != nil {
			return agentRoleDefinition{}, err
		}
	}
	if strings.TrimSpace(def.Tagline) == "" {
		return agentRoleDefinition{}, fmt.Errorf("agent role catalog: role %q missing tagline (fail-closed)", name)
	}
	if len(def.Capabilities) == 0 {
		return agentRoleDefinition{}, fmt.Errorf("agent role catalog: role %q missing capabilities (fail-closed)", name)
	}
	if strings.TrimSpace(def.Modes) == "" {
		return agentRoleDefinition{}, fmt.Errorf("agent role catalog: role %q missing modes (fail-closed)", name)
	}
	if strings.TrimSpace(def.DefaultHarness) == "" {
		return agentRoleDefinition{}, fmt.Errorf("agent role catalog: role %q missing default-harness (fail-closed)", name)
	}
	if def.Posture == "" {
		return agentRoleDefinition{}, fmt.Errorf("agent role catalog: role %q missing posture (fail-closed)", name)
	}
	return def, nil
}

var agentRoleDefinitionChildParsers = map[string]func(def *agentRoleDefinition, n *kdl.Node, roleName string) error{
	"tagline":         parseAgentRoleDefinitionTagline,
	"capabilities":    parseAgentRoleDefinitionCapabilities,
	"modes":           parseAgentRoleDefinitionModes,
	"default-harness": parseAgentRoleDefinitionDefaultHarness,
	"posture":         parseAgentRoleDefinitionPosture,
	"overlay":         parseAgentRoleDefinitionOverlay,
	"merge-authority": parseAgentRoleDefinitionMergeAuthority,
}

func parseAgentRoleDefinitionTagline(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	v, err := agentRoleStringArg(n, "agent-roles > role > tagline")
	if err != nil {
		return fmt.Errorf("agent role catalog: role %q: %w", roleName, err)
	}
	def.Tagline = v
	return nil
}

func parseAgentRoleDefinitionCapabilities(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	caps, err := agentRoleCapabilitiesArg(n)
	if err != nil {
		return fmt.Errorf("agent role catalog: role %q: %w", roleName, err)
	}
	def.Capabilities = caps
	return nil
}

func parseAgentRoleDefinitionModes(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	v, err := agentRoleStringArg(n, "agent-roles > role > modes")
	if err != nil {
		return fmt.Errorf("agent role catalog: role %q: %w", roleName, err)
	}
	def.Modes = v
	return nil
}

func parseAgentRoleDefinitionDefaultHarness(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	v, err := agentRoleStringArg(n, "agent-roles > role > default-harness")
	if err != nil {
		return fmt.Errorf("agent role catalog: role %q: %w", roleName, err)
	}
	if !frontierAgentNameAllowed(v) {
		return fmt.Errorf("agent role catalog: role %q default-harness %q must be one of %s (fail-closed)", roleName, v, strings.Join(frontierAgentNames(), "|"))
	}
	def.DefaultHarness = v
	return nil
}

func parseAgentRoleDefinitionPosture(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	v, err := agentRoleStringArg(n, "agent-roles > role > posture")
	if err != nil {
		return fmt.Errorf("agent role catalog: role %q: %w", roleName, err)
	}
	posture, ok := parseAgentRolePosture(v)
	if !ok {
		return fmt.Errorf("agent role catalog: role %q posture %q must be detached|attached|no-code|code-landing (fail-closed)", roleName, v)
	}
	def.Posture = posture
	return nil
}

// parseAgentRoleDefinitionMergeAuthority parses the role's PR merge grant (ward#1067):
// only the two PR-carrying modes are grantable, fail-closed. docs/agent-pr-workflow.md.
func parseAgentRoleDefinitionMergeAuthority(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	if len(def.MergeAuthority) != 0 {
		return fmt.Errorf("agent role catalog: role %q repeats merge-authority (fail-closed)", roleName)
	}
	if len(n.Arguments()) == 0 {
		return fmt.Errorf("agent role catalog: role %q merge-authority expects at least one workflow mode (drop the node to withhold merge; fail-closed)", roleName)
	}
	seen := map[workflowMode]bool{}
	for _, arg := range n.Arguments() {
		if arg.Kind() != kdl.String {
			return fmt.Errorf("agent role catalog: role %q merge-authority values must be strings (fail-closed)", roleName)
		}
		mode := workflowMode(strings.TrimSpace(arg.String()))
		if mode != workflowPullRequest && mode != workflowPullRequestAndMerge {
			return fmt.Errorf("agent role catalog: role %q merge-authority %q must be %s or %s - the other workflow modes never merge a pull request (fail-closed)",
				roleName, mode, workflowPullRequest, workflowPullRequestAndMerge)
		}
		if seen[mode] {
			return fmt.Errorf("agent role catalog: role %q merge-authority repeats %q (fail-closed)", roleName, mode)
		}
		seen[mode] = true
		def.MergeAuthority = append(def.MergeAuthority, mode)
	}
	return nil
}

func parseAgentRoleDefinitionOverlay(def *agentRoleDefinition, n *kdl.Node, roleName string) error {
	overlayName, overlay, err := parseAgentRoleOverlayNode(n)
	if err != nil {
		return fmt.Errorf("agent role catalog: role %q: %w", roleName, err)
	}
	if def.AgentOverlays == nil {
		def.AgentOverlays = map[string]fleetconfig.RoleAgentOverride{}
	}
	if _, exists := def.AgentOverlays[overlayName]; exists {
		return fmt.Errorf("agent role catalog: role %q overlay %q repeated (fail-closed)", roleName, overlayName)
	}
	def.AgentOverlays[overlayName] = overlay
	return nil
}

func parseAgentRoleOverlayNode(n *kdl.Node) (string, fleetconfig.RoleAgentOverride, error) {
	name, err := agentRoleStringArg(n, "agent-roles > role > overlay")
	if err != nil {
		return "", fleetconfig.RoleAgentOverride{}, err
	}
	overlay := fleetconfig.RoleAgentOverride{}
	for _, c := range n.Children().Nodes {
		switch c.Name() {
		case "model":
			v, err := agentRoleStringArg(c, "agent-roles > role > overlay > model")
			if err != nil {
				return "", fleetconfig.RoleAgentOverride{}, fmt.Errorf("overlay %q: %w", name, err)
			}
			overlay.Model = v
		case "endpoint":
			v, err := agentRoleStringArg(c, "agent-roles > role > overlay > endpoint")
			if err != nil {
				return "", fleetconfig.RoleAgentOverride{}, fmt.Errorf("overlay %q: %w", name, err)
			}
			overlay.Endpoint = v
		case "reasoning-effort":
			v, err := agentRoleStringArg(c, "agent-roles > role > overlay > reasoning-effort")
			if err != nil {
				return "", fleetconfig.RoleAgentOverride{}, fmt.Errorf("overlay %q: %w", name, err)
			}
			overlay.ReasoningEffort = v
		case "verbosity":
			v, err := agentRoleStringArg(c, "agent-roles > role > overlay > verbosity")
			if err != nil {
				return "", fleetconfig.RoleAgentOverride{}, fmt.Errorf("overlay %q: %w", name, err)
			}
			overlay.Verbosity = v
		default:
			return "", fleetconfig.RoleAgentOverride{}, unknownAgentRoleCatalogNode("agent-roles > role > overlay "+name, c.Name(), "model | endpoint | reasoning-effort | verbosity")
		}
	}
	return name, overlay, nil
}

func agentRoleCapabilitiesArg(n *kdl.Node) (semanticCapabilitySet, error) {
	if len(n.Arguments()) == 0 {
		return semanticCapabilitySet{}, fmt.Errorf("agent role catalog: agent-roles > role > capabilities expects at least one capability (fail-closed)")
	}
	names := make([]string, 0, len(n.Arguments()))
	for _, arg := range n.Arguments() {
		if arg.Kind() != kdl.String {
			return semanticCapabilitySet{}, fmt.Errorf("agent role catalog: agent-roles > role > capabilities values must be strings (fail-closed)")
		}
		names = append(names, strings.TrimSpace(arg.String()))
	}
	caps, err := semanticCapabilitiesFromNames(names...)
	if err != nil {
		return semanticCapabilitySet{}, err
	}
	return caps, nil
}

func agentRoleStringArg(n *kdl.Node, label string) (string, error) {
	args := n.Arguments()
	if len(args) != 1 {
		return "", fmt.Errorf("agent role catalog: %s expects exactly one value, got %d (fail-closed)", label, len(args))
	}
	if args[0].Kind() != kdl.String {
		return "", fmt.Errorf("agent role catalog: %s must be a string (fail-closed)", label)
	}
	v := strings.TrimSpace(args[0].String())
	if v == "" {
		return "", fmt.Errorf("agent role catalog: %s must be non-empty (fail-closed)", label)
	}
	return v, nil
}

func parseAgentRolePosture(raw string) (agentRolePosture, bool) {
	switch agentRolePosture(strings.TrimSpace(raw)) {
	case agentRolePostureDetached, agentRolePostureAttached, agentRolePostureNoCode, agentRolePostureCodeLanding:
		return agentRolePosture(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

func frontierAgentNameAllowed(name string) bool {
	for _, candidate := range frontierAgentNames() {
		if candidate == name {
			return true
		}
	}
	return false
}

func unknownAgentRoleCatalogNode(where, name, want string) error {
	return fmt.Errorf("agent role catalog: %s: unknown node %q (want %s; fail-closed)", where, name, want)
}
