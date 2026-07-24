package main

import (
	"slices"

	kdl "github.com/calico32/kdl-go"
)

// placeholderAnnotation marks a sentinel, e.g. `(placeholder)base-url "example.com"`:
// ward embeds no real config, and sentinels let doctor fail an unsupplied deployment.
const placeholderAnnotation = "placeholder"

func isPlaceholderValue(v kdl.Value) bool {
	typ, ok := v.TypeAnnotation()
	return ok && typ == placeholderAnnotation
}

func nodeHasPlaceholderSentinel(n *kdl.Node) bool {
	if n == nil {
		return false
	}
	if typ, ok := n.TypeAnnotation(); ok && typ == placeholderAnnotation {
		return true
	}
	for _, arg := range n.Arguments() {
		if isPlaceholderValue(arg) {
			return true
		}
	}
	for _, entry := range n.PropertyEntries() {
		if isPlaceholderValue(entry.Value) {
			return true
		}
	}
	for _, child := range n.Children().Nodes {
		if nodeHasPlaceholderSentinel(child) {
			return true
		}
	}
	return false
}

func nodeEquivalentIgnoringPlaceholders(a, b *kdl.Node) bool {
	return a != nil && b != nil &&
		a.Name() == b.Name() &&
		annotationCompatible(a, b) &&
		nodeArgumentsCompatibleIgnoringPlaceholders(a.Arguments(), b.Arguments()) &&
		nodePropertiesCompatibleIgnoringPlaceholders(a.Properties(), b.Properties()) &&
		nodeChildrenCompatibleIgnoringPlaceholders(a.Children().Nodes, b.Children().Nodes)
}

func nodeArgumentsCompatibleIgnoringPlaceholders(aArgs, bArgs []kdl.Value) bool {
	if len(aArgs) != len(bArgs) {
		return false
	}
	for i := range aArgs {
		if !valueCompatibleIgnoringPlaceholders(aArgs[i], bArgs[i]) {
			return false
		}
	}
	return true
}

func nodePropertiesCompatibleIgnoringPlaceholders(aProps, bProps map[string]kdl.Value) bool {
	if len(aProps) != len(bProps) {
		return false
	}
	for key, aVal := range aProps {
		bVal, ok := bProps[key]
		if !ok || !valueCompatibleIgnoringPlaceholders(aVal, bVal) {
			return false
		}
	}
	return true
}

func nodeChildrenCompatibleIgnoringPlaceholders(aChildren, bChildren []*kdl.Node) bool {
	if len(aChildren) != len(bChildren) {
		return false
	}
	for i := range aChildren {
		if !nodeEquivalentIgnoringPlaceholders(aChildren[i], bChildren[i]) {
			return false
		}
	}
	return true
}

func annotationCompatible(a, b *kdl.Node) bool {
	annotA, okA := a.TypeAnnotation()
	annotB, okB := b.TypeAnnotation()
	switch {
	case okA && annotA == placeholderAnnotation:
		return true
	case okB && annotB == placeholderAnnotation:
		return true
	case okA != okB:
		return false
	default:
		return annotA == annotB
	}
}

func valueCompatibleIgnoringPlaceholders(a, b kdl.Value) bool {
	if isPlaceholderValue(a) || isPlaceholderValue(b) {
		return true
	}
	if a.Kind() != b.Kind() {
		return false
	}
	return a.String() == b.String()
}

func mergePlaceholderAwareChildren(children []*kdl.Node, incoming ...*kdl.Node) []*kdl.Node {
	out := slices.Clone(children)
	for _, node := range incoming {
		out = mergePlaceholderAwareChild(out, node)
	}
	return out
}

func mergePlaceholderAwareChild(children []*kdl.Node, incoming *kdl.Node) []*kdl.Node {
	if incoming == nil {
		return children
	}
	incomingPlaceholder := nodeHasPlaceholderSentinel(incoming)
	if incomingPlaceholder {
		for _, existing := range children {
			if nodeEquivalentIgnoringPlaceholders(existing, incoming) && !nodeHasPlaceholderSentinel(existing) {
				return children
			}
		}
		return append(children, incoming)
	}

	out := make([]*kdl.Node, 0, len(children)+1)
	for _, existing := range children {
		if nodeEquivalentIgnoringPlaceholders(existing, incoming) && nodeHasPlaceholderSentinel(existing) {
			continue
		}
		out = append(out, existing)
	}
	return append(out, incoming)
}

// resolvePlaceholderSentinels drops sentinels when a real equivalent sibling
// supplies one, in either merge order. Unmatched sentinels survive.
func resolvePlaceholderSentinels(n *kdl.Node) {
	if n == nil {
		return
	}
	children := n.Children()
	if children == nil {
		return
	}
	children.Nodes = mergePlaceholderAwareChildren(nil, children.Nodes...)
}
