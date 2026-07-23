package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two spellings of a raw Forgejo-token env read: bare literal and the
// cli-guard credseed constant. Both bypass the resolver chokepoints.
var rawForgejoTokenReads = []string{
	`os.Getenv("FORGEJO_TOKEN")`,
	`os.Getenv(credseed.EnvForgejoToken)`,
}

// rawForgejoTokenSites is the audited allowlist of files permitted a raw read.
// Keep it in lockstep with docs/forgejo-token-audit.md (ward#239).
var rawForgejoTokenSites = map[string]string{
	// Resolver chokepoints - the sanctioned path every forge consumer funnels through.
	"ops.go":       "forgejoTokenResolver: the ward ops forgejo auth provider (director broker, else env/SSM)",
	"container.go": "resolveForgejoToken: host->container seed, broker-first then env/SSM",
	// Root-only plumbing - runs outside the dropped-agent boundary.
	"broker.go":              "the root daemon that holds the token and serves the write tier",
	"container_bootstrap.go": "the PID-1 entrypoint seeding the git-credential store",
	"container_reap.go":      "the reaper filing salvage/reservation writes on teardown",
}

// TestNoNewRawForgejoTokenReads fails when a non-test .go file reads the raw
// Forgejo token and is not audited (ward#239). See docs/forgejo-token-audit.md.
func TestNoNewRawForgejoTokenReads(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	found := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, read := range rawForgejoTokenReads {
			if strings.Contains(string(src), read) {
				found[name] = true
				break
			}
		}
	}

	// The regression guard: a raw read in a file the allowlist does not sanction.
	for name := range found {
		if _, ok := rawForgejoTokenSites[name]; !ok {
			t.Errorf("%s reads the raw Forgejo token but is not an audited site. Route through "+
				"forgejoTokenResolver / resolveForgejoToken (broker-first), or add it to "+
				"rawForgejoTokenSites + docs/forgejo-token-audit.md. See ward#239.", name)
		}
	}

	// Keep the audit honest: an allowlist entry whose file no longer reads the token.
	for name := range rawForgejoTokenSites {
		if !found[name] {
			t.Errorf("%s is allowlisted but no longer reads the raw token; drop it from "+
				"rawForgejoTokenSites + docs/forgejo-token-audit.md. See ward#239.", name)
		}
	}
}
