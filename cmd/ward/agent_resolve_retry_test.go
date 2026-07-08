package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

// TestTransientResolveErr pins ward#497's classifier: a pinned 4xx is permanent (no
// retry), everything else (5xx, unreachable, a bare exit-status-3) biases to a retry.
func TestTransientResolveErr(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wantRet bool
	}{
		{name: "nil is not transient", err: nil, wantRet: false},
		{
			name:    "forgejo 404 envelope is permanent",
			err:     errors.New("forgejo: get issue coilysiren/website#57: exit status 3: GET https://forgejo.coilysiren.me/... -> 404 Not Found: {\"message\":\"issue does not exist\"}"),
			wantRet: false,
		},
		{
			name:    "forgejo 403 envelope is permanent",
			err:     errors.New("forgejo: get issue coilyco-bridge/lore#2: exit status 3: GET https://... -> 403 Forbidden: {}"),
			wantRet: false,
		},
		{
			name:    "gh 404 is permanent",
			err:     errors.New("github: get issue owner/repo#9: exit status 1: HTTP 404: Not Found"),
			wantRet: false,
		},
		{
			name:    "forgejo 502 envelope retries",
			err:     errors.New("forgejo: get issue owner/repo#1: exit status 3: GET https://... -> 502 Bad Gateway: upstream"),
			wantRet: true,
		},
		{
			name:    "forgejo 503 envelope retries",
			err:     errors.New("forgejo: get issue owner/repo#1: exit status 3: get https://... -> 503 service unavailable"),
			wantRet: true,
		},
		{
			name:    "unreachable api retries",
			err:     errors.New("forgejo: get issue owner/repo#1: exit status 3: the API was unreachable: dial tcp: connection refused"),
			wantRet: true,
		},
		{
			name:    "bare exit status 3 with no cause retries",
			err:     errors.New("forgejo: get issue owner/repo#1: exit status 3"),
			wantRet: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := transientResolveErr(tc.err); got != tc.wantRet {
				t.Fatalf("transientResolveErr(%v) = %v, want %v", tc.err, got, tc.wantRet)
			}
		})
	}
}

// TestResolveIssueWithRetry covers ward#497's bounded retry: a clean first read, a
// ride over a transient blip, a fast fail on a permanent 4xx, and a give-up.
func TestResolveIssueWithRetry(t *testing.T) {
	noSleep := func(time.Duration) {}
	ok := &dispatch.Issue{Title: "ready"}

	t.Run("first read wins", func(t *testing.T) {
		calls := 0
		got, err := resolveIssueWithRetry("t", "o/r#1", noSleep, func() (*dispatch.Issue, error) {
			calls++
			return ok, nil
		})
		if err != nil || got != ok || calls != 1 {
			t.Fatalf("first-read: calls=%d got=%v err=%v", calls, got, err)
		}
	})

	t.Run("transient blip then success", func(t *testing.T) {
		calls, sleeps := 0, 0
		got, err := resolveIssueWithRetry("t", "o/r#1", func(time.Duration) { sleeps++ }, func() (*dispatch.Issue, error) {
			calls++
			if calls < 3 {
				return nil, errors.New("exit status 3: -> 500 Internal Server Error")
			}
			return ok, nil
		})
		if err != nil || got != ok || calls != 3 || sleeps != 2 {
			t.Fatalf("transient-then-ok: calls=%d sleeps=%d got=%v err=%v", calls, sleeps, got, err)
		}
	})

	t.Run("permanent 4xx fails on the first try", func(t *testing.T) {
		calls := 0
		perm := errors.New("exit status 3: -> 403 Forbidden")
		_, err := resolveIssueWithRetry("t", "o/r#1", func(time.Duration) { t.Fatal("must not sleep on a permanent failure") }, func() (*dispatch.Issue, error) {
			calls++
			return nil, perm
		})
		if !errors.Is(err, perm) || calls != 1 {
			t.Fatalf("permanent: calls=%d err=%v", calls, err)
		}
	})

	t.Run("exhausts on a persistent transient failure", func(t *testing.T) {
		calls, sleeps := 0, 0
		blip := errors.New("exit status 3: the API was unreachable")
		_, err := resolveIssueWithRetry("t", "o/r#1", func(time.Duration) { sleeps++ }, func() (*dispatch.Issue, error) {
			calls++
			return nil, blip
		})
		if !errors.Is(err, blip) || calls != resolveRetryAttempts || sleeps != resolveRetryAttempts-1 {
			t.Fatalf("exhausts: calls=%d sleeps=%d err=%v", calls, sleeps, err)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%d transient attempt", resolveRetryAttempts)) {
			t.Fatalf("exhausts: error must name the attempt count, got %v", err)
		}
	})
}
