// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import "github.com/getloopscale/loopzero/pkg/statemachine"

// Machine singletons for the status-mutation stores. M9 5b relocated these from
// the root db package (build_types.go / release_types.go) into db/store/ so that
// SafeTransition is invoked ONLY from the adapter package — the MA-7 Gate C
// boundary (ADR 2026-07-03-m9-db-store-subpackage-topology D3;
// 2026-07-03-m9-5b-nil-tx-status-atomicity). Typed machines: the state name is
// the canonical persisted string, so rows written under the untyped path read
// back identically (no column migration).
var (
	buildMachine   = statemachine.NewBuildTypedMachine()
	releaseMachine = statemachine.NewReleaseTypedMachine()
	domainMachine  = statemachine.NewDomainMachine()
)
