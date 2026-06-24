package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestFirstSubcommandIndex(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"bare ward", []string{"ward"}, -1},
		{"plain verb", []string{"ward", "test"}, 1},
		{"verb with args", []string{"ward", "test", "--", "x"}, 1},
		{"help flag only", []string{"ward", "--help"}, -1},
		{"version flag only", []string{"ward", "-v"}, -1},
		{"bool root flag then verb", []string{"ward", "--audit-override-dirty", "test"}, 2},
		{"config space form then verb", []string{"ward", "--config", "/a/b.yaml", "test"}, 3},
		{"config equals form then verb", []string{"ward", "--config=/a/b.yaml", "test"}, 2},
		{"dangling config consumes nothing dispatchable", []string{"ward", "--config"}, -1},
		{"terminator first", []string{"ward", "--", "test"}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstSubcommandIndex(tc.args); got != tc.want {
				t.Fatalf("firstSubcommandIndex(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

func TestTopLevelVerbs(t *testing.T) {
	app := &cli.Command{
		Commands: []*cli.Command{
			{Name: "exec"},
			{Name: "version", Aliases: []string{"v"}},
		},
	}
	got := topLevelVerbs(app)
	for _, name := range []string{"exec", "version", "v", "help"} {
		if !got[name] {
			t.Errorf("topLevelVerbs missing %q", name)
		}
	}
	if got["test"] {
		t.Errorf("topLevelVerbs should not contain unregistered %q", "test")
	}
}

// writeWardConfig writes a minimal .ward/ward.yaml with the named commands and
// points WARD_CONFIG at it for the duration of the test.
func writeWardConfig(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	wardDir := filepath.Join(dir, ".ward")
	if err := os.MkdirAll(wardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "commands:\n"
	for _, n := range names {
		body += "  " + n + ":\n    run: make " + n + "\n"
	}
	path := filepath.Join(wardDir, "ward.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	t.Setenv("WARD_CONFIG", path)
}

func TestMaybeRewriteToExec(t *testing.T) {
	topLevel := map[string]bool{"exec": true, "version": true, "help": true}

	t.Run("declared leaf is rewritten", func(t *testing.T) {
		writeWardConfig(t, "build", "test")
		got := maybeRewriteToExec([]string{"ward", "test"}, topLevel)
		want := []string{"ward", "exec", "test"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("leaf args are preserved after the verb", func(t *testing.T) {
		writeWardConfig(t, "build")
		got := maybeRewriteToExec([]string{"ward", "build", "--", "-tags", "x"}, topLevel)
		want := []string{"ward", "exec", "build", "--", "-tags", "x"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("root flags are carried through ahead of exec", func(t *testing.T) {
		writeWardConfig(t, "build")
		got := maybeRewriteToExec([]string{"ward", "--config", "/x.yaml", "build"}, topLevel)
		want := []string{"ward", "--config", "/x.yaml", "exec", "build"}
		// loadDefault reads WARD_CONFIG, not the bogus --config here, so the
		// leaf still resolves; the flag must survive the splice regardless.
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("registered verb is never rewritten", func(t *testing.T) {
		writeWardConfig(t, "build")
		got := maybeRewriteToExec([]string{"ward", "version"}, topLevel)
		want := []string{"ward", "version"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("unknown verb with no matching leaf is untouched", func(t *testing.T) {
		writeWardConfig(t, "build", "test")
		got := maybeRewriteToExec([]string{"ward", "bogus"}, topLevel)
		want := []string{"ward", "bogus"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("no config reachable leaves args untouched", func(t *testing.T) {
		t.Setenv("WARD_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		got := maybeRewriteToExec([]string{"ward", "test"}, topLevel)
		want := []string{"ward", "test"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("bare ward is untouched", func(t *testing.T) {
		writeWardConfig(t, "build")
		got := maybeRewriteToExec([]string{"ward"}, topLevel)
		want := []string{"ward"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func TestMaybeRewriteWardedShim(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "warded splices in drive",
			args: []string{"warded", "claude", "summarize X"},
			want: []string{"ward", "drive", "claude", "summarize X"},
		},
		{
			name: "warded by absolute path still rewrites",
			args: []string{"/usr/local/bin/warded", "codex", "explain Y"},
			want: []string{"ward", "drive", "codex", "explain Y"},
		},
		{
			name: "bare warded rewrites to bare drive (drive reports the missing harness)",
			args: []string{"warded"},
			want: []string{"ward", "drive"},
		},
		{
			name: "flags ride through after drive",
			args: []string{"warded", "claude", "do X", "--print"},
			want: []string{"ward", "drive", "claude", "do X", "--print"},
		},
		{
			name: "ward itself is untouched",
			args: []string{"ward", "drive", "claude", "x"},
			want: []string{"ward", "drive", "claude", "x"},
		},
		{
			name: "another tool name is untouched",
			args: []string{"/usr/local/bin/brew", "install", "x"},
			want: []string{"/usr/local/bin/brew", "install", "x"},
		},
		{
			name: "empty argv is untouched",
			args: []string{},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maybeRewriteWardedShim(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("maybeRewriteWardedShim(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestMaybeInsertDriveBoundary(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "splices -- after harness so the prompt is raw",
			args: []string{"ward", "drive", "claude", "summarize X"},
			want: []string{"ward", "drive", "claude", "--", "summarize X"},
		},
		{
			name: "ward flag before harness is preserved, boundary after harness",
			args: []string{"ward", "drive", "--print", "claude", "summarize X"},
			want: []string{"ward", "drive", "--print", "claude", "--", "summarize X"},
		},
		{
			name: "space-form value flag is skipped when finding the harness",
			args: []string{"ward", "drive", "--repo", "o/r", "codex", "what is X"},
			want: []string{"ward", "drive", "--repo", "o/r", "codex", "--", "what is X"},
		},
		{
			name: "equals-form value flag needs no value skip",
			args: []string{"ward", "drive", "--repo=o/r", "codex", "what is X"},
			want: []string{"ward", "drive", "--repo=o/r", "codex", "--", "what is X"},
		},
		{
			name: "a flag-looking token in the prompt is protected by the spliced --",
			args: []string{"ward", "drive", "claude", "explain the --print flag"},
			want: []string{"ward", "drive", "claude", "--", "explain the --print flag"},
		},
		{
			name: "explicit -- already present is left untouched",
			args: []string{"ward", "drive", "claude", "--", "literal --print"},
			want: []string{"ward", "drive", "claude", "--", "literal --print"},
		},
		{
			name: "harness with no prompt is untouched (nothing to protect)",
			args: []string{"ward", "drive", "claude"},
			want: []string{"ward", "drive", "claude"},
		},
		{
			name: "harness-less drive (only flags) is untouched",
			args: []string{"ward", "drive", "--print"},
			want: []string{"ward", "drive", "--print"},
		},
		{
			name: "drive --help is untouched (cli renders help)",
			args: []string{"ward", "drive", "--help"},
			want: []string{"ward", "drive", "--help"},
		},
		{
			name: "root flag before drive is carried, harness still found",
			args: []string{"ward", "--config", "/a/b.yaml", "drive", "claude", "do X"},
			want: []string{"ward", "--config", "/a/b.yaml", "drive", "claude", "--", "do X"},
		},
		{
			name: "non-drive verb is untouched",
			args: []string{"ward", "exec", "build", "claude", "x"},
			want: []string{"ward", "exec", "build", "claude", "x"},
		},
		{
			name: "repeatable with-repo skips each value",
			args: []string{"ward", "drive", "--with-repo", "o/a", "--with-repo", "o/b", "qwen", "go"},
			want: []string{"ward", "drive", "--with-repo", "o/a", "--with-repo", "o/b", "qwen", "--", "go"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maybeInsertDriveBoundary(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("maybeInsertDriveBoundary(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
