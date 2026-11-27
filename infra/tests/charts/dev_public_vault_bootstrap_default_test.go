// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T-A3 of follow-up to ADR 2026-06-20-dev-public-vault-automated-init.
//
// TestDevPublicVaultBootstrapDefault locks the contract that the
// dev-public Cleanroom flow uses the AUTOMATED Shamir init path
// (vault-bootstrap-dev.sh against the SEPARATE k3d-loop-runtime-secrets cluster)
// as its canonical default, AND that the bootstrap-in-core shortcut
// remains PROHIBITED (Approach A UNBREAKABLE).
//
// 4 assertions, all source-grep on canonical files (no kubectl, no
// docker, no helmfile):
//
//	(a) seed-post-cleanroom-all.sh invokes vault-bootstrap-dev.sh
//	(b) seed-post-cleanroom-all.sh, when LOOP_RUNTIME_HELMFILE_ENV=dev-public,
//	    auto-provisions loop-runtime-secrets via `task k3d:secrets:create` +
//	    `task k3d:secrets:bootstrap` BEFORE vault-bootstrap-dev.sh runs
//	    (so the dev-public chain is solo/CI provisionable per the ADR)
//	(c) seed-post-cleanroom-all.sh NEVER invokes the forbidden
//	    `task k3d:secrets:bootstrap-in-core` shortcut (Approach A is
//	    UNBREAKABLE regardless of witness scope)
//
// Cross-references:
//   - ADR: .claude/knowledge-base/decisions/2026-06-20-dev-public-vault-automated-init.md
//   - Script: infra/scripts/local/seed-post-cleanroom-all.sh
//   - Companion guards: vault_host_published_reach_test.go (T1.1), host_k3d_internal_canonical_test.go (T1.2)
//   - Issues #111, #112, #113, #114 — empirical close-out after T-A4
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDevPublicVaultBootstrapDefault — locks the 4-assertion contract.
func TestDevPublicVaultBootstrapDefault(t *testing.T) {
	root := repoRoot(t)
	seedPath := filepath.Join(root, "infra/scripts/local/seed-post-cleanroom-all.sh")
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read %s: %v", seedPath, err)
	}
	seed := string(seedBytes)

	// (a) seed-post-cleanroom-all.sh invokes vault-bootstrap-dev.sh
	if !regexp.MustCompile(`vault-bootstrap-dev\.sh`).MatchString(seed) {
		t.Errorf("(a) seed-post-cleanroom-all.sh does NOT reference vault-bootstrap-dev.sh — the canonical dev-public Cleanroom default per ADR 2026-06-20-dev-public-vault-automated-init.")
	}

	// (b) For LOOP_RUNTIME_HELMFILE_ENV=dev-public, the script must auto-provision
	//     loop-runtime-secrets via `task k3d:secrets:create` AND `task k3d:secrets:bootstrap`
	//     BEFORE vault-bootstrap-dev.sh runs, so the dev-public chain is
	//     solo/CI provisionable. We look for both `task k3d:secrets:create`
	//     and `task k3d:secrets:bootstrap` invocations gated on
	//     LOOP_RUNTIME_HELMFILE_ENV == dev-public.
	hasCreate := regexp.MustCompile(`task\s+k3d:secrets:create\b`).MatchString(seed)
	hasBootstrap := regexp.MustCompile(`task\s+k3d:secrets:bootstrap\b`).MatchString(seed)
	hasDevPublicGate := regexp.MustCompile(`LOOP_RUNTIME_HELMFILE_ENV[^=]*==?\s*"?dev-public"?`).MatchString(seed) ||
		regexp.MustCompile(`"dev-public"`).MatchString(seed)
	if !hasCreate || !hasBootstrap || !hasDevPublicGate {
		t.Errorf("(b) seed-post-cleanroom-all.sh does NOT auto-provision loop-runtime-secrets on dev-public (need `task k3d:secrets:create` + `task k3d:secrets:bootstrap` BEFORE vault-bootstrap-dev.sh, gated on LOOP_RUNTIME_HELMFILE_ENV=dev-public). Current state: hasCreate=%v hasBootstrap=%v hasDevPublicGate=%v", hasCreate, hasBootstrap, hasDevPublicGate)
	}

	// (c) NEVER actually INVOKE `task k3d:secrets:bootstrap-in-core` — Approach A
	//     UNBREAKABLE forbids co-locating OpenBao in the mgmt cluster
	//     regardless of the witness/tier scope. The token MAY appear in a
	//     comment line explaining the prohibition; what we forbid is an
	//     executable invocation (non-comment line containing the token).
	for i, line := range strings.Split(seed, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if regexp.MustCompile(`task\s+k3d:secrets:bootstrap-in-core\b`).MatchString(line) {
			t.Errorf("(c) seed-post-cleanroom-all.sh executes `task k3d:secrets:bootstrap-in-core` at line %d — Approach A UNBREAKABLE forbids this shortcut. Use `task k3d:secrets:create` + `task k3d:secrets:bootstrap` against the SEPARATE loop-runtime-secrets cluster.\n  offending line: %s", i+1, line)
			break
		}
	}
}
