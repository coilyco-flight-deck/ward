package main

import (
	"context"
	"strings"
	"testing"
)

// fetchLatestWardTag stays quiet (ok=false) rather than panicking when it has no
// runner to shell the specverb through - best-effort, never blocks dispatch.
func TestFetchLatestWardTagQuietWithoutRunner(t *testing.T) {
	if _, ok := (&Runner{}).fetchLatestWardTag(context.Background()); ok {
		t.Error("fetchLatestWardTag with a nil shell runner should report ok=false")
	}
	var r *Runner
	if _, ok := r.fetchLatestWardTag(context.Background()); ok {
		t.Error("fetchLatestWardTag on a nil receiver should report ok=false")
	}
}

func TestWardOutdatedNotice(t *testing.T) {
	got := wardOutdatedNotice("v0.5.1", "v0.5.2")
	for _, want := range []string{"v0.5.1", "v0.5.2", "ward upgrade", "behind"} {
		if !strings.Contains(got, want) {
			t.Errorf("wardOutdatedNotice missing %q; got:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("wardOutdatedNotice should end in a newline; got %q", got)
	}
}
