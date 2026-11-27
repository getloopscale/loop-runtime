// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// The single-status-writer contract for the platform package.
//
// The contract (ADR 2026-08-12-status-writer-contract): a CRD's .status has
// exactly ONE writer, and it persists with a merge patch derived from a base
// snapshot. Update sends the WHOLE object, so a status write races any
// concurrent spec or metadata write and the loser's change vanishes silently.
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
)

// notYetConverted are the controllers in this package still holding bare
// Status().Update calls, with the count measured at the time of writing.
//
// An exemption list rather than a file-scoped scan, and the difference matters.
// Scoping the scan to one file was measured bypassable in the preview slice: a
// helper dropped in a sibling file passed. Scanning the package closes that —
// but this package holds THREE controllers and B-085 converts one per slice, so
// without exemptions the scan would fail on files the slice does not touch.
//
// The count is part of the exemption on purpose. Adding a write to an exempt
// controller trips the check just as surely as adding one to a converted file,
// so the list can only shrink.
//
// EMPTY since 2026-08-13: every controller in this package writes status by
// merge patch. The map and its frozen twin stay rather than being deleted —
// they are what makes the next bare Update in this package fail loudly instead
// of arriving with an exemption attached.
var notYetConverted = map[string]exemption{}

type exemption struct {
	writes int
	since  string
}

// exemptedFiles freezes the SET, not just the counts.
//
// The review showed the gap: the count locks growth INSIDE an exempt file, but
// nothing stopped a new key being added — so "the list only shrinks" was a
// convention I asserted in a comment and nowhere else. Freezing the set makes
// adding an exemption a deliberate edit to two places, which is the point.
var exemptedFiles = []string{}

func TestPlatform_TheExemptionListOnlyShrinks(t *testing.T) {
	if len(notYetConverted) != len(exemptedFiles) {
		t.Fatalf("the exemption map has %d entries and the frozen set %d — an exemption was added or "+
			"removed in one place only", len(notYetConverted), len(exemptedFiles))
	}
	for _, name := range exemptedFiles {
		e, ok := notYetConverted[name]
		if !ok {
			t.Errorf("%s is frozen as exempt but absent from the map", name)
			continue
		}
		if e.since == "" {
			t.Errorf("%s carries no `since` date — an exemption without one cannot be aged out "+
				"(rules/code-quality-golden-rule.md § 4)", name)
		}
	}
	for name := range notYetConverted {
		found := false
		for _, f := range exemptedFiles {
			if f == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was added to the exemption map without being frozen — the list only shrinks; "+
				"a NEW exemption is a decision, not an edit (B-085)", name)
		}
	}
}

// packageSource returns each production file's code with comments stripped.
//
// Stripping is not cosmetic: comments here quote the forbidden pattern to
// explain why it is forbidden, and a naive scan matches the explanation instead
// of the code. Both comment forms — the line form and the block form, the second
// of which produced a false FAIL in the preview slice.
func packageSource(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(n))
		if err != nil {
			t.Fatalf("reading %s: %v", n, err)
		}
		out[n] = stripGoComments(string(b))
	}
	if len(out) == 0 {
		t.Fatal("no production file read; a check that read nothing verified nothing")
	}
	return out
}

func stripGoComments(src string) string {
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

// Every file in the package is either converted or exempt with its measured count.
func TestPlatform_BareStatusUpdatesAreConvertedOrDeclared(t *testing.T) {
	for name, src := range packageSource(t) {
		got := strings.Count(src, "Status().Update(")
		e, exempt := notYetConverted[name]
		want := e.writes
		switch {
		case !exempt && got > 0:
			t.Errorf("%s holds %d bare Status().Update call(s). The contract persists status by merge "+
				"patch off a base snapshot, never by Update — convert it, or declare it in "+
				"notYetConverted with its count (B-085).", name, got)
		case exempt && got != want:
			t.Errorf("%s is declared with %d bare Status().Update call(s) but holds %d. The list only "+
				"shrinks: convert the file and drop its entry, or fix the count — never grow it.",
				name, want, got)
		}
	}
}

// The converted controller writes by merge patch, and says so in code.
//
// Asserted apart from the absence above: a file with zero Updates and zero
// Patches satisfies the first check while writing no status at all — a worse
// defect than the one being fixed.
func TestPlatform_ConvertedControllersPersistStatusByMergePatch(t *testing.T) {
	pkg := packageSource(t)
	for _, name := range []string{
		"platformbootstrap_controller.go",
		"clustercredential_controller.go",
		"gitrepositorybinding_controller.go",
	} {
		src := pkg[name]
		if src == "" {
			t.Fatalf("%s not read", name)
		}
		assertMergePatchShape(t, name, src)
	}
}

func assertMergePatchShape(t *testing.T, name, src string) {
	t.Helper()
	if !strings.Contains(src, "Status().Patch(") {
		t.Fatalf("%s has no Status().Patch — a controller that stopped writing status satisfies the "+
			"no-Update check for the wrong reason", name)
	}
	if !strings.Contains(src, "client.MergeFrom(") {
		t.Errorf("%s: Status().Patch is present but not derived from client.MergeFrom — the patch must "+
			"be a diff against a base snapshot, or it is an Update wearing a different name", name)
	}
}

// The snapshot placement, observed by RUNNING Reconcile.
//
// The first version compared string offsets — `base :=` after `AddFinalizer(` —
// and the review broke it both ways. Extracting the finalizer into a helper
// defined ABOVE Reconcile moved its offset down, so a snapshot taken before the
// call passed; and if this controller ever gains a delete-path base like
// preview_controller.go:549, the first `base :=` lands before AddFinalizer and
// correct code FAILS. Offsets are text, and placement is execution.
//
// It also missed the direction that loses a write silently: mutating .Status on
// the line ABOVE the snapshot leaves the change in both objects, so the diff is
// empty and the field is never persisted — no error anywhere. That is the case
// CLAUDE.md's canonical snippet warns about by name ("right after r.Get, BEFORE
// mutating .Status"), and nothing here checked it.
//
// Both are the same fix: capture what the controller actually emits.
func TestPlatformBootstrap_ReconcileEmitsAPatchThatIsCorrectlyAnchored(t *testing.T) {
	bodies := capturedStatusPatches(t, &loopruntimev1.PlatformBootstrap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "platform",
			Namespace: "loop-runtime-system",
			// Non-zero on purpose. With Generation at its zero value, a mutation
			// like `Status.ObservedGeneration = pb.Generation` writes 0, omitempty
			// drops it, and the lost-write comparison below cannot see the very
			// thing it exists to see — the fixture would make the guard vacuous.
			Generation: 7,
			// No finalizer: Reconcile must add one, and that Update is the
			// metadata write the snapshot has to come after.
		},
	})

	// The controller is the single status writer: every write folds the
	// readiness summary in, so the Ready condition travels in the same patch as
	// the conditions it summarises, and no second, re-fetching writer exists.
	if len(bodies) == 0 {
		t.Fatal("no status patch observed — an incomplete observation is not a clean one")
	}
	assertEveryPatchCarriesTheReadySummary(t, bodies)
	for i, body := range bodies {
		// (1) No resourceVersion: a snapshot taken before the finalizer write
		// inherits the stale one and turns the patch into an optimistic lock
		// frozen at snapshot time — a WIDER conflict window than the refetch this
		// contract removes. Measured as a real 409 in slice-18.
		if strings.Contains(body, "resourceVersion") {
			t.Errorf("patch #%d carries resourceVersion — capture `base` AFTER the finalizer Update "+
				"(B-085).\npatch: %s", i+1, body)
		}
		// (2) It carries the transition. A phase-less patch means the snapshot was
		// taken AFTER the mutation, so the diff cancelled out and the write
		// persists nothing while reporting success.
		//
		// Named field on purpose, and the general case is guarded below: a patch
		// cannot reveal a field that was mutated before the snapshot, because
		// such a field is simply ABSENT — and absence is indistinguishable from
		// "was not supposed to change". Only the persisted end state can tell.
		if !strings.Contains(body, `"phase"`) {
			t.Errorf("patch #%d does not carry the phase transition it exists to persist — the "+
				"snapshot was taken after .Status was mutated, so the diff is empty.\npatch: %s", i+1, body)
		}
	}
}

// capturedStatusPatches runs one Reconcile against a fake client and returns the
// raw body of every status patch the controller emitted.
//
// The interceptor sits where the write actually goes, so it cannot be satisfied
// by a helper, a rename, or a synthetic object built to match.
func capturedStatusPatches(t *testing.T, seed *loopruntimev1.PlatformBootstrap) []string {
	t.Helper()

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{loopruntimev1.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}

	var bodies []string
	var lost []string
	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(seed).
		WithStatusSubresource(&loopruntimev1.PlatformBootstrap{}).
		Build()

	c := interceptor.NewClient(base, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
			patch client.Patch, opts ...client.SubResourcePatchOption) error {
			data, err := patch.Data(obj)
			if err != nil {
				return err
			}
			bodies = append(bodies, string(data))
			// What the controller HOLDS at write time, before the round trip.
			want, _ := json.Marshal(obj.(*loopruntimev1.PlatformBootstrap).Status)
			if err := cl.Status().Patch(ctx, obj, patch, opts...); err != nil {
				return err
			}
			// And what actually landed. A field mutated BEFORE the snapshot is in
			// both base and obj, so the diff omits it and it never reaches the
			// server — with no error anywhere. The patch body cannot show that;
			// only this comparison can.
			var got loopruntimev1.PlatformBootstrap
			if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), &got); err != nil {
				return err
			}
			persisted, _ := json.Marshal(got.Status)
			if string(want) != string(persisted) {
				lost = append(lost, fmt.Sprintf("held %s\n    persisted %s", want, persisted))
			}
			return nil
		},
	})

	r := &PlatformBootstrapReconciler{Client: c}
	// The result is not asserted: this Reconcile is expected to fail somewhere
	// downstream against a fake cluster. What is under test is the SHAPE of the
	// status writes it emits on the way.
	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	})
	if len(lost) > 0 {
		t.Errorf("%d status write(s) did not persist what the controller held. A field mutated BEFORE "+
			"the snapshot sits in both base and object, so the merge diff omits it and the write is "+
			"silently empty for that field — the failure mode CLAUDE.md's canonical snippet names "+
			"(\"right after r.Get, BEFORE mutating .Status\"):\n  %s", len(lost), strings.Join(lost, "\n  "))
	}
	return bodies
}

// The same execution guard for the ClusterCredential controller.
//
// A second helper rather than generics: the interceptor closure needs the
// concrete type to read .Status back, and two twelve-line helpers are cheaper to
// read than one generic one with a type-parameter dance.
func TestClusterCredential_ReconcileEmitsAPatchThatIsCorrectlyAnchored(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{loopruntimev1.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	seed := &loopruntimev1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-0",
			Namespace: "loop-runtime-system",
			// Non-zero for the same reason as the sibling test: with Generation at
			// zero a lost write of ObservedGeneration is erased by omitempty and
			// the comparison below sees nothing.
			Generation: 7,
		},
		Spec: loopruntimev1.ClusterCredentialSpec{CellID: "cell-0"},
	}

	var bodies []string
	var lost []string
	c := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed).
			WithStatusSubresource(&loopruntimev1.ClusterCredential{}).Build(),
		interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
				patch client.Patch, opts ...client.SubResourcePatchOption) error {
				data, err := patch.Data(obj)
				if err != nil {
					return err
				}
				bodies = append(bodies, string(data))
				want, _ := json.Marshal(obj.(*loopruntimev1.ClusterCredential).Status)
				if err := cl.Status().Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				var got loopruntimev1.ClusterCredential
				if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), &got); err != nil {
					return err
				}
				if persisted, _ := json.Marshal(got.Status); string(want) != string(persisted) {
					lost = append(lost, fmt.Sprintf("held %s\n    persisted %s", want, persisted))
				}
				return nil
			},
		})

	r := &ClusterCredentialReconciler{Client: c}
	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	})

	if len(bodies) == 0 {
		t.Fatal("Reconcile emitted no status patch — the guard observed nothing, which is not the " +
			"same as observing a clean run")
	}
	if len(lost) > 0 {
		t.Errorf("%d status write(s) did not persist what the controller held — a field mutated BEFORE "+
			"the snapshot sits in both base and object, so the diff omits it and the write is silently "+
			"empty for that field:\n  %s", len(lost), strings.Join(lost, "\n  "))
	}
	// The last patch is the deferred readiness summary, which carries the lock by
	// design; everything before it is the controller body and must not.
	for i, body := range bodies[:max(0, len(bodies)-1)] {
		if strings.Contains(body, "resourceVersion") {
			t.Errorf("body patch #%d carries resourceVersion — capture `base` AFTER EnsureFinalizer "+
				"(B-085).\npatch: %s", i+1, body)
		}
	}
}

// The same execution guard for the GitRepositoryBinding controller.
//
// Scope note, learned the hard way in slice-23: this catches the two classes the
// fake client CAN see — a stale resourceVersion riding the patch, and a write
// the controller intended that did not persist. It cannot see absent-versus-false,
// because the fake client stores typed objects and re-materialises zero values.
// For that class the guard is the envtest suite reading the unstructured object.
// Measured here first: this CRD has no required status field and no zero-valued
// status field without omitempty, so the class does not arise.
func TestGitRepositoryBinding_ReconcileEmitsAPatchThatIsCorrectlyAnchored(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{loopruntimev1.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	seed := &loopruntimev1.GitRepositoryBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "gh", Namespace: "loop-runtime-system", Generation: 7},
		Spec: loopruntimev1.GitRepositoryBindingSpec{
			RepoURL: "https://example.invalid/x.git",
			CredentialSecretRef: loopruntimev1.CredentialSecretRef{
				Name: "creds", Key: "token", Namespace: "loop-runtime-system",
			},
		},
	}
	// The Secret EXISTS on purpose.
	//
	// The first version of this guard left it absent, so resolveCredential failed
	// and only the FIRST of the three patch sites ever ran. The third one is the
	// one that writes the non-condition fields — CredentialHash and
	// ObservedGeneration — which are exactly the shape the clustercredential
	// slice showed can vanish from a merge patch. A guard that never reaches the
	// site it most needs to watch is a guard by name. Review of slice-24.
	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "loop-runtime-system"},
		Data:       map[string][]byte{"token": []byte("ghp_probe")},
	}

	var bodies []string
	var lost []string
	c := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed, creds).
			WithStatusSubresource(&loopruntimev1.GitRepositoryBinding{}).Build(),
		interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
				patch client.Patch, opts ...client.SubResourcePatchOption) error {
				data, err := patch.Data(obj)
				if err != nil {
					return err
				}
				bodies = append(bodies, string(data))
				want, _ := json.Marshal(obj.(*loopruntimev1.GitRepositoryBinding).Status)
				if err := cl.Status().Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				var got loopruntimev1.GitRepositoryBinding
				if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), &got); err != nil {
					return err
				}
				if persisted, _ := json.Marshal(got.Status); string(want) != string(persisted) {
					lost = append(lost, fmt.Sprintf("held %s\n    persisted %s", want, persisted))
				}
				return nil
			},
		})

	r := &GitRepositoryBindingReconciler{Client: c}
	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	})

	if len(bodies) == 0 {
		t.Fatal("no status patch observed — an incomplete observation is not a clean one")
	}
	if len(lost) > 0 {
		t.Errorf("%d status write(s) did not persist what the controller held:\n  %s",
			len(lost), strings.Join(lost, "\n  "))
	}
	assertEveryPatchCarriesTheReadySummary(t, bodies)
	for i, body := range bodies {
		if strings.Contains(body, "resourceVersion") {
			t.Errorf("body patch #%d carries resourceVersion — capture `base` AFTER EnsureFinalizer "+
				"(B-085).\npatch: %s", i+1, body)
		}
	}
}

// assertEveryPatchCarriesTheReadySummary checks the single-status-writer
// contract: each status write carries the kstatus Ready condition it computed,
// so no pass leaves Ready at what a previous pass decided.
func assertEveryPatchCarriesTheReadySummary(t *testing.T, bodies []string) {
	t.Helper()
	for i, body := range bodies {
		if !strings.Contains(body, `"type":"Ready"`) {
			t.Errorf("status patch #%d does not carry the Ready summary; the controller must fold it "+
				"into every write (ADR-0025 D3).\npatch: %s", i+1, body)
		}
	}
}
