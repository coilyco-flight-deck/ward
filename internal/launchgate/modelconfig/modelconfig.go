// Package modelconfig holds the named recovery path and cheap heuristics for
// detecting stale per-harness model values before launch.
package modelconfig

import "strings"

// GateName is the named recovery path for a resolved model that the harness
// cannot use as configured.
const GateName = "model-config"

// LooksLike reports whether text contains a model rejection or stale metadata
// hint, as opposed to an unrelated launch failure.
func LooksLike(text string) bool {
	l := strings.ToLower(text)
	for _, marker := range []string{
		"fallback metadata",
		"invalid model",
		"model metadata",
		"model not found",
		"model is not available",
		"no such model",
		"not supported",
		"unsupported model",
		"unknown model",
	} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}
