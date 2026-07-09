package main

import "strings"

// skipSet normalizes repeated --skip probe names into a lookup map.
func skipSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[strings.ToLower(strings.TrimSpace(n))] = true
	}
	return out
}
