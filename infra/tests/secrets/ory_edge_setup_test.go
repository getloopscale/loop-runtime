// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The core k3d cluster reserves host ports for the Ory edge NodePorts, and the
// Caddyfile logs in through them. Only `task cloud:setup` applied those NodePort
// manifests, and `task setup` never called it; the bootstrap seed then tried to
// port-forward onto one of the reserved host ports and could never bind (#401).

func taskCommands(t *testing.T, file, name string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var doc struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	task, ok := doc.Tasks[name]
	if !ok {
		t.Fatalf("%s declares no task %s", file, name)
	}
	var b strings.Builder
	for _, c := range task.Cmds {
		if s, ok := c.(string); ok {
			b.WriteString(s + "\n")
		}
	}
	return b.String()
}

func TestTheCoreInstallAppliesTheOryEdgeNodePorts(t *testing.T) {
	cmds := taskCommands(t, "../../../taskfiles/infra.yml", "infra:install:core")
	for _, manifest := range []string{"infra/helm/ory-kratos/nodeport-service.yaml", "infra/helm/ory-hydra/nodeport-service.yaml"} {
		if !strings.Contains(cmds, "apply -f "+manifest) {
			t.Errorf("infra:install:core does not apply %s; the host port k3d reserves for it answers nothing", manifest)
		}
	}
}

func TestTheSeedPortForwardsOnNoHostPortK3dReserves(t *testing.T) {
	cluster, err := os.ReadFile("../../k3d/core-cluster.yaml")
	if err != nil {
		t.Fatalf("read the core k3d config: %v", err)
	}
	reserved := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*-\s*port:\s*"?(\d+):`).FindAllStringSubmatch(string(cluster), -1) {
		reserved[m[1]] = true
	}
	if len(reserved) == 0 {
		t.Fatal("found no host port mapping in the core k3d config; the check measured nothing")
	}
	seed, err := os.ReadFile("../../helmfile/scripts/seed-local-user.sh")
	if err != nil {
		t.Fatalf("read the seed: %v", err)
	}
	for _, m := range regexp.MustCompile(`HYDRA_PORT_LOCAL=["']?(\d+)`).FindAllStringSubmatch(string(seed), -1) {
		if reserved[m[1]] {
			t.Errorf("the seed port-forwards on host port %s, which the core k3d load balancer already binds", m[1])
		}
	}
}
