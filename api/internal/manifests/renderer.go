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
package manifests

import (
	"fmt"
	"strings"
)

// AppManifestInput contains everything needed to generate manifests for a single app.
type AppManifestInput struct {
	Name        string            // e.g., "api", "worker", "dashboard"
	Type        string            // "server", "worker", "frontend"
	Port        int               // Container port (server/frontend only)
	ImageRef    string            // Full image reference, e.g., "registry:5000/tenant/project/api:abc123"
	Secrets     []string          // Secret names from loopzero.yaml (injected via envFrom)
	Env         map[string]string // Environment variables
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

// renderServerApp generates Deployment + Service + IngressRoute for a server app.
func renderServerApp(ri RenderInput, app AppManifestInput) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	port := app.Port
	if port == 0 {
		port = 3000 // sensible default
	}

	deployment := renderDeployment(ns, app.Name, app.ImageRef, labels, port, app.Secrets, app.Env, ri)
	service := renderService(ns, app.Name, labels, port)
	hostname := fmt.Sprintf("%s-%s.%s.%s.%s", app.Name, ri.Environment, ri.ProjectID, ri.TenantID, ri.BaseDomain)
	ingress := renderIngressRoute(ns, app.Name, labels, hostname, port)

	return []string{deployment, service, ingress}
}

// renderWorkerApp generates Deployment only (no Service/Ingress — workers don't receive traffic).
func renderWorkerApp(ri RenderInput, app AppManifestInput) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)

	deployment := renderDeployment(ns, app.Name, app.ImageRef, labels, 0, app.Secrets, app.Env, ri)
	return []string{deployment}
}

// renderFrontendApp generates Deployment + Service + IngressRoute for a frontend app.
// The container serves static assets via a lightweight HTTP server.
func renderFrontendApp(ri RenderInput, app AppManifestInput) []string {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	port := app.Port
	if port == 0 {
		port = 80 // typical for frontend static servers
	}

	deployment := renderDeployment(ns, app.Name, app.ImageRef, labels, port, app.Secrets, app.Env, ri)
	service := renderService(ns, app.Name, labels, port)
	hostname := fmt.Sprintf("%s-%s.%s.%s.%s", app.Name, ri.Environment, ri.ProjectID, ri.TenantID, ri.BaseDomain)
	ingress := renderIngressRoute(ns, app.Name, labels, hostname, port)

	return []string{deployment, service, ingress}
}

// renderDeployment generates a Kubernetes Deployment YAML.
func renderDeployment(ns, appName, imageRef string, labels map[string]string, port int, secrets []string, env map[string]string, ri RenderInput) string {
	labelStr := renderLabels(labels, 6)
	selectorStr := renderLabels(map[string]string{"app": appName, "loopzero.io/tenant": ri.TenantID, "loopzero.io/project": ri.ProjectID}, 8)

	// Container spec
	portSpec := ""
	if port > 0 {
		portSpec = fmt.Sprintf(`
        ports:
        - containerPort: %d
          protocol: TCP
        readinessProbe:
          httpGet:
            path: /health
            port: %d
          initialDelaySeconds: 5
          periodSeconds: 10
        livenessProbe:
          httpGet:
            path: /health
            port: %d
          initialDelaySeconds: 15
          periodSeconds: 20`, port, port, port)
	}

	// Environment variables
	envSpec := ""
	if len(env) > 0 {
		envSpec = "\n        env:"
		for k, v := range env {
			envSpec += fmt.Sprintf("\n        - name: %s\n          value: %q", k, v)
		}
	}

	// Secret references via envFrom
	envFromSpec := ""
	if len(secrets) > 0 {
		envFromSpec = "\n        envFrom:"
		secretRefName := fmt.Sprintf("user-secrets-%s-%s-%s-%s", ri.TenantID, ri.ProjectID, ri.Environment, appName)
		envFromSpec += fmt.Sprintf("\n        - secretRef:\n            name: %s", secretRefName)
	}

	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
%s
  annotations:
    loopzero.io/build-id: %q
    loopzero.io/release-id: %q
spec:
  replicas: 1
  selector:
    matchLabels:
%s
  template:
    metadata:
      labels:
%s
    spec:
      containers:
      - name: %s
        image: %s
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi%s%s%s`,
		appName, ns, labelStr, ri.BuildID, ri.ReleaseID,
		selectorStr,
		renderLabels(labels, 8),
		appName, imageRef,
		portSpec, envSpec, envFromSpec,
	)
}

// renderService generates a Kubernetes Service YAML.
func renderService(ns, appName string, labels map[string]string, port int) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
%s
spec:
  selector:
    app: %s
  ports:
  - port: %d
    targetPort: %d
    protocol: TCP`,
		appName, ns,
		renderLabels(labels, 4),
		appName, port, port,
	)
}

// renderIngressRoute generates a Traefik IngressRoute YAML.
func renderIngressRoute(ns, appName string, labels map[string]string, hostname string, port int) string {
	return fmt.Sprintf(`apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: %s
  namespace: %s
  labels:
%s
spec:
  entryPoints:
  - websecure
  routes:
  - match: Host(%s)
    kind: Rule
    services:
    - name: %s
      port: %d
  tls:
    certResolver: letsencrypt`,
		appName, ns,
		renderLabels(labels, 4),
		"`"+hostname+"`",
		appName, port,
	)
}

// commonLabels returns the standard labels for all resources.
func commonLabels(ri RenderInput, app AppManifestInput) map[string]string {
	return map[string]string{
		"app":                 app.Name,
		"app.kubernetes.io/managed-by": "loopzero",
		"loopzero.io/tenant":      ri.TenantID,
		"loopzero.io/project":     ri.ProjectID,
		"loopzero.io/environment": ri.Environment,
		"loopzero.io/app-type":    app.Type,
	}
}

// namespace returns the Kubernetes namespace for a tenant environment.
func namespace(tenantID, environment string) string {
	return fmt.Sprintf("loopzero-%s-%s", tenantID, environment)
}

// renderLabels formats labels as YAML with the given indentation.
func renderLabels(labels map[string]string, indent int) string {
	prefix := strings.Repeat(" ", indent)
	var lines []string
	for k, v := range labels {
		lines = append(lines, fmt.Sprintf("%s%s: %q", prefix, k, v))
	}
	return strings.Join(lines, "\n")
}
