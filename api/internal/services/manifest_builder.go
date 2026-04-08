// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/pkg/naming"
)

// BuildManifestAppsInput holds the parameters for building manifest app inputs.
// F-010: shared between DomainProvisioner and BuildReconcileLoop.
type BuildManifestAppsInput struct {
	RegistryURL   string
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	Apps          []db.BuildAppSpec
	DomainStore   CustomDomainQuerier // nil-safe — no custom domains when nil
	ExcludeDomain string              // optional: domain to exclude (for removeDomain flow)
}

// BuildManifestApps constructs []manifests.AppManifestInput with custom domains attached.
// This is the single source of truth for manifest app construction, used by both the
// DomainProvisioner and the BuildReconcileLoop. Eliminates code duplication (F-010).
//
// Returns an error if any custom domain query fails — callers must treat this as fatal
// to prevent silent erasure of IngressRoutes (CODE-002).
func BuildManifestApps(ctx context.Context, input BuildManifestAppsInput) ([]manifests.AppManifestInput, error) {
	var result []manifests.AppManifestInput

	for _, app := range input.Apps {
		imageRef := naming.ImageTag(input.RegistryURL, input.TenantID, input.ProjectID, app.Name, input.CommitSHA)

		var customDomains []manifests.CustomDomainInput
		if input.DomainStore != nil {
			domains, err := input.DomainStore.ListActiveByApp(ctx, input.TenantID, input.ProjectID, app.Name, input.Environment)
			if err != nil {
				return nil, fmt.Errorf("custom domain query failed for app %s: %w", app.Name, err)
			}
			for _, d := range domains {
				if d.Domain == input.ExcludeDomain {
					continue
				}
				customDomains = append(customDomains, manifests.CustomDomainInput{
					Domain:    d.Domain,
					ID:        d.ID,
					CFManaged: d.IsCFManaged(),
				})
			}
		}

		result = append(result, manifests.AppManifestInput{
			Name:          app.Name,
			Type:          app.Type,
			Rendering:     app.Rendering,
			Port:          app.Port,
			ImageRef:      imageRef,
			Secrets:       app.Secrets,
			CustomDomains: customDomains,
		})
	}

	return result, nil
}

// BuildAppRelease constructs an AppRelease for the build callback.
// Used only by BuildReconcileLoop (not by DomainProvisioner).
func BuildAppRelease(registryURL, tenantID, projectID, appName, commitSHA, environment, baseDomain, appType, rendering string) AppRelease {
	imageRef := naming.ImageTag(registryURL, tenantID, projectID, appName, commitSHA)
	url := ""
	if appType == "server" || appType == "frontend" {
		url = fmt.Sprintf("https://%s-%s-%s-%s.%s", tenantID, projectID, appName, environment, baseDomain)
	}
	return AppRelease{
		Name:        appName,
		ImageDigest: imageRef,
		URL:         url,
		Type:        appType,
		Rendering:   rendering,
	}
}
