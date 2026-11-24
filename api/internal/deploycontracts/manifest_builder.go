// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"context"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/manifests"
	"github.com/getloopscale/loopzero/pkg/naming"
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

	// AppDigests maps app name → sha256 digest captured by the build
	// pipeline (cosign step output, with fallback to BuildKit digest).
	// When present, the manifest renders use `ImageDigestRef` instead of
	// `ImageTag` — required by the supply-chain VAP
	// `loopzero-require-image-signature` which rejects mutable tag refs in
	// tenant namespaces (CHG-2026-04-29-DOGFOOD iter 8: discovered live
	// when standalone-multi pods got Forbidden by the VAP).
	// Nil/empty map → fall back to ImageTag (legacy path).
	AppDigests map[string]string
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
		// T3.3 (plan simplify-deploy-loopzerokit-only v2.0): the static-frontend
		// skip predicate is removed — frontend/static delivery no longer exists
		// (server|worker only). Every app emits K8s manifests.

		// CHG-2026-04-29-DOGFOOD iter 8 — supply-chain VAP enforcement.
		// Mutable tags are rejected by `loopzero-require-image-signature` in
		// tenant namespaces. Without a digest from the build pipeline the
		// manifest would produce pods Forbidden at admission. Failing loud
		// here beats a silent admission failure later — the caller is
		// responsible for supplying the digest (build_reconcile_complete via
		// buildApps; domain_provisioner via the release's image digest).
		// There is no fallback to an image tag: the legacy paths were removed.
		dig, ok := input.AppDigests[app.Name]
		if !ok || dig == "" {
			return nil, fmt.Errorf("manifest_builder: missing image digest for app %q (tenant=%s project=%s); "+
				"the supply-chain VAP rejects tag-only references and the build pipeline must emit "+
				"the cosign digest output before manifests can be rendered",
				app.Name, input.TenantID, input.ProjectID)
		}
		imageRef := naming.ImageDigestRef(input.RegistryURL, input.TenantID, input.ProjectID, app.Name, dig)

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

		mi := manifests.AppManifestInput{
			Name:          app.Name,
			Type:          app.Type,
			Port:          app.Port,
			ImageRef:      imageRef,
			Secrets:       app.Secrets,
			Replicas:      app.Replicas,
			CustomDomains: customDomains,
		}
		if app.Autoscaling != nil {
			mi.Autoscaling = &manifests.AutoscalingConfig{
				MinReplicas: app.Autoscaling.MinReplicas,
				MaxReplicas: app.Autoscaling.MaxReplicas,
				TargetCPU:   app.Autoscaling.TargetCPU,
			}
		}
		result = append(result, mi)
	}

	return result, nil
}

// BuildAppRelease constructs an AppRelease for the build callback.
// Used only by BuildReconcileLoop (not by DomainProvisioner).
func BuildAppRelease(registryURL, tenantID, projectID, appName, commitSHA, environment, baseDomain, appType, rendering string) AppRelease {
	imageRef := naming.ImageTag(registryURL, tenantID, projectID, appName, commitSHA)
	url := ""
	if appType == "server" {
		url = naming.AppURL(tenantID, projectID, appName, environment, baseDomain)
	}
	return AppRelease{
		Name:        appName,
		ImageDigest: imageRef,
		URL:         url,
		Type:        appType,
		Rendering:   rendering,
	}
}
