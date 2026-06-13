package domain

import "testing"

// TestDefaultPermanentEnv asserts the canonical value AND existence of the constant.
// Per ADR D3: this constant is the single source of truth across the monorepo.
// A change to this value MUST flow through plan amendment + frontend mirror update
// + parity gate re-run; it is NOT a deploy-time configuration knob.
func TestDefaultPermanentEnv(t *testing.T) {
	if DefaultPermanentEnv != "production" {
		t.Fatalf("DefaultPermanentEnv = %q; want %q (canonical product vocabulary per ADR D3)", DefaultPermanentEnv, "production")
	}
}
