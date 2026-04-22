// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// --- Test doubles ---

type fakeCustomDomainQuerier struct {
	domains map[string][]routes.CustomDomain
	err     error
}

func (f *fakeCustomDomainQuerier) ListActiveByApp(_ context.Context, tenant, project, app, env string) ([]routes.CustomDomain, error) {
	if f.err != nil {
		return nil, f.err
	}
	key := tenant + "-" + project + "-" + app + "-" + env
	return f.domains[key], nil
}

// --- Tests ---

func TestBuildManifestApps_NoDomainStore(t *testing.T) {
	apps, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
		RegistryURL: "registry.example.com",
		TenantID:    "tn-1",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Apps: []db.BuildAppSpec{
			{Name: "api", Type: "server", Port: 3000, Rendering: "container"},
		},
		DomainStore: nil, // no domain store
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	if apps[0].Name != "api" {
		t.Errorf("expected app name 'api', got %q", apps[0].Name)
	}
	if len(apps[0].CustomDomains) != 0 {
		t.Errorf("expected no custom domains, got %d", len(apps[0].CustomDomains))
	}
}

func TestBuildManifestApps_WithCustomDomains(t *testing.T) {
	domainStore := &fakeCustomDomainQuerier{
		domains: map[string][]routes.CustomDomain{
			"tn-1-my-app-api-production": {
				{ID: "d1", Domain: "api.example.com", TenantID: "tn-1", ProjectName: "my-app"},
				{ID: "d2", Domain: "api2.example.com", TenantID: "tn-1", ProjectName: "my-app"},
			},
		},
	}

	apps, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
		RegistryURL: "registry.example.com",
		TenantID:    "tn-1",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Apps: []db.BuildAppSpec{
			{Name: "api", Type: "server", Port: 3000},
		},
		DomainStore: domainStore,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps[0].CustomDomains) != 2 {
		t.Errorf("expected 2 custom domains, got %d", len(apps[0].CustomDomains))
	}
}

func TestBuildManifestApps_ExcludesDomain(t *testing.T) {
	domainStore := &fakeCustomDomainQuerier{
		domains: map[string][]routes.CustomDomain{
			"tn-1-my-app-api-production": {
				{ID: "d1", Domain: "keep.example.com"},
				{ID: "d2", Domain: "exclude.example.com"},
			},
		},
	}

	apps, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
		RegistryURL:   "registry.example.com",
		TenantID:      "tn-1",
		ProjectID:     "my-app",
		Environment:   "production",
		CommitSHA:     "abc123",
		Apps:          []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}},
		DomainStore:   domainStore,
		ExcludeDomain: "exclude.example.com",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps[0].CustomDomains) != 1 {
		t.Errorf("expected 1 custom domain (excluded 1), got %d", len(apps[0].CustomDomains))
	}
	if apps[0].CustomDomains[0].Domain != "keep.example.com" {
		t.Errorf("expected keep.example.com, got %q", apps[0].CustomDomains[0].Domain)
	}
}

func TestBuildManifestApps_DomainQueryError_ReturnsFatalError(t *testing.T) {
	domainStore := &fakeCustomDomainQuerier{
		err: fmt.Errorf("database connection refused"),
	}

	_, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
		RegistryURL: "registry.example.com",
		TenantID:    "tn-1",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Apps:        []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}},
		DomainStore: domainStore,
	})

	if err == nil {
		t.Fatal("expected error when domain query fails")
	}
}

func TestBuildManifestApps_MultipleApps(t *testing.T) {
	apps, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
		RegistryURL: "registry.example.com",
		TenantID:    "tn-1",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Apps: []db.BuildAppSpec{
			{Name: "api", Type: "server", Port: 3000},
			{Name: "worker", Type: "worker"},
			{Name: "web", Type: "frontend", Rendering: "static"},
		},
		DomainStore: nil,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}
}

// --- BuildAppRelease tests ---

func TestBuildAppRelease_ServerApp(t *testing.T) {
	rel := BuildAppRelease("registry.example.com", "tn-1", "my-app", "api", "sha123", "production", "dev.loopzero.example", "server", "container")
	if rel.Name != "api" {
		t.Errorf("expected name 'api', got %q", rel.Name)
	}
	if rel.URL == "" {
		t.Error("expected URL for server app")
	}
	if rel.Type != "server" {
		t.Errorf("expected type 'server', got %q", rel.Type)
	}
}

func TestBuildAppRelease_WorkerApp_NoURL(t *testing.T) {
	rel := BuildAppRelease("registry.example.com", "tn-1", "my-app", "worker", "sha123", "production", "dev.loopzero.example", "worker", "container")
	if rel.URL != "" {
		t.Errorf("expected empty URL for worker app, got %q", rel.URL)
	}
}

func TestBuildAppRelease_FrontendApp(t *testing.T) {
	rel := BuildAppRelease("registry.example.com", "tn-1", "my-app", "web", "sha123", "production", "dev.loopzero.example", "frontend", "static")
	if rel.URL == "" {
		t.Error("expected URL for frontend app")
	}
	if rel.Rendering != "static" {
		t.Errorf("expected rendering 'static', got %q", rel.Rendering)
	}
}
