// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// generateTestRSAKey generates a 2048-bit RSA private key in PEM format for tests.
func generateTestRSAKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return string(pemBytes), key
}

// newTestAppClient creates an AppClient pointing at a test server.
// It replaces the httpClient so all requests go to the test server.
func newTestAppClient(t *testing.T, server *httptest.Server) *AppClient {
	t.Helper()
	pemStr, key := generateTestRSAKey(t)
	_ = pemStr

	return &AppClient{
		appID:      12345,
		privateKey: key,
		httpClient: server.Client(),
		tokenCache: make(map[int64]*cachedToken),
	}
}

func TestNewAppClientUsesTheGivenAPIBase(t *testing.T) {
	pemStr, _ := generateTestRSAKey(t)

	c, err := NewAppClient(42, pemStr, "https://github.acme.corp/api/v3")

	if err != nil {
		t.Fatalf("NewAppClient: %v", err)
	}
	if got := c.apiBase(); got != "https://github.acme.corp/api/v3" {
		t.Errorf("apiBase() = %q, want the configured GitHub Enterprise root", got)
	}
}

// TestAppClient_ApiBase_FallsBackOnZeroValue — defense-in-depth so
// struct-literal construction without NewAppClient (test fixtures,
// future shape changes) doesn't produce empty URL formatting.
func TestAppClient_ApiBase_FallsBackOnZeroValue(t *testing.T) {
	c := &AppClient{} // zero value — no baseURL set
	if got := c.apiBase(); got != DefaultGitHubAPIBase {
		t.Errorf("apiBase() on zero value must fall back: got %q", got)
	}
}

func TestNewAppClient_ValidCredentials(t *testing.T) {
	// Arrange
	pemStr, _ := generateTestRSAKey(t)

	// Act
	client, err := NewAppClient(42, pemStr, "")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.appID != 42 {
		t.Errorf("expected appID=42, got %d", client.appID)
	}
}

func TestNewAppClient_ZeroAppID_ReturnsNil(t *testing.T) {
	// Arrange
	pemStr, _ := generateTestRSAKey(t)

	// Act
	client, err := NewAppClient(0, pemStr, "")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client != nil {
		t.Fatal("expected nil client when appID is 0")
	}
}

func TestNewAppClient_EmptyPEM_ReturnsNil(t *testing.T) {
	// Act
	client, err := NewAppClient(42, "", "")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client != nil {
		t.Fatal("expected nil client when PEM is empty")
	}
}

func TestNewAppClient_InvalidPEM_ReturnsError(t *testing.T) {
	// Act
	client, err := NewAppClient(42, "not-a-valid-pem", "")

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "failed to decode PEM") {
		t.Errorf("expected PEM decode error, got: %v", err)
	}
}

func TestNewAppClient_InvalidRSAKey_ReturnsError(t *testing.T) {
	// Arrange — valid PEM block but garbage bytes (not a valid RSA key)
	badPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: []byte("not-a-real-key-just-garbage-bytes"),
	})

	// Act
	client, err := NewAppClient(42, string(badPEM), "")

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid RSA key, got nil")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "parse private key") {
		t.Errorf("expected parse key error, got: %v", err)
	}
}

func TestGetInstallationToken_Success(t *testing.T) {
	// Arrange
	expiresAt := time.Now().Add(1 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/app/installations/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_test_token_123",
			"expires_at": expiresAt.Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	// Override the hardcoded githubAPIBase by injecting a pre-cached token won't work,
	// so we use a transport that redirects.
	client.httpClient = redirectingClient(server.URL)

	// Act
	token, err := client.GetInstallationToken(context.Background(), 99)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token != "ghs_test_token_123" {
		t.Errorf("expected token 'ghs_test_token_123', got %q", token)
	}
}

func TestGetInstallationToken_CachesToken(t *testing.T) {
	// Arrange
	var callCount int32
	expiresAt := time.Now().Add(1 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_cached_token",
			"expires_at": expiresAt.Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Act — call twice
	token1, err1 := client.GetInstallationToken(context.Background(), 99)
	token2, err2 := client.GetInstallationToken(context.Background(), 99)

	// Assert
	if err1 != nil {
		t.Fatalf("first call error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second call error: %v", err2)
	}
	if token1 != token2 {
		t.Errorf("tokens should match: %q vs %q", token1, token2)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", atomic.LoadInt32(&callCount))
	}
}

func TestGetInstallationToken_RefreshesExpired(t *testing.T) {
	// Arrange
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_refreshed_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
		_ = n
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Seed cache with an expired token (expired 10 minutes ago)
	client.tokenCache[99] = &cachedToken{
		Token:     "ghs_old_expired_token",
		ExpiresAt: time.Now().Add(-10 * time.Minute),
	}

	// Act
	token, err := client.GetInstallationToken(context.Background(), 99)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token != "ghs_refreshed_token" {
		t.Errorf("expected refreshed token, got %q", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call for refresh, got %d", atomic.LoadInt32(&callCount))
	}
}

func TestGetInstallationToken_APIError(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Act
	_, err := client.GetInstallationToken(context.Background(), 99)

	// Assert
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("expected status 401 in error, got: %v", err)
	}
}

func TestDownloadSource_Returns200(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fake-tarball-data"))
			return
		}
		// Installation token endpoint
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_dl_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Act
	reader, err := client.DownloadSource(context.Background(), 1, "owner", "repo", "abc123")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer func() { _ = reader.Close() }()

	data, _ := io.ReadAll(reader)
	if string(data) != "fake-tarball-data" {
		t.Errorf("expected 'fake-tarball-data', got %q", string(data))
	}
}

func TestDownloadSource_Returns404(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		// Installation token endpoint
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_dl_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Act
	_, err := client.DownloadSource(context.Background(), 1, "owner", "repo", "badsha")

	// Assert
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("expected status 404 in error, got: %v", err)
	}
}

func TestSetCommitStatus_Success(t *testing.T) {
	// Arrange
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/statuses/") {
			body, _ := io.ReadAll(r.Body)
			receivedBody = string(body)
			w.WriteHeader(http.StatusCreated)
			return
		}
		// Installation token endpoint
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_status_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Act
	err := client.SetCommitStatus(context.Background(), 1, "owner", "repo", "sha123", CommitStatus{
		State:       "success",
		Description: "Deployed",
		Context:     "loopruntime/deploy",
	})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(receivedBody, `"success"`) {
		t.Errorf("expected 'success' in body, got: %s", receivedBody)
	}
	if !strings.Contains(receivedBody, `"loopruntime/deploy"`) {
		t.Errorf("expected 'loopruntime/deploy' in body, got: %s", receivedBody)
	}
}

func TestSetCommitStatus_APIError(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/statuses/") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)

	// Act
	err := client.SetCommitStatus(context.Background(), 1, "owner", "repo", "sha123", CommitStatus{
		State: "success",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for 422 response, got nil")
	}
	if !strings.Contains(err.Error(), "status 422") {
		t.Errorf("expected status 422 in error, got: %v", err)
	}
}

func TestSetCommitStatusSendsTheDescriptionAsValidJSON(t *testing.T) {
	// Arrange: a description with a control byte, which Go quoting (%q)
	// renders as \a — an escape JSON does not have, so GitHub answers 422.
	const description = "build \a failed"
	var got CommitStatus
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/statuses/") {
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				State, Description, Context string
				TargetURL                   string `json:"target_url"`
			}
			decodeErr = json.Unmarshal(raw, &body)
			got = CommitStatus{State: body.State, TargetURL: body.TargetURL, Description: body.Description, Context: body.Context}
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_status_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()
	client := newTestAppClient(t, server)
	client.httpClient = redirectingClient(server.URL)
	want := CommitStatus{State: "failure", TargetURL: "https://x.example", Description: description, Context: "loopruntime/deploy"}

	// Act
	err := client.SetCommitStatus(context.Background(), 1, "owner", "repo", "sha123", want)

	// Assert
	if err != nil {
		t.Fatalf("SetCommitStatus: %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("status body is not valid JSON: %v", decodeErr)
	}
	if got != want {
		t.Errorf("status did not round-trip: got %+v, want %+v", got, want)
	}
}

func TestGenerateJWT_ValidSignature(t *testing.T) {
	// Arrange
	_, key := generateTestRSAKey(t)
	client := &AppClient{
		appID:      12345,
		privateKey: key,
	}

	// Act
	jwtStr, err := client.generateJWT()

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if jwtStr == "" {
		t.Fatal("expected non-empty JWT string")
	}
	// JWT has 3 parts separated by dots
	parts := strings.Split(jwtStr, ".")
	if len(parts) != 3 {
		t.Errorf("expected 3 JWT parts, got %d", len(parts))
	}
}

// redirectingClient creates an http.Client that rewrites all requests
// from https://api.github.com to the test server URL.
func redirectingClient(testServerURL string) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{
			target:    testServerURL,
			transport: http.DefaultTransport,
		},
		Timeout: 5 * time.Second,
	}
}

// redirectTransport intercepts requests to api.github.com and sends them
// to a local test server instead.
type redirectTransport struct {
	target    string
	transport http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point to our test server
	if strings.Contains(req.URL.Host, "api.github.com") {
		req.URL.Scheme = "http"
		// Parse the test server URL to get host
		req.URL.Host = strings.TrimPrefix(t.target, "http://")
	}
	return t.transport.RoundTrip(req)
}
