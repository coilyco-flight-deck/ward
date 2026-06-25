package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/credseed"
)

// recordingRunner captures the one command's argv + env and returns canned
// output, asserting argv shaping without the real binary on PATH.
type recordingRunner struct {
	name string
	args []string
	env  []string
	out  []byte
	err  error
}

func (r *recordingRunner) run(_ context.Context, name string, args, env []string) ([]byte, error) {
	r.name, r.args, r.env = name, args, env
	return r.out, r.err
}

func TestExecutorFileIssueArgv(t *testing.T) {
	rr := &recordingRunner{out: []byte(`{"number": 42, "html_url": "https://forge/x/y/issues/42"}`)}
	ex := &wardKdlWriteExecutor{token: "tok", run: rr.run}

	res, err := ex.FileIssue(context.Background(), broker.Target{Owner: "coilyco-flight-deck", Repo: "ward"}, "title here", "body here")
	if err != nil {
		t.Fatalf("FileIssue: %v", err)
	}
	if rr.name != wardKdlWriteBin {
		t.Errorf("binary = %q, want %q", rr.name, wardKdlWriteBin)
	}
	want := []string{"ops", "forgejo", "issue", "create", "coilyco-flight-deck", "ward", "--title", "title here", "--body", "body here", "--output", "json"}
	if strings.Join(rr.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %v\nwant\n  %v", rr.args, want)
	}
	if res.Number != 42 || res.URL != "https://forge/x/y/issues/42" {
		t.Errorf("result = %+v, want number 42 + html_url", res)
	}
	// The token rides env, never argv.
	for _, a := range rr.args {
		if strings.Contains(a, "tok") {
			t.Errorf("token leaked into argv: %v", rr.args)
		}
	}
	if !containsEnv(rr.env, credseed.EnvForgejoToken, "tok") {
		t.Errorf("env missing %s=tok: %v", credseed.EnvForgejoToken, rr.env)
	}
}

func TestExecutorEditIssueOmitsEmptyFields(t *testing.T) {
	rr := &recordingRunner{out: []byte(`{"number": 7}`)}
	ex := &wardKdlWriteExecutor{token: "tok", run: rr.run}

	if _, err := ex.EditIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 7}, "", "new body", "closed"); err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	want := []string{"ops", "forgejo", "issue", "edit", "coilyco", "r", "7", "--body", "new body", "--state", "closed", "--output", "json"}
	if strings.Join(rr.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %v\nwant\n  %v", rr.args, want)
	}
}

func TestExecutorCommentIssueArgvAndResultNumber(t *testing.T) {
	// A comment payload carries html_url but no issue number; the result reuses
	// the request's target number.
	rr := &recordingRunner{out: []byte(`{"html_url": "https://forge/x/y/issues/7#issuecomment-1"}`)}
	ex := &wardKdlWriteExecutor{token: "tok", run: rr.run}

	res, err := ex.CommentIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 7}, "hello")
	if err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	want := []string{"ops", "forgejo", "comment", "create", "coilyco", "r", "7", "--body", "hello", "--output", "json"}
	if strings.Join(rr.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n  %v\nwant\n  %v", rr.args, want)
	}
	if res.Number != 7 {
		t.Errorf("result number = %d, want 7 (reused from target)", res.Number)
	}
}

// Unit C: Dispatch vends the root-held token as the child env-file seed; it
// shells nothing (no recordingRunner call) - the seed rides Result.Detail.
func TestExecutorDispatchVendsSeed(t *testing.T) {
	rr := &recordingRunner{}
	ex := &wardKdlWriteExecutor{token: "tok", run: rr.run}
	res, err := ex.Dispatch(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("Dispatch should be served in Unit C: %v", err)
	}
	if res.Detail != "tok" {
		t.Errorf("dispatch seed = %q, want the held token in Result.Detail", res.Detail)
	}
	if rr.args != nil {
		t.Errorf("Dispatch must not shell the write binary; ran %v", rr.args)
	}
}

// With no token held, Dispatch errors rather than vending an empty seed.
func TestExecutorDispatchNoToken(t *testing.T) {
	ex := &wardKdlWriteExecutor{token: "", run: (&recordingRunner{}).run}
	if _, err := ex.Dispatch(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 1}); err == nil {
		t.Fatal("Dispatch with no token should error, not vend an empty seed")
	}
}

func TestParseIssueResultUnparseableIsBestEffort(t *testing.T) {
	res := parseIssueResult([]byte("not json at all"))
	if res.Number != 0 || res.URL != "" {
		t.Errorf("expected zero number/url on unparseable body, got %+v", res)
	}
	if res.Detail == "" {
		t.Error("expected raw body echoed in Detail on unparseable body")
	}
}

func TestWriteTierAuthorizer(t *testing.T) {
	auth := writeTierAuthorizer(brokerOwnerPrefix)
	ctx := context.Background()
	tests := []struct {
		name   string
		req    broker.Request
		permit bool
	}{
		{"file issue ok", broker.Request{Op: broker.OpFileIssue, Target: broker.Target{Owner: "coilyco-flight-deck", Repo: "ward"}, Title: "t"}, true},
		{"edit issue ok", broker.Request{Op: broker.OpEditIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}}, true},
		{"comment ok", broker.Request{Op: broker.OpCommentIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}}, true},
		{"dispatch ok (Unit C, served with a number)", broker.Request{Op: broker.OpDispatch, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}}, true},
		{"dispatch without number rejected", broker.Request{Op: broker.OpDispatch, Target: broker.Target{Owner: "coilyco", Repo: "ward"}}, false},
		{"dispatch non-coily owner rejected", broker.Request{Op: broker.OpDispatch, Target: broker.Target{Owner: "evilcorp", Repo: "ward", Number: 3}}, false},
		{"non-coily owner rejected", broker.Request{Op: broker.OpFileIssue, Target: broker.Target{Owner: "evilcorp", Repo: "ward"}, Title: "t"}, false},
		{"file issue without title rejected", broker.Request{Op: broker.OpFileIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward"}}, false},
		{"edit without number rejected", broker.Request{Op: broker.OpEditIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.Authorize(ctx, tt.req)
			if tt.permit && err != nil {
				t.Errorf("expected permit, got %v", err)
			}
			if !tt.permit && err == nil {
				t.Error("expected refusal, got permit")
			}
		})
	}
}

// TestBrokerServerRoundTrip runs the full path over a real unix socket: a write op
// reaches the executor, a numbered dispatch is served, a number-less one refused.
func TestBrokerServerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "broker.sock")
	ln, err := newBrokerListener(sock, os.Getgid())
	if err != nil {
		t.Fatalf("newBrokerListener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Socket must be group-readable (0660), not world.
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != brokerSocketMode {
		t.Errorf("socket perm = %#o, want %#o", perm, brokerSocketMode)
	}

	fake := &fakeExecutor{result: broker.Result{Number: 99, URL: "https://forge/i/99"}}
	srv, err := broker.NewServer(ln, fake, writeTierAuthorizer(brokerOwnerPrefix))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	client := broker.NewClient(sock)

	// Accepted write op reaches the executor and returns its result.
	resp, err := client.FileIssue(ctx, broker.Target{Owner: "coilyco-flight-deck", Repo: "ward"}, "hi", "body")
	if err != nil {
		t.Fatalf("client.FileIssue transport: %v", err)
	}
	if !resp.OK || resp.Result.Number != 99 {
		t.Errorf("file issue resp = %+v, want OK + number 99", resp)
	}
	if !fake.fileCalled {
		t.Error("executor.FileIssue was not invoked")
	}

	// A numbered dispatch is served in Unit C: it reaches the executor and returns.
	dresp, err := client.Dispatch(ctx, broker.Target{Owner: "coilyco", Repo: "ward", Number: 1})
	if err != nil {
		t.Fatalf("client.Dispatch transport: %v", err)
	}
	if !dresp.OK {
		t.Errorf("numbered dispatch should be served, got %+v", dresp)
	}
	if !fake.dispatchCalled {
		t.Error("numbered dispatch should reach the executor")
	}

	// A number-less dispatch is still refused at authz, before the executor.
	fake.dispatchCalled = false
	nresp, err := client.Dispatch(ctx, broker.Target{Owner: "coilyco", Repo: "ward"})
	if err != nil {
		t.Fatalf("client.Dispatch (no number) transport: %v", err)
	}
	if nresp.OK || nresp.Error == "" {
		t.Errorf("number-less dispatch should be refused, got %+v", nresp)
	}
	if fake.dispatchCalled {
		t.Error("number-less dispatch reached the executor; it should be refused at authz")
	}
}

// fakeExecutor records which methods were called and returns a canned result,
// standing in for the real ward-kdl-write shell-out in the socket round-trip.
type fakeExecutor struct {
	result         broker.Result
	fileCalled     bool
	dispatchCalled bool
}

func (f *fakeExecutor) FileIssue(_ context.Context, _ broker.Target, _, _ string) (broker.Result, error) {
	f.fileCalled = true
	return f.result, nil
}

func (f *fakeExecutor) EditIssue(_ context.Context, _ broker.Target, _, _, _ string) (broker.Result, error) {
	return f.result, nil
}

func (f *fakeExecutor) CommentIssue(_ context.Context, _ broker.Target, _ string) (broker.Result, error) {
	return f.result, nil
}

func (f *fakeExecutor) Dispatch(_ context.Context, _ broker.Target) (broker.Result, error) {
	f.dispatchCalled = true
	return f.result, nil
}

// containsEnv reports whether env carries key=value.
func containsEnv(env []string, key, value string) bool {
	for _, e := range env {
		if e == key+"="+value {
			return true
		}
	}
	return false
}
