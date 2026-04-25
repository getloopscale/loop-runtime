// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package manifests generates Kubernetes manifests for tenant applications.
//
// C-02 fix: Manifest generation moved from the build pipeline (Argo Workflow CWT)
// to the API (control plane). This is the correct separation of concerns for a PaaS:
//   - Build plane: produces container images (BuildKit)
//   - Control plane: decides what to deploy (manifest generation + gitops-push)
//
// The build pipeline no longer generates manifests or pushes to GitOps. It only
// builds images and exits. The API detects completion, generates manifests from
// the app spec + image info, and handles the entire release flow.
//
// MOD-1 fix: Uses typed Go structs + sigs.k8s.io/yaml for serialization instead of
// fmt.Sprintf string concatenation. This prevents YAML corruption from special chars.
//
// MOD-5 fix: sigs.k8s.io/yaml marshals maps with sorted keys, producing deterministic
// output that avoids unnecessary GitOps diffs.
//
// LEVE-2 fix: IngressRoute TLS uses empty struct (tls: {}) to reference the default
// TLSStore instead of certResolver: letsencrypt.
package manifests

import (
	"os"
	"strings"
)

// --- Public API ---

// CustomDomainInput represents a verified custom domain to include in manifests.
// MEET-2026-04-05-033: Custom Domains feature.
type CustomDomainInput struct {
	Domain    string // FQDN (e.g., "api.mycompany.com")
	ID        string // Domain row ID for traceability
	CFManaged bool   // v2: true if domain uses CF Zone (TLS at CF edge, no cert-manager Certificate needed)
}

// AutoscalingConfig defines horizontal pod autoscaler settings.
// When present on an app, an HPA resource is generated alongside the Deployment.
// When MinReplicas is 0, KEDA ScaledObject is generated instead of HPA for scale-to-zero.
type AutoscalingConfig struct {
	MinReplicas    int // Minimum replicas (default: 1, 0 = scale-to-zero via KEDA)
	MaxReplicas    int // Maximum replicas (default: 3)
	TargetCPU      int // Target CPU utilization percentage (default: 80)
	CooldownPeriod int // Seconds before scale-to-zero (default: 300, only when MinReplicas = 0)
}

// AppManifestInput contains everything needed to generate manifests for a single app.
type AppManifestInput struct {
	Name      string            // e.g., "api", "worker", "dashboard"
	Type      string            // "server", "worker", "frontend"
	Rendering string            // "static" or "server" (MEET-009). Static → S3 proxy, no pod.
	Port      int               // Container port (server/frontend only)
	ImageRef  string            // Full image reference (container apps only)
	S3Prefix  string            // S3 path prefix for static assets (static apps only)
	Secrets   []string          // Secret names from loopzero.yaml (injected via envFrom)
	Env       map[string]string // Environment variables
	Replicas  int               // Desired replica count (default: 1 when 0)

	// Autoscaling configures HPA generation. When non-nil, an HPA is rendered
	// alongside the Deployment and the Deployment replicas are set to MinReplicas.
	Autoscaling *AutoscalingConfig

	// CustomDomains are active custom domains for this app (MEET-2026-04-05-033).
	// Each generates an additional IngressRoute + Certificate alongside the auto-generated one.
	CustomDomains []CustomDomainInput
}

// RenderInput contains everything needed to generate manifests for a full deployment.
type RenderInput struct {
	TenantID     string
	ProjectID    string
	Environment  string
	BuildID      string
	ReleaseID    string
	BaseDomain   string
	S3Endpoint   string // DO Spaces endpoint for static proxy (e.g., "sfo3.digitaloceanspaces.com")
	S3BucketName string // Static assets bucket (e.g., "loopzero-static-assets")
	Apps         []AppManifestInput

	// Configurable defaults (arch-cp-006). Zero values fall back to built-in defaults.
	// B14 (MEET-2026-04-23-058): ClusterIssuerName is REQUIRED — no fallback.
	// Set from Config.ClusterIssuer (env LOOPZERO_CLUSTER_ISSUER). Empty = panic.
	ClusterIssuerName    string // Required — no default; panics if empty at render time
	GatewayNamespace     string // Default: "loopzero-system"
	DefaultCPURequest    string // Default: "100m"
	DefaultMemoryRequest string // Default: "128Mi"
	DefaultCPULimit      string // Default: "500m"
	DefaultMemoryLimit   string // Default: "512Mi"
}

// Render generates Kubernetes manifests (Deployment, Service, IngressRoute) for all apps.
// Returns the combined YAML as a single string with documents separated by "---".
//
// B14 (MEET-2026-04-23-058): ClusterIssuerName is required. Callers MUST populate
// it from Config.ClusterIssuer (env LOOPZERO_CLUSTER_ISSUER). Panics on empty because
// this is a programmer error (missing wiring), not a runtime condition.
func Render(input RenderInput) string {
	if input.ClusterIssuerName == "" {
		panic("manifests.Render: ClusterIssuerName is required (B14 — wire from Config.ClusterIssuer)")
	}
	var docs []string

	for _, app := range input.Apps {
		// Static frontends get S3 proxy instead of Deployment+Service (MEET-009)
		if app.Type == "frontend" && app.Rendering == "static" {
			docs = append(docs, renderStaticFrontendApp(input, app)...)
			continue
		}
		switch app.Type {
		case "server":
			docs = append(docs, renderServerApp(input, app)...)
		case "worker":
			docs = append(docs, renderWorkerApp(input, app)...)
		case "frontend":
			docs = append(docs, renderFrontendApp(input, app)...)
		default:
			docs = append(docs, renderServerApp(input, app)...)
		}
	}

	return strings.Join(docs, "\n---\n")
}

// --- Default resolution helpers (arch-cp-006) ---

func (ri RenderInput) clusterIssuerName() string {
	// B14 (MEET-2026-04-23-058): no fallback. Render() already panicked if empty.
	return ri.ClusterIssuerName
}

func (ri RenderInput) gatewayNamespace() string {
	if ri.GatewayNamespace != "" {
		return ri.GatewayNamespace
	}
	return "loopzero-system"
}

// leanResourcesEnabled reports whether the LOOPZERO_LEAN_RESOURCES env var
// is set (case-insensitive "true" or "1"). When lean mode is on, the
// fallback defaults are halved to fit 100 apps in the current DO dev
// runtime cluster while a droplet quota increase is in flight.
func leanResourcesEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("LOOPZERO_LEAN_RESOURCES")))
	return v == "true" || v == "1"
}

func (ri RenderInput) cpuRequest() string {
	if ri.DefaultCPURequest != "" {
		return ri.DefaultCPURequest
	}
	if leanResourcesEnabled() {
		return "50m"
	}
	return "100m"
}

func (ri RenderInput) memoryRequest() string {
	if ri.DefaultMemoryRequest != "" {
		return ri.DefaultMemoryRequest
	}
	if leanResourcesEnabled() {
		return "64Mi"
	}
	return "128Mi"
}

func (ri RenderInput) cpuLimit() string {
	if ri.DefaultCPULimit != "" {
		return ri.DefaultCPULimit
	}
	if leanResourcesEnabled() {
		return "250m"
	}
	return "500m"
}

func (ri RenderInput) memoryLimit() string {
	if ri.DefaultMemoryLimit != "" {
		return ri.DefaultMemoryLimit
	}
	if leanResourcesEnabled() {
		return "256Mi"
	}
	return "512Mi"
}
