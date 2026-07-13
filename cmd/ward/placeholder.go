package main

import (
	kdl "github.com/calico32/kdl-go"
)

// placeholderAnnotation marks a value as a sentinel rather than real config.
// ward uses sentinels so generic guardfiles can stay example-safe.
const placeholderAnnotation = "placeholder"

func isPlaceholderNode(n *kdl.Node) bool {
	if n == nil {
		return false
	}
	typ, ok := n.TypeAnnotation()
	return ok && typ == placeholderAnnotation
}

// resolvePlaceholderSentinels drops sentinel children when a real sibling exists.
// The merge is order-independent so a placeholder can never beat a real value.
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
