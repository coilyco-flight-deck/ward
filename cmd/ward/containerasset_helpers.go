package main

import (
	"fmt"
	"os"
	"strings"
)

func mustReadContainerAssetLines(name string) []string {
	b, err := containerAssets.ReadFile("containerassets/" + name)
	if err != nil {
		panic(fmt.Errorf("read container asset %s: %w", name, err))
	}
	var out []string
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		panic(fmt.Errorf("container asset %s is empty", name))
	}
	return out
}

func mustReadContainerAssetKV(name string) map[string]string {
	lines := mustReadContainerAssetLines(name)
	out := make(map[string]string, len(lines))
	for _, line := range lines {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			panic(fmt.Errorf("container asset %s: want key=value, got %q", name, line))
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.Trim(val, `"'`))
		if key == "" || val == "" {
			panic(fmt.Errorf("container asset %s: want non-empty key=value, got %q", name, line))
		}
		out[key] = os.ExpandEnv(val)
	}
	return out
}
