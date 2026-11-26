// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// The clusters that run platform images pull them from Amazon ECR (#435), and their
// nodes authenticate the way EKS nodes do: the kubelet image credential provider
// (`ecr-credential-provider`), with no pull secret in any namespace.
//
// k3s enables the provider only when BOTH its bin directory and its config exist
// under /var/lib/rancher/credentialprovider (pkg/daemons/agent/agent_linux.go). So
// every node of those clusters mounts that directory from the host, and an
// environment that never writes the config — standalone — runs exactly as before.

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const credentialProviderMount = "${HOME}/.loop-runtime/credentialprovider:/var/lib/rancher/credentialprovider"

type k3dVolume struct {
	Volume      string   `yaml:"volume"`
	NodeFilters []string `yaml:"nodeFilters"`
}

type k3dVolumesConfig struct {
	Volumes []k3dVolume `yaml:"volumes"`
}

// Core runs loop-runtime-api, the operators, loop-console and MinIO; build runs the clone,
// sign and scan steps. Both pull platform images, so every node of both mounts it.
func TestPlatformClustersMountTheCredentialProvider(t *testing.T) {
	for _, file := range []string{"infra/k3d/core-cluster.yaml", "infra/k3d/build-cluster.yaml"} {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repoRoot(), file))
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			var cfg k3dVolumesConfig
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("%s is not valid YAML: %v", file, err)
			}
			var mount *k3dVolume
			for i := range cfg.Volumes {
				if cfg.Volumes[i].Volume == credentialProviderMount {
					mount = &cfg.Volumes[i]
				}
			}
			if mount == nil {
				t.Fatalf("%s: no volume %q — its nodes cannot authenticate to ECR", file, credentialProviderMount)
			}
			filters := strings.Join(mount.NodeFilters, ",")
			for _, role := range []string{"server:*", "agent:*"} {
				if !strings.Contains(filters, role) {
					t.Errorf("%s: the credential-provider mount skips %s (nodeFilters %v) — a pod scheduled there cannot pull", file, role, mount.NodeFilters)
				}
			}
		})
	}
}
