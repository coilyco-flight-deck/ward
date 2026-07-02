package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
)

// writeMakefile drops a Makefile into dir and returns its path.
func writeMakefile(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	return path
}

func TestParseMakefileTargets_OrderAndSkips(t *testing.T) {
	dir := t.TempDir()
	mk := writeMakefile(t, dir, strings.Join([]string{
		".PHONY: help build",
		"help: ## Print this help.",
		"\t@echo hi",
		"build: ## Build all packages.",
		"\tgo build ./...",
		"test: deps ## Run the unit test suite.",
		"\tgo test ./...",
		"BADNAME: ## uppercase, not a verb",
		"\t@echo x",
		"weird.dot: ## dotted, not a verb",
		"\t@echo x",
		"GO_VERSION := $(shell echo 1)", // assignment, no help - ignored
		"",                              // trailing newline
	}, "\n"))

	targets, skipped, err := parseMakefileTargets(mk)
	if err != nil {
		t.Fatalf("parseMakefileTargets: %v", err)
	}

	// Source order is preserved and descriptions are trimmed verbatim.
	wantNames := []string{"help", "build", "test"}
	if len(targets) != len(wantNames) {
		t.Fatalf("got %d targets, want %d: %+v", len(targets), len(wantNames), targets)
	}
	for i, want := range wantNames {
		if targets[i].name != want {
			t.Errorf("target[%d].name = %q, want %q", i, targets[i].name, want)
		}
	}
	if targets[2].description != "Run the unit test suite." {
		t.Errorf("test description = %q", targets[2].description)
	}

	// Non-verb names are reported, not silently dropped.
	wantSkipped := map[string]bool{"BADNAME": true, "weird.dot": true}
	if len(skipped) != len(wantSkipped) {
		t.Fatalf("skipped = %v, want the two invalid names", skipped)
	}
	for _, s := range skipped {
		if !wantSkipped[s] {
			t.Errorf("unexpected skipped target %q", s)
		}
	}
}

func TestParseMakefileTargets_FirstDescriptionWins(t *testing.T) {
	dir := t.TempDir()
	mk := writeMakefile(t, dir, "build: ## first.\n\t@echo a\nbuild: ## second.\n\t@echo b\n")
	targets, _, err := parseMakefileTargets(mk)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(targets) != 1 || targets[0].description != "first." {
		t.Fatalf("want single build with first description, got %+v", targets)
	}
}

func TestRenderWardYAML_ContractHoldsUnderDoctor(t *testing.T) {
	dir := t.TempDir()
	mk := writeMakefile(t, dir, strings.Join([]string{
		"build: ## Build all packages.",
		"\tgo build ./...",
		// A description with a colon-space + parens must round-trip so the
		// allowlist compare against the Makefile still matches.
		"install: ## Install the binaries into GOBIN (build: test triple).",
		"\tgo install ./...",
		"",
	}, "\n"))
	targets, _, err := parseMakefileTargets(mk)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	yamlPath := filepath.Join(dir, ".ward", "ward.yaml")
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yamlPath, []byte(renderWardYAML(targets)), 0o644); err != nil {
		t.Fatal(err)
	}

	// The generated file must parse and round-trip the colon-bearing description.
	cfg, err := repocfg.Load(yamlPath)
	if err != nil {
		t.Fatalf("repocfg.Load on generated file: %v", err)
	}
	if !securityIsZero(cfg.Security) {
		t.Errorf("commented security scaffold must parse to zero Security, got %+v", cfg.Security)
	}
	var install *repocfg.Command
	for i := range cfg.Commands {
		if cfg.Commands[i].Name == "install" {
			install = &cfg.Commands[i]
		}
	}
	if install == nil || install.Description != "Install the binaries into GOBIN (build: test triple)." {
		t.Fatalf("install description did not round-trip: %+v", install)
	}

	// doctor passes against the generated file: allowlist matches the Makefile,
	// and allowMissingSecurity downgrades the ward#450 no-security: FAIL to a NOTE.
	var out bytes.Buffer
	if err := runDoctorAt(&out, yamlPath, doctorOptions{allowMissingSecurity: true}); err != nil {
		t.Fatalf("runDoctorAt on generated scaffold: %v\noutput:\n%s", err, out.String())
	}
}

func TestRunSetup_WritesRunsDoctorAndGuardsOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeMakefile(t, dir, "test: ## Run the unit test suite.\n\tgo test ./...\n")

	var out bytes.Buffer
	if err := runSetup(&out, setupOptions{dir: dir}); err != nil {
		t.Fatalf("runSetup: %v\n%s", err, out.String())
	}
	yamlPath := filepath.Join(dir, ".ward", "ward.yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Fatalf("expected %s written: %v", yamlPath, err)
	}
	if !strings.Contains(out.String(), "running doctor") {
		t.Errorf("expected doctor to run, output:\n%s", out.String())
	}

	// A second run refuses without --force.
	err := runSetup(&bytes.Buffer{}, setupOptions{dir: dir})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second run must refuse to clobber, got %v", err)
	}

	// --force overwrites; --skip-doctor stops before the doctor line.
	var out2 bytes.Buffer
	if err := runSetup(&out2, setupOptions{dir: dir, force: true, skipDoctor: true}); err != nil {
		t.Fatalf("forced run: %v", err)
	}
	if strings.Contains(out2.String(), "running doctor") {
		t.Errorf("--skip-doctor should not run doctor, output:\n%s", out2.String())
	}
}

func TestRunSetup_NoMakefileAndNoTargets(t *testing.T) {
	// No Makefile anywhere up the tree the temp dir controls.
	empty := t.TempDir()
	if err := runSetup(&bytes.Buffer{}, setupOptions{dir: empty}); err == nil {
		t.Error("expected an error when no Makefile is reachable")
	}

	// Makefile present but no documented targets.
	dir := t.TempDir()
	writeMakefile(t, dir, "build:\n\tgo build ./...\n") // no `## ` help comment
	err := runSetup(&bytes.Buffer{}, setupOptions{dir: dir})
	if err == nil || !strings.Contains(err.Error(), "no Makefile targets") {
		t.Fatalf("expected no-targets error, got %v", err)
	}
}

func TestYAMLScalar_QuotesOnlyWhenNeeded(t *testing.T) {
	cases := map[string]string{
		"Build all packages.":     "Build all packages.",
		"go vet across the tree.": "go vet across the tree.",
		"a: b":                    `"a: b"`, // colon-space forces quoting
		"trailing colon:":         `"trailing colon:"`,
		"has # hash":              `"has # hash"`,
		`quote " inside`:          `"quote \" inside"`,
		"- leading dash":          `"- leading dash"`,
	}
	for in, want := range cases {
		if got := yamlScalar(in); got != want {
			t.Errorf("yamlScalar(%q) = %q, want %q", in, got, want)
		}
	}
}
