// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// k3dConfig mirrors the subset of k3d Simple config we validate.
type k3dConfig struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Agents  int `yaml:"agents"`
	KubeAPI struct {
		HostPort string `yaml:"hostPort"`
	} `yaml:"kubeAPI"`
	Ports []struct {
		Port string `yaml:"port"`
	} `yaml:"ports"`
	Registries struct {
		Use []string `yaml:"use"`
	} `yaml:"registries"`
}

func k3dDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "k3d")
}

var k3dFiles = []string{
	"core-cluster.yaml",
	"build-cluster.yaml",
	"cell-0-cluster.yaml",
}

func loadK3dConfigs(t *testing.T) []k3dConfig {
	t.Helper()
	dir := k3dDir()
	configs := make([]k3dConfig, 0, len(k3dFiles))

	for _, name := range k3dFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}

		var cfg k3dConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("failed to parse %s: %v", name, err)
		}
		configs = append(configs, cfg)
	}
	return configs
}

func TestK3dConfigFilesExistAndAreValidYAML(t *testing.T) {
	dir := k3dDir()
	for _, name := range k3dFiles {
		path := filepath.Join(dir, name)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: file does not exist or cannot be read: %v", name, err)
			continue
		}

		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Errorf("%s: invalid YAML: %v", name, err)
		}
	}
}

func TestK3dClusterNamesAreUnique(t *testing.T) {
	configs := loadK3dConfigs(t)

	expectedNames := map[string]bool{
		"loopzero-core":   false,
		"loopzero-build":  false,
		"loopzero-cell-0": false,
	}

	for _, cfg := range configs {
		name := cfg.Metadata.Name
		if _, ok := expectedNames[name]; !ok {
			t.Errorf("unexpected cluster name: %q", name)
			continue
		}
		if expectedNames[name] {
			t.Errorf("duplicate cluster name: %q", name)
		}
		expectedNames[name] = true
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected cluster name %q not found", name)
		}
	}
}

func TestK3dKubeAPIPorts(t *testing.T) {
	configs := loadK3dConfigs(t)

	expectedPorts := map[string]string{
		"loopzero-core":   "6443",
		"loopzero-build":  "6445",
		"loopzero-cell-0": "6444",
	}

	seen := make(map[string]string) // port -> cluster name
	for _, cfg := range configs {
		port := cfg.KubeAPI.HostPort
		name := cfg.Metadata.Name

		if expected, ok := expectedPorts[name]; ok && port != expected {
			t.Errorf("cluster %q: expected kubeAPI port %s, got %s", name, expected, port)
		}

		if other, exists := seen[port]; exists {
			t.Errorf("kubeAPI port %s conflicts: used by %q and %q", port, other, name)
		}
		seen[port] = name
	}
}

// TestK3dAgentCount enforces the per-cluster agent capacity each cluster
// actually needs.
//
//   - loopzero-core   — 2 agents: hosts CRDs + 4 controllers + ArgoCD + Gitea HA
//   - Kratos + Hydra + Postgres + Traefik + cert-manager.
//     1 agent OOM's mid-bootstrap (Achado #131, T5.1 run 4).
//   - loopzero-build  — 1 agent:  only runs Argo Workflows + BuildKit; concurrency
//     is bounded by the workflow controller, not node count.
//   - loopzero-cell-0 — 1 agent: tenant pods scale horizontally, but the
//     cluster itself doesn't need a second node for HA in dev.
//
// Tightening this beyond actual need wastes ~1.7 GiB RAM per cluster on
// the developer host (cf. T5.1 capacity findings).
func TestK3dAgentCount(t *testing.T) {
	configs := loadK3dConfigs(t)

	expected := map[string]int{
		"loopzero-core":   2,
		"loopzero-build":  1,
		"loopzero-cell-0": 1,
	}

	for _, cfg := range configs {
		name := cfg.Metadata.Name
		want, ok := expected[name]
		if !ok {
			t.Errorf("cluster %q: not in expected map (add it or rename the cluster)", name)
			continue
		}
		if cfg.Agents != want {
			t.Errorf("cluster %q: expected agents: %d, got %d", name, want, cfg.Agents)
		}
	}
}

func TestK3dRegistryConfiguration(t *testing.T) {
	configs := loadK3dConfigs(t)

	expectedRegistry := "k3d-loopzero-registry:5000"
	clustersWithRegistry := map[string]bool{
		"loopzero-core":  true,
		"loopzero-build": true,
	}

	for _, cfg := range configs {
		name := cfg.Metadata.Name
		if !clustersWithRegistry[name] {
			continue
		}

		found := false
		for _, reg := range cfg.Registries.Use {
			if reg == expectedRegistry {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cluster %q: registries.use should contain %q", name, expectedRegistry)
		}
	}
}

func TestK3dNoPortMappingConflicts(t *testing.T) {
	configs := loadK3dConfigs(t)

	// Track host ports across all clusters: hostPort -> cluster name
	seen := make(map[string]string)

	for _, cfg := range configs {
		name := cfg.Metadata.Name
		for _, p := range cfg.Ports {
			// Port format is "hostPort:containerPort"
			hostPort := p.Port
			if len(hostPort) > 0 {
				// Extract host port (before the colon)
				for i, c := range hostPort {
					if c == ':' {
						hostPort = hostPort[:i]
						break
					}
				}
			}

			if other, exists := seen[hostPort]; exists {
				t.Errorf("host port %s conflicts: used by cluster %q and %q", hostPort, other, name)
			}
			seen[hostPort] = name
		}
	}
}
