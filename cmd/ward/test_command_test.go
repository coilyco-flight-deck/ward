package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testCommandProxyPath string

func prepareTestShellCommands() (func(), error) {
	if runtime.GOOS != "windows" {
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "ward-test-command-*")
	if err != nil {
		return nil, fmt.Errorf("create test command directory: %w", err)
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("locate test command source")
	}
	testCommandProxyPath = filepath.Join(dir, "testcmdproxy.exe")
	cmd := exec.Command("go", "build", "-o", testCommandProxyPath, "./testdata/testcmdproxy")
	cmd.Dir = filepath.Dir(sourceFile)
	cmd.Env = append(withoutEnvironment(os.Environ(), "GOFLAGS"), "GOFLAGS=")
	if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("build test command proxy: %w\n%s", buildErr, output)
	}
	return func() { _ = os.RemoveAll(dir) }, nil
}

func withoutEnvironment(env []string, name string) []string {
	filtered := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if !strings.EqualFold(key, name) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func writeTestShellCommand(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write test command %s: %v", filepath.Base(path), err)
	}
	if runtime.GOOS != "windows" {
		return
	}
	proxyPath := path + ".exe"
	if err := os.Link(testCommandProxyPath, proxyPath); err == nil {
		return
	}
	proxy, err := os.ReadFile(testCommandProxyPath)
	if err != nil {
		t.Fatalf("read test command proxy: %v", err)
	}
	if err := os.WriteFile(proxyPath, proxy, 0o755); err != nil { //nolint:gosec
		t.Fatalf("write test command proxy %s: %v", filepath.Base(proxyPath), err)
	}
}

func testShellPath(path string) string {
	return filepath.ToSlash(path)
}

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestWriteTestShellCommandRunsFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ward-test-command")
	writeTestShellCommand(t, path, "#!/bin/sh\nprintf '%s\\n' \"$@\"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	args := []string{"one", "{{.Names}}", `{{json .Config.Env}}`, `{{index .Config.Labels "ward.role"}}`, "/home/ward"}
	output, err := exec.Command("ward-test-command", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("run test command: %v\n%s", err, output)
	}
	if got, want := string(output), strings.Join(args, "\n")+"\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
