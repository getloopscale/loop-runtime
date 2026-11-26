// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// seedsRequiringTheirNamespace are the seeds that write a Secret into a namespace
// they cannot assume exists, and must therefore ensure it first.
//
// The list is EXPLICIT rather than derived, and that is the point. Measured on
// 2026-08-14: seven seeds in this package write Secrets and only ONE ensured its
// namespace. Deriving "every seed that writes a Secret must ensure" would have
// been a guess about the other five — some of their namespaces may be guaranteed
// by the position of the seed in the chain, and asserting otherwise would fail
// them for a problem nobody measured.
//
// So this holds the ones with a decision behind them. Adding a row is how a seed
// joins the contract; the five absent ones are registered as an open question,
// not as an exemption.
var seedsRequiringTheirNamespace = map[string]string{
	"github_app_dev.go": "B-021 — ran before loopruntime-system existed on a fresh cleanroom, the webhook " +
		"secret was never written, and the endpoint 404'd with no error anywhere in our logs",
	"broker.go": "ensured its namespace since it was written",

	// B-108, measured 2026-08-16. These write into namespaces the canonical
	// chain does create (core.helmfile.yaml.gotmpl declares createNamespace, and
	// `task setup` runs before seed-post-cleanroom-all.sh). The guarantee belongs
	// to the CHAIN, not to the seed: each is also a subcommand an operator runs on
	// its own to repair one piece, which is exactly how B-021 failed.
	"gitea_token.go": "B-108 — step 6; guaranteed by the chain, not by a standalone run",
	// This one writes into `argocd`, not loopruntime-system: the constant the file
	// declares at the top (giteaAdminNamespace) is not the one it writes with
	// (argocdNamespace, via repoCredsSecretOf).
	"argocd_gitea_creds.go":    "B-108 — step 6b; writes into `argocd`, whose lifecycle is the ArgoCD release's",
	"argocd_token.go":          "B-108 — step 13; writes into loopruntime-system via minioNamespace",
	"stripe_webhook_secret.go": "B-108 — step 15",
	"stripe_api_keys.go":       "B-108 — step 16",
	"cluster_kubeconfigs.go":   "#236 — run by k3d:heal and provision-api-tokens.sh, and on its own as a repair",
}

// TestSeedsEnsureTheirNamespaceBeforeWriting asserts the call site: every seed
// in the contract calls EnsureNamespace in the same file that writes its
// Secret. EnsureNamespace itself is covered behaviourally in kube_roles_test.go;
// what a behavioural seed test cannot show without building each seed's whole
// fixture is that the call was not deleted, which is the failure that produced
// B-021.
func TestSeedsEnsureTheirNamespaceBeforeWriting(t *testing.T) {
	for file, why := range seedsRequiringTheirNamespace {
		t.Run(file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Clean(file))
			if err != nil {
				t.Fatalf("reading %s: %v", file, err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, file, src, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", file, err)
			}

			var ensures, writes []int
			ast.Inspect(parsed, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				line := fset.Position(call.Pos()).Line
				if fn, ok := call.Fun.(*ast.SelectorExpr); ok {
					switch fn.Sel.Name {
					case "EnsureNamespace":
						ensures = append(ensures, line)
					case "CreateOrUpdateSecret", "MergeSecretData":
						// Both create the Secret when it is absent, so both
						// need the namespace to exist first.
						writes = append(writes, line)
					}
				}
				return true
			})

			if len(writes) == 0 {
				t.Fatalf("%s writes no Secret — it no longer belongs in this contract, so remove its "+
					"row rather than leaving an assertion that verifies nothing", file)
			}
			if len(ensures) == 0 {
				t.Fatalf("%s writes a Secret at line %d without ensuring its namespace anywhere.\n"+
					"Why this file is in the contract: %s", file, writes[0], why)
			}

			// Ordering is asserted only when the file has exactly one of each, and
			// that restriction came from being wrong: the first version compared the
			// earliest ensure against the earliest write globally and failed
			// broker.go, which writes to TWO clusters — mgmt first, then the workload
			// namespace it ensures. Per pair the order is right; measured across the
			// file it looked inverted.
			//
			// Tracking the client and namespace of each call would express the real
			// invariant, but both arrive as identifiers whose values live elsewhere,
			// so the AST alone cannot resolve which write belongs to which ensure.
			// Claiming the ordering anyway would be the same defect this contract
			// exists to catch, one level up. Presence still catches the failure mode
			// that produced B-021: somebody deleting the line.
			if len(ensures) == 1 && len(writes) == 1 && ensures[0] > writes[0] {
				t.Errorf("%s ensures its namespace at line %d, AFTER writing its only Secret at line %d "+
					"— with a single pair the order is the whole guarantee", file, ensures[0], writes[0])
			}
		})
	}
}
