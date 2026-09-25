// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// NetworkingCrossClusterCorednsSeed is the Go port of
// infra/scripts/local/networking-cross-cluster-coredns.sh.
//
// Patches mgmt cluster CoreDNS NodeHosts ConfigMap so mgmt pods can resolve
// cross-cluster Service LoadBalancer hostnames (k3d-loopzero-build-serverlb,
// k3d-loopzero-cell-0-serverlb, k3d-loopzero-secrets-serverlb).
//
// Uses `docker inspect` to discover each container IP (READ-ONLY, D1-exempt).
// Merges into the existing NodeHosts block instead of overwriting: unrelated
// hosts (e.g. localhost aliases the operator added) are preserved.
type NetworkingCrossClusterCorednsSeed struct{}

func NewNetworkingCrossClusterCorednsSeed() *NetworkingCrossClusterCorednsSeed {
	return &NetworkingCrossClusterCorednsSeed{}
}
func (s *NetworkingCrossClusterCorednsSeed) Name() string { return "networking-cross-cluster-coredns" }

const (
	corednsNamespace  = "kube-system"
	corednsConfigMap  = "coredns"
	corednsDataKey    = "NodeHosts"
	corednsDeployment = "coredns"
)

// corednsContainers is the ordered set of k3d containers whose IPs must resolve
// in the mgmt CoreDNS. The value is the hostname CoreDNS will publish.
// Aliases (e.g. `k3d-loopzero-cell-0-serverlb` for `-server-0`) are handled
// separately in Run.
var corednsContainers = []string{
	"k3d-loopzero-build-serverlb",
	"k3d-loopzero-cell-0-server-0",
	"k3d-loopzero-secrets-server-0",
}

// corednsAliases maps container name → (alias hostname, aliasApplies) so a
// single docker inspect can publish both the canonical hostname AND a common
// alias (matches the bash script's serverlb alias mapping).
var corednsAliases = map[string]string{
	"k3d-loopzero-cell-0-server-0":  "k3d-loopzero-cell-0-serverlb",
	"k3d-loopzero-secrets-server-0": "k3d-loopzero-secrets-serverlb",
}

func (s *NetworkingCrossClusterCorednsSeed) Run(d Deps) Result {
	hosts, err := discoverCorednsHosts(d.Host)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if len(hosts) == 0 {
		return Result{Verdict: FAIL, Reason: "no k3d-loopzero-* docker containers found — run `task setup` first"}
	}
	targetBlock := renderCorednsNodeHosts(hosts)

	cm, err := d.Kube.GetConfigMap(d.Ctx, corednsNamespace, corednsConfigMap)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	current := cm.Data[corednsDataKey]
	merged := mergeCorednsNodeHosts(current, targetBlock)
	if merged == current {
		return Result{Verdict: PASS, Reason: "CoreDNS NodeHosts already canonical (no diff)"}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would patch CoreDNS NodeHosts + rollout coredns"}
	}
	if err := d.Kube.PatchConfigMapData(d.Ctx, corednsNamespace, corednsConfigMap,
		map[string]string{corednsDataKey: merged}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch %s/%s: %v", corednsNamespace, corednsConfigMap, err)}
	}
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, corednsNamespace, corednsDeployment); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout %s/%s: %v", corednsNamespace, corednsDeployment, err)}
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, corednsNamespace, corednsDeployment, d.settleTimeout(corednsSettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s did not settle after the NodeHosts patch: %v", corednsNamespace, corednsDeployment, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("CoreDNS NodeHosts patched (%d canonical entries) + rolled out", len(hosts))}
}

// Verify probes the ConfigMap contains all canonical hostnames.
func (s *NetworkingCrossClusterCorednsSeed) Verify(d Deps) Result {
	hosts, err := discoverCorednsHosts(d.Host)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	cm, err := d.Kube.GetConfigMap(d.Ctx, corednsNamespace, corednsConfigMap)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	current := cm.Data[corednsDataKey]
	for hostname := range hosts {
		if !strings.Contains(current, " "+hostname) && !strings.Contains(current, hostname+"\n") {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("CoreDNS NodeHosts missing %s", hostname)}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("CoreDNS NodeHosts has all %d canonical entries", len(hosts))}
}

// discoverCorednsHosts returns hostname → IP for each present k3d container.
// An absent container is skipped (its entry is absent from the map); any other
// docker failure is returned, so a dead daemon does not read as "no clusters".
func discoverCorednsHosts(h Host) (map[string]string, error) {
	out := map[string]string{}
	for _, container := range corednsContainers {
		ip, err := h.dockerContainerIP(container)
		if errors.Is(err, errContainerAbsent) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[container] = ip
		if alias, ok := corednsAliases[container]; ok {
			out[alias] = ip
		}
	}
	return out, nil
}

// renderCorednsNodeHosts formats hostname → IP into the CoreDNS NodeHosts
// block form (one line per host: `<ip> <hostname>`). Sorted for determinism.
func renderCorednsNodeHosts(hosts map[string]string) string {
	names := make([]string, 0, len(hosts))
	for n := range hosts {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(hosts[n])
		b.WriteByte(' ')
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return b.String()
}

// mergeCorednsNodeHosts unions the existing block with the target block: the
// target hostnames overwrite any prior IP; unrelated hostnames are preserved.
// Result is sorted for determinism.
func mergeCorednsNodeHosts(existing, target string) string {
	targetByHost := parseNodeHostsBlock(target)
	existingByHost := parseNodeHostsBlock(existing)
	for host, ip := range targetByHost {
		existingByHost[host] = ip
	}
	names := make([]string, 0, len(existingByHost))
	for n := range existingByHost {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(existingByHost[n])
		b.WriteByte(' ')
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return b.String()
}

// parseNodeHostsBlock decodes an `<ip> <hostname>` block into a map.
func parseNodeHostsBlock(block string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		out[fields[1]] = fields[0]
	}
	return out
}
