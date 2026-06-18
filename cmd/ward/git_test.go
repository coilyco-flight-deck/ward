package main

import "testing"

// TestGitCommandRegistersPassthroughVerbs asserts every declared passthrough
// verb (plus the dedicated commit verb) is wired as a `ward git` subcommand.
// remote is the read passthrough added for repo-identity resolution (ward#119).
func TestGitCommandRegistersPassthroughVerbs(t *testing.T) {
	cmd := gitCommand()
	have := map[string]bool{}
	for _, sub := range cmd.Commands {
		have[sub.Name] = true
	}
	want := []string{"commit", "remote", "status", "fetch", "push"}
	for _, name := range want {
		if !have[name] {
			t.Errorf("ward git missing subcommand %q", name)
		}
	}
}

// TestGitVerbRewriterHoistsDashC checks the argv rewriter prepends the verb and
// hoists a leading -C ahead of it, matching git's flag-ordering requirement.
func TestGitVerbRewriterHoistsDashC(t *testing.T) {
	rw := gitVerbRewriter("remote")
	got := rw([]string{"get-url", "origin"})
	want := []string{"remote", "get-url", "origin"}
	assertArgv(t, got, want)

	got = rw([]string{"-C", "/repo", "get-url", "origin"})
	want = []string{"-C", "/repo", "remote", "get-url", "origin"}
	assertArgv(t, got, want)
}
