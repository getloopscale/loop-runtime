// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// The single-status-writer contract for the app package.
//
// Contract: ADR 2026-08-12-status-writer-contract — a CRD's .status has exactly
// ONE writer, persisting with a merge patch derived from a base snapshot. Update
// sends the WHOLE object, so a status write races any concurrent spec or
// metadata write and the loser's change vanishes with no error anywhere.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

// notYetConverted are the files in this package still holding bare
// Status().Update calls, with the count measured at the time of writing.
//
// The set is frozen separately below: the count locks growth INSIDE a file, but
// nothing stops a new key being added, so "the list only shrinks" would be a
// convention asserted in a comment and nowhere else (review of slice-22).
var notYetConverted = map[string]exemption{
	"application_controller.go": {writes: 1, since: "2026-08-14"},
	"application_reaper.go":     {writes: 1, since: "2026-08-14"},
}

type exemption struct {
	writes int
	since  string
}

var exemptedFiles = []string{
	"application_controller.go",
	"application_reaper.go",
}

func TestApp_TheExemptionListOnlyShrinks(t *testing.T) {
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
			t.Errorf("%s carries no `since` date — an exemption without one cannot be aged out", name)
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

// stripGoComments removes BOTH comment forms. The line form alone was measured
// insufficient in slice-21: the forbidden pattern written inside a /* */ block
// produced a false FAIL.
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

func TestApp_BareStatusUpdatesAreConvertedOrDeclared(t *testing.T) {
	for name, src := range packageSource(t) {
		got := strings.Count(src, "Status().Update(")
		e, exempt := notYetConverted[name]
		switch {
		case !exempt && got > 0:
			t.Errorf("%s holds %d bare Status().Update call(s). The contract persists status by merge "+
				"patch off a base snapshot, never by Update — convert it, or declare it in "+
				"notYetConverted with its count (B-085).", name, got)
		case exempt && got != e.writes:
			t.Errorf("%s is declared with %d bare Status().Update call(s) but holds %d. The list only "+
				"shrinks: convert the file and drop its entry, or fix the count — never grow it.",
				name, e.writes, got)
		}
	}
}

// Nobody hand-rolls the readiness summary again.
//
// This file carried its own copy until 2026-08-14, and the copy was the PRE-FIX
// shape: it persisted with Status().Update — so a conditions write, an array that
// RFC 7386 treats as a scalar, could erase a concurrent write of the whole array
// — and it discarded the error, making a dropped status write silent in the one
// place that has nowhere to return to. Slice-10 fixed both in the shared helper;
// this copy never received the fix, because duplicated knowledge does not.
//
// Asserted structurally on purpose: the defect is the DUPLICATION, and a runtime
// assertion cannot tell "called the helper" from "reimplemented it correctly".
//
// One file computes the summary itself: application_controller.go folds it into
// its own Status().Patch, "so the deferred Status().Update is no longer needed
// (single write per pass)", its comment says.
//
// I first called that a BETTER shape. The review showed it is a DIFFERENT and
// WEAKER one, in two ways I had not measured:
//
//   - It is skipped on early return. Four error paths return before the summary
//     is computed at all. The helper is a `defer` precisely because ADR-0025 D3
//     requires the summary to be the last status mutation EVEN ON EARLY RETURN
//     (reconcilehelpers/readiness.go). Folding it inline means that on any error
//     path the Ready condition stays stale — exactly when a reader needs it.
//
//   - Its base is anchored wrong. It re-fetches (application_controller.go:154)
//     and takes MergeFrom off the FRESH object, while the payload is the
//     composite computed BEFORE the re-fetch. The diff therefore actively
//     REVERTS any status field another writer changed in between. Not literally
//     the forbidden dance — that names Update, and this is Patch — but the
//     damage the contract names survives the rename.
//
// So this is an exemption, not an endorsement, and it is B-105. Declared with
// its reason rather than weakening the assertion until it catches nothing.
var foldsSummaryIntoItsOwnPatch = map[string]string{
	"application_controller.go": "folds the summary into its own Status().Patch — weaker than the " +
		"helper, not better: skipped on early return, and its base is taken after a re-fetch (B-105)",
}

func TestApp_ReadinessSummaryIsNotHandRolled(t *testing.T) {
	// Per SITE, not per file. Checking `does the file mention the helper?` was
	// measured insufficient: now that exposure_controller.go calls it, a
	// hand-rolled copy reintroduced in THAT SAME file would pass. What is checked
	// is each place the summary is computed, and whether that place is a `defer`
	// that persists it itself — which is the shape of the pre-slice-10 copy.
	//
	// Honest limit, stated because the comment above used to overclaim: with the
	// one exemption in place this loop currently inspects ZERO sites. It is a
	// tripwire, not an active assertion — and the sibling bare-Update check is
	// what has teeth today.
	for name, src := range packageSource(t) {
		if _, ok := foldsSummaryIntoItsOwnPatch[name]; ok {
			continue
		}
		if !strings.Contains(src, "SetReadinessSummaryViaPkg") {
			continue
		}
		// The defect shape: the summary computed inside a defer that persists it
		// on its own, rather than delegating to the shared helper.
		handRolled := false
		for _, chunk := range strings.Split(src, "defer func()")[1:] {
			body := chunk
			if end := strings.Index(chunk, "\n\t}()"); end >= 0 {
				body = chunk[:end]
			}
			if strings.Contains(body, "SetReadinessSummaryViaPkg") {
				handRolled = true
			}
		}
		if handRolled && !strings.Contains(src, "DeferReadinessSummary") {
			t.Errorf("%s computes the readiness summary itself instead of calling "+
				"reconcilehelpers.DeferReadinessSummary. The shared helper carries the optimistic "+
				"lock and logs the failure; a local copy is how this package kept the pre-slice-10 "+
				"shape for months (B-085).", name)
		}
	}
}

// The snapshot placement, observed by RUNNING Reconcile.
//
// Structural checks were measured bypassable in slice-22 (string offsets break
// both ways), so this captures what the controller actually emits, through an
// interceptor on the fake client.
//
// Scope, learned in slice-23: the fake client does NOT validate against the CRD
// schema, and it re-materialises zero-valued fields lacking omitempty — so it is
// blind to the absent-versus-false class. Pre-checked for this package: neither
// CRD declares a required status field, and every status field carries
// omitempty, so that class does not arise here.
func TestExposedRoute_ReconcileEmitsAPatchThatIsCorrectlyAnchored(t *testing.T) {
	// TWO fixtures, and the second is the one that matters.
	//
	// The first version seeded only a TLS-less route, so `checkCertificateReady`
	// returned true immediately and the guard at exposure_controller.go:187 was
	// never taken — one of the three patch sites never executed. Measured by the
	// review: with the base moved AFTER the phase initialisation (the defect the
	// base comment names), the single-fixture guard PASSED, and with TLS enabled
	// the same mutation loses `phase` from the patch and the route ends with
	// phase="".
	//
	// It is slice-24's lesson repeated and worse: there, the unreached site was
	// merely uncovered; here it is the ONLY site where this defect class shows,
	// because by the later site the phase has already changed and the self-diff
	// cancels nothing.
	for _, tc := range []struct {
		name string
		tls  bool
	}{
		{"sem TLS", false},
		{"com TLS — roteia pelo sitio que so ele alcanca", true},
	} {
		t.Run(tc.name, func(t *testing.T) { assertAnchoredPatches(t, tc.tls) })
	}
}

func assertAnchoredPatches(t *testing.T, tlsEnabled bool) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	seed := &loopzerov1.ExposedRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "loopzero-system", Generation: 7},
		Spec: loopzerov1.ExposedRouteSpec{
			Hostname: "probe.example.invalid",
			TLS:      loopzerov1.TLSConfig{Enabled: tlsEnabled},
		},
	}

	var bodies []string
	var lost []string
	c := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed).
			WithStatusSubresource(&loopzerov1.ExposedRoute{}).Build(),
		interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
				patch client.Patch, opts ...client.SubResourcePatchOption) error {
				data, err := patch.Data(obj)
				if err != nil {
					return err
				}
				bodies = append(bodies, string(data))
				want, _ := json.Marshal(obj.(*loopzerov1.ExposedRoute).Status)
				if err := cl.Status().Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				var got loopzerov1.ExposedRoute
				if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), &got); err != nil {
					return err
				}
				if persisted, _ := json.Marshal(got.Status); string(want) != string(persisted) {
					lost = append(lost, fmt.Sprintf("held %s\n    persisted %s", want, persisted))
				}
				return nil
			},
		})

	// HTTPClient is a declared dependency of this reconciler (it probes the
	// route's hostname), and leaving it nil panics rather than degrading — the
	// first version of this fixture did exactly that. Not a product defect: the
	// struct documents the field. A transport that fails every request keeps the
	// probe honest without reaching the network.
	r := &ExposureReconciler{
		Client: c,
		Scheme: scheme,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("probe blocked: this guard observes status writes, not the network")
		})},
	}
	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	})

	if len(bodies) == 0 {
		t.Fatal("Reconcile emitted no status patch — the guard observed nothing, which is not the " +
			"same as observing a clean run")
	}
	// The body's patch must carry the phase transition it exists to persist. An
	// empty or phase-less body patch means the snapshot was taken AFTER .Status
	// was mutated, so the diff cancelled out and the write persists nothing while
	// reporting success — the failure the base comment names, and the one the
	// single-fixture version of this guard could not see.
	if len(bodies) > 1 && !strings.Contains(bodies[0], `"phase"`) {
		t.Errorf("the first body patch does not carry the phase transition — the snapshot was taken "+
			"after .Status was mutated, so the diff is empty for it.\npatch: %s", bodies[0])
	}
	if len(lost) > 0 {
		t.Errorf("%d status write(s) did not persist what the controller held — a field mutated BEFORE "+
			"the snapshot sits in both base and object, so the diff omits it and the write is silently "+
			"empty for that field:\n  %s", len(lost), strings.Join(lost, "\n  "))
	}
	// The deferred readiness summary is the LAST write and carries the lock by
	// design (slice-10); everything before it is the controller body and must not.
	for i, body := range bodies[:max(0, len(bodies)-1)] {
		if strings.Contains(body, "resourceVersion") {
			t.Errorf("body patch #%d carries resourceVersion — capture `base` AFTER EnsureFinalizer "+
				"(B-085).\npatch: %s", i+1, body)
		}
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
