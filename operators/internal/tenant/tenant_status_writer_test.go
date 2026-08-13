// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// B-085 / ADR 2026-08-12-status-writer-contract (D3).
//
// This controller persisted `.status` with the pattern the invariant names and
// forbids verbatim — "Never use the refetch-then-restore Status().Update dance
// (lost update)":
//
//	savedStatus := tenant.Status
//	r.Get(ctx, key, &tenant)          // fresh resourceVersion
//	tenant.Status = savedStatus       // stale status back on top
//	r.Status().Update(ctx, &tenant)   // overwrites whatever landed in between
//
// The re-fetch is what makes it dangerous rather than merely wrong: it acquires a
// resourceVersion that will NOT conflict, so a write from another actor between
// the Get and the Update disappears silently instead of surfacing a 409.
//
// A merge patch taken off a snapshot carries no resourceVersion and mentions only
// the fields it changed, so it cannot erase what it never read.
package tenant

import (
	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"testing"
)

// controllerSource reads this controller with COMMENT LINES REMOVED.
//
// Stripping them is not cosmetic. The comments here deliberately quote the
// forbidden patterns to explain why they are forbidden — and a naive scan then
// matches the explanation instead of the code, reporting a defect that the same
// commit just removed. Measured three times across this series before it was
// worth encoding.
func controllerSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("tenant_controller.go")
	if err != nil {
		t.Fatalf("reading the controller: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("the controller file is empty; a check that read nothing verified nothing")
	}
	var code []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		code = append(code, line)
	}
	out := strings.Join(code, "\n")
	if strings.TrimSpace(out) == "" {
		t.Fatal("stripping comments left no code; the scan would pass for the wrong reason")
	}
	return out
}

// The dance itself. Asserted structurally because its defining feature is the
// SHAPE — a Get whose only purpose is to obtain a resourceVersion that will not
// conflict. No runtime assertion distinguishes "re-fetched then overwrote" from
// "wrote once" when nothing else is writing; the shape is the evidence.
func TestTenantController_HasNoRefetchRestoreDance(t *testing.T) {
	src := controllerSource(t)
	if strings.Contains(src, "savedStatus") {
		t.Error("tenant_controller.go still saves and restores `.Status` around a re-fetch. That is " +
			"the refetch-then-restore dance CLAUDE.md forbids by name: the fresh resourceVersion " +
			"cannot conflict, so a concurrent write vanishes instead of surfacing a 409 (B-085).")
	}
}

// Gap closed after review: the presence checks below would pass with a leftover
// bare `Status().Update` still in the file — and a bare Update is exactly what
// the ADR retires. This asserts its ABSENCE, which is the stronger claim.
func TestTenantController_HasNoBareStatusUpdate(t *testing.T) {
	src := controllerSource(t)
	if strings.Contains(src, "r.Status().Update(") {
		t.Error("tenant_controller.go still calls Status().Update. The contract is a merge patch off " +
			"a snapshot; an Update replays the whole status subresource at the values read, which " +
			"is what the migration removes (B-085).")
	}
}

// The scan above covers ONE file. `reconcileDelete` lives in tenant_deletion.go,
// is called from this same Reconcile, and still writes status with bare Updates —
// so "the tenant controller is migrated" is true of the file and NOT of the
// controller. Pinning the count keeps that distinction from being lost, and makes
// the next slice's scope visible instead of assumed.
func TestTenantDeletion_RemainingBareStatusUpdatesAreKnown(t *testing.T) {
	b, err := os.ReadFile("tenant_deletion.go")
	if err != nil {
		t.Fatalf("reading tenant_deletion.go: %v", err)
	}
	var code []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		code = append(code, line)
	}
	const known = 2
	got := strings.Count(strings.Join(code, "\n"), "r.Status().Update(")
	if got > known {
		t.Errorf("tenant_deletion.go has %d bare Status().Update calls, up from the %d measured on "+
			"2026-08-13. The migration is going backwards (B-085).", got, known)
	}
	if got < known {
		t.Errorf("tenant_deletion.go has %d bare Status().Update calls, down from %d — good, but "+
			"this ratchet must be lowered in the same commit, or it stops measuring anything.", got, known)
	}
}

// The mechanism the contract prescribes.
func TestTenantController_PersistsStatusByMergePatch(t *testing.T) {
	src := controllerSource(t)
	if !strings.Contains(src, "client.MergeFrom") {
		t.Error("tenant_controller.go never builds a merge patch. The contract is a patch taken off a " +
			"DeepCopy captured right after the Get — see environment_controller.go for the applied " +
			"model (B-085).")
	}
	if !strings.Contains(src, "r.Status().Patch(") {
		t.Error("tenant_controller.go never calls Status().Patch; every status write still goes " +
			"through Update, which replays the whole subresource at the values read (B-085).")
	}
}

// A status-write failure must not be discarded. The inline readiness summary did
// `_ = r.Status().Update(...)`, so a lost status write left no trace at all —
// and the helper that four sibling controllers already use was fixed in slice-10
// to log it.
func TestTenantController_DoesNotDiscardStatusWriteErrors(t *testing.T) {
	src := controllerSource(t)
	if strings.Contains(src, "_ = r.Status().Update") || strings.Contains(src, "_ = r.Status().Patch") {
		t.Error("a status write result is discarded with `_ =`. CLAUDE.md is explicit that a " +
			"status-write failure is never swallowed; the readiness summary belongs to " +
			"reconcilehelpers.DeferReadinessSummary, which logs it (B-085).")
	}
}

// And the summary must come from the shared helper rather than a seventh
// hand-rolled copy — the shape B-089 measured the cost of.
func TestTenantController_DelegatesReadinessSummaryToTheHelper(t *testing.T) {
	src := controllerSource(t)
	if !strings.Contains(src, "DeferReadinessSummary") {
		t.Error("the readiness summary is still inlined here. Four of the six controllers delegate " +
			"to reconcilehelpers.DeferReadinessSummary, which slice-10 fixed to use a locked merge " +
			"patch and to log the error; a local copy diverges from it silently (B-085).")
	}
}

// THE ASSERTION THAT MATTERS, and the one four structural greps could not make.
//
// An earlier version of this change captured the snapshot ABOVE EnsureFinalizer.
// The four tests above all passed: the dance was gone, MergeFrom was there, no
// error was discarded, the helper was used. And the patch was still wrong —
// `client.MergeFrom` diffs the WHOLE object, EnsureFinalizer bumps
// resourceVersion via Update+Get, so the emitted body carried a STALE optimistic
// lock that a real apiserver honours with a 409. On the first reconcile of every
// Tenant.
//
// The patch builder is a pure function, so the decisive check needs no cluster,
// no fakes and no reconciler: build the patch the controller builds, and read the
// bytes.
func TestTenantStatusPatch_CarriesNoResourceVersion(t *testing.T) {
	// The snapshot, as the controller takes it: after the finalizer write.
	base := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "tn-probe",
			Namespace:       "loopzero-system",
			ResourceVersion: "101",
			Finalizers:      []string{tenantFinalizer},
		},
		Status: loopzerov1.TenantStatus{Phase: loopzerov1.TenantPhase("Provisioning")},
	}

	// The object as the controller holds it at write time: same RV, mutated status.
	obj := base.DeepCopy()
	obj.Status.Phase = loopzerov1.TenantPhase("Active")

	data, err := client.MergeFrom(base).Data(obj)
	if err != nil {
		t.Fatalf("building the merge patch: %v", err)
	}
	body := string(data)

	if strings.Contains(body, "resourceVersion") {
		t.Errorf("the status merge patch carries resourceVersion, which makes it an optimistic "+
			"lock — and one frozen at snapshot time, before the reconcile's network-bound steps. "+
			"That is a WIDER conflict window than the re-fetch this migration removed, not a "+
			"narrower one. Capture `base` after EnsureFinalizer (B-085).\npatch: %s", body)
	}
	if !strings.Contains(body, "status") {
		t.Errorf("the patch does not mention status; a patch that changes nothing writes nothing.\npatch: %s", body)
	}
}

// And the inverse, so the test above cannot pass by the patch being empty: a
// snapshot taken BEFORE the finalizer write does emit the resourceVersion. This
// reproduces the defect, so the assertion above is measuring rather than hoping.
func TestTenantStatusPatch_StaleSnapshotWouldCarryResourceVersion(t *testing.T) {
	before := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-probe", Namespace: "loopzero-system", ResourceVersion: "100"},
		Status:     loopzerov1.TenantStatus{Phase: loopzerov1.TenantPhase("Provisioning")},
	}
	after := before.DeepCopy()
	after.ResourceVersion = "101" // EnsureFinalizer's Update+Get
	after.Finalizers = []string{tenantFinalizer}
	after.Status.Phase = loopzerov1.TenantPhase("Active")

	data, err := client.MergeFrom(before).Data(after)
	if err != nil {
		t.Fatalf("building the merge patch: %v", err)
	}
	if !strings.Contains(string(data), "resourceVersion") {
		t.Fatal("a snapshot taken before the finalizer write did NOT emit resourceVersion — then " +
			"the assertion in the test above proves nothing, because the failure mode it guards " +
			"against cannot be produced")
	}
}
