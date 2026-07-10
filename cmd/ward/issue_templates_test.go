package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForgejoIssueTemplateSurface fails if a Forgejo-facing issue template
// directory appears under .forgejo. GitHub issue forms stay GitHub-only.
func TestNoForgejoIssueTemplateSurface(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	forgejoRoot := filepath.Join(root, ".forgejo")
	if _, err := os.Stat(forgejoRoot); err != nil {
		t.Fatalf("stat %s: %v", forgejoRoot, err)
	}

	var hits []string
	err := filepath.WalkDir(forgejoRoot, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, string(filepath.Separator)+"ISSUE_TEMPLATE") {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", forgejoRoot, err)
	}
	if len(hits) > 0 {
		t.Fatalf("Forgejo-side structured issue templates must not live under .forgejo: %v", hits)
	}
}
