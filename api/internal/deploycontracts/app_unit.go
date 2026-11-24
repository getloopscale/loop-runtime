// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package deploycontracts is the leaf shared-kernel of the services contexts:
// cross-context deploy DTOs and consumer-defined DIP ports. It is the sink of the
// services acyclic DAG (every other context may import it; it imports no
// sibling). M9 Step 6 created it as `shared`; renamed to `deploycontracts` per the
// architecture re-audit §7 (MEET-2026-07-03-002) to name its real responsibility.
//
// # It is the sink, and it is NOT pure
//
// This doc-comment said "the pure sink" until 2026-08-12. Measured, it is not:
// persistence and application types cross its public boundary, and every file
// that depends on this package depends transitively on them. Re-measured
// 2026-09-04: **38 production files in 11 packages** import the sink — the
// figure carried here since 2026-08-12 was 35 in 10, and it had drifted.
//
//	shared_contracts.go:213   WithLock(..., fn func(tx db.DBTX) error) error
//	manifest_builder.go:23    Apps []db.BuildAppSpec
//
// Plus manifest_builder.go:11, which imports api/internal/manifests — an
// application-layer package.
//
// # Two of the four are gone (B-079, 2026-09-04)
//
// `db.Build` and `db.Release` left this surface on 2026-09-04. They were the
// cheapest pair — both lived in delivery.go, together 11 of the 63 measured call
// sites — and they were replaced by types this package owns: ActivationBuild
// (7 of the build row's 16 fields) and RollbackTarget (2 of the release row's
// 18) — counted with reflect, not by eye. The build
// and release contexts translate at their own boundary, in
// build/activation_build.go and release/rollback_target.go. delivery.go no
// longer imports persistence at all.
//
// What did NOT shrink is the line count: 9 of those 11 still exist (this commit
// deleted two doc-comment lines here), and they
// just name a contract type now. The 63 was always the cost of removal, not a
// backlog that removal burns down — reading a smaller number here as progress
// would be the same mistake this comment was written to prevent.
//
// # `db.DBTX` and `db.BuildAppSpec` stay, and each says why
//
// `db.DBTX` is 34 of the 63, and the only one that is also risk: `WithLock` is
// the environment-lock primitive Unbreakable Rule 6 puts on every mutation, so
// changing its signature touches the point every write in the product passes
// through, and `cleanroom-up-live-test-mandate.md` makes LIVE validation
// mandatory for that. It is release work, not refactor work.
//
// `db.BuildAppSpec` is 18, and it leaves WITH the manifest renderer, not before:
// the type crosses because the sink describes the apps the manifest will render,
// and converting only this side would force all 18 points to convert back.
//
// The `domain-sink-purity` gate freezes exactly this set and refuses a new one:
// the leak is not closed, but it has stopped growing and it is two smaller.
//
// Claiming purity here would have been the cheapest thing to keep and the most
// expensive thing to believe — every new consumer reads this comment first.
package deploycontracts

import "fmt"

// AppUnit is a fully resolved app — all fields required, ready for build plan generation.
// This is the normalization boundary: before AppUnit, data comes from parsing and heuristics.
// After AppUnit, everything is deterministic.
type AppUnit struct {
	Name string `json:"name"`
	Type string `json:"type"` // "server", "worker", "frontend"
	Path string `json:"path"`

	// Runtime is the services.json build-strategy key (`python` | `node`).
	// The legacy `framework` field was removed (simplify-deploy-loopzerokit-
	// runtime-wiring T2.1 / F-dom-2 — runtime is the single contract, no
	// backward compatibility per D1/D2).
	Runtime string `json:"runtime,omitempty"`

	Port       int      `json:"port"`
	Build      string   `json:"build"`                // "dockerfile", "buildpack", "custom"
	Start      string   `json:"start,omitempty"`      // T0.2b: declared services.json start command (canonical Dockerfile CMD)
	Dockerfile string   `json:"dockerfile,omitempty"` // resolved Dockerfile path (relative to app path)
	Rendering  string   `json:"rendering,omitempty"`  // always "server": every app is served by its container
	Secrets    []string `json:"secrets,omitempty"`
}

// Plan B v3.1 T-B.1 (2026-06-05) — `frontendFrameworks` map removed post
// LoopZeroKit-only cutover. Apps now declare `type` explicitly em
// `.loopzero/services.json` (or via the legacy `services_json` form-field path
// during transition). The framework-string heuristic no longer participates
// em type inference; absent type defaults to "server" as the safe choice
// (frontends are CDN-static per ADR D5 + explicit em services manifest).

// NormalizeApp resolves a raw AppSpec into a fully populated AppUnit.
// Returns error if the combination is invalid (e.g., server without port).
func NormalizeApp(name string, raw AppSpec) (AppUnit, error) {
	// Resolve type — fixed stack is server|worker only (T3.3; default server).
	appType := raw.Type
	if appType == "" {
		appType = "server"
	}
	if appType != "server" && appType != "worker" {
		return AppUnit{}, fmt.Errorf("app '%s': invalid type '%s' (server|worker only)", name, appType)
	}
	// Static delivery was removed (T3.3): every app is served by its container, so a
	// declared rendering other than server is refused rather than ignored.
	if raw.Rendering != "" && raw.Rendering != "server" {
		return AppUnit{}, fmt.Errorf("app '%s': render '%s' is not supported; every app is served by its container, so remove render from its configuration", name, raw.Rendering)
	}

	// Resolve build strategy.
	// T2.2/D4 (plan simplify-deploy-loopzerokit-only): the BYO user-Dockerfile path
	// is deleted. The fixed LoopZeroKit stack ALWAYS builds from the platform-
	// generated canonical Dockerfile, so build is pinned to "buildpack"
	// (platform-generated) and no user Dockerfile path is resolved. The legacy
	// `build=="dockerfile"` default (which routed to a user-controlled path and
	// reopened the D4 build-escape hole for monorepo subpaths) is removed.
	// CreateBuildPlan resolves the canonical path regardless; this keeps the
	// AppUnit consistent with that contract.
	build := "buildpack"
	dockerfile := ""

	// Resolve port
	port := raw.Port
	if appType == "server" && port <= 0 {
		port = 8080
	}
	if appType == "worker" {
		port = 0
	}

	// Resolve path
	path := raw.Path
	if path == "" {
		path = "."
	}

	secrets := raw.Secrets
	if secrets == nil {
		secrets = []string{}
	}

	// T3.3 (plan simplify-deploy-loopzerokit-only v2.0): static-frontend delivery
	// removed — all apps are container delivery ("server" rendering). The
	// Rendering field is retained (always "server") for downstream compat.
	rendering := "server"

	return AppUnit{
		Name:       name,
		Type:       appType,
		Path:       path,
		Runtime:    raw.Runtime, // F-dom-2: carry services.json runtime — the build-strategy key
		Port:       port,
		Build:      build,
		Start:      raw.Start, // T0.2b: declared start → canonical Dockerfile CMD
		Dockerfile: dockerfile,
		Rendering:  rendering,
		Secrets:    secrets,
	}, nil
}
