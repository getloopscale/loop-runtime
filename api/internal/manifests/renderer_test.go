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

func TestRender_ServerApp_ProducesThreeDocuments(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents (Deployment, Service, IngressRoute), got %d", len(docs))
	}
}

func TestRender_WorkerApp_ProducesOneDocument(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "processor", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	if len(docs) != 1 {
		t.Fatalf("expected 1 YAML document (Deployment only), got %d", len(docs))
	}
}

func TestRender_FrontendApp_ProducesThreeDocuments(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "dashboard", Type: "frontend", Port: 80, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)
	docs := strings.Split(result, "\n---\n")

	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents, got %d", len(docs))
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

// LEVE-2: IngressRoute must use tls: {} (default TLSStore), not certResolver.
func TestRender_LEVE2_IngressRouteTLS_UsesDefaultTLSStore(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if strings.Contains(result, "certResolver") {
		t.Error("IngressRoute must not contain certResolver — should use tls: {} for default TLSStore")
	}
	if strings.Contains(result, "letsencrypt") {
		t.Error("IngressRoute must not reference letsencrypt")
	}

	// Verify tls: {} is present
	if !strings.Contains(result, "tls: {}") {
		t.Error("IngressRoute must contain 'tls: {}' to use the default TLSStore")
	}
}

func TestRender_WorkerApp_HasNoPorts(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "worker", Type: "worker", ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result := Render(input)

	if strings.Contains(result, "containerPort") {
		t.Error("worker app should not have container ports")
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

	expectedHost := "api-production.webapp.acme.loopzero.example.com"
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

	// server=3 + worker=1 + frontend=3 = 7
	if len(docs) != 7 {
		t.Fatalf("expected 7 YAML documents, got %d", len(docs))
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
