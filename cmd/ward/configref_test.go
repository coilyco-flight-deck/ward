package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// configref_test.go covers the ward#654 grammar: the pure WARD_CONFIG_REF
// parser plus the cache-root / TTL plumbing of the config-bundle resolver.

// TestParseConfigRef pins the <host>/<owner>/<repo>[@<ref>]//<subpath>
// grammar, parseConfigOverrides-style: table in, loud errors out.
func TestParseConfigRef(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want configRef
		err  string // non-empty: an error containing this
	}{
		{
			name: "full form",
			raw:  "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os@main//.ward",
			want: configRef{repoSpec: "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os", ref: "main", subpath: ".ward"},
		},
		{
			name: "no ref tracks the remote default branch",
			raw:  "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os//.ward",
			want: configRef{repoSpec: "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os", ref: "", subpath: ".ward"},
		},
		{
			name: "sha ref passes to git untouched",
			raw:  "github.com/owner/repo@8428c38//bundle",
			want: configRef{repoSpec: "github.com/owner/repo", ref: "8428c38", subpath: "bundle"},
		},
		{
			name: "slash in the ref (first // still splits the subpath)",
			raw:  "github.com/owner/repo@feature/x//bundle",
			want: configRef{repoSpec: "github.com/owner/repo", ref: "feature/x", subpath: "bundle"},
		},
		{
			name: "empty subpath means the repo root",
			raw:  "github.com/owner/repo@v1//",
			want: configRef{repoSpec: "github.com/owner/repo", ref: "v1", subpath: "."},
		},
		{
			name: "nested subpath cleans",
			raw:  "github.com/owner/repo//a//b/",
			want: configRef{repoSpec: "github.com/owner/repo", ref: "", subpath: "a/b"},
		},
		{name: "no // separator", raw: "github.com/owner/repo@main", err: "no `//` before the subpath"},
		{name: "empty ref after @", raw: "github.com/owner/repo@//x", err: "empty ref"},
		{name: "two-segment repo-spec", raw: "owner/repo//x", err: "want exactly <host>/<owner>/<repo>"},
		{name: "four-segment repo-spec", raw: "host/org/sub/repo//x", err: "want exactly <host>/<owner>/<repo>"},
		{name: "empty owner", raw: "host//repo//x", err: "want exactly <host>/<owner>/<repo>"},
		{name: "scheme prefix is not part of the grammar", raw: "https://host/owner/repo//x", err: "want exactly <host>/<owner>/<repo>"},
		{name: "subpath escaping the checkout", raw: "host/owner/repo//../etc", err: "escapes the checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConfigRef(tc.raw)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("parseConfigRef(%q) err = %v, want it to contain %q", tc.raw, err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfigRef(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("parseConfigRef(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestConfigRefCloneURL pins the https form the repo-spec renders to.
func TestConfigRefCloneURL(t *testing.T) {
	cr := configRef{repoSpec: "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os"}
	want := "https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os.git"
	if got := cr.cloneURL(); got != want {
		t.Errorf("cloneURL() = %q, want %q", got, want)
	}
}

// TestConfigBundleCacheRoot pins the split: the shared gitcache volume in a
// container, ~/.cache/ward on a host.
func TestConfigBundleCacheRoot(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	gc := t.TempDir()
	got, err := configBundleCacheRoot(env(map[string]string{"WARD_CONTAINER": "1", "WARD_GITCACHE": gc, "WARD_CONTAINER_NAME": "director-1234"}))
	if err != nil || got != filepath.Join(gc, "config-bundle", "director-1234") {
		t.Errorf("container root = %q (%v), want %s/config-bundle/director-1234", got, err, gc)
	}
	if !isDir(got) {
		t.Errorf("container root %q was not created", got)
	}
	got, err = configBundleCacheRoot(env(nil))
	if err != nil || !strings.HasSuffix(got, filepath.Join("ward", "config-bundle")) {
		t.Errorf("host root = %q (%v), want a .../ward/config-bundle", got, err)
	}
	// A broken container volume path degrades to the home cache rather than
	// bricking the ref.
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.WriteFile(ro, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = configBundleCacheRoot(env(map[string]string{"WARD_CONTAINER": "1", "WARD_GITCACHE": ro}))
	if err != nil || !strings.HasSuffix(got, filepath.Join("ward", "config-bundle")) {
		t.Errorf("unwritable-volume root = %q (%v), want the home-cache fallback", got, err)
	}
	if runtime.GOOS != "windows" {
		locked := filepath.Join(t.TempDir(), "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0o555); err != nil {
			t.Fatal(err)
		}
		got, err = configBundleCacheRoot(env(map[string]string{"WARD_CONTAINER": "1", "WARD_GITCACHE": locked}))
		if err != nil || !strings.HasSuffix(got, filepath.Join("ward", "config-bundle")) {
			t.Errorf("read-only container root = %q (%v), want the home-cache fallback", got, err)
		}
	}
}

// TestConfigBundleTTL pins WARD_CONFIG_TTL seconds with the substrate default.
func TestConfigBundleTTL(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == wardConfigTTLEnv {
				return v
			}
			return ""
		}
	}
	if got := configBundleTTL(env("")); got != 600*time.Second {
		t.Errorf("default TTL = %v, want 600s", got)
	}
	if got := configBundleTTL(env("30")); got != 30*time.Second {
		t.Errorf("TTL(30) = %v, want 30s", got)
	}
	if got := configBundleTTL(env("0")); got != 0 {
		t.Errorf("TTL(0) = %v, want 0", got)
	}
	if got := configBundleTTL(env("junk")); got != 600*time.Second {
		t.Errorf("TTL(junk) = %v, want the 600s default", got)
	}
}

// TestHashConfigRef pins the cache key: short, hex, distinct per raw ref.
func TestHashConfigRef(t *testing.T) {
	a := hashConfigRef("host/owner/repo@main//.ward")
	b := hashConfigRef("host/owner/repo@v2//.ward")
	if len(a) != 16 || a == b {
		t.Errorf("hashConfigRef: got %q vs %q, want 16-char distinct keys", a, b)
	}
}
