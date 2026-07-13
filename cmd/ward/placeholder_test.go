package main

import (
	"testing"

	kdl "github.com/calico32/kdl-go"
)

func mergedWrap(t *testing.T, src string) *kdl.Node {
	t.Helper()
	doc, err := kdl.ParseString(src)
	if err != nil {
		t.Fatalf("parse kdl: %v", err)
	}
	if len(doc.Nodes) != 1 {
		t.Fatalf("want one top-level node, got %d", len(doc.Nodes))
	}
	return doc.Nodes[0]
}

func childValues(n *kdl.Node, name string) []string {
	var out []string
	for _, c := range n.Children().Nodes {
		if c.Name() != name {
			continue
		}
		if args := c.Arguments(); len(args) > 0 {
			out = append(out, args[0].String())
		}
	}
	return out
}

// A sentinel must lose to a real value no matter which side of the merge it
// lands on.
func TestPlaceholderSentinelYieldsToRealValueEitherOrder(t *testing.T) {
	for name, src := range map[string]string{
		"sentinel last": `wrap ward-kdl ops forgejo {
    base-url "forgejo.coilysiren.me/api/v1"
    (placeholder)base-url "git.example.com/api/v1"
}`,
		"sentinel first": `wrap ward-kdl ops forgejo {
    (placeholder)base-url "git.example.com/api/v1"
    base-url "forgejo.coilysiren.me/api/v1"
}`,
	} {
		t.Run(name, func(t *testing.T) {
			n := mergedWrap(t, src)
			resolvePlaceholderSentinels(n)
			got := childValues(n, "base-url")
			if len(got) != 1 || got[0] != "forgejo.coilysiren.me/api/v1" {
				t.Fatalf("the real base-url must be the only survivor, got %v", got)
			}
		})
	}
}

// With nothing to override it, the sentinel survives so doctor still fails the
// deployment that never supplied its own value.
func TestPlaceholderSentinelSurvivesWhenNothingSuppliesIt(t *testing.T) {
	n := mergedWrap(t, `wrap ward-kdl ops forgejo {
    (placeholder)base-url "git.example.com/api/v1"
}`)
	resolvePlaceholderSentinels(n)
	got := childValues(n, "base-url")
	if len(got) != 1 || got[0] != "git.example.com/api/v1" {
		t.Fatalf("an unopposed sentinel must survive so doctor can fail on it, got %v", got)
	}
}

// Only the marked node yields. An unmarked duplicate is a real conflict and
// must still reach cli-guard.
func TestPlaceholderResolutionLeavesUnmarkedNodesAlone(t *testing.T) {
	n := mergedWrap(t, `wrap ward-kdl ops forgejo {
    base-url "a.example/api/v1"
    base-url "b.example/api/v1"
    can get repo
}`)
	resolvePlaceholderSentinels(n)
	if got := childValues(n, "base-url"); len(got) != 2 {
		t.Fatalf("unmarked duplicates must survive untouched, got %v", got)
	}
	if len(n.Children().Nodes) != 3 {
		t.Fatalf("verbs must survive, got %d children", len(n.Children().Nodes))
	}
}
