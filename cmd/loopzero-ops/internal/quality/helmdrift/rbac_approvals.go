// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

// rbacValuesMatrix pins the values the published side is rendered against, so
// the reported count moves only when a CHART changes (B-001 NFR-006).
//
// The divergence reported is the UNION of the divergences found under each entry:
// a cell the published chart grants only under one flag state still diverges
// under the other, and a comparison that unioned the published CELL sets instead
// would call that agreement.
//
// `rbac.create=false` is EXCLUDED deliberately. At that value the published chart
// renders no ClusterRole at all — helm reports "could not find template
// templates/clusterrole.yaml in chart" — so every comparison degenerates to "all
// 175 bootstrap cells diverge", a number printed forever, which teaches everyone
// to ignore the gate.
//
// Measured 2026-09-15 with helm v3.19.0+g3d8990f: 162 divergent cells at
// legacyClusterWideSecrets=true, 174 at false, and the 162 are a strict SUBSET of
// the 174 — so the union is monotonic and cannot shrink because somebody flipped
// a default in values.yaml. The helm version is RECORDED and not enforced: a pin
// would fail the gate on a routine helm upgrade, which is the opposite failure
// from the one being guarded against, while the record is what lets a reader
// answer "did the count move because a chart changed, or because helm did?".
var rbacValuesMatrix = [][]string{
	{"rbac.create=true", "rbac.legacyClusterWideSecrets=true"},  // shipped default
	{"rbac.create=true", "rbac.legacyClusterWideSecrets=false"}, // post-rollout target
}
