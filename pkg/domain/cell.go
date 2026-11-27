// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domain

// DefaultCellID names the cell every installation starts with, and the cell a
// row or resource belongs to when nothing assigned it one. It is the single
// definition of the value in Go: api, operators and cmd/loop-runtime-ops all import it
// from here, because pkg is the one module every other module already depends
// on.
//
// The same value is also written outside Go, where a constant cannot reach:
// the tenants.cell_id SQL DEFAULT, and the kubebuilder default on
// TenantSpec.CellID and ClusterCredentialSpec.CellID. The operators module pins
// those CRD defaults to this constant in a test, so the two cannot drift.
//
// It is NOT an authorization value. A multi-cluster broker must derive cell
// identity from a per-cell TokenReview, never from this constant.
const DefaultCellID = "cell-0"
