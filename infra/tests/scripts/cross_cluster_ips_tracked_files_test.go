// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// update-cross-cluster-ips.sh runs inside `task setup` (networking:setup). It rewrote
// two tracked files with this machine's container IPs, so every cold setup left the
// checkout dirty — the cleanroom's baseline diff failed on working_tree_clean, and a
// commit after a setup carried one host's IPs into git (#264, measured 2026-09-28).
// Cross-cluster addresses are container names, which Docker's DNS resolves on the
// k3d network; a setup step must never edit a file git tracks.
func TestTheCrossClusterIPScriptWritesNoTrackedFile(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/local/update-cross-cluster-ips.sh")
	if err != nil {
		t.Fatalf("read the script: %v", err)
	}
	src := string(raw)
	paths := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*([A-Z_]+)="(infra/[^"]+)"`).FindAllStringSubmatch(src, -1) {
		paths[m[1]] = m[2]
	}
	tracked, err := exec.Command("git", "-C", "../../..", "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	isTracked := map[string]bool{}
	for _, f := range strings.Split(string(tracked), "\n") {
		isTracked[f] = true
	}
	for _, m := range regexp.MustCompile(`sed -i [^\n]*"\$\{?([A-Za-z_]+)\}?"`).FindAllStringSubmatch(src, -1) {
		target := paths[m[1]]
		if target == "" {
			target = "$" + m[1]
		}
		if isTracked[target] {
			t.Errorf("the script edits the tracked file %s (via $%s)", target, m[1])
		}
	}
	if strings.Contains(src, "patch_env_inline \"$OPERATOR_DEPLOYMENT\"") {
		t.Error("the script still patches the tracked infra/tilt/operator-deployment.yaml")
	}
}

// The committed scrape targets name the containers rather than carrying a host's IPs.
func TestTheMonitoringScrapeTargetsNameTheContainers(t *testing.T) {
	raw, err := os.ReadFile("../../helm/monitoring/values.yaml")
	if err != nil {
		t.Fatalf("read the monitoring values: %v", err)
	}
	if ip := regexp.MustCompile(`"\d+\.\d+\.\d+\.\d+:3009[01]"`).Find(raw); ip != nil {
		t.Errorf("infra/helm/monitoring/values.yaml carries the IP target %s; name the k3d container", ip)
	}
}
