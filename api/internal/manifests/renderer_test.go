// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

func baseInput() RenderInput {
	return RenderInput{
		TenantID:    "acme",
		ProjectID:   "webapp",
		Environment: "production",
		BuildID:     "build-001",
		ReleaseID:   "rel-001",
		BaseDomain:  "loopzero.example.com",
	}
}

func TestRender_ServerApp_ProducesFiveDocuments(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	// Deployment + PDB + Service + Certificate + IngressRoute = 5
	if len(docs) != 5 {
		t.Fatalf("expected 5 YAML documents (Deployment, PDB, Service, Certificate, IngressRoute), got %d", len(docs))
	}
}

func TestRender_WorkerApp_ProducesTwoDocuments(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "processor", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	// Deployment + PDB = 2 (F1-C2: workers MUST get PDB)
	if len(docs) != 2 {
		t.Fatalf("expected 2 YAML documents (Deployment, PDB), got %d", len(docs))
	}
}

func TestRender_FrontendApp_ProducesFiveDocuments(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "dashboard", Type: "frontend", Port: 80, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	// Deployment + PDB + Service + Certificate + IngressRoute = 5
	if len(docs) != 5 {
		t.Fatalf("expected 5 YAML documents (Deployment, PDB, Service, Certificate, IngressRoute), got %d", len(docs))
	}
}

// MOD-1: YAML special characters in env values must not corrupt the manifest.
func TestRender_MOD1_SpecialCharsInEnvValues_ProducesValidYAML(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     3000,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Env: map[string]string{
				"DB_URL":      "postgres://user:p@ss#word@host:5432/db",
				"COMMENT":     "value with # hash and : colon",
				"JSON_CONFIG": `{"key": "value", "nested": {"a": 1}}`,
				"MULTILINE":   "line1\nline2\nline3",
				"CURLY":       "{braces} and [brackets]",
			},
		},
	}

	result := Render(input)

	// Every document must be valid YAML
	docs := strings.Split(result, "\n---\n")
	for i, doc := range docs {
		var parsed interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Errorf("document %d is not valid YAML: %v\ndocument:\n%s", i, err, doc)
		}
	}
}

// MOD-5: Rendering must be deterministic — same input, same output.
func TestRender_MOD5_DeterministicOutput(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Env: map[string]string{
				"Z_VAR": "last",
				"A_VAR": "first",
				"M_VAR": "middle",
			},
		},
	}

	// Render 50 times and verify all outputs are identical.
	first := Render(input)
	for i := 0; i < 50; i++ {
		got := Render(input)
		if got != first {
			t.Fatalf("non-deterministic output detected on iteration %d", i)
		}
	}
}

// IngressRoute must use tls.secretName referencing the cert-manager Certificate, not certResolver.
func TestRender_IngressRouteTLS_UsesSecretName(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if strings.Contains(result, "certResolver") {
		t.Error("IngressRoute must not contain certResolver — should use tls.secretName")
	}

	// Verify tls.secretName references the Certificate secret
	if !strings.Contains(result, "secretName: tls-api") {
		t.Error("IngressRoute must contain 'secretName: tls-api' referencing the Certificate secret")
	}

	// Verify a Certificate CR is generated
	if !strings.Contains(result, "kind: Certificate") {
		t.Error("output must contain a cert-manager Certificate CR")
	}
	if !strings.Contains(result, "name: letsencrypt-prod") {
		t.Error("Certificate must reference letsencrypt-prod ClusterIssuer")
	}
}

func TestRender_WorkerApp_HasNoPortsOrProbes(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "worker", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if strings.Contains(result, "containerPort") {
		t.Error("worker app should not have container ports")
	}
	if strings.Contains(result, "startupProbe") {
		t.Error("worker app should not have startup probe")
	}
	if strings.Contains(result, "readinessProbe") {
		t.Error("worker app should not have readiness probe")
	}
	if strings.Contains(result, "livenessProbe") {
		t.Error("worker app should not have liveness probe")
	}
}

func TestRender_ServerApp_DefaultPort3000(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 0, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if !strings.Contains(result, "containerPort: 3000") {
		t.Error("server app with port=0 should default to 3000")
	}
}

func TestRender_FrontendApp_DefaultPort80(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "dashboard", Type: "frontend", Port: 0, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if !strings.Contains(result, "containerPort: 80") {
		t.Error("frontend app with port=0 should default to 80")
	}
}

func TestRender_SecretsGenerateEnvFrom(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Secrets:  []string{"db-credentials"},
		},
	}

	result := Render(input)

	expectedSecretRef := "user-secrets-acme-webapp-production-api"
	if !strings.Contains(result, expectedSecretRef) {
		t.Errorf("expected envFrom secretRef name %q in output", expectedSecretRef)
	}
}

func TestRender_Namespace(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if !strings.Contains(result, "namespace: loopzero-acme-production") {
		t.Error("namespace should be loopzero-{tenantID}-{environment}")
	}
}

func TestRender_Hostname(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	expectedHost := "acme-webapp-api-production.loopzero.example.com"
	if !strings.Contains(result, expectedHost) {
		t.Errorf("expected hostname %q in IngressRoute", expectedHost)
	}
}

func TestRender_MultipleApps(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
		{Name: "worker", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
		{Name: "dashboard", Type: "frontend", Port: 80, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	// server=5 + worker=2 + frontend=5 = 12
	if len(docs) != 12 {
		t.Fatalf("expected 12 YAML documents, got %d", len(docs))
	}

	// All documents must be valid YAML
	for i, doc := range docs {
		var parsed interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Errorf("document %d is not valid YAML: %v", i, err)
		}
	}
}

func TestRender_EnvVarsSortedByKey(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "worker",
			ImageRef: "reg:5000/acme/webapp:abc123",
			Env: map[string]string{
				"ZEBRA":    "z",
				"ALPHA":    "a",
				"MIDDLE":   "m",
			},
		},
	}

	result := Render(input)

	alphaIdx := strings.Index(result, "ALPHA")
	middleIdx := strings.Index(result, "MIDDLE")
	zebraIdx := strings.Index(result, "ZEBRA")

	if alphaIdx == -1 || middleIdx == -1 || zebraIdx == -1 {
		t.Fatal("expected all env vars to be present in output")
	}
	if alphaIdx >= middleIdx || middleIdx >= zebraIdx {
		t.Error("env vars should be sorted alphabetically by key")
	}
}

func TestRender_AllDocumentsAreValidYAML(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Secrets:  []string{"db-creds"},
			Env: map[string]string{
				"DATABASE_URL": "postgres://user:pass@db:5432/mydb",
			},
		},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	for i, doc := range docs {
		var parsed map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Errorf("document %d is not valid YAML: %v\n---\n%s", i, err, doc)
			continue
		}

		// Every document must have apiVersion, kind, metadata
		if _, ok := parsed["apiVersion"]; !ok {
			t.Errorf("document %d missing apiVersion", i)
		}
		if _, ok := parsed["kind"]; !ok {
			t.Errorf("document %d missing kind", i)
		}
		if _, ok := parsed["metadata"]; !ok {
			t.Errorf("document %d missing metadata", i)
		}
	}
}

// --- Structural tests (F1-C7: parse YAML into typed structs, not string matching) ---

// parseDeployment extracts the first Deployment from rendered output.
func parseDeployment(t *testing.T, rendered string) k8sDeployment {
	t.Helper()
	docs := strings.Split(rendered, "\n---\n")
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "Deployment" {
			var dep k8sDeployment
			if err := sigsyaml.Unmarshal([]byte(doc), &dep); err != nil {
				t.Fatalf("failed to parse Deployment: %v", err)
			}
			return dep
		}
	}
	t.Fatal("no Deployment found in rendered output")
	return k8sDeployment{}
}

// parsePDB extracts the first PDB from rendered output.
func parsePDB(t *testing.T, rendered string) k8sPDB {
	t.Helper()
	docs := strings.Split(rendered, "\n---\n")
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "PodDisruptionBudget" {
			var pdb k8sPDB
			if err := sigsyaml.Unmarshal([]byte(doc), &pdb); err != nil {
				t.Fatalf("failed to parse PDB: %v", err)
			}
			return pdb
		}
	}
	t.Fatal("no PodDisruptionBudget found in rendered output")
	return k8sPDB{}
}

func TestRender_ServerApp_StartupProbe(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	container := dep.Spec.Template.Spec.Containers[0]

	if container.StartupProbe == nil {
		t.Fatal("server app must have startup probe")
	}
	sp := container.StartupProbe
	if sp.HTTPGet.Path != "/health" {
		t.Errorf("startup probe path: got %q, want /health", sp.HTTPGet.Path)
	}
	if sp.PeriodSeconds != 5 {
		t.Errorf("startup probe periodSeconds: got %d, want 5", sp.PeriodSeconds)
	}
	if sp.TimeoutSeconds != 3 {
		t.Errorf("startup probe timeoutSeconds: got %d, want 3", sp.TimeoutSeconds)
	}
	if sp.FailureThreshold != 30 {
		t.Errorf("startup probe failureThreshold: got %d, want 30 (150s max startup)", sp.FailureThreshold)
	}
}

func TestRender_ServerApp_ReadinessProbeThresholds(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	container := dep.Spec.Template.Spec.Containers[0]

	if container.ReadinessProbe == nil {
		t.Fatal("server app must have readiness probe")
	}
	rp := container.ReadinessProbe
	if rp.PeriodSeconds != 10 {
		t.Errorf("readiness periodSeconds: got %d, want 10", rp.PeriodSeconds)
	}
	if rp.TimeoutSeconds != 3 {
		t.Errorf("readiness timeoutSeconds: got %d, want 3", rp.TimeoutSeconds)
	}
	if rp.SuccessThreshold != 1 {
		t.Errorf("readiness successThreshold: got %d, want 1", rp.SuccessThreshold)
	}
	if rp.FailureThreshold != 3 {
		t.Errorf("readiness failureThreshold: got %d, want 3", rp.FailureThreshold)
	}
}

func TestRender_ServerApp_LivenessProbeThresholds(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	container := dep.Spec.Template.Spec.Containers[0]

	if container.LivenessProbe == nil {
		t.Fatal("server app must have liveness probe")
	}
	lp := container.LivenessProbe
	if lp.PeriodSeconds != 15 {
		t.Errorf("liveness periodSeconds: got %d, want 15", lp.PeriodSeconds)
	}
	if lp.TimeoutSeconds != 3 {
		t.Errorf("liveness timeoutSeconds: got %d, want 3", lp.TimeoutSeconds)
	}
	if lp.SuccessThreshold != 1 {
		t.Errorf("liveness successThreshold: got %d, want 1", lp.SuccessThreshold)
	}
	if lp.FailureThreshold != 5 {
		t.Errorf("liveness failureThreshold: got %d, want 5 (more tolerant than readiness)", lp.FailureThreshold)
	}
}

func TestRender_Deployment_ProgressDeadlineAndMinReady(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))

	// F1-C1: progressDeadlineSeconds = 210 (not 180)
	if dep.Spec.ProgressDeadlineSeconds != 210 {
		t.Errorf("progressDeadlineSeconds: got %d, want 210", dep.Spec.ProgressDeadlineSeconds)
	}
	if dep.Spec.MinReadySeconds != 10 {
		t.Errorf("minReadySeconds: got %d, want 10", dep.Spec.MinReadySeconds)
	}
}

func TestRender_Deployment_ZeroDowntimeRollingStrategy(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))

	if dep.Spec.Strategy == nil {
		t.Fatal("deployment must have strategy defined")
	}
	if dep.Spec.Strategy.Type != "RollingUpdate" {
		t.Errorf("strategy type: got %q, want RollingUpdate", dep.Spec.Strategy.Type)
	}
	if dep.Spec.Strategy.RollingUpdate == nil {
		t.Fatal("strategy must have rollingUpdate config")
	}
	if dep.Spec.Strategy.RollingUpdate.MaxUnavailable != 0 {
		t.Errorf("maxUnavailable: got %d, want 0 (zero-downtime)", dep.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
	if dep.Spec.Strategy.RollingUpdate.MaxSurge != 1 {
		t.Errorf("maxSurge: got %q, want 1", dep.Spec.Strategy.RollingUpdate.MaxSurge)
	}
}

func TestRender_Deployment_GracefulShutdown(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))

	if dep.Spec.Template.Spec.TerminationGracePeriodSeconds != 30 {
		t.Errorf("terminationGracePeriodSeconds: got %d, want 30", dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}
}

func TestRender_ServerApp_PDB(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	pdb := parsePDB(t, Render(input))

	if pdb.APIVersion != "policy/v1" {
		t.Errorf("PDB apiVersion: got %q, want policy/v1", pdb.APIVersion)
	}
	if pdb.Metadata.Name != "api-pdb" {
		t.Errorf("PDB name: got %q, want api-pdb", pdb.Metadata.Name)
	}
	if pdb.Spec.MaxUnavailable != 1 {
		t.Errorf("PDB maxUnavailable: got %d, want 1", pdb.Spec.MaxUnavailable)
	}
	if pdb.Spec.UnhealthyPodEvictionPolicy != "AlwaysAllow" {
		t.Errorf("PDB unhealthyPodEvictionPolicy: got %q, want AlwaysAllow", pdb.Spec.UnhealthyPodEvictionPolicy)
	}
}

// F1-C2: Workers MUST get PDB (no tenant workload should block node drain)
func TestRender_WorkerApp_HasPDB(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "processor", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	pdb := parsePDB(t, Render(input))

	if pdb.Spec.UnhealthyPodEvictionPolicy != "AlwaysAllow" {
		t.Errorf("worker PDB unhealthyPodEvictionPolicy: got %q, want AlwaysAllow", pdb.Spec.UnhealthyPodEvictionPolicy)
	}
}

// Worker deployments also get strategy, deadline, and graceful shutdown
func TestRender_WorkerApp_DeploymentProtection(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "processor", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))

	if dep.Spec.ProgressDeadlineSeconds != 210 {
		t.Errorf("worker progressDeadlineSeconds: got %d, want 210", dep.Spec.ProgressDeadlineSeconds)
	}
	if dep.Spec.Strategy == nil || dep.Spec.Strategy.RollingUpdate == nil {
		t.Fatal("worker must have rolling update strategy")
	}
	if dep.Spec.Strategy.RollingUpdate.MaxUnavailable != 0 {
		t.Errorf("worker maxUnavailable: got %d, want 0", dep.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
	if dep.Spec.Template.Spec.TerminationGracePeriodSeconds != 30 {
		t.Errorf("worker terminationGracePeriodSeconds: got %d, want 30", dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}
}

// --- Sprint 7: Workload Security ---

// Task 7.1: Pod SecurityContext — pods must run as non-root with restricted capabilities.
func TestRender_SecurityContext_PodRunsAsNonRoot(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	podSec := dep.Spec.Template.Spec.SecurityContext
	if podSec == nil {
		t.Fatal("pod must have securityContext")
	}
	if podSec.RunAsNonRoot == nil || !*podSec.RunAsNonRoot {
		t.Error("pod securityContext.runAsNonRoot must be true")
	}
	if podSec.RunAsUser == nil || *podSec.RunAsUser != 1000 {
		t.Error("pod securityContext.runAsUser must be 1000")
	}
	if podSec.RunAsGroup == nil || *podSec.RunAsGroup != 1000 {
		t.Error("pod securityContext.runAsGroup must be 1000")
	}
	if podSec.FSGroup == nil || *podSec.FSGroup != 1000 {
		t.Error("pod securityContext.fsGroup must be 1000")
	}
}

func TestRender_SecurityContext_ContainerRestricted(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	cSec := dep.Spec.Template.Spec.Containers[0].SecurityContext
	if cSec == nil {
		t.Fatal("container must have securityContext")
	}
	if cSec.AllowPrivilegeEscalation == nil || *cSec.AllowPrivilegeEscalation {
		t.Error("container securityContext.allowPrivilegeEscalation must be false")
	}
	if cSec.ReadOnlyRootFilesystem == nil || !*cSec.ReadOnlyRootFilesystem {
		t.Error("container securityContext.readOnlyRootFilesystem must be true")
	}
	if cSec.Capabilities == nil || len(cSec.Capabilities.Drop) == 0 || cSec.Capabilities.Drop[0] != "ALL" {
		t.Error("container securityContext.capabilities.drop must contain ALL")
	}
}

func TestRender_SecurityContext_WorkerAlsoSecured(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "worker", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	if dep.Spec.Template.Spec.SecurityContext == nil {
		t.Fatal("worker pod must also have securityContext")
	}
	if dep.Spec.Template.Spec.Containers[0].SecurityContext == nil {
		t.Fatal("worker container must also have securityContext")
	}
}

// Task 7.4: ServiceAccount — pods must use a dedicated SA, not default.
func TestRender_ServiceAccount_Dedicated(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, Render(input))
	if dep.Spec.Template.Spec.ServiceAccountName != "loopzero-app" {
		t.Errorf("serviceAccountName: got %q, want loopzero-app", dep.Spec.Template.Spec.ServiceAccountName)
	}
	if dep.Spec.Template.Spec.AutomountServiceAccountToken == nil || *dep.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Error("automountServiceAccountToken must be false")
	}
}

// Backward compatibility: baseInput with no new config produces valid manifests (W3 gate)
func TestRender_BackwardCompat_DefaultsApplied(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	// Must produce exactly 5 documents (Deployment + PDB + Service + Certificate + IngressRoute)
	if len(docs) != 5 {
		t.Fatalf("backward compat: expected 5 docs, got %d", len(docs))
	}

	// All documents must be valid YAML
	for i, doc := range docs {
		var parsed interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Errorf("backward compat: document %d is not valid YAML: %v", i, err)
		}
	}

	// Deployment must have all protection fields with defaults
	dep := parseDeployment(t, result)
	if dep.Spec.ProgressDeadlineSeconds == 0 {
		t.Error("backward compat: progressDeadlineSeconds must have a default value")
	}
	if dep.Spec.Strategy == nil {
		t.Error("backward compat: strategy must have a default value")
	}
}
