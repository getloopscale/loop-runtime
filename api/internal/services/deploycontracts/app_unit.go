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
// four persistence types and three application types cross its public boundary,
// and 35 files in 10 packages depend on this package, so all of them depend
// transitively on persistence.
//
//	shared_contracts.go:207   WithLock(..., fn func(tx db.DBTX) error) error
//	delivery.go:37            Activate(ctx, build *db.Build, ...) error
//	delivery.go:55            TargetRelease *db.Release
//	manifest_builder.go:23    Apps []db.BuildAppSpec
//
// Plus manifest_builder.go:11, which imports api/internal/manifests — an
// application-layer package.
//
// Removing them costs 63 call sites, 34 of them `WithLock`, the environment-lock
// primitive Unbreakable Rule 6 puts on every mutation. That work is tracked as
// B-079. Until it lands, the `domain-sink-purity` gate freezes exactly this set
// and refuses a fifth: the leak is not closed, but it has stopped growing.
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

	Port       int               `json:"port"`
	Build      string            `json:"build"`                // "dockerfile", "buildpack", "custom"
	Start      string            `json:"start,omitempty"`      // T0.2b: declared services.json start command (canonical Dockerfile CMD)
	Dockerfile string            `json:"dockerfile,omitempty"` // resolved Dockerfile path (relative to app path)
	Rendering  string            `json:"rendering,omitempty"`  // "static" or "server" (MEET-009)
	Env        map[string]string `json:"env,omitempty"`        // build-time environment variables
	Secrets    []string          `json:"secrets,omitempty"`
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

	env := raw.Env
	if env == nil {
		env = map[string]string{}
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
		Env:        env,
		Secrets:    secrets,
	}, nil
}
