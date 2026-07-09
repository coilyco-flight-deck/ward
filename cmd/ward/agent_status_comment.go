package main

import (
	"fmt"
	"strings"
)

// collapsedIssueComment keeps tracker automation hypercurt: one visible status
// line, then all diagnostics inside a collapsed details block.
func collapsedIssueComment(visible, summary, detail string) string {
	visible = strings.TrimSpace(visible)
	summary = strings.TrimSpace(summary)
	detail = strings.TrimSpace(detail)
	if summary == "" {
		summary = "details"
	}
	if detail == "" {
		return visible
	}
	return fmt.Sprintf("%s\n\n<details><summary>%s</summary>\n\n%s\n\n</details>", visible, summary, detail)
}
