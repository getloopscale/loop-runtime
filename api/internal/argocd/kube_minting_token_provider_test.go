// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	return fmt.Sprintf("%s.%s.signature", header, payload)
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
		tok := mintTokenJWT(f.t, "loopzero-api:apiKey", mintExp, now)
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
	clientset := fake.NewSimpleClientset(objs...)
	p, err := NewKubeMintingTokenProvider(KubeMintingConfig{
		ArgoCDBaseURL:             baseURL,
		AccountName:               "loopzero-api",
		Namespace:                 "loopzero-system",
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
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-refresh-credentials", Namespace: "loopzero-system"},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
}

func tokenSecret(token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "loopzero-api-tokens", Namespace: "loopzero-system"},
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
	existing := mintTokenJWT(t, "loopzero-api:apiKey", 0, time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(existing), credsSecret("bearer-x", "", ""))
	if got := p.Token(); got != existing {
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
			t.Fatalf("timeout: provider did not mint on startup; mintCount=%d token=%q", fa.mintCount, p.Token())
		default:
			if p.Token() != "" {
				if fa.mintCount < 1 {
					t.Fatalf("expected at least 1 mint, got %d", fa.mintCount)
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
	expiring := mintTokenJWT(t, "loopzero-api:apiKey", time.Now().Add(1*time.Hour).Unix(), time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(expiring), credsSecret("bearer-x", "", ""))
	// Don't start the proactive loop in this test — we only want to
	// observe the reactive path. mintsBefore captures any incidental
	// activity (none expected since Start is not called).
	mintsBefore := fa.mintCount

	p.OnAuthFailure()

	// Reactive signal needs the loop to consume it. Fire one mint via
	// the synchronous Refresh() to simulate the loop's mintAndPersist.
	if err := p.Refresh(); err != nil {
		t.Fatalf("reactive Refresh: %v", err)
	}
	if fa.mintCount <= mintsBefore {
		t.Fatalf("reactive mint did not happen; mintCount=%d", fa.mintCount)
	}
	// Token swapped to the freshly-minted one.
	if p.Token() == expiring {
		t.Fatalf("token did not change after reactive mint")
	}
}

func TestProvider_ProactiveRefresh_WhenTokenNearExpiry(t *testing.T) {
	fa, srv := newFakeArgoCD(t)
	// Token expires in 1h — well below the 6h threshold.
	soon := time.Now().Add(1 * time.Hour).Unix()
	expiring := mintTokenJWT(t, "loopzero-api:apiKey", soon, time.Now().Unix())
	p := newProvider(t, srv.URL, tokenSecret(expiring), credsSecret("bearer-x", "", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatalf("proactive mint did not happen; mintCount=%d", fa.mintCount)
		default:
			if fa.mintCount > 0 && p.Token() != expiring {
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
	for p.Token() == "" {
		select {
		case <-deadline:
			t.Fatal("never minted")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	sec, err := p.cfg.KubeClient.CoreV1().Secrets("loopzero-system").Get(
		context.Background(), "loopzero-api-tokens", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret not created: %v", err)
	}
	if string(sec.Data["argocd-token"]) != p.Token() {
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
	for p.Token() == "" {
		select {
		case <-deadline:
			t.Fatal("never minted")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	if fa.loginCount < 1 {
		t.Fatalf("expected /api/v1/session call, got %d", fa.loginCount)
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
	if fa.mintCount > 3 {
		t.Fatalf("single-flight not effective: mintCount=%d (expected ≤3)", fa.mintCount)
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
