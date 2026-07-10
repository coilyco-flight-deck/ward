package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// writeCatalogConfig writes a .ward/ward.yaml declaring the given dependsOn entries
// under a fresh temp dir and returns that dir (a stand-in host cwd).
func writeCatalogConfig(t *testing.T, deps ...string) string {
	t.Helper()
	dir := t.TempDir()
	wardDir := filepath.Join(dir, ".ward")
	if err := os.MkdirAll(wardDir, 0o755); err != nil {
		t.Fatalf("mkdir ward: %v", err)
	}
	body := "catalog:\n  dependsOn:\n"
	for _, d := range deps {
		body += "    - " + d + "\n"
	}
	if err := os.WriteFile(filepath.Join(wardDir, "ward.yaml"), []byte(body), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write ward.yaml: %v", err)
	}
	return dir
}

// externalContextDeps keeps only the non-Forgejo entries (the ones needing a
// host-side seed); bare owner/name and Forgejo URLs are internal and dropped.
func TestExternalContextDepsFiltersToExternal(t *testing.T) {
	cwd := writeCatalogConfig(t,
		"ssh://git@github.com/acme/widgets.git",
		"coilyco-flight-deck/eco-protos",
		"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard",
	)
	deps := externalContextDeps(cwd)
	if len(deps) != 1 {
		t.Fatalf("externalContextDeps = %+v, want only the github widgets dep", deps)
	}
	if deps[0].slug() != "acme/widgets" || deps[0].Host != "github.com" ||
		deps[0].CloneURL != "ssh://git@github.com/acme/widgets.git" {
		t.Fatalf("external dep = %+v, want widgets over ssh on github.com", deps[0])
	}
}

// A cwd with no config (the dispatch was not launched from the target repo) yields
// no external deps to seed - the container then fails loud rather than mounting empty.
func TestExternalContextDepsNoConfig(t *testing.T) {
	if deps := externalContextDeps(t.TempDir()); deps != nil {
		t.Fatalf("externalContextDeps with no config = %+v, want nil", deps)
	}
}

// The seed helper argv target the shared gitcache volume + mount, bind the staged
// clone read-only, and name the exact mirror dir - the plumbing the seed depends on.
func TestGitcacheSeedArgv(t *testing.T) {
	probe := gitcacheMirrorProbeArgv("img:latest", "acme__widgets.git")
	wantProbe := []string{"run", "--rm", "-v", "ward-gitcache:/gitcache", "img:latest",
		"test", "-d", "/gitcache/acme__widgets.git"}
	if strings.Join(probe, " ") != strings.Join(wantProbe, " ") {
		t.Fatalf("gitcacheMirrorProbeArgv = %v, want %v", probe, wantProbe)
	}
	cp := gitcacheMirrorCopyArgv("img:latest", "/tmp/stage", "acme__widgets.git")
	wantCp := []string{"run", "--rm", "-v", "ward-gitcache:/gitcache",
		"-v", "/tmp/stage:/ward-seed:ro", "img:latest",
		"cp", "-a", "/ward-seed/acme__widgets.git", "/gitcache/acme__widgets.git"}
	if strings.Join(cp, " ") != strings.Join(wantCp, " ") {
		t.Fatalf("gitcacheMirrorCopyArgv = %v, want %v", cp, wantCp)
	}
}

// seedStubRunner builds a Runner whose git + docker resolve to per-name stubs that
// append their argv to logPath, with docker reporting the mirror absent (test -> 1).
func seedStubRunner(t *testing.T, logPath string, gitFails bool) (*Runner, *strings.Builder) {
	t.Helper()
	dir := t.TempDir()
	gitExit := "exit 0"
	if gitFails {
		gitExit = "exit 1"
	}
	git := "#!/bin/sh\n" +
		"echo \"git $*\" >> " + logPath + "\n" +
		"# clone --mirror URL DST: create DST so the copy step has a mirror to lift\n" +
		"for a in \"$@\"; do dst=\"$a\"; done\n" +
		"[ \"$1\" = clone ] && mkdir -p \"$dst\"\n" +
		gitExit + "\n"
	docker := "#!/bin/sh\n" +
		"echo \"docker $*\" >> " + logPath + "\n" +
		"# a bare `test -d` probe reports the mirror ABSENT so the seed fires; cp succeeds\n" +
		"case \"$*\" in *' test -d '*) exit 1 ;; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(git), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	var errb strings.Builder
	r := &Runner{Runner: &shell.Runner{
		Stdout:  &strings.Builder{},
		Stderr:  &errb,
		Resolve: func(bin string) (string, error) { return filepath.Join(dir, bin), nil },
	}}
	return r, &errb
}

// A first-time external dep clones host-side, then copies the bare mirror
// into the gitcache volume via the cp-only helper (ward#612).
func TestSeedExternalContextMirrorClonesAndCopies(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmds.log")
	r, errb := seedStubRunner(t, logPath, false)
	dep := catalogContextRepo{
		targetRepo: targetRepo{Owner: "acme", Name: "widgets"},
		Host:       "github.com",
		CloneURL:   "ssh://git@github.com/acme/widgets.git",
	}
	r.seedExternalContextMirror(t.Context(), sampleUpPlan(), dep)

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	got := string(log)
	if !strings.Contains(got, "git clone --mirror ssh://git@github.com/acme/widgets.git") {
		t.Errorf("expected a host-side clone, got commands:\n%s", got)
	}
	if !strings.Contains(got, "cp -a /ward-seed/acme__widgets.git /gitcache/acme__widgets.git") {
		t.Errorf("expected a cp of the mirror into the gitcache volume, got commands:\n%s", got)
	}
	if !strings.Contains(errb.String(), "seeded external dep acme/widgets") {
		t.Errorf("expected a success line naming the seeded dep, got stderr:\n%s", errb.String())
	}
}

// A host-side clone the credentials cannot read fails LOUD - a MISSING DEPENDENCY
// line naming the dep - and never copies an empty mirror into the volume (ward#612).
func TestSeedExternalContextMirrorFailsLoudOnCloneError(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmds.log")
	r, errb := seedStubRunner(t, logPath, true)
	dep := catalogContextRepo{
		targetRepo: targetRepo{Owner: "acme", Name: "widgets"},
		Host:       "github.com",
		CloneURL:   "ssh://git@github.com/acme/widgets.git",
	}
	r.seedExternalContextMirror(t.Context(), sampleUpPlan(), dep)

	if !strings.Contains(errb.String(), "MISSING DEPENDENCY") ||
		!strings.Contains(errb.String(), "acme/widgets") {
		t.Errorf("expected a loud MISSING DEPENDENCY naming the dep, got stderr:\n%s", errb.String())
	}
	log, _ := os.ReadFile(logPath)
	if strings.Contains(string(log), "cp -a") {
		t.Errorf("a failed clone must not copy anything into the volume, got:\n%s", string(log))
	}
}
