package main

import (
	kdl "github.com/calico32/kdl-go"
)

// placeholderAnnotation marks a sentinel, e.g. `(placeholder)base-url "example.com"`:
// ward embeds no real config, and sentinels let doctor fail an unsupplied deployment.
const placeholderAnnotation = "placeholder"

func isPlaceholderNode(n *kdl.Node) bool {
	if n == nil {
		return false
	}
	typ, ok := n.TypeAnnotation()
	return ok && typ == placeholderAnnotation
}

// resolvePlaceholderSentinels drops each sentinel child a real same-name sibling
// supplies, in either merge order. Unmatched sentinels survive so doctor still fails.
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
