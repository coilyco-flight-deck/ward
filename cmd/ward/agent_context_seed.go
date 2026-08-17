package main

import (
	"context"
	"os"
	"path/filepath"
)

// agent_context_seed.go carries the host-side gitcache seed for external (non-Forgejo)
// catalog.dependsOn deps (ward#612). Full rationale: docs/container.md.

// seedExternalContextMirrors seeds the ward-gitcache volume with every external
// catalog.dependsOn mirror host-side before the sealed container launches (ward#612).
func (r *Runner) seedExternalContextMirrors(ctx context.Context, plan upPlan) {
	if plan.Collaboration {
		return
	}
	// Only the real host can seed the mirror.
	// An in-container dispatch has none of the launch-time credentials.
	if inContainer() {
		return
	}
	deps := externalContextDeps(plan.HostCwd)
	for _, dep := range deps {
		r.seedExternalContextMirror(ctx, plan, dep)
	}
}

// externalContextDeps resolves the external (non-Forgejo) catalog.dependsOn entries
// from the host config at cwd - a warm-cache hint, not the resolution (ward#580).
func externalContextDeps(cwd string) []catalogContextRepo {
	if cwd == "" {
		cwd = "."
	}
	var out []catalogContextRepo
	for _, dep := range catalogContextRepos(cwd) {
		if dep.external() {
			out = append(out, dep)
		}
	}
	return out
}

// seedExternalContextMirror clones one external dep over the host's default credential
// chain into the gitcache volume; a clone or copy failure warns loud (ward#612).
func (r *Runner) seedExternalContextMirror(ctx context.Context, plan upPlan, dep catalogContextRepo) {
	mirror := dep.Owner + "__" + dep.Name + ".git"
	lock := filepath.Join(os.TempDir(), "ward-extseed-"+dep.Owner+"__"+dep.Name+".lock")
	r.withFlock(lock, func() {
		// The volume persists across dispatches, so a mirror a prior run already warmed is
		// reused - an external upstream is stable and re-cloning it each launch is wasteful.
		if r.gitcacheMirrorPresent(ctx, plan.Image, mirror) {
			writef(r.Runner.Stderr, "ward agent: external dep %s/%s already seeded in %s (ward#612)\n",
				dep.Owner, dep.Name, containerGitcacheVol)
			return
		}
		writef(r.Runner.Stderr, "ward agent: seeding external dep %s/%s host-side from %s (ward#612)\n",
			dep.Owner, dep.Name, dep.CloneURL)
		tmp, err := os.MkdirTemp("", "ward-extseed-")
		if err != nil {
			writef(r.Runner.Stderr, "ward agent: MISSING DEPENDENCY: could not stage a seed dir for %s/%s: %v; "+
				"the container will fail loud at bring-up (ward#612)\n", dep.Owner, dep.Name, err)
			return
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		dst := filepath.Join(tmp, mirror)
		// Clone on the HOST over the user's default git credentials (agent, then ~/.ssh /
		// ~/.ssh/config); the credential chain stays on the host.
		if cerr := r.Runner.Exec(ctx, "git", "clone", "--mirror", dep.CloneURL, dst); cerr != nil {
			writef(r.Runner.Stderr, "ward agent: MISSING DEPENDENCY: host-side clone of %s failed: %v. "+
				"The host user's default git credentials must have clone access to %s/%s; the sealed "+
				"container will report the missing sibling ../%s at bring-up (ward#611, ward#612)\n",
				dep.CloneURL, cerr, dep.Owner, dep.Name, dep.Name)
			return
		}
		// Copy the finished bare mirror into the volume via a cp-only helper - it touches
		// no external forge, so no credential material crosses into any container.
		if cperr := r.copyMirrorIntoGitcache(ctx, plan.Image, tmp, mirror); cperr != nil {
			writef(r.Runner.Stderr, "ward agent: MISSING DEPENDENCY: staged %s/%s but could not copy it into %s: %v; "+
				"the container will fail loud at bring-up (ward#612)\n", dep.Owner, dep.Name, containerGitcacheVol, cperr)
			return
		}
		writef(r.Runner.Stderr, "ward agent: seeded external dep %s/%s into %s host-side (ward#612)\n",
			dep.Owner, dep.Name, containerGitcacheVol)
	})
}

// gitcacheMirrorPresent reports whether the gitcache volume already holds this bare
// mirror, probed through a throwaway `test -d` helper (docker noise silenced).
func (r *Runner) gitcacheMirrorPresent(ctx context.Context, image, mirror string) bool {
	return r.runDockerSilenced(ctx, true, gitcacheMirrorProbeArgv(image, mirror)...) == nil
}

// copyMirrorIntoGitcache copies a host-staged bare mirror into the gitcache volume via a
// cp-only helper container (the volume is docker-managed, unreachable from host git).
func (r *Runner) copyMirrorIntoGitcache(ctx context.Context, image, srcDir, mirror string) error {
	return r.dockerExec(ctx, gitcacheMirrorCopyArgv(image, srcDir, mirror)...)
}

// gitcacheMirrorProbeArgv builds the `docker run --rm ... test -d` argv reporting
// whether the gitcache volume already holds mirror.
func gitcacheMirrorProbeArgv(image, mirror string) []string {
	return []string{
		"run", "--rm",
		"-v", containerGitcacheVol + ":" + containerGitcacheMnt,
		image, "test", "-d", containerGitcacheMnt + "/" + mirror,
	}
}

// gitcacheMirrorCopyArgv builds the `docker run --rm ... cp -a` argv copying the staged
// bare mirror at srcDir/<mirror> into the gitcache volume (srcDir bound read-only).
func gitcacheMirrorCopyArgv(image, srcDir, mirror string) []string {
	return []string{
		"run", "--rm",
		"-v", containerGitcacheVol + ":" + containerGitcacheMnt,
		"-v", srcDir + ":/ward-seed:ro",
		image, "cp", "-a", "/ward-seed/" + mirror, containerGitcacheMnt + "/" + mirror,
	}
}
