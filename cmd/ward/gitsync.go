package main

// gitsync.go is the shared TTL-cached git-ref sync (ward#654), the factored
// substrate-warmer core; see docs/config-source.md for the full contract.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// gitRefSpec names one remote-to-working-checkout sync for syncGitRef.
type gitRefSpec struct {
	url    string // remote clone URL
	mirror string // bare mirror path in the shared cache
	lock   string // flock path serializing mirror + checkout mutation
	work   string // working-checkout destination (recreated when the mirror moves)
	ref    string // ref to check out, passed to git untouched; "" keeps the default branch
	seed   string // optional baked mirror to hydrate from (substrate image tier)

	// netEnv lazily resolves auth env for the network ops, so the zero-network
	// TTL fast path never pays for token resolution; nil means none.
	netEnv func() []string

	// logf is the progress sink (blog for the substrate warmer); nil is silent.
	logf func(format string, a ...any)
}

// syncGitRef ensures spec.mirror exists and is TTL-fresh, then materializes
// spec.work from it, all under the spec.lock flock (docs/config-source.md).
func (r *Runner) syncGitRef(ctx context.Context, spec gitRefSpec, ttl time.Duration) (string, error) {
	logf := spec.logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var syncErr error
	r.withFlock(spec.lock, func() {
		refreshed, err := r.ensureMirrorFresh(ctx, spec, ttl, logf)
		if err != nil {
			syncErr = err
			return
		}
		if !refreshed && isDir(filepath.Join(spec.work, ".git")) {
			return // fresh mirror + existing checkout: zero work
		}
		syncErr = r.dropWorkingCheckout(ctx, spec)
	})
	if syncErr != nil {
		return "", syncErr
	}
	return spec.work, nil
}

// ensureMirrorFresh hydrates/clones a missing mirror and refreshes a stale
// one; refreshed reports the checkout must be re-dropped.
func (r *Runner) ensureMirrorFresh(ctx context.Context, spec gitRefSpec, ttl time.Duration, logf func(string, ...any)) (refreshed bool, err error) {
	if !isDir(spec.mirror) {
		if spec.seed != "" && isDir(spec.seed) {
			logf("gitsync: hydrate %s from baked seed", spec.url)
			_ = r.Runner.Exec(ctx, "cp", "-a", spec.seed, spec.mirror)
		}
	}
	if !isDir(spec.mirror) {
		logf("gitsync: clone mirror (first time) %s", spec.url)
		if cerr := r.netExec(ctx, spec, "git", "clone", "--mirror", spec.url, spec.mirror); cerr != nil {
			_ = os.RemoveAll(spec.mirror)
			return false, fmt.Errorf("clone --mirror %s: %w", spec.url, cerr)
		}
		// clone writes no FETCH_HEAD, which substrateMirrorStale reads as
		// forever-fresh; stamp it so the TTL clock starts at clone time.
		touchFetchHead(spec.mirror)
		return true, nil
	}
	if substrateMirrorStale(spec.mirror, int64(ttl.Seconds()), time.Now()) {
		return r.refreshStaleMirror(ctx, spec, ttl, logf)
	}
	return false, nil
}

func (r *Runner) refreshStaleMirror(ctx context.Context, spec gitRefSpec, ttl time.Duration, logf func(format string, a ...any)) (bool, error) {
	if refreshDeniedRecently(spec.mirror, refreshDeniedBackoff(ttl)) {
		return false, nil
	}
	if denied, derr := fetchHeadPermissionDenied(spec.mirror); denied {
		logf("gitsync: refresh failed %s at %s/FETCH_HEAD: permission denied (using cached state; make the cache writable by the ward process or isolate it per user/container)", spec.url, spec.mirror)
		markRefreshDenied(spec.mirror)
		return false, nil
	} else if derr != nil {
		logf("gitsync: refresh failed %s at %s: %v (using cached state)", spec.url, spec.mirror, derr)
		return false, nil
	}
	logf("gitsync: refresh %s (TTL %s elapsed)", spec.url, ttl)
	if uerr := r.netExec(ctx, spec, "git", "-C", spec.mirror, "remote", "update", "--prune"); uerr != nil {
		// Cache-fallback keeps the cached mirror serving. Permission-denied failures
		// get a more specific diagnostic so a bad cache path is easy to recognize.
		if isFetchHeadPermissionDenied(uerr) {
			logf("gitsync: refresh failed %s at %s/FETCH_HEAD: permission denied (using cached state; make the cache writable by the ward process or isolate it per user/container)", spec.url, spec.mirror)
			markRefreshDenied(spec.mirror)
		} else {
			logf("gitsync: refresh failed %s (using cached state)", spec.url)
		}
		return false, nil //nolint:nilerr // never brick offline (ward#654)
	}
	clearRefreshDenied(spec.mirror)
	return true, nil
}

// dropWorkingCheckout re-creates spec.work from the mirror and detaches it at
// spec.ref when one is named (branch, tag, or sha - git classifies, not ward).
func (r *Runner) dropWorkingCheckout(ctx context.Context, spec gitRefSpec) error {
	_ = os.RemoveAll(spec.work)
	if cerr := r.Runner.Exec(ctx, "git", "clone", "--quiet", spec.mirror, spec.work); cerr != nil {
		return fmt.Errorf("working clone from %s: %w", spec.mirror, cerr)
	}
	_ = r.Runner.Exec(ctx, "git", "-C", spec.work, "remote", "set-url", "origin", spec.url)
	if spec.ref == "" {
		return nil
	}
	if cerr := r.Runner.Exec(ctx, "git", "-C", spec.work,
		"-c", "advice.detachedHead=false", "checkout", "--quiet", spec.ref); cerr != nil {
		// A half-checked-out work dir must not be reused as "current" next call.
		_ = os.RemoveAll(spec.work)
		return fmt.Errorf("checkout %q in %s: %w", spec.ref, spec.work, cerr)
	}
	return nil
}

// netExec runs one network git op with the spec's lazily-resolved auth env,
// mirroring git_clone.go's shadow-runner pattern.
func (r *Runner) netExec(ctx context.Context, spec gitRefSpec, bin string, argv ...string) error {
	runner := r.Runner
	if spec.netEnv != nil {
		if env := spec.netEnv(); len(env) > 0 {
			shadow := *r.Runner
			shadow.Env = append(append([]string(nil), r.Runner.Env...), env...)
			runner = &shadow
		}
	}
	return runner.Exec(ctx, bin, argv...)
}

// substrateMirrorStale ports substrate_mirror_stale: stale when FETCH_HEAD is
// older than the TTL. A missing FETCH_HEAD (just hydrated) is fresh.
func substrateMirrorStale(mirror string, ttlSeconds int64, now time.Time) bool {
	head := filepath.Join(mirror, "FETCH_HEAD")
	fi, err := os.Stat(head)
	if err != nil {
		return false
	}
	return int64(now.Sub(fi.ModTime()).Seconds()) >= ttlSeconds
}

// touchFetchHead stamps mirror/FETCH_HEAD to now, creating it if absent.
func touchFetchHead(mirror string) {
	head := filepath.Join(mirror, "FETCH_HEAD")
	now := time.Now()
	if err := os.Chtimes(head, now, now); err != nil {
		_ = os.WriteFile(head, nil, 0o644)
	}
}

func refreshDeniedMarker(mirror string) string {
	return filepath.Join(filepath.Dir(mirror), ".refresh-denied")
}

func refreshDeniedRecently(mirror string, ttl time.Duration) bool {
	fi, err := os.Stat(refreshDeniedMarker(mirror))
	if err != nil {
		return false
	}
	return time.Since(fi.ModTime()) < ttl
}

func refreshDeniedBackoff(ttl time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}
	return configBundleTTLDefault()
}

func markRefreshDenied(mirror string) {
	head := filepath.Join(mirror, "FETCH_HEAD")
	if _, err := os.Stat(head); err != nil {
		return
	}
	now := time.Now()
	_ = os.WriteFile(refreshDeniedMarker(mirror), []byte(now.Format(time.RFC3339Nano)), 0o644)
}

func clearRefreshDenied(mirror string) {
	_ = os.Remove(refreshDeniedMarker(mirror))
}

func fetchHeadPermissionDenied(mirror string) (bool, error) {
	head := filepath.Join(mirror, "FETCH_HEAD")
	fi, err := os.Stat(head)
	if err != nil {
		return false, err
	}
	if fi.Mode().Perm()&0o222 != 0 {
		return false, nil
	}
	return true, nil
}

func isFetchHeadPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "fetch_head") && strings.Contains(s, "permission denied")
}
