package main

import (
	"strings"
	"testing"
)

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"v0.5.2", [3]int{0, 5, 2}, true},
		{"0.5.2", [3]int{0, 5, 2}, true},
		{" v1.2.3 ", [3]int{1, 2, 3}, true},
		{"v0.5", [3]int{0, 5, 0}, true},       // missing patch -> zero-padded
		{"v3", [3]int{3, 0, 0}, true},         // major only
		{"v0.5.2-rc1", [3]int{0, 5, 2}, true}, // prerelease suffix dropped
		{"v0.5.2+build7", [3]int{0, 5, 2}, true},
		{"v0.5.2.1", [3]int{0, 5, 2}, true}, // 4th part ignored
		{"", [3]int{}, false},
		{"v", [3]int{}, false},
		{"dev", [3]int{}, false},
		{"vfoo", [3]int{}, false},
		{"v0.x.2", [3]int{}, false},
	}
	for _, tc := range cases {
		got, ok := parseSemver(tc.in)
		if ok != tc.ok {
			t.Errorf("parseSemver(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestVersionBehind(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.5.1", "v0.5.2", true},   // patch behind
		{"v0.4.9", "v0.5.0", true},   // minor behind
		{"v0.9.9", "v1.0.0", true},   // major behind
		{"v0.5.2", "v0.5.2", false},  // current
		{"v0.5.3", "v0.5.2", false},  // ahead (never nag)
		{"dev", "v0.5.2", false},     // dev build never nags
		{"", "v0.5.2", false},        // blank never nags
		{"v0.5.2", "", false},        // unparseable latest -> no nag
		{"v0.5.2", "garbage", false}, // unparseable latest -> no nag
		{"v0.5", "v0.5.0", false},    // zero-padded equality
		{"v0.5.0", "v0.5", false},
	}
	for _, tc := range cases {
		if got := versionBehind(tc.current, tc.latest); got != tc.want {
			t.Errorf("versionBehind(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestVersionLooksReleased(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"v0.5.2", true},
		{"0.5.2", true},
		{"dev", false},
		{"", false},
		{"  ", false},
	} {
		if got := versionLooksReleased(tc.in); got != tc.want {
			t.Errorf("versionLooksReleased(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestWardOutdatedNotice(t *testing.T) {
	got := wardOutdatedNotice("v0.5.1", "v0.5.2")
	for _, want := range []string{"v0.5.1", "v0.5.2", "ward upgrade", "behind"} {
		if !strings.Contains(got, want) {
			t.Errorf("wardOutdatedNotice missing %q; got:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("wardOutdatedNotice should end in a newline; got %q", got)
	}
}
