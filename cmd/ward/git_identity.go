package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/shell"
)

const gitUseConfigOnly = "user.useConfigOnly=true"

var gitResolvedIdentityPattern = regexp.MustCompile(`^(.*) <([^<>]+)> [0-9]+ [+-][0-9]{4}$`)

// resolveGitCommitIdentity asks Git to apply its native author and committer
// precedence while disabling username and hostname guessing.
func resolveGitCommitIdentity(ctx context.Context, base *shell.Runner, dir string, fallback map[string]string) (map[string]string, error) {
	runner := gitIdentityRunner(base, fallback)
	resolved := map[string]string{}
	missing := make([]string, 0, 2)
	for _, identity := range []struct {
		gitVar   string
		nameEnv  string
		emailEnv string
		label    string
	}{
		{gitVar: "GIT_AUTHOR_IDENT", nameEnv: "GIT_AUTHOR_NAME", emailEnv: "GIT_AUTHOR_EMAIL", label: "author name/email"},
		{gitVar: "GIT_COMMITTER_IDENT", nameEnv: "GIT_COMMITTER_NAME", emailEnv: "GIT_COMMITTER_EMAIL", label: "committer name/email"},
	} {
		out, err := runner.Capture(ctx, "git", gitUseConfigOnlyArgv(dir, "var", identity.gitVar)...)
		parts := gitResolvedIdentityPattern.FindStringSubmatch(strings.TrimSpace(string(out)))
		if err != nil || len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
			missing = append(missing, identity.label)
			continue
		}
		resolved[identity.nameEnv] = strings.TrimSpace(parts[1])
		resolved[identity.emailEnv] = strings.TrimSpace(parts[2])
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("git commit identity is missing %s. Configure user.name and user.email in repository-local or global Git config, provide GIT_AUTHOR_NAME/GIT_AUTHOR_EMAIL and GIT_COMMITTER_NAME/GIT_COMMITTER_EMAIL, or set WARD_GIT_NAME/WARD_GIT_EMAIL as an explicit container fallback. Ward did not write Git identity configuration", strings.Join(missing, " and "))
	}
	return resolved, nil
}

func validateGitCommitIdentity(ctx context.Context, runner *shell.Runner, dir string) error {
	_, err := resolveGitCommitIdentity(ctx, runner, dir, nil)
	return err
}

func projectEngineerGitIdentity(ctx context.Context, runner *shell.Runner, plan *upPlan, dir string) error {
	resolved, err := resolveGitCommitIdentity(ctx, runner, dir, plan.ConfigEnv)
	if err != nil {
		return err
	}
	if plan.ConfigEnv == nil {
		plan.ConfigEnv = map[string]string{}
	}
	for key, value := range resolved {
		plan.ConfigEnv[key] = value
	}
	return nil
}

func gitIdentityRunner(base *shell.Runner, fallback map[string]string) *shell.Runner {
	if base == nil {
		base = &shell.Runner{}
	}
	shadow := *base
	shadow.Stderr = io.Discard
	shadow.Env = append([]string(nil), base.Env...)
	name := strings.TrimSpace(fallback["WARD_GIT_NAME"])
	email := strings.TrimSpace(fallback["WARD_GIT_EMAIL"])
	for _, pair := range []struct {
		key   string
		value string
	}{
		{key: "GIT_AUTHOR_NAME", value: name},
		{key: "GIT_AUTHOR_EMAIL", value: email},
		{key: "GIT_COMMITTER_NAME", value: name},
		{key: "GIT_COMMITTER_EMAIL", value: email},
	} {
		if pair.value != "" && effectiveEnvValue(shadow.Env, pair.key) == "" {
			shadow.Env = append(shadow.Env, pair.key+"="+pair.value)
		}
	}
	return &shadow
}

func effectiveEnvValue(overrides []string, key string) string {
	value := os.Getenv(key)
	for _, raw := range overrides {
		name, candidate, ok := strings.Cut(raw, "=")
		if ok && name == key {
			value = candidate
		}
	}
	return strings.TrimSpace(value)
}

func gitUseConfigOnlyArgv(dir, command string, args ...string) []string {
	argv := make([]string, 0, len(args)+5)
	if strings.TrimSpace(dir) != "" {
		argv = append(argv, "-C", dir)
	}
	argv = append(argv, "-c", gitUseConfigOnly, command)
	return append(argv, args...)
}

func applyWardGitIdentityFallback(name, email string) {
	for _, pair := range []struct {
		key   string
		value string
	}{
		{key: "GIT_AUTHOR_NAME", value: name},
		{key: "GIT_AUTHOR_EMAIL", value: email},
		{key: "GIT_COMMITTER_NAME", value: name},
		{key: "GIT_COMMITTER_EMAIL", value: email},
	} {
		if strings.TrimSpace(os.Getenv(pair.key)) == "" && strings.TrimSpace(pair.value) != "" {
			_ = os.Setenv(pair.key, strings.TrimSpace(pair.value))
		}
	}
}
