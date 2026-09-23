// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Hermetic regression-lock for the OpenBao cert SAN canonical contract
// — closes the post-/review MEDIUM "cert-SAN dependency only LIVE-spike
// proven, no hermetic guard". The slice's D1 (x509-clean base) depends
// on the rendered openbao Certificate including `host.k3d.internal` in
// `dnsNames`. Without it, the mgmt-pod → host.k3d.internal:8200 reach
// would fail TLS verification with "no SAN matches host.k3d.internal"
// — the F1-class blanket failure mode.
//
// The LIVE-spike (Phase 0 of the slice plan) proves the END-TO-END
// reach; this guard proves the RENDER-LEVEL contract is canonical, so
// a future values.yaml edit that drops `host.k3d.internal` from the
// SAN list fails CI before it reaches a LIVE Cleanroom.
//
// Render path: `helm template infra/helm/openbao-cert/` (no kubectl,
// no network). The openbao chart consumes the Secret produced by the
// Certificate; the Certificate's `dnsNames` IS the canonical SAN list.
//
// SCOPE DISCIPLINE:
//
//	This guard ONLY asserts `host.k3d.internal` is in the rendered
//	Certificate's `dnsNames`. It does NOT validate the full SAN list
//	shape (the values.yaml file is the canonical SoT for the full
//	set). Negative-control mutates the rendered output (NOT the
//	values.yaml on disk).
//
// Cross-references:
//   - Source values: infra/helm/openbao-cert/values.yaml `dnsNames`
//   - Source template: infra/helm/openbao-cert/templates/certificate.yaml
//   - Sister rendered guard: vault_host_published_reach_test.go (T1.1 NodePort/serverlb)
//   - F1 root cause sister: vault_bootstrap_san_aligned_host_test.go
//   - /review MEDIUM closed: cert-SAN coverage gap (2026-06-20)
package charts

import (
	"strings"
	"testing"
)

// renderOpenbaoCert renders the openbao-cert chart (hermetic — no kubectl,
// no network). Returns the multi-doc YAML.
func renderOpenbaoCert(t *testing.T) string {
	t.Helper()
	return renderChart(t, uniqueChart(t, "openbao-cert"), defaultRelease, nil)
}

// extractCertificateBlock returns the YAML block of the named
// Certificate from a multi-doc render. Tightens the `kind: Certificate`
// match so it does NOT accidentally match other `kind:` lines.
func extractCertificateBlock(rendered, name string) string {
	for _, doc := range strings.Split(rendered, "\n---\n") {
		hasKind := false
		hasName := false
		for _, line := range strings.Split(doc, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "kind: Certificate" {
				hasKind = true
			}
			if strings.HasPrefix(trimmed, "name: "+name) {
				hasName = true
			}
		}
		if hasKind && hasName {
			return doc
		}
	}
	return ""
}

// auditOpenbaoCertSAN encodes the cert-SAN canonical contract.
// Returns failure messages (empty slice => guard passes).
func auditOpenbaoCertSAN(certBlock string) []string {
	var errs []string

	// (a) Certificate block MUST be non-empty (proves render found it).
	if certBlock == "" {
		errs = append(errs, "(a) rendered output does NOT contain a `kind: Certificate` document with `name: openbao` — chart shape changed or render failed silently.")
		return errs // can't proceed
	}

	// (b) The `dnsNames:` section MUST be present.
	if !strings.Contains(certBlock, "dnsNames:") {
		errs = append(errs, "(b) Certificate `dnsNames:` section is absent — chart template shape changed; openbao would have NO SAN coverage.")
		return errs
	}

	// (c) `host.k3d.internal` MUST appear in `dnsNames` as a SAN entry.
	//     Match the YAML list-item form `- "host.k3d.internal"` or
	//     `- host.k3d.internal` (with optional quotes).
	hasHostK3dInternal := false
	for _, line := range strings.Split(certBlock, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == `- "host.k3d.internal"` ||
			trimmed == `- 'host.k3d.internal'` ||
			trimmed == `- host.k3d.internal` {
			hasHostK3dInternal = true
			break
		}
	}
	if !hasHostK3dInternal {
		errs = append(errs, "(c) rendered Certificate `dnsNames` does NOT include `host.k3d.internal` — the slice's D1 (x509-clean base) depends on this SAN coverage. Without it, the mgmt-pod → host.k3d.internal:8200 reach fails TLS verification (F1-class blanket failure mode). Restore the entry in infra/helm/openbao-cert/values.yaml.")
	}

	return errs
}

// TestOpenbaoCertSANCanonical runs the cert-SAN canonical regression-lock.
func TestOpenbaoCertSANCanonical(t *testing.T) {
	rendered := renderOpenbaoCert(t)
	cert := extractCertificateBlock(rendered, "openbao")
	if errs := auditOpenbaoCertSAN(cert); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestOpenbaoCertSANCanonical_NegativeControl mutates the in-memory
// rendered Certificate to remove the `host.k3d.internal` SAN entry and
// asserts the guard fires. A guard that passes the negative control is
// theatre.
func TestOpenbaoCertSANCanonical_NegativeControl(t *testing.T) {
	rendered := renderOpenbaoCert(t)
	cert := extractCertificateBlock(rendered, "openbao")
	if cert == "" {
		t.Fatalf("negative-control setup failed: rendered Certificate block not found — GREEN test would be vacuous.")
	}

	// Mutate: remove the host.k3d.internal list item from the rendered
	// Certificate block.
	mutated := strings.ReplaceAll(cert, `    - "host.k3d.internal"`, `    # REMOVED FOR NEGATIVE CONTROL`)
	mutated = strings.ReplaceAll(mutated, `    - host.k3d.internal`, `    # REMOVED FOR NEGATIVE CONTROL`)
	if mutated == cert {
		t.Fatalf("negative-control mutation did not modify the rendered Certificate block — the SAN entry shape on disk differs from expected; update both the guard's positive matcher and this mutation.")
	}

	errs := auditOpenbaoCertSAN(mutated)
	cFired := false
	for _, e := range errs {
		if strings.HasPrefix(e, "(c)") {
			cFired = true
			break
		}
	}
	if !cFired {
		t.Errorf("negative-control: removing `host.k3d.internal` from the rendered Certificate did NOT trigger check (c) — guard is theatre.")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}
}
