// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"strings"
	"testing"
)

// generate-secrets.sh sources the standalone database password from OpenBao's
// database engine when a session exists (T6.5 of plan 2026-06-02-openbao-adoption),
// and otherwise falls back to loopruntime-dev-password. A cleanroom has no OpenBao yet, so
// the fallback is what `task setup` runs on; if a refactor drops it, setup waits
// forever for a Vault that does not exist.

const generateSecrets = "../../helmfile/scripts/generate-secrets.sh"

// The Vault branch must need all three of an address, a token and the CLI; with
// any one absent the script is on the cold-bootstrap path.
func TestGenerateSecretsTakesTheVaultPathOnlyWithASession(t *testing.T) {
	requireClauses(t, generateSecrets,
		clause(`BAO_ADDR.*BAO_TOKEN.*command -v bao`,
			"the Vault branch is no longer gated on BAO_ADDR, BAO_TOKEN and the bao CLI together"),
		clause(`T6\.5.*openbao-adoption`, "the comment naming the plan that introduced the Vault path drifted"),
	)
}

func TestGenerateSecretsFallsBackToTheDevPassword(t *testing.T) {
	requireClauses(t, generateSecrets,
		clause(`LOOPRUNTIME_DB_PASSWORD="\$\{LOOPRUNTIME_DB_PASSWORD:-loopruntime-dev-password\}"`,
			"the cold-bootstrap assignment is gone; the standalone database password is pinned to loopruntime-dev-password across the local stack"),
	)
}

// The fallback is a `:-` default, so it must come AFTER the Vault block: placed
// before, it would set the password first and the Vault answer would never be read.
func TestGenerateSecretsAppliesTheFallbackAfterTheVaultBlock(t *testing.T) {
	src := readSource(t, generateSecrets)
	vault := lineOf(src, "database/creds/loopruntime-api")
	fallback := lineOf(src, "LOOPRUNTIME_DB_PASSWORD:-loopruntime-dev-password")
	if vault == 0 || fallback == 0 {
		t.Fatalf("could not locate the Vault block (line %d) or the fallback (line %d)", vault, fallback)
	}
	if fallback <= vault {
		t.Errorf("the fallback (line %d) precedes the Vault block (line %d), so the Vault path is bypassed", fallback, vault)
	}
}

// lineOf returns the 1-based line of the first occurrence of needle, or 0.
func lineOf(src, needle string) int {
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 0
}
