package main

// fleet.go parses ward's dialect-2 fleet config from ward-owned runtime data.
// Edge bundle selection stays on the configsource seam, core agents stay baked.

import (
	"fmt"
	"io/fs"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	kdl "github.com/calico32/kdl-go"
)

// loadFleetConfig resolves ward's built-in frontier defaults over the baked
// fleet config and fails loud only on baked drift or parse failure.
func loadFleetConfig() (fleetconfig.Fleet, error) {
	return loadFleetConfigFrom(coreRuntimeConfigSource())
}

func loadFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	raw, err := loadRawFleetConfigFrom(src)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	return resolveEffectiveFleet(raw)
}

func loadRawFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	if src.fleetKDL != "" {
		b, err := fs.ReadFile(src.fsys, src.fleetKDL)
		if err != nil {
			return fleetconfig.Fleet{}, fmt.Errorf("read fleet config %s: %w", src.fleetKDL, err)
		}
		return fleetconfig.Parse(b)
	}
	return loadBundleFleetConfigFrom(src)
}

func loadBundleFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	files, err := loadBundleKDLFiles(src)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	sel, err := selectBundleFleetNodes(files)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	return parseBundleFleetSelection(sel)
}

type bundleFleetSelection struct {
	descNode     *kdl.Node
	directorNode *kdl.Node
	fleetNode    *kdl.Node
	agentsNode   *kdl.Node
	rolesNode    *kdl.Node
}

func selectBundleFleetNodes(files []bundleKDLFile) (bundleFleetSelection, error) {
	_, descNode, _, err := findOptionalNamedBundleNode(files, "top-level `description` block", "description")
	if err != nil {
		return bundleFleetSelection{}, err
	}
	_, directorNode, _, err := findOptionalNamedBundleNode(files, "top-level `director` block", "director")
	if err != nil {
		return bundleFleetSelection{}, err
	}
	fleetFile, fleetNode, fleetOK, err := findOptionalNamedBundleNode(files, "top-level `fleet` block", "fleet")
	if err != nil {
		return bundleFleetSelection{}, err
	}
	agentsFile, agentsNode, agentsOK, err := findOptionalNamedBundleNode(files, "top-level `agents` block", "agents")
	if err != nil {
		return bundleFleetSelection{}, err
	}
	rolesFile, rolesNode, rolesOK, err := findOptionalNamedBundleNode(files, "top-level `roles` block", "roles")
	if err != nil {
		return bundleFleetSelection{}, err
	}
	if fleetOK && (agentsOK || rolesOK) {
		conflictPath := agentsFile.path
		if !agentsOK {
			conflictPath = rolesFile.path
		}
		return bundleFleetSelection{}, fmt.Errorf("bundle: conflicting fleet layouts in %s and %s (fail-closed)", fleetFile.path, conflictPath)
	}
	if !fleetOK && !agentsOK {
		return bundleFleetSelection{}, fmt.Errorf("bundle: missing top-level `agents` or `fleet` block (fail-closed)")
	}
	sel := bundleFleetSelection{
		descNode:     descNode,
		directorNode: directorNode,
		fleetNode:    fleetNode,
		agentsNode:   agentsNode,
		rolesNode:    rolesNode,
	}
	return sel, nil
}

func parseBundleFleetSelection(sel bundleFleetSelection) (fleetconfig.Fleet, error) {
	nodes := make([]*kdl.Node, 0, 3)
	if sel.descNode != nil {
		nodes = append(nodes, sel.descNode)
	}
	if sel.directorNode != nil {
		nodes = append(nodes, sel.directorNode)
	}
	if sel.fleetNode != nil {
		nodes = append(nodes, sel.fleetNode)
		return parseBundleFleetNodes(nodes)
	}
	fleet := kdl.NewNode("fleet")
	fleet.AddChildren(sel.agentsNode.Children().Nodes...)
	if sel.rolesNode != nil {
		roles := kdl.NewNode("roles")
		roles.AddChildren(sel.rolesNode.Children().Nodes...)
		fleet.AddChild(roles)
	}
	nodes = append(nodes, fleet)
	return parseBundleFleetNodes(nodes)
}

func parseBundleFleetNodes(nodes []*kdl.Node) (fleetconfig.Fleet, error) {
	srcBytes, err := emitKDLDocument(nodes...)
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("emit fleet bundle: %w", err)
	}
	return fleetconfig.Parse(srcBytes)
}
