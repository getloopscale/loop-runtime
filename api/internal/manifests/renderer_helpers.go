// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"fmt"
	"strings"

	sigsyaml "sigs.k8s.io/yaml"
)

// appHostname returns the flat hostname for an app.
// Format: {tenant}-{project}-{app}-{env}.{baseDomain}
// This produces a single-level subdomain under baseDomain, which is compatible
// with wildcard TLS certificates (*.dev.loopzero.example covers all apps).
func appHostname(tenantID, projectID, appName, environment, baseDomain string) string {
	subdomain := fmt.Sprintf("%s-%s-%s-%s", tenantID, projectID, appName, environment)
	// Sanitize: lowercase, max 63 chars, RFC 1123
	if len(subdomain) > 63 {
		subdomain = subdomain[:63]
	}
	// Remove trailing hyphens
	subdomain = strings.TrimRight(subdomain, "-")
	return fmt.Sprintf("%s.%s", subdomain, baseDomain)
}

// commonLabels returns the standard labels for all resources.
func commonLabels(ri RenderInput, app AppManifestInput) map[string]string {
	return map[string]string{
		"app":                          app.Name,
		"app.kubernetes.io/managed-by": "loopzero",
		"loopzero.io/tenant":               ri.TenantID,
		"loopzero.io/project":              ri.ProjectID,
		"loopzero.io/environment":          ri.Environment,
		"loopzero.io/app-type":             app.Type,
	}
}

// namespace returns the Kubernetes namespace for a tenant environment.
func int64Ptr(v int64) *int64 { return &v }

func namespace(tenantID, environment string) string {
	return fmt.Sprintf("loopzero-%s-%s", tenantID, environment)
}

// marshalResource serializes a struct to YAML using sigs.k8s.io/yaml.
// This library marshals to JSON first (sorted keys) then converts to YAML,
// guaranteeing deterministic output regardless of map iteration order.
func marshalResource(v interface{}) string {
	data, err := sigsyaml.Marshal(v)
	if err != nil {
		// This should never happen with our controlled struct types.
		// If it does, it is a programming error — panic is appropriate.
		panic(fmt.Sprintf("manifests: failed to marshal resource: %v", err))
	}
	return strings.TrimSpace(string(data))
}

// renderServerApp generates Deployment + Service + IngressRoute for a server app.
func renderServerApp(ri RenderInput, app AppManifestInput) []string {
	port := app.Port
	if port == 0 {
		port = 3000 // sensible default
	}

	deployment := buildDeployment(ri, app, port)
	pdb := buildPDB(ri, app)
	service := buildService(ri, app, port)
	ingressResources := buildIngressResources(ri, app, port)

	docs := []string{
		marshalResource(deployment),
		marshalResource(pdb),
		marshalResource(service),
	}
	docs = append(docs, ingressResources...)
	return docs
}

// renderWorkerApp generates Deployment + PDB (no Service/Ingress — workers don't receive traffic).
func renderWorkerApp(ri RenderInput, app AppManifestInput) []string {
	deployment := buildDeployment(ri, app, 0)
	pdb := buildPDB(ri, app)
	return []string{marshalResource(deployment), marshalResource(pdb)}
}

// renderFrontendApp generates Deployment + PDB + Service + IngressRoute for a frontend app.
// The container serves static assets via a lightweight HTTP server.
func renderFrontendApp(ri RenderInput, app AppManifestInput) []string {
	port := app.Port
	if port == 0 {
		port = 80 // typical for frontend static servers
	}

	deployment := buildDeployment(ri, app, port)
	pdb := buildPDB(ri, app)
	service := buildService(ri, app, port)
	ingressResources := buildIngressResources(ri, app, port)

	docs := []string{
		marshalResource(deployment),
		marshalResource(pdb),
		marshalResource(service),
	}
	docs = append(docs, ingressResources...)
	return docs
}
