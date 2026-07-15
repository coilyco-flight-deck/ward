package scripts

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate the test file")
	}
	return filepath.Dir(filepath.Dir(self))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestReleasePipelineUsesDraftArtifacts(t *testing.T) {
	promote := readRepoFile(t, ".forgejo/workflows/promote.yml")
	release := readRepoFile(t, ".forgejo/workflows/release.yml")
	docs := readRepoFile(t, "docs/release.md")

	for _, want := range []string{
		"draft-${{ github.sha }}",
		"tag-bump@main",
		"main.Version=${TAG}",
		"published draft release ${DRAFT_TAG}",
	} {
		if !strings.Contains(promote, want) {
			t.Fatalf("promote workflow should mention %q:\n%s", want, promote)
		}
	}

	for _, want := range []string{
		"draft-${{ github.sha }}",
		"promote-draft-assets",
		"fetched ${name} from ${DRAFT_TAG}",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release workflow should mention %q:\n%s", want, release)
		}
	}

	for _, ban := range []string{"go build -trimpath", "go mod download", "make sync-defaults-assets"} {
		if strings.Contains(release, ban) {
			t.Fatalf("release workflow should not rebuild on release; found %q", ban)
		}
	}

	for _, want := range []string{
		"builds the binary",
		"matrix once",
		"retags the already-built",
		"draft assets",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("release docs should mention %q:\n%s", want, docs)
		}
	}
}
