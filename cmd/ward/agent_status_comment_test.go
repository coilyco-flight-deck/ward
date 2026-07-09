package main

import "strings"

func visibleLinesBeforeDetails(body string) string {
	before, _, _ := strings.Cut(body, "<details>")
	var lines []string
	for _, line := range strings.Split(before, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "<!--") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
