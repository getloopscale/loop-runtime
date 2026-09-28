// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"fmt"
	"os"
)

// A capability is something an environment DECLARES it offers, and the gap
// between declaring and having is what this file makes visible.
//
// MEASURED 2026-08-11: app-dev served, for 20h, a dashboard with a plan, a price
// and an upgrade button while the secret that verifies payments did not exist in
// the cluster. Four independent layers treated that absence as legitimate
// configuration — the Secret mount (`optional: true`), the seed
// (`SKIP-graceful`), the runtime (declaring billing by looking only at the API
// key) and usage reporting (walking zero rows and returning nil).
//
// None of them is wrong alone: in a hermetic environment, running without
// billing credentials is the desired behaviour. What was missing is the question
// that separates the two cases — *did this environment promise to charge?* — and
// it has to be answered in one place. Answered in four, it is answered four ways,
// and the fifth layer is born without it.

// Capability names a declarable capability.
//
// It is a type of its own, not a bare string, so the table below and its callers
// cannot diverge through a typo the compiler would accept.
type Capability string

const (
	// CapabilityBilling — the environment really charges: it issues portal
	// sessions, receives payment events and reports usage.
	CapabilityBilling Capability = "billing"
)

// RequirementPolicy answers whether the absence of a capability's input is
// tolerable.
//
// One method, on purpose (ISP): a caller wants to know whether it may proceed
// without the input, nothing more. The seed decides HOW to obtain the input;
// this policy decides WHETHER it is mandatory.
type RequirementPolicy interface {
	Requires(Capability) bool
}

// capabilitiesByEnv is the table of declarations — DATA, not a `switch`.
//
// A new environment is a row here; a new capability is a constant above plus an
// entry. No consumer changes (OCP). The previous shape — `if env == "dev-public"`
// scattered around — required editing every consumer, which is how the fifth
// layer was born without the check the other four had.
//
// Absence is deliberately NOT demanding: an environment outside the table
// promises nothing, so it must not be held to it. That is the opposite polarity
// of the port registry (`infra/quality/host-ports.txt`), for a reason: there,
// silence means an open port nobody reviewed; here, it means an environment that
// did not promise to charge.
var capabilitiesByEnv = map[string][]Capability{
	// dev-public serves app-dev.loopzero.example — the public dashboard, with plan and
	// price on screen.
	"dev-public": {CapabilityBilling},
}

// EnvRequirementPolicy resolves the requirement from the Helmfile environment.
//
// It reuses the signal tenant-fixture already used to decide whether to run; the
// difference is that there the decision was embedded in the seed, and here it is
// a collaborator tests replace without an environment variable.
type EnvRequirementPolicy struct {
	declared map[Capability]bool
}

// NewEnvRequirementPolicy builds the policy for a named environment.
//
// It takes the environment as a parameter instead of reading `os.Getenv`: a
// collaborator that reads the process cannot be replaced in a test, and testing
// through an environment variable makes tests order-dependent, which
// `rules/testing.md § 3` forbids.
func NewEnvRequirementPolicy(env string) *EnvRequirementPolicy {
	declared := make(map[Capability]bool, 1)
	for _, c := range capabilitiesByEnv[env] {
		declared[c] = true
	}
	return &EnvRequirementPolicy{declared: declared}
}

// PolicyFromEnvironment is the factory the composition root uses, and the one
// place this policy reads the process environment.
func PolicyFromEnvironment() *EnvRequirementPolicy {
	return NewEnvRequirementPolicy(os.Getenv("LOOPZERO_HELMFILE_ENV"))
}

// Requires reports whether this environment declared the capability.
func (p *EnvRequirementPolicy) Requires(c Capability) bool {
	if p == nil {
		return false
	}
	return p.declared[c]
}

// MissingInput turns the absence of an input into the verdict the environment
// deserves.
//
// It is the one place in the repository that knows the rule "missing input +
// declared capability = defect". Both Stripe seeds consult it; a third consumer
// inherits it without reimplementing it, which is the difference between fixing
// four layers and fixing the pattern.
//
// `detail` says WHAT is missing in the caller's words — the credentials file, a
// placeholder value. The reason adds the capability and where to fix it, because
// a failure that does not say what to do becomes a dig for whoever meets it.
func (d *Deps) MissingInput(c Capability, detail string) Result {
	if d.RequiresCapability(c) {
		return Result{
			Verdict: FAIL,
			Reason: fmt.Sprintf("%s — this environment declares the %q capability, and without this input "+
				"it does not exist. Fill it in ~/.loopzero/dev-credentials.json", detail, c),
		}
	}
	// The environment promised nothing: skipping is correct, and it is what the
	// post-cleanroom chain expects in a hermetic tier.
	return Result{Verdict: SKIP, Reason: detail + " (SKIP-graceful)"}
}

// RequiresCapability is the shortcut the seeds use.
//
// Nil-safe at two levels — Deps without a policy, and a nil policy — because
// Deps is built in many places that have nothing to do with billing. If an empty
// Deps started demanding, this would break unrelated callers with an unexplained
// FAIL, the opposite of what it exists to produce.
func (d *Deps) RequiresCapability(c Capability) bool {
	if d == nil || d.Policy == nil {
		return false
	}
	return d.Policy.Requires(c)
}
