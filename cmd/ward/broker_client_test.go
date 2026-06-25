package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"github.com/urfave/cli/v3"
)

// verbTail splits a specverb leaf name into its classifying (resource, verb) tail.
func TestVerbTail(t *testing.T) {
	cases := []struct{ name, wantRes, wantVerb string }{
		{"ward.ops.forgejo.issue.create", "issue", "create"},
		{"ward.ops.forgejo.repo.delete", "repo", "delete"},
		{"ward.ops.forgejo.org-repo.list", "org-repo", "list"},
		{"solo", "", "solo"},
	}
	for _, c := range cases {
		res, vb := verbTail(c.name)
		if res != c.wantRes || vb != c.wantVerb {
			t.Errorf("verbTail(%q) = (%q, %q), want (%q, %q)", c.name, res, vb, c.wantRes, c.wantVerb)
		}
	}
}

// mapForgejoWriteOp maps only the issue file/edit/comment/close/reopen tier; every
// other verb (delete, repo/label mutations, foreign resources) is out of tier.
func TestMapForgejoWriteOp(t *testing.T) {
	cases := []struct {
		resource, verb string
		wantOp         broker.Op
		wantState      string
		wantOK         bool
	}{
		{"issue", "create", broker.OpFileIssue, "", true},
		{"issue", "edit", broker.OpEditIssue, "", true},
		{"issue", "comment", broker.OpCommentIssue, "", true},
		{"issue", "close", broker.OpEditIssue, "closed", true},
		{"issue", "reopen", broker.OpEditIssue, "open", true},
		{"issue", "delete", "", "", false},
		{"repo", "create", "", "", false},
		{"repo", "delete", "", "", false},
		{"issue-label", "add", "", "", false},
	}
	for _, c := range cases {
		got, ok := mapForgejoWriteOp(c.resource, c.verb)
		if ok != c.wantOK {
			t.Errorf("mapForgejoWriteOp(%q,%q) ok = %v, want %v", c.resource, c.verb, ok, c.wantOK)
			continue
		}
		if ok && (got.op != c.wantOp || got.state != c.wantState) {
			t.Errorf("mapForgejoWriteOp(%q,%q) = %+v, want op %q state %q", c.resource, c.verb, got, c.wantOp, c.wantState)
		}
	}
}

// isOutOfTierRefusal distinguishes an authorizer refusal from a relayed API error.
func TestIsOutOfTierRefusal(t *testing.T) {
	refusals := []string{
		`broker: operation "delete" not permitted`,
		`broker: owner "evilcorp" is out of scope (restricted to coily* owners)`,
		`broker: edit_issue requires a positive issue number`,
	}
	for _, m := range refusals {
		if !isOutOfTierRefusal(m) {
			t.Errorf("isOutOfTierRefusal(%q) = false, want true", m)
		}
	}
	apiErrors := []string{
		"broker: ward-kdl-write ops forgejo issue create: exit status 1: 404 not found",
		"some unrelated failure",
	}
	for _, m := range apiErrors {
		if isOutOfTierRefusal(m) {
			t.Errorf("isOutOfTierRefusal(%q) = true, want false", m)
		}
	}
}

// forgejoWritePayload reads the body flags, falling back to a --body-file JSON
// object for fields the flags leave unset (the shape ward's own client writes).
func TestForgejoWritePayload(t *testing.T) {
	t.Run("flags win", func(t *testing.T) {
		title, body, state := payloadVia(t, []string{"--title", "T", "--body", "B", "--state", "closed"})
		if title != "T" || body != "B" || state != "closed" {
			t.Errorf("got (%q,%q,%q), want (T,B,closed)", title, body, state)
		}
	})
	t.Run("body-file fills unset fields", func(t *testing.T) {
		dir := t.TempDir()
		bf := filepath.Join(dir, "body.json")
		if err := os.WriteFile(bf, []byte(`{"title":"FT","body":"FB"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		title, body, state := payloadVia(t, []string{"--body-file", bf})
		if title != "FT" || body != "FB" || state != "" {
			t.Errorf("got (%q,%q,%q), want (FT,FB,)", title, body, state)
		}
	})
	t.Run("flag overrides body-file", func(t *testing.T) {
		dir := t.TempDir()
		bf := filepath.Join(dir, "body.json")
		if err := os.WriteFile(bf, []byte(`{"title":"FT","body":"FB"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		title, body, _ := payloadVia(t, []string{"--title", "OVERRIDE", "--body-file", bf})
		if title != "OVERRIDE" || body != "FB" {
			t.Errorf("got (%q,%q), want (OVERRIDE,FB)", title, body)
		}
	})
}

// payloadVia parses argv through a flagged command and returns the resolved payload.
func payloadVia(t *testing.T, argv []string) (title, body, state string) {
	t.Helper()
	var gotT, gotB, gotS string
	cmd := writeFlagCommand(func(_ context.Context, c *cli.Command) error {
		var err error
		gotT, gotB, gotS, err = forgejoWritePayload(c)
		return err
	})
	if err := cmd.Run(context.Background(), append([]string{"leaf"}, argv...)); err != nil {
		t.Fatalf("run: %v", err)
	}
	return gotT, gotB, gotS
}

// renderBrokerResult honors --query number (the --quiet capture), --output json
// (machine callers), and a human default.
func TestRenderBrokerResult(t *testing.T) {
	target := broker.Target{Owner: "coilyco", Repo: "ward", Number: 7}
	res := broker.Result{Number: 7, URL: "https://forge/i/7"}

	got := captureRender(t, []string{"--query", "number"}, target, res)
	if strings.TrimSpace(got) != "7" {
		t.Errorf("--query number rendered %q, want 7", got)
	}

	got = captureRender(t, []string{"--output", "json"}, target, res)
	if !strings.Contains(got, `"number":7`) || !strings.Contains(got, `"html_url":"https://forge/i/7"`) {
		t.Errorf("--output json rendered %q, want number+html_url", got)
	}

	got = captureRender(t, nil, target, res)
	if !strings.Contains(got, "coilyco/ward#7") || !strings.Contains(got, "via broker") {
		t.Errorf("human render %q, want the ref + via-broker note", got)
	}
}

// captureRender runs renderBrokerResult under the given flags and returns stdout.
func captureRender(t *testing.T, argv []string, target broker.Target, res broker.Result) string {
	t.Helper()
	var out string
	cmd := writeFlagCommand(func(_ context.Context, c *cli.Command) error {
		captured, err := captureLeafStdout(func() error { return renderBrokerResult(c, target, res) })
		out = captured
		return err
	})
	if err := cmd.Run(context.Background(), append([]string{"leaf"}, argv...)); err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

// TestBrokerForgejoActionRouting exercises the full Wrap interception over a live
// broker: writes route, out-of-tier refuses, reads pass through, unbrokered is direct.
func TestBrokerForgejoActionRouting(t *testing.T) {
	fake := &fakeExecutor{result: broker.Result{Number: 99, URL: "https://forge/i/99"}}
	sock := serveTestBroker(t, fake)

	r := &Runner{}

	t.Run("unbrokered: always direct", func(t *testing.T) {
		t.Setenv(envBrokerSocket, "")
		called := false
		act := r.brokerForgejoAction("ward.ops.forgejo.issue.create", markCalled(&called))
		runLeaf(t, act, []string{"coilyco", "ward", "--title", "hi"})
		if !called {
			t.Error("unbrokered create must run the direct action")
		}
	})

	t.Run("brokered write routes to broker", func(t *testing.T) {
		t.Setenv(envBrokerSocket, sock)
		fake.fileCalled = false
		called := false
		act := r.brokerForgejoAction("ward.ops.forgejo.issue.create", markCalled(&called))
		out := captureLeaf(t, act, []string{"coilyco", "ward", "--title", "hi", "--body", "b"})
		if called {
			t.Error("a brokered write must NOT run the direct (token) action")
		}
		if !fake.fileCalled {
			t.Error("a brokered create must reach the broker executor")
		}
		if !strings.Contains(out, "coilyco/ward#99") {
			t.Errorf("brokered create output = %q, want the created ref", out)
		}
	})

	t.Run("brokered out-of-tier delete is refused locally", func(t *testing.T) {
		t.Setenv(envBrokerSocket, sock)
		called := false
		act := r.brokerForgejoAction("ward.ops.forgejo.repo.delete", markCalled(&called))
		err := runLeafErr(t, act, []string{"coilyco", "ward"})
		if err == nil || !strings.Contains(err.Error(), "write tier only") {
			t.Errorf("delete error = %v, want a 'write tier only' refusal", err)
		}
		if called {
			t.Error("a refused out-of-tier verb must not run the direct action")
		}
	})

	t.Run("brokered read passes through to direct", func(t *testing.T) {
		t.Setenv(envBrokerSocket, sock)
		called := false
		act := r.brokerForgejoAction("ward.ops.forgejo.issue.get", markCalled(&called))
		runLeaf(t, act, []string{"coilyco", "ward", "5"})
		if !called {
			t.Error("a brokered read must still run the direct action")
		}
	})
}

// serveTestBroker stands up a real broker over a fresh socket and returns its path.
func serveTestBroker(t *testing.T, ex broker.Executor) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "broker.sock")
	ln, err := newBrokerListener(sock, os.Getgid())
	if err != nil {
		t.Fatalf("newBrokerListener: %v", err)
	}
	srv, err := broker.NewServer(ln, ex, writeTierAuthorizer(brokerOwnerPrefix))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	return sock
}

// markCalled returns an action that records it ran, standing in for the direct leaf.
func markCalled(flag *bool) cli.ActionFunc {
	return func(context.Context, *cli.Command) error {
		*flag = true
		return nil
	}
}

// writeFlagCommand builds a leaf carrying the write-leaf flags the broker route reads.
func writeFlagCommand(action cli.ActionFunc) *cli.Command {
	return &cli.Command{
		Name: "leaf",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title"},
			&cli.StringFlag{Name: "body"},
			&cli.StringFlag{Name: "state"},
			&cli.StringFlag{Name: "body-file"},
			&cli.StringFlag{Name: flagOutput},
			&cli.StringFlag{Name: flagQuery},
			&cli.BoolFlag{Name: flagDryRun},
		},
		Action: action,
	}
}

// runLeaf runs a wrapped action through a flagged command, failing on error.
func runLeaf(t *testing.T, action cli.ActionFunc, argv []string) {
	t.Helper()
	if err := runLeafErr(t, action, argv); err != nil {
		t.Fatalf("run leaf: %v", err)
	}
}

// runLeafErr runs a wrapped action through a flagged command and returns its error.
func runLeafErr(t *testing.T, action cli.ActionFunc, argv []string) error {
	t.Helper()
	cmd := writeFlagCommand(action)
	return cmd.Run(context.Background(), append([]string{"leaf"}, argv...))
}

// captureLeaf runs a wrapped action and returns its stdout.
func captureLeaf(t *testing.T, action cli.ActionFunc, argv []string) string {
	t.Helper()
	out, err := captureLeafStdout(func() error { return runLeafErr(t, action, argv) })
	if err != nil {
		t.Fatalf("capture leaf: %v", err)
	}
	return out
}
