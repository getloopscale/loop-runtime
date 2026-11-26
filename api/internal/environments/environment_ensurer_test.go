// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environments

import (
	"context"
	"errors"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

type stubLister struct {
	exists    atomic.Bool
	err       error
	callCount atomic.Int32
	// m37 plan (2026-06-25): capture (project, env) so per-(project, env)
	// tuple assertions are possible.
	gotProject string
	gotEnv     string
	// existsByTuple, when set, overrides the simple `exists` toggle:
	// HasEnvironment returns existsByTuple[project+"|"+envName].
	// Lets a single stubLister model multiple distinct tuples.
	existsByTuple map[string]bool
}

func (s *stubLister) HasEnvironment(_ context.Context, _, project, envName string) (bool, error) {
	s.callCount.Add(1)
	s.gotProject = project
	s.gotEnv = envName
	if s.existsByTuple != nil {
		return s.existsByTuple[project+"|"+envName], s.err
	}
	return s.exists.Load(), s.err
}

type stubAdder struct {
	err         error
	callCount   atomic.Int32
	onAddBecome bool // when true, marks lister as existing after AddEnvironment
	lister      *stubLister
	// M2 v1.2 T3.1 — capture (project, env) for propagation tests.
	gotProject string
	gotEnv     string
}

func (s *stubAdder) AddEnvironment(_ context.Context, _, project, envName string) error {
	s.callCount.Add(1)
	s.gotProject = project
	s.gotEnv = envName
	if s.err != nil {
		return s.err
	}
	if s.onAddBecome && s.lister != nil {
		// Legacy fixtures (no tuple map): toggle the simple `exists` bool.
		s.lister.exists.Store(true)
		// m37 fixtures (tuple map): also flip the new (project, env) tuple
		// so the post-Add poll loop sees the entry as present.
		if s.lister.existsByTuple != nil {
			s.lister.existsByTuple[project+"|"+envName] = true
		}
	}
	return nil
}

// TestEnsureEnvironment_NoopWhenExists asserts that an existing env returns
// nil immediately without calling AddEnvironment.
// Plan T2.2 RED #1.
func TestEnsureEnvironment_NoopWhenExists(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(true)
	adder := &stubAdder{}
	e := &EnvironmentEnsurer{Lister: lister, Adder: adder}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview"); err != nil {
		t.Errorf("expected nil for existing env, got %v", err)
	}
	if adder.callCount.Load() != 0 {
		t.Errorf("expected zero AddEnvironment calls for existing env, got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_CreatesAndWaits asserts that a missing env is
// created via AddEnvironment, then polling confirms existence.
// Plan T2.2 RED #2.
func TestEnsureEnvironment_CreatesAndWaits(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  3 * time.Second,
		PollInterval: 100 * time.Millisecond,
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview"); err != nil {
		t.Errorf("expected nil after create+wait, got %v", err)
	}
	if adder.callCount.Load() != 1 {
		t.Errorf("expected exactly 1 AddEnvironment call, got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_TimeoutWhenOperatorSlow asserts that the ensurer
// times out gracefully if the operator never reconciles.
// Plan T2.2 RED #3.
func TestEnsureEnvironment_TimeoutWhenOperatorSlow(t *testing.T) {
	lister := &stubLister{} // exists stays false
	adder := &stubAdder{}   // does NOT flip lister
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  300 * time.Millisecond,
		PollInterval: 50 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet ready") {
		t.Errorf("expected timeout message, got %q", err.Error())
	}
}

// A concurrent ensure that added the entry first reports the typed
// ErrEnvironmentExists; the environment exists, which is all the call wants.
func TestEnsureEnvironment_TypedAlreadyExistsIsSuccess(t *testing.T) {
	lister := &stubLister{} // exists=false, so we'll call Add
	adder := &stubAdder{err: fmt.Errorf("k8s: %w", ports.ErrEnvironmentExists)}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  100 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")
	if err != nil {
		t.Errorf("expected nil for the typed already-exists error, got %v", err)
	}
}

// Only the typed sentinel means "already present". Any other error is a
// failure, whatever its message says — the text "already exists" used to turn
// such an error into success.
func TestEnsureEnvironment_AddErrorMentioningAlreadyExistsFails(t *testing.T) {
	lister := &stubLister{}
	adder := &stubAdder{err: errors.New("admission webhook: object already exists in cache")}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  100 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")

	var te *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &te) || te.Code != "ENV_PROVISION_FAILED" {
		t.Fatalf("expected ENV_PROVISION_FAILED, got %v", err)
	}
}

// A wired ensurer without its Lister or Adder is a wiring bug. Reporting
// success would let the deploy proceed as if the environment existed (the
// callers already skip an ensurer that is not wired at all).
func TestEnsureEnvironment_MisconfiguredEnsurerFailsLoud(t *testing.T) {
	e := &EnvironmentEnsurer{}
	if err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview"); !errors.Is(err, ErrEnvironmentEnsurerMisconfigured) {
		t.Errorf("want ErrEnvironmentEnsurerMisconfigured, got %v", err)
	}
}

func TestEnsureEnvironment_NilReceiverFailsLoud(t *testing.T) {
	var e *EnvironmentEnsurer
	if err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview"); !errors.Is(err, ErrEnvironmentEnsurerMisconfigured) {
		t.Errorf("want ErrEnvironmentEnsurerMisconfigured, got %v", err)
	}
}

// TestEnsureEnvironment_AddFailReturnsLoopRuntimeError asserts that non-already-exists
// errors from AddEnvironment surface as LoopRuntimeError(ENV_PROVISION_FAILED).
// Plan T2.2 RED #6.
func TestEnsureEnvironment_AddFailReturnsLoopRuntimeError(t *testing.T) {
	lister := &stubLister{}
	adder := &stubAdder{err: errors.New("permission denied")}
	e := &EnvironmentEnsurer{Lister: lister, Adder: adder}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")
	if err == nil {
		t.Fatalf("expected error from AddEnvironment failure, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected underlying error in message, got %q", err.Error())
	}
}

// stubReadinessChecker implements EnvironmentReadinessChecker for tests.
// Toggles via .ready atomic.Bool; callCount tracks invocations.
type stubReadinessChecker struct {
	ready     atomic.Bool
	err       error
	callCount atomic.Int32
	// M2 v1.2 T3.1 — capture (project, env) for propagation tests.
	gotProject string
	gotEnv     string
}

func (s *stubReadinessChecker) IsReady(_ context.Context, _, project, envName string) (bool, error) {
	s.callCount.Add(1)
	s.gotProject = project
	s.gotEnv = envName
	return s.ready.Load(), s.err
}

// TestEnsureEnvironment_BlockedUntilReadinessActive (Sprint A trio-fix
// 2026-06-01) asserts that with a non-nil ReadinessChecker, EnsureEnvironment
// continues polling AFTER HasEnvironment returns true UNTIL IsReady also
// returns true. This is the canonical regression for the
// SYNC_FAILED-because-AppProject-not-reconciled bug detected during the
// trio dogfood: shallow check (env in tenant.spec.environments) passes
// immediately but the operator hasn't updated AppProject destinations yet,
// causing downstream ArgoCD App stuck InvalidSpecError.
func TestEnsureEnvironment_BlockedUntilReadinessActive(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{} // ready stays false
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      400 * time.Millisecond,
		PollInterval:     50 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")
	if err == nil {
		t.Fatalf("expected timeout because readiness never flips, got nil")
	}
	if !strings.Contains(err.Error(), "not yet ready") {
		t.Errorf("expected ENV_PROVISION_TIMEOUT-style error, got %q", err.Error())
	}
	// HasEnvironment should have been called multiple times (poll loop).
	if lister.callCount.Load() < 2 {
		t.Errorf("expected HasEnvironment polled >=2x, got %d", lister.callCount.Load())
	}
	// IsReady should have been called multiple times too (gating the return).
	if checker.callCount.Load() < 2 {
		t.Errorf("expected IsReady polled >=2x (poll loop), got %d", checker.callCount.Load())
	}
}

// TestEnsureEnvironment_ReturnsWhenBothReady (Sprint A trio-fix) asserts the
// HAPPY path: HasEnvironment + IsReady both return true → ensurer returns nil
// quickly without timeout.
func TestEnsureEnvironment_ReturnsWhenBothReady(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{}
	checker.ready.Store(true) // ready immediately
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      2 * time.Second,
		PollInterval:     20 * time.Millisecond,
	}

	start := time.Now()
	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")
	if err != nil {
		t.Fatalf("expected nil when both checks pass, got %v", err)
	}
	// Should complete fast — well under WaitTimeout.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("expected fast return (<500ms) when ready, took %v", elapsed)
	}
	if adder.callCount.Load() != 1 {
		t.Errorf("expected exactly 1 AddEnvironment call, got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_LegacyBehaviorWhenCheckerNil (Sprint A trio-fix)
// asserts that nil ReadinessChecker preserves backward-compatible behavior:
// HasEnvironment=true is sufficient to return success. This guards test
// fixtures and any boot path that intentionally omits the deep check.
func TestEnsureEnvironment_LegacyBehaviorWhenCheckerNil(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(true) // already exists
	adder := &stubAdder{}
	e := &EnvironmentEnsurer{
		Lister: lister,
		Adder:  adder,
		// ReadinessChecker intentionally nil
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview"); err != nil {
		t.Errorf("expected nil for existing env with no readiness check, got %v", err)
	}
}

// TestEnsureEnvironment_ReadinessErrorTreatedAsTransient (Sprint A trio-fix)
// asserts that IsReady errors do NOT immediately fail EnsureEnvironment —
// the poll loop continues, treating the error as transient (matches existing
// HasEnvironment error semantics). This guards against flaky K8s API
// responses (e.g., rolling control-plane upgrade) causing spurious
// ENV_PROVISION_TIMEOUT.
func TestEnsureEnvironment_ReadinessErrorTreatedAsTransient(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{err: errors.New("API server timeout")}
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      250 * time.Millisecond,
		PollInterval:     50 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "preview")
	// Expected: timeout (readiness check NEVER returned ready=true), but
	// the loop should have retried multiple times (treated err as transient).
	if err == nil {
		t.Fatalf("expected timeout (readiness perma-erroring), got nil")
	}
	if !strings.Contains(err.Error(), "not yet ready") {
		t.Errorf("expected timeout error, got %q", err.Error())
	}
	if checker.callCount.Load() < 2 {
		t.Errorf("expected IsReady polled multiple times (transient retry), got %d",
			checker.callCount.Load())
	}
}

// =============================================================================
// M2 v1.2 — T3.1 propagation tests
// Plan: 2026-06-07-runtime-envs-per-project-m2 v1.2
// =============================================================================

// TestEnsureEnvironment_PassesProjectToAdder asserts that the project
// arg of EnsureEnvironment is forwarded verbatim to Adder.AddEnvironment.
// Without this, T2.1 (TenantCRMgr writes project) would never receive
// the project from the deploy hot path.
func TestEnsureEnvironment_PassesProjectToAdder_M2(t *testing.T) {
	lister := &stubLister{} // exists=false → create path
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  250 * time.Millisecond,
		PollInterval: 50 * time.Millisecond,
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "web", "staging"); err != nil {
		t.Fatalf("EnsureEnvironment failed: %v", err)
	}
	if adder.gotProject != "web" {
		t.Errorf("expected project=%q propagated to Adder.AddEnvironment, got %q", "web", adder.gotProject)
	}
	if adder.gotEnv != "staging" {
		t.Errorf("expected env=%q propagated to Adder.AddEnvironment, got %q", "staging", adder.gotEnv)
	}
}

// TestEnsureEnvironment_PassesProjectToReadiness asserts that the
// project arg is forwarded verbatim to ReadinessChecker.IsReady so the
// Environment CR lookup hits the canonical per-project name.
func TestEnsureEnvironment_PassesProjectToReadiness_M2(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{}
	checker.ready.Store(true) // become ready on first poll
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      250 * time.Millisecond,
		PollInterval:     50 * time.Millisecond,
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "api", "preview-pr-7"); err != nil {
		t.Fatalf("EnsureEnvironment failed: %v", err)
	}
	if checker.gotProject != "api" {
		t.Errorf("expected project=%q propagated to ReadinessChecker.IsReady, got %q", "api", checker.gotProject)
	}
	if checker.gotEnv != "preview-pr-7" {
		t.Errorf("expected env=%q propagated to ReadinessChecker.IsReady, got %q", "preview-pr-7", checker.gotEnv)
	}
}

// TestEnsureEnvironment_EmptyProjectPreservesLegacyPath asserts the
// backward-compat path: production-legacy callers (project="") still
// land on the Adder.AddEnvironment with empty project so the operator
// writes a legacy entry (ADR D2).
func TestEnsureEnvironment_EmptyProjectPreservesLegacyPath_M2(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  250 * time.Millisecond,
		PollInterval: 50 * time.Millisecond,
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "", "production"); err != nil {
		t.Fatalf("EnsureEnvironment(empty project) failed: %v", err)
	}
	if adder.gotProject != "" {
		t.Errorf("expected empty project propagated for legacy path, got %q", adder.gotProject)
	}
	if adder.gotEnv != "production" {
		t.Errorf("expected env=production propagated, got %q", adder.gotEnv)
	}
}

// =============================================================================
// m37 — Tenant CR per-(project, env) shallow check canonical fix
// Plan: m37-tenant-cr-project-sync-canonical-fix v1.0
// Closes #37 scenario 4 (BUILD → RELEASE → URL)
// =============================================================================

// TestEnsureEnvironment_DifferentProjectSameEnvName_TriggersAddEnvironment
// is the m37 RED test. Reproduces the QA Red Team's #37 scenario verbatim:
//
//   - Tenant `tn-1` already has `(project=qa-redteam-dogfood, env=preview)` in
//     spec.environments[].
//   - Operator runs `loopruntime deploy` for `(project=loopzerokit-example, env=preview)`.
//   - Pre-m37 (project-agnostic shallow check): HasEnvironment returned
//     exists=true on env-name match, EnsureEnvironment returned early,
//     AddEnvironment never called → Tenant CR stale → AppProject destinations
//     never updated → ArgoCD sync failed → no Live URL.
//   - Post-m37 (per-(project, env) tuple shallow check): HasEnvironment for
//     (loopzerokit-example, preview) returns exists=false (only qa-redteam-dogdood
//     tuple is in the map), AddEnvironment fires, Tenant CR gains the new
//     tuple, downstream chain succeeds.
func TestEnsureEnvironment_DifferentProjectSameEnvName_TriggersAddEnvironment(t *testing.T) {
	// Stub lister with the EXACT verbatim #37 dev-public state:
	// existing tuple (qa-redteam-dogfood, preview).
	lister := &stubLister{
		existsByTuple: map[string]bool{
			"qa-redteam-dogfood|preview":    true,
			"qa-redteam-dogfood|production": true,
		},
	}
	adder := &stubAdder{
		// When AddEnvironment fires, mark the NEW tuple as existing so the
		// poll loop exits without timeout.
		onAddBecome: true,
		lister:      lister,
	}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  500 * time.Millisecond,
		PollInterval: 25 * time.Millisecond,
	}

	// Deploy the QA Red Team scenario: NEW project (loopzerokit-example), env name
	// (preview) that ALREADY exists for a DIFFERENT project on the same tenant.
	err := e.EnsureEnvironment(context.Background(), "tn-1", "loopzerokit-example", "preview")
	if err != nil {
		// Sanity: we set onAddBecome=true so after AddEnvironment fires,
		// the lister flips exists=true for the new tuple. If the test
		// times out here, the production-impl bug is masking the assertion.
		t.Fatalf("EnsureEnvironment must succeed when AddEnvironment fires for a new project: %v", err)
	}
	// PRIMARY assertion (m37 RED): AddEnvironment MUST have been called
	// exactly once with the new (project, env) tuple. Pre-m37 it was 0
	// because the project-agnostic shallow check returned exists=true on
	// the qa-redteam-dogfood entry's name match.
	if got := adder.callCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 AddEnvironment call for new (project=loopzerokit-example, env=preview), got %d (pre-m37 was 0 — the silent no-op #37 bug)", got)
	}
	if adder.gotProject != "loopzerokit-example" {
		t.Errorf("expected AddEnvironment project=loopzerokit-example, got %q", adder.gotProject)
	}
	if adder.gotEnv != "preview" {
		t.Errorf("expected AddEnvironment env=preview, got %q", adder.gotEnv)
	}
	// Secondary: the lister captured the most recent tuple it was queried
	// with — proves the new per-(project, env) call shape reaches it.
	if lister.gotProject != "loopzerokit-example" {
		t.Errorf("expected Lister.HasEnvironment called with project=loopzerokit-example, got %q", lister.gotProject)
	}
}

// TestEnsureEnvironment_SameProjectSameEnvName_IsNoOp is the negative-direction
// guard for the m37 fix: when the SAME (project, env) tuple already exists,
// the ensurer must short-circuit (idempotent) without calling AddEnvironment.
// Otherwise the fix would double the k8s round-trips on the happy path.
func TestEnsureEnvironment_SameProjectSameEnvName_IsNoOp(t *testing.T) {
	lister := &stubLister{
		existsByTuple: map[string]bool{
			"loopzerokit-example|preview": true,
		},
	}
	adder := &stubAdder{}
	e := &EnvironmentEnsurer{Lister: lister, Adder: adder}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "loopzerokit-example", "preview"); err != nil {
		t.Errorf("expected nil for existing (project, env) tuple, got %v", err)
	}
	if adder.callCount.Load() != 0 {
		t.Errorf("expected ZERO AddEnvironment calls for existing tuple (idempotent), got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_LegacyEmptyProject_HonoredOnShallowCheck is the
// ADR D2 regression-protector: production-legacy tuples carry empty Project
// and must still no-op when the same name is queried with project="".
func TestEnsureEnvironment_LegacyEmptyProject_HonoredOnShallowCheck(t *testing.T) {
	lister := &stubLister{
		existsByTuple: map[string]bool{
			"|production": true, // legacy entry: project=""
		},
	}
	adder := &stubAdder{}
	e := &EnvironmentEnsurer{Lister: lister, Adder: adder}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "", "production"); err != nil {
		t.Errorf("expected nil for existing legacy tuple (project=\"\"), got %v", err)
	}
	if adder.callCount.Load() != 0 {
		t.Errorf("expected ZERO AddEnvironment calls for legacy tuple no-op, got %d", adder.callCount.Load())
	}
}
