// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

// notYetConverted are the files in this package still holding bare
// Status().Update calls, with the count measured at the time of writing.
//
// The set is frozen separately below: the count locks growth INSIDE a file, but
// nothing stops a new key being added, so "the list only shrinks" would be a
// convention asserted in a comment and nowhere else (review of slice-22).
// EMPTY since 2026-08-14 (B-085 closed at zero).
//
// It held `application_controller.go` while that file's only base was taken
// after a re-fetch, leaving its write inside reconcileExposedRoute with no base
// to use. Re-anchoring removed both the re-fetch and the exemption in the same
// slice.
//
// The map and its frozen twin stay rather than being deleted: they are what
// makes the next bare Update fail loudly instead of arriving with an exemption
// attached.
var notYetConverted = map[string]exemption{}

type exemption struct {
	writes int
	since  string
}

var exemptedFiles = []string{}

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
//     (reconcileloop/readiness.go). Folding it inline means that on any error
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
	"application_controller.go": "folds the summary into its own Status().Patch — skipped on early " +
		"return, which the shared helper handles by being a defer (B-105). The other half of this " +
		"reason — a base taken after a re-fetch — was FIXED on 2026-08-14 and no longer applies",
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
				"reconcile.DeferReadinessSummary. The shared helper carries the optimistic "+
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
		{"without TLS", false},
		{"with TLS — routes through the site only it reaches", true},
	} {
		t.Run(tc.name, func(t *testing.T) { assertAnchoredPatches(t, tc.tls) })
	}
}

func assertAnchoredPatches(t *testing.T, tlsEnabled bool) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := loopruntimev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	seed := &loopruntimev1.ExposedRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "loop-runtime-system", Generation: 7},
		Spec: loopruntimev1.ExposedRouteSpec{
			Hostname: "probe.example.invalid",
			TLS:      loopruntimev1.TLSConfig{Enabled: tlsEnabled},
		},
	}

	var bodies []string
	var lost []string
	c := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed).
			WithStatusSubresource(&loopruntimev1.ExposedRoute{}).Build(),
		interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
				patch client.Patch, opts ...client.SubResourcePatchOption) error {
				data, err := patch.Data(obj)
				if err != nil {
					return err
				}
				bodies = append(bodies, string(data))
				want, _ := json.Marshal(obj.(*loopruntimev1.ExposedRoute).Status)
				if err := cl.Status().Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				var got loopruntimev1.ExposedRoute
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

// The application controller's base is anchored BEFORE the status is composed —
// observed by running Reconcile against a concurrent writer.
//
// The first version of this guard read the source for the strings "re-fetching
// application" and "computedStatus". The review broke it by moving
// `base := app.DeepCopy()` below the first .Status mutation — the auto-diff
// defect — and it PASSED, while printing "the re-fetch at offset %d". An offset,
// in the file whose own comment 140 lines above says structural checks were
// measured bypassable in slice-22. Renaming the error string would bypass it too.
//
// So this asserts the property behaviourally: a field written by SOMEONE ELSE
// between the controller's read and its patch must SURVIVE. Under the old shape
// the base was the freshly re-read object while the payload was the composite
// from before, so the diff actively reverted that field. Under the correct
// anchor the patch simply does not mention it.
//
// Scope, honest: RFC 7386 treats arrays as scalars, so `status.conditions` is
// still replaced wholesale by any pass that touches a condition. This guards the
// scalar fields — url, phase, observedGeneration, endpointsReady — which is what
// the re-anchoring actually fixes.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-application-status-anchor
func TestApplicationController_ConcurrentStatusWriteSurvivesThePatch(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := loopruntimev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	seed := &loopruntimev1.LoopRuntimeApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "app1", Namespace: "loop-runtime-system", Generation: 7},
		Spec: loopruntimev1.LoopRuntimeApplicationSpec{
			TenantRef: "tn-x", Project: "p", Environment: "e",
			ServiceName: "svc", Hostname: "app1.example.invalid",
		},
	}

	inner := fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed).
		WithStatusSubresource(&loopruntimev1.LoopRuntimeApplication{}).Build()

	var bodies []string
	c := interceptor.NewClient(inner, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object,
			patch client.Patch, opts ...client.SubResourcePatchOption) error {
			// A DIFFERENT writer lands a scalar status field between the
			// controller's read and its patch — the race the anchor decides.
			var live loopruntimev1.LoopRuntimeApplication
			if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), &live); err == nil {
				live.Status.EndpointsReady = true
				_ = cl.Status().Update(ctx, &live)
			}
			data, err := patch.Data(obj)
			if err != nil {
				return err
			}
			bodies = append(bodies, string(data))
			return cl.Status().Patch(ctx, obj, patch, opts...)
		},
	})

	r := &ApplicationReconciler{Client: c, Scheme: scheme, BaseDomain: "example.invalid"}
	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	})

	if len(bodies) == 0 {
		t.Fatal("Reconcile emitted no status patch — the guard observed nothing, which is not the " +
			"same as observing a clean run")
	}
	// (1) The patch carries what the controller computed. An empty or url-less
	// body means the base was taken AFTER the mutation and the diff cancelled it.
	if !strings.Contains(bodies[0], `"url"`) {
		t.Errorf("the patch does not carry status.url, set before the base is taken — the snapshot is "+
			"anchored after the mutation and the write persists nothing for it.\npatch: %s", bodies[0])
	}
	// (2) The concurrent write survived. Under the old anchor the diff carried
	// endpointsReady:false — the value from the pre-refetch composite — and
	// reverted it.
	var got loopruntimev1.LoopRuntimeApplication
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(seed), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Status.EndpointsReady {
		t.Errorf("a status field written by another writer between the read and the patch was REVERTED. "+
			"That is the damage the single-writer contract names, and an anchor taken after a re-fetch "+
			"produces it while still looking like a merge patch (B-105).\npatch: %s", bodies[0])
	}
}

// TestApplicationController_ErrorPathPersistsAFreshReadinessSummary is the
// behavioural guard for B-105 bala 2 (decision
// MEET-ASYNC-AMENDMENT-2026-08-14-application-status-persist-encapsulation).
//
// The reconcile has TWO points that persist .status: the happy path at the end,
// and the ExposedRoute failure path, which marks IngressReady=False and patches
// before returning the error. Only the first folded in the readiness summary, so
// a failed pass persisted a FRESH IngressReady=False next to a Ready condition
// left over from the previous pass — stale, and possibly True.
//
// The assertion is on persisted state, never on the source text: seed a stale
// Ready=True at an older generation, force the ExposedRoute write to fail, and
// read back what the server holds. ADR-0025 D3 requires the summary to be the
// last status mutation even on early return.
func TestApplicationController_ErrorPathPersistsAFreshReadinessSummary(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := loopruntimev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	const staleGen, currentGen = 6, 7
	seed := &loopruntimev1.LoopRuntimeApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "app-err", Namespace: "loop-runtime-system", Generation: currentGen},
		Spec: loopruntimev1.LoopRuntimeApplicationSpec{
			TenantRef: "tn-x", Project: "p", Environment: "e",
			ServiceName: "svc", Hostname: "app-err.example.invalid",
		},
		Status: loopruntimev1.LoopRuntimeApplicationStatus{
			Conditions: []metav1.Condition{{
				Type:               conditions.TypeReady,
				Status:             metav1.ConditionTrue,
				Reason:             conditions.ReasonReady,
				Message:            "left over from the previous pass",
				ObservedGeneration: staleGen,
				LastTransitionTime: metav1.Now(),
			}},
		},
	}

	inner := fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed).
		WithStatusSubresource(&loopruntimev1.LoopRuntimeApplication{}).Build()

	// Deny the ExposedRoute write so reconcileExposedRoute takes its failure
	// branch — the branch that persists status and returns.
	c := interceptor.NewClient(inner, interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object,
			opts ...client.CreateOption) error {
			if _, isRoute := obj.(*loopruntimev1.ExposedRoute); isRoute {
				return fmt.Errorf("denied by the guard")
			}
			return cl.Create(ctx, obj, opts...)
		},
	})

	r := &ApplicationReconciler{Client: c, Scheme: scheme, BaseDomain: "example.invalid"}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: seed.Name, Namespace: seed.Namespace},
	}); err == nil {
		t.Fatal("Reconcile returned no error — the failure branch under test never ran, so a green " +
			"result here would prove nothing")
	}

	var got loopruntimev1.LoopRuntimeApplication
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(seed), &got); err != nil {
		t.Fatal(err)
	}

	ingress := meta.FindStatusCondition(got.Status.Conditions, conditions.TypeIngressReady)
	if ingress == nil || ingress.Status != metav1.ConditionFalse {
		t.Fatalf("the failure branch did not persist IngressReady=False — the guard is not observing "+
			"the write it was built for. conditions: %+v", got.Status.Conditions)
	}

	ready := meta.FindStatusCondition(got.Status.Conditions, conditions.TypeReady)
	if ready == nil {
		t.Fatalf("no Ready condition persisted. conditions: %+v", got.Status.Conditions)
	}
	if ready.ObservedGeneration != currentGen {
		t.Errorf("Ready was persisted at generation %d while the object is at %d — the summary was NOT "+
			"recomputed on this pass. A reader sees a readiness verdict from before the failure, which "+
			"is exactly when it is read (ADR-0025 D3, B-105).", ready.ObservedGeneration, currentGen)
	}
	if ready.Status != metav1.ConditionFalse {
		t.Errorf("IngressReady=False was persisted but Ready stayed %s — a failed pass left the summary "+
			"claiming the application is ready.", ready.Status)
	}

	// The write must not contradict itself. A Ready stamped with the current
	// generation next to a .status.observedGeneration from the previous one tells
	// a reader two different things about which generation was seen, and kstatus
	// resolves that by treating the whole subresource as stale — discarding the
	// very verdict this test proved was recomputed.
	if got.Status.ObservedGeneration != currentGen {
		t.Errorf("Ready is stamped at generation %d but .status.observedGeneration is %d — one status "+
			"write disagreeing with itself about which generation the controller has seen.",
			ready.ObservedGeneration, got.Status.ObservedGeneration)
	}
}

// statusPersistSite is one place in the package that writes a .status subresource.
type statusPersistSite struct {
	file, fn string
	line     int
	form     string // how the writer was reached, for the failure message
}

// findStatusPersistSites walks the package AST for every call that persists a
// status subresource, and reports the enclosing function of each.
//
// It replaced a guard that parsed ONE file and required the receiver to be
// literally `x.Status()`. The review of this slice killed that guard with three
// two-line mutants, all of which compiled and persisted status while the package
// stayed green:
//
//   - `sw := r.Status()` then `sw.Patch(...)` — receiver is an Ident, not a call
//   - `r.SubResource("status").Patch(...)` — different selector, same subresource
//   - the same write moved to a sibling file of the package
//
// So: every production file, local aliases resolved, and SubResource treated as
// the synonym it is. A guard that only recognises the shape its author happened
// to write reports clean because it is looking away — measured twice in one day,
// here and in the backlog checker's closure-note rule.
func findStatusPersistSites(t *testing.T) []statusPersistSite {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	reachesStatus := func(e ast.Expr) string {
		src := gotypes.ExprString(e)
		switch {
		case strings.Contains(src, ".Status()"):
			return "Status()"
		case strings.Contains(src, ".SubResource(\"status\")"):
			return `SubResource("status")`
		}
		return ""
	}

	var sites []statusPersistSite
	var files int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			// Locals bound to a status writer: `sw := r.Status()`.
			aliases := map[string]string{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range assign.Rhs {
					if via := reachesStatus(rhs); via != "" && i < len(assign.Lhs) {
						if id, ok := assign.Lhs[i].(*ast.Ident); ok {
							aliases[id.Name] = via + " via local `" + id.Name + "`"
						}
					}
				}
				return true
			})

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Patch" && sel.Sel.Name != "Update") {
					return true
				}
				form := reachesStatus(sel.X)
				if form == "" {
					if id, ok := sel.X.(*ast.Ident); ok {
						form = aliases[id.Name]
					}
				}
				if form == "" {
					return true
				}
				sites = append(sites, statusPersistSite{
					file: name, fn: fn.Name.Name,
					line: fset.Position(call.Pos()).Line, form: form,
				})
				return true
			})
		}
	}
	if files == 0 {
		t.Fatal("no production file parsed; a check that read nothing verified nothing")
	}
	return sites
}

// TestApp_EveryStatusPersistFoldsTheReadinessSummary is the structural guard for
// ADR-0025 D3 across the package.
//
// The invariant is one of ORDER — the readiness summary is the last status
// mutation before any persist — and order is what a call-counting table cannot
// see. That is how B-105 stayed hidden: the B-085 table scored this package
// conformant because it counted `Patch` vs `Update`, while a failure branch
// persisted without ever summarising.
//
// # Population sweep, 2026-08-17 (B-109) — and what it does NOT establish
//
// `rules/testing.md § 4.2` requires every structural guard to be measured
// against the real population, with the result recorded beside it even when it
// is zero. This guard is PACKAGE-scoped; the population is six operator
// packages.
//
// The six were swept heuristically (persists of `.Status().Patch|Update(`, and
// whether the enclosing function names the readiness summary):
//
//	operators/internal/app                 2 with summary,  2 without
//	operators/internal/environment         5                1
//	operators/internal/platform            6                7
//	operators/internal/preview             0                6
//	operators/internal/reconcileloop       1                0
//	operators/internal/tenant              2                3
//	                                      ──               ──
//	                                      16               19
//
// **THE LIMITATION IS PART OF THE RESULT, and without it the number would be
// worse than none.** The sweep split each file at `func ` at line start and
// looked for the summary by name; it did not follow calls, so a function that
// delegated the fold to a helper counted as "without". The 19 were **sites to
// look at**, not 19 defects — claiming the latter would commit here the very
// error B-109 names.
//
// # The SIX-package guard exists now, and it is the authoritative one (B-138)
//
// `cmd/loop-runtime-ops/internal/quality/readinesssummary` covers the six packages by
// AST and FOLLOWS THE CALL: a persist inside a helper counts as covered when
// the helper folds the summary. That is what the count above could not see —
// measured on 2026-08-19, the "19 without summary" were ALL artefacts of
// stopping at the function around the `Patch`.
//
// This package guard STAYS, and not out of inertia: it runs in the `operators`
// suite, where whoever edits the controller already is, and fails next to the
// error instead of in a separate binary. Both assert the same invariant at
// different scopes; when they diverge, the six-package one wins.
//
// So the assertion is on the ENCLOSING FUNCTION of each persist, not on how
// many persists there are: the function must fold the summary in-line before
// the write (patchStatus, advanceAndPatch). The deferred form this guard once
// also accepted was removed together with its helper, so every controller now
// has one status writer that folds the summary itself.
func TestApp_EveryStatusPersistFoldsTheReadinessSummary(t *testing.T) {
	sites := findStatusPersistSites(t)
	if len(sites) == 0 {
		t.Fatal("no status persist found in the package — the guard observed nothing, which is not " +
			"the same as observing a clean package")
	}

	sources := map[string]string{}
	read := func(t *testing.T, file string) string {
		t.Helper()
		if s, ok := sources[file]; ok {
			return s
		}
		b, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		sources[file] = stripGoComments(string(b))
		return sources[file]
	}

	for _, s := range sites {
		src := read(t, s.file)
		fnStart := strings.Index(src, "func "+s.fn+"(")
		if fnStart < 0 {
			// Method: `func (r *X) Name(`.
			fnStart = strings.Index(src, ") "+s.fn+"(")
		}
		if fnStart < 0 {
			t.Errorf("could not locate %s in %s to inspect its body", s.fn, s.file)
			continue
		}
		body := src[fnStart:]
		if end := strings.Index(body, "\nfunc "); end > 0 {
			body = body[:end]
		}

		if !strings.Contains(body, "SetReadinessSummaryViaPkg(") {
			t.Errorf("%s:%d — %s() persists .status (reached by %s) without folding in the readiness "+
				"summary. A pass through this function leaves the Ready "+
				"condition at whatever the PREVIOUS pass computed, which is read as current "+
				"(ADR-0025 D3, B-105).", s.file, s.line, s.fn, s.form)
		}
	}
}
