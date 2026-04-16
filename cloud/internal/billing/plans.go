// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

// Plan definitions with Stripe Price IDs.
// Price IDs are configured via environment variables — never hardcoded.
//
// Set via:
//   STRIPE_PRICE_STARTER=price_xxx
//   STRIPE_PRICE_PRO=price_yyy
//   STRIPE_PRICE_TEAM=price_zzz

import "os"

// PlanConfig holds the Stripe price IDs for each plan tier.
type PlanConfig struct {
	StarterPriceID string
	ProPriceID     string
	TeamPriceID    string
}

// LoadPlanConfig reads Stripe price IDs from environment variables.
func LoadPlanConfig() PlanConfig {
	return PlanConfig{
		StarterPriceID: os.Getenv("STRIPE_PRICE_STARTER"),
		ProPriceID:     os.Getenv("STRIPE_PRICE_PRO"),
		TeamPriceID:    os.Getenv("STRIPE_PRICE_TEAM"),
	}
}

// PriceIDForPlan returns the Stripe Price ID for a plan name.
func (c PlanConfig) PriceIDForPlan(plan string) string {
	switch plan {
	case "starter":
		return c.StarterPriceID
	case "pro":
		return c.ProPriceID
	case "team":
		return c.TeamPriceID
	default:
		return ""
	}
}

// PlanForPriceID returns the plan name for a Stripe Price ID (reverse lookup).
// Returns empty string if the price ID is unknown.
func (c PlanConfig) PlanForPriceID(priceID string) string {
	switch priceID {
	case c.StarterPriceID:
		return "starter"
	case c.ProPriceID:
		return "pro"
	case c.TeamPriceID:
		return "team"
	default:
		return ""
	}
}

// PlanDisplayName returns the user-facing name for a plan.
func PlanDisplayName(plan string) string {
	switch plan {
	case "starter":
		return "Starter"
	case "pro":
		return "Pro"
	case "team":
		return "Team"
	case "enterprise":
		return "Enterprise"
	default:
		return plan
	}
}
