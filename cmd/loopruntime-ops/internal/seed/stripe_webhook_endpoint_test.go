// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// stripeStub captures POST/DELETE + returns canned list responses.
type stripeStub struct {
	mu          sync.Mutex
	authOK      string
	existingURL string // if non-empty, GET /webhook_endpoints returns one entry with this URL
	existingID  string
	deletions   []string
	creations   []map[string][]string
	createReply stripeAPIWebhookEndpoint
}

func newStripeStub(t *testing.T, token, existingURL, existingID string, reply stripeAPIWebhookEndpoint) *httptest.Server {
	t.Helper()
	stub := &stripeStub{authOK: token, existingURL: existingURL, existingID: existingID, createReply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _, ok := r.BasicAuth()
		if !ok || u != stub.authOK {
			http.Error(w, "unauth", http.StatusUnauthorized)
			return
		}
		stub.mu.Lock()
		defer stub.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/webhook_endpoints" && r.Method == http.MethodGet:
			resp := stripeAPIWebhookEndpointList{}
			if stub.existingURL != "" {
				resp.Data = append(resp.Data, stripeAPIWebhookEndpoint{
					ID: stub.existingID, URL: stub.existingURL,
				})
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v1/webhook_endpoints" && r.Method == http.MethodPost:
			_ = r.ParseForm()
			stub.creations = append(stub.creations, r.Form)
			_ = json.NewEncoder(w).Encode(stub.createReply)
		case strings.HasPrefix(r.URL.Path, "/v1/webhook_endpoints/") && r.Method == http.MethodDelete:
			stub.deletions = append(stub.deletions, strings.TrimPrefix(r.URL.Path, "/v1/webhook_endpoints/"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted": true}`))
		case strings.HasPrefix(r.URL.Path, "/v1/webhook_endpoints/") && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"` + strings.TrimPrefix(r.URL.Path, "/v1/webhook_endpoints/") + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

// stripeHost is a Host whose environment points the seed at the stub.
func stripeHost(stubURL, credsPath, webhookURL string) Host {
	return Host{Getenv: envOf(map[string]string{
		stripeAPIEnv:        stubURL,
		stripeCredsPathEnv:  credsPath,
		stripeWebhookURLEnv: webhookURL,
	})}
}

func TestStripeWhsecCanonical(t *testing.T) {
	cases := map[string]bool{
		"whsec_test_abc": true,
		"whsec_live_x":   true,
		"whsec_xyz":      true,
		"whsec_dev_x":    false,
		"":               false,
		"nope":           false,
	}
	for in, want := range cases {
		if got := stripeWhsecCanonical(in); got != want {
			t.Errorf("stripeWhsecCanonical(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStripeWebhookEndpoint_SkipWhenCredsAbsent(t *testing.T) {
	deps := Deps{Ctx: context.Background(), Creds: nil}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookEndpoint_FailWhenAPIKeyIsPublishable(t *testing.T) {
	creds := &Creds{}
	creds.Stripe.APIKey = "pk_test_notallowed"
	deps := Deps{Ctx: context.Background(), Creds: creds}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookEndpoint_SkipWhenAPIKeyPlaceholder(t *testing.T) {
	creds := &Creds{}
	creds.Stripe.APIKey = "sk_test_REPLACE_ME"
	deps := Deps{Ctx: context.Background(), Creds: creds}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookEndpoint_SkipWhenEndpointAndWhsecCanonical(t *testing.T) {
	targetURL := "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stub := newStripeStub(t, "rk_test_abc", targetURL, "we_existing", stripeAPIWebhookEndpoint{})
	defer stub.Close()
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "dev-creds.json")
	_ = os.WriteFile(credsPath, []byte(`{"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_test_existing"}}`), 0o600)

	host := stripeHost(stub.URL, credsPath, targetURL)

	creds, err := LoadCreds(credsPath)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	deps := Deps{Host: host, Ctx: context.Background(), Creds: creds}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "already canonical") {
		t.Errorf("Reason = %q, want 'already canonical'", got.Reason)
	}
}

func TestStripeWebhookEndpoint_DryRunReports(t *testing.T) {
	targetURL := "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stub := newStripeStub(t, "rk_test_abc", "", "", stripeAPIWebhookEndpoint{})
	defer stub.Close()
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "dev-creds.json")
	_ = os.WriteFile(credsPath, []byte(`{"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_dev_placeholder"}}`), 0o600)
	host := stripeHost(stub.URL, credsPath, targetURL)
	creds, _ := LoadCreds(credsPath)
	deps := Deps{Host: host, Ctx: context.Background(), Creds: creds, DryRun: true}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q, want dry-run marker", got.Reason)
	}
}

func TestStripeWebhookEndpoint_CreatesAndDelegatesToSibling(t *testing.T) {
	targetURL := "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stub := newStripeStub(t, "rk_test_abc", "", "", stripeAPIWebhookEndpoint{
		ID: "we_new_abc", Secret: "whsec_test_new_secret",
	})
	defer stub.Close()

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "dev-creds.json")
	_ = os.WriteFile(credsPath, []byte(`{"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_dev_placeholder"}}`), 0o600)

	host := stripeHost(stub.URL, credsPath, targetURL)

	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-cloud"}},
	).Build()
	deps := Deps{Host: host,
		Ctx:   context.Background(),
		Kube:  NewKubeClient(c),
		Creds: creds,
	}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	// Assert dev-credentials.json was updated with the new whsec.
	raw, _ := os.ReadFile(credsPath)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	st := doc["stripe"].(map[string]any)
	if st["webhook_secret"] != "whsec_test_new_secret" {
		t.Errorf("whsec_secret not persisted: %v", st["webhook_secret"])
	}
	// Assert mode 0600 preserved.
	info, _ := os.Stat(credsPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
	// Assert sibling seed materialized the K8s Secret.
	var sec corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-stripe-webhook"), &sec); err != nil {
		t.Errorf("sibling did not create loopruntime-stripe-webhook Secret: %v", err)
	} else if got := string(sec.Data["STRIPE_WEBHOOK_SECRET"]); got != "whsec_test_new_secret" {
		t.Errorf("Secret STRIPE_WEBHOOK_SECRET = %q, want whsec_test_new_secret", got)
	}
}

func TestStripeWebhookEndpoint_DeletesStaleBeforeCreate(t *testing.T) {
	targetURL := "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stub := newStripeStub(t, "rk_test_abc", targetURL, "we_stale", stripeAPIWebhookEndpoint{
		ID: "we_new", Secret: "whsec_test_fresh",
	})
	defer stub.Close()

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "dev-creds.json")
	_ = os.WriteFile(credsPath, []byte(`{"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_dev_stale"}}`), 0o600)

	host := stripeHost(stub.URL, credsPath, targetURL)

	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-cloud"}},
	).Build()
	deps := Deps{Host: host, Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookEndpoint_FailWhenStripeReturnsPlaceholderSecret(t *testing.T) {
	targetURL := "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stub := newStripeStub(t, "rk_test_abc", "", "", stripeAPIWebhookEndpoint{
		ID: "we_ok", Secret: "whsec_dev_placeholder_from_stub",
	})
	defer stub.Close()

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "dev-creds.json")
	_ = os.WriteFile(credsPath, []byte(`{"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_dev_x"}}`), 0o600)
	host := stripeHost(stub.URL, credsPath, targetURL)

	creds, _ := LoadCreds(credsPath)
	deps := Deps{Host: host, Ctx: context.Background(), Creds: creds}
	got := NewStripeWebhookEndpointSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "placeholder shape") {
		t.Errorf("Reason = %q, want 'placeholder shape'", got.Reason)
	}
}

func TestStripeWebhookEndpoint_Verify_PassWhenCanonical(t *testing.T) {
	targetURL := "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stub := newStripeStub(t, "rk_test_abc", targetURL, "we_ok", stripeAPIWebhookEndpoint{})
	defer stub.Close()

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "dev-creds.json")
	_ = os.WriteFile(credsPath, []byte(`{"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_test_canonical"}}`), 0o600)
	host := stripeHost(stub.URL, credsPath, targetURL)

	creds, _ := LoadCreds(credsPath)
	deps := Deps{Host: host, Ctx: context.Background(), Creds: creds}
	got := NewStripeWebhookEndpointSeed().Verify(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookEndpoint_Name(t *testing.T) {
	if got := NewStripeWebhookEndpointSeed().Name(); got != "stripe-webhook-endpoint" {
		t.Errorf("Name = %q", got)
	}
}

func TestStripePersistWhsec_PreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.json")
	orig := `{"github":{"app_id":"3319955"},"stripe":{"api_key":"rk_test_abc"},"resend":{"api_key":"re_x"}}`
	_ = os.WriteFile(p, []byte(orig), 0o600)
	if err := stripePersistWhsec(p, "whsec_test_new"); err != nil {
		t.Fatalf("stripePersistWhsec: %v", err)
	}
	raw, _ := os.ReadFile(p)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	if doc["github"].(map[string]any)["app_id"] != "3319955" {
		t.Errorf("github.app_id lost: %v", doc)
	}
	if doc["stripe"].(map[string]any)["webhook_secret"] != "whsec_test_new" {
		t.Errorf("webhook_secret not written: %v", doc)
	}
	if doc["stripe"].(map[string]any)["api_key"] != "rk_test_abc" {
		t.Errorf("stripe.api_key lost: %v", doc)
	}
}
