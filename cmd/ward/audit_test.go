package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	t.Run("empty is zero", func(t *testing.T) {
		got, err := parseSince("")
		if err != nil || got != 0 {
			t.Fatalf("parseSince(\"\") = %d, %v", got, err)
		}
	})
	t.Run("unix seconds pass through", func(t *testing.T) {
		got, err := parseSince("1780000000")
		if err != nil || got != 1780000000 {
			t.Fatalf("parseSince = %d, %v", got, err)
		}
	})
	t.Run("duration is relative", func(t *testing.T) {
		got, err := parseSince("1h")
		if err != nil {
			t.Fatal(err)
		}
		if delta := time.Now().Unix() - got; delta < 3500 || delta > 3700 {
			t.Fatalf("1h ago delta = %d, want ~3600", delta)
		}
	})
	t.Run("days suffix", func(t *testing.T) {
		got, err := parseSince("7d")
		if err != nil {
			t.Fatal(err)
		}
		if delta := time.Now().Unix() - got; delta < 7*24*3600-100 || delta > 7*24*3600+100 {
			t.Fatalf("7d ago delta = %d", delta)
		}
	})
	t.Run("garbage errors", func(t *testing.T) {
		if _, err := parseSince("nope"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestScopeMatches(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		rec, filter string
		want        bool
	}{
		{"/a/b", "", true},
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/bother", "/a/b", false},
		{"/x", "/a/b", false},
		{"/a/b" + sep + "c", "/a/b", true},
	}
	for _, tc := range cases {
		if got := scopeMatches(tc.rec, tc.filter); got != tc.want {
			t.Fatalf("scopeMatches(%q, %q) = %v, want %v", tc.rec, tc.filter, got, tc.want)
		}
	}
}

func TestRowMatches(t *testing.T) {
	row := `{"ts":1000,"repo_root":"/a/b","verb":"repo.test"}`
	t.Run("no filters", func(t *testing.T) {
		if !rowMatches(row, 0, "") {
			t.Fatal("want match with no filters")
		}
	})
	t.Run("since excludes older", func(t *testing.T) {
		if rowMatches(row, 2000, "") {
			t.Fatal("row at ts=1000 should be excluded by since=2000")
		}
	})
	t.Run("scope excludes other repo", func(t *testing.T) {
		if rowMatches(row, 0, "/other") {
			t.Fatal("row in /a/b should not match scope /other")
		}
	})
	t.Run("scope includes descendant", func(t *testing.T) {
		if !rowMatches(`{"ts":1,"repo_root":"/a/b/c"}`, 0, "/a/b") {
			t.Fatal("descendant should match")
		}
	})
	t.Run("unparseable passes through", func(t *testing.T) {
		if !rowMatches("not json", 0, "") {
			t.Fatal("malformed line should pass through")
		}
	})
}

// TestTailAuditLog streams a fixture log with combined since + scope
// filters and asserts only the matching rows reach stdout.
func TestTailAuditLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	lines := []string{
		`{"ts":1000,"repo_root":"/repo/a","verb":"old"}`,
		`{"ts":3000,"repo_root":"/repo/a","verb":"keep"}`,
		`{"ts":3000,"repo_root":"/repo/b","verb":"otherrepo"}`,
		`{"ts":4000,"repo_root":"/repo/a/sub","verb":"descendant"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := tailAuditLog(context.Background(), path, 2000, "/repo/a", false); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "old") || strings.Contains(out, "otherrepo") {
		t.Fatalf("filtered rows leaked: %q", out)
	}
	if !strings.Contains(out, "keep") || !strings.Contains(out, "descendant") {
		t.Fatalf("expected rows missing: %q", out)
	}
}

func TestTailAuditLogMissingFileNoFollow(t *testing.T) {
	err := tailAuditLog(context.Background(), filepath.Join(t.TempDir(), "absent.jsonl"), 0, "", false)
	if err != nil {
		t.Fatalf("missing file without --follow should be a no-op, got %v", err)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- string(buf)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}
