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
// would make this registry lie: the second says a live asymmetry was RECORDED,
// not that anyone approved it.
const (
	statusDeclared  = "declared"  // intended, reviewed, and explained
	statusUndecided = "undecided" // live, unreviewed; the decision belongs elsewhere
)

// rbacApproval is one CELL's licence to differ.
//
// It is deliberately NOT the `approval` next door. That registry's shape test
// requires the third key part to be a Kubernetes Kind — uppercase first letter,
// no dot — and `(core)/secrets/delete` fails it on both counts. Reusing the
// registry would mean weakening the assertion that caught thirteen bad entries
// when the kind registry was re-keyed, so the precedent is extended rather than
// consumed, with the same teeth plus one field.
//
// Status is that field. Two different claims have to be expressible: "this
// asymmetry is intended and reviewed", and "this asymmetry is live, unreviewed,
// and its decision belongs to another item". A registry with only the first would
// force this gate to assert a security judgement nobody made.
type rbacApproval struct {
	ADR    string
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

// Reasons are shared per originating RULE rather than retyped per cell. Each names the
// file and line range that produced its group and how many cells that rule contributes,
// so an entry is traceable to the template by a reader with the file open — which is the
// property that makes a registry this size reviewable. One sentence repeated 172 times
// would be shorter to read and would say nothing more.
const (
	reasonBootArgoprojio23 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:23-25 grants apiGroups " +
		"[argoproj.io], resources [workflows, workflowtemplates, cronworkflows], verbs [get, " +
		"list, watch, create, update, patch, delete]. The published chart grants none of these 7 " +
		"cells under either entry of rbacValuesMatrix; where it grants other cells of the same " +
		"resources those agree and are not listed here. Measured 2026-09-15. Recorded and not " +
		"converged: narrowing the bootstrap side changes the RBAC Helmfile installs, and nothing " +
		"in B-001 measured what depends on it."
	reasonBootArgoprojio31 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:36-38 grants apiGroups " +
		"[argoproj.io], resources [applications, appprojects], verbs [get, list, watch, create, " +
		"update, patch, delete]. The published chart grants none of these 14 cells under either " +
		"entry of rbacValuesMatrix; where it grants other cells of the same resources those agree " +
		"and are not listed here. Measured 2026-09-15. Recorded and not converged: narrowing the " +
		"bootstrap side changes the RBAC Helmfile installs, and nothing in B-001 measured what " +
		"depends on it."
	reasonBootCore35 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:40-42 grants apiGroups " +
		"[(core)], resources [namespaces, configmaps, secrets, services, pods, events], verbs " +
		"[get, list, watch, create, update, patch, delete]. The published chart grants none of " +
		"these 32 cells under either entry of rbacValuesMatrix; where it grants other cells of " +
		"the same resources those agree and are not listed here. Measured 2026-09-15. Recorded " +
		"and not converged: narrowing the bootstrap side changes the RBAC Helmfile installs, and " +
		"nothing in B-001 measured what depends on it."
	reasonBootCore38 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:43-45 grants apiGroups " +
		"[(core)], resources [pods/log, pods/exec], verbs [get, list, create]. The published " +
		"chart grants none of these 4 cells under either entry of rbacValuesMatrix; where it " +
		"grants other cells of the same resources those agree and are not listed here. Measured " +
		"2026-09-15. Recorded and not converged: narrowing the bootstrap side changes the RBAC " +
		"Helmfile installs, and nothing in B-001 measured what depends on it."
	reasonBootApps42 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:47-49 grants apiGroups " +
		"[apps], resources [deployments, statefulsets, daemonsets, replicasets], verbs [get, " +
		"list, watch, create, update, patch, delete]. The published chart grants none of these 28 " +
		"cells under either entry of rbacValuesMatrix; where it grants other cells of the same " +
		"resources those agree and are not listed here. Measured 2026-09-15. Recorded and not " +
		"converged: narrowing the bootstrap side changes the RBAC Helmfile installs, and nothing " +
		"in B-001 measured what depends on it."
	reasonBootBatch45 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:50-52 grants apiGroups " +
		"[batch], resources [jobs, cronjobs], verbs [get, list, watch, create, update, patch, " +
		"delete]. The published chart grants none of these 14 cells under either entry of " +
		"rbacValuesMatrix; where it grants other cells of the same resources those agree and are " +
		"not listed here. Measured 2026-09-15. Recorded and not converged: narrowing the " +
		"bootstrap side changes the RBAC Helmfile installs, and nothing in B-001 measured what " +
		"depends on it."
	reasonBootNetworkingk8sio49 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:54-56 grants apiGroups " +
		"[networking.k8s.io], resources [ingresses, networkpolicies], verbs [get, list, watch, " +
		"create, update, patch, delete]. The published chart grants none of these 14 cells under " +
		"either entry of rbacValuesMatrix; where it grants other cells of the same resources " +
		"those agree and are not listed here. Measured 2026-09-15. Recorded and not converged: " +
		"narrowing the bootstrap side changes the RBAC Helmfile installs, and nothing in B-001 " +
		"measured what depends on it."
	reasonBootTraefikio52 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:57-59 grants apiGroups " +
		"[traefik.io], resources [ingressroutes, middlewares], verbs [get, list, watch, create, " +
		"update, patch, delete]. The published chart grants none of these 14 cells under either " +
		"entry of rbacValuesMatrix; where it grants other cells of the same resources those agree " +
		"and are not listed here. Measured 2026-09-15. Recorded and not converged: narrowing the " +
		"bootstrap side changes the RBAC Helmfile installs, and nothing in B-001 measured what " +
		"depends on it."
	reasonBootExternalsecretsio56 = "bootstrap-only: infra/helm/loopzero-api/templates/serviceaccount.yaml:61-63 grants apiGroups " +
		"[external-secrets.io], resources [externalsecrets, secretstores, clustersecretstores], " +
		"verbs [get, list, watch, create, update, patch, delete]. The published chart grants none " +
		"of these 21 cells under either entry of rbacValuesMatrix; where it grants other cells of " +
		"the same resources those agree and are not listed here. Measured 2026-09-15. Recorded " +
		"and not converged: narrowing the bootstrap side changes the RBAC Helmfile installs, and " +
		"nothing in B-001 measured what depends on it."
	reasonPubArgoprojio23 = "published-only: charts/loopzero-api/templates/clusterrole.yaml:23-25 grants apiGroups " +
		"[argoproj.io], resources [workflows, workflows/status, workflowtemplates, " +
		"clusterworkflowtemplates], verbs [get, list, watch, create, update, patch, delete]. The " +
		"bootstrap chart grants none of these 14 cells under either entry of rbacValuesMatrix. " +
		"Measured 2026-09-15. Recorded as an asymmetry of the published chart, which is installed " +
		"by ArgoCD rather than by Helmfile."
	reasonPubCore45 = "published-only: charts/loopzero-api/templates/clusterrole.yaml:45-47 grants apiGroups " +
		"[(core)], resources [services, pods, pods/log], verbs [get, list, watch]. The bootstrap " +
		"chart grants none of these 1 cell under either entry of rbacValuesMatrix. Measured " +
		"2026-09-15. Recorded as an asymmetry of the published chart, which is installed by " +
		"ArgoCD rather than by Helmfile."
)

// approvedRBACCells is keyed "published:bootstrap:apiGroup/resource/verb".
//
// Every divergent cell measured on this tree has exactly one entry. The gate is green
// because the divergence is RECORDED, which is strictly better than the previous green:
// green because nobody looked. It does NOT say the pair is in parity, and it does not
// say the grants are appropriate. Two entries say so out loud — they carry
// statusUndecided, they are printed by name on every run, and the decision they wait on
// belongs to B-048, not to this gate.
var approvedRBACCells = map[string]rbacApproval{
	// --- the two security cells: RECORDED, NOT APPROVED (B-001) ---
	//
	// Both are bootstrap-only under BOTH entries of the matrix, so neither is an
	// artefact of a flag state. Neither is declared as intended: nothing measured
	// in B-001 establishes a caller for either grant, and a gate asserting they
	// were fine would be asserting a security judgement nobody made.
	"loopzero-api:loopzero-api:(core)/secrets/delete": {
		ADR:    "B-001",
		Status: statusUndecided,
		Reason: "bootstrap grants cluster-wide secrets including delete, unconditionally " +
			"(infra/helm/loopzero-api/templates/serviceaccount.yaml:40-42). The published chart " +
			"gates secrets behind rbac.legacyClusterWideSecrets and omits delete from the verb " +
			"list under EVERY value of that flag (charts/loopzero-api/templates/clusterrole.yaml:" +
			"40-43, whose own comment calls the cluster-wide grant a catastrophic blast " +
			"radius). Live unreviewed asymmetry; the decision belongs to B-048, not to this gate.",
	},
	"loopzero-api:loopzero-api:(core)/pods/exec/create": {
		ADR:    "B-001",
		Status: statusUndecided,
		Reason: "bootstrap grants pods/exec create cluster-wide " +
			"(infra/helm/loopzero-api/templates/serviceaccount.yaml:43-45); the published chart " +
			"omits pods/exec entirely. Nothing measured in B-001 establishes a caller for it. " +
			"Live unreviewed asymmetry, owned by B-048.",
	},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:23-25 — 7 cell(s)
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/cronworkflows/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio23},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:36-38 — 14 cell(s)
	"loopzero-api:loopzero-api:argoproj.io/applications/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/applications/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/applications/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/applications/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/applications/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/applications/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/applications/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/create":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/delete":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/get":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/list":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/patch":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/update":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},
	"loopzero-api:loopzero-api:argoproj.io/appprojects/watch":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootArgoprojio31},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:40-42 — 31 cell(s); 1 more from this rule is recorded above as undecided
	"loopzero-api:loopzero-api:(core)/configmaps/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/configmaps/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/configmaps/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/configmaps/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/configmaps/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/configmaps/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/configmaps/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/create":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/delete":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/get":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/list":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/patch":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/update":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/events/watch":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/namespaces/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/namespaces/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/namespaces/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/pods/create":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/pods/delete":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/pods/patch":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/pods/update":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/secrets/create":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/secrets/get":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/secrets/list":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/secrets/patch":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/secrets/update":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/secrets/watch":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/services/create":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/services/delete":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/services/patch":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},
	"loopzero-api:loopzero-api:(core)/services/update":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore35},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:43-45 — 3 cell(s); 1 more from this rule is recorded above as undecided
	"loopzero-api:loopzero-api:(core)/pods/exec/get":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore38},
	"loopzero-api:loopzero-api:(core)/pods/exec/list":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore38},
	"loopzero-api:loopzero-api:(core)/pods/log/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootCore38},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:47-49 — 28 cell(s)
	"loopzero-api:loopzero-api:apps/daemonsets/create":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/daemonsets/delete":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/daemonsets/get":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/daemonsets/list":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/daemonsets/patch":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/daemonsets/update":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/daemonsets/watch":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/create":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/delete":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/get":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/list":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/patch":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/update":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/deployments/watch":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/create":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/delete":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/get":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/list":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/patch":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/update":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/replicasets/watch":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},
	"loopzero-api:loopzero-api:apps/statefulsets/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootApps42},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:50-52 — 14 cell(s)
	"loopzero-api:loopzero-api:batch/cronjobs/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/cronjobs/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/cronjobs/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/cronjobs/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/cronjobs/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/cronjobs/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/cronjobs/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/create":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/delete":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/get":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/list":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/patch":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/update":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},
	"loopzero-api:loopzero-api:batch/jobs/watch":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootBatch45},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:54-56 — 14 cell(s)
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/create":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/delete":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/get":          {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/list":         {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/patch":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/update":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/ingresses/watch":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},
	"loopzero-api:loopzero-api:networking.k8s.io/networkpolicies/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootNetworkingk8sio49},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:57-59 — 14 cell(s)
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/ingressroutes/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/create":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/delete":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/get":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/list":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/patch":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/update":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},
	"loopzero-api:loopzero-api:traefik.io/middlewares/watch":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootTraefikio52},

	// bootstrap-only — infra/helm/loopzero-api/templates/serviceaccount.yaml:61-63 — 21 cell(s)
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/clustersecretstores/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/create":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/delete":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/get":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/list":       {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/patch":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/update":     {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/externalsecrets/watch":      {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/create":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/delete":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/get":           {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/list":          {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/patch":         {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/update":        {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},
	"loopzero-api:loopzero-api:external-secrets.io/secretstores/watch":         {ADR: "B-001", Status: statusDeclared, Reason: reasonBootExternalsecretsio56},

	// published-only — charts/loopzero-api/templates/clusterrole.yaml:23-25 — 14 cell(s)
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/create": {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/delete": {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/get":    {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/list":   {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/patch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/update": {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/clusterworkflowtemplates/watch":  {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/create":         {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/delete":         {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/get":            {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/list":           {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/patch":          {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/update":         {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},
	"loopzero-api:loopzero-api:argoproj.io/workflows/status/watch":          {ADR: "B-001", Status: statusDeclared, Reason: reasonPubArgoprojio23},

	// published-only — charts/loopzero-api/templates/clusterrole.yaml:45-47 — 1 cell(s)
	"loopzero-api:loopzero-api:(core)/pods/log/watch": {ADR: "B-001", Status: statusDeclared, Reason: reasonPubCore45},
}
