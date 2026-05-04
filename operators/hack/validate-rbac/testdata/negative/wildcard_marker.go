// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// negative testdata — analyzer MUST detect the wildcard markers below
// and exit non-zero. Used by validate-rbac_test.go.
//
// This file intentionally does not compile against the production code;
// it lives in testdata/ which Go tooling skips by default.

package negative

// BadReconciler triggers wildcard violations per ADR-0026 condition #5.
//
// +kubebuilder:rbac:groups=core,resources=*,verbs=get;list
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=*
type BadReconciler struct{}
