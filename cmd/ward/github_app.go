package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// github_app.go mints a short-lived, repo-scoped GitHub App installation token for the
// WARD_GITHUB_TOKEN_SOURCE=app dispatch arm (ward#534). See docs/github-token.md.

const (
	// envGitHubAppID names the numeric App ID (the JWT `iss`), operator-supplied.
	envGitHubAppID = "WARD_GITHUB_APP_ID"
	// envGitHubAppKeySSM names the SSM param holding the App's PEM private key. The
	// param NAME is operator config (env); the key VALUE never leaves SSM until dispatch.
	envGitHubAppKeySSM = "WARD_GITHUB_APP_KEY_SSM"
)

// githubAPIBase is the GitHub REST origin the App mint calls hit; a package var so a
// test can point it at an httptest server. Never on argv, never audited.
var githubAPIBase = "https://api.github.com"

// resolveGitHubTokenFromApp mints an owner/repo-scoped installation token: env config ->
// PEM from SSM -> App JWT -> installation lookup -> repo-scoped token (ward#534).
func (r *Runner) resolveGitHubTokenFromApp(ctx context.Context, owner, repo string) (string, error) {
	appID := strings.TrimSpace(os.Getenv(envGitHubAppID))
	keySSM := strings.TrimSpace(os.Getenv(envGitHubAppKeySSM))
	if appID == "" || keySSM == "" {
		return "", fmt.Errorf(
			"ward: WARD_GITHUB_TOKEN_SOURCE=app needs %s (the App ID) and %s (the SSM param holding the App private key) - "+
				"set both from operator config, or switch to WARD_GITHUB_TOKEN_SOURCE=env or gh. "+
				"app mode is gated on a registered GitHub App (ward#534). See docs/github-token.md",
			envGitHubAppID, envGitHubAppKeySSM)
	}
	if owner == "" || repo == "" {
		return "", fmt.Errorf("ward: WARD_GITHUB_TOKEN_SOURCE=app needs a target owner/repo to scope the installation token, but none was resolved")
	}

	pemKey, err := r.ssmValueResolver(ctx, keySSM)
	if err != nil {
		return "", fmt.Errorf("ward: app mode: resolve App private key from SSM param %q (host needs aws creds): %w", keySSM, err)
	}
	key, err := parseRSAPrivateKeyPEM(pemKey)
	if err != nil {
		return "", fmt.Errorf("ward: app mode: parse App private key from SSM param %q: %w", keySSM, err)
	}

	jwt, err := signAppJWT(key, appID, time.Now())
	if err != nil {
		return "", fmt.Errorf("ward: app mode: sign App JWT: %w", err)
	}
	tok, err := mintInstallationToken(ctx, http.DefaultClient, githubAPIBase, jwt, owner, repo)
	if err != nil {
		return "", fmt.Errorf("ward: app mode: mint installation token for %s/%s: %w", owner, repo, err)
	}
	return tok, nil
}

// parseRSAPrivateKeyPEM decodes an RSA private key from PEM, accepting both the PKCS#1
// (`RSA PRIVATE KEY`) form GitHub hands out and the PKCS#8 (`PRIVATE KEY`) form. Pure.
func parseRSAPrivateKeyPEM(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemKey)))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS#1 or PKCS#8 RSA key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("PKCS#8 key is %T, not RSA", parsed)
	}
	return key, nil
}

// signAppJWT builds the RS256-signed App JWT: iss is the App ID, iat backdated 60s for
// clock skew, exp +9m (under GitHub's 10m cap). now is a param so the window is testable.
func signAppJWT(key *rsa.PrivateKey, appID string, now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// mintInstallationToken runs the two-call App flow against apiBase: resolve owner/repo's
// installation with the JWT, then exchange the JWT for a token scoped to just that repo.
func mintInstallationToken(ctx context.Context, client *http.Client, apiBase, jwt, owner, repo string) (string, error) {
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := githubAppGET(ctx, client, apiBase+"/repos/"+owner+"/"+repo+"/installation", jwt, &inst); err != nil {
		return "", fmt.Errorf("resolve installation: %w", err)
	}
	if inst.ID == 0 {
		return "", fmt.Errorf("the App has no installation on %s/%s", owner, repo)
	}

	body, err := json.Marshal(map[string]any{"repositories": []string{repo}})
	if err != nil {
		return "", err
	}
	var minted struct {
		Token string `json:"token"`
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBase, inst.ID)
	if err := githubAppPOST(ctx, client, url, jwt, body, &minted); err != nil {
		return "", fmt.Errorf("exchange JWT for installation token: %w", err)
	}
	if strings.TrimSpace(minted.Token) == "" {
		return "", fmt.Errorf("GitHub returned an empty installation token")
	}
	return minted.Token, nil
}

// githubAppGET issues an App-authenticated GET and decodes a 2xx JSON body into out.
func githubAppGET(ctx context.Context, client *http.Client, url, jwt string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return doGitHubAppReq(client, req, jwt, out)
}

// githubAppPOST issues an App-authenticated POST with a JSON body and decodes the reply.
func githubAppPOST(ctx context.Context, client *http.Client, url, jwt string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doGitHubAppReq(client, req, jwt, out)
}

// doGitHubAppReq stamps the App headers, sends req, and decodes a 2xx body into out; a
// non-2xx is an error carrying a bounded slice of the response for diagnosis.
func doGitHubAppReq(client *http.Client, req *http.Request, jwt string, out any) error {
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GitHub returned %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("parse GitHub response: %w", err)
	}
	return nil
}
