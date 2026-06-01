// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

func TestTenantProvisionerNilDB(t *testing.T) {
	prov := auth.NewTenantProvisioner(nil, nil)
	tid, role, err := prov.EnsureTenant(context.TODO(), "user-123", "test@example.com")
	if err != nil {
		t.Fatalf("EnsureTenant with nil DB should not error: %v", err)
	}
	if tid != "dev-tenant" {
		t.Errorf("tenant_id = %q, want %q", tid, "dev-tenant")
	}
	if role != "owner" {
		t.Errorf("role = %q, want %q", role, "owner")
	}
}

func TestDeriveTenantSlug(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"paulo@example.com", "paulo"},
		{"john.doe@company.com", "john-doe"},
		{"UPPER@TEST.COM", "upper"},
		{"under_score@test.com", "under-score"},
		{"a+b@test.com", "ab"},
		{"@test.com", "user"},
		{"special-chars@test.com", "special-chars"},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := auth.DeriveTenantSlug(tt.email)
			if got != tt.want {
				t.Errorf("DeriveTenantSlug(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

// B2: DeriveTenantSlug is now display-only. Collisions are harmless because
// tenant identity uses UUID, not slug. Verify slug derivation is deterministic.
func TestDeriveTenantSlug_SameEmailSameSlug(t *testing.T) {
	slug1 := auth.DeriveTenantSlug("ana@empresa.com")
	slug2 := auth.DeriveTenantSlug("ana@gmail.com")
	// Both derive to "ana" — this is fine because slug is display-only.
	// Tenant IDs are UUID-based, so no collision on identity.
	if slug1 != slug2 {
		t.Errorf("same local-part should produce same slug: %q vs %q", slug1, slug2)
	}
	if slug1 != "ana" {
		t.Errorf("slug = %q, want %q", slug1, "ana")
	}
}

// B2: Verify slug length limit for K8s label safety.
func TestDeriveTenantSlug_LongEmail(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz@test.com" // 78-char local part
	slug := auth.DeriveTenantSlug(long)
	if len(slug) > 60 {
		t.Errorf("slug length = %d, want <= 60", len(slug))
	}
}

// Regression: users.username is NOT NULL with no default in the schema.
// EnsureTenant must include username in the INSERT, otherwise new sign-ups
// fail with 23502 not_null_violation and the user sees TENANT_ERROR.
// Fix: use the Kratos UUID (userID) as username for guaranteed global uniqueness.
func TestEnsureTenant_InsertsUsernameOnNewUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.NewTextHandler(httptest.NewRecorder().Body, nil))
	prov := auth.NewTenantProvisioner(db, logger)

	userID := "3095bb15-0b35-47c2-b6d6-3af72676f40d"
	email := "paulo@loopzero.test"
	noRows := sqlmock.NewRows([]string{"id", "role"})

	// 1) Fast-path SELECT — no existing membership.
	mock.ExpectQuery(`SELECT t\.id, tm\.role FROM tenants t JOIN tenant_members tm`).
		WithArgs(userID).
		WillReturnRows(noRows)

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).
		WithArgs(userID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	// 2) Re-check inside the lock — still no membership.
	mock.ExpectQuery(`SELECT t\.id, tm\.role FROM tenants t JOIN tenant_members tm`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role"}))

	// 3) THE assertion this test exists for: INSERT into users includes the
	//    `username` column, and username==userID for global uniqueness.
	insertUserRe := regexp.QuoteMeta(`INSERT INTO users (id, email, display_name, username, created_at, updated_at)`)
	mock.ExpectExec(insertUserRe).
		WithArgs(userID, email, "paulo", userID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 4) Tenant + membership + subscription — we don't assert form, only that they fire.
	mock.ExpectExec(`INSERT INTO tenants`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO tenant_members`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO subscriptions`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	_, _, err = prov.EnsureTenant(context.TODO(), userID, email)
	if err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations not met (likely INSERT users missing `username` column): %v", err)
	}
}
