package main

import (
	"strings"
	"testing"
	"time"
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

func TestIsAuthFailure(t *testing.T) {
	auth := []string{
		"remote: Invalid username or password.\nfatal: Authentication failed for 'https://forgejo.example/x.git/'",
		"fatal: unable to access 'https://...': The requested URL returned error: 403 Forbidden",
		"remote: Forbidden\nfatal: unable to access",
		"error: 401 Unauthorized",
		"fatal: could not read Username for 'https://forgejo.example': terminal prompts disabled",
	}
	for _, o := range auth {
		if !isAuthFailure(o) {
			t.Errorf("expected auth failure for %q", o)
		}
	}
	notAuth := []string{
		"! [rejected]        HEAD -> main (non-fast-forward)\nerror: failed to push some refs",
		"hint: Updates were rejected because the remote contains work that you do not have locally.\nhint: fetch first",
		"fatal: unable to access 'https://...': Could not resolve host: forgejo.example",
		"",
	}
	for _, o := range notAuth {
		if isAuthFailure(o) {
			t.Errorf("did not expect auth failure for %q", o)
		}
	}
}

func TestFormatTokenAge(t *testing.T) {
	up := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		upAt string
		now  time.Time
		want string
		ok   bool
	}{
		{"hours and minutes", up.Format(time.RFC3339), up.Add(3*time.Hour + 42*time.Minute), "3h42m", true},
		{"days and hours", up.Format(time.RFC3339), up.Add(2*24*time.Hour + 3*time.Hour), "2d3h", true},
		{"minutes only", up.Format(time.RFC3339), up.Add(45 * time.Minute), "45m", true},
		{"sub-minute", up.Format(time.RFC3339), up.Add(30 * time.Second), "30s", true},
		{"empty stamp", "", up, "", false},
		{"unparseable stamp", "not-a-time", up, "", false},
		{"future stamp (clock skew)", up.Format(time.RFC3339), up.Add(-time.Hour), "", false},
	}
	for _, c := range cases {
		got, ok := formatTokenAge(c.upAt, c.now)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: formatTokenAge = (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSalvageIssueBodyStampsAuthCauseAndAge(t *testing.T) {
	r := salvageReport{
		Repo:      targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Mode:      "claude",
		Branch:    "ward-salvage/ward-a1b2",
		Reason:    reasonAuthFail,
		AuthCause: true,
		TokenAge:  "5h12m",
		Base:      "https://forgejo.coilysiren.me",
	}
	body := salvageIssueBody(r)
	for _, want := range []string{
		"Container uptime at reap:",
		"5h12m",
		"dead/rotated PAT",
		"rebase and land cleanly",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("auth-cause body missing %q\n---\n%s", want, body)
		}
	}

	// A conflict salvage (no auth cause, no stamp) must NOT claim a dead PAT.
	conflict := salvageReport{
		Repo:   targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Mode:   "claude",
		Branch: "ward-salvage/ward-c3d4",
		Reason: reasonConflict,
		Base:   "https://forgejo.coilysiren.me",
	}
	cbody := salvageIssueBody(conflict)
	if strings.Contains(cbody, "dead/rotated PAT") {
		t.Errorf("conflict body should not mention a dead PAT\n---\n%s", cbody)
	}
	if strings.Contains(cbody, "Container uptime at reap:") {
		t.Errorf("conflict body should omit uptime when TokenAge is empty\n---\n%s", cbody)
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
