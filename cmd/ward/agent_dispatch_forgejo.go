package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// This file is the credential-free director half of Ward's native Forgejo
// control plane. The broker allows fixed route families, not arbitrary HTTP.

const (
	nativeForgejoRequestBodyLimit  = 1 << 20
	nativeForgejoResponseBodyLimit = 8 << 20
	forgejoHTTPTimeout             = 30 * time.Second

	nativeForgejoErrorAuth    = "auth"
	nativeForgejoErrorNetwork = "network"
	nativeForgejoErrorPolicy  = "policy"
)

type nativeForgejoRequest struct {
	Method   string              `json:"method"`
	Segments []string            `json:"segments"`
	Query    map[string][]string `json:"query,omitempty"`
	Body     []byte              `json:"body,omitempty"`
	Accept   string              `json:"accept,omitempty"`
}

// nativeForgejoTransport brokers Ward's fixed-base Forgejo requests while
// exposing only the upstream status and bounded response body.
type nativeForgejoTransport struct {
	addr      string
	token     string
	requester string
	role      string
}

func (t nativeForgejoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("forgejo broker policy: request URL is missing")
	}
	segments, err := nativeForgejoSegments(req.URL)
	if err != nil {
		return nil, err
	}
	body, err := readBoundedBody(req.Body, nativeForgejoRequestBodyLimit)
	if err != nil {
		return nil, fmt.Errorf("forgejo broker policy: request body: %w", err)
	}
	result, err := sendDispatchBrokerForgejoRequest(req.Context(), t.addr, dispatchBrokerRequest{
		Action:    dispatchActionForgejo,
		Role:      t.role,
		Requester: t.requester,
		Token:     t.token,
		Forgejo: &nativeForgejoRequest{
			Method:   req.Method,
			Segments: segments,
			Query:    req.URL.Query(),
			Body:     body,
			Accept:   req.Header.Get("Accept"),
		},
	})
	if err != nil {
		return nil, err
	}
	statusText := http.StatusText(result.Status)
	if statusText == "" {
		statusText = "Unknown Status"
	}
	return &http.Response{
		StatusCode: result.Status,
		Status:     strconv.Itoa(result.Status) + " " + statusText,
		Header:     http.Header{"Content-Type": []string{result.ContentType}},
		Body:       io.NopCloser(bytes.NewReader(result.Body)),
		Request:    req,
	}, nil
}

type nativeForgejoResult struct {
	Status      int
	ContentType string
	Body        []byte
}

func nativeForgejoSegments(u *url.URL) ([]string, error) {
	const prefix = "/api/v1/"
	path := u.EscapedPath()
	if !strings.HasPrefix(path, prefix) {
		return nil, fmt.Errorf("forgejo broker policy: path %q is outside /api/v1", u.Path)
	}
	raw := strings.Split(strings.TrimPrefix(path, prefix), "/")
	segments := make([]string, 0, len(raw))
	for _, item := range raw {
		segment, err := url.PathUnescape(item)
		if err != nil {
			return nil, fmt.Errorf("forgejo broker policy: decode path segment: %w", err)
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func readBoundedBody(r io.ReadCloser, limit int64) ([]byte, error) {
	if r == nil || r == http.NoBody {
		return nil, nil
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("exceeds %d bytes", limit)
	}
	return data, nil
}

func (r *Runner) runDispatchBrokerForgejo(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
	if err := validateDispatchBrokerForgejo(req); err != nil {
		writeDispatchBrokerForgejoResponse(conn, nativeForgejoResult{}, nativeForgejoErrorPolicy, err)
		return
	}
	result, kind, err := r.execDispatchBrokerForgejo(ctx, *req.Forgejo)
	writeDispatchBrokerForgejoResponse(conn, result, kind, err)
}

func (r *Runner) execDispatchBrokerForgejo(ctx context.Context, request nativeForgejoRequest) (nativeForgejoResult, string, error) {
	return execDispatchBrokerForgejoWith(ctx, r.hostForgejoClient(ctx), request)
}

func execDispatchBrokerForgejoWith(ctx context.Context, cl *forgejoClient, request nativeForgejoRequest) (nativeForgejoResult, string, error) {
	token, err := cl.apiToken(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return nativeForgejoResult{}, nativeForgejoErrorAuth, errors.New("privileged broker has no Forgejo credential; recycle the director")
	}
	endpoint := cl.apiURL(request.Segments...)
	if len(request.Query) > 0 {
		endpoint += "?" + url.Values(request.Query).Encode()
	}
	upstream, err := http.NewRequestWithContext(ctx, request.Method, endpoint, bytes.NewReader(request.Body))
	if err != nil {
		return nativeForgejoResult{}, nativeForgejoErrorPolicy, fmt.Errorf("build fixed Forgejo request: %w", err)
	}
	upstream.Header.Set("Authorization", "token "+token)
	upstream.Header.Set("Accept", request.Accept)
	if len(request.Body) > 0 {
		upstream.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: forgejoHTTPTimeout}).Do(upstream)
	if err != nil {
		return nativeForgejoResult{}, nativeForgejoErrorNetwork, fmt.Errorf("reach Forgejo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, nativeForgejoResponseBodyLimit+1))
	if err != nil {
		return nativeForgejoResult{}, nativeForgejoErrorNetwork, fmt.Errorf("read Forgejo response: %w", err)
	}
	if len(body) > nativeForgejoResponseBodyLimit {
		return nativeForgejoResult{}, nativeForgejoErrorPolicy,
			fmt.Errorf("forgejo response exceeds %d bytes", nativeForgejoResponseBodyLimit)
	}
	body = bytes.ReplaceAll(body, []byte(token), []byte(redactionPlaceholder))
	if resp.StatusCode == http.StatusUnauthorized {
		return nativeForgejoResult{}, nativeForgejoErrorAuth,
			errors.New("privileged broker credential was rejected; recycle the director")
	}
	return nativeForgejoResult{
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
	}, "", nil
}

func writeDispatchBrokerForgejoResponse(conn net.Conn, result nativeForgejoResult, kind string, err error) {
	resp := dispatchBrokerResponse{
		OK:          err == nil,
		Status:      result.Status,
		Body:        result.Body,
		ContentType: result.ContentType,
		ErrorKind:   kind,
	}
	if err != nil {
		resp.Error = redactSecrets(err.Error())
	}
	if data, marshalErr := json.Marshal(resp); marshalErr == nil {
		_, _ = conn.Write(data)
	}
}

func sendDispatchBrokerForgejoRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (nativeForgejoResult, error) {
	conn, err := dialDispatchBroker(ctx, addr)
	if err != nil {
		return nativeForgejoResult{}, dispatchBrokerDialDiagnostic(addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nativeForgejoResult{}, fmt.Errorf("forgejo broker network: send request: %w", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nativeForgejoResult{}, fmt.Errorf("forgejo broker network: read response: %w", err)
	}
	if !resp.OK {
		kind := strings.TrimSpace(resp.ErrorKind)
		if kind == "" {
			kind = nativeForgejoErrorNetwork
		}
		return nativeForgejoResult{}, fmt.Errorf("forgejo broker %s: %s", kind, resp.Error)
	}
	return nativeForgejoResult{Status: resp.Status, ContentType: resp.ContentType, Body: resp.Body}, nil
}

func validateDispatchBrokerForgejo(req dispatchBrokerRequest) error {
	if err := validateDispatchBrokerForgejoEnvelope(req); err != nil {
		return err
	}
	call := req.Forgejo
	if !nativeForgejoRouteAllowed(call.Method, call.Segments) {
		return fmt.Errorf("dispatch broker: forgejo operation %s %s is not in Ward's native route allowlist",
			call.Method, apiPath(call.Segments))
	}
	owner, repo, ok := nativeForgejoScope(call.Segments)
	if !ok || !dispatchStopTargetRe.MatchString(owner) || !strings.HasPrefix(owner, brokerOwnerPrefix) {
		return fmt.Errorf("dispatch broker: forgejo owner %q is out of scope (restricted to %s* owners)", owner, brokerOwnerPrefix)
	}
	if repo != "" && !dispatchStopTargetRe.MatchString(repo) {
		return fmt.Errorf("dispatch broker: forgejo repository %q is not well formed", repo)
	}
	return nil
}

func validateDispatchBrokerForgejoEnvelope(req dispatchBrokerRequest) error {
	if strings.TrimSpace(req.Role) != roleDirector {
		return fmt.Errorf("dispatch broker: forgejo action requires role %q, got %q", roleDirector, req.Role)
	}
	if len(req.Argv) != 0 || strings.TrimSpace(req.Target) != "" {
		return fmt.Errorf("dispatch broker: forgejo action accepts no launch argv or free-form target")
	}
	if req.Forgejo == nil {
		return fmt.Errorf("dispatch broker: forgejo action requires a native request")
	}
	call := req.Forgejo
	if len(call.Body) > nativeForgejoRequestBodyLimit {
		return fmt.Errorf("dispatch broker: forgejo request body exceeds %d bytes", nativeForgejoRequestBodyLimit)
	}
	if call.Accept != "" && call.Accept != "application/json" && call.Accept != "text/plain" {
		return fmt.Errorf("dispatch broker: forgejo accept %q is not allowed", call.Accept)
	}
	return validateNativeForgejoQuery(call.Query)
}

func validateNativeForgejoQuery(query map[string][]string) error {
	allowed := map[string]bool{
		"direction": true,
		"limit":     true,
		"page":      true,
		"sort":      true,
		"state":     true,
		"style":     true,
		"type":      true,
	}
	for key, values := range query {
		if !allowed[key] {
			return fmt.Errorf("dispatch broker: forgejo query key %q is not allowed", key)
		}
		if len(values) > 4 {
			return fmt.Errorf("dispatch broker: forgejo query key %q has too many values", key)
		}
	}
	return nil
}

func nativeForgejoScope(segments []string) (owner, repo string, ok bool) {
	switch {
	case len(segments) >= 3 && segments[0] == "repos":
		return strings.TrimSpace(segments[1]), strings.TrimSpace(segments[2]), true
	case len(segments) == 3 && (segments[0] == "orgs" || segments[0] == "users") && segments[2] == "repos":
		return strings.TrimSpace(segments[1]), "", true
	default:
		return "", "", false
	}
}

type nativeForgejoRoute struct {
	method string
	tail   []string
}

const (
	nativeForgejoAny     = "*"
	nativeForgejoDecimal = "#"
)

var nativeForgejoRepoRoutes = []nativeForgejoRoute{
	{http.MethodGet, nil},
	{http.MethodGet, []string{"issues"}},
	{http.MethodGet, []string{"pulls"}},
	{http.MethodGet, []string{"issues", nativeForgejoDecimal}},
	{http.MethodGet, []string{"pulls", nativeForgejoDecimal}},
	{http.MethodGet, []string{"issues", nativeForgejoDecimal, "comments"}},
	{http.MethodGet, []string{"pulls", nativeForgejoDecimal, "comments"}},
	{http.MethodGet, []string{"pulls", nativeForgejoDecimal, "merge"}},
	{http.MethodGet, []string{"branches", nativeForgejoAny}},
	{http.MethodGet, []string{"commits", nativeForgejoAny}},
	{http.MethodGet, []string{"commits", nativeForgejoAny, "status"}},
	{http.MethodGet, []string{"actions", "runs"}},
	{http.MethodGet, []string{"actions", "runs", nativeForgejoDecimal}},
	{http.MethodGet, []string{"actions", "runs", nativeForgejoDecimal, "jobs", nativeForgejoDecimal, "attempt", nativeForgejoDecimal, "logs"}},
	{http.MethodPost, []string{"issues"}},
	{http.MethodPost, []string{"pulls"}},
	{http.MethodPost, []string{"issues", nativeForgejoDecimal, "comments"}},
	{http.MethodPost, []string{"issues", nativeForgejoDecimal, "labels"}},
	{http.MethodPost, []string{"pulls", nativeForgejoDecimal, "merge"}},
	{http.MethodPost, []string{"pulls", nativeForgejoDecimal, "update"}},
	{http.MethodPost, []string{"actions", "runs", nativeForgejoDecimal, "rerun"}},
	{http.MethodPatch, []string{"issues", nativeForgejoDecimal}},
	{http.MethodPatch, []string{"pulls", nativeForgejoDecimal}},
	{http.MethodDelete, []string{"issues", "comments", nativeForgejoDecimal}},
}

func nativeForgejoRouteAllowed(method string, segments []string) bool {
	s := segments
	if len(s) < 3 {
		return false
	}
	if (s[0] == "orgs" || s[0] == "users") && len(s) == 3 && s[2] == "repos" {
		return method == http.MethodGet
	}
	if s[0] != "repos" || s[1] == "" || s[2] == "" {
		return false
	}
	for _, route := range nativeForgejoRepoRoutes {
		if route.method == method && nativeForgejoTailMatches(route.tail, s[3:]) {
			return true
		}
	}
	return false
}

func nativeForgejoTailMatches(pattern, tail []string) bool {
	if len(pattern) != len(tail) {
		return false
	}
	for i, want := range pattern {
		switch want {
		case nativeForgejoAny:
			if tail[i] == "" {
				return false
			}
		case nativeForgejoDecimal:
			if !positiveDecimal(tail[i]) {
				return false
			}
		default:
			if tail[i] != want {
				return false
			}
		}
	}
	return true
}

func positiveDecimal(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0
}

func nativeForgejoBrokerEnabled() bool {
	return os.Getenv("WARD_READONLY") == "1" &&
		strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)) != ""
}

func nativeForgejoHTTPClient() *http.Client {
	return &http.Client{
		Timeout: forgejoHTTPTimeout,
		Transport: nativeForgejoTransport{
			addr:      strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)),
			token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
			requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
			role:      prWorkflowRole(),
		},
	}
}
