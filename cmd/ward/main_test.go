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
