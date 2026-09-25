// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package placement exports canonical multi-cell identifiers used
// across the API, operators, and tests. T1.1 of plan
// 2026-06-10-j22-layer2-code-review-fixes (ADR D1 + EC-3 absorption):
// the literal "cell-0" lived as a magic string across 8+ Go
// files (brokerBearerMiddleware fallback, multiple unit tests,
// PassthroughRouter, etc.). Centralizing the constant here gives the
// dev-public-single-cluster path one source of truth and lets future
// refactors (e.g., renaming to "cell-sfo3-01" for prod) propagate
// without grepping the codebase.
//
// Per Unbreakable Rule 9 (Don't Reinvent the Wheel) and project
// design-principles.md SRP: the package that already owns CellConfig,
// Router, DynamicRouter, and the placement engine is the right home
// for cell identifier constants — no new package needed.
package placement

// DefaultCellID names the canonical single-cluster cell used by
// dev-public and the standalone tilt-up environment. A multi-cluster
// broker must derive cell identity from a per-cell TokenReview, NOT this
// constant; that path does not exist yet.
//
// Callers MUST NOT use this constant to authorize cross-cell access
// in production — it is exclusively for the dev-public synthetic
// TokenReviewResult emitted by brokerBearerMiddleware (closing #13
// consensus-CRITICAL per plan T1.1 + ADR D1).
const DefaultCellID = "cell-0"
