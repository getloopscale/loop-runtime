// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// =============================================================================
// Bootstrap Store tests (0% → covered)
// =============================================================================

func TestBootstrapStore_Bootstrap_HappyPath(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresBootstrapStore{DB: fake}

	userID, tenantID, created, err := store.Bootstrap(context.Background(), routes.BootstrapRequest{
		AdminLogin:        "octocat",
		AdminGitHubID:     "12345",
		TenantSlug:        "acme",
		TenantDisplayName: "Acme Corp",
		Plan:              "pro",
		CellID:            "cell-sfo3",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "github-octocat" {
		t.Errorf("expected github-octocat, got: %s", userID)
	}
	if tenantID != "tn-acme" {
		t.Errorf("expected tn-acme, got: %s", tenantID)
	}
	if !created {
		t.Error("expected created=true when rows affected")
	}
}

func TestBootstrapStore_Bootstrap_DefaultPlanAndCell(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresBootstrapStore{DB: fake}

	_, _, _, err := store.Bootstrap(context.Background(), routes.BootstrapRequest{
		AdminLogin:    "user1",
		AdminGitHubID: "111",
		TenantSlug:    "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapStore_Bootstrap_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("connection refused")}
	store := &PostgresBootstrapStore{DB: fake}

	_, _, _, err := store.Bootstrap(context.Background(), routes.BootstrapRequest{
		AdminLogin:    "user1",
		AdminGitHubID: "111",
		TenantSlug:    "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBootstrapStore_Bootstrap_Idempotent(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresBootstrapStore{DB: fake}

	_, _, created, err := store.Bootstrap(context.Background(), routes.BootstrapRequest{
		AdminLogin:    "user1",
		AdminGitHubID: "111",
		TenantSlug:    "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Error("expected created=false when no rows affected (idempotent)")
	}
}

// =============================================================================
// Tenant Deletion Store tests (0% → covered)
// =============================================================================

func TestTenantDeletionStore_CancelDeletion_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTenantDeletionStore{DB: fake}

	err := store.CancelDeletion(context.Background(), "tn-acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTenantDeletionStore_CancelDeletion_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresTenantDeletionStore{DB: fake}

	err := store.CancelDeletion(context.Background(), "tn-acme")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTenantDeletionStore_GetPendingDeletion_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTenantDeletionStore{DB: fake}

	record, err := store.GetPendingDeletion(context.Background(), "tn-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if record != nil {
		t.Error("expected nil record for nonexistent tenant")
	}
}

func TestTenantDeletionStore_GetPendingDeletion_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresTenantDeletionStore{DB: fake}

	_, err := store.GetPendingDeletion(context.Background(), "tn-test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTenantDeletionStore_RequestDeletion_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("constraint violation")}
	store := &PostgresTenantDeletionStore{DB: fake}

	grace := time.Now().Add(24 * time.Hour)
	_, err := store.RequestDeletion(context.Background(), routes.DeletionRequest{
		TenantID:    "tn-test",
		RequestedBy: "user1",
		Reason:      "test",
		GraceUntil:  &grace,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTenantDeletionStore_WriteTombstone_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTenantDeletionStore{DB: fake}

	err := store.WriteTombstone(context.Background(), routes.TombstoneRecord{
		TenantID:         "tn-test",
		TenantSlug:       "test",
		RequestedBy:      "user1",
		Reason:           "cleanup",
		ResourcesCleaned: map[string]interface{}{"builds": 5, "releases": 3},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTenantDeletionStore_WriteTombstone_WithFailed(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTenantDeletionStore{DB: fake}

	err := store.WriteTombstone(context.Background(), routes.TombstoneRecord{
		TenantID:         "tn-test",
		TenantSlug:       "test",
		RequestedBy:      "user1",
		Reason:           "cleanup",
		ResourcesCleaned: map[string]interface{}{"builds": 5},
		ResourcesFailed:  map[string]interface{}{"namespaces": "timeout"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTenantDeletionStore_WriteTombstone_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresTenantDeletionStore{DB: fake}

	err := store.WriteTombstone(context.Background(), routes.TombstoneRecord{
		TenantID:         "tn-test",
		TenantSlug:       "test",
		ResourcesCleaned: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Repository Store tests (0% → covered)
// =============================================================================

func TestRepositoryStore_SaveInstallation_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.SaveInstallation(context.Background(), 12345, "octocat", "User", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryStore_SaveInstallation_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.SaveInstallation(context.Background(), 12345, "octocat", "User", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_GetInstallation_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	inst, err := store.GetInstallation(context.Background(), 99999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst != nil {
		t.Error("expected nil for nonexistent installation")
	}
}

func TestRepositoryStore_GetInstallation_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	_, err := store.GetInstallation(context.Background(), 12345)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_DeleteInstallation_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.DeleteInstallation(context.Background(), 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryStore_DeleteInstallation_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.DeleteInstallation(context.Background(), 12345)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_LinkRepository_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.LinkRepository(context.Background(), "tn-acme", "myapp", "octocat/myapp", 12345, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryStore_LinkRepository_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("constraint violation")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.LinkRepository(context.Background(), "tn-acme", "myapp", "octocat/myapp", 12345, "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_UnlinkRepository_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.UnlinkRepository(context.Background(), "tn-acme", "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryStore_UnlinkRepository_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.UnlinkRepository(context.Background(), "tn-acme", "myapp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_GetByRepo_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	repo, err := store.GetByRepo(context.Background(), "octocat/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != nil {
		t.Error("expected nil for nonexistent repo")
	}
}

func TestRepositoryStore_GetByRepo_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	_, err := store.GetByRepo(context.Background(), "octocat/myapp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_GetByProject_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	repo, err := store.GetByProject(context.Background(), "tn-acme", "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != nil {
		t.Error("expected nil for nonexistent project")
	}
}

func TestRepositoryStore_GetByProject_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	_, err := store.GetByProject(context.Background(), "tn-acme", "myapp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateRepoID_HasPrefix(t *testing.T) {
	id := generateRepoID()
	if len(id) < 6 || id[:5] != "repo-" {
		t.Errorf("expected repo- prefix, got: %s", id)
	}
}

func TestGenerateRepoID_IsUnique(t *testing.T) {
	id1 := generateRepoID()
	id2 := generateRepoID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

// =============================================================================
// Domain Store - CreateWithCFZone tests (0% → covered)
// =============================================================================

func TestDomainStore_CreateWithCFZone_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresDomainStore{DB: fake}

	zoneID := "zone-123"
	err := store.CreateWithCFZone(context.Background(), &routes.CustomDomain{
		ID:       "dom-1",
		TenantID: "tn-acme",
		Domain:   "example.com",
		Status:   "pending_ns",
		CFZoneID: &zoneID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_CreateWithCFZone_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresDomainStore{DB: fake}

	err := store.CreateWithCFZone(context.Background(), &routes.CustomDomain{
		ID:       "dom-1",
		TenantID: "tn-acme",
		Domain:   "example.com",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Release Store — FindPendingReleases/FindActiveReleases with explicit cellID
// =============================================================================

func TestReleaseStore_FindPendingReleases_WithCellID(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	releases, err := store.FindPendingReleases(context.Background(), "cell-sfo3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("expected empty, got %d", len(releases))
	}
}

func TestReleaseStore_FindPendingReleases_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := NewPostgresReleaseStore(fake)

	_, err := store.FindPendingReleases(context.Background(), "cell-default")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReleaseStore_FindActiveReleases_WithCellID(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	releases, err := store.FindActiveReleases(context.Background(), "cell-sfo3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("expected empty, got %d", len(releases))
	}
}

func TestReleaseStore_FindActiveReleases_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := NewPostgresReleaseStore(fake)

	_, err := store.FindActiveReleases(context.Background(), "cell-default")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReleaseStore_FindPendingReleasesByTenantEnv_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := NewPostgresReleaseStore(fake)

	_, err := store.FindPendingReleasesByTenantEnv(context.Background(), "t1", "prod")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReleaseStore_ListByEnv_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := NewPostgresReleaseStore(fake)

	_, err := store.ListByEnv(context.Background(), "t1", "p1", "prod", 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReleaseStore_GetLatestByBuildID_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetLatestByBuildID(context.Background(), "bld-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Error("expected nil for nonexistent build")
	}
}

// =============================================================================
// Domain Store — ListByTenant query error
// =============================================================================

func TestDomainStore_ListByTenant_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresDomainStore{DB: fake}

	_, _, err := store.ListByTenant(context.Background(), "tn-test", routes.DomainListFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
}
