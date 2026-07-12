package main

import (
	kdl "github.com/calico32/kdl-go"
)

// placeholderAnnotation marks a value as a sentinel rather than real config:
//
//	(placeholder)base-url "git.example.com/api/v1"
//
// ward must never embed real deployment config, so its generic guardfiles have
// to name *some* host, token, and owner gate. Marking those as sentinels is what
// lets `ward doctor` fail a deployment that never supplied its own.
const placeholderAnnotation = "placeholder"

func isPlaceholderNode(n *kdl.Node) bool {
	if n == nil {
		return false
	}
	typ, ok := n.TypeAnnotation()
	return ok && typ == placeholderAnnotation
}

// resolvePlaceholderSentinels drops every sentinel child that a real sibling of
// the same name already supplies.
//
// This is order-independent on purpose. The bundle merge concatenates children
// across files, and cli-guard's singletons are last-wins (applyBaseURL keeps the
// last non-empty, `gf.Auth = a` overwrites). So whichever file the walk happened
// to reach last decided the surface: a generic guardfile's sentinel base-url
// could beat the deployment's real one while its auth lost, pairing an example
// host with a live credential. A sentinel that can win a merge is worse than no
// sentinel at all.
//
// A sentinel with no real counterpart survives, so doctor still fails on it.
func resolvePlaceholderSentinels(n *kdl.Node) {
	if n == nil {
		return
	}
	children := n.Children()
	if children == nil {
		return
	}
	supplied := map[string]bool{}
	for _, c := range children.Nodes {
		if !isPlaceholderNode(c) {
			supplied[c.Name()] = true
		}
	}
	kept := children.Nodes[:0:0]
	for _, c := range children.Nodes {
		if isPlaceholderNode(c) && supplied[c.Name()] {
			continue
		}
		kept = append(kept, c)
	}
	children.Nodes = kept
}
