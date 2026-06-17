package main

import (
	"strings"
	"testing"
)

func TestDecideReap(t *testing.T) {
	cases := []struct {
		name string
		in   reapInputs
		want reapAction
	}{
		{"clean tree does nothing", reapInputs{HasResidualWork: false}, reapNothing},
		{"clean integration + clean scan lands on main",
			reapInputs{HasResidualWork: true, IntegrationClean: true}, reapPushMain},
		{"conflict salvages",
			reapInputs{HasResidualWork: true, IntegrationClean: false}, reapSalvage},
		{"scan finding salvages even when integration is clean",
			reapInputs{HasResidualWork: true, IntegrationClean: true,
				Findings: []scanFinding{{"node_modules/x", "vendored"}}}, reapSalvage},
	}
	for _, c := range cases {
		if got := decideReap(c.in); got != c.want {
			t.Errorf("%s: decideReap = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScanDiffFlagsJunk(t *testing.T) {
	entries := []diffEntry{
		{Path: "src/main.go", Bytes: 2000},
		{Path: "web/node_modules/left-pad/index.js", Bytes: 100},
		{Path: "deploy/.env", Bytes: 50},
		{Path: "deploy/.env.example", Bytes: 50},
		{Path: "certs/server.pem", Bytes: 50},
		{Path: "assets/logo.png", Bytes: 2 << 20, Binary: true},
		{Path: "data/dump.sql", Bytes: 8 << 20},
		{Path: "vendor/foo/bar.go", Bytes: 100},
	}
	findings := scanDiff(entries)
	flagged := map[string]string{}
	for _, f := range findings {
		flagged[f.Path] = f.Reason
	}

	mustFlag := []string{
		"web/node_modules/left-pad/index.js",
		"deploy/.env",
		"certs/server.pem",
		"assets/logo.png",
		"data/dump.sql",
		"vendor/foo/bar.go",
	}
	for _, p := range mustFlag {
		if _, ok := flagged[p]; !ok {
			t.Errorf("expected %q to be flagged, was not", p)
		}
	}
	mustPass := []string{"src/main.go", "deploy/.env.example"}
	for _, p := range mustPass {
		if _, ok := flagged[p]; ok {
			t.Errorf("expected %q to pass, was flagged as %q", p, flagged[p])
		}
	}
}

func TestSecretLikePath(t *testing.T) {
	cases := []struct {
		path string
		flag bool
	}{
		{".env", true},
		{"a/b/.env.production", true},
		{".env.sample", false},
		{".env.example", false},
		{"id_rsa", true},
		{"deploy/cluster.key", true},
		{"app/main.go", false},
		{"README.md", false},
	}
	for _, c := range cases {
		_, got := secretLikePath(c.path)
		if got != c.flag {
			t.Errorf("secretLikePath(%q) = %v, want %v", c.path, got, c.flag)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{500, "500 B"},
		{2 << 10, "2.0 KiB"},
		{5 << 20, "5.0 MiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestSalvageBranchAndTitleStable(t *testing.T) {
	if got := salvageBranchName("eco-app-a1b2"); got != "ward-salvage/eco-app-a1b2" {
		t.Errorf("salvageBranchName = %q", got)
	}
	r := salvageReport{
		Repo:   targetRepo{Owner: "coilyco-gaming", Name: "eco-app"},
		Branch: "ward-salvage/eco-app-a1b2",
	}
	title := salvageIssueTitle(r)
	if !strings.HasPrefix(title, salvageIssueTitlePrefix) {
		t.Errorf("title %q missing dedupe prefix", title)
	}
	if !strings.Contains(title, "eco-app") || !strings.Contains(title, r.Branch) {
		t.Errorf("title %q missing repo/branch", title)
	}
}

func TestSalvageIssueBodyRendersRecoveryAndFindings(t *testing.T) {
	r := salvageReport{
		Repo:     targetRepo{Owner: "coilyco-gaming", Name: "eco-app"},
		Mode:     "claude",
		Branch:   "ward-salvage/eco-app-a1b2",
		Reason:   reasonConflict,
		Findings: []scanFinding{{"node_modules/x/i.js", "vendored/generated tree (node_modules/)"}},
		Status:   " M src/main.go\n?? scratch.txt",
		Base:     "https://forgejo.coilysiren.me",
	}
	body := salvageIssueBody(r)
	for _, want := range []string{
		"claude",
		"ward-salvage/eco-app-a1b2",
		string(reasonConflict),
		"git fetch https://forgejo.coilysiren.me/coilyco-gaming/eco-app.git ward-salvage/eco-app-a1b2",
		"node_modules/x/i.js",
		"src/main.go",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body missing %q\n---\n%s", want, body)
		}
	}
}
