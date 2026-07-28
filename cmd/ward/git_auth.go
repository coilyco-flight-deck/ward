package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// git_auth.go pre-configures Forgejo credentials for ward's network git verbs so
// they authenticate without a prompt (ward#507). See docs/git-verbs.md.

// gitForgejoAuthEnv resolves the Forgejo token into the GIT_CONFIG_* env adding a
// Forgejo-scoped extraheader; best-effort (empty map => git behaves as before).
func (r *Runner) gitForgejoAuthEnv(ctx context.Context) (map[string]string, error) {
	token := r.resolveForgejoGitToken(ctx)
	if token == "" {
		return map[string]string{}, nil
	}
	return forgejoExtraheaderEnv(os.Getenv("GIT_CONFIG_COUNT"), token), nil
}

// resolveForgejoGitToken resolves the token through native credential plumbing.
// A token-less host falls back to git's own flow.
func (r *Runner) resolveForgejoGitToken(ctx context.Context) string {
	tok, err := r.forgejoTokenResolver(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tok)
}

// forgejoExtraheaderEnv builds the one-entry GIT_CONFIG_* env adding a Forgejo-scoped
// Basic-auth header, appended past any existing GIT_CONFIG_COUNT. See docs/git-verbs.md.
func forgejoExtraheaderEnv(existingCount, token string) map[string]string {
	n := parseGitConfigCount(existingCount)
	basic := base64.StdEncoding.EncodeToString([]byte(forgeForgejo.gitPushUser() + ":" + token))
	return map[string]string{
		"GIT_CONFIG_COUNT":                    strconv.Itoa(n + 1),
		fmt.Sprintf("GIT_CONFIG_KEY_%d", n):   "http." + forgejoBaseURL + "/.extraheader",
		fmt.Sprintf("GIT_CONFIG_VALUE_%d", n): "Authorization: Basic " + basic,
	}
}

// parseGitConfigCount reads an existing GIT_CONFIG_COUNT into a non-negative offset;
// a missing, empty, or malformed value counts as zero.
func parseGitConfigCount(s string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return 0
}

// envSliceFrom flattens an env map into the "k=v" slice shell.Runner.Env takes,
// for the clone path that sets the child env directly.
func envSliceFrom(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
