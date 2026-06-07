package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreParseConfigFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"absent", []string{"ward", "exec", "build"}, ""},
		{"space form", []string{"ward", "--config", "/a/b.yaml", "exec", "build"}, "/a/b.yaml"},
		{"equals form", []string{"ward", "--config=/a/b.yaml", "exec", "build"}, "/a/b.yaml"},
		{"dangling space form returns empty", []string{"ward", "--config"}, ""},
		{"stops at --", []string{"ward", "--", "--config", "/a/b.yaml"}, ""},
		{"stops at positional", []string{"ward", "exec", "--config", "/a/b.yaml"}, ""},
		{"flag wins over later positional", []string{"ward", "--config=/x.yaml", "exec", "--config=/y.yaml"}, "/x.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preParseConfigFlag(tc.args)
			if got != tc.want {
				t.Fatalf("preParseConfigFlag(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestResolveConfigPath(t *testing.T) {
	tmp := t.TempDir()
	abs := filepath.Join(tmp, "explicit.yaml")

	t.Run("explicit wins over env", func(t *testing.T) {
		got, err := resolveConfigPath(abs, "/env/path.yaml", tmp)
		if err != nil {
			t.Fatal(err)
		}
		if got != abs {
			t.Fatalf("got %q, want %q", got, abs)
		}
	})

	t.Run("env wins over walkup", func(t *testing.T) {
		envPath := filepath.Join(tmp, "env.yaml")
		got, err := resolveConfigPath("", envPath, tmp)
		if err != nil {
			t.Fatal(err)
		}
		if got != envPath {
			t.Fatalf("got %q, want %q", got, envPath)
		}
	})

	t.Run("explicit relative path is absolutized", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		got, err := resolveConfigPath("rel.yaml", "", tmp)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(cwd, "rel.yaml")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to walkup", func(t *testing.T) {
		// Build a valid .ward/ward.yaml so walkup succeeds.
		wardDir := filepath.Join(tmp, ".ward")
		if err := os.MkdirAll(wardDir, 0o755); err != nil {
			t.Fatal(err)
		}
		yamlPath := filepath.Join(wardDir, "ward.yaml")
		if err := os.WriteFile(yamlPath, []byte("commands: {}\n"), 0o644); err != nil { //nolint:gosec
			t.Fatal(err)
		}
		got, err := resolveConfigPath("", "", tmp)
		if err != nil {
			t.Fatal(err)
		}
		if got != yamlPath {
			t.Fatalf("got %q, want %q", got, yamlPath)
		}
	})
}
