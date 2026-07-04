package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnapDockerBin pins the ward#557 fail-fast detection: a snap-tree docker is
// flagged with its path; a native or absent docker reads clean so launch proceeds.
func TestSnapDockerBin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lookPath func(string) (string, error)
		want     string
	}{
		{
			name:     "snap docker on PATH",
			lookPath: func(string) (string, error) { return "/snap/bin/docker", nil },
			want:     "/snap/bin/docker",
		},
		{
			name:     "native docker-ce",
			lookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
			want:     "",
		},
		{
			name:     "docker desktop on mac",
			lookPath: func(string) (string, error) { return "/usr/local/bin/docker", nil },
			want:     "",
		},
		{
			name:     "no docker on PATH",
			lookPath: func(string) (string, error) { return "", errors.New("not found") },
			want:     "",
		},
		{
			// /snapfoo/docker must not read as under /snap - a path-segment boundary.
			name:     "sibling dir named like snap",
			lookPath: func(string) (string, error) { return "/snapfoo/docker", nil },
			want:     "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapDockerBin(tc.lookPath); got != tc.want {
				t.Errorf("snapDockerBin() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDockerPathIsSnapFollowsSymlinkToWrapper covers the PATH-shim shape: a docker
// symlink whose canonical target is the snap runtime wrapper (basename "snap").
func TestDockerPathIsSnapFollowsSymlinkToWrapper(t *testing.T) {
	dir := t.TempDir()
	// The snap runtime wrapper /snap/bin/docker canonically resolves to is named
	// "snap"; model that as a plain file here so the check keys on the basename.
	snapRuntime := filepath.Join(dir, "snap")
	if err := os.WriteFile(snapRuntime, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "docker")
	if err := os.Symlink(snapRuntime, shim); err != nil {
		t.Fatal(err)
	}
	if !dockerPathIsSnap(shim) {
		t.Errorf("dockerPathIsSnap(%q) = false, want true (symlink resolves to the snap wrapper)", shim)
	}

	// A symlink to an ordinary native docker must not read as snap.
	nativeDocker := filepath.Join(dir, "docker.real")
	if err := os.WriteFile(nativeDocker, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nativeShim := filepath.Join(dir, "docker-native")
	if err := os.Symlink(nativeDocker, nativeShim); err != nil {
		t.Fatal(err)
	}
	if dockerPathIsSnap(nativeShim) {
		t.Errorf("dockerPathIsSnap(%q) = true, want false (native docker via symlink)", nativeShim)
	}
}

// TestPathUnderSnap guards the path-segment boundary so /snapfoo is not under /snap.
func TestPathUnderSnap(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/snap", true},
		{"/snap/bin/docker", true},
		{"/snap/", true},
		{"/snapfoo/docker", false},
		{"/usr/bin/docker", false},
		{"/usr/local/bin/docker", false},
	} {
		if got := pathUnderSnap(tc.path); got != tc.want {
			t.Errorf("pathUnderSnap(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestSnapDockerRemediation checks the fail-fast message names the cause, the
// offending path, and the docker-ce fix so an operator can act on it.
func TestSnapDockerRemediation(t *testing.T) {
	msg := snapDockerRemediation("/snap/bin/docker")
	for _, want := range []string{
		"/snap/bin/docker",
		"private /tmp",
		"exit 125",
		"docker-ce",
		"ward#557",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("remediation message missing %q:\n%s", want, msg)
		}
	}
}
