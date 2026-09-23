// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.1 of plan k3d-host-published-vault-reach (sponsor 2026-06-20).
//
// TestVaultHostPublishedReach locks the canonical host-publish topology
// for the loopzero-secrets k3d cluster + openbao helmfile release onto the
// mechanism the T0.1 LIVE spike proved end-to-end (2026-06-20):
//
//	mgmt pod
//	   → docker bridge gateway (172.18.0.1) :8200
//	   → docker DNAT (k3d-loopzero-secrets-serverlb 8200:30200@loadbalancer)
//	   → nginx stream{} L4 passthrough  (TLS bytes intact)
//	   → k3d-loopzero-secrets-server-0 :30200  (kube-proxy NodePort)
//	   → openbao-0 pod :8200
//	   → OpenBao /v1/sys/health → 200 + JSON, no x509, no decode_error
//
// The 7 assertions below all read from disk / render helmfile (no
// kubectl, no live cluster) so the contract is regression-locked across
// Cleanrooms.
//
// Cross-references:
//   - Plan: knowledge-base/plans/k3d-host-published-vault-reach-plan.md T1.1
//   - Blueprint D1: serverlb nginx stream{} = L4 passthrough
//   - D3: --disable=servicelb (Klipper) — single LB mechanism
//   - D4: nodePort: 30200 FIXED — deterministic across Cleanrooms (#112 trap)
//   - D5: confirm chart's fixed-nodePort knob (helm show values openbao/openbao)
//   - EC-1: rendered Service nodePort == 30200 (NOT auto-assigned 31xxx)
//   - EC-5: port/targetPort/nodePort aligned at 8200/8200/30200
//   - F-1 absorption: openbao chart's NetworkPolicy MUST be disabled in
//     vault-local — the default policy allows ingress only from
//     `namespaceSelector: {}` (intra-cluster pod sources). NodePort traffic
//     arrives with source IP from the host network namespace (outside any
//     K8s namespace) and is DROPPED by the policy. T0.1 SPIKE confirmed
//     LIVE that deleting the policy was sufficient to unblock the chain.
//   - Issues #111, #112, #113, #114
package charts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mustRead is a thin wrapper that fails the test on any read error.
func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// renderVaultHelmfileOpenbao renders the openbao release of the vault
// helmfile for the vault-local environment and returns the combined YAML.
func renderVaultHelmfileOpenbao(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command(
		"helmfile",
		"-f", "infra/helmfile/secrets.helmfile.yaml.gotmpl",
		"-e", "vault-local",
		"--selector", "name=openbao",
		"template",
	)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		_, howToRead := ClassifyRenderFailure(string(out))
		t.Fatalf("helmfile template failed: %v\n%s\n\n%s", err, truncate(string(out), 1200), howToRead)
	}
	return string(out)
}

// extractServiceBlock returns the YAML block for the named Service from
// a multi-doc helmfile template output. Honest-best-effort regex split;
// the install-contract suite uses the same shape elsewhere. Tightens the
// `kind: Service` match so it does NOT accidentally match `ServiceAccount`.
func extractServiceBlock(rendered, name string) string {
	// Documents are separated by `^---$` lines in helmfile output.
	kindServiceRE := regexp.MustCompile(`(?m)^kind:\s+Service\s*$`)
	nameRE := regexp.MustCompile(`(?m)^\s*name:\s+` + regexp.QuoteMeta(name) + `\s*$`)
	for _, doc := range strings.Split(rendered, "\n---\n") {
		if !kindServiceRE.MatchString(doc) {
			continue
		}
		if nameRE.MatchString(doc) {
			return doc
		}
	}
	return ""
}

// TestVaultHostPublishedReach — locks the 7-assertion contract.
//
// RED initially: assertions (a)+(b)+(c)+(g) fail before T1.1 GREEN edits;
// (d)+(e)+(f) already match the current source (the edits in this commit
// landed the spike config in the same step as the test).
//
// To skip when helmfile is not on PATH (CI without helmfile):
//
//	LOOPZERO_TEST_SKIP_HELMFILE=1 go test ./...
func TestVaultHostPublishedReach(t *testing.T) {
	if os.Getenv("LOOPZERO_TEST_SKIP_HELMFILE") == "1" {
		t.Skip("LOOPZERO_TEST_SKIP_HELMFILE=1 — helmfile-template assertions skipped")
	}
	if _, err := exec.LookPath("helmfile"); err != nil {
		t.Skip("helmfile not on PATH — skipping render assertions")
	}

	root := repoRoot(t)

	// --- Source-level reads -------------------------------------------------

	clusterYAML := mustRead(t, filepath.Join(root, "infra/k3d/secrets-cluster.yaml"))
	helmfileGotmpl := mustRead(t, filepath.Join(root, "infra/helmfile/secrets.helmfile.yaml.gotmpl"))
	cssYAML := mustRead(t, filepath.Join(root, "infra/manifests/cluster-secret-stores/loopzero-secrets-secrets.yaml"))

	// --- Render -------------------------------------------------------------

	rendered := renderVaultHelmfileOpenbao(t)
	openbaoService := extractServiceBlock(rendered, "openbao")
	if openbaoService == "" {
		t.Fatalf("rendered output has no `openbao` Service block")
	}

	// (a) Service type == NodePort (NOT LoadBalancer).
	if !regexp.MustCompile(`(?m)^\s*type:\s+NodePort\b`).MatchString(openbaoService) {
		t.Errorf("(a) openbao Service.type != NodePort (D1) — rendered block:\n%s", openbaoService)
	}
	if regexp.MustCompile(`(?m)^\s*type:\s+LoadBalancer\b`).MatchString(openbaoService) {
		t.Errorf("(a) openbao Service.type is LoadBalancer — must be NodePort per plan k3d-host-published-vault-reach D1 (regression from R5 of dev-public-cross-cluster-secrets)")
	}

	// (b) Service nodePort == 30200 (FIXED, not auto-assigned).
	// The Service has two ports (https + https-internal); only the https one
	// gets the explicit nodePort. Match the literal `nodePort: 30200`.
	if !regexp.MustCompile(`(?m)^\s*nodePort:\s+30200\b`).MatchString(openbaoService) {
		t.Errorf("(b) openbao Service nodePort != 30200 (D4/D5 — EC-1 #112 trap) — rendered block:\n%s", openbaoService)
	}

	// (c) Service port: 8200 + targetPort: 8200 (EC-5 alignment).
	// Note: openbao chart's Service has 2 ports (https=8200 + https-internal=8201).
	// We assert both 8200 keys appear (port + targetPort) — `(?m)^\s*port:\s+8200`
	// matches with leading-whitespace + literal at line start; the trailing `$`
	// would require coordination with helmfile output's exact line endings.
	if !regexp.MustCompile(`(?m)^\s*port:\s+8200\b`).MatchString(openbaoService) {
		t.Errorf("(c) openbao Service https port != 8200 (EC-5)")
	}
	if !regexp.MustCompile(`(?m)^\s*targetPort:\s+8200\b`).MatchString(openbaoService) {
		t.Errorf("(c) openbao Service https targetPort != 8200 (EC-5)")
	}

	// (d) secrets-cluster.yaml maps 8200:30200@loadbalancer (NOT @server:0).
	if !regexp.MustCompile(`(?m)^\s*-\s*port:\s*8200:30200\s*$`).MatchString(clusterYAML) {
		t.Errorf("(d) secrets-cluster.yaml does NOT map `port: 8200:30200` — the host-publish chain requires the serverlb to forward host:8200 → node:30200 (#112 root cause)")
	}
	if regexp.MustCompile(`(?m)^\s*-\s*port:\s*8200:8200\s*$`).MatchString(clusterYAML) {
		t.Errorf("(d) secrets-cluster.yaml still uses `port: 8200:8200` (the #112 fix that #114 superseded) — must be `8200:30200@loadbalancer`")
	}
	if !strings.Contains(clusterYAML, "- loadbalancer") {
		t.Errorf("(d) secrets-cluster.yaml port mapping must have nodeFilters `- loadbalancer` (publish via serverlb), not `server:0`")
	}

	// (e) --disable=servicelb present in k3s extraArgs (Klipper off).
	if !strings.Contains(clusterYAML, "--disable=servicelb") {
		t.Errorf("(e) secrets-cluster.yaml missing `--disable=servicelb` (D3 — single LB mechanism)")
	}

	// (f) CSS server unchanged at host.k3d.internal:8200 (SAN-clean).
	if !strings.Contains(cssYAML, "https://host.k3d.internal:8200") {
		t.Errorf("(f) CSS server != https://host.k3d.internal:8200 — must stay SAN-covered (D4)")
	}
	// And explicitly NOT a raw IP — the cert SAN doesn't cover 172.18.x.x.
	if regexp.MustCompile(`server:\s*https://172\.18\.`).MatchString(cssYAML) {
		t.Errorf("(f) CSS server appears to be a raw 172.18.x.x IP — breaks the cert SAN (regression)")
	}

	// (g) F-1 absorption: openbao chart NetworkPolicy MUST be disabled in
	// vault-local. The spike confirmed LIVE that the chart-default policy
	// blocks NodePort ingress (source IP from host network namespace is
	// outside `namespaceSelector: {}`). We assert two layers:
	//
	//   1. Source-level: the `vault-local` block of secrets.helmfile.yaml.gotmpl
	//      contains `networkPolicy:\n  enabled: false` (NOT enabled: true).
	//   2. Render-level: the rendered output for env=vault-local has NO
	//      `kind: NetworkPolicy` resource for the openbao release.
	//
	// (The prod env block at secrets.helmfile.yaml.gotmpl:289 KEEPS
	// networkPolicy.enabled=true — prod is NOT host-published; this scope is
	// strictly vault-local.)
	vaultLocalBlock := extractEnvBlock(helmfileGotmpl, "vault-local")
	if vaultLocalBlock == "" {
		t.Fatalf("(g) could not locate `if eq .Environment.Name \"vault-local\"` block in secrets.helmfile.yaml.gotmpl")
	}
	// Match a YAML stanza `networkPolicy:` followed by `enabled: false` on
	// the next non-comment line. `(?s)` makes `.` match newlines; we anchor
	// against `enabled:` literally and require the value to be `false`.
	netpolDisabledRE := regexp.MustCompile(`(?s)networkPolicy:\s*\n\s*enabled:\s+false\b`)
	netpolEnabledRE := regexp.MustCompile(`(?s)networkPolicy:\s*\n\s*enabled:\s+true\b`)
	if !netpolDisabledRE.MatchString(vaultLocalBlock) {
		t.Errorf("(g) vault-local block does NOT set `networkPolicy: { enabled: false }` — F-1 (T0.1 spike LIVE-confirmed: chart-default policy blocks NodePort ingress)")
	}
	if netpolEnabledRE.MatchString(vaultLocalBlock) {
		t.Errorf("(g) vault-local block still has `networkPolicy.enabled: true` — must be false per F-1 absorption")
	}
	// Render-side guard: no NetworkPolicy kind for openbao release.
	for _, doc := range strings.Split(rendered, "\n---\n") {
		if !strings.Contains(doc, "kind: NetworkPolicy") {
			continue
		}
		// Filter to docs from the openbao release (managed-by: Helm + name: openbao).
		if strings.Contains(doc, "name: openbao") &&
			strings.Contains(doc, "app.kubernetes.io/instance: openbao") {
			t.Errorf("(g) rendered output STILL emits a NetworkPolicy for openbao release in vault-local — F-1 not closed:\n%s", doc)
			break
		}
	}
}

// extractEnvBlock returns the inner body of the `{{- if eq
// .Environment.Name "<env>" }} ... {{- end }}` block from a helmfile
// gotmpl source. Returns "" if not found. Handles nested `{{- if ... }}`
// blocks by counting opens vs closes until balanced (the vault helmfile
// nests an inner `{{- if .Values | get "transitAutoUnsealEnabled" ... }}`
// inside the vault-local block).
func extractEnvBlock(src, env string) string {
	open := `{{- if eq .Environment.Name "` + env + `" }}`
	startIdx := strings.Index(src, open)
	if startIdx < 0 {
		return ""
	}
	startIdx += len(open)

	openRE := regexp.MustCompile(`\{\{-?\s*if\b`)
	closeMarker := "{{- end }}"

	depth := 1
	pos := startIdx
	for depth > 0 {
		closeIdx := strings.Index(src[pos:], closeMarker)
		if closeIdx < 0 {
			return ""
		}
		absCloseIdx := pos + closeIdx
		// Count any nested `{{- if ... }}` between pos and absCloseIdx.
		between := src[pos:absCloseIdx]
		nested := len(openRE.FindAllString(between, -1))
		depth += nested
		depth-- // the close we just found
		pos = absCloseIdx + len(closeMarker)
	}
	return src[startIdx : pos-len(closeMarker)]
}
