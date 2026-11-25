// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// Real-Postgres tests for the binding of a GitHub App installation to a
// tenant. Skipped when no database is reachable; run them with DATABASE_URL
// pointing at a migrated PostgreSQL. The rule lives in one SQL statement
// (ClaimGitHubInstallation), which a fake cannot exercise.

func installationStore(t *testing.T) (*PostgresRepositoryStore, *pgxpool.Pool) {
	t.Helper()
	pool := deployEventPool(t)
	return &PostgresRepositoryStore{DB: &db.PgxAdapter{Pool: pool}}, pool
}

func seedInstallationTenants(t *testing.T, pool *pgxpool.Pool, installationID int64, tenants ...string) {
	t.Helper()
	ctx := context.Background()
	for _, tenant := range tenants {
		seedDeployEventTenant(t, ctx, pool, tenant)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM github_installations WHERE id=$1`, installationID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_installations WHERE id=$1`, installationID)
	})
}

func TestRepositoryStore_PG_ClaimBindsAnUnknownInstallationAsVerified(t *testing.T) {
	s, pool := installationStore(t)
	const id = int64(990001)
	seedInstallationTenants(t, pool, id, "tn-claim-a")
	ctx := context.Background()

	bound, err := s.ClaimInstallation(ctx, id, "acme", "Organization", "tn-claim-a", "user-1")
	if err != nil || !bound {
		t.Fatalf("ClaimInstallation = %v, %v; want true, nil", bound, err)
	}

	inst, err := s.GetInstallation(ctx, id)
	if err != nil || inst == nil {
		t.Fatalf("GetInstallation = %v, %v", inst, err)
	}
	if !inst.VerifiedFor("tn-claim-a") {
		t.Errorf("installation = %+v, want it verified for tn-claim-a", inst)
	}
	if inst.VerifiedBy == nil || *inst.VerifiedBy != "user-1" || inst.AccountLogin != "acme" || inst.AccountType != "Organization" {
		t.Errorf("installation = %+v, want verified_by user-1 and the account GitHub named", inst)
	}
}

func TestRepositoryStore_PG_ClaimBindsTheRowTheWebhookCreated(t *testing.T) {
	s, pool := installationStore(t)
	const id = int64(990002)
	seedInstallationTenants(t, pool, id, "tn-claim-a")
	ctx := context.Background()
	if err := s.SaveInstallation(ctx, id, "acme", "Organization", nil); err != nil {
		t.Fatalf("SaveInstallation: %v", err)
	}
	if inst, _ := s.GetInstallation(ctx, id); inst.VerifiedFor("tn-claim-a") {
		t.Fatal("a row the webhook created counts as verified")
	}

	bound, err := s.ClaimInstallation(ctx, id, "acme", "Organization", "tn-claim-a", "user-1")
	if err != nil || !bound {
		t.Fatalf("ClaimInstallation = %v, %v; want true, nil", bound, err)
	}
	if inst, _ := s.GetInstallation(ctx, id); !inst.VerifiedFor("tn-claim-a") {
		t.Errorf("installation = %+v, want it verified for tn-claim-a", inst)
	}
}

func TestRepositoryStore_PG_ClaimVerifiesABindingMadeBeforeVerification(t *testing.T) {
	s, pool := installationStore(t)
	const id = int64(990003)
	seedInstallationTenants(t, pool, id, "tn-claim-a")
	ctx := context.Background()
	tenant := "tn-claim-a"
	if err := s.SaveInstallation(ctx, id, "acme", "Organization", &tenant); err != nil {
		t.Fatalf("SaveInstallation: %v", err)
	}
	if inst, _ := s.GetInstallation(ctx, id); inst.VerifiedFor(tenant) {
		t.Fatal("a binding without a confirmation counts as verified")
	}

	bound, err := s.ClaimInstallation(ctx, id, "", "Organization", tenant, "user-1")
	if err != nil || !bound {
		t.Fatalf("ClaimInstallation = %v, %v; want true, nil", bound, err)
	}
	inst, _ := s.GetInstallation(ctx, id)
	if !inst.VerifiedFor(tenant) {
		t.Errorf("installation = %+v, want it verified", inst)
	}
	if inst.AccountLogin != "acme" {
		t.Errorf("account_login = %q, want the stored login kept when the claim carries none", inst.AccountLogin)
	}
}

func TestRepositoryStore_PG_ClaimLeavesAnotherTenantsInstallationAlone(t *testing.T) {
	s, pool := installationStore(t)
	const id = int64(990004)
	seedInstallationTenants(t, pool, id, "tn-claim-a", "tn-claim-b")
	ctx := context.Background()
	if _, err := s.ClaimInstallation(ctx, id, "acme", "Organization", "tn-claim-a", "user-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	bound, err := s.ClaimInstallation(ctx, id, "acme", "Organization", "tn-claim-b", "user-2")

	if err != nil || bound {
		t.Fatalf("ClaimInstallation by another tenant = %v, %v; want false, nil", bound, err)
	}
	inst, _ := s.GetInstallation(ctx, id)
	if !inst.VerifiedFor("tn-claim-a") || inst.VerifiedFor("tn-claim-b") {
		t.Errorf("installation = %+v, want it still verified for tn-claim-a only", inst)
	}
	if inst.VerifiedBy == nil || *inst.VerifiedBy != "user-1" {
		t.Errorf("verified_by = %v, want user-1 unchanged", inst.VerifiedBy)
	}
}

func TestRepositoryStore_PG_ListVerifiedInstallationsIsTheTenantsVerifiedOnes(t *testing.T) {
	s, pool := installationStore(t)
	const verified, unverified, others = int64(990005), int64(990006), int64(990007)
	seedInstallationTenants(t, pool, verified, "tn-claim-a", "tn-claim-b")
	seedInstallationTenants(t, pool, unverified)
	seedInstallationTenants(t, pool, others)
	ctx := context.Background()
	tenant := "tn-claim-a"
	if _, err := s.ClaimInstallation(ctx, verified, "acme", "Organization", tenant, "user-1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.SaveInstallation(ctx, unverified, "acme-old", "User", &tenant); err != nil {
		t.Fatalf("save unverified: %v", err)
	}
	if _, err := s.ClaimInstallation(ctx, others, "globex", "Organization", "tn-claim-b", "user-2"); err != nil {
		t.Fatalf("claim for the other tenant: %v", err)
	}

	list, err := s.ListVerifiedInstallations(ctx, tenant)

	if err != nil {
		t.Fatalf("ListVerifiedInstallations: %v", err)
	}
	if len(list) != 1 || list[0].ID != verified {
		t.Errorf("list = %+v, want only installation %d", list, verified)
	}
}

// The webhook's upsert refreshes the account of an installation. It is not a
// way to move an installation to another tenant: only the claim binds.
func TestRepositoryStore_PG_SaveInstallationNeverMovesABoundInstallation(t *testing.T) {
	s, pool := installationStore(t)
	const id = int64(990006)
	seedInstallationTenants(t, pool, id, "tn-claim-a", "tn-claim-b")
	ctx := context.Background()
	if bound, err := s.ClaimInstallation(ctx, id, "acme", "Organization", "tn-claim-a", "user-1"); err != nil || !bound {
		t.Fatalf("ClaimInstallation = %v, %v; want true, nil", bound, err)
	}

	other := "tn-claim-b"
	if err := s.SaveInstallation(ctx, id, "acme-renamed", "Organization", &other); err != nil {
		t.Fatalf("SaveInstallation: %v", err)
	}
	if err := s.SaveInstallation(ctx, id, "", "Organization", nil); err != nil {
		t.Fatalf("SaveInstallation without a tenant: %v", err)
	}

	inst, err := s.GetInstallation(ctx, id)
	if err != nil || inst == nil {
		t.Fatalf("GetInstallation = %v, %v", inst, err)
	}
	if !inst.VerifiedFor("tn-claim-a") || inst.VerifiedFor("tn-claim-b") {
		t.Errorf("installation = %+v, want it still verified for tn-claim-a only", inst)
	}
	if inst.AccountLogin != "acme-renamed" {
		t.Errorf("account_login = %q, want the upsert to refresh it", inst.AccountLogin)
	}
}
