// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretshttp

import (
	"context"
	"net/http"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// The broker is a system entry point: ESO authenticates as a cell's service
// account and names the tenant in the path, with no user and no tenant scope.
// The secret store runs on the request pool, where `secrets` and `audit_log`
// are fail-closed under Row Level Security — so once the cell binding has
// authorized the caller for the tenant, the reads and audit writes that follow
// must run scoped to it. Before the binding, no scope is granted.

type scopeRecordingSecretReader struct{ scope string }

func (r *scopeRecordingSecretReader) ReadSecret(ctx context.Context, _, _, _, _ string) ([]byte, error) {
	r.scope, _ = dbtx.TenantScope(ctx)
	return []byte("v"), nil
}

type scopeRecordingBinding struct {
	cell   string
	scoped bool
}

func (b *scopeRecordingBinding) GetTenantCellID(ctx context.Context, _ string) (string, error) {
	_, b.scoped = dbtx.TenantScope(ctx)
	return b.cell, nil
}

type scopeRecordingAudit struct{ scopes []string }

func (a *scopeRecordingAudit) WriteAudit(ctx context.Context, _ SecretReadAuditEvent) error {
	scope, _ := dbtx.TenantScope(ctx)
	a.scopes = append(a.scopes, scope)
	return nil
}

func brokerIdentity() *auth.TokenReviewResult {
	return &auth.TokenReviewResult{Authenticated: true, Username: "system:serviceaccount:external-secrets:eso"}
}

func fetchAs(t *testing.T, deps SecretFetchHandlerDeps, cell string) int {
	t.Helper()
	srv := newTestServerWithCrossCluster(t, deps, brokerIdentity())
	defer srv.Close()
	resp, err := http.DefaultClient.Do(newAuthedRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-a/MY_SECRET.proj.production", cell))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestSecretFetchReadsTheSecretScopedToTheAuthorizedTenant(t *testing.T) {
	reader := &scopeRecordingSecretReader{}
	deps := SecretFetchHandlerDeps{
		TenantBinding: &scopeRecordingBinding{cell: "cell-0"},
		SecretReader:  reader,
		AuditWriter:   &scopeRecordingAudit{},
	}

	if code := fetchAs(t, deps, "cell-0"); code != http.StatusOK {
		t.Fatalf("fetch = %d, want 200", code)
	}

	if reader.scope != "tenant-a" {
		t.Fatalf("secret read under scope %q; want tenant-a", reader.scope)
	}
}

func TestSecretFetchResolvesTheCellBindingBeforeGrantingAnyScope(t *testing.T) {
	binding := &scopeRecordingBinding{cell: "cell-0"}
	deps := SecretFetchHandlerDeps{
		TenantBinding: binding,
		SecretReader:  &scopeRecordingSecretReader{},
		AuditWriter:   &scopeRecordingAudit{},
	}

	_ = fetchAs(t, deps, "cell-0")

	if binding.scoped {
		t.Fatal("the cell binding ran under a tenant scope; it must run before the caller is authorized")
	}
}

// A refused cross-cell attempt is recorded against the tenant it targeted.
func TestSecretFetchRecordsARefusedCrossCellAttemptScopedToTheTargetedTenant(t *testing.T) {
	audit := &scopeRecordingAudit{}
	deps := SecretFetchHandlerDeps{
		TenantBinding: &scopeRecordingBinding{cell: "cell-0"},
		SecretReader:  &scopeRecordingSecretReader{},
		AuditWriter:   audit,
	}

	if code := fetchAs(t, deps, "cell-1"); code != http.StatusForbidden {
		t.Fatalf("cross-cell fetch = %d, want 403", code)
	}

	if len(audit.scopes) != 1 || audit.scopes[0] != "tenant-a" {
		t.Fatalf("audit scopes = %v; want [tenant-a]", audit.scopes)
	}
}
