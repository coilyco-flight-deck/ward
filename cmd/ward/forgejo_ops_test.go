package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestWriteForgejoBody verifies the --body-file seam: the temp file holds the exact
// JSON object, cleanup removes it, and a metachar-bearing body survives round-trip.
func TestWriteForgejoBody(t *testing.T) {
	body := "line with `backticks`, $vars, and a pipe | plus\nnewlines"
	path, cleanup, err := writeForgejoBody(map[string]string{"title": "t", "body": body})
	if err != nil {
		t.Fatalf("writeForgejoBody: %v", err)
	}
	defer cleanup()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read body file: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("body file is not a JSON object: %v", err)
	}
	if got["title"] != "t" || got["body"] != body {
		t.Fatalf("round-trip mismatch: got %#v", got)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove %s (err=%v)", path, err)
	}
}

// TestCreateIssueBodyIsSigned guards that createIssue's body-file carries the agent
// attribution footer (ward#155) - signing must survive the cut to the ops runtime.
func TestCreateIssueBodyIsSigned(t *testing.T) {
	signed := modeClaude.signBody("raw report body")
	if !strings.Contains(signed, agentSignatureMarker) {
		t.Fatalf("signBody dropped the marker: %q", signed)
	}
	// The client writes exactly this signed string into the body-file object, so a
	// marker here is the same marker the runtime POSTs.
	path, cleanup, err := writeForgejoBody(map[string]string{"title": "t", "body": signed})
	if err != nil {
		t.Fatalf("writeForgejoBody: %v", err)
	}
	defer cleanup()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read body file: %v", err)
	}
	// Decode the way the ops runtime does (os.ReadFile + json.Unmarshal): the
	// encoder HTML-escapes the marker's <> in the file bytes, but it round-trips.
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("body file is not a JSON object: %v", err)
	}
	if !strings.Contains(got["body"], agentSignatureMarker) {
		t.Fatalf("body lost the signature marker after round-trip: %q", got["body"])
	}
}

// TestForgejoClientInvocationsUseAcceptedFlags is the ward#404 skew guard: every
// flag forgejoClient passes on an `ops forgejo` verb must be one the verb accepts.
func TestForgejoClientInvocationsUseAcceptedFlags(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	// Mirrors the c.run(...) call sites in forgejo_ops.go: the verb path each
	// client method drives, and the flags it passes on that verb.
	cases := []struct {
		name  string
		path  []string
		flags []string
	}{
		{name: "getIssue", path: []string{"issue", "get"}, flags: []string{flagOutput}},
		{name: "listIssueComments", path: []string{"issue-comment", "list"}, flags: []string{flagOutput}},
		{name: "createIssue", path: []string{"issue", "create"}, flags: []string{flagBodyFile, flagOutput}},
		{name: "commentIssue", path: []string{"issue", "comment"}, flags: []string{flagBodyFile}},
		{name: "closeIssue", path: []string{"issue", "close"}, flags: nil},
		{name: "reopenIssue", path: []string{"issue", "reopen"}, flags: nil},
		{name: "listIssues", path: []string{"issue", "list"}, flags: []string{"state", "type", "limit", flagOutput}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := forgejo
			for _, seg := range tc.path {
				cmd = subCommandNamed(cmd, seg)
				if cmd == nil {
					t.Fatalf("verb path %v: no command at segment %q", tc.path, seg)
				}
			}
			for _, fl := range tc.flags {
				if !hasFlagNamed(cmd, fl) {
					t.Errorf("verb %v does not accept --%s, but %s passes it", tc.path, fl, tc.name)
				}
			}
		})
	}
}

// TestCondenseOpsStderr checks the ward#596 stderr condenser: blank lines drop, the
// rest join with "; ", and an over-long envelope is capped (docs/broker.md).
func TestCondenseOpsStderr(t *testing.T) {
	if got := condenseOpsStderr(nil); got != "" {
		t.Errorf("empty stderr: got %q, want \"\"", got)
	}
	if got := condenseOpsStderr([]byte("  \n\n \n")); got != "" {
		t.Errorf("whitespace-only stderr: got %q, want \"\"", got)
	}
	multi := "ward: internal\n\n  check the value provider address and credentials  \nfetch /forgejo/api-token: no aws creds\n"
	want := "ward: internal; check the value provider address and credentials; fetch /forgejo/api-token: no aws creds"
	if got := condenseOpsStderr([]byte(multi)); got != want {
		t.Errorf("condense mismatch:\n got %q\nwant %q", got, want)
	}
	long := strings.Repeat("x", 1000)
	got := condenseOpsStderr([]byte(long))
	if len(got) != 803 || !strings.HasSuffix(got, "...") { // 800 + "..."
		t.Errorf("cap failed: len=%d suffix=%q", len(got), got[len(got)-3:])
	}
}

// TestFoldOpsStderr checks the fold seam: a non-empty stderr rides into the error, an
// empty one leaves the bare exit error untouched (so a silent failure reads as before).
func TestFoldOpsStderr(t *testing.T) {
	base := context.DeadlineExceeded // any stand-in error
	if got := foldOpsStderr(base, nil); got.Error() != base.Error() {
		t.Errorf("empty stderr should return the error unchanged, got %v", got)
	}
	folded := foldOpsStderr(base, []byte("upstream_failed: 404 Not Found"))
	if !strings.Contains(folded.Error(), "404 Not Found") {
		t.Errorf("folded error dropped the stderr detail: %q", folded.Error())
	}
	if !strings.Contains(folded.Error(), base.Error()) {
		t.Errorf("folded error dropped the underlying cause: %q", folded.Error())
	}
}

// TestForgejoGetIssueFlattensLabels pins that getIssue flattens the Forgejo
// label objects to the name list the ceiling gate reads (agentic-os#246).
func TestForgejoGetIssueFlattensLabels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake exe is POSIX-only")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ward")
	script := "#!/bin/sh\ncat <<'JSON'\n" +
		`{"number":246,"title":"t","body":"b","state":"open","html_url":"https://f/246","labels":[{"name":"interactive"},{"name":"P3"}]}` +
		"\nJSON\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil { // #nosec G306 -- test-only executable
		t.Fatalf("write fake ward: %v", err)
	}
	c := &forgejoClient{r: &Runner{Runner: &shell.Runner{}}, exe: fake}
	issue, err := c.getIssue(context.Background(), "coilyco-flight-deck", "agentic-os", 246)
	if err != nil {
		t.Fatalf("getIssue: %v", err)
	}
	if got := strings.Join(issue.Labels, ","); got != "interactive,P3" {
		t.Errorf("issue.Labels = %v, want [interactive P3]", issue.Labels)
	}
	if issue.State != "open" || issue.Number != 246 {
		t.Errorf("issue core fields lost: %+v", issue)
	}
}

// TestRunFoldsSubprocessStderr is the ward#596 end-to-end: a failing `ops forgejo`
// subprocess's stderr cause rides the error, not a bare `exit status N`.
func TestRunFoldsSubprocessStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake exe is POSIX-only")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ward")
	script := "#!/bin/sh\n" +
		"echo 'ward: internal fetch /forgejo/api-token: no aws creds on this host' 1>&2\n" +
		"exit 4\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil { // #nosec G306 -- test-only executable
		t.Fatalf("write fake ward: %v", err)
	}
	c := &forgejoClient{
		r:   &Runner{Runner: &shell.Runner{}},
		exe: fake,
	}
	_, err := c.getIssue(context.Background(), "coilyco-flight-deck", "ward", 596)
	if err == nil {
		t.Fatal("expected getIssue to fail against the exit-4 fake ward")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no aws creds on this host") {
		t.Errorf("folded error lost the subprocess stderr cause: %q", msg)
	}
	if !strings.Contains(msg, "coilyco-flight-deck/ward#596") {
		t.Errorf("folded error lost the issue-ref context: %q", msg)
	}
}
