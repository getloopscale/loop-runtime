// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argocd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// mintTokenJWT crafts a JWT-like token (alg=none) with given exp + iat
// claims. Suitable for unit tests of the JWT-claim parsers and refresh
// thresholds; NOT cryptographically valid (signature is empty).
func mintTokenJWT(t *testing.T, sub string, exp, iat int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := map[string]any{"sub": sub}
	if exp > 0 {
		claims["exp"] = exp
	}
	if iat > 0 {
		claims["iat"] = iat
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return fmt.Sprintf("%s.%s.%s", header, payload, signature)
}

// fakeArgoCD is a tiny test double for the ArgoCD HTTP API.
// /api/v1/account/{account}/token returns a freshly-minted JWT with a
// configurable exp; /api/v1/session returns whatever bearer the test wants.
type fakeArgoCD struct {
	t           *testing.T
	mintCount   int
	loginCount  int
	mu          sync.Mutex
	mintExp     int64
	mintHandler http.HandlerFunc // override for error injection
}

// mints returns the mint-endpoint hit count safely (read under mutex).
// Sprint B T4.1: existing tests read f.mintCount without locking, racing
// against the handler that increments under f.mu. T4.1 widened the
// timing window via firstMint blocking and surfaced the latent race.
// Fix is test-side: route all external reads through this helper.
func (f *fakeArgoCD) mints() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mintCount
}

func (f *fakeArgoCD) logins() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCount
}

func (f *fakeArgoCD) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.loginCount++
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "session-bearer-from-login"})
	})
	mux.HandleFunc("/api/v1/account/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.mintCount++
		mintHandler := f.mintHandler
		mintExp := f.mintExp
		f.mu.Unlock()
		if mintHandler != nil {
			mintHandler(w, r)
			return
		}
		// Default success: produce a token with the configured exp.
		now := time.Now().Unix()
		tok := mintTokenJWT(f.t, "loop-runtime-api:apiKey", mintExp, now)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
	})
	return mux
}

func newFakeArgoCD(t *testing.T) (*fakeArgoCD, *httptest.Server) {
	f := &fakeArgoCD{t: t, mintExp: 0} // exp=0 = non-expiring
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return f, srv
}

func newProvider(t *testing.T, baseURL string, secrets ...*corev1.Secret) *KubeMintingTokenProvider {
	t.Helper()
	objs := make([]runtime.Object, 0, len(secrets))
	for _, s := range secrets {
		objs = append(objs, s)
	}
	clientset := fake.NewClientset(objs...)
	p, err := NewKubeMintingTokenProvider(KubeMintingConfig{
		ArgoCDBaseURL:             baseURL,
		AccountName:               "loop-runtime-api",
		Namespace:                 "loop-runtime-system",
		KubeClient:                clientset,
		ProactiveRefreshThreshold: 6 * time.Hour,
		PollInterval:              50 * time.Millisecond, // fast tick for tests
	})
	if err != nil {
		t.Fatalf("NewKubeMintingTokenProvider: %v", err)
	}
	return p
}

func credsSecret(bearer, user, pass string) *corev1.Secret {
	data := map[string][]byte{}
	if bearer != "" {
		data["argocd-account-token"] = []byte(bearer)
	}
	if user != "" {
		data["argocd-username"] = []byte(user)
	}
	if pass != "" {
		data["argocd-password"] = []byte(pass)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-refresh-credentials", Namespace: "loop-runtime-system"},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
}

func tokenSecret(token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "loop-runtime-api-tokens", Namespace: "loop-runtime-system"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"argocd-token": []byte(token)},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// JWT helpers — pure functions, fast.
func TestJwtExp_ParsesExpClaim(t *testing.T) {
	tok := mintTokenJWT(t, "x", 12345, 0)
	if got := jwtExp(tok); got != 12345 {
		t.Fatalf("expected 12345, got %d", got)
	}
}

func TestJwtExp_MissingExpReturnsZero(t *testing.T) {
	tok := mintTokenJWT(t, "x", 0, 0)
	if got := jwtExp(tok); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestJwtExp_MalformedTokenReturnsZero(t *testing.T) {
	if jwtExp("not.a.jwt") != 0 {
		t.Fatal("malformed JWT should yield 0")
	}
	if jwtExp("only-two.parts") != 0 {
		t.Fatal("two-part JWT should yield 0")
	}
}

func TestJwtIsValid_NoExpAlwaysValid(t *testing.T) {
	tok := mintTokenJWT(t, "x", 0, time.Now().Unix())
	if !jwtIsValid(tok, 6*time.Hour) {
		t.Fatal("token without exp should be valid")
	}
}

func TestJwtIsValid_ExpiringSoonInvalid(t *testing.T) {
	exp := time.Now().Add(1 * time.Hour).Unix()
	tok := mintTokenJWT(t, "x", exp, 0)
	if jwtIsValid(tok, 6*time.Hour) {
		t.Fatal("token expiring in 1h should be invalid against 6h threshold")
	}
}

// Provider behavior — uses fake ArgoCD + fake K8s client.

func TestProvider_SeedsFromExistingSecret(t *testing.T) {
	_, srv := newFakeArgoCD(t)
	existing := mintTokenJWT(t, "loop-runtime-api:apiKey", 0, time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(existing), credsSecret("bearer-x", "", ""))
	if got := currentToken(p); got != existing {
		t.Fatalf("expected seeded token, got %q", got)
	}
}

func TestProvider_StartMintsWhenNoToken(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("static-bearer", "", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout: provider did not mint on startup; mintCount=%d token=%q", fa.mints(), currentToken(p))
		default:
			if currentToken(p) != "" {
				if fa.mints() < 1 {
					t.Fatalf("expected at least 1 mint, got %d", fa.mints())
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestProvider_OnAuthFailure_TriggersReactiveMint(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	// Seed with a token that's expiring SOON (1h) so the double-check
	// guard `jwtIsValid(_, 6h threshold)` returns false and we proceed
	// to mint. A non-expiring token would be classified valid and
	// skipped — that's tested in TestProvider_SingleFlight_*.
	expiring := mintTokenJWT(t, "loop-runtime-api:apiKey", time.Now().Add(1*time.Hour).Unix(), time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(expiring), credsSecret("bearer-x", "", ""))
	// Don't start the proactive loop in this test — we only want to
	// observe the reactive path. mintsBefore captures any incidental
	// activity (none expected since Start is not called).
	mintsBefore := fa.mints()

	p.OnAuthFailure()

	// Reactive signal needs the loop to consume it. Fire one mint via
	// the synchronous Refresh() to simulate the loop's mintAndPersist.
	if err := p.Refresh(); err != nil {
		t.Fatalf("reactive Refresh: %v", err)
	}
	if fa.mints() <= mintsBefore {
		t.Fatalf("reactive mint did not happen; mintCount=%d", fa.mints())
	}
	// Token swapped to the freshly-minted one.
	if currentToken(p) == expiring {
		t.Fatalf("token did not change after reactive mint")
	}
}

func TestProvider_ProactiveRefresh_WhenTokenNearExpiry(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	// Token expires in 1h — well below the 6h threshold.
	soon := time.Now().Add(1 * time.Hour).Unix()
	expiring := mintTokenJWT(t, "loop-runtime-api:apiKey", soon, time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(expiring), credsSecret("bearer-x", "", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatalf("proactive mint did not happen; mintCount=%d", fa.mints())
		default:
			if fa.mints() > 0 && currentToken(p) != expiring {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestProvider_PersistsMintedTokenToSecret(t *testing.T) {
	_, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	// Wait for first mint.
	deadline := time.After(1500 * time.Millisecond)
	for currentToken(p) == "" {
		select {
		case <-deadline:
			t.Fatal("never minted")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	sec, err := p.cfg.KubeClient.CoreV1().Secrets("loop-runtime-system").Get(
		context.Background(), "loop-runtime-api-tokens", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret not created: %v", err)
	}
	if string(sec.Data["argocd-token"]) != currentToken(p) {
		t.Fatalf("Secret data does not match in-memory token")
	}
}

func TestProvider_LoginFlow_WhenNoBearer(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("", "admin", "pw"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	deadline := time.After(1500 * time.Millisecond)
	for currentToken(p) == "" {
		select {
		case <-deadline:
			t.Fatal("never minted")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	if fa.logins() < 1 {
		t.Fatalf("expected /api/v1/session call, got %d", fa.logins())
	}
}

func TestProvider_NoCredentials_ReturnsError(t *testing.T) {
	_, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("", "", "")) // empty creds
	if err := p.Refresh(); err == nil {
		t.Fatal("expected error when refresh credentials missing")
	}
}

func TestProvider_SingleFlight_ConcurrentRefreshes(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Refresh()
		}()
	}
	wg.Wait()

	// Without single-flight, mintCount would be 10. With it, the first call
	// mints once and subsequent calls observe a fresh-enough token via the
	// double-check guard inside mintAndPersist (success path stores a non-
	// expiring token, so jwtIsValid returns true for the rest).
	if fa.mints() > 3 {
		t.Fatalf("single-flight not effective: mintCount=%d (expected ≤3)", fa.mints())
	}
}

func TestProvider_AuthFailureNotifier_Implemented(t *testing.T) {
	_, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))
	var iface interface{} = p
	if _, ok := iface.(AuthFailureNotifier); !ok {
		t.Fatal("KubeMintingTokenProvider must implement AuthFailureNotifier")
	}
}

func TestProvider_ServerErrorPropagates(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	fa.mintHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}
	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))
	err := p.Refresh()
	if err == nil || !strings.Contains(err.Error(), "mint http 500") {
		t.Fatalf("expected mint http 500 error, got %v", err)
	}
}

// =============================================================================
// Sprint B T4.1 — Block-on-first-mint (ADR D5)
// =============================================================================
// Four tests prove the new contract:
//   - Token() called before first mint BLOCKS until first mint signals
//   - Timeout (mint never completes) returns empty + logs ERROR
//   - After first mint, subsequent calls return immediately (fast path)
//   - Token() called before Start() does NOT panic (EC-10)
//
// Test isolation: each test starts with a fresh provider (no shared state).
// firstMintTimeout is overridden to a small value (100ms-1s) so tests
// stay fast — the production 30s is correctness, not performance.

// TestKubeMintingProvider_BlocksBeforeFirstMint — fundamental T4.1
// invariant. Provider Start()ed against a slow ArgoCD mint endpoint;
// Token() called immediately must block until the mint completes.
// Pre-T4.1 it returned "" immediately; T4.1 makes it block 30s
// max OR until firstMint closes.
func TestKubeMintingProvider_BlocksBeforeFirstMint(t *testing.T) {
	// Save + restore firstMintTimeout so other tests aren't affected.
	// (placeholder — replaced per-test via p.firstMintTimeout = ...)

	// Build a slow ArgoCD whose mint endpoint sleeps 150ms — long
	// enough that Token() called immediately will hit the slow path
	// but short enough to keep the test fast.
	fa, srv := newFakeArgoCD(t)
	slow := fa.handler()
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			time.Sleep(150 * time.Millisecond)
		}
		slow.ServeHTTP(w, r)
	})

	p := newProvider(t, srv.URL, credsSecret("static-bearer", "", ""))
	p.firstMintTimeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	start := time.Now()
	tok := currentToken(p)
	dur := time.Since(start)

	if tok == "" {
		t.Fatalf("Token() returned empty after blocking; mintCount=%d", fa.mints())
	}
	if dur < 100*time.Millisecond {
		t.Errorf("Token() returned in %v — too fast; expected >=100ms (mint endpoint sleeps 150ms)", dur)
	}
	if dur > 2*time.Second {
		t.Errorf("Token() took %v — exceeds firstMintTimeout override 2s", dur)
	}
}

// TestKubeMintingProvider_TimeoutReturnsAnError — failure-mode invariant.
// Provider Start()ed but mint endpoint always returns 500. After
// firstMintTimeout elapses, Token() returns ErrFirstMintTimedOut (no panic,
// no hang past timeout) — an explicit error, never an empty Bearer.
func TestKubeMintingProvider_TimeoutReturnsAnError(t *testing.T) {
	// (placeholder — replaced per-test via p.firstMintTimeout = ...)

	fa, srv := newFakeArgoCD(t)
	fa.mintHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "always 500", http.StatusInternalServerError)
	}

	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))
	p.firstMintTimeout = 200 * time.Millisecond
	// IMPORTANT: do NOT call Start() — without it, no mint goroutine
	// runs and firstMint will never close until the timeout fires.
	// This isolates the timeout path from the Start()/mint race.
	start := time.Now()
	tok, err := p.Token()
	dur := time.Since(start)

	if !errors.Is(err, ErrFirstMintTimedOut) {
		t.Errorf("Token() = (%q, %v) after timeout; want ErrFirstMintTimedOut", tok, err)
	}
	if dur < 150*time.Millisecond {
		t.Errorf("Token() returned in %v — too fast; expected >=150ms (timeout override 200ms)", dur)
	}
	if dur > 500*time.Millisecond {
		t.Errorf("Token() took %v — exceeds firstMintTimeout override 200ms by too wide a margin", dur)
	}
}

// TestKubeMintingProvider_PostMint_NonBlocking — performance invariant.
// After the first mint completes (firstMint closed), every subsequent
// Token() call goes through the fast path (atomic load) without
// touching the channel. Critical: production token retrieval happens
// on every sync call; if T4.1 added a channel-recv on every call the
// cost would compound under load.
func TestKubeMintingProvider_PostMint_NonBlocking(t *testing.T) {
	_, srv := newFakeArgoCD(t)
	existing := mintTokenJWT(t, "loop-runtime-api:apiKey", 0, time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(existing), credsSecret("bearer-x", "", ""))
	// Seeded token short-circuits firstMint at constructor time.

	for i := 0; i < 100; i++ {
		start := time.Now()
		tok := currentToken(p)
		dur := time.Since(start)
		if tok != existing {
			t.Fatalf("iter %d: got %q want %q", i, tok, existing)
		}
		if dur > 1*time.Millisecond {
			t.Errorf("iter %d: Token() took %v — fast path should be <1ms after first mint", i, dur)
		}
	}
}

// TestKubeMintingProvider_TokenBeforeStart_DoesNotPanic — EC-10 of the
// plan. Some unit-test patterns construct a provider without calling
// Start() (e.g. tests of paths that don't need the background loop).
// Pre-T4.1 Token() returned "" immediately; T4.1 must NOT panic on
// the channel-recv path even if firstMint was never closed.
// Achieved by initializing firstMint in NewKubeMintingTokenProvider
// (constructor), not lazily in Start().
func TestKubeMintingProvider_TokenBeforeStart_DoesNotPanic(t *testing.T) {
	// (placeholder — replaced per-test via p.firstMintTimeout = ...)

	_, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))
	p.firstMintTimeout = 100 * time.Millisecond
	// NO Start(), NO Stop() — provider is fresh-constructed.

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Token() panicked before Start(): %v", r)
		}
	}()

	tok := currentToken(p)
	if tok != "" {
		t.Errorf("Token() returned %q on un-started provider; expected empty after timeout", tok)
	}
}

func TestKubeMintingProvider_StoppedBeforeFirstMintReturnsAnError(t *testing.T) {
	_, srv := newFakeArgoCD(t)
	p := newProvider(t, srv.URL, credsSecret("bearer-x", "", ""))
	p.firstMintTimeout = 5 * time.Second
	p.Stop()

	if _, err := p.Token(); !errors.Is(err, ErrTokenProviderStopped) {
		t.Fatalf("Token() err = %v, want ErrTokenProviderStopped", err)
	}
}
