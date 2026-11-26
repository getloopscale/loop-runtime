// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package drillcredisolation is the pure-logic port of
// infra/scripts/check-drill-credentials-isolation.sh (allowlist disposition
// MIGRATE-GO-PUREFN). It is the drill credentials isolation pre-flight: if a
// DRILL_* token equals the corresponding prod token, a drill teardown could
// destroy production resources (catastrophic per D7). The check compares the
// first-8-hex of SHA256 of each DRILL_* token against the prod token.
//
// Pure-logic: reads env vars + hashes + string compares; no LIVE calls. The
// drill-skill / preflight bash invokes this as a gate before any provision.
//
// Faithful port of the bash exit contract:
//
//	0 — isolation confirmed OR prod tokens absent (safe) OR D7' override
//	1 — a DRILL_* token EQUALS its prod counterpart (CATASTROPHIC; abort)
//	2 — neither DRILL_DO_TOKEN nor DIGITALOCEAN_TOKEN set (cannot proceed),
//	    or strict mode with DRILL_DO_TOKEN absent
package drillcredisolation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Env holds the token env vars the check reads. Injected for hermetic testing
// (production reads them via os.Getenv in the loopzero-ops runner).
type Env struct {
	DrillDO         string // DRILL_DO_TOKEN
	DigitalOcean    string // DIGITALOCEAN_TOKEN (prod OR D7' drill token)
	DO              string // DO_TOKEN (legacy prod alias)
	DrillPulumi     string // DRILL_PULUMI_TOKEN
	PulumiAccess    string // PULUMI_ACCESS_TOKEN (prod)
	DrillCF         string // DRILL_CF_TOKEN
	CloudflareAPI   string // CLOUDFLARE_API_TOKEN (prod)
	RequireIsolated string // DRILL_REQUIRE_ISOLATED_TOKENS ("true" = strict)
}

// Result is what the check concluded.
type Result struct {
	ExitCode int      // 0 safe / 1 catastrophic / 2 missing
	Messages []string // stderr-style lines (faithful to bash log() output intent)
}

// Verdict is Pass when the drill tokens are isolated from production, Fail
// when a drill token is a production token, and NotVerified when the tokens
// needed to compare them are not set.
func (r Result) Verdict() verdict.Verdict {
	switch r.ExitCode {
	case 0:
		return verdict.Pass
	case 1:
		return verdict.Fail
	}
	return verdict.NotVerified
}

// hashShort mirrors the bash `printf %s "$x" | sha256sum | cut -c1-8`.
func hashShort(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// Check runs the isolation verdict. Mirrors check-drill-credentials-isolation.sh
// branch-for-branch.
func Check(e Env) Result {
	var msg []string
	add := func(s string) { msg = append(msg, s) }
	strict := e.RequireIsolated == "true"

	// --- DRILL_DO_TOKEN absent: D7' override path or missing ---
	if e.DrillDO == "" {
		if e.DigitalOcean == "" {
			add("ERROR: neither DRILL_DO_TOKEN nor DIGITALOCEAN_TOKEN set")
			return Result{ExitCode: 2, Messages: msg}
		}
		if strict {
			add("ERROR (strict mode): DRILL_DO_TOKEN required when DRILL_REQUIRE_ISOLATED_TOKENS=true")
			return Result{ExitCode: 2, Messages: msg}
		}
		add("ADVISORY: DRILL_DO_TOKEN not set; using DIGITALOCEAN_TOKEN per D7' sponsor override")
		add("✓ Sponsor-approved override path; proceed with single-token model")
		return Result{ExitCode: 0, Messages: msg}
	}

	// --- DRILL_DO_TOKEN present: compare vs prod DO token (if any) ---
	drillDO := hashShort(e.DrillDO)
	if e.DO != "" || e.DigitalOcean != "" {
		prodDO := e.DO
		if prodDO == "" {
			prodDO = e.DigitalOcean
		}
		if drillDO == hashShort(prodDO) {
			add("CATASTROPHIC: DRILL_DO_TOKEN hash matches prod DO_TOKEN/DIGITALOCEAN_TOKEN!")
			add("  PER D7: drill MUST use a separate token scoped to the drill account.")
			return Result{ExitCode: 1, Messages: msg}
		}
		add(fmt.Sprintf("✓ DRILL_DO_TOKEN distinct from prod (hashes %s vs %s)", drillDO, hashShort(prodDO)))
	} else {
		add("✓ No prod DO token in this env (drill-only context) — isolation trivially confirmed")
	}

	// --- DRILL_PULUMI_TOKEN vs prod ---
	if e.DrillPulumi != "" && e.PulumiAccess != "" {
		if hashShort(e.DrillPulumi) == hashShort(e.PulumiAccess) {
			add("CATASTROPHIC: DRILL_PULUMI_TOKEN hash matches prod PULUMI_ACCESS_TOKEN!")
			return Result{ExitCode: 1, Messages: msg}
		}
		add("✓ DRILL_PULUMI_TOKEN distinct from prod")
	}

	// --- DRILL_CF_TOKEN vs prod ---
	if e.DrillCF != "" && e.CloudflareAPI != "" {
		if hashShort(e.DrillCF) == hashShort(e.CloudflareAPI) {
			add("CATASTROPHIC: DRILL_CF_TOKEN hash matches prod CLOUDFLARE_API_TOKEN!")
			return Result{ExitCode: 1, Messages: msg}
		}
		add("✓ DRILL_CF_TOKEN distinct from prod")
	}

	add("✓ All drill credentials confirmed isolated per D7. Safe to proceed.")
	return Result{ExitCode: 0, Messages: msg}
}
