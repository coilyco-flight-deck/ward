package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveContainerMemoryLimitUsesOperatorLocalOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ward"), 0o755); err != nil {
		t.Fatalf("mkdir .ward: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ward", "config.yaml"), []byte("container:\n  memory-limit: 768m\n"), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	limit, err := resolveContainerMemoryLimit()
	if err != nil {
		t.Fatalf("resolveContainerMemoryLimit: %v", err)
	}
	if limit != "768m" {
		t.Fatalf("resolveContainerMemoryLimit() = %q, want 768m", limit)
	}
	swap, err := resolveContainerMemorySwap(limit)
	if err != nil {
		t.Fatalf("resolveContainerMemorySwap: %v", err)
	}
	if swap != "1536m" {
		t.Fatalf("resolveContainerMemorySwap(%q) = %q, want 1536m", limit, swap)
	}
}
