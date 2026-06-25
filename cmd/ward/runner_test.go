package main

import (
	"os"
	"testing"
)

// TestResolveInvokeCWD covers ward#302: $OLDPWD (the operator's last `cd` target)
// must not win over the real cwd, while COILY_INVOKE_CWD still overrides explicitly.
func TestResolveInvokeCWD(t *testing.T) {
	t.Run("OLDPWD does not override the real cwd", func(t *testing.T) {
		stale := t.TempDir()
		t.Setenv("OLDPWD", stale)
		t.Setenv("COILY_INVOKE_CWD", "")
		if got := resolveInvokeCWD(); got == stale {
			t.Fatalf("resolveInvokeCWD() returned the stale $OLDPWD %q; should report the real cwd", stale)
		}
	})

	t.Run("COILY_INVOKE_CWD overrides", func(t *testing.T) {
		want := t.TempDir()
		t.Setenv("COILY_INVOKE_CWD", want)
		t.Setenv("OLDPWD", t.TempDir())
		if got := resolveInvokeCWD(); got != want {
			t.Fatalf("resolveInvokeCWD() = %q, want the COILY_INVOKE_CWD override %q", got, want)
		}
	})

	// Guard the startup-cwd path: it equals os.Getwd() with no overrides set.
	t.Run("falls back to the startup cwd", func(t *testing.T) {
		t.Setenv("COILY_INVOKE_CWD", "")
		t.Setenv("OLDPWD", "")
		wd, err := os.Getwd()
		if err != nil {
			t.Skip("cannot resolve cwd")
		}
		if got := resolveInvokeCWD(); got != wd {
			t.Fatalf("resolveInvokeCWD() = %q, want startup cwd %q", got, wd)
		}
	})
}
