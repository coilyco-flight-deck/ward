package scripts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
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
		"scripts/release-tag.sh",
		"TAG_PUSH_TOKEN",
		"main.Version=${TAG}",
		"scripts/publish-draft-release.sh",
		"packages: write",
		"SOURCE_TOKEN: ${{ github.token }}",
		"TARGET_USER: coilyco-ops",
		"TARGET_TOKEN: ${{ secrets.REGISTRY_TOKEN }}",
	} {
		if !strings.Contains(promote, want) {
			t.Fatalf("promote workflow should mention %q:\n%s", want, promote)
		}
	}
	for _, want := range []string{
		"scripts/registry-copy-tag.sh",
		"for f in ward-*",
	} {
		if !strings.Contains(promote, want) {
			t.Fatalf("promote workflow should mention %q:\n%s", want, promote)
		}
	}
	for _, ban := range []string{
		"docker login forgejo.coilysiren.me",
		"docker pull \"$source_image\"",
		"docker tag \"$source_image\" \"$target_image\"",
		"docker push \"$target_image\"",
		"docker manifest inspect \"$target_image\" >/dev/null",
	} {
		if strings.Contains(promote, ban) {
			t.Fatalf("promote workflow should not rely on daemon-bound alias publish %q:\n%s", ban, promote)
		}
	}

	for _, want := range []string{
		"draft-${{ github.sha }}",
		"promote-draft-assets",
		"fetched ${name} from ${DRAFT_TAG}",
		"scripts/forgejo-release-asset.sh",
		"scripts/release-tag.sh",
		"TAG_PUSH_TOKEN",
		"scripts/publish-draft-release.sh",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release workflow should mention %q:\n%s", want, release)
		}
	}
	for _, ban := range []string{
		"coilysiren/agentic-os/actions/tag-bump@main",
		"coilysiren/agentic-os/actions/create-release@main",
	} {
		if strings.Contains(promote, ban) || strings.Contains(release, ban) {
			t.Fatalf("release workflows should not use external action %q", ban)
		}
	}
	for _, want := range []string{
		"scripts/release-tag.sh",
		"scripts/publish-draft-release.sh",
		"docker manifest inspect \"$image\" >/dev/null 2>&1",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release workflow should mention %q:\n%s", want, release)
		}
	}
	scoopJob := workflowJobSection(t, release, "bump-scoop-manifest:", "Alert Telegram on main failure")
	for _, want := range []string{
		"actions/checkout@v6",
		"scripts/forgejo-release-asset.sh",
		"ward-linux-amd64#/ward-linux-amd64",
		"ward-linux-arm64#/ward-linux-arm64",
		"LINUX_AMD64_HASH",
		"LINUX_ARM64_HASH",
	} {
		if !strings.Contains(scoopJob, want) {
			t.Fatalf("bump-scoop-manifest job should mention %q:\n%s", want, scoopJob)
		}
	}
	for _, want := range []string{
		"scripts/forgejo-asset-url.sh",
		"SHA256SUMS",
		"DARWIN_ARM64_URL",
		"LINUX_ARM64_URL",
		"resource(\\\"ward-linux\\\").stage do",
		"libexec.install sidecar",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("Homebrew formula generator should mention %q:\n%s", want, release)
		}
	}
	// Forgejo ignores Accept on the download route (ward#1493): formulas must
	// carry resolved attachment URLs, never header-dependent download-route
	// URLs. Any header stanza reappearing means the stub regression is back.
	if got := strings.Count(release, "headers: [\\\"Accept: application/octet-stream\\\"]"); got != 0 {
		t.Fatalf("Homebrew formula generator must not emit Accept-header URL stanzas (ward#1493), got %d:\n%s", got, release)
	}
	if strings.Contains(release, "ARTIFACT_ROOT}/ward-darwin") {
		t.Fatalf("Homebrew formula generator must not emit download-route URLs (ward#1493):\n%s", release)
	}

	for _, ban := range []string{"go build -trimpath", "go mod download", "make sync-defaults-assets"} {
		if strings.Contains(release, ban) {
			t.Fatalf("release workflow should not rebuild on release; found %q", ban)
		}
	}

	for _, want := range []string{
		"::warning::SCOOP_WRITE_TOKEN not set; skipping scoop manifest bump",
		"::warning::push of ${MANIFEST_PATH} to ${BUCKET_REPO} failed; skipping scoop manifest bump",
		"::warning::bucket ${BUCKET_REPO} main does not serve ${VER} after push; skipping scoop manifest bump follow-up",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release workflow should soft-fail scoop updates with %q:\n%s", want, release)
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

	if !strings.Contains(docs, "the Scoop bucket bump is best-effort") {
		t.Fatalf("release docs should mention best-effort Scoop policy:\n%s", docs)
	}

	for _, want := range []string{
		"does not publish moving `release` or `latest` aliases",
		"tagged release assets directly",
		"matching Linux binary",
	} {
		if !strings.Contains(binaries, want) {
			t.Fatalf("release binaries docs should mention %q:\n%s", want, binaries)
		}
	}
}

func TestRegistryCopyTagPublishesManifestWithoutDockerDaemon(t *testing.T) {
	// The mock models what Forgejo's registry actually enforces on a
	// cross-repo copy: the target rejects a manifest whose referenced blobs
	// and child manifests are not already present in the target repo, so the
	// script must mount blobs (?mount=&from=) and copy child manifests by
	// digest before the final tag PUT lands.
	const (
		childDigest  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		configDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		layerDigest  = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
		sourceBody   = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + childDigest + `","size":123,"platform":{"architecture":"amd64","os":"linux"}}]}`
		childBody    = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + configDigest + `","size":10},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"` + layerDigest + `","size":20}]}`
		targetBody   = sourceBody
	)

	var (
		indexPutSeen bool
		childPutSeen bool
		mounted      = map[string]bool{}
		gotAuth      []string
		mu           sync.Mutex
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		mu.Unlock()
		wantUser := "coilyco-ops"
		wantPass := "target-secret"
		if strings.HasPrefix(r.URL.Path, "/v2/coilyco-flight-deck/agentic-os/") {
			wantUser = "oauth2"
			wantPass = "source-secret"
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != wantUser || pass != wantPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/coilyco-flight-deck/agentic-os/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, sourceBody)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/coilyco-flight-deck/agentic-os/manifests/"+childDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, childBody)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/coilyco-flight-deck/ward/blobs/sha256:"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/coilyco-flight-deck/ward/blobs/uploads/":
			mount := r.URL.Query().Get("mount")
			if mount == "" || r.URL.Query().Get("from") != "coilyco-flight-deck/agentic-os" {
				t.Fatalf("blob upload POST without mount+from: %q", r.URL.RawQuery)
			}
			mu.Lock()
			mounted[mount] = true
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/coilyco-flight-deck/ward/manifests/"+childDigest:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read child PUT body: %v", err)
			}
			mu.Lock()
			blobsReady := mounted[configDigest] && mounted[layerDigest]
			childPutSeen = true
			mu.Unlock()
			if !blobsReady {
				t.Fatal("child manifest PUT before its blobs were mounted")
			}
			if string(body) != childBody {
				t.Fatalf("child PUT body = %q, want child manifest", body)
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/coilyco-flight-deck/ward/manifests/release":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			mu.Lock()
			childReady := childPutSeen
			indexPutSeen = true
			mu.Unlock()
			if !childReady {
				t.Fatal("index PUT before its child manifest was copied")
			}
			if string(body) != sourceBody {
				t.Fatalf("PUT body = %q, want source manifest", body)
			}
			if got := r.Header.Get("Content-Type"); got != "application/vnd.oci.image.index.v1+json" {
				t.Fatalf("PUT content-type = %q, want manifest index", got)
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/coilyco-flight-deck/ward/manifests/release":
			mu.Lock()
			seen := indexPutSeen
			mu.Unlock()
			if !seen {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, targetBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	u := strings.TrimPrefix(srv.URL, "http://")
	script := filepath.Join(repoRoot(t), "scripts", "registry-copy-tag.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"SOURCE_IMAGE="+u+"/coilyco-flight-deck/agentic-os:latest",
		"TARGET_IMAGE="+u+"/coilyco-flight-deck/ward:release",
		"SOURCE_TOKEN=source-secret",
		"TARGET_USER=coilyco-ops",
		"TARGET_TOKEN=target-secret",
		"REGISTRY_SCHEME=http",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("registry-copy-tag.sh failed: %v\noutput: %s", err, out)
	}
	mu.Lock()
	seen := indexPutSeen
	childSeen := childPutSeen
	blobsMounted := mounted[configDigest] && mounted[layerDigest]
	auths := append([]string(nil), gotAuth...)
	defer mu.Unlock()
	if !seen {
		t.Fatal("registry-copy-tag.sh did not publish the target manifest")
	}
	if !childSeen {
		t.Fatal("registry-copy-tag.sh did not copy the child manifest into the target repo")
	}
	if !blobsMounted {
		t.Fatal("registry-copy-tag.sh did not mount the child manifest's blobs into the target repo")
	}
	if len(auths) < 3 {
		t.Fatalf("expected auth on source, target, and verify requests; got %d requests", len(auths))
	}
	for i, auth := range auths {
		if !strings.HasPrefix(auth, "Basic ") {
			t.Fatalf("request %d missing basic auth header: %q", i, auth)
		}
	}
}

func TestReleaseTagHelperReusesHeadTagAndBumpsNextMinor(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	writeFile(t, repo, "one.txt", "one\n")
	runGit(t, repo, "add", "one.txt")
	runGit(t, repo, "commit", "-m", "feat: first")
	runGit(t, repo, "tag", "v0.1.0")

	writeFile(t, repo, "two.txt", "two\n")
	runGit(t, repo, "add", "two.txt")
	runGit(t, repo, "commit", "-m", "fix: second")
	runGit(t, repo, "tag", "v0.2.0")

	got := runReleaseTagHelper(t, repo)
	if got["new_tag"] != "v0.2.0" {
		t.Fatalf("head-tagged new_tag = %q, want v0.2.0", got["new_tag"])
	}
	if got["previous_tag"] != "v0.1.0" {
		t.Fatalf("head-tagged previous_tag = %q, want v0.1.0", got["previous_tag"])
	}
	if got["new_version"] != "0.2.0" {
		t.Fatalf("head-tagged new_version = %q, want 0.2.0", got["new_version"])
	}
	if !strings.Contains(got["changelog"], "fix: second") {
		t.Fatalf("head-tagged changelog = %q, want second commit subject", got["changelog"])
	}

	writeFile(t, repo, "three.txt", "three\n")
	runGit(t, repo, "add", "three.txt")
	runGit(t, repo, "commit", "-m", "feat: third")

	got = runReleaseTagHelper(t, repo)
	if got["new_tag"] != "v0.3.0" {
		t.Fatalf("untagged new_tag = %q, want v0.3.0", got["new_tag"])
	}
	if got["previous_tag"] != "v0.2.0" {
		t.Fatalf("untagged previous_tag = %q, want v0.2.0", got["previous_tag"])
	}
	if got["new_version"] != "0.3.0" {
		t.Fatalf("untagged new_version = %q, want 0.3.0", got["new_version"])
	}
	if !strings.Contains(got["changelog"], "feat: third") {
		t.Fatalf("untagged changelog = %q, want third commit subject", got["changelog"])
	}
}

func TestReleaseTagHelperPushesToHttpAndHttpsOrigins(t *testing.T) {
	for _, tc := range []struct {
		name     string
		remote   string
		scheme   string
		hostPath string
	}{
		{
			name:     "http",
			remote:   "http://forgejo.forgejo.svc.cluster.local/coilyco-flight-deck/ward",
			scheme:   "http",
			hostPath: "forgejo.forgejo.svc.cluster.local/coilyco-flight-deck/ward",
		},
		{
			name:     "https",
			remote:   "https://forgejo.coilysiren.me/coilyco-flight-deck/ward",
			scheme:   "https",
			hostPath: "forgejo.coilysiren.me/coilyco-flight-deck/ward",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := "test-token"
			got, pushed := runReleaseTagHelperWithRemote(t, tc.remote, token)
			if got["new_tag"] != "v0.1.0" {
				t.Fatalf("new_tag = %q, want v0.1.0", got["new_tag"])
			}
			wantAuth := tc.scheme + "://" + "oauth2:" + token + "@" + tc.hostPath
			if !strings.Contains(pushed, wantAuth) {
				t.Fatalf("push args = %q, want auth URL %q", pushed, wantAuth)
			}
			if strings.Contains(pushed, token) && !strings.Contains(pushed, wantAuth) {
				t.Fatalf("push args leaked token unexpectedly: %q", pushed)
			}
		})
	}
}

func TestPublishDraftReleaseCreatesBodyWithoutAssets(t *testing.T) {
	srv := newDraftReleaseTestServer(t)
	dist := t.TempDir()

	script := filepath.Join(repoRoot(t), "scripts", "publish-draft-release.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"DRAFT_TAG=draft-testsha",
		"FORGEJO_API="+srv.URL+"/api/v1/repos/coilyco-flight-deck/ward",
		"TOKEN=secret",
		"DIST_DIR="+dist,
		"RELEASE_BODY=release body from helper",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("publish-draft-release.sh body-only failed: %v\noutput: %s", err, out)
	}
	if got := srv.count("POST /api/v1/repos/coilyco-flight-deck/ward/releases"); got != 1 {
		t.Fatalf("body-only create count = %d, want 1", got)
	}
	if got := srv.count("POST /api/v1/repos/coilyco-flight-deck/ward/releases/99/assets?name=README"); got != 0 {
		t.Fatalf("body-only asset upload count = %d, want 0", got)
	}
	if !strings.Contains(srv.lastCreateBody, `"body":"release body from helper"`) {
		t.Fatalf("body-only create payload = %q, want release body", srv.lastCreateBody)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\noutput: %s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runReleaseTagHelper(t *testing.T, repo string) map[string]string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "outputs.txt")
	script := filepath.Join(repoRoot(t), "scripts", "release-tag.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+output)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release-tag.sh failed: %v\noutput: %s", err, out)
	}
	return parseGitHubOutputs(t, output)
}

func runReleaseTagHelperWithRemote(t *testing.T, remoteURL, token string) (map[string]string, string) {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "remote", "add", "origin", remoteURL)

	writeFile(t, repo, "one.txt", "one\n")
	runGit(t, repo, "add", "one.txt")
	runGit(t, repo, "commit", "-m", "feat: first")

	output := filepath.Join(t.TempDir(), "outputs.txt")
	record := filepath.Join(t.TempDir(), "push.txt")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("look up git: %v", err)
	}

	binDir := t.TempDir()
	wrapper := filepath.Join(binDir, "git")
	wrapperScript := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  push)
    printf '%%s\n' "$*" > %q
    exit 1
    ;;
  ls-remote)
    printf 'deadbeef\t%%s\n' "${3:-}"
    exit 0
    ;;
  *)
    exec "${REAL_GIT:?}" "$@"
    ;;
esac
`, record)
	if err := os.WriteFile(wrapper, []byte(wrapperScript), 0o755); err != nil {
		t.Fatalf("write git wrapper: %v", err)
	}

	script := filepath.Join(repoRoot(t), "scripts", "release-tag.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GITHUB_OUTPUT="+output,
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"REAL_GIT="+realGit,
		"TAG_PUSH_TOKEN="+token,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("release-tag.sh with remote %s failed: %v\noutput: %s", remoteURL, err, out)
	}
	if strings.Contains(string(out), "secret") {
		t.Fatalf("release-tag.sh output leaked token for remote %s: %s", remoteURL, out)
	}

	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read push record: %v", err)
	}
	return parseGitHubOutputs(t, output), strings.TrimSpace(string(raw))
}

func parseGitHubOutputs(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read outputs %s: %v", path, err)
	}
	out := make(map[string]string)
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	for i := 0; i < len(lines); i++ {
		line := string(lines[i])
		if strings.Contains(line, "<<") {
			parts := strings.SplitN(line, "<<", 2)
			key, marker := parts[0], parts[1]
			var buf []string
			for i++; i < len(lines) && string(lines[i]) != marker; i++ {
				buf = append(buf, string(lines[i]))
			}
			out[key] = strings.Join(buf, "\n")
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if found {
			out[key] = val
		}
	}
	return out
}

func workflowJobSection(t *testing.T, workflow, start, end string) string {
	t.Helper()
	startIdx := strings.Index(workflow, start)
	if startIdx < 0 {
		t.Fatalf("workflow missing %q:\n%s", start, workflow)
	}
	section := workflow[startIdx:]
	if end != "" {
		if endIdx := strings.Index(section, "\n      - name: "+end); endIdx >= 0 {
			section = section[:endIdx]
		}
	}
	return section
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
		t.Fatalf("asset metadata fetch count = %d, want 1", got)
	}
	if got := srv.count("GET /attachments/7"); got != 1 {
		t.Fatalf("attachment metadata fetch count = %d, want 1", got)
	}
	if got := srv.count("GET /attachments/8"); got != 1 {
		t.Fatalf("raw attachment body fetch count = %d, want 1", got)
	}
}

func TestForgejoReleaseAssetHelperPassesMultilineBodiesThrough(t *testing.T) {
	// The scoop job fetches SHA256SUMS - a multi-line document, not a bare
	// digest. The helper must pass any non-metadata body through raw instead
	// of demanding a single 64-hex line (the gap behind the v0.782.0
	// bump-scoop-manifest failure, ward#1493).
	srv := newReleaseAssetTestServer(t)
	script := filepath.Join(repoRoot(t), "scripts", "forgejo-release-asset.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"RELEASE_TAG=v9.9.9",
		"ASSET_NAME=SHA256SUMS",
		"FORGEJO_API="+srv.URL+"/api/v1/repos/coilyco-flight-deck/ward",
		"TOKEN=secret",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forgejo-release-asset.sh failed: %v\noutput: %s", err, out)
	}
	if string(out) != testSHA256SUMSBody {
		t.Fatalf("asset body = %q, want the raw SHA256SUMS document", out)
	}
}

type draftReleaseTestServer struct {
	*httptest.Server
	mu             sync.Mutex
	counts         map[string]int
	released       bool
	assets         map[string]int
	lastCreateBody string
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
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.lastCreateBody = string(body)
	s.mu.Unlock()
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
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/99/assets/7", s.handleReleaseAssetMetadata)
	mux.HandleFunc("/attachments/7", s.handleReleaseAttachmentMetadata)
	mux.HandleFunc("/attachments/8", s.handleReleaseAttachmentBody)
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/releases/99/assets/9", s.handleChecksumsAssetMetadata)
	mux.HandleFunc("/attachments/10", s.handleChecksumsAttachmentBody)
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
		{"id":8,"name":"ward-windows-arm64.exe.sha256"},
		{"id":9,"name":"SHA256SUMS"}
	]`))
}

var testSHA256SUMSBody = strings.Repeat("a", 64) + "  ward-linux-amd64\n" + strings.Repeat("b", 64) + "  ward-linux-arm64\n"

func (s *releaseAssetTestServer) handleChecksumsAssetMetadata(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(`{"id":9,"name":"SHA256SUMS","browser_download_url":"` + s.URL + `/attachments/10"}`))
}

func (s *releaseAssetTestServer) handleChecksumsAttachmentBody(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(testSHA256SUMSBody))
}

func (s *releaseAssetTestServer) handleReleaseAssetMetadata(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(`{"id":7,"name":"ward-windows-amd64.exe.sha256","browser_download_url":"http://` + r.Host + `/attachments/7","type":"attachment"}`))
}

func (s *releaseAssetTestServer) handleReleaseAttachmentMetadata(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(`{"id":8,"name":"ward-windows-amd64.exe.sha256","browser_download_url":"http://` + r.Host + `/attachments/8","type":"attachment"}`))
}

func (s *releaseAssetTestServer) handleReleaseAttachmentBody(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	_, _ = w.Write([]byte(strings.Repeat("a", 64)))
}

func TestForgejoAssetURLResolverFollowsMetadataHops(t *testing.T) {
	// Models the ward#1493 Forgejo behavior: the download route serves tiny
	// attachment-metadata JSON regardless of Accept, and only the metadata's
	// browser_download_url (/attachments/<uuid>) serves bytes. The resolver
	// must emit the final byte URL plus the sha256 of those bytes.
	const body = "real ward binary bytes\n"
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/owner/ward/releases/download/v9.9.9/ward-darwin-arm64", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":1,"name":"ward-darwin-arm64","browser_download_url":"`+srvURL+`/attachments/50"}`)
	})
	mux.HandleFunc("/attachments/50", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	script := filepath.Join(repoRoot(t), "scripts", "forgejo-asset-url.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"DOWNLOAD_BASE="+srv.URL+"/owner/ward/releases/download/v9.9.9",
		"ASSET_NAME=ward-darwin-arm64",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forgejo-asset-url.sh failed: %v\noutput: %s", err, out)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		t.Fatalf("resolver output = %q, want \"<url> <sha>\"", out)
	}
	if fields[0] != srv.URL+"/attachments/50" {
		t.Fatalf("resolved url = %q, want the attachments byte URL", fields[0])
	}
	sum := sha256.Sum256([]byte(body))
	if fields[1] != hex.EncodeToString(sum[:]) {
		t.Fatalf("resolved sha = %q, want sha of the byte body", fields[1])
	}
}
