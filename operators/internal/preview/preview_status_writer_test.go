// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// The single-status-writer contract, asserted for the Preview controller.
//
// The contract (ADR 2026-08-12-status-writer-contract, meeting record
// MEET-ASYNC-AMENDMENT-2026-08-12-006): a CRD's .status has exactly ONE writer,
// and it persists with a merge patch derived from a base snapshot. A merge patch
// carries no resourceVersion, so it never 409s against a concurrent spec or
// metadata write, and it sends only the fields it changed, so it cannot erase
// what it never read.
package preview

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
)

// controllerSource reads the WHOLE package with comments removed.
//
// The package, not one file. The review moved the forbidden call into a new
// `preview_bypass_helper.go` beside the controller and the earlier
// single-file version passed — and the package already has
// `preview_resources.go`, so a helper landing next door is the ordinary case,
// not a contrived one.
//
// Stripping comments is not cosmetic either. The comments here deliberately
// quote the forbidden patterns to explain why they are forbidden, and a naive
// scan then matches the explanation instead of the code, reporting a defect the
// same commit just removed. Measured three times across this series. Block
// comments are stripped too: the first version handled only `//`, and the review
// produced a false FAIL by writing the pattern inside a `/* */`.
func controllerSource(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	var code []string
	files := 0
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(n))
		if err != nil {
			t.Fatalf("reading %s: %v", n, err)
		}
		files++
		code = append(code, stripComments(string(b)))
	}
	if files == 0 {
		t.Fatal("no production file read; a check that read nothing verified nothing")
	}
	out := strings.Join(code, "\n")
	if strings.TrimSpace(out) == "" {
		t.Fatal("stripping comments left no code; the scan would pass for the wrong reason")
	}
	return out
}

// stripComments removes both comment forms, leaving code only.
func stripComments(src string) string {
	// Block comments first: a `//` inside one must not survive the line pass.
	for {
		i := strings.Index(src, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(src[i:], "*/")
		if j < 0 {
			src = src[:i]
			break
		}
		src = src[:i] + src[i+j+2:]
	}
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// No bare Status().Update anywhere in this controller.
//
// Measured before the conversion: SIX, in four functions — failPreviewCondition
// (which FIVE call sites share), reconcileCreating (two), reconcileActive, and
// reconcileDelete (two). The failure they share is the lost update: Update sends
// the WHOLE object, so a status write races any concurrent spec or metadata
// write and the loser's change disappears without an error anywhere.
func TestPreviewController_HasNoBareStatusUpdate(t *testing.T) {
	src := controllerSource(t)
	if n := strings.Count(src, "Status().Update("); n != 0 {
		var lines []string
		for i, l := range strings.Split(src, "\n") {
			if strings.Contains(l, "Status().Update(") {
				lines = append(lines, strings.TrimSpace(l)+"  (line ~"+strconv.Itoa(i+1)+" of the comment-stripped package)")
			}
		}
		t.Errorf("%d bare Status().Update call(s) remain — the contract persists status by merge patch "+
			"off a base snapshot, never by Update:\n  %s", n, strings.Join(lines, "\n  "))
	}
}

// And the shape that replaces it is present.
//
// Asserted separately from the absence above: a file with zero Updates and zero
// Patches would satisfy the first check while writing no status at all, which is
// a worse defect than the one being fixed.
func TestPreviewController_PersistsStatusByMergePatch(t *testing.T) {
	src := controllerSource(t)
	if !strings.Contains(src, "Status().Patch(") {
		t.Fatal("no Status().Patch call — a controller that stopped writing status satisfies the " +
			"no-Update check for the wrong reason")
	}
	if !strings.Contains(src, "client.MergeFrom(") {
		t.Error("Status().Patch is present but not derived from client.MergeFrom — the patch must be " +
			"a diff against a base snapshot, or it is an Update wearing a different name")
	}
}

// The refetch-then-restore dance, forbidden by name in the contract.
//
// Its defining feature is the SHAPE: a Get whose only purpose is to obtain a
// resourceVersion that will not conflict, followed by restoring the mutated
// status onto the refetched object. No runtime assertion distinguishes
// "re-fetched then overwrote" from "wrote once" when nothing else is writing.
func TestPreviewController_HasNoRefetchRestoreDance(t *testing.T) {
	src := controllerSource(t)
	// The tell: a Get into a fresh variable followed by a status assignment from
	// the stale copy, then Update. Matching the Update alone is enough — the
	// check above already forbids it — so this asserts the helper is not
	// reintroduced under another name.
	for _, forbidden := range []string{"UpdateStatusWithRetry", "RetryOnConflict"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("%s reintroduces the refetch dance the contract forbids by name", forbidden)
		}
	}
}

// The decisive assertion — and it runs the real Reconcile.
//
// The first version of this test built `base` and `obj` BY HAND with matching
// resourceVersions and asserted that client.MergeFrom produced no
// resourceVersion. The review demolished it: that is a property of
// controller-runtime, not of this controller. Reconcile was never called, so the
// thing the test existed to guard — WHERE the snapshot is taken — was not
// observed at all.
//
// Proven by mutation: moving `base := preview.DeepCopy()` to before the
// finalizer block — the exact defect slice-18 paid for with a real 409 — left
// the entire suite green, envtest included.
//
// So the patch body is captured as the controller actually emits it, through an
// interceptor on the fake client. What is asserted is the contract: the body
// carries no resourceVersion, because a snapshot taken after the finalizer write
// shares the object's version and the diff is therefore empty of it.
func TestPreviewController_ReconcileEmitsAStatusPatchWithoutResourceVersion(t *testing.T) {
	bodies := capturedStatusPatches(t, &loopruntimev1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pr-42",
			Namespace:  "loopruntime-system",
			Generation: 1,
			// No finalizer: Reconcile must add one, which is the metadata write
			// that bumps resourceVersion and is exactly what the snapshot must
			// come after.
		},
		Spec: loopruntimev1.PreviewEnvironmentSpec{TenantID: "tn-x", ProjectID: "api", PRNumber: 42},
	})

	// The controller is the single status writer: every write folds the
	// readiness summary in and none carries a resourceVersion, because the
	// snapshot is taken after the finalizer write (B-085).
	if len(bodies) == 0 {
		t.Fatal("no status patch observed — the observation is incomplete, which is not the same as clean")
	}
	for i, body := range bodies {
		if strings.Contains(body, "resourceVersion") {
			t.Errorf("status patch #%d carries resourceVersion, which makes it an optimistic lock "+
				"frozen at snapshot time — a WIDER conflict window than the refetch this contract "+
				"removes. Capture `base` AFTER the finalizer Update (B-085).\npatch: %s", i+1, body)
		}
		if !strings.Contains(body, `"type":"Ready"`) {
			t.Errorf("status patch #%d does not carry the Ready summary; every write must fold it "+
				"in (ADR-0025 D3).\npatch: %s", i+1, body)
		}
	}
}

// And the mutation is producible: a snapshot taken before the finalizer write
// DOES leak the version.
//
// Without this the assertion above could be green for a reason having nothing to
// do with placement — which is precisely how the synthetic version fooled me.
func TestPreviewStatusPatch_ASnapshotTakenBeforeTheFinalizerLeaksTheVersion(t *testing.T) {
	stale := &loopruntimev1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42", Namespace: "loopruntime-system", ResourceVersion: "12345"},
	}
	obj := stale.DeepCopy()
	obj.ResourceVersion = "12346" // the finalizer Update bumped it
	obj.Status.Phase = loopruntimev1.PreviewPhaseActive

	data, err := client.MergeFrom(stale).Data(obj)
	if err != nil {
		t.Fatalf("building the merge patch: %v", err)
	}
	if !strings.Contains(string(data), "resourceVersion") {
		t.Fatal("a snapshot taken before the finalizer write was expected to leak resourceVersion; " +
			"it did not, so the assertion above proves nothing")
	}
}

// capturedStatusPatches runs one Reconcile against a fake client and returns the
// raw body of every status patch the controller emitted.
//
// The interceptor is the honest observer: it sits where the controller's write
// actually goes, so it cannot be satisfied by a helper, a rename, or a synthetic
// object built to match.
func capturedStatusPatches(t *testing.T, seed *loopruntimev1.PreviewEnvironment) []string {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := loopruntimev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	var bodies []string
	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(seed).
		WithStatusSubresource(&loopruntimev1.PreviewEnvironment{}).
		Build()

	c := interceptor.NewClient(base, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
			patch client.Patch, opts ...client.SubResourcePatchOption) error {
			data, err := patch.Data(obj)
			if err != nil {
				return err
			}
			bodies = append(bodies, string(data))
			return cl.Status().Patch(ctx, obj, patch, opts...)
		},
	})

	r := &PreviewReconciler{Client: c, Scheme: scheme, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(c)}
	// The result is not asserted: this Reconcile is expected to fail somewhere
	// downstream against a fake cluster. What is under test is the SHAPE of the
	// status writes it emits on the way, not whether the reconcile succeeds.
	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	})
	return bodies
}
