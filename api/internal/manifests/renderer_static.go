// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import "fmt"

// renderStaticFrontendApp generates IngressRoute + Middleware + Certificate for a
// static frontend served from S3 via nginx-s3-gateway proxy.
// NO Deployment, NO Service (uses shared static-assets Service in loopzero-system).
//
// Architecture (MEET-2026-03-26-011):
//
//	Browser → Traefik IngressRoute → Middleware (addPrefix) → static-assets Service → nginx → DO Spaces
//
// Same hostname as container apps — zero URL divergence.
// The Middleware adds the S3 path prefix so nginx proxies to the correct build's assets.
// SPA routing handled by nginx config (404 → index.html fallback).
func renderStaticFrontendApp(ri RenderInput, app AppManifestInput) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	// Same hostname format as container apps — NO static. prefix (DX VETO from MEET-011)
	hostname := appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)
	// Achado #17: project-scoped name avoids collision when two projects in
	// the same tenant env share an app name (e.g., both have a `web`).
	tlsSecretName := fmt.Sprintf("tls-%s", resName)

	// S3 path prefix: /{tenant}/{project}/{app}/{build-id}
	s3PathPrefix := app.S3Prefix
	if s3PathPrefix == "" {
		versionID := ri.BuildID
		if versionID == "" {
			versionID = ri.ReleaseID
		}
		s3PathPrefix = fmt.Sprintf("/%s/%s/%s/%s", ri.TenantID, ri.ProjectID, app.Name, versionID)
	}

	// Certificate (same as container apps)
	cert := certManagerCertificate{
		APIVersion: "cert-manager.io/v1",
		Kind:       "Certificate",
		Metadata: k8sMetadata{
			Name:      tlsSecretName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: certManagerCertificateSpec{
			SecretName: tlsSecretName,
			IssuerRef: certIssuerRef{
				Name: ri.clusterIssuerName(),
				Kind: "ClusterIssuer",
			},
			DNSNames: []string{hostname},
		},
	}

	// Middleware: addPrefix to route to the correct S3 path for this build
	rewriteMiddleware := traefikMiddleware{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "Middleware",
		Metadata: k8sMetadata{
			Name:      fmt.Sprintf("static-prefix-%s", resName),
			Namespace: ns,
			Labels:    labels,
		},
		Spec: traefikMiddlewareSpec{
			AddPrefix: &traefikAddPrefix{
				Prefix: s3PathPrefix,
			},
		},
	}

	// IngressRoute: routes to shared nginx-s3-gateway in loopzero-system namespace.
	ingress := traefikIngressRoute{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "IngressRoute",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
			Annotations: map[string]string{
				"loopzero.io/delivery":  "static",
				"loopzero.io/s3-prefix": s3PathPrefix,
				"loopzero.io/build-id":  ri.BuildID,
			},
		},
		Spec: traefikIngressRouteSpec{
			EntryPoints: []string{"web", "websecure"},
			Routes: []traefikRoute{
				{
					Match: fmt.Sprintf("Host(`%s`)", hostname),
					Kind:  "Rule",
					Middlewares: []traefikRouteMiddleware{
						{
							Name:      fmt.Sprintf("static-prefix-%s", resName),
							Namespace: ns,
						},
					},
					Services: []traefikRouteService{
						{
							Name:      "static-assets",
							Namespace: ri.gatewayNamespace(),
							Port:      80,
						},
					},
				},
			},
			TLS: &traefikTLS{SecretName: tlsSecretName},
		},
	}

	return []string{
		marshalResource(cert),
		marshalResource(rewriteMiddleware),
		marshalResource(ingress),
	}
}
