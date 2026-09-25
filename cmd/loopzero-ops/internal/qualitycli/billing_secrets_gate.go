// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/billingsecrets"
)

func init() {
	register(Gate{
		Name:    "billing-secrets",
		Summary: "Billing secrets must not have literal optionality; the decision is made by value.",
		Setup:   setupBillingSecrets,
	})
}

// setupBillingSecrets fails LITERAL optionality on the billing secrets.
//
// Measured on 2026-08-11 in app-dev: the dashboard pod ran 20h `Running` without
// `loopzero-stripe-webhook` and without `loopzero-stripe-api-keys`, emitting not a single
// event — because `optional: true` is, to Kubernetes, a perfectly normal setting.
//
// The gate does not require the secrets to be always mandatory: the hermetic setup
// mounts without credentials on purpose. It requires the decision to be a VALUE,
// because a literal holds equally for every environment, and the public one ends up
// inheriting the most permissive choice.
func setupBillingSecrets(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := billingsecrets.Check(billingsecrets.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if len(outcome.Findings) > 0 {
			fmt.Fprintf(inv.Stderr, "billing-secrets: FAIL — %d billing secret mount(s) with literal optionality:\n",
				len(outcome.Findings))
			for _, f := range outcome.Findings {
				fmt.Fprintf(inv.Stderr, "  %s:%d — %s\n", f.File, f.Line, f.Why)
			}
			return Fail, nil
		}
		// Saying HOW MANY files were read matters: a gate that found nothing because it
		// read nothing would look, in the log, like a gate that passed.
		fmt.Fprintf(inv.Stdout, "billing-secrets: PASS — %d chart(s) checked, optionality decided by value\n",
			len(outcome.Scanned))
		return Pass, nil
	}
}
