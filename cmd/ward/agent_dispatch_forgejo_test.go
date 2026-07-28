package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func TestPolicyBoundaryNativeForgejoBrokerAllowsSyntheticReadAndWrite(t *testing.T) {
	const syntheticCredential = "synthetic-forgejo-credential-ward-1612"
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		if got := req.Header.Get("Authorization"); got != "token "+syntheticCredential {
			t.Errorf("authorization = %q, want broker-held synthetic credential", got)
		}
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues/1612":
			_, _ = io.WriteString(w, `{"number":1612,"title":"broker read"}`)
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward/issues":
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode write body: %v", err)
			}
			if body["title"] != "broker write" {
				t.Errorf("write body = %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"number":1613}`)
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{token: syntheticCredential, baseURL: srv.URL}
	calls := []nativeForgejoRequest{
		{
			Method:   http.MethodGet,
			Segments: []string{"repos", "coilyco-flight-deck", "ward", "issues", "1612"},
			Accept:   "application/json",
		},
		{
			Method:   http.MethodPost,
			Segments: []string{"repos", "coilyco-flight-deck", "ward", "issues"},
			Body:     []byte(`{"title":"broker write","body":"synthetic"}`),
			Accept:   "application/json",
		},
	}
	for _, call := range calls {
		req := dispatchBrokerRequest{Action: dispatchActionForgejo, Role: roleDirector, Forgejo: &call}
		if err := validateDispatchBrokerForgejo(req); err != nil {
			t.Fatalf("validate %s %s: %v", call.Method, apiPath(call.Segments), err)
		}
		result, kind, err := execDispatchBrokerForgejoWith(t.Context(), cl, call)
		if err != nil {
			t.Fatalf("execute %s %s: %v", call.Method, apiPath(call.Segments), err)
		}
		if kind != "" || result.Status < 200 || result.Status >= 300 {
			t.Fatalf("execute %s %s = status %d kind %q", call.Method, apiPath(call.Segments), result.Status, kind)
		}
		if strings.Contains(string(result.Body), syntheticCredential) {
			t.Fatal("broker response contains the synthetic credential")
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests = %d, want read + write", got)
	}
}

func TestPolicyBoundaryNativeForgejoBrokerRejectsPolicyBeforeUpstream(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()

	call := nativeForgejoRequest{
		Method:   http.MethodDelete,
		Segments: []string{"repos", "coilyco-flight-deck", "ward"},
	}
	err := validateDispatchBrokerForgejo(dispatchBrokerRequest{
		Action:  dispatchActionForgejo,
		Role:    roleDirector,
		Forgejo: &call,
	})
	if err == nil || !strings.Contains(err.Error(), "not in Ward's native route allowlist") {
		t.Fatalf("policy error = %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("disallowed operation reached upstream %d time(s)", got)
	}

	call = nativeForgejoRequest{
		Method:   http.MethodGet,
		Segments: []string{"repos", "outside", "ward", "issues", "1"},
	}
	err = validateDispatchBrokerForgejo(dispatchBrokerRequest{
		Action:  dispatchActionForgejo,
		Role:    roleDirector,
		Forgejo: &call,
	})
	if err == nil || !strings.Contains(err.Error(), "out of scope") {
		t.Fatalf("scope error = %v", err)
	}
}

func TestPolicyBoundaryNativeForgejoBrokerClassifiesAuthNetworkAndPolicy(t *testing.T) {
	const syntheticCredential = "synthetic-rejected-credential-ward-1612"
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rejected "+syntheticCredential, http.StatusUnauthorized)
	}))
	defer authServer.Close()

	call := nativeForgejoRequest{
		Method:   http.MethodGet,
		Segments: []string{"repos", "coilyco-flight-deck", "ward", "issues", "1612"},
		Accept:   "application/json",
	}
	_, kind, err := execDispatchBrokerForgejoWith(t.Context(), &forgejoClient{
		token:   syntheticCredential,
		baseURL: authServer.URL,
	}, call)
	if kind != nativeForgejoErrorAuth || err == nil {
		t.Fatalf("auth result = kind %q err %v", kind, err)
	}
	if strings.Contains(err.Error(), syntheticCredential) {
		t.Fatalf("auth error leaked synthetic credential: %v", err)
	}

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected echo "+syntheticCredential, http.StatusInternalServerError)
	}))
	result, kind, err := execDispatchBrokerForgejoWith(t.Context(), &forgejoClient{
		token:   syntheticCredential,
		baseURL: echoServer.URL,
	}, call)
	echoServer.Close()
	if kind != "" || err != nil || result.Status != http.StatusInternalServerError {
		t.Fatalf("upstream error response = status %d kind %q err %v", result.Status, kind, err)
	}
	if strings.Contains(string(result.Body), syntheticCredential) || !strings.Contains(string(result.Body), redactionPlaceholder) {
		t.Fatalf("upstream response was not credential-redacted: %q", result.Body)
	}

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	networkURL := closed.URL
	closed.Close()
	_, kind, err = execDispatchBrokerForgejoWith(t.Context(), &forgejoClient{
		token:   syntheticCredential,
		baseURL: networkURL,
	}, call)
	if kind != nativeForgejoErrorNetwork || err == nil {
		t.Fatalf("network result = kind %q err %v", kind, err)
	}

	bad := dispatchBrokerRequest{
		Action: dispatchActionForgejo,
		Role:   roleEngineer,
		Forgejo: &nativeForgejoRequest{
			Method:   http.MethodGet,
			Segments: []string{"repos", "coilyco-flight-deck", "ward"},
		},
	}
	if err := validateDispatchBrokerForgejo(bad); err == nil || !strings.Contains(err.Error(), "requires role") {
		t.Fatalf("policy result = %v", err)
	}
}

func TestPolicyBoundaryDispatchBrokerCapabilityFailureIsAuthNotNetwork(t *testing.T) {
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&Runner{}).handleHostDispatchBrokerConn(t.Context(), server, "director-codex-test", "expected-capability")
	}()
	err := json.NewEncoder(client).Encode(dispatchBrokerRequest{
		Action: dispatchActionForgejo,
		Role:   roleDirector,
		Token:  "wrong-capability",
		Forgejo: &nativeForgejoRequest{
			Method:   http.MethodGet,
			Segments: []string{"repos", "coilyco-flight-deck", "ward"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	<-done
	if resp.OK || resp.ErrorKind != nativeForgejoErrorAuth || !strings.Contains(resp.Error, "capability rejected") {
		t.Fatalf("capability response = %#v", resp)
	}
}

func TestPolicyBoundaryDispatchBrokerStampsRoleFromBrokerService(t *testing.T) {
	t.Setenv("WARD_ROLE", roleEngineer)
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&Runner{}).handleHostDispatchBrokerConn(t.Context(), server, "broker-owned-requester", "capability")
	}()
	err := json.NewEncoder(client).Encode(dispatchBrokerRequest{
		Action:    dispatchActionForgejo,
		Role:      roleDirector,
		Requester: "spoofed-requester",
		Token:     "capability",
		Forgejo: &nativeForgejoRequest{
			Method:   http.MethodGet,
			Segments: []string{"repos", "coilyco-flight-deck", "ward"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	<-done
	if resp.OK || resp.ErrorKind != nativeForgejoErrorPolicy ||
		!strings.Contains(resp.Error, `requires role "director", got "engineer"`) {
		t.Fatalf("broker-owned role response = %#v", resp)
	}
}

func TestPolicyBoundaryDirectorNativeForgejoTransportCarriesNoForgejoCredential(t *testing.T) {
	const (
		brokerCapability  = "synthetic-broker-capability"
		forgejoCredential = "must-never-cross-director-boundary"
	)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	received := make(chan dispatchBrokerRequest, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var req dispatchBrokerRequest
		if decodeErr := json.NewDecoder(conn).Decode(&req); decodeErr != nil {
			return
		}
		received <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{
			OK:          true,
			Status:      http.StatusOK,
			ContentType: "application/json",
			Body:        []byte(`{"number":1612,"title":"brokered"}`),
		})
	}()

	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_ROLE", roleDirector)
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-test")
	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, brokerCapability)
	t.Setenv("FORGEJO_TOKEN", forgejoCredential)

	cl := (&Runner{}).hostForgejoClient(t.Context())
	if cl.token != "" {
		t.Fatal("director native client resolved the raw Forgejo credential")
	}
	cl.baseURL = "https://forge.invalid"
	issue, err := cl.GetIssue(
		t.Context(), "coilyco-flight-deck", "ward", 1612,
	)
	if err != nil {
		t.Fatalf("brokered GetIssue: %v", err)
	}
	if issue.Number != 1612 {
		t.Fatalf("brokered issue = %#v", issue)
	}
	req := <-received
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if req.Action != dispatchActionForgejo || req.Token != brokerCapability {
		t.Fatalf("broker request action/token = %q/%q", req.Action, req.Token)
	}
	if strings.Contains(string(encoded), forgejoCredential) || strings.Contains(string(encoded), "FORGEJO_TOKEN") {
		t.Fatalf("director broker request contains Forgejo credential material: %s", encoded)
	}
}

func TestPolicyBoundaryDirectorEnvFileExcludesForgejoCredential(t *testing.T) {
	const syntheticCredential = "synthetic-forgejo-credential-ward-1612"
	t.Setenv(envLaunchStagingDir, t.TempDir())
	path, cleanup, err := writeAgentEnvFile([]agentsapi.EnvLine{{
		Key:   codexAuthEnvKey,
		Value: "synthetic-codex-auth",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "FORGEJO_TOKEN") || strings.Contains(text, syntheticCredential) {
		t.Fatalf("director env file contains Forgejo credential material: %q", text)
	}
	if !strings.Contains(text, codexAuthEnvKey+"=synthetic-codex-auth") {
		t.Fatalf("director env file omitted selected harness credential: %q", text)
	}
}

func TestPolicyBoundaryNativeForgejoRequestBodyBound(t *testing.T) {
	body := io.NopCloser(strings.NewReader(strings.Repeat("x", nativeForgejoRequestBodyLimit+1)))
	_, err := readBoundedBody(body, nativeForgejoRequestBodyLimit)
	if err == nil {
		t.Fatal("oversized director request body was accepted")
	}
}

func TestPolicyBoundaryNativeForgejoTransportRejectsNonAPIPath(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://forge.invalid/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (nativeForgejoTransport{}).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "outside /api/v1") {
		t.Fatalf("non-API path error = %v", err)
	}
}
