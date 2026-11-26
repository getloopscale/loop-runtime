// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domain

import "testing"

// TestPermanentEnvs_CanonicalShape pins the contract that every
// project has EXACTLY TWO permanent environments (production +
// preview) per ADR-Q5 of qa-dogfood-cleanup-2026-06-21.
//
// The legacy single-env model (production only) surfaced as #124
// during the 2026-06-21 QA dogfooding run — `/environments` listed
// only production while the canonical PaaS model + QA doc J-10 +
// add-secret dialog (#127) all assume both are permanent.
//
// Iteration order MUST be [production, preview] so the dashboard
// /environments view renders deterministically and so the
// project.created handler ensures production FIRST (legacy-compat
// fail-fast) before preview (additive, non-fatal on failure).
func TestPermanentEnvs_CanonicalShape(t *testing.T) {
	if got, want := len(PermanentEnvs), 2; got != want {
		t.Fatalf("len(PermanentEnvs) = %d; want %d (ADR-Q5: production + preview only)", got, want)
	}
	if PermanentEnvs[0] != "production" {
		t.Errorf("PermanentEnvs[0] = %q; want %q (production must seed FIRST for legacy-compat)", PermanentEnvs[0], "production")
	}
	if PermanentEnvs[1] != "preview" {
		t.Errorf("PermanentEnvs[1] = %q; want %q (preview is the second canonical permanent env per ADR-Q5)", PermanentEnvs[1], "preview")
	}
	if DefaultPreviewEnv != "preview" {
		t.Errorf("DefaultPreviewEnv = %q; want %q (canonical PaaS lexicon, NOT 'staging' which is off-model per #127)", DefaultPreviewEnv, "preview")
	}
}

// TestPermanentEnvs_ContainsDefaultPermanentEnv guards against a
// future refactor that drops `production` from the canonical list.
// Backwards compat: every existing project + caller depends on
// `production` being permanent.
func TestPermanentEnvs_ContainsDefaultPermanentEnv(t *testing.T) {
	found := false
	for _, env := range PermanentEnvs {
		if env == DefaultPermanentEnv {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("PermanentEnvs %v MUST contain DefaultPermanentEnv %q (legacy-compat invariant)", PermanentEnvs, DefaultPermanentEnv)
	}
}

// TestPermanentEnvs_NoStagingLiteral is the cross-language echo of
// the dashboard env-terminology audit. #127 banished `staging` from
// the UI lexicon; this test prevents a future refactor from
// re-introducing it on the backend.
func TestPermanentEnvs_NoStagingLiteral(t *testing.T) {
	for _, env := range PermanentEnvs {
		if env == "staging" {
			t.Fatalf("PermanentEnvs %v MUST NOT contain 'staging' — canonical PaaS lexicon is preview + production (ADR-Q6, #127)", PermanentEnvs)
		}
	}
	if DefaultPreviewEnv == "staging" {
		t.Fatalf("DefaultPreviewEnv = %q is off-model; canonical is 'preview' per ADR-Q6", DefaultPreviewEnv)
	}
}
