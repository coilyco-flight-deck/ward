package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
	"github.com/urfave/cli/v3"
)

// configsource_test.go covers the ward#653 seam: the selection contract plus
// real-bundle integration against the tracked neutral bundle in .ward/ward-kdl.

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
	if src.defaultsKDL != bundleDefaultsKDLPath {
		t.Errorf("bundle defaults path = %q, want %q", src.defaultsKDL, bundleDefaultsKDLPath)
	}
	if src.topologyKDL != bundleTopologyKDLPath {
		t.Errorf("bundle topology path = %q, want %q", src.topologyKDL, bundleTopologyKDLPath)
	}
}

// TestSelectConfigSourceFileRefCapturesRevision pins the audit-integrity seam:
// when a local bundle is itself a git checkout, ward records its HEAD sha.
func TestSelectConfigSourceFileRefCapturesRevision(t *testing.T) {
	dir := t.TempDir()
	gitFixture(t, dir, "init", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, bundleFleetKDLPath), []byte("fleet { schema-version 2 }"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "add", ".")
	gitFixture(t, dir, "commit", "-m", "bundle")
	sha := gitFixture(t, dir, "rev-parse", "HEAD")

	t.Setenv(wardConfigRefEnv, "file://"+dir)
	src, err := selectConfigSource()
	if err != nil {
		t.Fatalf("selectConfigSource(file://%s): %v", dir, err)
	}
	if src.auditVersion != sha {
		t.Fatalf("auditVersion = %q, want bundle HEAD %q", src.auditVersion, sha)
	}
}

// TestBakedSourcePathsExist guards the path constants against the go:embed
// patterns: a rename must not silently empty the neutral default.
func TestBakedSourcePathsExist(t *testing.T) {
	src := bakedConfigSource()
	for _, p := range []string{src.forgejoGuardfile, src.forgejoSpecLock, src.fleetKDL, src.defaultsKDL, src.topologyKDL} {
		if _, err := fs.ReadFile(src.fsys, p); err != nil {
			t.Errorf("baked path %s unreadable: %v", p, err)
		}
	}
	entries, err := fs.ReadDir(src.fsys, src.execDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("baked exec dir %s empty or unreadable (err=%v)", src.execDir, err)
	}
}

// TestBuildForgejoOpsFromRealBundle compiles `ops forgejo` from the neutral bundle.
// The bundled admin guardfile is absent, so baked grafts it.
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
	if commandNamed(baked.Commands, "admin") != nil {
		t.Error("baked build still mounted the removed admin surface")
	}
}

// TestMountWardKdlExecFromRealBundle mounts exec surfaces from the local neutral
// bundle: exec guardfiles graft, spec-dialect siblings filter out, no mount error.
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

// TestLoadFleetConfigFromBundleSource parses the dialect-2 fleet config from an
// explicit bundle source, independent of WARD_CONFIG_REF.
func TestLoadFleetConfigFromBundleSource(t *testing.T) {
	abs, err := filepath.Abs(wardKdlSrcDir)
	if err != nil {
		t.Fatalf("abs(%s): %v", wardKdlSrcDir, err)
	}
	raw, err := loadRawFleetConfigFrom(bundleConfigSource(abs))
	if err != nil {
		t.Fatalf("loadRawFleetConfigFrom bundle source: %v", err)
	}
	f, err := resolveEffectiveFleet(raw)
	if err != nil {
		t.Fatalf("resolveEffectiveFleet: %v", err)
	}
	if f.SchemaVersion == 0 || len(f.Agents) == 0 {
		t.Errorf("bundle fleet config parsed empty (schema=%d, agents=%d)", f.SchemaVersion, len(f.Agents))
	}
}

// TestCoreLoadFleetConfigIgnoresBadConfigRef pins the boundary: a bad
// WARD_CONFIG_REF does not block the baked fleet defaults.
func TestCoreLoadFleetConfigIgnoresBadConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	f, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig with bad ref: %v", err)
	}
	if f.SchemaVersion == 0 || len(f.Agents) == 0 {
		t.Errorf("core fleet config parsed empty (schema=%d, agents=%d)", f.SchemaVersion, len(f.Agents))
	}
	if claude, ok := fleetAgent(f, string(modeClaude)); !ok || claude.Binary != "claude" {
		t.Errorf("core fleet config lost claude defaults: %+v (ok=%v)", claude, ok)
	}
}

// TestLoadContainerTopologyFromBundleSource parses the staged topology bundle
// from an explicit bundle source, independent of WARD_CONFIG_REF.
func TestLoadContainerTopologyFromBundleSource(t *testing.T) {
	dir := t.TempDir()
	src := `topology {
    tailnet-network "net-x"
    tailnet-proxy "proxy-x:9050"
    tower-host "tower-x"
    tower-ollama-port "19090"
    substrate-seed "/seed-x"
    substrate-dest "/dest-x"
    substrate-manifest "/manifest-x"
    substrate-ttl "42"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleTopologyKDLPath), []byte(src), 0o644); err != nil {
		t.Fatalf("write topology bundle: %v", err)
	}
	topo, err := loadContainerTopologyFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadContainerTopologyFrom(bundle source): %v", err)
	}
	if topo.TailnetNetwork != "net-x" || topo.TailnetProxy != "proxy-x:9050" || topo.TowerHost != "tower-x" || topo.TowerOllamaPort != "19090" {
		t.Errorf("bundle topology parsed unexpectedly: %+v", topo)
	}
	if topo.SubstrateSeed != "/seed-x" || topo.SubstrateDest != "/dest-x" || topo.SubstrateManifest != "/manifest-x" || topo.SubstrateTTL != "42" {
		t.Errorf("bundle substrate topology parsed unexpectedly: %+v", topo)
	}
}

// TestCoreContainerTopologyIgnoresBadConfigRef pins the boundary: a bad
// WARD_CONFIG_REF does not block the baked container topology.
func TestCoreContainerTopologyIgnoresBadConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	topo, err := currentContainerTopologyWithError()
	if err != nil {
		t.Fatalf("currentContainerTopologyWithError with bad ref: %v", err)
	}
	if topo.TailnetNetwork != defaultTailnetNetwork || topo.TailnetProxy != defaultTailnetProxy || topo.TowerHost != defaultTowerHost || topo.TowerOllamaPort != defaultTowerOllamaPort {
		t.Errorf("core container topology drifted: %+v", topo)
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

// TestWrapVerbStampsConfigVersion pins the audit row's bundle attribution:
// the config-bundle sha flows into the recorded version field.
func TestWrapVerbStampsConfigVersion(t *testing.T) {
	dir := t.TempDir()
	gitFixture(t, dir, "init", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, bundleFleetKDLPath), []byte("fleet { schema-version 2 }"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "add", ".")
	gitFixture(t, dir, "commit", "-m", "bundle")
	sha := gitFixture(t, dir, "rev-parse", "HEAD")

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	r := &Runner{Audit: audit.NewWriter(path), configAuditVersion: sha}
	wrapped := r.WrapVerb(verb.Spec{
		Name:   "test.version",
		Action: func(context.Context, *cli.Command) error { return nil },
	}, r.Audit)
	if err := wrapped(context.Background(), &cli.Command{}); err != nil {
		t.Fatalf("wrapped verb: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	rows, err := audit.ReadAll(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("decode audit row: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if rows[0].Version != sha {
		t.Fatalf("audit row version = %q, want %q", rows[0].Version, sha)
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
