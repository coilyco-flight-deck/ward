// Package scripts holds the release pipeline's shell helpers plus the Go tests
// that exercise them, so `make test` covers release-notes rendering the same way
// it covers the ward binary. The lone source in this package is a test: the
// tested artifact is release-notes.sh, driven here with synthetic commit
// fixtures instead of live git state (ward#486).
package scripts

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// scriptPath resolves release-notes.sh next to this test file, independent of
// the CWD `go test` happens to run the package from.
func scriptPath(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate the test file")
	}
	return filepath.Join(filepath.Dir(self), "release-notes.sh")
}

// record builds one `%h\t%s` git-log line (the contract release-notes.sh reads).
func record(hash, subject string) string {
	return hash + "\t" + subject + "\n"
}

// run feeds records to the script and returns its stdout.
func run(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("bash", append([]string{scriptPath(t)}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("release-notes.sh failed: %v\nstderr: %s", err, errb.String())
	}
	return out.String()
}

func TestVerdictBreaking(t *testing.T) {
	out := run(t,
		record("i7j8k9l", "feat(agent)!: retire ward dispatch")+
			record("m1n2o3p", "fix(mirror): backfill tags"),
		"--prev", "v0.241.0", "--new", "v0.242.0")

	if !strings.Contains(out, "**Yes.**") {
		t.Errorf("breaking release should verdict Yes, got:\n%s", out)
	}
	if !strings.Contains(out, "## Breaking / behavior changes") {
		t.Errorf("missing Breaking section:\n%s", out)
	}
	// The breaking commit must land in the Breaking section, not Features, even
	// though its type is feat.
	breakIdx := strings.Index(out, "## Breaking")
	featIdx := strings.Index(out, "## Features")
	dispatchIdx := strings.Index(out, "retire ward dispatch")
	if dispatchIdx < breakIdx {
		t.Errorf("breaking commit rendered before its section:\n%s", out)
	}
	if featIdx != -1 && dispatchIdx > featIdx {
		t.Errorf("breaking feat leaked into Features:\n%s", out)
	}
}

// A body-only BREAKING note (no `!` in the subject) arrives via --breaking-hashes,
// mirroring the workflow's `git log --grep` - AGENTS.md's "note in the commit body".
func TestBreakingFromBodyFooter(t *testing.T) {
	out := run(t,
		record("abc1234", "feat(api): change default timeout"),
		"--prev", "v1.0.0", "--new", "v1.1.0", "--breaking-hashes", "abc1234")
	if !strings.Contains(out, "**Yes.**") || !strings.Contains(out, "## Breaking") {
		t.Errorf("body BREAKING footer should flag the release:\n%s", out)
	}
	if !strings.Contains(out, "change default timeout") {
		t.Errorf("body-flagged commit should appear in Breaking:\n%s", out)
	}
}

func TestVerdictFeaturesAndFixes(t *testing.T) {
	out := run(t,
		record("a1b2c3d", "feat(advisor): fan research into per-repo issues")+
			record("m1n2o3p", "fix(mirror): backfill tags"),
		"--prev", "v0.241.0", "--new", "v0.242.0")
	if !strings.Contains(out, "**Maybe.**") {
		t.Errorf("feature+fix release should verdict Maybe, got:\n%s", out)
	}
	if strings.Contains(out, "## Breaking") {
		t.Errorf("no breaking commits but Breaking section rendered:\n%s", out)
	}
	if !strings.Contains(out, "## Features") || !strings.Contains(out, "## Fixes") {
		t.Errorf("missing Features/Fixes sections:\n%s", out)
	}
}

func TestVerdictInternalOnly(t *testing.T) {
	out := run(t,
		record("aaa1111", "docs: split agent harness pages")+
			record("bbb2222", "refactor(agents): rename agentspi -> agentsapi"),
		"--prev", "v0.239.0", "--new", "v0.240.0")
	if !strings.Contains(out, "**Probably not.**") {
		t.Errorf("internal-only release should verdict Probably not, got:\n%s", out)
	}
	// Internal churn folds under <details>, not a top-level section.
	if !strings.Contains(out, "<details>") || !strings.Contains(out, "safe to skip (2)") {
		t.Errorf("internal changes should fold under a details with count 2:\n%s", out)
	}
	if strings.Contains(out, "## Features") || strings.Contains(out, "## Fixes") {
		t.Errorf("internal-only release should have no Features/Fixes headers:\n%s", out)
	}
}

func TestCompareFooter(t *testing.T) {
	base := "https://forgejo.coilysiren.me/coilyco-flight-deck/ward"
	out := run(t,
		record("a1b2c3d", "feat: x"),
		"--prev", "v0.241.0", "--new", "v0.242.0", "--compare-url", base)
	want := base + "/compare/v0.241.0...v0.242.0"
	if !strings.Contains(out, want) {
		t.Errorf("missing compare link %q in:\n%s", want, out)
	}
}

// No prior tag (first release) must not emit a dangling compare footer.
func TestNoTagsNoFooter(t *testing.T) {
	out := run(t, record("ccc3333", "feat: first"))
	if strings.Contains(out, "Full changelog") {
		t.Errorf("first release should have no compare footer:\n%s", out)
	}
	if !strings.Contains(out, "## Features") {
		t.Errorf("first release should still categorise:\n%s", out)
	}
}

// Empty input (no commits in range) must still yield a well-formed body.
func TestEmptyInput(t *testing.T) {
	out := run(t, "", "--prev", "v0.1.0", "--new", "v0.2.0")
	if !strings.Contains(out, "Does this upgrade affect you?") {
		t.Errorf("empty range should still render the header:\n%s", out)
	}
}

// The standing install note must render on every body regardless of verdict -
// including an empty range - explaining the Linux-only assets (ward#442).
func TestInstallNoteAlwaysRenders(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"internal-only": record("aaa1111", "docs: split agent harness pages"),
		"features":      record("a1b2c3d", "feat: x"),
	}
	for name, stdin := range cases {
		t.Run(name, func(t *testing.T) {
			out := run(t, stdin, "--prev", "v0.1.0", "--new", "v0.2.0")
			if !strings.Contains(out, "## Install") {
				t.Errorf("missing standing install note:\n%s", out)
			}
			if !strings.Contains(out, "ward-linux-{amd64,arm64}") {
				t.Errorf("install note should name the Linux assets:\n%s", out)
			}
			if !strings.Contains(out, "brew install coilyco-flight-deck/tap/ward") {
				t.Errorf("install note should point humans at Homebrew:\n%s", out)
			}
		})
	}
}
