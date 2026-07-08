package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// configsource_test.go covers the ward#653 seam: the selection contract plus
// real-bundle integration against .ward/ward-kdl (the aos#332 flat layout).

// TestSelectConfigSourceDefaultsBaked pins the neutral default: no
// WARD_CONFIG_REF means the baked embed, never an error.
func TestSelectConfigSourceDefaultsBaked(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "")
	src, err := selectConfigSource()
	if err != nil {
		t.Fatalf("selectConfigSource with unset ref: %v", err)
	}
	if src.fsys != fs.FS(bakedAssets) {
		t.Error("unset ref did not select the baked embed")
	}
	if src.execMixedDialects {
		t.Error("baked source must not dialect-filter (execassets is pre-filtered)")
	}
}

// TestSelectConfigSourceRejectsMalformedGitRef pins fail-loud: a ref that is
// neither file:// nor the git grammar errors, never a silent baked fallback.
func TestSelectConfigSourceRejectsMalformedGitRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	if _, err := selectConfigSource(); err == nil {
		t.Fatal("malformed ref selected a source; want a loud parse error")
	} else if !strings.Contains(err.Error(), wardConfigRefEnv) {
		t.Errorf("error %q does not name %s", err, wardConfigRefEnv)
	}
}

// TestSelectConfigSourceRejectsMissingDir pins set-but-missing as loud, per
// docs/config-discovery.md's override rule.
func TestSelectConfigSourceRejectsMissingDir(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file:///nonexistent/ward-bundle")
	if _, err := selectConfigSource(); err == nil {
		t.Fatal("missing bundle dir selected a source; want a loud error")
	}
}

// TestSelectConfigSourceFileRef selects a DirFS bundle for the file:// form.
func TestSelectConfigSourceFileRef(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	src, err := selectConfigSource()
	if err != nil {
		t.Fatalf("selectConfigSource(file://%s): %v", dir, err)
	}
	if !src.execMixedDialects {
		t.Error("bundle source must dialect-filter its exec scan")
	}
	if src.fleetKDL != bundleFleetKDLPath {
		t.Errorf("bundle fleet path = %q, want %q", src.fleetKDL, bundleFleetKDLPath)
	}
}

// TestBakedSourcePathsExist guards the path constants against the go:embed
// patterns: a rename must not silently empty the neutral default.
func TestBakedSourcePathsExist(t *testing.T) {
	src := bakedConfigSource()
	for _, p := range []string{src.forgejoGuardfile, src.forgejoSpecLock, src.adminGuardfile, src.fleetKDL} {
		if _, err := fs.ReadFile(src.fsys, p); err != nil {
			t.Errorf("baked path %s unreadable: %v", p, err)
		}
	}
	entries, err := fs.ReadDir(src.fsys, src.execDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("baked exec dir %s empty or unreadable (err=%v)", src.execDir, err)
	}
}

// TestBuildForgejoOpsFromRealBundle compiles `ops forgejo` from the bundle dir;
// no admin guardfile there means the slice is withheld, while baked grafts it.
func TestBuildForgejoOpsFromRealBundle(t *testing.T) {
	forgejo, err := buildForgejoOpsFrom(bundleConfigSource(wardKdlSrcDir))
	if err != nil {
		t.Fatalf("buildForgejoOpsFrom(%s): %v", wardKdlSrcDir, err)
	}
	if commandNamed(forgejo.Commands, "issue") == nil {
		t.Errorf("bundle-built forgejo has no issue command; got %v", commandNames(forgejo.Commands))
	}
	if !strings.Contains(forgejo.Usage, "ward ops forgejo") {
		t.Errorf("bundle-built forgejo Usage = %q, want it rerooted to `ward ops forgejo`", forgejo.Usage)
	}
	if commandNamed(forgejo.Commands, "admin") != nil {
		t.Error("bundle without an admin guardfile still grafted the admin surface")
	}

	baked, err := buildForgejoOpsFrom(bakedConfigSource())
	if err != nil {
		t.Fatalf("buildForgejoOpsFrom(baked): %v", err)
	}
	if commandNamed(baked.Commands, "admin") == nil {
		t.Error("baked build lost the admin graft")
	}
}

// TestMountWardKdlExecFromRealBundle mounts exec surfaces from the mixed bundle
// dir: exec guardfiles graft, spec-dialect siblings filter out, no mount error.
func TestMountWardKdlExecFromRealBundle(t *testing.T) {
	root := newWardKdlTestRoot()
	if err := mountWardKdlExecFrom(root, bundleConfigSource(wardKdlSrcDir), leanRunner()); err != nil {
		t.Fatalf("mountWardKdlExecFrom(%s): %v", wardKdlSrcDir, err)
	}
	agents := commandNamed(root.Commands, "agents")
	if agents == nil || commandNamed(agents.Commands, "ollama") == nil {
		t.Errorf("bundle mount missing agents ollama; root = %v", commandNames(root.Commands))
	}
	if commandNamed(root.Commands, "docker") == nil {
		t.Errorf("bundle mount missing docker; root = %v", commandNames(root.Commands))
	}
	ops := commandNamed(root.Commands, "ops")
	for _, spec := range []string{"trello", "tailscale", "glitchtip", "signoz"} {
		if ops != nil && commandNamed(ops.Commands, spec) != nil {
			t.Errorf("spec-dialect %s guardfile mounted through the exec scan", spec)
		}
	}
}

// TestLoadFleetConfigFromBundleRef parses the dialect-2 fleet config off a
// file:// WARD_CONFIG_REF end to end.
func TestLoadFleetConfigFromBundleRef(t *testing.T) {
	abs, err := filepath.Abs(wardKdlSrcDir)
	if err != nil {
		t.Fatalf("abs(%s): %v", wardKdlSrcDir, err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+abs)
	f, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig from bundle ref: %v", err)
	}
	if f.SchemaVersion == 0 || len(f.Agents) == 0 {
		t.Errorf("bundle fleet config parsed empty (schema=%d, agents=%d)", f.SchemaVersion, len(f.Agents))
	}
}

// TestLoadFleetConfigBundleMissingFleetFailsLoud pins the no-silent-fallback
// rule: a bundle without its fleet KDL errors, it does not read the baked one.
func TestLoadFleetConfigBundleMissingFleetFailsLoud(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+t.TempDir())
	if _, err := loadFleetConfig(); err == nil {
		t.Fatal("empty bundle parsed a fleet config; want a loud read error")
	}
}

// TestOpsCommandDegradesOnBadRef pins the degrade path: a bad WARD_CONFIG_REF
// mounts the error leaf, surfacing on invocation, never a dropped surface.
func TestOpsCommandDegradesOnBadRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	cmd := opsCommand()
	forgejo := commandNamed(cmd.Commands, "forgejo")
	if forgejo == nil {
		t.Fatal("degraded ops umbrella lost the forgejo leaf entirely")
	}
	err := forgejo.Action(context.Background(), forgejo)
	if err == nil || !strings.Contains(err.Error(), "failed to mount") {
		t.Errorf("degraded leaf error = %v, want the failed-to-mount surface", err)
	}
	if err == nil || !strings.Contains(err.Error(), wardConfigRefEnv) {
		t.Errorf("degraded leaf error = %v, want it to name %s", err, wardConfigRefEnv)
	}
}

// TestMountWardKdlExecBadRefErrors: the exec mount refuses a bad ref (main.go
// downgrades that to a loud stderr warning), never a baked fallback.
func TestMountWardKdlExecBadRefErrors(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	if err := mountWardKdlExec(newWardKdlTestRoot(), leanRunner()); err == nil {
		t.Fatal("exec mount succeeded under a bad ref; want a loud error")
	}
}
