package main

import "testing"

func TestSplitStackCompactRefUsesTypedAuthority(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "ignored")
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatal(err)
	}
	if got := defs.authorityForRepo("coilysiren", "site").Tracker; got != trackerGitHub {
		t.Fatalf("typed tracker = %s, want github", got)
	}

	ref, err := parseAgentIssueRef("coilysiren/site#247")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Tracker != trackerGitHub {
		t.Fatalf("compact ref tracker = %s, want github", ref.Tracker)
	}
}
