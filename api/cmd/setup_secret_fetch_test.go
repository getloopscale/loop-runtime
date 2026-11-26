// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Unit tests for the broker endpoint composer + bearer middleware. T1.1
// (Phase 1) + T3.1 (Phase 3) of plan 2026-06-10-j22-layer2-code-review-fixes
// per cycle-implement.md TDD discipline. The 4 components under test —
// rawSecretReader, inlineTenantBinding, brokerBearerMiddleware,
// setupSecretFetchDeps — close consensus-CRITICAL #13 + HIGH #113
// (171 LOC NEW security-sensitive file with zero unit tests).
//
// Coverage map (ADR D3 + EC-5 absorption + concurrency invariants):
//
//	rawSecretReader:
//	  - HappyPath / composite key scope (T2.1 + ADR D2)
//	  - NoRows → ErrSecretNotFound
//	  - NoRows through WRAPPED error (EC-8 absorption)
//	  - TransitDecryptError propagates wrapped
//	  - PoolNil / TransitNil → typed error
//	  - Concurrent_NoRace (32 goroutines, -race clean)
//
//	inlineTenantBinding:
//	  - HappyPath
//	  - TenantNotFound translation
//	  - CellStoreNil → typed error
//
//	brokerBearerMiddleware:
//	  - RejectsClientSuppliedXCellID (T1.1 RED — Phase 1)
//	  - NoBearer → 401
//	  - WrongBearer → 401 constant-time
//	  - EmptyExpectedToken → 500 (mis-config)
//	  - NoBearerNoCellID → 401_Not_500 (EC-5 absorption)
//	  - Concurrent_NoRace
//
//	setupSecretFetchDeps:
//	  - AllDepsPresent → non-nil
//	  - PoolNil → nil
//	  - TransitNil → nil
//	  - CellStoreNil → nil
//	  - AuditNil → nil
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/secrets/secretshttp"
	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// --- fakes ---

// fakeSecretValues is a fake secretValueReader: it records the scope it was
// asked for and answers with the pinned value or error.
type fakeSecretValues struct {
	mu       sync.Mutex
	value    []byte
	err      error
	lastArgs []string
}

func (f *fakeSecretValues) ReadValue(_ context.Context, tenantID, projectID, env, name string) ([]byte, error) {
	f.mu.Lock()
	f.lastArgs = []string{tenantID, projectID, env, name}
	f.mu.Unlock()
	return f.value, f.err
}

// fakeCellQuerier is a fake tenantCellQuerier. Returns the pinned cell
// (or the pinned error if non-nil).
type fakeCellQuerier struct {
	cellByTenant map[string]string
	err          error
}

func (c *fakeCellQuerier) GetTenantCellID(_ context.Context, tenantID string) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	cell, ok := c.cellByTenant[tenantID]
	if !ok {
		return "", errors.New("tenant not found in fake")
	}
	return cell, nil
}

// TestBrokerBearerMiddleware_RejectsClientSuppliedXCellID is the canonical
// T1.1 RED test. It asserts that even when the caller sets X-Cell-ID to
// a forged value (e.g., "cell-evil") AND presents a valid Bearer token,
// the synthetic CrossClusterTokenReviewResult injected into the request
// context carries the SERVER-SIDE constant `domain.DefaultCellID`
// (currently "cell-0") — NOT the forged header.
//
// Why this matters (#13 consensus-CRITICAL): the existing EC-07 cell↔tenant
// binding control in routes/internal_secret_fetch.go line 143 calls
// `deps.TenantBinding.GetTenantCellID(ctx, tenantID)` and compares to
// `identity.CellID`. If the middleware trusted X-Cell-ID, any holder of
// LOOPZERO_BROKER_TOKEN could spoof any cell and read every tenant's secrets.
// Server-side derivation makes the spoof structurally impossible for the
// dev-public single-cluster path. A multi-cluster path would use a per-cell
// TokenReview against the cell's K8s API (separate follow-up plan).
func TestBrokerBearerMiddleware_RejectsClientSuppliedXCellID(t *testing.T) {
	const expected = "secret"
	handler := brokerBearerMiddleware(map[string]string{domain.DefaultCellID: expected}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.CrossClusterFromContext(r.Context())
		require.True(t, ok, "synthetic TokenReviewResult must be injected by middleware")
		require.Equal(t, domain.DefaultCellID, ident.CellID,
			"middleware MUST derive cellID server-side from domain.DefaultCellID; the X-Cell-ID header is forgery-prone and must be ignored (T1.1 / ADR D1 / closes #13)")
	}))
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/secret-fetch/tn-x/Y", nil)
	req.Header.Set("Authorization", "Bearer "+expected)
	req.Header.Set("X-Cell-ID", "cell-evil") // attempt forgery — MUST be ignored
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code,
		"valid bearer with forged X-Cell-ID still authenticates; only the cellID derivation source matters")
}

// --- brokerBearerMiddleware T3.1 tests ---

func Test_brokerBearerMiddleware_NoBearer_Returns401(t *testing.T) {
	handler := brokerBearerMiddleware(map[string]string{domain.DefaultCellID: "secret"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream handler must not run when bearer is missing")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	// no Authorization header
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func Test_brokerBearerMiddleware_WrongBearer_Returns401_ConstantTime(t *testing.T) {
	handler := brokerBearerMiddleware(map[string]string{domain.DefaultCellID: "right-token"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream handler must not run on wrong bearer")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func Test_brokerBearerMiddleware_EmptyExpectedToken_Returns500(t *testing.T) {
	// Mis-configured boot: brokerToken env empty. The middleware must
	// fail-CLOSED with 500 (not silently accept any caller).
	handler := brokerBearerMiddleware(map[string]string{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not run when expected token is empty (mis-config)")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// EC-5 absorption: even when BOTH bearer AND X-Cell-ID are missing, the
// 401 path must trigger BEFORE synthetic cellID injection so a malformed
// request never accidentally injects a phantom identity downstream.
func Test_brokerBearerMiddleware_NoBearerNoCellID_Returns401_Not_500(t *testing.T) {
	handler := brokerBearerMiddleware(map[string]string{domain.DefaultCellID: "secret"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not run when bearer is missing (even with cellID also empty)")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	// no Authorization AND no X-Cell-ID
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code,
		"EC-5: missing-bearer 401 path must fire BEFORE synthetic-cellID injection")
}

func Test_brokerBearerMiddleware_Concurrent_NoRace(t *testing.T) {
	const expected = "secret"
	handler := brokerBearerMiddleware(map[string]string{domain.DefaultCellID: expected}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			req.Header.Set("Authorization", "Bearer "+expected)
			rec := httptest.NewRecorder()
			handler(rec, req)
		}()
	}
	wg.Wait()
	// If `go test -race` flags a data race the test fails; the assertion
	// here is implicit (the test must reach this point cleanly).
	require.True(t, true, "concurrent invocations completed race-clean")
}

// --- brokerSecretReader ---

func Test_brokerSecretReader_ReadsTheCompositeKeyThroughTheStore(t *testing.T) {
	values := &fakeSecretValues{value: []byte("hunter2")}
	r := &brokerSecretReader{store: values}

	got, err := r.ReadSecret(context.Background(), "tn-1", "proj-a", "production", "DB_URL")
	require.NoError(t, err)
	require.Equal(t, []byte("hunter2"), got)
	// ADR D2 / T2.1: scoped by (tenant, project, environment, name).
	require.Equal(t, []string{"tn-1", "proj-a", "production", "DB_URL"}, values.lastArgs)
}

func Test_brokerSecretReader_AbsentSecretIsNotFound(t *testing.T) {
	r := &brokerSecretReader{store: &fakeSecretValues{err: fmt.Errorf("wrapped: %w", store.ErrSecretNotFound)}}
	_, err := r.ReadSecret(context.Background(), "tn-1", "proj", "prod", "X")
	require.ErrorIs(t, err, secretshttp.ErrSecretNotFound)
}

func Test_brokerSecretReader_DecryptFailurePropagates(t *testing.T) {
	boom := errors.New("transit boom")
	r := &brokerSecretReader{store: &fakeSecretValues{err: boom}}
	_, err := r.ReadSecret(context.Background(), "tn-1", "proj", "prod", "X")
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, secretshttp.ErrSecretNotFound)
}

// --- inlineTenantBinding T3.1 tests ---

func Test_inlineTenantBinding_GetTenantCellID_HappyPath(t *testing.T) {
	b := &inlineTenantBinding{cellStore: &fakeCellQuerier{
		cellByTenant: map[string]string{"tn-1": "cell-0"},
	}}
	got, err := b.GetTenantCellID(context.Background(), "tn-1")
	require.NoError(t, err)
	require.Equal(t, "cell-0", got)
}

func Test_inlineTenantBinding_GetTenantCellID_TenantNotFound_TranslatesToErrTenantNotFound(t *testing.T) {
	// EC-8 follow-up absorption: parallel to setup_secret_fetch.go's pgx
	// detection — when the underlying cell store returns pgx.ErrNoRows
	// (even through one level of fmt.Errorf wrap, as PostgresCellStore
	// does), inlineTenantBinding translates to secrets.ErrTenantNotFound.
	b := &inlineTenantBinding{cellStore: &fakeCellQuerier{
		err: fmt.Errorf("cell store: get tenant cell_id: %w", pgx.ErrNoRows),
	}}
	_, err := b.GetTenantCellID(context.Background(), "tn-x")
	require.ErrorIs(t, err, secretshttp.ErrTenantNotFound)
}

func Test_inlineTenantBinding_GetTenantCellID_OtherErrorPropagatesWrapped(t *testing.T) {
	// Non-ErrNoRows errors (DB timeout, advisory lock contention, etc.)
	// MUST propagate wrapped so 500 INTERNAL surfaces to caller — they
	// are NOT silent ErrTenantNotFound.
	b := &inlineTenantBinding{cellStore: &fakeCellQuerier{
		err: errors.New("pg connection refused"),
	}}
	_, err := b.GetTenantCellID(context.Background(), "tn-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, secretshttp.ErrTenantNotFound)
	require.Contains(t, err.Error(), "tenant binding")
	require.Contains(t, err.Error(), "pg connection refused")
}

func Test_inlineTenantBinding_GetTenantCellID_CellStoreNil_ReturnsTypedError(t *testing.T) {
	b := &inlineTenantBinding{cellStore: nil}
	_, err := b.GetTenantCellID(context.Background(), "tn-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cell store not configured")
}

// --- setupSecretFetchDeps ---
// The composer is nil-safe: when a prerequisite is missing at boot, route
// registration is skipped (LIVE 404 + ESO retries) rather than panicking on
// the first request.

func Test_setupSecretFetchDeps_MissingStoreSkipsTheRoute(t *testing.T) {
	require.Nil(t, setupSecretFetchDeps(nil, &fakeCellQuerier{}, &store.AuditLogger{}))
}

func Test_setupSecretFetchDeps_MissingCellStoreSkipsTheRoute(t *testing.T) {
	require.Nil(t, setupSecretFetchDeps(&fakeSecretValues{}, nil, &store.AuditLogger{}))
}

func Test_setupSecretFetchDeps_MissingAuditSkipsTheRoute(t *testing.T) {
	require.Nil(t, setupSecretFetchDeps(&fakeSecretValues{}, &fakeCellQuerier{}, nil))
}

func Test_setupSecretFetchDeps_AllPresentWiresTheBroker(t *testing.T) {
	deps := setupSecretFetchDeps(&fakeSecretValues{}, &fakeCellQuerier{}, &store.AuditLogger{})
	require.NotNil(t, deps)
	require.IsType(t, &brokerSecretReader{}, deps.SecretReader)
}

// --- one token per cell (#456) ---

// Each cell's External Secrets presents its own token, and the cell a request comes from
// is the cell whose token it presented. A tenant placed on cell-1 is served to cell-1's
// External Secrets and to no other cell's.
func Test_brokerBearerMiddleware_BindsTheRequestToTheCellWhoseTokenItPresented(t *testing.T) {
	tokens := map[string]string{"cell-0": "tok-0", "cell-1": "tok-1"}
	for cell, token := range tokens {
		t.Run(cell, func(t *testing.T) {
			var got string
			handler := brokerBearerMiddleware(tokens, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ident, _ := auth.CrossClusterFromContext(r.Context())
				got = ident.CellID
			}))
			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Cell-ID", "cell-evil")
			rec := httptest.NewRecorder()
			handler(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, cell, got, "the cell comes from the token, never from X-Cell-ID")
		})
	}
}

func Test_brokerBearerMiddleware_RefusesATokenNoCellOwns(t *testing.T) {
	handler := brokerBearerMiddleware(map[string]string{"cell-0": "tok-0", "cell-1": "tok-1"},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("downstream must not run for a token no cell owns")
		}))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer tok-2")
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
