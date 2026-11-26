// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package drillcredisolation

import "testing"

func TestCheck_BothTokensAbsent_Missing(t *testing.T) {
	if got := Check(Env{}).ExitCode; got != 2 {
		t.Errorf("absent DRILL_DO + DIGITALOCEAN: want exit 2, got %d", got)
	}
}

func TestCheck_DrillDOAbsent_Strict_Missing(t *testing.T) {
	r := Check(Env{DigitalOcean: "do-prod", RequireIsolated: "true"})
	if r.ExitCode != 2 {
		t.Errorf("strict + DRILL_DO absent: want exit 2, got %d", r.ExitCode)
	}
}

func TestCheck_DrillDOAbsent_D7Override_Safe(t *testing.T) {
	// D7' sponsor override: DRILL_DO absent + DIGITALOCEAN present + not strict → exit 0
	r := Check(Env{DigitalOcean: "single-token"})
	if r.ExitCode != 0 {
		t.Errorf("D7' override: want exit 0, got %d", r.ExitCode)
	}
}

func TestCheck_DrillDOEqualsProdDO_Catastrophic(t *testing.T) {
	// Same token in DRILL_DO and DIGITALOCEAN → hashes match → exit 1.
	r := Check(Env{DrillDO: "shared-token", DigitalOcean: "shared-token"})
	if r.ExitCode != 1 {
		t.Fatalf("shared DO token: want exit 1 (CATASTROPHIC), got %d", r.ExitCode)
	}
}

func TestCheck_DrillDOEqualsProdDO_viaDO_TOKEN_Catastrophic(t *testing.T) {
	r := Check(Env{DrillDO: "shared", DO: "shared"})
	if r.ExitCode != 1 {
		t.Errorf("shared via DO_TOKEN: want exit 1, got %d", r.ExitCode)
	}
}

func TestCheck_DrillDODistinct_Safe(t *testing.T) {
	r := Check(Env{DrillDO: "drill-token", DigitalOcean: "prod-token"})
	if r.ExitCode != 0 {
		t.Errorf("distinct DO tokens: want exit 0, got %d", r.ExitCode)
	}
}

func TestCheck_DrillDOPresent_NoProdDO_TriviallySafe(t *testing.T) {
	r := Check(Env{DrillDO: "drill-only"})
	if r.ExitCode != 0 {
		t.Errorf("drill-only context: want exit 0, got %d", r.ExitCode)
	}
}

func TestCheck_DrillPulumiEqualsProd_Catastrophic(t *testing.T) {
	r := Check(Env{DrillDO: "drill", DigitalOcean: "prod", DrillPulumi: "p", PulumiAccess: "p"})
	if r.ExitCode != 1 {
		t.Errorf("shared pulumi token: want exit 1, got %d", r.ExitCode)
	}
}

func TestCheck_DrillCFEqualsProd_Catastrophic(t *testing.T) {
	r := Check(Env{DrillDO: "drill", DigitalOcean: "prod", DrillCF: "c", CloudflareAPI: "c"})
	if r.ExitCode != 1 {
		t.Errorf("shared CF token: want exit 1, got %d", r.ExitCode)
	}
}

func TestCheck_AllDistinct_Safe(t *testing.T) {
	r := Check(Env{
		DrillDO: "d1", DigitalOcean: "p1",
		DrillPulumi: "d2", PulumiAccess: "p2",
		DrillCF: "d3", CloudflareAPI: "p3",
	})
	if r.ExitCode != 0 {
		t.Errorf("all distinct: want exit 0, got %d (msgs=%v)", r.ExitCode, r.Messages)
	}
}

func TestHashShort_DeterministicAnd8Hex(t *testing.T) {
	h := hashShort("abc")
	if len(h) != 8 {
		t.Errorf("want 8-char hash, got %d (%q)", len(h), h)
	}
	if hashShort("abc") != h {
		t.Error("hashShort not deterministic")
	}
	if hashShort("abc") == hashShort("abd") {
		t.Error("distinct inputs should not collide on 8 hex chars here")
	}
}
