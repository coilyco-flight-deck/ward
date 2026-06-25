package main

import (
	"fmt"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestOverrideForgejoCreateIssueAddsQuietFlag asserts the built `issue create`
// leaf gains the --quiet flag and keeps an action after the override (ward#316).
func TestOverrideForgejoCreateIssueAddsQuietFlag(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	issue := subCommandNamed(forgejo, "issue")
	if issue == nil {
		t.Fatalf("forgejo group missing issue command")
	}
	create := subCommandNamed(issue, "create")
	if create == nil {
		t.Fatalf("issue command missing create leaf")
	}
	if create.Action == nil {
		t.Errorf("issue create leaf has no action after override")
	}
	if !hasFlagNamed(create, flagQuiet) {
		t.Errorf("issue create leaf missing --%s flag after override", flagQuiet)
	}
}

// hasFlagNamed reports whether cmd carries a flag answering to name.
func hasFlagNamed(cmd *cli.Command, name string) bool {
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == name {
				return true
			}
		}
	}
	return false
}

// TestFormatCreatedIssueRef covers the projection-to-ref reshape: clean number
// to terse ref, whitespace trimmed, non-numeric rejected (ward#316).
func TestFormatCreatedIssueRef(t *testing.T) {
	cases := []struct {
		name      string
		captured  string
		want      string
		wantError bool
	}{
		{name: "plain", captured: "317\n", want: "coilyco-flight-deck/ward#317"},
		{name: "padded", captured: "  42  ", want: "coilyco-flight-deck/ward#42"},
		{name: "empty", captured: "", wantError: true},
		{name: "yaml leak", captured: "number: 9\n", wantError: true},
		{name: "non-numeric", captured: "error\n", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := formatCreatedIssueRef("coilyco-flight-deck", "ward", tc.captured)
			if tc.wantError {
				if err == nil {
					t.Fatalf("formatCreatedIssueRef(%q) = %q, want error", tc.captured, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("formatCreatedIssueRef(%q): %v", tc.captured, err)
			}
			if got != tc.want {
				t.Errorf("formatCreatedIssueRef(%q) = %q, want %q", tc.captured, got, tc.want)
			}
		})
	}
}

// TestCaptureLeafStdout asserts the redirect helper returns exactly what fn wrote to
// stdout and propagates fn's error without leaking the captured bytes (ward#316).
func TestCaptureLeafStdout(t *testing.T) {
	out, err := captureLeafStdout(func() error {
		fmt.Print("317\n")
		return nil
	})
	if err != nil {
		t.Fatalf("captureLeafStdout: %v", err)
	}
	if out != "317\n" {
		t.Errorf("captureLeafStdout = %q, want %q", out, "317\n")
	}

	sentinel := fmt.Errorf("boom")
	out, err = captureLeafStdout(func() error {
		fmt.Print("leaked")
		return sentinel
	})
	if err != sentinel {
		t.Errorf("captureLeafStdout error = %v, want %v", err, sentinel)
	}
	if out != "" {
		t.Errorf("captureLeafStdout on error = %q, want empty", out)
	}
}
