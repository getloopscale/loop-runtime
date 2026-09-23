// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.2 of plan k3d-host-published-vault-reach (ADR D6, sponsor 2026-06-20).
//
// TestHostK3dInternalCanonical asserts the canonical contract for the
// `setup-cross-cluster-networking.sh` script: mgmt CoreDNS `host.k3d.internal`
// MUST resolve to the docker bridge GATEWAY of the `k3d-loopzero` network, NEVER
// to the `k3d-loopzero-cell-0-serverlb` container IP.
//
// Why this gate exists:
//
//	The docker bridge gateway is the canonical k3d host-publish target — every
//	`port: <host>:<nodePort>@loadbalancer` mapping (vault:8200, mgmt
//	observability/tooling, workload ingress) DNAT-routes through the host's
//	external listeners which the gateway exposes from inside the bridge. The
//	workload-serverlb only publishes its OWN ports (8090/8443/30091/6444) and
//	was wrongly chosen as the resolution target by an earlier J-17 Loki path
//	that has long been superseded. The droplet probe 2026-06-20 (commit
//	following T0.1 SPIKE) measured:
//
//	  via GW (172.18.0.1) → vault:8200=HTTP 400 (OpenBao app-layer responded)
//	                     → mgmt :30082=200, :13001=404, :8090=301, :8443=404
//	                     → APIs :6443=400, :6444=400, :6447=400  ✅
//	  via WL (172.18.0.12) → EVERY port = TCP RST ≈ 0.4ms  ✗
//
//	The current script (`:34, :63, :77`) hardcodes WORKLOAD_LB_IP as the
//	resolution target and re-pollutes mgmt CoreDNS on every `task setup`. That
//	silently breaks the vault host-publish chain proved by T1.1 (#111-#114) —
//	the mechanism is right, the name resolution is wrong. Option B (gateway-
//	derived + guarded) over Option A (delete + trust k3d default) because this
//	DNS class already burned 4 cycles on implicit resolution; explicit is the
//	simplest correct design (KISS) per `architecture.md` fail-fast.
//
// The 4 assertions below all read the script as source — no kubectl, no
// docker, no helmfile. Pure-text grep on the canonical file path.
//
// Cross-references:
//   - Plan: knowledge-base/plans/k3d-host-published-vault-reach-plan.md T1.2
//   - ADR D6 (Option B over A): plan body line ~127
//   - Probe doc: docs/host-k3d-internal-collision-probe.md
//   - Companion guard: vault_host_published_reach_test.go (T1.1)
//   - Issues #111, #112, #113, #114
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestHostK3dInternalCanonical — locks the 4-assertion contract on the script.
func TestHostK3dInternalCanonical(t *testing.T) {
	root := repoRoot(t)
	scriptPath := filepath.Join(root, "infra/scripts/local/setup-cross-cluster-networking.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("script not found at %s: %v", scriptPath, err)
	}
	b, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	body := string(b)

	// (a) Script MUST NOT derive the host.k3d.internal target from the
	//     k3d-loopzero-cell-0-serverlb container — that's the F-2 hijack of the
	//     T0.1 SPIKE. `docker inspect k3d-loopzero-cell-0-serverlb` for the
	//     PURPOSE of populating host.k3d.internal is FORBIDDEN.
	if regexp.MustCompile(`(?s)docker\s+inspect\s+k3d-loopzero-cell-0-serverlb`).MatchString(body) {
		t.Errorf("(a) script still derives host.k3d.internal from k3d-loopzero-cell-0-serverlb — the wrong target per ADR D6 (probe-proved RST on every host-publish port). Replace with docker network inspect gateway derivation.")
	}
	// Also forbid the literal pollution line shape from the legacy script.
	if regexp.MustCompile(`(?m)^\s*\S*\$WORKLOAD_LB_IP\s+host\.k3d\.internal\s*['"]?\s*$`).MatchString(body) ||
		regexp.MustCompile(`\$WORKLOAD_LB_IP\s+host\.k3d\.internal`).MatchString(body) {
		t.Errorf("(a) script still writes `$WORKLOAD_LB_IP host.k3d.internal` to NodeHosts — the canonical target is the bridge gateway per ADR D6.")
	}

	// (b) Script MUST derive the docker bridge GATEWAY of the k3d-loopzero
	//     network via `docker network inspect <network>` honoring the
	//     Gateway field. We accept either the literal `k3d-loopzero` or a
	//     variable expansion (e.g. `"${SHARED_NETWORK}"`) PROVIDED that the
	//     resolved network name is `k3d-loopzero` (asserted by the presence of
	//     a `SHARED_NETWORK="k3d-loopzero"` assignment OR the literal token).
	hasInspectLiteral := regexp.MustCompile(`docker\s+network\s+inspect\s+k3d-loopzero\b`).MatchString(body)
	hasInspectVar := regexp.MustCompile(`docker\s+network\s+inspect\s+"?\$\{?[A-Z_]+\}?"?`).MatchString(body) &&
		regexp.MustCompile(`SHARED_NETWORK="?k3d-loopzero"?`).MatchString(body)
	hasGatewayKey := strings.Contains(body, ".Gateway") || strings.Contains(body, "Gateway")
	hasIPAMConfig := strings.Contains(body, ".IPAM.Config") || strings.Contains(body, "IPAM.Config")
	if (!hasInspectLiteral && !hasInspectVar) || !hasGatewayKey || !hasIPAMConfig {
		t.Errorf("(b) script does NOT derive the k3d-loopzero bridge gateway via `docker network inspect k3d-loopzero -f '{{(index .IPAM.Config 0).Gateway}}'` per ADR D6 — required for canonical host.k3d.internal resolution.")
	}

	// (c) The NodeHosts line written to mgmt CoreDNS MUST be `<gateway>
	//     host.k3d.internal` — i.e. the derived gateway variable feeds the
	//     same write-line that previously took $WORKLOAD_LB_IP. We accept a
	//     reasonable canonical variable name `GATEWAY_IP` (per the plan
	//     T1.2 § Why this step). The line must contain `host.k3d.internal`
	//     immediately preceded by an expansion of GATEWAY_IP (or similar
	//     gateway-derived variable matching `.*GATEWAY.*`).
	if !regexp.MustCompile(`\$\{?[A-Z_]*GATEWAY[A-Z_]*\}?\s+host\.k3d\.internal`).MatchString(body) {
		t.Errorf("(c) NodeHosts append/replace line MUST be of the shape `$GATEWAY_IP host.k3d.internal` (gateway-derived variable, NOT $WORKLOAD_LB_IP) per ADR D6.")
	}

	// (d) The script SHOULD document the #111-#115 history in a comment so the
	//     intent survives a future operator skim. Look for at least one of the
	//     issue references in the script body. This is a soft assertion to
	//     prevent silent regression to the legacy workload-serverlb design.
	historyMarkers := []string{"#111", "#112", "#113", "#114", "#115", "ADR D6", "k3d-host-published-vault-reach"}
	found := false
	for _, m := range historyMarkers {
		if strings.Contains(body, m) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("(d) script comments do NOT cite the #111-#115 history nor ADR D6 nor plan slug — operators reading this in 6 months won't know why the gateway derivation matters. Add a comment.")
	}
}
