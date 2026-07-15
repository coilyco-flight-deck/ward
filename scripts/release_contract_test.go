package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"net/http"
	"net/http/httptest"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate the test file")
	}
	return filepath.Dir(filepath.Dir(self))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestReleasePipelineUsesDraftArtifacts(t *testing.T) {
	promote := readRepoFile(t, ".forgejo/workflows/promote.yml")
	release := readRepoFile(t, ".forgejo/workflows/release.yml")
	docs := readRepoFile(t, "docs/release.md")
	binaries := readRepoFile(t, "docs/release-binaries.md")

	for _, want := range []string{
		"draft-${{ github.sha }}",
		"tag-bump@main",
		"main.Version=${TAG}",
		"scripts/publish-draft-release.sh",
	} {
		if !strings.Contains(promote, want) {
			t.Fatalf("promote workflow should mention %q:\n%s", want, promote)
		}
	}

	for _, want := range []string{
		"draft-${{ github.sha }}",
		"promote-draft-assets",
		"fetched ${name} from ${DRAFT_TAG}",
		"scripts/forgejo-release-asset.sh",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release workflow should mention %q:\n%s", want, release)
		}
	}

	for _, ban := range []string{"go build -trimpath", "go mod download", "make sync-defaults-assets"} {
		if strings.Contains(release, ban) {
			t.Fatalf("release workflow should not rebuild on release; found %q", ban)
		}
	}

	for _, want := range []string{
		"builds the binary",
		"matrix once",
		"retags the already-built",
		"draft assets",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("release docs should mention %q:\n%s", want, docs)
		}
	}

	for _, want := range []string{
		"does not publish moving `release` or `latest` aliases",
		"tagged release assets directly",
	} {
		if !strings.Contains(binaries, want) {
			t.Fatalf("release binaries docs should mention %q:\n%s", want, binaries)
		}
	}
}

func TestPublishDraftReleaseHandles404AndIdempotentAssetRewrites(t *testing.T) {
	srv := newDraftReleaseTestServer(t)
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte("alpha  ward-linux-amd64\n"), 0o600); err != nil {
		t.Fatalf("write SHA256SUMS: %v", err)
	}

	script := filepath.Join(repoRoot(t), "scripts", "publish-draft-release.sh")
	run := func() string {
		cmd := exec.Command("bash", script)
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(),
			"DRAFT_TAG=draft-testsha",
			"FORGEJO_API="+srv.URL+"/api/v1/repos/coilyco-flight-deck/ward",
			"TOKEN=secret",
			"DIST_DIR="+dist,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("publish-draft-release.sh failed: %v\noutput: %s", err, out)
		}
		return string(out)
	}

	first := run()
	if !strings.Contains(first, "uploaded SHA256SUMS to draft draft-testsha") || !strings.Contains(first, "published draft release draft-testsha") {
		t.Fatalf("first publish should create and upload assets, got:\n%s", first)
	}
	if got := srv.count("GET /api/v1/repos/coilyco-flight-deck/ward/releases/tags/draft-testsha"); got != 1 {
		t.Fatalf("first publish GET count = %d, want 1", got)
	}
	if got := srv.count("POST /api/v1/repos/coilyco-flight-deck/ward/releases"); got != 1 {
		t.Fatalf("first publish create count = %d, want 1", got)
	}
	if got := srv.count("POST /api/v1/repos/coilyco-flight-deck/ward/releases/99/assets?name=SHA256SUMS"); got != 1 {
		t.Fatalf("first publish SHA256SUMS upload count = %d, want 1", got)
	}

	second := run()
	if !strings.Contains(second, "uploaded SHA256SUMS to draft draft-testsha") || !strings.Contains(second, "published draft release draft-testsha") {
		t.Fatalf("second publish should reuse the release, got:\n%s", second)
	}
	if got := srv.count("GET /api/v1/repos/coilyco-flight-deck/ward/releases/tags/draft-testsha"); got != 2 {
		t.Fatalf("second publish GET count = %d, want 2", got)
	}
	if got := srv.count("POST /api/v1/repos/coilyco-flight-deck/ward/releases"); got != 1 {
		t.Fatalf("second publish should not recreate the draft, create count = %d", got)
	}
	if got := srv.count("DELETE /api/v1/repos/coilyco-flight-deck/ward/releases/99/assets/1"); got != 1 {
		t.Fatalf("second publish should replace the existing asset once, delete count = %d", got)
	}
}

func TestForgejoReleaseAssetHelperReadsRawAssetBody(t *testing.T) {
	srv := newReleaseAssetTestServer(t)
	script := filepath.Join(repoRoot(t), "scripts", "forgejo-release-asset.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"RELEASE_TAG=v9.9.9",
		"ASSET_NAME=ward-windows-amd64.exe.sha256",
		"FORGEJO_API="+srv.URL+"/api/v1/repos/coilyco-flight-deck/ward",
		"TOKEN=secret",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forgejo-release-asset.sh failed: %v\noutput: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != strings.Repeat("a", 64) {
		t.Fatalf("asset body = %q, want raw 64-hex digest", got)
	}
	if got := srv.count("GET /api/v1/repos/coilyco-flight-deck/ward/releases/tags/v9.9.9"); got != 1 {
		t.Fatalf("release lookup count = %d, want 1", got)
	}
	if got := srv.count("GET /api/v1/repos/coilyco-flight-deck/ward/releases/99/assets?per_page=100"); got != 1 {
		t.Fatalf("asset list count = %d, want 1", got)
	}
	if got := srv.count("GET /api/v1/repos/coilyco-flight-deck/ward/releases/99/assets/7"); got != 1 {
		t.Fatalf("asset body fetch count = %d, want 1", got)
	}
	if got := srv.count("GET /api/v1/repos/coilyco-flight-deck/ward/releases/download/v9.9.9/ward-windows-amd64.exe.sha256"); got != 0 {
		t.Fatalf("direct download path should stay unused, count = %d", got)
	}
}

type draftReleaseTestServer struct {
	*httptest.Server
	mu       sync.Mutex
	counts   map[string]int
	released bool
	assets   map[string]int
}

func newDraftReleaseTestServer(t *testing.T) *draftReleaseTestServer {
	t.Helper()
	s := &draftReleaseTestServer{
		counts: make(map[string]int),
		assets: make(map[string]int),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/tags/draft-testsha", s.handleDraftReleaseByTag)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases", s.handleDraftReleases)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/99/assets", s.handleDraftReleaseAssets)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/99/assets/", s.handleDraftReleaseAssetDelete)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *draftReleaseTestServer) count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[key]
}

func (s *draftReleaseTestServer) record(r *http.Request) {
	key := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	s.mu.Lock()
	s.counts[key]++
	s.mu.Unlock()
}

func (s *draftReleaseTestServer) handleDraftReleaseByTag(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	s.mu.Lock()
	released := s.released
	s.mu.Unlock()
	if !released {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(`{"id":99,"tag_name":"draft-testsha","draft":true}`))
}

func (s *draftReleaseTestServer) handleDraftReleases(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	s.released = true
	s.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"id":99,"tag_name":"draft-testsha","draft":true}`))
}

func (s *draftReleaseTestServer) handleDraftReleaseAssets(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		assets := make([]struct {
			id   int
			name string
		}, 0, len(s.assets))
		for name, id := range s.assets {
			assets = append(assets, struct {
				id   int
				name string
			}{id: id, name: name})
		}
		s.mu.Unlock()
		if len(assets) == 0 {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		var b strings.Builder
		b.WriteString("[")
		for i, asset := range assets {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":`)
			b.WriteString(strconv.Itoa(asset.id))
			b.WriteString(`,"name":"`)
			b.WriteString(asset.name)
			b.WriteString(`"}`)
		}
		b.WriteString("]")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.String()))
	case http.MethodPost:
		name := r.URL.Query().Get("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		if _, ok := s.assets[name]; !ok {
			s.assets[name] = 1
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"` + name + `"}`))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *draftReleaseTestServer) handleDraftReleaseAssetDelete(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/1") {
		delete(s.assets, "SHA256SUMS")
	}
	w.WriteHeader(http.StatusNoContent)
}

type releaseAssetTestServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func newReleaseAssetTestServer(t *testing.T) *releaseAssetTestServer {
	t.Helper()
	s := &releaseAssetTestServer{
		counts: make(map[string]int),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/tags/v9.9.9", s.handleReleaseByTag)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/99/assets", s.handleReleaseAssets)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/99/assets/7", s.handleReleaseAssetBody)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/download/v9.9.9/ward-windows-amd64.exe.sha256", s.handleReleaseDownloadMetadata)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *releaseAssetTestServer) count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[key]
}

func (s *releaseAssetTestServer) record(r *http.Request) {
	key := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	s.mu.Lock()
	s.counts[key]++
	s.mu.Unlock()
}

func (s *releaseAssetTestServer) handleReleaseByTag(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(`{"id":99,"tag_name":"v9.9.9","draft":false}`))
}

func (s *releaseAssetTestServer) handleReleaseAssets(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(`[
		{"id":7,"name":"ward-windows-amd64.exe.sha256"},
		{"id":8,"name":"ward-windows-arm64.exe.sha256"}
	]`))
}

func (s *releaseAssetTestServer) handleReleaseAssetBody(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(strings.Repeat("a", 64)))
}

func (s *releaseAssetTestServer) handleReleaseDownloadMetadata(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(`{"id":7,"name":"ward-windows-amd64.exe.sha256","browser_download_url":"https://forgejo.example/attachments/7","type":"attachment"}`))
}
