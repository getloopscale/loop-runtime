// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/credshape"
)

// StripeWebhookEndpointSeed is the Go port of
// infra/scripts/local/seed-stripe-webhook-endpoint-canonical.sh.
//
// Post-Cleanroom Red Team loop-console#156 closure — provisions the
// Stripe test-mode webhook_endpoint on api.stripe.com AND persists the
// returned `whsec_test_*` back into ~/.loop-runtime/dev-credentials.json so the
// sibling `stripe-webhook-secret` seed can materialize it as a K8s Secret.
//
// 5-layer chain:
//
//  1. Read stripe.api_key from Creds (mode-0600 already enforced by LoadCreds).
//  2. GET api.stripe.com/v1/webhook_endpoints?limit=100 for existing
//     endpoint targeting LOOP_RUNTIME_WEBHOOK_URL. SKIP if endpoint present AND
//     current stripe.webhook_secret is already canonical. DELETE the
//     stale endpoint otherwise (whsec is returned ONLY at creation).
//  3. POST /v1/webhook_endpoints with url + 5 canonical enabled_events;
//     capture returned .id + .secret.
//  4. Persist whsec back to dev-credentials.json (preserving mode 0600 +
//     unrelated keys via JSON round-trip).
//  5. Delegate K8s Secret + rollout to the sibling stripe-webhook-secret
//     seed (in-process re-invocation — no shell-out).
type StripeWebhookEndpointSeed struct{}

func NewStripeWebhookEndpointSeed() *StripeWebhookEndpointSeed {
	return &StripeWebhookEndpointSeed{}
}
func (s *StripeWebhookEndpointSeed) Name() string { return "stripe-webhook-endpoint" }

const (
	stripeAPIDefaultBase       = "https://api.stripe.com"
	stripeAPIEnv               = "STRIPE_API_BASE" // test override
	stripeDefaultWebhookURL    = "https://app-dev.getloopscale.com/v1/webhooks/stripe"
	stripeWebhookURLEnv        = "LOOP_RUNTIME_WEBHOOK_URL"
	stripeCredsPathEnv         = "LOOP_RUNTIME_DEV_CREDENTIALS" // matches bash env var
	stripeWhsecPlaceholderPref = "whsec_dev_"
)

// stripeAPIWebhookEndpoint mirrors the fields we read/write on the Stripe response.
type stripeAPIWebhookEndpoint struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

type stripeAPIWebhookEndpointList struct {
	Data []stripeAPIWebhookEndpoint `json:"data"`
}

// precondition applies the one Stripe key rule (credshape) and the one
// capability policy (Deps.MissingInput) shared with stripe-api-keys, so both
// seeds give the same verdict on the same input.
func (s *StripeWebhookEndpointSeed) precondition(d Deps) (Result, bool) {
	if d.Creds == nil {
		return d.MissingInput(CapabilityBilling, "dev-credentials.json absent"), true
	}
	switch err := credshape.ParseStripeSecretKey(d.Creds.Stripe.APIKey); {
	case errors.Is(err, credshape.ErrStripeKeyPlaceholder):
		return d.MissingInput(CapabilityBilling, "stripe.api_key placeholder/absent"), true
	case err != nil:
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("stripe.api_key: %v", err)}, true
	}
	return Result{}, false
}

func (s *StripeWebhookEndpointSeed) Run(d Deps) Result {
	if r, stop := s.precondition(d); stop {
		return r
	}
	apiKey := strings.TrimSpace(d.Creds.Stripe.APIKey)

	base := d.Host.envOr(stripeAPIEnv, stripeAPIDefaultBase)
	targetURL := d.Host.envOr(stripeWebhookURLEnv, stripeDefaultWebhookURL)

	existingID, err := stripeFindEndpointByURL(d.Ctx, d.Host.stripeHTTP(), base, apiKey, targetURL)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("list endpoints: %v", err)}
	}
	existingWhsec := strings.TrimSpace(d.Creds.Stripe.WebhookSecret)
	if existingID != "" && stripeWhsecCanonical(existingWhsec) {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("endpoint %s exists AND whsec already canonical", existingID)}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would create Stripe endpoint url=%s + persist whsec + delegate materialize", targetURL)}
	}
	if existingID != "" {
		if err := stripeDeleteEndpoint(d.Ctx, d.Host.stripeHTTP(), base, apiKey, existingID); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("delete stale endpoint %s: %v", existingID, err)}
		}
	}
	created, err := stripeCreateEndpoint(d.Ctx, d.Host.stripeHTTP(), base, apiKey, targetURL)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("create endpoint: %v", err)}
	}
	if created.ID == "" || created.Secret == "" {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Stripe API did not return id/secret (id=%q, secret present=%t)", created.ID, created.Secret != "")}
	}
	if !stripeWhsecCanonical(created.Secret) {
		return Result{Verdict: FAIL, Reason: "Stripe returned a webhook secret with a placeholder shape (whsec_dev_* or not whsec_*)"}
	}

	// Persist to dev-credentials.json — mode 0600 preserved by writeJSONAtomically.
	credsPath := stripeCredsPath(d.Host)
	if credsPath == "" {
		return Result{Verdict: FAIL, Reason: "cannot resolve dev-credentials.json path (LOOP_RUNTIME_DEV_CREDENTIALS or $HOME/.loop-runtime/dev-credentials.json)"}
	}
	if err := stripePersistWhsec(credsPath, created.Secret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("persist whsec to %s: %v", credsPath, err)}
	}
	// Reload Creds so the in-process delegation sees the fresh whsec.
	fresh, err := LoadCreds(credsPath)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("reload creds after persist: %v", err)}
	}
	d.Creds = fresh
	// Delegate to sibling — Deps carry the mgmt Kube client already.
	if sib := NewStripeWebhookSecretSeed().Run(d); sib.Verdict != PASS {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("delegate stripe-webhook-secret: %s (%s)", sib.Verdict, sib.Reason)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("endpoint %s created + whsec persisted + sibling seed applied", created.ID)}
}

// Verify probes that the canonical webhook_secret + endpoint remain live.
func (s *StripeWebhookEndpointSeed) Verify(d Deps) Result {
	if r, stop := s.precondition(d); stop {
		return r
	}
	apiKey := strings.TrimSpace(d.Creds.Stripe.APIKey)
	if !stripeWhsecCanonical(d.Creds.Stripe.WebhookSecret) {
		return Result{Verdict: FAIL, Reason: "stripe.webhook_secret placeholder — canonical seed did not run"}
	}
	base := d.Host.envOr(stripeAPIEnv, stripeAPIDefaultBase)
	targetURL := d.Host.envOr(stripeWebhookURLEnv, stripeDefaultWebhookURL)
	id, err := stripeFindEndpointByURL(d.Ctx, d.Host.stripeHTTP(), base, apiKey, targetURL)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if id == "" {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("no Stripe endpoint targets %s", targetURL)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("endpoint %s present + whsec canonical", id)}
}

// --- Stripe API helpers ---

func stripeWhsecCanonical(v string) bool {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "whsec_") {
		return false
	}
	if strings.HasPrefix(v, stripeWhsecPlaceholderPref) {
		return false
	}
	return true
}

func stripeFindEndpointByURL(ctx context.Context, client *http.Client, base, apiKey, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/webhook_endpoints?limit=100", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(apiKey, "")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list endpoints: HTTP %d", resp.StatusCode)
	}
	var list stripeAPIWebhookEndpointList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", fmt.Errorf("decode list: %w", err)
	}
	for _, ep := range list.Data {
		if ep.URL == targetURL {
			return ep.ID, nil
		}
	}
	return "", nil
}

func stripeDeleteEndpoint(ctx context.Context, client *http.Client, base, apiKey, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/v1/webhook_endpoints/"+id, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(apiKey, "")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func stripeCreateEndpoint(ctx context.Context, client *http.Client, base, apiKey, targetURL string) (stripeAPIWebhookEndpoint, error) {
	body := url.Values{}
	body.Set("url", targetURL)
	for _, event := range []string{
		"checkout.session.completed",
		"customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted",
		"invoice.payment_failed",
	} {
		body.Add("enabled_events[]", event)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/webhook_endpoints",
		strings.NewReader(body.Encode()))
	if err != nil {
		return stripeAPIWebhookEndpoint{}, err
	}
	req.SetBasicAuth(apiKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return stripeAPIWebhookEndpoint{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		snippet := string(raw)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return stripeAPIWebhookEndpoint{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	var got stripeAPIWebhookEndpoint
	if err := json.Unmarshal(raw, &got); err != nil {
		return stripeAPIWebhookEndpoint{}, fmt.Errorf("decode: %w", err)
	}
	return got, nil
}

// --- Credential file persistence ---

// stripeCredsPath resolves the dev-credentials.json path from env or $HOME.
// Returns "" when neither is available (test env with no HOME).
func stripeCredsPath(h Host) string {
	if p := h.getenv(stripeCredsPathEnv); p != "" {
		return p
	}
	if h := h.getenv("HOME"); h != "" {
		return h + "/.loop-runtime/dev-credentials.json"
	}
	return ""
}

// stripePersistWhsec reads the JSON, updates stripe.webhook_secret in a
// map-preserving way, and rewrites the file atomically at mode 0600.
func stripePersistWhsec(path, whsec string) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied path
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	stripe, _ := doc["stripe"].(map[string]any)
	if stripe == nil {
		stripe = map[string]any{}
		doc["stripe"] = stripe
	}
	stripe["webhook_secret"] = whsec
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// Atomic write via temp file + rename; explicit chmod 0600 covers both
	// paths (temp file created 0600 via O_CREAT|O_EXCL then rename).
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	return nil
}
