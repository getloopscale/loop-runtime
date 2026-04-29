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
		// CHG-2026-04-29-DOGFOOD-FB iter 1 — static frontend apps are
		// delivered via S3 + CF Worker (Sprint 10), NOT via K8s manifests.
		// They have no image, no digest, no Deployment. Skip the digest
		// check and the manifest emission entirely. Skipping here means
		// the rendered manifest set excludes static apps — which is
		// exactly what `manifests.Render` expects (it generates Deployment
		// + Service + IngressRoute only for entries it receives).
		// LoopZero enforces frontend == static in app_unit.go:NormalizeApp,
		// so we use type=frontend as the skip predicate (safer than
		// trusting Rendering field which may be "" in legacy specs).
		if app.Type == "frontend" {
			continue
		}

		// CHG-2026-04-29-DOGFOOD iter 8 — supply-chain VAP enforcement.
		// Mutable tags are rejected by `loopzero-require-image-signature` em
		// tenant namespaces. Sem digest emitido pelo build pipeline o
		// manifest produziria pods Forbidden no admission. Fail-loud aqui
		// é melhor que silent failure no admission depois — caller é
		// responsável por carregar digest (build_reconcile_complete via
		// buildApps; domain_provisioner via attestation store).
		// Não há fallback para ImageTag — legacy paths foram removidos.
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
			Rendering:     app.Rendering,
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
