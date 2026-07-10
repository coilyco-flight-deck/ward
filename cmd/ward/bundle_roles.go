package main

import (
	"fmt"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	kdl "github.com/calico32/kdl-go"
)

func parseBundleRoles(src []byte) ([]fleetconfig.Role, error) {
	doc, err := kdl.ParseString(string(src))
	if err != nil {
		return nil, fmt.Errorf("fleet config: parse KDL: %w", err)
	}
	if len(doc.Nodes) != 1 {
		return nil, fmt.Errorf("fleet config: split roles bundle needs one top-level `roles` block (fail-closed)")
	}
	root := doc.Nodes[0]
	if root.Name() != "roles" {
		return nil, fmt.Errorf("fleet config: split roles bundle needs a top-level `roles` block (found %q; fail-closed)", root.Name())
	}
	if len(root.Arguments()) != 0 {
		return nil, fmt.Errorf("fleet config: `roles` takes no arguments, only a block (fail-closed)")
	}
	if len(root.Properties()) != 0 {
		return nil, fmt.Errorf("fleet config: `roles` takes no properties (fail-closed)")
	}
	out := make([]fleetconfig.Role, 0, len(root.Children().Nodes))
	seen := map[string]bool{}
	for _, c := range root.Children().Nodes {
		if c.Name() != "role" {
			return nil, fmt.Errorf("fleet config: unknown node %q in roles body (want role; fail-closed)", c.Name())
		}
		role, err := parseBundleRole(c)
		if err != nil {
			return nil, err
		}
		if seen[role.Name] {
			return nil, fmt.Errorf("fleet config: duplicate role %q (fail-closed)", role.Name)
		}
		seen[role.Name] = true
		out = append(out, role)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fleet config: `roles` block declares no `role` (nothing to key capability on; fail-closed)")
	}
	return out, nil
}

func parseBundleRole(n *kdl.Node) (fleetconfig.Role, error) {
	name, err := bundleSingleStringArg(n, "role")
	if err != nil {
		return fleetconfig.Role{}, fmt.Errorf("fleet config: `role` needs a single name, e.g. `role advisor`: %w", err)
	}
	role := fleetconfig.Role{Name: name}
	for _, c := range n.Children().Nodes {
		if err := applyBundleRoleChild(&role, name, c); err != nil {
			return fleetconfig.Role{}, err
		}
	}
	return role, nil
}

func applyBundleRoleChild(role *fleetconfig.Role, name string, c *kdl.Node) error {
	switch c.Name() {
	case "guardfile":
		return applyBundleRoleGuardfile(role, name, c)
	case "guardfiles":
		return applyBundleRoleGuardfiles(role, name, c)
	case "agent":
		return applyBundleRoleAgentOverride(role, name, c)
	default:
		return fmt.Errorf("fleet config: unknown node %q in role %q body (want guardfile | guardfiles | agent; fail-closed)", c.Name(), name)
	}
}

func applyBundleRoleGuardfile(role *fleetconfig.Role, name string, c *kdl.Node) error {
	gf, err := bundleSingleStringArg(c, "role guardfile")
	if err != nil {
		return fmt.Errorf("fleet config: role %q: %w", name, err)
	}
	if role.Guardfiles.Prefix != "" {
		return fmt.Errorf("fleet config: role %q mixes `guardfile` and `guardfiles` (fail-closed)", name)
	}
	role.Guardfiles.List = append(role.Guardfiles.List, gf)
	return nil
}

func applyBundleRoleGuardfiles(role *fleetconfig.Role, name string, c *kdl.Node) error {
	gf, err := bundleGuardfiles(c, name)
	if err != nil {
		return err
	}
	if len(role.Guardfiles.List) != 0 || role.Guardfiles.Prefix != "" {
		return fmt.Errorf("fleet config: role %q has duplicate guardfile bindings (fail-closed)", name)
	}
	role.Guardfiles = gf
	return nil
}

func applyBundleRoleAgentOverride(role *fleetconfig.Role, name string, c *kdl.Node) error {
	an, ov, err := parseBundleRoleAgent(c, name)
	if err != nil {
		return err
	}
	if role.AgentConfig == nil {
		role.AgentConfig = map[string]fleetconfig.RoleAgentOverride{}
	}
	if _, dup := role.AgentConfig[an]; dup {
		return fmt.Errorf("fleet config: role %q has a duplicate `agent %s` override (fail-closed)", name, an)
	}
	role.AgentConfig[an] = ov
	return nil
}

func parseBundleRoleAgent(n *kdl.Node, role string) (string, fleetconfig.RoleAgentOverride, error) {
	name, err := bundleSingleStringArg(n, "role agent")
	if err != nil {
		return "", fleetconfig.RoleAgentOverride{}, fmt.Errorf("fleet config: role %q `agent` needs a single name, e.g. `agent claude`: %w", role, err)
	}
	var o fleetconfig.RoleAgentOverride
	fields := map[string]*string{
		"model":            &o.Model,
		"endpoint":         &o.Endpoint,
		"reasoning-effort": &o.ReasoningEffort,
		"verbosity":        &o.Verbosity,
	}
	for _, c := range n.Children().Nodes {
		dst, ok := fields[c.Name()]
		if !ok {
			return "", fleetconfig.RoleAgentOverride{}, fmt.Errorf("fleet config: unknown node %q in role %q agent %q body (want model | endpoint | reasoning-effort | verbosity; fail-closed)", c.Name(), role, name)
		}
		v, verr := bundleSingleStringArg(c, fmt.Sprintf("role %q agent %q > %s", role, name, c.Name()))
		if verr != nil {
			return "", fleetconfig.RoleAgentOverride{}, verr
		}
		*dst = v
	}
	return name, o, nil
}

func bundleGuardfiles(n *kdl.Node, role string) (fleetconfig.Guardfiles, error) {
	args := n.Arguments()
	prefix := ""
	for k, v := range n.Properties() {
		if k != "prefix" {
			return fleetconfig.Guardfiles{}, fmt.Errorf("fleet config: role %q `guardfiles` has unknown property %q (want prefix; fail-closed)", role, k)
		}
		prefix = strings.TrimSpace(v.String())
	}
	switch {
	case len(args) > 0 && prefix != "":
		return fleetconfig.Guardfiles{}, fmt.Errorf("fleet config: role %q `guardfiles` is a flat list OR a prefix=, not both (fail-closed)", role)
	case prefix != "":
		return fleetconfig.Guardfiles{Prefix: prefix}, nil
	case len(args) > 0:
		out := make([]string, 0, len(args))
		for _, arg := range args {
			if arg.Kind() != kdl.String {
				return fleetconfig.Guardfiles{}, fmt.Errorf("fleet config: role %q `guardfiles` entries must be strings (fail-closed)", role)
			}
			out = append(out, strings.TrimSpace(arg.String()))
		}
		return fleetconfig.Guardfiles{List: out}, nil
	default:
		return fleetconfig.Guardfiles{}, fmt.Errorf("fleet config: role %q `guardfiles` needs a flat list of names or a prefix= (an empty node is ambiguous; fail-closed)", role)
	}
}

func bundleSingleStringArg(n *kdl.Node, label string) (string, error) {
	args := n.Arguments()
	if len(args) != 1 {
		return "", fmt.Errorf("`%s` expects exactly one value, got %d", label, len(args))
	}
	if args[0].Kind() != kdl.String {
		return "", fmt.Errorf("`%s` must be a string", label)
	}
	return strings.TrimSpace(args[0].String()), nil
}
