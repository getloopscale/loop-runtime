// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Proof that the readiness-summary defer honours the single-status-writer
// contract (CLAUDE.md; ADR 2026-08-12-status-writer-contract).
//
// The helper is the LAST status mutation of five reconcile loops, and it runs
// AFTER the controller's own write. That position is precisely why the write
// mechanism matters: `Status().Update` sends the whole object carrying the
// resourceVersion this helper just read, so a write landing in between is
// either rejected (409) or silently overwritten. A merge patch carries no
// resourceVersion and does not mention the fields it did not change, so it
// cannot erase what it never saw.
//
// The helper's own doc-comment states it re-fetches *because* other writers
// exist. That makes the write mechanism the whole point, not an implementation
// detail — which is what these tests pin.
package reconcilehelpers

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

func readinessScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgoscheme.AddToScheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(s); err != nil {
		t.Fatalf("loopzerov1.AddToScheme: %v", err)
	}
	return s
}

func readinessEnv() *loopzerov1.Environment {
	return &loopzerov1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "env-readiness",
			Namespace:  "loopzero-system",
			Generation: 3,
		},
		Status: loopzerov1.EnvironmentCRStatus{
			Conditions: []metav1.Condition{{
				Type:               "NamespaceReady",
				Status:             metav1.ConditionTrue,
				Reason:             "Created",
				ObservedGeneration: 3,
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
}

// deferForEnv builds the helper under test bound to an Environment.
func deferForEnv(ctx context.Context, c client.Client, key types.NamespacedName) func() {
	return DeferReadinessSummary(ctx, c, key,
		func() *loopzerov1.Environment { return &loopzerov1.Environment{} },
		func(e *loopzerov1.Environment) *[]metav1.Condition { return &e.Status.Conditions },
		[]string{"NamespaceReady"},
	)
}

// The contract test: the summary is persisted by merge patch, never by Update.
//
// Pinning the verb is necessary but NOT sufficient — the property the verb was
// chosen for is staged separately, by
// TestDeferReadinessSummary_ConcurrentWriteIsRejectedNotErased. An earlier draft
// of this file claimed a concurrent write could not be staged in a unit test;
// an independent review disproved it in about forty lines, using the same
// interceptor already imported here. That version of the suite locked in a
// regression: it proved which verb ran, and nothing about what the verb bought.
func TestDeferReadinessSummary_PersistsByPatchNotUpdate(t *testing.T) {
	var patches, updates int
	c := fake.NewClientBuilder().
		WithScheme(readinessScheme(t)).
		WithObjects(readinessEnv()).
		WithStatusSubresource(&loopzerov1.Environment{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string,
				obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				patches++
				return cl.Status().Patch(ctx, obj, patch, opts...)
			},
			SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string,
				obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updates++
				return cl.Status().Update(ctx, obj, opts...)
			},
		}).
		Build()

	key := types.NamespacedName{Name: "env-readiness", Namespace: "loopzero-system"}
	deferForEnv(context.Background(), c, key)()

	if updates != 0 {
		t.Errorf("status persisted via Status().Update %d time(s); the contract forbids the "+
			"refetch-then-restore dance (CLAUDE.md single-status-writer invariant)", updates)
	}
	if patches != 1 {
		t.Errorf("status merge patches = %d; want exactly 1", patches)
	}
}

// The summary must actually be written — a helper that patches nothing would
// pass the verb assertion above while silently dropping the kstatus Ready
// condition that ADR-0025 D3 makes mandatory.
func TestDeferReadinessSummary_WritesTheReadySummary(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(readinessScheme(t)).
		WithObjects(readinessEnv()).
		WithStatusSubresource(&loopzerov1.Environment{}).
		Build()

	key := types.NamespacedName{Name: "env-readiness", Namespace: "loopzero-system"}
	deferForEnv(context.Background(), c, key)()

	var got loopzerov1.Environment
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("Get after defer: %v", err)
	}
	var found bool
	for _, cond := range got.Status.Conditions {
		if cond.Type == "Ready" {
			found = true
			if cond.Status != metav1.ConditionTrue {
				t.Errorf("Ready=%s; want True (the only detail condition is True)", cond.Status)
			}
		}
	}
	if !found {
		t.Error("no Ready condition written; the readiness summary is mandatory (ADR-0025 D3)")
	}
}

// EC-5: a failed status write must be observable. It runs in a `defer`, so it
// cannot return the error — but discarding it makes a lost status write silent,
// and CLAUDE.md is explicit that a status-write failure is never swallowed.
func TestDeferReadinessSummary_SurfacesWriteFailure(t *testing.T) {
	boom := errors.New("simulated status patch failure")
	c := fake.NewClientBuilder().
		WithScheme(readinessScheme(t)).
		WithObjects(readinessEnv()).
		WithStatusSubresource(&loopzerov1.Environment{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string,
				obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				return boom
			},
		}).
		Build()

	sink := &recordingSink{}
	ctx := contextWithSink(sink)

	key := types.NamespacedName{Name: "env-readiness", Namespace: "loopzero-system"}
	deferForEnv(ctx, c, key)()

	if !sink.sawError(boom) {
		t.Errorf("status patch failure was discarded; it must reach the log "+
			"(recorded errors: %v)", sink.errs)
	}
}

// EC-3: the object may be deleted between the controller's write and the defer
// firing. Both early exits must survive the change of write mechanism.
func TestDeferReadinessSummary_SkipsWhenGoneOrDeleting(t *testing.T) {
	t.Run("object absent", func(t *testing.T) {
		var writes int
		c := fake.NewClientBuilder().
			WithScheme(readinessScheme(t)).
			WithStatusSubresource(&loopzerov1.Environment{}).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, sub string,
					obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					writes++
					return nil
				},
				// Both verbs are counted: counting only Patch would let this test
				// pass under an Update-based implementation with a broken guard.
				SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string,
					obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					return nil
				},
			}).
			Build()

		deferForEnv(context.Background(), c,
			types.NamespacedName{Name: "absent", Namespace: "loopzero-system"})()

		if writes != 0 {
			t.Errorf("wrote status for an object that does not exist (%d write(s))", writes)
		}
	})

	t.Run("object being deleted", func(t *testing.T) {
		env := readinessEnv()
		now := metav1.Now()
		env.DeletionTimestamp = &now
		env.Finalizers = []string{"loopzero.io/test"} // fake client requires one to accept a DeletionTimestamp

		var writes int
		c := fake.NewClientBuilder().
			WithScheme(readinessScheme(t)).
			WithObjects(env).
			WithStatusSubresource(&loopzerov1.Environment{}).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, sub string,
					obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					writes++
					return nil
				},
				// Both verbs are counted: counting only Patch would let this test
				// pass under an Update-based implementation with a broken guard.
				SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string,
					obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					return nil
				},
			}).
			Build()

		deferForEnv(context.Background(), c,
			types.NamespacedName{Name: "env-readiness", Namespace: "loopzero-system"})()

		if writes != 0 {
			t.Errorf("wrote status for an object under deletion (%d write(s))", writes)
		}
	})
}

// recordingSink is a minimal logr.LogSink that keeps the errors passed to
// Error(). The alternative — asserting on captured stderr — would couple the
// test to the log encoder rather than to the fact that the failure was
// reported, which is the property under test.
type recordingSink struct{ errs []error }

func (s *recordingSink) Init(logr.RuntimeInfo)               {}
func (s *recordingSink) Enabled(int) bool                    { return true }
func (s *recordingSink) Info(int, string, ...any)            {}
func (s *recordingSink) Error(err error, _ string, _ ...any) { s.errs = append(s.errs, err) }
func (s *recordingSink) WithValues(...any) logr.LogSink      { return s }
func (s *recordingSink) WithName(string) logr.LogSink        { return s }

func (s *recordingSink) sawError(target error) bool {
	for _, e := range s.errs {
		if errors.Is(e, target) {
			return true
		}
	}
	return false
}

func contextWithSink(s *recordingSink) context.Context {
	return logf.IntoContext(context.Background(), logr.New(s))
}

// The property the merge patch was chosen for, staged directly.
//
// A JSON merge patch (RFC 7386) treats an array as a scalar: the conditions
// patch carries the WHOLE array, controller-authored conditions included. So a
// bare client.MergeFrom would silently erase a write that landed between this
// helper's Get and its patch — strictly worse than the Status().Update it
// replaced, which at least got a 409 for free.
//
// The optimistic lock is what closes that. This test stages the race by having
// a competing writer land inside the patch interceptor, and asserts the helper
// LOSES loudly rather than winning silently.
func TestDeferReadinessSummary_ConcurrentWriteIsRejectedNotErased(t *testing.T) {
	const rival = "RivalWriterCondition"

	var c client.Client = fake.NewClientBuilder().
		WithScheme(readinessScheme(t)).
		WithObjects(readinessEnv()).
		WithStatusSubresource(&loopzerov1.Environment{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, sub string,
				obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				// A second writer lands after the helper's Get, before its patch.
				var live loopzerov1.Environment
				key := types.NamespacedName{Name: "env-readiness", Namespace: "loopzero-system"}
				if err := cl.Get(ctx, key, &live); err != nil {
					return err
				}
				live.Status.Conditions = append(live.Status.Conditions, metav1.Condition{
					Type:               rival,
					Status:             metav1.ConditionTrue,
					Reason:             "WrittenByAnotherActor",
					ObservedGeneration: 3,
					LastTransitionTime: metav1.Now(),
				})
				if err := cl.Status().Update(ctx, &live); err != nil {
					return err
				}
				return cl.Status().Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	sink := &recordingSink{}
	key := types.NamespacedName{Name: "env-readiness", Namespace: "loopzero-system"}
	deferForEnv(logf.IntoContext(context.Background(), logr.New(sink)), c, key)()

	var got loopzerov1.Environment
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("Get after defer: %v", err)
	}
	var survived bool
	for _, cond := range got.Status.Conditions {
		if cond.Type == rival {
			survived = true
		}
	}
	if !survived {
		t.Error("the concurrent writer's condition was ERASED. A merge patch carries the whole " +
			"conditions array, so without MergeFromWithOptimisticLock this helper silently " +
			"overwrites what another writer put there between its Get and its patch")
	}
	if len(sink.errs) == 0 {
		t.Error("the rejected write was not reported; a lost status write must never be silent")
	}
}
