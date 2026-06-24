package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
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
