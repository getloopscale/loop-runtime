package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func helmDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "helm")
}

var helmCharts = []string{
	"postgresql",
	"gitea",
	"minio",
	"argocd",
	"cert-manager",
	"argo-workflows",
	"traefik",
	"monitoring",
	"otel",
}

func TestHelmValuesFilesExist(t *testing.T) {
	dir := helmDir()
	for _, chart := range helmCharts {
		path := filepath.Join(dir, chart, "values.yaml")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("values file missing: %s", path)
		}
	}
}

func TestHelmValuesNoLatestImageTag(t *testing.T) {
	dir := helmDir()
	for _, chart := range helmCharts {
		path := filepath.Join(dir, chart, "values.yaml")

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		// First check: parse YAML and look for tag fields set to "latest"
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Errorf("%s: invalid YAML: %v", chart, err)
			continue
		}
		if containsLatestTag(raw) {
			t.Errorf("%s: values.yaml contains an image tag set to \"latest\"", chart)
		}

		// Second check: scan raw text for common patterns like `tag: latest` or `tag: "latest"`
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "tag:") {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "tag:"))
				value = strings.Trim(value, `"'`)
				if value == "latest" {
					t.Errorf("%s: values.yaml line %d has tag set to \"latest\"", chart, i+1)
				}
			}
		}
	}
}

func TestHelmValuesHaveResourceLimits(t *testing.T) {
	dir := helmDir()
	for _, chart := range helmCharts {
		path := filepath.Join(dir, chart, "values.yaml")

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		content := string(data)
		if !strings.Contains(content, "limits:") {
			t.Errorf("%s: values.yaml missing resource \"limits:\"", chart)
		}
		if !strings.Contains(content, "cpu:") {
			t.Errorf("%s: values.yaml missing \"cpu:\" in resource limits", chart)
		}
		if !strings.Contains(content, "memory:") {
			t.Errorf("%s: values.yaml missing \"memory:\" in resource limits", chart)
		}
	}
}

func TestHelmValuesRunAsNonRoot(t *testing.T) {
	dir := helmDir()
	for _, chart := range helmCharts {
		path := filepath.Join(dir, chart, "values.yaml")

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		content := string(data)
		if !strings.Contains(content, "runAsNonRoot: true") {
			t.Errorf("%s: values.yaml missing \"runAsNonRoot: true\"", chart)
		}
	}
}

func TestHelmValuesHaveHealthProbes(t *testing.T) {
	dir := helmDir()
	for _, chart := range helmCharts {
		path := filepath.Join(dir, chart, "values.yaml")

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		content := string(data)
		hasLiveness := strings.Contains(content, "livenessProbe")
		hasReadiness := strings.Contains(content, "readinessProbe")
		if !hasLiveness {
			t.Errorf("%s: values.yaml missing livenessProbe configuration", chart)
		}
		if !hasReadiness {
			t.Errorf("%s: values.yaml missing readinessProbe configuration", chart)
		}
	}
}

// containsLatestTag recursively checks a parsed YAML tree for any key named
// "tag" with a string value of "latest".
func containsLatestTag(node interface{}) bool {
	switch v := node.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if key == "tag" {
				if s, ok := val.(string); ok && s == "latest" {
					return true
				}
			}
			if containsLatestTag(val) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if containsLatestTag(item) {
				return true
			}
		}
	}
	return false
}
