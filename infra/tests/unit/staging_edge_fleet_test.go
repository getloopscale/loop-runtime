// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Staging runs loop-edge, the fleet that serves custom domains in production, on its core
// cluster. The fleet proxies a host to its cell's load balancer, and the emulator gives
// each of those a new name on every rebuild, so the names are read from the cells.

const (
	cell0EdgeALB = "k8s-traefik-traefike-0000000000-1111111111.us-east-1.elb.amazonaws.com"
	cell1EdgeALB = "k8s-traefik-traefike-2222222222-3333333333.us-east-1.elb.amazonaws.com"
)

func TestMonitoringAddressesExportsEveryCellOrigin(t *testing.T) {
	f := newFakeCluster(t, fakeIngressKubectl)
	f.publish("cell-0", "traefik-edge", cell0EdgeALB)
	f.publish("cell-1", "traefik-edge", cell1EdgeALB)

	out, stderr, err := f.run("monitoring-addresses.sh", "cell-origins", "--wait", "5")

	if err != nil {
		t.Fatalf("monitoring-addresses.sh cell-origins: %v\n%s", err, stderr)
	}
	want := "cell-0=" + cell0EdgeALB + ",cell-1=" + cell1EdgeALB
	if got := exported(t, out, "LOOP_RUNTIME_CELL_ORIGINS"); got != want {
		t.Errorf("LOOP_RUNTIME_CELL_ORIGINS = %q, want %q", got, want)
	}
	calls, _ := os.ReadFile(filepath.Join(f.state, "calls"))
	if !strings.Contains(string(calls), "--context cell-1 -n traefik get ingress traefik-edge") {
		t.Errorf("the cells' edge Ingress was not read from their traefik namespace: %s", calls)
	}
}

// The fleet is synced once the cells publish their load balancers, with the emulator's
// authority where its pods can read it. The sync that runs before the cells leaves it out:
// on a rebuild it would have no cell to proxy to.
func TestStagingPlatformSyncsTheCustomDomainFleetAfterTheCells(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	order := []string{
		"helmfile --selector 'name!=promtail,name!=loop-edge' -f infra/helmfile/core.helmfile.yaml.gotmpl",
		"infra/helmfile/cell.helmfile.yaml.gotmpl",
		"monitoring-addresses.sh cell-origins --wait",
		"create configmap loop-edge-emulator-ca",
		"helmfile --selector name=loop-edge -f infra/helmfile/core.helmfile.yaml.gotmpl",
		"rollout status deployment/loop-edge",
	}
	at := 0
	for _, step := range order {
		i := strings.Index(text[at:], step)
		if i < 0 {
			t.Fatalf("taskfiles/staging.yml: %q is missing or out of order (after offset %d)", step, at)
		}
		at += i + len(step)
	}
}
