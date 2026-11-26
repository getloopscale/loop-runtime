// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/credshape"
)

// StripeAPIKeysSeed is the Go port of
// infra/scripts/local/seed-stripe-api-keys-canonical.sh.
// It materializes loopruntime-system/loopruntime-stripe-api-keys with 4 keys:
//
//	STRIPE_API_KEY        (secret; rk_/sk_ + test_/live_ prefix)
//	STRIPE_PRICE_STARTER  (may be empty — free tier)
//	STRIPE_PRICE_PRO      (Red Team #155 closure)
//	STRIPE_PRICE_TEAM     (Red Team #155 closure)
//
// After apply, loopruntime-cloud is rollout-restarted so envFrom re-reads STRIPE_API_KEY
// (mirrors bash Layer 4; Red Team #154 closure). A missing loopruntime-cloud Deployment
// on Run is non-fatal (out-of-cluster gateway shape today).
//
// Parity contract (D3):
//   - creds absent (Deps.Creds == nil)                    → SKIP
//   - stripe.api_key empty or a placeholder               → Deps.MissingInput
//     (SKIP, or FAIL when the environment declares billing)
//   - stripe.api_key prefix not rk_/sk_ + test_/live_     → FAIL (EC-2)
//   - k8s API error on Secret apply                       → FAIL
//   - success                                             → PASS
type StripeAPIKeysSeed struct{}

// NewStripeAPIKeysSeed returns the seed instance for the registry.
func NewStripeAPIKeysSeed() *StripeAPIKeysSeed { return &StripeAPIKeysSeed{} }

// Name is the CLI token: `loopruntime-ops seed stripe-api-keys`.
func (s *StripeAPIKeysSeed) Name() string { return "stripe-api-keys" }

const (
	stripeAPIKeysNamespace = "loopruntime-system"
	stripeAPIKeysName      = "loopruntime-stripe-api-keys"
	stripeAPIKeyKey        = "STRIPE_API_KEY"
	stripePriceStarterKey  = "STRIPE_PRICE_STARTER"
	stripePriceProKey      = "STRIPE_PRICE_PRO"
	stripePriceTeamKey     = "STRIPE_PRICE_TEAM"
	stripeCloudDeployment  = "loopruntime-cloud"
)

func (s *StripeAPIKeysSeed) Run(d Deps) Result {
	if r, skip := s.precondition(d); skip {
		return r
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would apply loopruntime-stripe-api-keys + rollout loopruntime-cloud"}
	}
	// B-108 — this seed runs on its own (`loopruntime-ops seed stripe-api-keys`), which is how an operator
	// repairs one piece without repeating the whole chain. Nothing then guarantees
	// the namespace, and the failure is silent on our side: the seed stops, the
	// chain goes on, and the effect shows up in another system. Idempotent — a
	// Get when the namespace exists, which is the usual path.
	if err := d.Kube.EnsureNamespace(d.Ctx, stripeAPIKeysNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", stripeAPIKeysNamespace, err)}
	}
	data := map[string][]byte{
		stripeAPIKeyKey:       []byte(strings.TrimRight(d.Creds.Stripe.APIKey, "\n")),
		stripePriceStarterKey: []byte(strings.TrimRight(d.Creds.Stripe.PriceStarter, "\n")),
		stripePriceProKey:     []byte(strings.TrimRight(d.Creds.Stripe.PricePro, "\n")),
		stripePriceTeamKey:    []byte(strings.TrimRight(d.Creds.Stripe.PriceTeam, "\n")),
	}
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx,
		secretOf(stripeAPIKeysNamespace, stripeAPIKeysName, data)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("apply %s: %v", stripeAPIKeysName, err)}
	}
	// Rollout restart loopruntime-cloud so envFrom re-reads STRIPE_API_KEY. Byte-parity
	// contract with the bash seed (D3): a missing loopruntime-cloud Deployment is FAIL
	// (bash exit 1) — even though the Secret was successfully applied — because
	// the operator's canonical expectation is that the pod actually re-reads
	// the new key. A softer PASS + reason would be a nicer contract; treat
	// that as a follow-up ADR after all Wave 1 seeds are migrated.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, stripeAPIKeysNamespace, stripeCloudDeployment); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout restart %s: %v", stripeCloudDeployment, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("applied %s + rollout %s", stripeAPIKeysName, stripeCloudDeployment)}
}

func (s *StripeAPIKeysSeed) Verify(d Deps) Result {
	if r, skip := s.precondition(d); skip {
		return r
	}
	got, err := d.Kube.GetSecret(d.Ctx, stripeAPIKeysNamespace, stripeAPIKeysName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s: %v", stripeAPIKeysName, err)}
	}
	if string(got.Data[stripeAPIKeyKey]) != strings.TrimRight(d.Creds.Stripe.APIKey, "\n") {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s value diverges from creds", stripeAPIKeysName, stripeAPIKeyKey)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s/%s matches creds", stripeAPIKeysName, stripeAPIKeyKey)}
}

func (s *StripeAPIKeysSeed) precondition(d Deps) (Result, bool) {
	if d.Creds == nil {
		return d.MissingInput(CapabilityBilling, "dev-credentials.json absent"), true
	}
	switch err := credshape.ParseStripeSecretKey(d.Creds.Stripe.APIKey); {
	case errors.Is(err, credshape.ErrStripeKeyPlaceholder):
		return d.MissingInput(CapabilityBilling, "stripe.api_key placeholder/absent"), true
	case err != nil:
		// EC-2: a publishable key (pk_*) must never be accepted as the secret key.
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("stripe.api_key: %v", err)}, true
	}
	return Result{}, false
}
