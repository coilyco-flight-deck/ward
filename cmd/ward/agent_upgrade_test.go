package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fetchLatestWardTag stays quiet rather than panicking on a nil receiver.
func TestPolicyBoundaryFetchLatestWardTagQuietWithoutRunner(t *testing.T) {
	var r *Runner
	if _, ok := r.fetchLatestWardTag(context.Background()); ok {
		t.Error("fetchLatestWardTag on a nil receiver should report ok=false")
	}
}

func TestPolicyBoundaryFetchLatestWardTagUsesNativeReleaseAPI(t *testing.T) {
	original := forgejoBaseURL
	t.Cleanup(func() { forgejoBaseURL = original })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/repos/"+wardBootstrapRepo+"/releases" {
			t.Fatalf("release path = %q", req.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.9.0-rc1","draft":false,"prerelease":true},
			{"tag_name":"v0.8.0","draft":false,"prerelease":false}
		]`))
	}))
	defer server.Close()
	forgejoBaseURL = server.URL

	tag, ok := (&Runner{}).fetchLatestWardTag(context.Background())
	if !ok || tag != "v0.8.0" {
		t.Fatalf("fetchLatestWardTag = %q, %v, want v0.8.0, true", tag, ok)
	}
}

func TestPolicyBoundaryWardOutdatedNotice(t *testing.T) {
	got := wardOutdatedNotice("v0.5.1", "v0.5.2")
	for _, want := range []string{"v0.5.1", "v0.5.2", "refresh it", "behind"} {
		if !strings.Contains(got, want) {
			t.Errorf("wardOutdatedNotice missing %q; got:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("wardOutdatedNotice should end in a newline; got %q", got)
	}
}
