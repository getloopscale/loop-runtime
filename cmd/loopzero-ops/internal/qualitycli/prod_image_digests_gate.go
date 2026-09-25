// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagedigests"
)

func init() {
	register(Gate{
		Name:    "prod-image-digests",
		Summary: "Production manifests reference images by digest.",
		Setup:   setupProdImageDigests,
	})
}

// setupProdImageDigests checks that published platform charts use
// image@sha256:<digest> instead of mutable :tag references.
// T2.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap. Default mode:
// WARN (violations reported, exit 0). Pass --enforce to fail on
// violations after 7-day soak validates zero false positives.
func setupProdImageDigests(fs *flag.FlagSet) Check {
	enforce := fs.Bool("enforce", false, "fail on violations (default: warn-only)")
	return func(inv Invocation) (Outcome, error) {
		// `--enforce` upgrades the policy to every kind. Without it the decided
		// default applies: literal `:tag` blocks, missing `image.digest` warns until
		// the image publisher emits the value. See kind.go.
		policy := prodimagedigests.DefaultEnforce()
		if *enforce {
			policy = prodimagedigests.EnforceAll()
		}
		outcome, err := prodimagedigests.Check(prodimagedigests.Config{Root: inv.Root, Enforce: policy})
		if err != nil {
			return Fail, err
		}
		if len(outcome.Violations) > 0 {
			// Reported per kind, not under one banner. One number over both
			// populations is what kept three fixable findings invisible inside seven
			// that nobody could fix — the reader could not tell which half was which.
			if blocking := outcome.Enforced(); len(blocking) > 0 {
				inv.Stderr.Printf("prod-image-digests: FAIL — %d referência(s) de imagem mutável que bloqueiam:\n", len(blocking))
				for _, v := range blocking {
					inv.Stderr.Printf("  %s\n", v)
				}
			}
			if warned := outcome.Warned(); len(warned) > 0 {
				inv.Stderr.Printf("prod-image-digests: WARN — %d referência(s) reportada(s) sem bloquear (aguardam o publicador de imagens):\n", len(warned))
				for _, v := range warned {
					inv.Stderr.Printf("  %s\n", v)
				}
			}
			inv.Stderr.Println("")
			// Two classes, two fixes. Before B-113 only the first existed, and the
			// message said "use the helper" — useless advice for a chart that ALREADY
			// uses the helper and declares no digest, which is the whole second class.
			inv.Stderr.Println("Fix — literal in the template (`image: foo/bar:v1.2.3`):")
			inv.Stderr.Println("     use `image: foo/bar@sha256:<digest>`, or delegate to")
			inv.Stderr.Println("     `{{ include \"loopzero-common.image\" . }}`, which prefers a digest.")
			inv.Stderr.Println("Fix — `image.digest missing` in values.yaml:")
			inv.Stderr.Println("     the template is already right; the VALUE is missing. Declare `image.digest`")
			inv.Stderr.Println("     (the image publisher produces it — a digest pinned by hand")
			inv.Stderr.Println("     becomes a lie on the next publication).")
			inv.Stderr.Println("See docs/architecture.md for context.")
			if !outcome.Pass {
				return Fail, nil
			}
		}
		if outcome.Pass {
			inv.Stdout.Printf("prod-image-digests: PASS (scanned %d files, %d warn-mode violation(s))\n", outcome.ScannedFiles, len(outcome.Violations))
		}
		return Pass, nil
	}
}
