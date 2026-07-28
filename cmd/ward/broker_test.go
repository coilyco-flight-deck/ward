package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

func shortBrokerSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ward-broker-")
	if err != nil {
		t.Fatalf("broker temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "broker.sock")
}

func TestExecutorFileIssueHTTP(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 42, "html_url": "https://forge/x/y/issues/42"}`))
	}))
	defer srv.Close()

	ex := &wardKdlWriteExecutor{token: "tok", baseURL: srv.URL}
	res, err := ex.FileIssue(context.Background(), broker.Target{Owner: "coilyco-flight-deck", Repo: "ward"}, "title here", "body here")
	if err != nil {
		t.Fatalf("FileIssue: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/coilyco-flight-deck/ward/issues" {
		t.Errorf("request = %s %s, want POST issue-create endpoint", gotMethod, gotPath)
	}
	if gotAuth != "token tok" {
		t.Errorf("auth = %q, want token tok", gotAuth)
	}
	if gotBody["title"] != "title here" || gotBody["body"] != "body here" {
		t.Errorf("body = %#v, want title/body", gotBody)
	}
	if res.Number != 42 || res.URL != "https://forge/x/y/issues/42" {
		t.Errorf("result = %+v, want number 42 + html_url", res)
	}
}

func TestExecutorRefreshesRootCredentialAfterForgejo401(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "token token-a" {
			http.Error(w, "access token does not exist", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 42}`))
	}))
	defer srv.Close()

	refreshes := 0
	ex := newWardKdlWriteExecutor("token-a", func(context.Context) (string, error) {
		refreshes++
		return "token-b", nil
	})
	ex.baseURL = srv.URL
	if _, err := ex.FileIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "ward"}, "title", "body"); err != nil {
		t.Fatalf("FileIssue after refresh: %v", err)
	}
	if refreshes != 1 {
		t.Errorf("credential refreshes = %d, want 1", refreshes)
	}
	if got, want := gotAuth, []string{"token token-a", "token token-b"}; len(got) != len(want) {
		t.Errorf("auth attempts = %q, want %q", got, want)
	} else if got[0] != want[0] || got[1] != want[1] {
		t.Errorf("auth attempts = %q, want %q", got, want)
	}
}

func TestExecutorDispatchRefreshesCurrentRootCredential(t *testing.T) {
	ex := newWardKdlWriteExecutor("token-a", func(context.Context) (string, error) { return "token-b", nil })
	res, err := ex.Dispatch(context.Background(), broker.Target{Owner: "coilyco", Repo: "ward", Number: 1})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.Detail != "token-b" {
		t.Errorf("dispatch seed = %q, want refreshed token", res.Detail)
	}
}

func TestExecutorEditIssueOmitsEmptyFields(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco/r/issues/7":
			_, _ = w.Write([]byte(`{"number":7,"pull_request":null}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/coilyco/r/issues/7":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			_, _ = w.Write([]byte(`{"number": 7}`))
		default:
			t.Fatalf("request = %s %s, want GET+PATCH issue endpoint", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	ex := &wardKdlWriteExecutor{token: "tok", baseURL: srv.URL}
	if _, err := ex.EditIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 7}, "", "new body", "closed"); err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	if _, ok := gotBody["title"]; ok {
		t.Errorf("empty title should be omitted, body = %#v", gotBody)
	}
	if gotBody["body"] != "new body" || gotBody["state"] != "closed" {
		t.Errorf("body = %#v, want body + state only", gotBody)
	}
}

func TestExecutorEditIssueRoutesPullRequestsToPullEndpoint(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/coilyco/r/issues/7":
			_, _ = w.Write([]byte(`{"number":7,"pull_request":{"url":"https://forge/x/y/pulls/7"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/coilyco/r/pulls/7":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			_, _ = w.Write([]byte(`{"number": 7}`))
		default:
			t.Fatalf("request = %s %s, want GET issue + PATCH pull endpoint", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	ex := &wardKdlWriteExecutor{token: "tok", baseURL: srv.URL}
	if _, err := ex.EditIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 7}, "new title", "new body", "open"); err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	if gotBody["title"] != "new title" || gotBody["body"] != "new body" || gotBody["state"] != "open" {
		t.Errorf("body = %#v, want title/body/state on pull endpoint", gotBody)
	}
}

func TestExecutorCommentIssueHTTPAndResultNumber(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/repos/coilyco/r/issues/7/comments" {
			t.Fatalf("request = %s %s, want POST comment endpoint", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"html_url": "https://forge/x/y/issues/7#issuecomment-1"}`))
	}))
	defer srv.Close()

	ex := &wardKdlWriteExecutor{token: "tok", baseURL: srv.URL}
	res, err := ex.CommentIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 7}, "hello")
	if err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	if gotBody["body"] != "hello" {
		t.Errorf("body = %#v, want comment body", gotBody)
	}
	if res.Number != 7 {
		t.Errorf("result number = %d, want 7 (reused from target)", res.Number)
	}
	if res.URL != "https://forge/x/y/issues/7#issuecomment-1" {
		t.Errorf("result URL = %q, want the nested comment html_url", res.URL)
	}
}

func TestExecutorLabelIssueHTTP(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		labels     []string
		wantMethod []string
		wantPath   []string
	}{
		{"add", broker.LabelAdd, []string{"headless", "P1"}, []string{http.MethodPost}, []string{"/api/v1/repos/coilyco/r/issues/7/labels"}},
		{"set", broker.LabelSet, []string{"P2"}, []string{http.MethodPut}, []string{"/api/v1/repos/coilyco/r/issues/7/labels"}},
		{"remove", broker.LabelRemove, []string{"headless", "P1"}, []string{http.MethodDelete, http.MethodDelete}, []string{"/api/v1/repos/coilyco/r/issues/7/labels/headless", "/api/v1/repos/coilyco/r/issues/7/labels/P1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethods, gotPaths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethods = append(gotMethods, r.Method)
				gotPaths = append(gotPaths, r.URL.Path)
				if r.Method == http.MethodPost || r.Method == http.MethodPut {
					var body map[string][]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("decode request body: %v", err)
					}
					if len(body["labels"]) != len(tt.labels) {
						t.Fatalf("labels body = %#v, want %v", body, tt.labels)
					}
				}
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = w.Write([]byte(`[{"name":"headless"}]`))
			}))
			defer srv.Close()

			ex := &wardKdlWriteExecutor{token: "tok", baseURL: srv.URL}
			res, err := ex.LabelIssue(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 7}, tt.mode, tt.labels)
			if err != nil {
				t.Fatalf("LabelIssue: %v", err)
			}
			if res.Number != 7 {
				t.Errorf("result number = %d, want 7 (reused from target)", res.Number)
			}
			if len(gotMethods) != len(tt.wantMethod) {
				t.Fatalf("requests = %v %v, want %v %v", gotMethods, gotPaths, tt.wantMethod, tt.wantPath)
			}
			for i := range tt.wantMethod {
				if gotMethods[i] != tt.wantMethod[i] || gotPaths[i] != tt.wantPath[i] {
					t.Errorf("request %d = %s %s, want %s %s", i, gotMethods[i], gotPaths[i], tt.wantMethod[i], tt.wantPath[i])
				}
			}
		})
	}
}

// Unit C: Dispatch vends the root-held token as the child env-file seed; it
// shells nothing - the seed rides Result.Detail.
func TestExecutorDispatchVendsSeed(t *testing.T) {
	ex := &wardKdlWriteExecutor{token: "tok"}
	res, err := ex.Dispatch(context.Background(), broker.Target{Owner: "coilyco", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("Dispatch should be served in Unit C: %v", err)
	}
	if res.Detail != "tok" {
		t.Errorf("dispatch seed = %q, want the held token in Result.Detail", res.Detail)
	}
}

// With no token held, Dispatch errors rather than vending an empty seed.
func TestExecutorDispatchNoToken(t *testing.T) {
	ex := &wardKdlWriteExecutor{token: ""}
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
	auth := (&Runner{}).writeTierAuthorizer()
	ctx := context.Background()
	tests := []struct {
		name   string
		req    broker.Request
		permit bool
	}{
		{"file issue ok", broker.Request{Op: broker.OpFileIssue, Target: broker.Target{Owner: "coilyco-flight-deck", Repo: "ward"}, Title: "t"}, true},
		{"edit issue ok", broker.Request{Op: broker.OpEditIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}}, true},
		{"comment ok", broker.Request{Op: broker.OpCommentIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}}, true},
		{"label add ok", broker.Request{Op: broker.OpLabelIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}, LabelMode: broker.LabelAdd, Labels: []string{"headless"}}, true},
		{"label set ok", broker.Request{Op: broker.OpLabelIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}, LabelMode: broker.LabelSet, Labels: []string{"P1"}}, true},
		{"label unknown mode rejected", broker.Request{Op: broker.OpLabelIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}, LabelMode: "toggle", Labels: []string{"headless"}}, false},
		{"label empty set rejected", broker.Request{Op: broker.OpLabelIssue, Target: broker.Target{Owner: "coilyco", Repo: "ward", Number: 3}, LabelMode: broker.LabelAdd}, false},
		{"label non-coily owner rejected", broker.Request{Op: broker.OpLabelIssue, Target: broker.Target{Owner: "evilcorp", Repo: "ward", Number: 3}, LabelMode: broker.LabelAdd, Labels: []string{"headless"}}, false},
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
	sock := shortBrokerSocket(t)
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
	if perm := fi.Mode().Perm(); runtime.GOOS != "windows" && perm != brokerSocketMode {
		t.Errorf("socket perm = %#o, want %#o", perm, brokerSocketMode)
	}

	fake := &fakeExecutor{result: broker.Result{Number: 99, URL: "https://forge/i/99"}}
	srv, err := broker.NewServer(ln, fake, (&Runner{}).writeTierAuthorizer())
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
// standing in for the real direct executor in the socket round-trip.
type fakeExecutor struct {
	result         broker.Result
	fileCalled     bool
	dispatchCalled bool
	labelCalled    bool
	labelMode      string
	labels         []string
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

func (f *fakeExecutor) LabelIssue(_ context.Context, _ broker.Target, mode string, labels []string) (broker.Result, error) {
	f.labelCalled = true
	f.labelMode = mode
	f.labels = labels
	return f.result, nil
}

func (f *fakeExecutor) Dispatch(_ context.Context, _ broker.Target) (broker.Result, error) {
	f.dispatchCalled = true
	return f.result, nil
}
