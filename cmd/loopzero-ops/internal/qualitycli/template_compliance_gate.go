// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/templatecompliance"
)

func init() {
	register(Gate{
		Name:    "template-compliance",
		Summary: "Checks documents comply with their canonical templates.",
		// Needs the PR body (--pr-body / $PR_BODY). Reported as "FAIL: empty PR
		// body" from a repo scan, which was read as a real finding and filed under
		// failingGate; it is a missing mandatory input, like helm-diff-structural.
		Wiring: parameterized,
		NoRoot: true,
		Setup:  setupTemplateCompliance,
	})
}

// setupTemplateCompliance replaces template-compliance.sh: PR-template checkbox
// compliance gate.
func setupTemplateCompliance(fs *flag.FlagSet) Check {
	prBody := fs.String("pr-body", "", "PR markdown body (falls back to $PR_BODY)")
	prAuthor := fs.String("pr-author", "", "PR author login (falls back to $PR_AUTHOR)")
	prLabels := fs.String("pr-labels", "", "CSV PR labels (falls back to $PR_LABELS)")
	return func(inv Invocation) (Outcome, error) {
		// Env fallback: PR body is untrusted input. CI passes it via env var
		// (NOT a CLI arg) to avoid GitHub Actions ${{ }} shell-injection.
		body, author, labels := *prBody, *prAuthor, *prLabels
		if body == "" {
			body = os.Getenv("PR_BODY")
		}
		if author == "" {
			author = os.Getenv("PR_AUTHOR")
		}
		if labels == "" {
			labels = os.Getenv("PR_LABELS")
		}
		outcome, err := templatecompliance.Run(templatecompliance.Config{
			PRBody:   body,
			PRAuthor: author,
			PRLabels: labels,
		})
		if err != nil {
			return Fail, err
		}
		for _, m := range outcome.Messages {
			fmt.Fprintln(inv.Stdout, m)
		}
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
