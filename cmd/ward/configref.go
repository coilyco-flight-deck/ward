package main

// configref.go resolves the WARD_CONFIG_REF git grammar (ward#654) into the
// local bundle dir the ward#653 seam consumes: docs/config-source.md.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// wardConfigTTLEnv overrides the config-bundle refresh TTL in seconds; the
// default comes from the smart-defaults bundle.
const wardConfigTTLEnv = "WARD_CONFIG_TTL"

// configRef is the parsed git form of WARD_CONFIG_REF: self-describing, no
// forge assumptions - the ref is passed to git untouched (branch, tag, or sha).
type configRef struct {
	repoSpec string // host/owner/repo
	ref      string // git ref; "" means the remote default branch
	subpath  string // bundle dir inside the checkout ("." = repo root)
}

// cloneURL renders the https clone URL for the repo-spec.
func (c configRef) cloneURL() string { return "https://" + c.repoSpec + ".git" }

// parseConfigRef parses `<host>/<owner>/<repo>[@<ref>]//<subpath>`: split once
// on the first `//`, then the left half once on `@`. Pure; fails loud.
func parseConfigRef(raw string) (configRef, error) {
	left, subpath, ok := strings.Cut(raw, "//")
	if !ok {
		return configRef{}, fmt.Errorf("want <host>/<owner>/<repo>[@<ref>]//<subpath> (no `//` before the subpath)")
	}
	repoSpec, ref, hasRef := strings.Cut(left, "@")
	if hasRef && strings.TrimSpace(ref) == "" {
		return configRef{}, fmt.Errorf("empty ref after `@` (drop the `@` to track the remote default branch)")
	}
	parts := strings.Split(repoSpec, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return configRef{}, fmt.Errorf("repo-spec %q: want exactly <host>/<owner>/<repo>", repoSpec)
	}
	subpath = path.Clean(subpath) // "" cleans to ".", the repo root
	if path.IsAbs(subpath) || subpath == ".." || strings.HasPrefix(subpath, "../") {
		return configRef{}, fmt.Errorf("subpath %q escapes the checkout", subpath)
	}
	return configRef{repoSpec: repoSpec, ref: strings.TrimSpace(ref), subpath: subpath}, nil
}

// resolveConfigBundle syncs the ref's repo into the config-bundle cache and
// returns the local bundle dir (checkout + subpath) for bundleConfigSource.
func (r *Runner) resolveConfigBundle(ctx context.Context, cr configRef, rawRef string) (string, error) {
	root, err := configBundleCacheRoot(os.Getenv)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, hashConfigRef(rawRef))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("config-bundle cache %s: %w", dir, err)
	}
	work, err := r.syncGitRef(ctx, gitRefSpec{
		url:    cr.cloneURL(),
		mirror: filepath.Join(dir, "mirror.git"),
		lock:   filepath.Join(dir, ".lock"),
		work:   filepath.Join(dir, "work"),
		ref:    cr.ref,
		netEnv: func() []string {
			env, _ := r.gitForgejoAuthEnv(ctx) // best-effort (ward#507)
			return envSliceFrom(env)
		},
		logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "ward: config-bundle "+format+"\n", a...)
		},
	}, configBundleTTL(os.Getenv))
	if err != nil {
		return "", err
	}
	return filepath.Join(work, cr.subpath), nil
}

// configBundleCacheRoot picks and creates the cache home: a per-container subdir
// in containers, else ~/.cache/ward.
func configBundleCacheRoot(getenv func(string) string) (string, error) {
	if getenv("WARD_CONTAINER") == "1" {
		cache := getenv("WARD_GITCACHE")
		if cache == "" {
			cache = containerGitcacheMnt
		}
		root := filepath.Join(cache, "config-bundle")
		if instance := strings.TrimSpace(getenv("WARD_CONTAINER_NAME")); instance != "" {
			root = filepath.Join(root, instance)
		} else if uid := strings.TrimSpace(getenv("WARD_AGENT_UID")); uid != "" {
			root = filepath.Join(root, uid)
		}
		if writableConfigBundleRoot(root, 0o777) {
			return root, nil
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return writableConfigBundleTempRoot()
	}
	root := filepath.Join(base, "ward", "config-bundle")
	if writableConfigBundleRoot(root, 0o755) {
		return root, nil
	}
	return writableConfigBundleTempRoot()
}

// configBundleTTL reads WARD_CONFIG_TTL (seconds); unset or malformed falls
// back to the substrate warmer's default.
func configBundleTTL(getenv func(string) string) time.Duration {
	if n, err := strconv.ParseInt(strings.TrimSpace(getenv(wardConfigTTLEnv)), 10, 64); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	return configBundleTTLDefault()
}

// hashConfigRef keys the cache dir on the whole raw ref (repo + ref + subpath),
// so two refs never share a checkout.
func hashConfigRef(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}

func writableConfigBundleRoot(root string, perm os.FileMode) bool {
	if err := os.MkdirAll(root, perm); err != nil {
		return false
	}
	probe, err := os.CreateTemp(root, ".ward-write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}

func writableConfigBundleTempRoot() (string, error) {
	root := filepath.Join(os.TempDir(), "ward", "config-bundle")
	if writableConfigBundleRoot(root, 0o755) {
		return root, nil
	}
	return "", fmt.Errorf("config-bundle cache root is not writable in the configured cache, home cache, or temp cache")
}
