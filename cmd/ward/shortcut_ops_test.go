package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestShortcutClientLifecycle(t *testing.T) {
	t.Setenv("SHORTCUT_LABELS", "bug, triage")
	t.Setenv("SHORTCUT_EPIC_ID", "123")
	var putCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Shortcut-Token"); got != "secret" {
			t.Fatalf("token header = %q, want secret", got)
		}
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v3/stories/7":
			_, _ = io.WriteString(w, `{"id":7,"name":"ship it","description":"body","app_url":"https://app.shortcut.com/acme/story/7/ship-it","workflow_id":99,"workflow_state_id":10,"completed":false,"labels":[{"name":"bug"}]}`)
		case http.MethodGet + " /api/v3/stories/7/comments":
			_, _ = io.WriteString(w, `[{"id":1,"text":"note","created_at":"2026-07-09T00:00:00Z","author_id":"member-1"}]`)
		case http.MethodGet + " /api/v3/workflows":
			_, _ = io.WriteString(w, `[{"id":99,"default_state_id":10,"states":[{"id":10,"name":"Unstarted","type":"unstarted"},{"id":20,"name":"Done","type":"done"}]}]`)
		case http.MethodPost + " /api/v3/stories":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body["name"] != "new story" {
				t.Fatalf("create name = %v, want new story", body["name"])
			}
			if body["workflow_state_id"] != float64(10) {
				t.Fatalf("create workflow_state_id = %v, want 10", body["workflow_state_id"])
			}
			if !strings.Contains(body["description"].(string), agentSignatureMarker) {
				t.Fatalf("create body lost signature marker: %v", body["description"])
			}
			if body["epic_id"] != float64(123) {
				t.Fatalf("create epic_id = %v, want 123", body["epic_id"])
			}
			labels, ok := body["labels"].([]any)
			if !ok || len(labels) != 2 {
				t.Fatalf("create labels = %#v, want two label objects", body["labels"])
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":8}`)
		case http.MethodPost + " /api/v3/stories/7/comments":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode comment body: %v", err)
			}
			if !strings.Contains(body["text"].(string), agentSignatureMarker) {
				t.Fatalf("comment body lost signature marker: %v", body["text"])
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut + " /api/v3/stories/7":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			want := 20
			if atomic.AddInt32(&putCount, 1) == 2 {
				want = 10
			}
			if body["workflow_state_id"] != float64(want) {
				t.Fatalf("update workflow_state_id = %v, want %d", body["workflow_state_id"], want)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"id":7}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &shortcutClient{mode: modeClaude, baseURL: srv.URL + "/api/v3", token: "secret"}
	issue, err := c.GetIssue(context.Background(), "acme", "ward", 7)
	if err != nil {
		t.Fatalf("getIssue: %v", err)
	}
	if issue.Title != "ship it" || issue.Body != "body" || issue.State != "open" || issue.URL != "https://app.shortcut.com/acme/story/7/ship-it" {
		t.Fatalf("issue = %+v", issue)
	}
	if got := strings.Join(issue.Labels, ","); got != "bug" {
		t.Fatalf("labels = %q, want bug", got)
	}

	comments, err := c.ListIssueComments(context.Background(), "acme", "ward", 7)
	if err != nil {
		t.Fatalf("listIssueComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "note" || comments[0].User.Login != "member-1" || !comments[0].CreatedAt.Equal(time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("comments = %+v", comments)
	}

	if num, err := c.CreateIssue(context.Background(), "acme", "ward", "new story", "body"); err != nil || num != 8 {
		t.Fatalf("createIssue = %d,%v want 8,nil", num, err)
	}
	if err := c.CommentIssue(context.Background(), "acme", "ward", 7, "note"); err != nil {
		t.Fatalf("commentIssue: %v", err)
	}
	if err := c.CloseIssue(context.Background(), "acme", "ward", 7); err != nil {
		t.Fatalf("closeIssue: %v", err)
	}
	if err := c.ReopenIssue(context.Background(), "acme", "ward", 7); err != nil {
		t.Fatalf("reopenIssue: %v", err)
	}
}

var _ Tracker = (*shortcutClient)(nil)
