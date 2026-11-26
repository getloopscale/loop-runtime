// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deploycontracts

import (
	"context"
	"fmt"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// --- Test doubles ---

type fakeCustomDomainQuerier struct {
	domains map[string][]ports.CustomDomain
	err     error
}

func (f *fakeCustomDomainQuerier) ListActiveByApp(_ context.Context, tenant, project, app, env string) ([]ports.CustomDomain, error) {
	if f.err != nil {
		return nil, f.err
	}
	key := tenant + "-" + project + "-" + app + "-" + env
	return f.domains[key], nil
}

// --- Tests ---

// digestFor produces a deterministic 64-hex digest for a given app name —
// keeps tests readable while satisfying the supply-chain VAP contract.
func digestFor(app string) string {
	// 64 hex chars, padded with the app name's bytes so each app has a
	// distinct digest. The 'a' filler ensures hex validity.
	return fmt.Sprintf("%-64s", app+"deadbeef0000000000000000000000000000000000000000")[:64]
}

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
		AppDigests:  map[string]string{"api": digestFor("api")},
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
		domains: map[string][]ports.CustomDomain{
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
		AppDigests:  map[string]string{"api": digestFor("api")},
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
		domains: map[string][]ports.CustomDomain{
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
		AppDigests:    map[string]string{"api": digestFor("api")},
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
		AppDigests:  map[string]string{"api": digestFor("api")}, // valid digest so we hit the domain query path
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
		},
		AppDigests: map[string]string{
			"api":    digestFor("api"),
			"worker": digestFor("worker"),
		},
		DomainStore: nil,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// api + worker both render manifests (server|worker only stack).
	if len(apps) != 2 {
		t.Errorf("expected 2 apps (api + worker), got %d", len(apps))
	}
}

// T3.3 (plan simplify-deploy-loopzerokit-only v2.0): TestBuildManifestApps_SkipsStaticFrontend
// removed — static-frontend delivery no longer exists; every app emits K8s manifests.

// CHG-2026-04-29-DOGFOOD iter 8 — supply-chain VAP enforcement: a manifest
// rendered without an image digest produces pods Forbidden by the
// `loop-runtime-require-image-signature` ValidatingAdmissionPolicy. We must
// fail-loud at manifest build time instead of letting the pipeline run
// to admission and produce an opaque error there.
func TestBuildManifestApps_MissingDigest_FailsLoud(t *testing.T) {
	cases := []struct {
		name    string
		digests map[string]string
	}{
		{name: "nil digest map", digests: nil},
		{name: "empty digest map", digests: map[string]string{}},
		{name: "digest map missing this app", digests: map[string]string{"other": digestFor("other")}},
		{name: "explicit empty digest", digests: map[string]string{"api": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
				RegistryURL: "registry.example.com",
				TenantID:    "tn-1",
				ProjectID:   "my-app",
				Environment: "production",
				CommitSHA:   "abc123",
				Apps:        []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}},
				AppDigests:  tc.digests,
				DomainStore: nil,
			})
			if err == nil {
				t.Fatal("expected error when digest is missing — VAP would reject pods")
			}
		})
	}
}

// --- BuildAppRelease tests ---

func TestBuildAppRelease_ServerApp(t *testing.T) {
	rel := BuildAppRelease("registry.example.com", "tn-1", "my-app", "api", "sha123", "production", "dev.getloopscale.com", "server", "container")
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
	rel := BuildAppRelease("registry.example.com", "tn-1", "my-app", "worker", "sha123", "production", "dev.getloopscale.com", "worker", "container")
	if rel.URL != "" {
		t.Errorf("expected empty URL for worker app, got %q", rel.URL)
	}
}

func TestBuildAppRelease_URLIsTheCanonicalAppURLEvenWhenTheHostIsTruncated(t *testing.T) {
	long := "a-project-name-long-enough-to-push-the-subdomain-past-sixty-three"

	rel := BuildAppRelease("registry.example.com", "tn-1", long, "api", "sha", "production", "example.com", "server", "container")

	if want := naming.AppURL("tn-1", long, "api", "production", "example.com"); rel.URL != want {
		t.Errorf("URL = %q, want the host the IngressRoute serves %q", rel.URL, want)
	}
}

func TestBuildManifestApps_CarriesTheAutoscalingTargets(t *testing.T) {
	apps, err := BuildManifestApps(context.Background(), BuildManifestAppsInput{
		RegistryURL: "registry.example.com",
		TenantID:    "tn-1",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Apps: []db.BuildAppSpec{{
			Name: "api", Type: "server", Port: 3000,
			Autoscaling: &db.BuildAppAutoscaling{MinReplicas: 1, MaxReplicas: 3, TargetCPU: 70, TargetMemory: 60},
		}},
		AppDigests: map[string]string{"api": digestFor("api")},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := apps[0].Autoscaling
	if a == nil || a.MinReplicas != 1 || a.MaxReplicas != 3 || a.TargetCPU != 70 || a.TargetMemory != 60 {
		t.Errorf("autoscaling = %+v, want min 1, max 3, cpu 70, memory 60", a)
	}
}
