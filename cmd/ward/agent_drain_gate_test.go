package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// agent_drain_gate_test.go is the ward#425 ratchet: it fails on a per-agent name
// as a string literal in core cmd/ward outside a tiny allowlist. docs/agentsapi.md.

// drainAgentLiterals are the per-agent roster names that belong in the folders
// (internal/agents/<name>), not scattered through core as string literals.
var drainAgentLiterals = map[string]bool{
	"claude": true, "codex": true, "goose": true,
	"opencode": true, "qwen": true, "ollama": true,
}

// drainGateAllowlist is the tiny set of core files that legitimately carry
// per-agent literals: the manifest reader plus the signature/review exceptions.
var drainGateAllowlist = map[string]bool{
	"agent_adapter.go":   true, // manifest reader + runtime adapter projection
	"agent_signature.go": true, // roster identity/signer defaults
	"agent_review.go":    true, // review-panel roster: which families review + their availability preconditions (ward#134)
}

// TestNoPerAgentLiteralsInCore walks every core cmd/ward non-test .go file and
// flags an agent-name string literal outside the allowlist (ward#425 ratchet).
func TestNoPerAgentLiteralsInCore(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if drainGateAllowlist[name] {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if drainAgentLiterals[strings.ToLower(val)] {
				violations = append(violations, fset.Position(lit.Pos()).String()+": per-agent literal "+lit.Value)
			}
			return true
		})
	}
	if len(violations) > 0 {
		t.Errorf("per-agent string literals must live in internal/agents/<name>, not core (ward#425). Move the body into its folder or, for a legitimately shared mode/roster switch, add the file to drainGateAllowlist:\n  %s", strings.Join(violations, "\n  "))
	}
}
