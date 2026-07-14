package main

import "testing"

func TestParseIssueRef(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		want     IssueRef
		wantPlat Platform
	}{
		{
			name:     "github issue url",
			in:       "https://github.com/coilyco-flight-deck/ward/issues/98",
			want:     IssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98, Platform: PlatformGitHub},
			wantPlat: PlatformGitHub,
		},
		{
			name:     "github compact ref",
			in:       "coilyco-flight-deck/ward#98",
			want:     IssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98, Platform: ""},
			wantPlat: "",
		},
		{
			name:     "forgejo issue url",
			in:       forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98",
			want:     IssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98, Platform: PlatformForgejo},
			wantPlat: PlatformForgejo,
		},
		{
			name:     "scheme-less forgejo url",
			in:       "forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98",
			want:     IssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98, Platform: PlatformForgejo},
			wantPlat: PlatformForgejo,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseIssueRef(forgejoBaseURL, tc.in)
			if err != nil {
				t.Fatalf("ParseIssueRef(%q): %v", tc.in, err)
			}
			if got.Owner != tc.want.Owner || got.Repo != tc.want.Repo || got.Number != tc.want.Number {
				t.Fatalf("ParseIssueRef(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
			if got.Platform != tc.wantPlat {
				t.Fatalf("ParseIssueRef(%q) platform = %q, want %q", tc.in, got.Platform, tc.wantPlat)
			}
		})
	}
}
