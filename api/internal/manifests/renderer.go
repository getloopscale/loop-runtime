// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
	"fmt"
	"strings"
)

// K8sDoc is a single Kubernetes manifest document with metadata extracted
// from the YAML header. Plan v1.2 T4.1 — typed output so downstream
// consumers (kubeconform, drift detection, dispatch by Kind) can act on
// structured fields instead of re-parsing the joined string.
type K8sDoc struct {
	Kind      string
	Name      string
	Namespace string
	YAML      string
}

// kindFromYAML is a best-effort one-pass scanner that extracts `kind: X`
// from the first 8 lines of a manifest body. K8sDoc carries the result so
// downstream filters can skip the doc-parse round trip.
func kindFromYAML(doc string) string {
	return scanField(doc, "kind:")
}

func nameFromYAML(doc string) string {
	return scanField(doc, "  name:")
}

func namespaceFromYAML(doc string) string {
	return scanField(doc, "  namespace:")
}

func scanField(doc, prefix string) string {
	// Scan up to the first 16 lines (covers apiVersion/kind/metadata block).
	limit := 16
	rest := doc
	for i := 0; i < limit && rest != ""; i++ {
		idx := strings.IndexByte(rest, '\n')
		var line string
		if idx < 0 {
			line = rest
			rest = ""
		} else {
			line = rest[:idx]
			rest = rest[idx+1:]
		}
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

// Concat joins K8sDoc.YAML payloads into a single multi-document YAML
// string. Plan v1.2 T4.1 + EC-10 (v1.1): each doc's trailing newline is
// trimmed before joining so the separator does not produce "\n\n---\n".
// Plan v1.2 T4.3 (O2): uses strings.Builder with a size hint to avoid the
// quadratic copy cost of repeated strings.Join calls.
func Concat(docs []K8sDoc) string {
	if len(docs) == 0 {
		return ""
	}
	estimate := 0
	for i := range docs {
		estimate += len(docs[i].YAML) + len("\n---\n")
	}
	var b strings.Builder
	b.Grow(estimate)
	for i := range docs {
		body := strings.TrimRight(docs[i].YAML, "\n")
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(body)
	}
	return b.String()
}

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
	TargetMemory   int // Target memory utilization percentage (0: the HPA scales on CPU alone)
	CooldownPeriod int // Seconds before scale-to-zero (default: 300, only when MinReplicas = 0)
}

// Service type values mirror services.schema.json (server | worker). An empty
// or unknown type renders as a server.
const (
	ServiceTypeServer = "server"
	ServiceTypeWorker = "worker"
)

// AppManifestInput contains everything needed to generate manifests for a single app.
type AppManifestInput struct {
	Name     string            // e.g., "api", "worker", "dashboard"
	Type     string            // "server", "worker", "frontend"
	Port     int               // Container port (server/frontend only)
	ImageRef string            // Full image reference
	Secrets  []string          // Secret names from loopzero.yaml (injected via envFrom)
	Env      map[string]string // Environment variables
	Replicas int               // Desired replica count (default: 1 when 0)

	// Autoscaling configures HPA generation. When non-nil, an HPA is rendered
	// alongside the Deployment and the Deployment replicas are set to MinReplicas.
	Autoscaling *AutoscalingConfig

	// CustomDomains are active custom domains for this app (MEET-2026-04-05-033).
	// Each generates an additional IngressRoute + Certificate alongside the auto-generated one.
	CustomDomains []CustomDomainInput
}

// RenderInput contains everything needed to generate manifests for a full deployment.
type RenderInput struct {
	TenantID    string
	ProjectID   string
	Environment string
	BuildID     string
	ReleaseID   string
	BaseDomain  string
	Apps        []AppManifestInput

	// Configurable defaults (arch-cp-006). Zero values fall back to built-in defaults.
	// B14 (MEET-2026-04-23-058): ClusterIssuerName is REQUIRED — no fallback.
	// Set from Config.ClusterIssuer (env LOOPRUNTIME_CLUSTER_ISSUER). Empty = RenderDocs error.
	ClusterIssuerName    string // Required — no default; RenderDocs returns an error when empty
	GatewayNamespace     string // Default: "loopruntime-system"
	DefaultCPURequest    string // Default: "100m"
	DefaultMemoryRequest string // Default: "128Mi"
	DefaultCPULimit      string // Default: "500m"
	DefaultMemoryLimit   string // Default: "512Mi"

	// argo-rollouts-progressive-delivery-plan T1.2 — Progressive delivery fields.
	//
	// Plan is the tenant subscription plan ("starter", "pro", "team",
	// "enterprise") used to select canary strategy steps (D2). Empty
	// defaults to "starter" (most conservative).
	Plan string

	// PrometheusAddress is the Prometheus URL the AnalysisTemplate queries
	// (D6). Must be reachable from the workload cluster — for k3d local
	// dev this is typically "http://172.18.0.X:30090" (mgmt cluster
	// NodePort). Empty disables Prometheus-driven canary analysis (steps
	// without analysis still run pause/setWeight).
	PrometheusAddress string

	// ProgressiveDelivery controls whether to emit Rollout (canary) or
	// legacy Deployment manifests (D8). Callers fill it from
	// config.Config.ProgressiveDelivery (env LOOPRUNTIME_PROGRESSIVE_DELIVERY,
	// default on), so an operator can revert to legacy Deployment during an
	// incident rollback.
	ProgressiveDelivery bool

	// LeanResources halves the fallback resource defaults (the Default*
	// fields above still win when set) to fit 100 apps in the current DO dev
	// runtime cluster while a droplet quota increase is in flight. Callers
	// fill it from config.Config.LeanResources (env LOOPRUNTIME_LEAN_RESOURCES).
	LeanResources bool
}

// RenderDocs generates Kubernetes manifests (Deployment, Service,
// IngressRoute, Rollout, …) for every app in the input as typed K8sDoc
// values. Plan v1.2 T4.1 — new authoritative API. Existing callers may
// keep using Render() (which delegates here + Concat).
//
// B14 (MEET-2026-04-23-058): ClusterIssuerName is required. Callers MUST
// populate it from Config.ClusterIssuer (env LOOPRUNTIME_CLUSTER_ISSUER).
func RenderDocs(input RenderInput) ([]K8sDoc, error) {
	if input.ClusterIssuerName == "" {
		return nil, fmt.Errorf("manifests.Render: ClusterIssuerName is required (B14 — wire from Config.ClusterIssuer)")
	}
	scope, err := newRenderScope(input)
	if err != nil {
		return nil, err
	}
	var raw []string
	for _, app := range input.Apps {
		// T3.3 (plan simplify-deploy-loopzerokit-only v2.0): frontend/static delivery
		// removed — the fixed stack is server|worker only (D5). Unknown/empty
		// types default to server.
		render := renderServerApp
		if app.Type == ServiceTypeWorker {
			render = renderWorkerApp
		}
		docs, err := render(scope, app)
		if err != nil {
			return nil, fmt.Errorf("manifests: app %q: %w", app.Name, err)
		}
		raw = append(raw, docs...)
	}
	docs := make([]K8sDoc, 0, len(raw))
	for _, body := range raw {
		docs = append(docs, K8sDoc{
			Kind:      kindFromYAML(body),
			Name:      nameFromYAML(body),
			Namespace: namespaceFromYAML(body),
			YAML:      body,
		})
	}
	return docs, nil
}

// Render generates Kubernetes manifests (Deployment, Service, IngressRoute)
// for all apps. Returns the combined YAML as a single string with
// documents separated by "---". Plan v1.2 T4.1: thin wrapper over
// RenderDocs + Concat. Plan v1.2 T3.1: returns error instead of panicking.
func Render(input RenderInput) (string, error) {
	docs, err := RenderDocs(input)
	if err != nil {
		return "", err
	}
	return Concat(docs), nil
}

// --- Default resolution helpers (arch-cp-006) ---

func (ri RenderInput) clusterIssuerName() string {
	// B14 (MEET-2026-04-23-058): no fallback. RenderDocs already refused an empty value.
	return ri.ClusterIssuerName
}

func (ri RenderInput) cpuRequest() string {
	if ri.DefaultCPURequest != "" {
		return ri.DefaultCPURequest
	}
	if ri.LeanResources {
		return "50m"
	}
	return "100m"
}

func (ri RenderInput) memoryRequest() string {
	if ri.DefaultMemoryRequest != "" {
		return ri.DefaultMemoryRequest
	}
	if ri.LeanResources {
		return "64Mi"
	}
	return "128Mi"
}

func (ri RenderInput) cpuLimit() string {
	if ri.DefaultCPULimit != "" {
		return ri.DefaultCPULimit
	}
	if ri.LeanResources {
		return "250m"
	}
	return "500m"
}

func (ri RenderInput) memoryLimit() string {
	if ri.DefaultMemoryLimit != "" {
		return ri.DefaultMemoryLimit
	}
	if ri.LeanResources {
		return "256Mi"
	}
	return "512Mi"
}
