// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"strings"
	"testing"
)

// The false green these tests kill.
//
// MEASURED 2026-08-11: `seed stripe-webhook-secret --verify` on a machine
// without dev-credentials.json printed `SKIP — dev-credentials.json absent
// (SKIP-graceful)` and exited 0. Verdict.ExitCode returns success for PASS and
// for SKIP, so the whole post-cleanroom chain stayed green in an environment
// that could neither charge nor verify a payment.
//
// SKIP is the right meaning when the input is optional. It stops being right
// once the environment declared it offers the capability — then the absence is
// a defect, and a defect that reports success is worse than one that reports
// failure.

// stripePreconditioner is what the Stripe seeds have in common.
//
// Declared here, in the test, and not in production: the test is what needs to
// treat them uniformly. An interface in production code serving only the test
// would be an abstraction with no second consumer, which
// rules/parsimony-ladder.md rung 1 refuses.
type stripePreconditioner interface {
	precondition(Deps) (Result, bool)
}

func stripeSeeds() map[string]stripePreconditioner {
	return map[string]stripePreconditioner{
		"webhook-secret":   &StripeWebhookSecretSeed{},
		"api-keys":         &StripeAPIKeysSeed{},
		"webhook-endpoint": &StripeWebhookEndpointSeed{},
	}
}

// The case that flips the outcome.
func TestSeedFailsWithoutCredentialsWhenBillingIsRequired(t *testing.T) {
	for name, s := range stripeSeeds() {
		t.Run(name, func(t *testing.T) {
			d := Deps{Creds: nil, Policy: NewEnvRequirementPolicy("dev-public")}
			res, stopped := s.precondition(d)
			if !stopped {
				t.Fatal("without credentials the seed must not go on to mutate anything")
			}
			if res.Verdict != FAIL {
				t.Fatalf("an environment that declares billing and lacks the credential is "+
					"defective, not configured; want FAIL, got %s (%s)", res.Verdict, res.Reason)
			}
			if res.Verdict.ExitCode() == 0 {
				t.Error("the exit status must be non-zero, or the chain stays green and nothing changes")
			}
		})
	}
}

// The regression guard: the hermetic tier runs without Stripe credentials ON
// PURPOSE. Breaking it would trade a false green for a false red.
func TestSeedSkipsWithoutCredentialsWhenBillingIsNotRequired(t *testing.T) {
	for name, s := range stripeSeeds() {
		t.Run(name, func(t *testing.T) {
			for _, env := range []string{"", "standalone-multi"} {
				d := Deps{Creds: nil, Policy: NewEnvRequirementPolicy(env)}
				res, stopped := s.precondition(d)
				if !stopped || res.Verdict != SKIP {
					t.Errorf("environment %q does not promise to charge; want SKIP, got %s", env, res.Verdict)
				}
			}
		})
	}
}

// Nil-safe end to end.
func TestSeedWithoutAPolicyKeepsThePreviousBehaviour(t *testing.T) {
	for name, s := range stripeSeeds() {
		t.Run(name, func(t *testing.T) {
			res, _ := s.precondition(Deps{Creds: nil})
			if res.Verdict != SKIP {
				t.Errorf("Deps without a policy must skip as before; got %s", res.Verdict)
			}
		})
	}
}

// There are TWO ways for the input to be missing: the absent file and the
// placeholder value. Fixing only the first leaves the second producing the same
// false green — and the second is likelier in production, because a present
// file looks configured.
func TestPlaceholderAlsoFailsWhenRequired(t *testing.T) {
	creds := &Creds{}
	creds.Stripe.WebhookSecret = stripePlaceholder
	creds.Stripe.APIKey = stripePlaceholder

	for name, s := range stripeSeeds() {
		t.Run(name, func(t *testing.T) {
			d := Deps{Creds: creds, Policy: NewEnvRequirementPolicy("dev-public")}
			res, stopped := s.precondition(d)
			if !stopped || res.Verdict != FAIL {
				t.Fatalf("a placeholder is an absence that looks like configuration; want FAIL, got %s (%s)",
					res.Verdict, res.Reason)
			}
		})
	}
}

// A failure that does not say what to do becomes a dig.
func TestFailReasonSaysWhatToDo(t *testing.T) {
	for name, s := range stripeSeeds() {
		t.Run(name, func(t *testing.T) {
			d := Deps{Creds: nil, Policy: NewEnvRequirementPolicy("dev-public")}
			res, _ := s.precondition(d)
			r := strings.ToLower(res.Reason)
			if !strings.Contains(r, "dev-credentials.json") {
				t.Errorf("the reason must name the missing file; got %q", res.Reason)
			}
			if !strings.Contains(r, string(CapabilityBilling)) {
				t.Errorf("the reason must name the required capability; got %q", res.Reason)
			}
		})
	}
}

// stripeKeySeeds are the seeds that read stripe.api_key. They must answer the
// same question the same way: one rule (credshape.ParseStripeSecretKey), one
// capability policy (Deps.MissingInput).
func stripeKeySeeds() map[string]stripePreconditioner {
	return map[string]stripePreconditioner{
		"api-keys":         &StripeAPIKeysSeed{},
		"webhook-endpoint": &StripeWebhookEndpointSeed{},
	}
}

// A key with a valid secret prefix and a placeholder body passed the prefix
// check and was written to loopzero-stripe-api-keys, restarting loopzero-cloud onto it.
func TestStripeKeyWithPlaceholderBodyFailsWhenBillingIsRequired(t *testing.T) {
	for name, s := range stripeKeySeeds() {
		t.Run(name, func(t *testing.T) {
			creds := &Creds{}
			creds.Stripe.APIKey = "sk_test_REPLACE_ME"
			creds.Stripe.WebhookSecret = "whsec_test_realvalue"
			res, stopped := s.precondition(Deps{Creds: creds, Policy: NewEnvRequirementPolicy("dev-public")})
			if !stopped || res.Verdict != FAIL {
				t.Fatalf("want FAIL before any mutation, got stopped=%v %s (%s)", stopped, res.Verdict, res.Reason)
			}
		})
	}
}

func TestStripeKeyWithPlaceholderBodySkipsWhenBillingIsNotRequired(t *testing.T) {
	for name, s := range stripeKeySeeds() {
		t.Run(name, func(t *testing.T) {
			creds := &Creds{}
			creds.Stripe.APIKey = "sk_test_REPLACE_ME"
			res, stopped := s.precondition(Deps{Creds: creds, Policy: NewEnvRequirementPolicy("")})
			if !stopped || res.Verdict != SKIP {
				t.Fatalf("want SKIP, got stopped=%v %s (%s)", stopped, res.Verdict, res.Reason)
			}
		})
	}
}

// A publishable key is a misconfiguration, not an absence: FAIL in every seed,
// with or without a billing policy.
func TestStripePublishableKeyFailsInEveryKeySeed(t *testing.T) {
	for name, s := range stripeKeySeeds() {
		t.Run(name, func(t *testing.T) {
			creds := &Creds{}
			creds.Stripe.APIKey = "pk_test_publishable"
			res, stopped := s.precondition(Deps{Creds: creds})
			if !stopped || res.Verdict != FAIL {
				t.Fatalf("want FAIL, got stopped=%v %s (%s)", stopped, res.Verdict, res.Reason)
			}
		})
	}
}
