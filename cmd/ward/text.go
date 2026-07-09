package main

import "strings"

// oneLine collapses whitespace runs into single spaces and trims, so a
// multi-line message can be rendered inline.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
