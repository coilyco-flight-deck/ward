package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoImageDefaultsStayPublic(t *testing.T) {
	files := []string{
		filepath.Join("..", "..", "docker", "demo", "Dockerfile"),
		filepath.Join("..", "..", "docker", "demo", "demo.sh"),
		filepath.Join("..", "..", "docs", "demo-image.md"),
	}
	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			got := string(data)
			for _, want := range []string{"cli/cli", "WARD_DEMO_WORKSPACE", "WARD_DEMO_SUBSTRATE"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s missing %q", path, want)
				}
			}
			for _, banned := range []string{"coilyco-", "coilysiren"} {
				if strings.Contains(got, banned) {
					t.Errorf("%s unexpectedly contains %q", path, banned)
				}
			}
		})
	}
}
