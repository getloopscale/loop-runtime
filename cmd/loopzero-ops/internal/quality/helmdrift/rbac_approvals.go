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
// It is keyed by PAIR, and a pair with no entry is rendered once at chart
// defaults. The matrix belongs to the chart that carries the conditional, not to
// the gate: measured 2026-09-15, `helm template charts/loopzero-admission-policies
// --set rbac.create=true` FAILS — that chart ships a values.schema.json and
// rejects the key outright. A gate-wide matrix would therefore have made two of
// the three pairs unrenderable, and swallowing that error to keep going would
// report "no divergence" about a pair nothing rendered.
var rbacValuesMatrix = map[string][][]string{
	"loopzero-api:loopzero-api": {
		{"rbac.create=true", "rbac.legacyClusterWideSecrets=true"},  // shipped default
		{"rbac.create=true", "rbac.legacyClusterWideSecrets=false"}, // post-rollout target
	},
}

// defaultValuesMatrix renders once, at whatever the chart itself defaults to.
var defaultValuesMatrix = [][]string{nil}

// matrixFor returns the values matrix a pair is compared under.
func matrixFor(pair string) [][]string {
	if m, ok := rbacValuesMatrix[pair]; ok {
		return m
	}
	return defaultValuesMatrix
}

// The two claims an entry can make. They are different, and collapsing them
// would make the registry lie: the second says a live asymmetry was RECORDED,
// not that anyone approved it.
const (
	statusDeclared  = "declared"  // intended, reviewed, and explained
	statusUndecided = "undecided" // live, unreviewed; the decision is still open
)

// Which chart grants a recorded cell that the other does not.
const (
	sideBootstrapOnly = "bootstrap-only"
	sidePublishedOnly = "published-only"
)

// rbacApproval is one CELL's licence to differ.
//
// It is deliberately NOT the kind `approval`. A kind key's third part is a
// Kubernetes Kind — uppercase first letter, no dot — and `(core)/secrets/delete`
// fails it on both counts, so the cell registry carries its own shape check with
// the same teeth plus one field.
//
// Status is that field. Two different claims have to be expressible: "this
// asymmetry is intended and reviewed", and "this asymmetry is live, unreviewed,
// and its decision is still open". A registry with only the first would force
// this gate to assert a security judgement nobody made.
type rbacApproval struct {
	Decision string
	// Rule is the grant that produces the cell, stated by its content (apiGroups,
	// resources, verbs) rather than by a template line number that moves on the
	// next edit.
	Rule   string
	Reason string
	Status string
}

// cellApprovalKey spells a cell's registry key exactly as the report spells the
// cell, so comparing a gate failure to a registry entry is a literal string
// match rather than a mental translation — the property that makes a registry of
// this size reviewable at all.
func cellApprovalKey(pair string, c Cell) string {
	return pair + ":" + c.String()
}
