// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/credshape"
)

// StripeWebhookSecretSeed is the Go port of
// infra/scripts/local/seed-stripe-secret-canonical.sh.
// It materializes loop-runtime-system/loop-runtime-stripe-webhook with a single key
// STRIPE_WEBHOOK_SECRET (canonical env-var-name match — cloud config reads
// envOr("STRIPE_WEBHOOK_SECRET", "") after envFrom exposes the Secret keys
// verbatim as env vars).
//
// Parity contract (D3):
//   - creds absent (Deps.Creds == nil)                 → SKIP
//   - stripe.webhook_secret empty                      → SKIP
//   - stripe.webhook_secret == "whsec_PLACEHOLDER"     → SKIP (M5 sponsor-blocked)
//   - credshape.IsPlaceholder(webhook_secret) matches other sentinels (whsec_dev_, REPLACE_, …) → SKIP
//   - k8s API error                                    → FAIL
//   - success (create OR update)                       → PASS
type StripeWebhookSecretSeed struct{}

// NewStripeWebhookSecretSeed returns the seed instance for the registry.
func NewStripeWebhookSecretSeed() *StripeWebhookSecretSeed { return &StripeWebhookSecretSeed{} }

// Name is the CLI token: `loop-runtime-ops seed stripe-webhook-secret`.
func (s *StripeWebhookSecretSeed) Name() string { return "stripe-webhook-secret" }

const (
	stripeSecretNamespace = "loop-runtime-system"
	stripeSecretName      = "loop-runtime-stripe-webhook"
	stripeSecretKey       = "STRIPE_WEBHOOK_SECRET"
	stripePlaceholder     = "whsec_PLACEHOLDER" // bash literal sentinel
)

// Run applies the Secret idempotently.
func (s *StripeWebhookSecretSeed) Run(d Deps) Result {
	if r, skip := s.precondition(d); skip {
		return r
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would apply loop-runtime-stripe-webhook"}
	}
	// Trim trailing newlines for byte-for-byte parity with the bash seed
	// (python3 `d.get(...)` preserves the JSON string but bash `$(...)` strips
	// trailing newlines; then `create secret --from-literal` embeds the raw value).
	// B-108 — this seed runs on its own (`loop-runtime-ops seed stripe-webhook-secret`), which is how an operator
	// repairs one piece without repeating the whole chain. Nothing then guarantees
	// the namespace, and the failure is silent on our side: the seed stops, the
	// chain goes on, and the effect shows up in another system. Idempotent — a
	// Get when the namespace exists, which is the usual path.
	if err := d.Kube.EnsureNamespace(d.Ctx, stripeSecretNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", stripeSecretNamespace, err)}
	}
	value := []byte(strings.TrimRight(d.Creds.Stripe.WebhookSecret, "\n"))
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx,
		secretOf(stripeSecretNamespace, stripeSecretName, map[string][]byte{stripeSecretKey: value})); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("apply %s: %v", stripeSecretName, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("applied %s in %s (key=%s)", stripeSecretName, stripeSecretNamespace, stripeSecretKey)}
}

// Verify probes the Secret and confirms STRIPE_WEBHOOK_SECRET matches the
// creds file. Absence or divergence is FAIL (drift).
func (s *StripeWebhookSecretSeed) Verify(d Deps) Result {
	if r, skip := s.precondition(d); skip {
		return r
	}
	got, err := d.Kube.GetSecret(d.Ctx, stripeSecretNamespace, stripeSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s: %v", stripeSecretName, err)}
	}
	if string(got.Data[stripeSecretKey]) != strings.TrimRight(d.Creds.Stripe.WebhookSecret, "\n") {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s value diverges from creds", stripeSecretName, stripeSecretKey)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s/%s matches creds", stripeSecretName, stripeSecretKey)}
}

// precondition returns (result, true) when the seed should return early
// with SKIP or FAIL. (Result{}, false) means proceed to mutation.
func (s *StripeWebhookSecretSeed) precondition(d Deps) (Result, bool) {
	if d.Creds == nil {
		return d.MissingInput(CapabilityBilling, "dev-credentials.json absent"), true
	}
	wh := d.Creds.Stripe.WebhookSecret
	if wh == "" || wh == stripePlaceholder || credshape.IsPlaceholder(wh) {
		return d.MissingInput(CapabilityBilling, "stripe.webhook_secret placeholder/absent"), true
	}
	return Result{}, false
}
