package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoIssueTemplateSurface fails if any issue template surface comes back.
func TestNoIssueTemplateSurface(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}

	var hits []string
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, string(filepath.Separator)+"ISSUE_TEMPLATE") {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(hits) > 0 {
		t.Fatalf("issue templates must not live in the repository: %v", hits)
	}
}
