// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// B-072 / ADR 2026-08-13-transit-key-policy.
//
// The api encrypts tenant secrets with a per-tenant Transit key created LAZILY:
// Encrypt reacts to a 404 by POSTing to /v1/transit/keys/loopruntime-tenants-<id>.
// Measured 2026-08-13: the VERSIONED policy — the one `helmfile sync` applies —
// granted `transit/encrypt/+` and `transit/decrypt/+` and NOTHING on
// `transit/keys/*`, so that create is denied by design in every environment that
// loads the chart. The operational runbook and the dev script both granted it.
// Three sources, and the one that ships was the one that said no.
//
// # WHY THIS TEST DERIVES FROM THE CLIENT
//
// The chart and the runbook drifted because nothing compared them. Comparing the
// chart against the runbook would catch this instance and keep the same failure
// mode: both are hand-written prose. What decides is what the CODE asks for, so
// the expectations here come from the client's own constant.
//
// This is the third time in this series the remedy is the same shape — stop
// maintaining two lists, compare against the source that executes (B-087/B-088
// for test targets, B-077 for VAP exemptions).
package helm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// clientKeyPrefix reads the prefix the Transit client actually uses, from the
// client itself. Hard-coding "loopruntime-tenants-" here would recreate the very drift
// this test exists to catch.
func clientKeyPrefix(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "vault", "transit_client.go"))
	if err != nil {
		t.Fatalf("reading the transit client: %v", err)
	}
	m := regexp.MustCompile(`tenantKeyPrefix\s*=\s*"([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("tenantKeyPrefix not found in pkg/vault/transit_client.go — the client changed " +
			"shape and this test can no longer derive what it asks for")
	}
	return string(m[1])
}

// apiPolicy returns the `loopruntime-api` policy body from the openbao-init values.
func apiPolicy(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "helm", "openbao-init", "values.yaml"))
	if err != nil {
		t.Fatalf("reading openbao-init values: %v", err)
	}
	text := string(body)
	start := strings.Index(text, "- name: loopruntime-api\n")
	if start < 0 {
		t.Fatal("policy `loopruntime-api` not found in infra/helm/openbao-init/values.yaml")
	}
	rest := text[start+1:]
	if end := strings.Index(rest, "\n  - name: "); end >= 0 {
		rest = rest[:end]
	}
	if strings.TrimSpace(rest) == "" {
		t.Fatal("the loopruntime-api policy body is empty; a check that read nothing verified nothing")
	}
	return rest
}

// The capability the whole item is about: the client CREATES the key, and the
// versioned policy must allow it.
func TestTransitPolicy_GrantsTheKeyPathTheClientCreates(t *testing.T) {
	prefix := clientKeyPrefix(t)
	policy := apiPolicy(t)

	want := "transit/keys/" + prefix + "*"
	if !strings.Contains(policy, want) {
		t.Errorf("the versioned `loopruntime-api` policy does not grant %q, but the client POSTs to "+
			"/v1/transit/keys/%s<id> when a tenant key is missing (transit_client.go). Lazy key "+
			"creation is denied by design in every environment that loads this chart (B-072).",
			want, prefix)
	}
}

// The lesson the runbook recorded LIVE on 2026-06-10: the Go client's POST is an
// HTTP PUT, which OpenBao policy-enforces as "update" even when the key does not
// exist. With ["create","read"] the token reported the capabilities and the PUT
// still returned 403. No hermetic test can catch that at runtime — this one
// catches it in the policy text instead.
func TestTransitPolicy_KeyPathCarriesUpdateNotJustCreate(t *testing.T) {
	prefix := clientKeyPrefix(t)
	policy := apiPolicy(t)

	block := capabilitiesFor(t, policy, "transit/keys/"+prefix+"*")
	for _, cap := range []string{"create", "update"} {
		if !strings.Contains(block, `"`+cap+`"`) {
			t.Errorf("capability %q missing from the key path. `update` is required IN ADDITION to "+
				"`create` because the client's POST is an HTTP PUT, which OpenBao enforces as "+
				"update semantics — LIVE-caught 2026-06-10 (J-22), see the cutover runbook. "+
				"capabilities found: %s", cap, strings.TrimSpace(block))
		}
	}
}

// Encrypt and decrypt must be scoped to the same prefix. `+` matches ANY single
// segment, which let the api encrypt with any key in the engine; the only key it
// names comes from TenantKeyName.
func TestTransitPolicy_EncryptDecryptScopedToTheClientPrefix(t *testing.T) {
	prefix := clientKeyPrefix(t)
	policy := apiPolicy(t)

	for _, op := range []string{"encrypt", "decrypt"} {
		want := "transit/" + op + "/" + prefix + "*"
		if !strings.Contains(policy, want) {
			t.Errorf("policy does not scope %s to %q. A bare `+` matches any single segment, so the "+
				"api could %s with any key in the engine — and the only key it ever names is "+
				"TenantKeyName's (B-072).", op, want, op)
		}
	}
}

// capabilitiesFor returns the capabilities line following a path declaration.
func capabilitiesFor(t *testing.T, policy, path string) string {
	t.Helper()
	i := strings.Index(policy, path)
	if i < 0 {
		t.Fatalf("path %q not declared in the policy", path)
	}
	rest := policy[i:]
	j := strings.Index(rest, "capabilities")
	if j < 0 {
		t.Fatalf("path %q declares no capabilities", path)
	}
	line := rest[j:]
	if k := strings.Index(line, "\n"); k >= 0 {
		line = line[:k]
	}
	return line
}
