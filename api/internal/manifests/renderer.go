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
	"fmt"
	"os"
	"sort"
	"strings"

	sigsyaml "sigs.k8s.io/yaml"
)

// --- Public API (unchanged) ---

// CustomDomainInput represents a verified custom domain to include in manifests.
// MEET-2026-04-05-033: Custom Domains feature.
type CustomDomainInput struct {
	Domain    string // FQDN (e.g., "api.mycompany.com")
	ID        string // Domain row ID for traceability
	CFManaged bool   // v2: true if domain uses CF Zone (TLS at CF edge, no cert-manager Certificate needed)
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

	// CustomDomains are active custom domains for this app (MEET-2026-04-05-033).
	// Each generates an additional IngressRoute + Certificate alongside the auto-generated one.
	CustomDomains []CustomDomainInput
}

// RenderInput contains everything needed to generate manifests for a full deployment.
type RenderInput struct {
	TenantID       string
	ProjectID      string
	Environment    string
	BuildID        string
	ReleaseID      string
	BaseDomain     string
	S3Endpoint     string // DO Spaces endpoint for static proxy (e.g., "sfo3.digitaloceanspaces.com")
	S3BucketName   string // Static assets bucket (e.g., "loopzero-static-assets")
	Apps           []AppManifestInput

	// Configurable defaults (arch-cp-006). Zero values fall back to built-in defaults.

	// ClusterIssuerName is the cert-manager ClusterIssuer name for TLS certificates.
	// Default: "letsencrypt-prod".
	ClusterIssuerName string

	// GatewayNamespace is the namespace where the shared static-assets gateway service lives.
	// Default: "platform-system".
	GatewayNamespace string

	// DefaultCPURequest is the default CPU request for containers (e.g., "100m").
	// Default: "100m".
	DefaultCPURequest string

	// DefaultMemoryRequest is the default memory request for containers (e.g., "128Mi").
	// Default: "128Mi".
	DefaultMemoryRequest string

	// DefaultCPULimit is the default CPU limit for containers (e.g., "500m").
	// Default: "500m".
	DefaultCPULimit string

	// DefaultMemoryLimit is the default memory limit for containers (e.g., "512Mi").
	// Default: "512Mi".
	DefaultMemoryLimit string
}

// Render generates Kubernetes manifests (Deployment, Service, IngressRoute) for all apps.
// Returns the combined YAML as a single string with documents separated by "---".
func Render(input RenderInput) string {
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

// --- K8s resource struct types ---
// Local definitions to avoid heavy k8s/api imports.
// Only fields we actually set are included.

type k8sMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Deployment types

type k8sDeployment struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   k8sMetadata       `json:"metadata"`
	Spec       k8sDeploymentSpec `json:"spec"`
}

type k8sDeploymentSpec struct {
	Replicas                int                 `json:"replicas"`
	ProgressDeadlineSeconds int                 `json:"progressDeadlineSeconds,omitempty"`
	MinReadySeconds         int                 `json:"minReadySeconds,omitempty"`
	Strategy                *k8sDeployStrategy  `json:"strategy,omitempty"`
	Selector                k8sLabelSelector    `json:"selector"`
	Template                k8sPodTemplateSpec  `json:"template"`
}

type k8sDeployStrategy struct {
	Type          string             `json:"type"`
	RollingUpdate *k8sRollingUpdate  `json:"rollingUpdate,omitempty"`
}

type k8sRollingUpdate struct {
	MaxUnavailable int `json:"maxUnavailable"`
	MaxSurge       int `json:"maxSurge"`
}

type k8sLabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

type k8sPodTemplateSpec struct {
	Metadata k8sPodMeta `json:"metadata"`
	Spec     k8sPodSpec `json:"spec"`
}

type k8sPodMeta struct {
	Labels map[string]string `json:"labels"`
}

type k8sPodSpec struct {
	ServiceAccountName            string                  `json:"serviceAccountName,omitempty"`
	AutomountServiceAccountToken  *bool                   `json:"automountServiceAccountToken,omitempty"`
	SecurityContext               *k8sPodSecurityContext   `json:"securityContext,omitempty"`
	Containers                    []k8sContainer           `json:"containers"`
	TerminationGracePeriodSeconds int                      `json:"terminationGracePeriodSeconds,omitempty"`
}

type k8sPodSecurityContext struct {
	RunAsNonRoot   *bool              `json:"runAsNonRoot,omitempty"`
	RunAsUser      *int64             `json:"runAsUser,omitempty"`
	RunAsGroup     *int64             `json:"runAsGroup,omitempty"`
	FSGroup        *int64             `json:"fsGroup,omitempty"`
	SeccompProfile *k8sSeccompProfile `json:"seccompProfile,omitempty"`
}

type k8sSeccompProfile struct {
	Type string `json:"type"`
}

type k8sContainer struct {
	Name            string                      `json:"name"`
	Image           string                      `json:"image"`
	Ports           []k8sContainerPort          `json:"ports,omitempty"`
	Resources       k8sResources                `json:"resources"`
	Env             []k8sEnvVar                 `json:"env,omitempty"`
	EnvFrom         []k8sEnvFromSource          `json:"envFrom,omitempty"`
	SecurityContext *k8sContainerSecurityContext `json:"securityContext,omitempty"`
	StartupProbe    *k8sProbe                   `json:"startupProbe,omitempty"`
	ReadinessProbe  *k8sProbe                   `json:"readinessProbe,omitempty"`
	LivenessProbe   *k8sProbe                   `json:"livenessProbe,omitempty"`
}

type k8sContainerSecurityContext struct {
	AllowPrivilegeEscalation *bool              `json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool              `json:"readOnlyRootFilesystem,omitempty"`
	Capabilities             *k8sCapabilities   `json:"capabilities,omitempty"`
}

type k8sCapabilities struct {
	Drop []string `json:"drop,omitempty"`
}

type k8sContainerPort struct {
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type k8sResources struct {
	Requests k8sResourceList `json:"requests"`
	Limits   k8sResourceList `json:"limits"`
}

type k8sResourceList struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type k8sEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type k8sEnvFromSource struct {
	SecretRef k8sSecretRef `json:"secretRef"`
}

type k8sSecretRef struct {
	Name string `json:"name"`
}

type k8sProbe struct {
	HTTPGet             k8sHTTPGet `json:"httpGet"`
	InitialDelaySeconds int        `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int        `json:"periodSeconds"`
	TimeoutSeconds      int        `json:"timeoutSeconds,omitempty"`
	SuccessThreshold    int        `json:"successThreshold,omitempty"`
	FailureThreshold    int        `json:"failureThreshold,omitempty"`
}

type k8sHTTPGet struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// PodDisruptionBudget types

type k8sPDB struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   k8sMetadata `json:"metadata"`
	Spec       k8sPDBSpec  `json:"spec"`
}

type k8sPDBSpec struct {
	MaxUnavailable             int              `json:"maxUnavailable"`
	UnhealthyPodEvictionPolicy string           `json:"unhealthyPodEvictionPolicy,omitempty"`
	Selector                   k8sLabelSelector `json:"selector"`
}

// Service types

type k8sService struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   k8sMetadata    `json:"metadata"`
	Spec       k8sServiceSpec `json:"spec"`
}

type k8sServiceSpec struct {
	Selector map[string]string `json:"selector"`
	Ports    []k8sServicePort  `json:"ports"`
}

type k8sServicePort struct {
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
	Protocol   string `json:"protocol"`
}

// Traefik IngressRoute types (CRD)

type traefikIngressRoute struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   k8sMetadata            `json:"metadata"`
	Spec       traefikIngressRouteSpec `json:"spec"`
}

type traefikIngressRouteSpec struct {
	EntryPoints []string       `json:"entryPoints"`
	Routes      []traefikRoute `json:"routes"`
	TLS         *traefikTLS    `json:"tls,omitempty"`
}

type traefikRoute struct {
	Match       string                   `json:"match"`
	Kind        string                   `json:"kind"`
	Middlewares []traefikRouteMiddleware  `json:"middlewares,omitempty"`
	Services    []traefikRouteService    `json:"services"`
}

type traefikRouteService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // cross-namespace reference
	Port      int    `json:"port"`
	Scheme    string `json:"scheme,omitempty"`
}

// traefikTLS configures TLS on IngressRoute.
// Uses secretName to reference the cert-manager issued certificate.
type traefikTLS struct {
	SecretName string `json:"secretName"`
}

// certManagerCertificate represents a cert-manager Certificate CR.
type certManagerCertificate struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Metadata   k8sMetadata                `json:"metadata"`
	Spec       certManagerCertificateSpec `json:"spec"`
}

type certManagerCertificateSpec struct {
	SecretName string           `json:"secretName"`
	IssuerRef  certIssuerRef    `json:"issuerRef"`
	DNSNames   []string         `json:"dnsNames"`
}

type certIssuerRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}


// --- Default resolution helpers (arch-cp-006) ---

func (ri RenderInput) clusterIssuerName() string {
	if ri.ClusterIssuerName != "" {
		return ri.ClusterIssuerName
	}
	return "letsencrypt-prod"
}

func (ri RenderInput) gatewayNamespace() string {
	if ri.GatewayNamespace != "" {
		return ri.GatewayNamespace
	}
	return "platform-system"
}

// leanResourcesEnabled reports whether the LOOPZERO_LEAN_RESOURCES env var
// is set (case-insensitive "true" or "1"). When lean mode is on, the
// fallback defaults are halved to fit 100 apps in the current DO dev
// runtime cluster while a droplet quota increase is in flight.
//
// Plan: docs/plans/50k-immediate-focus.md (T-08 contingency, Option A).
//
// This flag affects ONLY the fallback path. If a caller sets
// RenderInput.DefaultCPURequest/etc. explicitly (e.g. from config or
// loopzero.yaml), those win — lean mode never overrides an explicit value.
// Read live on every call so ops can toggle without restarting the API.
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

// --- Internal render functions ---

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

// renderStaticFrontendApp generates IngressRoute + Middleware + Certificate for a
// static frontend served from S3 via nginx-s3-gateway proxy.
// NO Deployment, NO Service (uses shared static-assets Service in platform-system).
//
// Architecture (MEET-2026-03-26-011):
//   Browser → Traefik IngressRoute → Middleware (addPrefix) → static-assets Service → nginx → DO Spaces
//
// Same hostname as container apps — zero URL divergence.
// The Middleware adds the S3 path prefix so nginx proxies to the correct build's assets.
// SPA routing handled by nginx config (404 → index.html fallback).
func renderStaticFrontendApp(ri RenderInput, app AppManifestInput) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	// Same hostname format as container apps — NO static. prefix (DX VETO from MEET-011)
	hostname := appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)
	tlsSecretName := fmt.Sprintf("tls-%s", app.Name)

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
	// Request: /styles.css → /{tenant}/{project}/{app}/{build-id}/styles.css
	rewriteMiddleware := traefikMiddleware{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "Middleware",
		Metadata: k8sMetadata{
			Name:      fmt.Sprintf("static-prefix-%s", app.Name),
			Namespace: ns,
			Labels:    labels,
		},
		Spec: traefikMiddlewareSpec{
			AddPrefix: &traefikAddPrefix{
				Prefix: s3PathPrefix,
			},
		},
	}

	// IngressRoute: routes to shared nginx-s3-gateway in platform-system namespace.
	// Cross-namespace reference requires allowCrossNamespace: true in Traefik (already enabled).
	ingress := traefikIngressRoute{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "IngressRoute",
		Metadata: k8sMetadata{
			Name:      app.Name,
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
							Name:      fmt.Sprintf("static-prefix-%s", app.Name),
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

// --- Traefik Middleware types (for static frontend) ---

type traefikMiddleware struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   k8sMetadata            `json:"metadata"`
	Spec       traefikMiddlewareSpec   `json:"spec"`
}

type traefikMiddlewareSpec struct {
	AddPrefix     *traefikAddPrefix     `json:"addPrefix,omitempty"`
	RedirectRegex *traefikRedirectRegex `json:"redirectRegex,omitempty"`
}

type traefikRedirectRegex struct {
	Regex       string `json:"regex"`
	Replacement string `json:"replacement"`
	Permanent   bool   `json:"permanent"`
}

type traefikAddPrefix struct {
	Prefix string `json:"prefix"`
}

type traefikRouteMiddleware struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // cross-namespace reference
}

// --- Struct builders ---

func buildDeployment(ri RenderInput, app AppManifestInput, port int) k8sDeployment {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	selectorLabels := map[string]string{
		"app":              app.Name,
		"loopzero.io/tenant":   ri.TenantID,
		"loopzero.io/project":  ri.ProjectID,
	}

	falseVal := false
	trueVal := true

	container := k8sContainer{
		Name:  app.Name,
		Image: app.ImageRef,
		Resources: k8sResources{
			Requests: k8sResourceList{CPU: ri.cpuRequest(), Memory: ri.memoryRequest()},
			Limits:   k8sResourceList{CPU: ri.cpuLimit(), Memory: ri.memoryLimit()},
		},
		SecurityContext: &k8sContainerSecurityContext{
			AllowPrivilegeEscalation: &falseVal,
			ReadOnlyRootFilesystem:   &trueVal,
			Capabilities:             &k8sCapabilities{Drop: []string{"ALL"}},
		},
	}

	if port > 0 {
		container.Ports = []k8sContainerPort{
			{ContainerPort: port, Protocol: "TCP"},
		}
		// Inject PORT env var so the app knows which port to listen on.
		// Many frameworks use PORT env var (Express, Fastify, Next.js, etc.).
		container.Env = append(container.Env, k8sEnvVar{
			Name: "PORT", Value: fmt.Sprintf("%d", port),
		})
		container.StartupProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 30, // 30 * 5s = 150s max startup time
		}
		container.ReadinessProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    10,
			TimeoutSeconds:   3,
			SuccessThreshold: 1,
			FailureThreshold: 3, // 3 * 10s = 30s before removing from Service
		}
		container.LivenessProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    15,
			TimeoutSeconds:   3,
			SuccessThreshold: 1,
			FailureThreshold: 5, // 5 * 15s = 75s — more tolerant than readiness
		}
	}

	// Environment variables — sorted by key for determinism
	if len(app.Env) > 0 {
		keys := make([]string, 0, len(app.Env))
		for k := range app.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			container.Env = append(container.Env, k8sEnvVar{Name: k, Value: app.Env[k]})
		}
	}

	// Secret references via envFrom
	if len(app.Secrets) > 0 {
		secretRefName := fmt.Sprintf("user-secrets-%s-%s-%s-%s",
			ri.TenantID, ri.ProjectID, ri.Environment, app.Name)
		container.EnvFrom = []k8sEnvFromSource{
			{SecretRef: k8sSecretRef{Name: secretRefName}},
		}
	}

	return k8sDeployment{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata: k8sMetadata{
			Name:      app.Name,
			Namespace: ns,
			Labels:    labels,
			Annotations: map[string]string{
				"loopzero.io/build-id":   ri.BuildID,
				"loopzero.io/release-id": ri.ReleaseID,
			},
		},
		Spec: k8sDeploymentSpec{
			Replicas:                1,
			ProgressDeadlineSeconds: 210,
			MinReadySeconds:         10,
			Strategy: &k8sDeployStrategy{
				Type: "RollingUpdate",
				RollingUpdate: &k8sRollingUpdate{
					MaxUnavailable: 0,
					MaxSurge:       1,
				},
			},
			Selector: k8sLabelSelector{MatchLabels: selectorLabels},
			Template: k8sPodTemplateSpec{
				Metadata: k8sPodMeta{Labels: labels},
				Spec: k8sPodSpec{
					ServiceAccountName:           "loopzero-app",
					AutomountServiceAccountToken: &falseVal,
					SecurityContext: &k8sPodSecurityContext{
						RunAsNonRoot:   &trueVal,
						RunAsUser:      int64Ptr(1000),
						RunAsGroup:     int64Ptr(1000),
						FSGroup:        int64Ptr(1000),
						SeccompProfile: &k8sSeccompProfile{Type: "RuntimeDefault"},
					},
					Containers:                    []k8sContainer{container},
					TerminationGracePeriodSeconds: 30,
				},
			},
		},
	}
}

func buildPDB(ri RenderInput, app AppManifestInput) k8sPDB {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	selectorLabels := map[string]string{
		"app":             app.Name,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}

	return k8sPDB{
		APIVersion: "policy/v1",
		Kind:       "PodDisruptionBudget",
		Metadata: k8sMetadata{
			Name:      app.Name + "-pdb",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sPDBSpec{
			MaxUnavailable:             1,
			UnhealthyPodEvictionPolicy: "AlwaysAllow",
			Selector:                   k8sLabelSelector{MatchLabels: selectorLabels},
		},
	}
}

func buildService(ri RenderInput, app AppManifestInput, port int) k8sService {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)

	return k8sService{
		APIVersion: "v1",
		Kind:       "Service",
		Metadata: k8sMetadata{
			Name:      app.Name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sServiceSpec{
			Selector: map[string]string{"app": app.Name},
			Ports: []k8sServicePort{
				{Port: port, TargetPort: port, Protocol: "TCP"},
			},
		},
	}
}

// buildIngressResources generates a Certificate CR and IngressRoute for TLS.
// cert-manager issues a Let's Encrypt certificate via the ClusterIssuer.
// The IngressRoute references the certificate secret for TLS termination.
// Hostname format: {app}-{env}.{project}.{tenant}.{baseDomain}
func buildIngressResources(ri RenderInput, app AppManifestInput, port int) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	hostname := appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)
	tlsSecretName := fmt.Sprintf("tls-%s", app.Name)

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

	ingress := traefikIngressRoute{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "IngressRoute",
		Metadata: k8sMetadata{
			Name:      app.Name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: traefikIngressRouteSpec{
			EntryPoints: []string{"web", "websecure"},
			Routes: []traefikRoute{
				{
					Match:    fmt.Sprintf("Host(`%s`)", hostname),
					Kind:     "Rule",
					Services: []traefikRouteService{{Name: app.Name, Port: port}},
				},
			},
			TLS: &traefikTLS{SecretName: tlsSecretName},
		},
	}

	docs := []string{marshalResource(cert), marshalResource(ingress)}

	// Custom domains: each gets its own IngressRoute + Certificate (MEET-2026-04-05-033).
	// Resource names are domain-derived (not index-based) to prevent churn on add/remove.
	for _, cd := range app.CustomDomains {
		suffix, cdTLSName := domainResourceNames(cd.Domain)

		// v2 (CF-managed): CF handles TLS at the edge — no cert-manager Certificate needed.
		// Only generate IngressRoute for Traefik routing (CF Tunnel → Traefik → Pod).
		// v1 (non-CF): Generate both Certificate + IngressRoute (cert-manager handles TLS).
		if !cd.CFManaged {
			cdCert := certManagerCertificate{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
				Metadata: k8sMetadata{
					Name:      cdTLSName,
					Namespace: ns,
					Labels:    labels,
					Annotations: map[string]string{
						"loopzero.io/custom-domain": cd.Domain,
					},
				},
				Spec: certManagerCertificateSpec{
					SecretName: cdTLSName,
					IssuerRef: certIssuerRef{
						Name: ri.clusterIssuerName(),
						Kind: "ClusterIssuer",
					},
					DNSNames: []string{cd.Domain},
				},
			}
			docs = append(docs, marshalResource(cdCert))
		}

		// IngressRoute always needed — Traefik must match Host() for routing
		cdIngress := traefikIngressRoute{
			APIVersion: "traefik.io/v1alpha1",
			Kind:       "IngressRoute",
			Metadata: k8sMetadata{
				Name:      suffix,
				Namespace: ns,
				Labels:    labels,
				Annotations: map[string]string{
					"loopzero.io/custom-domain": cd.Domain,
				},
			},
			Spec: traefikIngressRouteSpec{
				EntryPoints: []string{"web", "websecure"},
				Routes: []traefikRoute{
					{
						Match:    fmt.Sprintf("Host(`%s`)", cd.Domain),
						Kind:     "Rule",
						Services: []traefikRouteService{{Name: app.Name, Port: port}},
					},
				},
			},
		}

		// v1: TLS terminates at Traefik using cert-manager secret
		// v2: TLS terminates at CF edge — Traefik sees plain HTTP from tunnel
		if !cd.CFManaged {
			cdIngress.Spec.TLS = &traefikTLS{SecretName: cdTLSName}
		}

		docs = append(docs, marshalResource(cdIngress))
	}

	return docs
}

// domainResourceNames returns stable K8s resource names for a custom domain's
// IngressRoute and Certificate. Both names are guaranteed ≤ 63 characters.
// For short domains: "cd-api-mycompany-com" / "tls-cd-api-mycompany-com"
// For long domains: hash-based suffix prevents collisions after truncation.
// MEET-2026-04-05-033: Domain-derived naming prevents resource churn.
// CODE-005 fix: tls- prefix is accounted for in the length budget.
func domainResourceNames(domain string) (ingressName, certName string) {
	s := strings.ToLower(domain)
	s = strings.ReplaceAll(s, ".", "-")
	base := "cd-" + s

	// Budget: certName = "tls-" + ingressName, so ingressName max = 59 chars
	const maxIngressLen = 59

	if len(base) <= maxIngressLen {
		ingressName = base
	} else {
		// Truncate + append 8-char hash of full domain to prevent collisions
		h := fmt.Sprintf("%x", hashDomain(domain))[:8]
		ingressName = base[:maxIngressLen-9] + "-" + h // 50 + 1 + 8 = 59
	}
	ingressName = strings.TrimRight(ingressName, "-")

	certName = "tls-" + ingressName
	return ingressName, certName
}

// hashDomain returns a deterministic hash of a domain string for collision avoidance.
func hashDomain(domain string) uint64 {
	// FNV-1a 64-bit: fast, deterministic, no crypto needed (not a security hash).
	var h uint64 = 14695981039346656037
	for i := 0; i < len(domain); i++ {
		h ^= uint64(domain[i])
		h *= 1099511628211
	}
	return h
}

// --- Helpers ---

// appHostname returns the flat hostname for an app.
// Format: {tenant}-{project}-{app}-{env}.{baseDomain}
// This produces a single-level subdomain under baseDomain, which is compatible
// with wildcard TLS certificates (*.dev.loopzero.example covers all apps).
//
// Previous format ({app}-{env}.{project}.{tenant}.{baseDomain}) created multi-level
// subdomains that no CA can issue wildcard certificates for.
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
