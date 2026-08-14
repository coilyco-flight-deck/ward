package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/shell"
)

// genTestKeyPEM makes a fresh RSA key, PKCS1 PEM-encoded. Generating it keeps the suite
// offline and ships no real-looking key literal a scanner would flag.
func genTestKeyPEM(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, string(pemBytes)
}

// TestParseRSAPrivateKeyPEM accepts both PKCS#1 and PKCS#8 encodings and rejects junk.
func TestParseRSAPrivateKeyPEM(t *testing.T) {
	key, pkcs1 := genTestKeyPEM(t)
	if _, err := parseRSAPrivateKeyPEM(pkcs1); err != nil {
		t.Fatalf("PKCS#1 parse: %v", err)
	}
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes}))
	if _, err := parseRSAPrivateKeyPEM(pkcs8); err != nil {
		t.Fatalf("PKCS#8 parse: %v", err)
	}
	if _, err := parseRSAPrivateKeyPEM("not a pem"); err == nil {
		t.Fatal("garbage PEM: want error, got nil")
	}
}

// TestSignAppJWT checks the JWT is three RS256 segments with a verifiable signature and
// the expected App-authentication claims (iss, a backdated iat, and a sub-10m exp).
func TestSignAppJWT(t *testing.T) {
	key, _ := genTestKeyPEM(t)
	now := time.Unix(1_700_000_000, 0)
	jwt, err := signAppJWT(key, "12345", now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d segments, want 3", len(parts))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "12345" {
		t.Errorf("iss = %q, want 12345", claims.Iss)
	}
	if claims.Iat >= now.Unix() {
		t.Errorf("iat = %d, want backdated below %d", claims.Iat, now.Unix())
	}
	if window := claims.Exp - claims.Iat; window > 600 {
		t.Errorf("exp-iat window = %ds, exceeds GitHub's 600s cap", window)
	}
}

// TestMintInstallationToken drives the two-call flow against a stub GitHub API, asserting
// the App headers, the repositories-scoping body, and that the minted token is returned.
func TestMintInstallationToken(t *testing.T) {
	var sawInstall, sawScoped bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer the.jwt.here" {
			t.Errorf("Authorization = %q, want the App JWT bearer", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/coilyco/ward/installation":
			sawInstall = true
			_, _ = w.Write([]byte(`{"id": 987}`))
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/987/access_tokens":
			sawScoped = true
			var body struct {
				Repositories []string `json:"repositories"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Repositories) != 1 || body.Repositories[0] != "ward" {
				t.Errorf("token request repositories = %v, want [ward]", body.Repositories)
			}
			_, _ = w.Write([]byte(`{"token": "ghs_minted_scoped"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tok, err := mintInstallationToken(t.Context(), srv.Client(), srv.URL, "the.jwt.here", "coilyco", "ward")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if tok != "ghs_minted_scoped" {
		t.Fatalf("token = %q, want ghs_minted_scoped", tok)
	}
	if !sawInstall || !sawScoped {
		t.Fatalf("flow incomplete: install=%v scoped=%v", sawInstall, sawScoped)
	}
}

// TestMintInstallationTokenNoInstallation surfaces the App-not-installed case (id 0) as a
// clear error, not an empty-token success.
func TestMintInstallationTokenNoInstallation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id": 0}`))
	}))
	defer srv.Close()
	if _, err := mintInstallationToken(t.Context(), srv.Client(), srv.URL, "j", "o", "r"); err == nil {
		t.Fatal("id 0: want an error, got nil")
	}
}

// TestMintInstallationTokenAPIError maps a non-2xx GitHub reply to an error.
func TestMintInstallationTokenAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad jwt"}`))
	}))
	defer srv.Close()
	_, err := mintInstallationToken(t.Context(), srv.Client(), srv.URL, "j", "o", "r")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("api error = %v, want an error naming the 401", err)
	}
}

// TestResolveGitHubTokenFromAppEndToEnd stitches PEM env -> JWT -> installation ->
// scoped token with a stub GitHub API.
func TestResolveGitHubTokenFromAppEndToEnd(t *testing.T) {
	_, keyPEM := genTestKeyPEM(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id": 42}`))
			return
		}
		_, _ = w.Write([]byte(`{"token": "ghs_e2e"}`))
	}))
	defer srv.Close()

	orig := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = orig }()

	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "app")
	t.Setenv(envGitHubAppID, "999")
	t.Setenv(envGitHubAppPrivateKey, keyPEM)

	tok, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward")
	if err != nil {
		t.Fatalf("app end-to-end: %v", err)
	}
	if tok != "ghs_e2e" {
		t.Fatalf("token = %q, want ghs_e2e", tok)
	}
}

// TestResolveGitHubTokenFromAppNeedsTarget guards that app mode refuses without a target
// owner/repo to scope the token to.
func TestResolveGitHubTokenFromAppNeedsTarget(t *testing.T) {
	t.Setenv(envGitHubAppID, "1")
	t.Setenv(envGitHubAppPrivateKey, "pem")
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}
	if _, err := r.resolveGitHubTokenFromApp(t.Context(), "", ""); err == nil {
		t.Fatal("app mode with no target: want error, got nil")
	}
}
