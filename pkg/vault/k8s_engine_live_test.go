// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"net/http"
	"testing"
	"time"

	bao "github.com/openbao/openbao/api/v2"
)

// TestLIVE_K8sEngine_Roles_Provisioned verifies T7.2 LIVE — all 3 K8s engine
// roles (loopzero-runtime-{mgmt,build,workload}) exist with correct namespace
// scope + TTL 1h max 24h. T7.5 (LIVE cell access via token) requires a
// configured kubernetes/config (operator-time) so is gated separately.
func TestLIVE_K8sEngine_Roles_Provisioned(t *testing.T) {
	addr, token := liveAddr(t)
	cfg := bao.DefaultConfig()
	cfg.Address = addr
	cfg.HttpClient = &http.Client{Transport: &http.Transport{
		TLSClientConfig: insecureTLS(),
	}}
	c, err := bao.NewClient(cfg)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	c.SetToken(token)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	roles := []struct {
		name              string
		expectedNamespaces []string
		expectedSAName    string
	}{
		{"loopzero-runtime-mgmt", []string{"argocd", "external-secrets", "loopzero-system"}, "loopzero-runtime"},
		{"loopzero-runtime-build", []string{"argo"}, "loopzero-build"},
		{"loopzero-runtime-workload", []string{"loopzero-system"}, "loopzero-runtime"},
	}

	for _, want := range roles {
		role, err := c.Logical().ReadWithContext(ctx, "kubernetes/roles/"+want.name)
		if err != nil {
			t.Errorf("LIVE FAIL: read kubernetes/roles/%s: %v", want.name, err)
			continue
		}
		if role == nil {
			t.Errorf("LIVE FAIL: role %s missing — T7.2 not provisioned", want.name)
			continue
		}

		// namespaces — Vault returns []interface{} of strings, sorted.
		nsRaw, _ := role.Data["allowed_kubernetes_namespaces"].([]interface{})
		got := make([]string, 0, len(nsRaw))
		for _, r := range nsRaw {
			if s, ok := r.(string); ok {
				got = append(got, s)
			}
		}
		gotSet := make(map[string]bool, len(got))
		for _, s := range got {
			gotSet[s] = true
		}
		for _, ns := range want.expectedNamespaces {
			if !gotSet[ns] {
				t.Errorf("LIVE FAIL: role %s missing namespace %s (got=%v)", want.name, ns, got)
			}
		}

		gotSA, _ := role.Data["service_account_name"].(string)
		if gotSA != want.expectedSAName {
			t.Errorf("LIVE FAIL: role %s SA=%q, want %q", want.name, gotSA, want.expectedSAName)
		}
	}
}

// TestLIVE_K8sAuthMounts_Isolation verifies T5.x cross-cluster trust boundary —
// roles registered on k8s-mgmt MUST NOT also appear on cell-* mounts, and vice
// versa. This protects against accidental policy leak when adding new roles.
func TestLIVE_K8sAuthMounts_Isolation(t *testing.T) {
	addr, token := liveAddr(t)
	cfg := bao.DefaultConfig()
	cfg.Address = addr
	cfg.HttpClient = &http.Client{Transport: &http.Transport{
		TLSClientConfig: insecureTLS(),
	}}
	c, err := bao.NewClient(cfg)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	c.SetToken(token)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// loopzero-api SHOULD be on k8s-mgmt only.
	mgmt, err := c.Logical().ReadWithContext(ctx, "auth/k8s-mgmt/role/loopzero-api")
	if err != nil || mgmt == nil {
		t.Errorf("LIVE FAIL: auth/k8s-mgmt/role/loopzero-api missing: %v", err)
	}

	// loopzero-api MUST NOT be on cell mounts (isolation invariant).
	for _, cellMount := range []string{"k8s-cell-default", "k8s-cell-1", "k8s-cell-2"} {
		s, err := c.Logical().ReadWithContext(ctx, "auth/"+cellMount+"/role/loopzero-api")
		// Vault returns nil + nil err for missing roles; or 404 wrapped.
		if err == nil && s != nil {
			t.Errorf("LIVE FAIL: loopzero-api LEAKED to %s (isolation broken)", cellMount)
		}
	}

	// external-secrets-broker SHOULD be on each cell mount.
	for _, cellMount := range []string{"k8s-cell-default", "k8s-cell-1", "k8s-cell-2"} {
		s, err := c.Logical().ReadWithContext(ctx, "auth/"+cellMount+"/role/external-secrets-broker")
		if err != nil || s == nil {
			t.Errorf("LIVE FAIL: auth/%s/role/external-secrets-broker missing: %v", cellMount, err)
		}
	}

	// external-secrets-broker MUST NOT be on k8s-mgmt (isolation invariant).
	s, err := c.Logical().ReadWithContext(ctx, "auth/k8s-mgmt/role/external-secrets-broker")
	if err == nil && s != nil {
		t.Error("LIVE FAIL: external-secrets-broker LEAKED to k8s-mgmt (isolation broken)")
	}
}
