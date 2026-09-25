// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import "testing"

// These tests measure the gap between DECLARING a capability and HAVING it.
//
// Measured 2026-08-11: app-dev ran for 20h serving a dashboard with plan, price
// and upgrade button while the secret that verifies payments was absent. No
// layer lied on its own — each treated the absence as legitimate configuration.
// It is legitimate in a hermetic environment; it is not in one that promises to
// charge.

func TestPolicyRequiresBillingWhereTheEnvironmentDeclaresIt(t *testing.T) {
	p := NewEnvRequirementPolicy("dev-public")
	if !p.Requires(CapabilityBilling) {
		t.Fatal("dev-public serves the public dashboard with plan and price; since it declares billing, " +
			"a missing credential is a defect, not configuration")
	}
}

// The hermetic environment runs without Stripe credentials on purpose, and
// staying green there is what let this change land without breaking anyone
// who promised nothing.
func TestPolicyDoesNotRequireBillingWhereItIsNotDeclared(t *testing.T) {
	for _, env := range []string{"", "standalone-multi", "drill"} {
		if NewEnvRequirementPolicy(env).Requires(CapabilityBilling) {
			t.Errorf("environment %q does not declare billing; requiring it there breaks the hermetic tier", env)
		}
	}
}

// The table is data, and data has holes: a capability nobody declared must not
// become a requirement by omission.
func TestUnknownCapabilityIsNotRequired(t *testing.T) {
	if NewEnvRequirementPolicy("dev-public").Requires(Capability("no-such-capability")) {
		t.Error("an undeclared capability must not be required by omission")
	}
}

// Deps is built in many places (tests, older subcommands). If a Deps without a
// policy started demanding, this would break callers unrelated to billing with
// an unexplained FAIL.
func TestDepsWithoutAPolicyRequiresNothing(t *testing.T) {
	var d Deps
	if d.RequiresCapability(CapabilityBilling) {
		t.Error("Deps without a policy must behave as before: require nothing")
	}
}

func TestDepsDelegatesToTheInjectedPolicy(t *testing.T) {
	d := Deps{Policy: NewEnvRequirementPolicy("dev-public")}
	if !d.RequiresCapability(CapabilityBilling) {
		t.Error("Deps must delegate to the injected policy, not reimplement the decision")
	}
}

// A requirement table that empties does not fail — it starts permitting
// everything, in silence. That is how the firewall block list aged toward the
// unsafe side, measured 2026-08-10, without anything noticing.
func TestTheBillingEnvironmentIsInTheTable(t *testing.T) {
	if len(capabilitiesByEnv) == 0 {
		t.Fatal("the capability table is empty — no environment would require anything")
	}
	caps, ok := capabilitiesByEnv["dev-public"]
	if !ok || len(caps) == 0 {
		t.Fatal("dev-public serves the public dashboard; it must declare what it offers, " +
			"or the policy has nothing to require")
	}
}
