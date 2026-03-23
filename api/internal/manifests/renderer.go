// Package manifests generates Kubernetes manifests for tenant applications.
//
// C-02 fix: Manifest generation moved from the build pipeline (Argo Workflow CWT)
// to the API (control plane). This is the correct separation of concerns for a PaaS:
//   - Build plane: produces container images (Kaniko)
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
	"sort"
	"strings"

	sigsyaml "sigs.k8s.io/yaml"
)

// --- Public API (unchanged) ---

// AppManifestInput contains everything needed to generate manifests for a single app.
type AppManifestInput struct {
	Name     string            // e.g., "api", "worker", "dashboard"
	Type     string            // "server", "worker", "frontend"
	Port     int               // Container port (server/frontend only)
	ImageRef string            // Full image reference, e.g., "registry:5000/tenant/project/api:abc123"
	Secrets  []string          // Secret names from loopzero.yaml (injected via envFrom)
	Env      map[string]string // Environment variables
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
}

// Render generates Kubernetes manifests (Deployment, Service, IngressRoute) for all apps.
// Returns the combined YAML as a single string with documents separated by "---".
func Render(input RenderInput) string {
	var docs []string

	for _, app := range input.Apps {
		switch app.Type {
		case "server":
			docs = append(docs, renderServerApp(input, app)...)
		case "worker":
			docs = append(docs, renderWorkerApp(input, app)...)
		case "frontend":
			docs = append(docs, renderFrontendApp(input, app)...)
		default:
			// Default to server behavior
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
	Replicas int                `json:"replicas"`
	Selector k8sLabelSelector   `json:"selector"`
	Template k8sPodTemplateSpec `json:"template"`
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
	Containers []k8sContainer `json:"containers"`
}

type k8sContainer struct {
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	Ports          []k8sContainerPort `json:"ports,omitempty"`
	Resources      k8sResources       `json:"resources"`
	Env            []k8sEnvVar        `json:"env,omitempty"`
	EnvFrom        []k8sEnvFromSource `json:"envFrom,omitempty"`
	ReadinessProbe *k8sProbe          `json:"readinessProbe,omitempty"`
	LivenessProbe  *k8sProbe          `json:"livenessProbe,omitempty"`
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
	InitialDelaySeconds int        `json:"initialDelaySeconds"`
	PeriodSeconds       int        `json:"periodSeconds"`
}

type k8sHTTPGet struct {
	Path string `json:"path"`
	Port int    `json:"port"`
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
	Match    string                `json:"match"`
	Kind     string                `json:"kind"`
	Services []traefikRouteService `json:"services"`
}

type traefikRouteService struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// traefikTLS as an empty struct serializes to `tls: {}` which tells Traefik
// to use the default TLSStore (LEVE-2 fix: no more certResolver: letsencrypt).
type traefikTLS struct{}

// --- Internal render functions ---

// renderServerApp generates Deployment + Service + IngressRoute for a server app.
func renderServerApp(ri RenderInput, app AppManifestInput) []string {
	port := app.Port
	if port == 0 {
		port = 3000 // sensible default
	}

	deployment := buildDeployment(ri, app, port)
	service := buildService(ri, app, port)
	ingress := buildIngressRoute(ri, app, port)

	return []string{
		marshalResource(deployment),
		marshalResource(service),
		marshalResource(ingress),
	}
}

// renderWorkerApp generates Deployment only (no Service/Ingress — workers don't receive traffic).
func renderWorkerApp(ri RenderInput, app AppManifestInput) []string {
	deployment := buildDeployment(ri, app, 0)
	return []string{marshalResource(deployment)}
}

// renderFrontendApp generates Deployment + Service + IngressRoute for a frontend app.
// The container serves static assets via a lightweight HTTP server.
func renderFrontendApp(ri RenderInput, app AppManifestInput) []string {
	port := app.Port
	if port == 0 {
		port = 80 // typical for frontend static servers
	}

	deployment := buildDeployment(ri, app, port)
	service := buildService(ri, app, port)
	ingress := buildIngressRoute(ri, app, port)

	return []string{
		marshalResource(deployment),
		marshalResource(service),
		marshalResource(ingress),
	}
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

	container := k8sContainer{
		Name:  app.Name,
		Image: app.ImageRef,
		Resources: k8sResources{
			Requests: k8sResourceList{CPU: "100m", Memory: "128Mi"},
			Limits:   k8sResourceList{CPU: "500m", Memory: "512Mi"},
		},
	}

	if port > 0 {
		container.Ports = []k8sContainerPort{
			{ContainerPort: port, Protocol: "TCP"},
		}
		container.ReadinessProbe = &k8sProbe{
			HTTPGet:             k8sHTTPGet{Path: "/health", Port: port},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		}
		container.LivenessProbe = &k8sProbe{
			HTTPGet:             k8sHTTPGet{Path: "/health", Port: port},
			InitialDelaySeconds: 15,
			PeriodSeconds:       20,
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
			Replicas: 1,
			Selector: k8sLabelSelector{MatchLabels: selectorLabels},
			Template: k8sPodTemplateSpec{
				Metadata: k8sPodMeta{Labels: labels},
				Spec:     k8sPodSpec{Containers: []k8sContainer{container}},
			},
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

func buildIngressRoute(ri RenderInput, app AppManifestInput, port int) traefikIngressRoute {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	// Hostname: {app}-{env}--{tenant}.{baseDomain}
	// Single subdomain level under baseDomain so *.dev.loopzero.example wildcard works.
	// Double-dash separates tenant from app-env for unambiguous parsing.
	hostname := fmt.Sprintf("%s-%s--%s.%s",
		app.Name, ri.Environment, ri.TenantID, ri.BaseDomain)

	return traefikIngressRoute{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "IngressRoute",
		Metadata: k8sMetadata{
			Name:      app.Name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: traefikIngressRouteSpec{
			EntryPoints: []string{"websecure"},
			Routes: []traefikRoute{
				{
					Match:    fmt.Sprintf("Host(`%s`)", hostname),
					Kind:     "Rule",
					Services: []traefikRouteService{{Name: app.Name, Port: port}},
				},
			},
			TLS: &traefikTLS{}, // LEVE-2 fix: empty tls → use default TLSStore
		},
	}
}

// --- Helpers ---

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
