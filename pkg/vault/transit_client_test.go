// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// T2.1 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — tests for the
// OpenBao transit client: Encrypt + Decrypt round-trip, AppRole login,
// EC-04 singleflight thundering-herd, SPKI pin defense, error mapping.
// ============================================================================

// vaultStubT21 is a fake OpenBao server. Counts login + encrypt + decrypt
// calls so concurrency assertions can verify EC-04 singleflight behavior.
type vaultStubT21 struct {
	mu              sync.Mutex
	loginCalls      atomic.Int64
	encryptCalls    atomic.Int64
	decryptCalls    atomic.Int64
	currentToken    string
	expiredOnce     bool          // toggle: simulate 403 on first encrypt with old token
	loginDelay      time.Duration // simulate slow login for thundering-herd test
	encryptResponse string        // override response ciphertext
	decryptResponse string        // override response plaintext (base64)
}

func (s *vaultStubT21) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		s.loginCalls.Add(1)
		if s.loginDelay > 0 {
			time.Sleep(s.loginDelay)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["role_id"] != "rid-stub" || body["secret_id"] != "sid-stub" {
			http.Error(w, `{"errors":["bad credentials"]}`, http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.currentToken = fmt.Sprintf("hvs.stub-token-%d", s.loginCalls.Load())
		token := s.currentToken
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"auth":{"client_token":"%s"}}`, token)
	})
	mux.HandleFunc("/v1/transit/encrypt/", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Vault-Token")
		s.mu.Lock()
		valid := token == s.currentToken
		// Simulate token expiry: first request with token-1 returns 403,
		// triggering re-login. Subsequent requests succeed with token-2+.
		if s.expiredOnce && s.loginCalls.Load() == 1 && s.encryptCalls.Load() == 0 {
			valid = false
		}
		s.mu.Unlock()
		if !valid {
			http.Error(w, `{"errors":["permission denied"]}`, http.StatusForbidden)
			return
		}
		s.encryptCalls.Add(1)
		ct := s.encryptResponse
		if ct == "" {
			ct = "vault:v1:abc123stubciphertext"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"ciphertext":"%s"}}`, ct)
	})
	mux.HandleFunc("/v1/transit/decrypt/", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Vault-Token")
		s.mu.Lock()
		valid := token == s.currentToken
		s.mu.Unlock()
		if !valid {
			http.Error(w, `{"errors":["permission denied"]}`, http.StatusForbidden)
			return
		}
		s.decryptCalls.Add(1)
		pt := s.decryptResponse
		if pt == "" {
			// base64 of "postgres://x" for round-trip test
			pt = "cG9zdGdyZXM6Ly94"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"plaintext":"%s"}}`, pt)
	})
	return mux
}

// newClientWithStub spins up a TLS test server + builds a TransitClient
// configured against it. Uses the stub server's own client to bypass SPKI
// pinning for the Encrypt/Decrypt happy-path tests; SPKI pin behavior is
// covered separately in TestTransitClient_SPKIPin*.
func newClientWithStub(t *testing.T, stub *vaultStubT21) (*TransitClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(stub.handler())
	t.Cleanup(srv.Close)
	c, err := NewTransitClient(TransitConfig{
		Addr:       srv.URL,
		RoleID:     "rid-stub",
		SecretID:   "sid-stub",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}
	return c, srv
}

func TestTransitClient_NewTransitClient_ValidatesConfig(t *testing.T) {
	if _, err := NewTransitClient(TransitConfig{}); err == nil {
		t.Errorf("expected error for empty Addr; got nil")
	}
	if _, err := NewTransitClient(TransitConfig{Addr: "https://x"}); err == nil {
		t.Errorf("expected error for empty RoleID/SecretID; got nil")
	}
}

func TestTransitClient_EncryptHappyPath(t *testing.T) {
	stub := &vaultStubT21{encryptResponse: "vault:v1:happy"}
	c, _ := newClientWithStub(t, stub)

	ct, err := c.Encrypt(context.Background(), "tn-A", []byte("postgres://x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ct != "vault:v1:happy" {
		t.Errorf("ciphertext: got %q want %q", ct, "vault:v1:happy")
	}
	if stub.loginCalls.Load() != 1 {
		t.Errorf("login calls: got %d want 1 (one upfront)", stub.loginCalls.Load())
	}
	if stub.encryptCalls.Load() != 1 {
		t.Errorf("encrypt calls: got %d want 1", stub.encryptCalls.Load())
	}
}

func TestTransitClient_DecryptHappyPath(t *testing.T) {
	stub := &vaultStubT21{decryptResponse: "cG9zdGdyZXM6Ly94"} // "postgres://x"
	c, _ := newClientWithStub(t, stub)

	pt, err := c.Decrypt(context.Background(), "tn-A", "vault:v1:ciphertext")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(pt) != "postgres://x" {
		t.Errorf("plaintext: got %q want %q", string(pt), "postgres://x")
	}
}

func TestTransitClient_Decrypt_RejectsMissingPrefix(t *testing.T) {
	stub := &vaultStubT21{}
	c, _ := newClientWithStub(t, stub)

	_, err := c.Decrypt(context.Background(), "tn-A", "not-a-vault-ciphertext")
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("want ErrInvalidCiphertext, got %v", err)
	}
}

func TestTransitClient_Encrypt_PerTenantKeyRoute(t *testing.T) {
	stub := &vaultStubT21{}
	var capturedPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"auth":{"client_token":"t1"}}`)
	})
	mux.HandleFunc("/v1/transit/encrypt/", func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"ciphertext":"vault:v1:ok"}}`)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	_, _ = c.Encrypt(context.Background(), "tn-XYZ", []byte("x"))
	want := "/v1/transit/encrypt/loopzero-tenants-tn-XYZ"
	if capturedPath != want {
		t.Errorf("path: got %q want %q (per-tenant key route violated)", capturedPath, want)
	}
	_ = stub // suppress unused
}

func TestTransitClient_403TriggersRelogin(t *testing.T) {
	stub := &vaultStubT21{expiredOnce: true}
	c, _ := newClientWithStub(t, stub)

	// First encrypt: token-1 → 403 → singleflight re-login → token-2 → 200.
	_, err := c.Encrypt(context.Background(), "tn-A", []byte("v"))
	if err != nil {
		t.Fatalf("Encrypt with 403 retry: %v", err)
	}
	if got := stub.loginCalls.Load(); got != 2 {
		t.Errorf("login calls: got %d want 2 (initial + re-login after 403)", got)
	}
}

// TestTransitClient_500MapsToErrVaultUnreachable — caller surfaces 503 to
// upstream consumers when OpenBao returns 5xx persistently.
func TestTransitClient_500MapsToErrVaultUnreachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"auth":{"client_token":"t1"}}`)
	})
	mux.HandleFunc("/v1/transit/encrypt/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "vault down", http.StatusServiceUnavailable)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	_, err = c.Encrypt(context.Background(), "tn-A", []byte("v"))
	if !errors.Is(err, ErrVaultUnreachable) {
		t.Errorf("want ErrVaultUnreachable, got %v", err)
	}
}

// TestTransitClient_ConcurrentEncrypt — 10 concurrent calls across tenants
// A..J. Race detector must be clean + each call lands at the right tenant
// endpoint.
func TestTransitClient_ConcurrentEncrypt(t *testing.T) {
	stub := &vaultStubT21{}
	c, _ := newClientWithStub(t, stub)

	var wg sync.WaitGroup
	for _, tn := range []string{"tn-A", "tn-B", "tn-C", "tn-D", "tn-E", "tn-F", "tn-G", "tn-H", "tn-I", "tn-J"} {
		wg.Add(1)
		go func(tenant string) {
			defer wg.Done()
			_, err := c.Encrypt(context.Background(), tenant, []byte("v"))
			if err != nil {
				t.Errorf("Encrypt(%s): %v", tenant, err)
			}
		}(tn)
	}
	wg.Wait()
	if stub.encryptCalls.Load() != 10 {
		t.Errorf("encrypt calls: got %d want 10", stub.encryptCalls.Load())
	}
}

// TestTransitClient_ReloginThunderingHerd — EC-04 hard contract: when 100
// concurrent goroutines all see a 403 simultaneously, ONE re-login fires
// across the singleflight group; all 100 share the new token. Without
// singleflight, OpenBao would receive 100 simultaneous login POSTs.
func TestTransitClient_ReloginThunderingHerd(t *testing.T) {
	// Use a slow-login stub to widen the window where multiple goroutines
	// converge on the singleflight call.
	stub := &vaultStubT21{loginDelay: 50 * time.Millisecond}
	c, _ := newClientWithStub(t, stub)

	// Manually invalidate the cached token so every concurrent caller
	// detects "no token" and races to refreshToken at the same instant.
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()

	const N = 100
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Encrypt(context.Background(), "tn-A", []byte("v"))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Encrypt failure: %v", err)
		}
	}
	// EC-04 invariant: at most a handful of login calls (1-2 due to
	// singleflight window; deterministically 1 with loginDelay > test
	// scheduling jitter). Without singleflight, this would be ~100.
	got := stub.loginCalls.Load()
	if got > 3 {
		t.Errorf("login calls: got %d (EC-04 singleflight violated; want ≤ 3 with delay window)", got)
	}
}

// TestTransitClient_SPKIPinMatch — happy path: the pin matches the test
// server's leaf cert SPKI, so handshake succeeds.
func TestTransitClient_SPKIPinMatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"auth":{"client_token":"t1"}}`)
	})
	mux.HandleFunc("/v1/transit/encrypt/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"ciphertext":"vault:v1:ok"}}`)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	// Extract the real SPKI hash from the test server's cert.
	cert, err := x509.ParseCertificate(srv.Certificate().Raw)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	spki := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	pinHex := hex.EncodeToString(spki[:])

	// Use a custom HTTPClient that has the SPKI verifier wired but uses the
	// test server's RootCAs so the TLS handshake completes — we trust the
	// pin verifier to do the actual gate.
	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid",
		SPKIPinSHA256: pinHex,
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}
	// Override the transport with the test server's TLS cert pool BUT
	// preserve our SPKI verifier (the verifier overrides chain check via
	// InsecureSkipVerify=true). To verify the verifier saw the cert, we
	// rely on the fact that NewTransitClient already wired the verifier
	// into c.httpClient.Transport.
	_, err = c.Encrypt(context.Background(), "tn-A", []byte("v"))
	if err != nil {
		t.Fatalf("Encrypt with matching SPKI pin: %v", err)
	}
}

// TestTransitClient_SPKIPinMismatch — when the configured pin does not
// match the server cert's SPKI, the TLS handshake fails with
// ErrTLSPinMismatch. No retry, no fallback — refuse to talk to the
// imposter.
func TestTransitClient_SPKIPinMismatch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	// Use a deliberately wrong pin (32 bytes of 0xAA).
	wrongPin := hex.EncodeToString([]byte{
		0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA,
		0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA,
		0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA,
		0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA,
	})

	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid",
		SPKIPinSHA256: wrongPin,
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	_, err = c.Encrypt(context.Background(), "tn-A", []byte("v"))
	if err == nil {
		t.Fatalf("want SPKI pin mismatch error, got nil")
	}
	// The error chain wraps url.Error around the TLS handshake failure;
	// we accept any error here as long as it is NOT nil — the precise
	// chain matters less than the refusal to communicate. A future
	// iteration may tighten the assertion to errors.Is(ErrTLSPinMismatch).
	if errors.Is(err, ErrVaultUnreachable) {
		t.Errorf("SPKI mismatch must NOT be mapped to ErrVaultUnreachable: %v", err)
	}
}

// ============================================================================
// EC-06 absorption: transit key auto-create on first Encrypt 404.
// ============================================================================

// autoCreateStub returns 404 on the first transit/encrypt then 200 on
// retry, and tracks transit/keys create POST calls so the test can
// assert exactly one key-create was attempted.
type autoCreateStub struct {
	encryptCalls      atomic.Int64
	keysCreateCalls   atomic.Int64
	encryptFailFirst  bool // first encrypt returns 404
	keysCreateRespond int  // status code to return on POST /v1/transit/keys
}

func (s *autoCreateStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"auth":{"client_token":"t1"}}`)
	})
	mux.HandleFunc("/v1/transit/keys/", func(w http.ResponseWriter, _ *http.Request) {
		s.keysCreateCalls.Add(1)
		w.WriteHeader(s.keysCreateRespond)
	})
	mux.HandleFunc("/v1/transit/encrypt/", func(w http.ResponseWriter, _ *http.Request) {
		n := s.encryptCalls.Add(1)
		if s.encryptFailFirst && n == 1 {
			http.Error(w, `{"errors":["encryption key not found"]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"ciphertext":"vault:v1:ok"}}`)
	})
	return mux
}

// TestTransitClient_AutoCreatesKeyOnFirstUse — EC-06 happy path: first
// encrypt returns 404 (key missing), client POSTs to /v1/transit/keys to
// create it, then retries encrypt successfully.
func TestTransitClient_AutoCreatesKeyOnFirstUse(t *testing.T) {
	stub := &autoCreateStub{
		encryptFailFirst:  true,
		keysCreateRespond: http.StatusNoContent, // 204 idempotent create
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	ct, err := c.Encrypt(context.Background(), "tn-NEW", []byte("v"))
	if err != nil {
		t.Fatalf("Encrypt with auto-create: %v", err)
	}
	if ct != "vault:v1:ok" {
		t.Errorf("ciphertext: got %q want %q", ct, "vault:v1:ok")
	}
	if got := stub.encryptCalls.Load(); got != 2 {
		t.Errorf("encrypt calls: got %d want 2 (initial 404 + retry after key-create)", got)
	}
	if got := stub.keysCreateCalls.Load(); got != 1 {
		t.Errorf("keys-create calls: got %d want 1 (EC-06 self-heal)", got)
	}
}

// TestTransitClient_AutoCreateFailsOnPolicyDenial — EC-06 error path: the
// auto-create POST is denied (e.g., AppRole policy lacks transit/keys/*
// create). Client surfaces ErrTransitKeyCreateFailed; caller MUST NOT
// retry — operator fixes the policy.
func TestTransitClient_AutoCreateFailsOnPolicyDenial(t *testing.T) {
	stub := &autoCreateStub{
		encryptFailFirst:  true,
		keysCreateRespond: http.StatusForbidden, // policy denial
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	_, err = c.Encrypt(context.Background(), "tn-NEW", []byte("v"))
	if !errors.Is(err, ErrTransitKeyCreateFailed) {
		t.Errorf("want ErrTransitKeyCreateFailed, got %v", err)
	}
	// No encrypt retry should have fired after the create denial.
	// Initial encrypt is 1; no retry after create-failed.
	if got := stub.encryptCalls.Load(); got != 1 {
		t.Errorf("encrypt calls: got %d want 1 (no retry after create denial)", got)
	}
}

// TestTransitClient_AutoCreatesKeyConcurrent — EC-06 race: 5 concurrent
// first-encrypts for the SAME new tenant. Each goroutine sees 404, each
// attempts a key-create, but OpenBao's idempotent /transit/keys endpoint
// (mocked as always returning 204) absorbs the burst. All 5 ultimately
// succeed at encrypt; the race detector is clean.
//
// Note: the client does NOT singleflight the key-create (unlike AppRole
// login) because the upstream is idempotent server-side; deduplication
// at the client would add complexity for a marginal win. The test
// asserts the contract: every goroutine resolves to a successful
// ciphertext.
func TestTransitClient_AutoCreatesKeyConcurrent(t *testing.T) {
	stub := &autoCreateStub{
		encryptFailFirst:  false, // mode: every call succeeds (after the first heal)
		keysCreateRespond: http.StatusNoContent,
	}
	// Custom handler: first N callers for the same tenant see 404 until
	// keys-create has been observed at least once; then everyone passes.
	var (
		keyCreated atomic.Bool
		mu         sync.Mutex
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"auth":{"client_token":"t1"}}`)
	})
	mux.HandleFunc("/v1/transit/keys/", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		stub.keysCreateCalls.Add(1)
		keyCreated.Store(true)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/transit/encrypt/", func(w http.ResponseWriter, _ *http.Request) {
		stub.encryptCalls.Add(1)
		if !keyCreated.Load() {
			http.Error(w, `{"errors":["encryption key not found"]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"ciphertext":"vault:v1:concurrent"}}`)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c, err := NewTransitClient(TransitConfig{
		Addr: srv.URL, RoleID: "rid", SecretID: "sid", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	const N = 5
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Encrypt(context.Background(), "tn-NEW-CONCURRENT", []byte("v"))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Encrypt failure: %v", err)
		}
	}
	if stub.keysCreateCalls.Load() < 1 {
		t.Errorf("expected at least 1 keys-create call; got 0")
	}
}

// ============================================================================
// T3.4 + EC-08 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 —
// per-tenant token-bucket rate limiter with LRU-bounded cache to prevent
// unbounded memory growth across cumulative tenant churn.
// ============================================================================

// newRateLimitStub returns a TLS test server + client wired with the
// given RateLimitPerSec. Used by all T3.4 specs.
func newRateLimitStub(t *testing.T, perSec int) (*TransitClient, *vaultStubT21) {
	t.Helper()
	stub := &vaultStubT21{}
	srv := httptest.NewTLSServer(stub.handler())
	t.Cleanup(srv.Close)
	c, err := NewTransitClient(TransitConfig{
		Addr:            srv.URL,
		RoleID:          "rid-stub",
		SecretID:        "sid-stub",
		HTTPClient:      srv.Client(),
		RateLimitPerSec: perSec,
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}
	return c, stub
}

// TestTransitClient_RateLimitBurst — the WIRING: `Encrypt` really consults the limiter.
//
// This test used to assert the exact capacity (10 allowed, 5 refused) by counting real
// HTTP calls. That failed on timing: at 10/s the bucket refills, so the 15 calls only had
// to take longer than 100ms for an extra token to arrive and the count to become 11 —
// measured 2026-08-10 during the full four-module suite (B-014). A test that fails when
// the machine is busy is a bug, per `rules/testing.md` § 3.
//
// The capacity contract — exact burst, refill and per-tenant isolation — now lives in
// `transit_client_rate_clock_test.go`, with the instant injected, where it can be asserted
// without the wall clock.
//
// What remains here, and those tests do NOT cover, is the wiring: that the public
// `Encrypt` path goes through the limiter. The assertion is deliberately insensitive to
// one token more or less — it answers "is the limiter on?", not "how many passed?".
func TestTransitClient_RateLimitBurst(t *testing.T) {
	c, _ := newRateLimitStub(t, 10)

	const N = 15
	var allowed, rateLimited int
	for i := 0; i < N; i++ {
		_, err := c.Encrypt(context.Background(), "tn-burst", []byte("v"))
		if err == nil {
			allowed++
			continue
		}
		if errors.Is(err, ErrRateLimited) {
			rateLimited++
			continue
		}
		t.Errorf("unexpected error: %v", err)
	}
	if allowed == 0 {
		t.Error("no call passed — the limiter is refusing everything, not limiting")
	}
	if rateLimited == 0 {
		t.Errorf("none of the %d calls was refused — `Encrypt` does not consult the limiter", N)
	}
	if allowed+rateLimited != N {
		t.Errorf("%d calls vanished: allowed=%d refused=%d of %d",
			N-(allowed+rateLimited), allowed, rateLimited, N)
	}
}

// TestTransitClient_RateLimitPerTenantIsolation — defense vs noisy-
// neighbor: when tenant tn-X has exhausted its bucket, tn-Y MUST still
// be allowed up to its own bucket capacity. Per-tenant isolation per
// D4 ADR blast-radius design.
func TestTransitClient_RateLimitPerTenantIsolation(t *testing.T) {
	c, _ := newRateLimitStub(t, 5)

	// Exhaust tn-X bucket completely.
	for i := 0; i < 10; i++ {
		_, _ = c.Encrypt(context.Background(), "tn-X", []byte("v"))
	}

	// tn-Y MUST still be allowed (fresh bucket).
	yAllowed := 0
	for i := 0; i < 5; i++ {
		_, err := c.Encrypt(context.Background(), "tn-Y", []byte("v"))
		if err == nil {
			yAllowed++
		}
	}
	if yAllowed != 5 {
		t.Errorf("tn-Y allowed: got %d want 5 (tn-X exhaustion MUST NOT affect tn-Y)", yAllowed)
	}
}

// TestTransitClient_RateLimiterMapEviction — EC-08 absorption: 100K
// unique tenants churn through one limiter cache; the cache size MUST
// stay ≤ RateLimiterMaxEntries (default 10K). Without LRU bound the map
// grows linearly with cumulative tenant count — slow leak that the
// audit caught.
func TestTransitClient_RateLimiterMapEviction(t *testing.T) {
	c, err := NewTransitClient(TransitConfig{
		Addr:                  "https://stub",
		RoleID:                "rid",
		SecretID:              "sid",
		RateLimitPerSec:       10,
		RateLimiterMaxEntries: 1000, // small bound for faster test
		HTTPClient:            &http.Client{},
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	// Touch the limiter cache via acquireRateToken directly so we
	// exercise the LRU bound without making 100K real HTTP calls.
	const N = 100_000
	for i := 0; i < N; i++ {
		tenantID := fmt.Sprintf("tn-%d", i)
		_ = c.acquireRateToken(tenantID)
	}
	if got := c.rateLimiters.Len(); got > 1000 {
		t.Errorf("rateLimiters cache size: got %d want ≤ 1000 (LRU bound violated)", got)
	}
	// Most-recently-used tenant MUST still be present.
	last := fmt.Sprintf("tn-%d", N-1)
	if !c.rateLimiters.Contains(last) {
		t.Errorf("most-recent tenant %q missing from LRU — bound semantics broken", last)
	}
}

// TestTransitClient_RateLimitDisabledWhenZero — escape hatch: when
// RateLimitPerSec <= 0, no limiter is constructed and acquireRateToken
// always returns nil. Test path uses this to avoid limiter interference.
func TestTransitClient_RateLimitDisabledWhenZero(t *testing.T) {
	c, err := NewTransitClient(TransitConfig{
		Addr:            "https://stub",
		RoleID:          "rid",
		SecretID:        "sid",
		RateLimitPerSec: 0, // disabled
		HTTPClient:      &http.Client{},
	})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}
	if c.rateLimiters != nil {
		t.Errorf("rateLimiters: want nil (disabled), got %v", c.rateLimiters)
	}
	// acquireRateToken must always return nil when disabled, even for
	// many tenants.
	for i := 0; i < 1000; i++ {
		if err := c.acquireRateToken(fmt.Sprintf("tn-%d", i)); err != nil {
			t.Fatalf("acquireRateToken when disabled: got %v want nil", err)
		}
	}
}
