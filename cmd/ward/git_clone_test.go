package main

import (
	"context"
	"path/filepath"
	"testing"
)

// noEnv is a getenv stub with no TMPDIR set, so ephemeral roots reduce to /tmp
// and the platform temp dir.
func noEnv(string) string { return "" }

func TestSplitCloneArgs(t *testing.T) {
	cases := []struct {
		name    string
		argv    []string
		url     string
		dir     string
		wantErr bool
	}{
		{"url only", []string{"https://h/o/r.git"}, "https://h/o/r.git", "", false},
		{"url and dir", []string{"https://h/o/r.git", "dest"}, "https://h/o/r.git", "dest", false},
		{"bool flags skipped", []string{"--bare", "-q", "https://h/o/r.git"}, "https://h/o/r.git", "", false},
		{"value flag eats next", []string{"--depth", "1", "https://h/o/r.git", "dest"}, "https://h/o/r.git", "dest", false},
		{"branch value flag", []string{"-b", "main", "git@h:o/r.git"}, "git@h:o/r.git", "", false},
		{"attached value form", []string{"--depth=1", "https://h/o/r.git"}, "https://h/o/r.git", "", false},
		{"dashdash terminator", []string{"--", "https://h/o/r.git", "dest"}, "https://h/o/r.git", "dest", false},
		{"no positionals", []string{"--bare"}, "", "", true},
		{"too many positionals", []string{"a", "b", "c"}, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, dir, err := splitCloneArgs(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %v", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tc.url || dir != tc.dir {
				t.Fatalf("got url=%q dir=%q, want url=%q dir=%q", url, dir, tc.url, tc.dir)
			}
		})
	}
}

func TestRepoFromURL(t *testing.T) {
	cases := []struct {
		raw         string
		owner, name string
		ok          bool
	}{
		{"https://forgejo.coilysiren.me/coilyco-flight-deck/ward.git", "coilyco-flight-deck", "ward", true},
		{"https://github.com/coilysiren/coilysiren", "coilysiren", "coilysiren", true},
		{"git@github.com:coilyco-bridge/lore.git", "coilyco-bridge", "lore", true},
		{"ssh://git@host:2222/Owner/Name.git", "owner", "name", true},
		{"git@host:o/r", "o", "r", true},
		{"/local/path/repo", "", "", false},
		{"./repo", "", "", false},
		{"file:///srv/git/repo.git", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			owner, name, ok := repoFromURL(tc.raw)
			if ok != tc.ok || owner != tc.owner || name != tc.name {
				t.Fatalf("repoFromURL(%q) = (%q,%q,%v), want (%q,%q,%v)",
					tc.raw, owner, name, ok, tc.owner, tc.name, tc.ok)
			}
		})
	}
}

func TestHumanishName(t *testing.T) {
	cases := map[string]string{
		"https://h/o/repo.git":  "repo",
		"https://h/o/repo/":     "repo",
		"git@h:o/repo.git":      "repo",
		"https://h/o/repo":      "repo",
		"/local/path/thing.git": "thing",
	}
	for in, want := range cases {
		if got := humanishName(in); got != want {
			t.Errorf("humanishName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloneGate(t *testing.T) {
	tmp := filepath.Join(t.TempDir()) // an existing dir under the platform temp root
	persistent := "/home/agent/projects"

	cases := []struct {
		name    string
		url     string
		dest    string
		allowed bool
	}{
		{"ephemeral dest, off-allowlist repo", "https://evil.example/x/y.git", filepath.Join("/tmp", "y"), true},
		{"platform-temp dest, off-allowlist repo", "https://evil.example/x/y.git", filepath.Join(tmp, "y"), true},
		{"persistent dest, allowlisted repo", "https://forgejo.coilysiren.me/coilyco-flight-deck/ward.git", filepath.Join(persistent, "ward"), true},
		{"persistent dest, off-allowlist repo", "https://evil.example/x/y.git", filepath.Join(persistent, "y"), false},
		{"persistent dest, local-path url", "/some/local/repo", filepath.Join(persistent, "repo"), false},
		{"lookalike /tmpfoo is not ephemeral", "https://evil.example/x/y.git", "/tmpfoo/y", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cloneGate(tc.url, tc.dest, noEnv)
			if tc.allowed && err != nil {
				t.Fatalf("expected allowed, got refusal: %v", err)
			}
			if !tc.allowed && err == nil {
				t.Fatalf("expected refusal, got allowed for dest=%q url=%q", tc.dest, tc.url)
			}
		})
	}
}

// TestCloneGateTMPDIR confirms a custom $TMPDIR root is honored.
func TestCloneGateTMPDIR(t *testing.T) {
	tmpdir := t.TempDir()
	getenv := func(k string) string {
		if k == "TMPDIR" {
			return tmpdir
		}
		return ""
	}
	dest := filepath.Join(tmpdir, "anything")
	if err := cloneGate("https://evil.example/x/y.git", dest, getenv); err != nil {
		t.Fatalf("dest under $TMPDIR should be allowed: %v", err)
	}
}

func TestDestFromCloneArgs(t *testing.T) {
	base := "/work"
	if got := destFromCloneArgs(base, "https://h/o/repo.git", ""); got != "/work/repo" {
		t.Errorf("no-dir dest = %q, want /work/repo", got)
	}
	if got := destFromCloneArgs(base, "https://h/o/repo.git", "sub/dir"); got != "/work/sub/dir" {
		t.Errorf("rel-dir dest = %q, want /work/sub/dir", got)
	}
	if got := destFromCloneArgs(base, "https://h/o/repo.git", "/tmp/dest"); got != "/tmp/dest" {
		t.Errorf("abs-dir dest = %q, want /tmp/dest", got)
	}
}

// TestRunGitCloneRefusal drives the full verb on an off-allowlist clone into a
// non-ephemeral destination and asserts it is refused before git ever runs.
func TestRunGitCloneRefusal(t *testing.T) {
	err := discardRunner().runGitClone(context.Background(),
		[]string{"https://evil.example/x/y.git", "/home/agent/projects/y"})
	if err == nil {
		t.Fatal("off-allowlist clone into a persistent dir should be refused")
	}
}
