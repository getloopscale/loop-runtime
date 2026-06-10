// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	bao "github.com/openbao/openbao/api/v2"
)

// normalizeTTLToSeconds renders a Vault TTL value into its seconds-string form.
// Vault SDK returns TTLs as json.Number, float64, or formatted string
// ("1h"/"1h0m0s") depending on API version + endpoint. This helper accepts all
// three and emits a canonical decimal-seconds form for comparison.
func normalizeTTLToSeconds(v interface{}) string {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case float64:
		return fmt.Sprintf("%d", int64(t))
	case int64:
		return fmt.Sprintf("%d", t)
	case int:
		return fmt.Sprintf("%d", t)
	case string:
		// Could be "1h" or "3600" — return as-is; caller compares.
		return t
	}
	return fmt.Sprintf("%v", v)
}

// TestLIVE_DatabaseRole_LoopZeroAPI_TTLConfig verifies T6.3 LIVE — the loopzero-api
// database role exists with TTL 1h max 24h. This does NOT require a live PG
// connection (T6.2); only the ROLE config (creation_statements + ttl) is
// checked. T6.6 (PG audit log validation) requires operator-time DOKS PG.
func TestLIVE_DatabaseRole_LoopZeroAPI_TTLConfig(t *testing.T) {
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

	role, err := c.Logical().ReadWithContext(ctx, "database/roles/loopzero-api")
	if err != nil {
		t.Fatalf("LIVE FAIL: read database/roles/loopzero-api: %v", err)
	}
	if role == nil {
		t.Fatal("LIVE FAIL: loopzero-api role missing — T6.3 not provisioned")
	}

	wantDB := "postgres-loopzero-api"
	gotDB, _ := role.Data["db_name"].(string)
	if gotDB != wantDB {
		t.Errorf("LIVE FAIL: db_name=%q, want %q", gotDB, wantDB)
	}

	// Vault may return TTL as json.Number, float64, or string "1h" depending
	// on API version. Normalize via fmt + string compare for robustness.
	defaultTTLStr := normalizeTTLToSeconds(role.Data["default_ttl"])
	if defaultTTLStr != "3600" {
		t.Errorf("LIVE FAIL: default_ttl=%v, want 3600 (1h)", role.Data["default_ttl"])
	}
	maxTTLStr := normalizeTTLToSeconds(role.Data["max_ttl"])
	if maxTTLStr != "86400" {
		t.Errorf("LIVE FAIL: max_ttl=%v, want 86400 (24h)", role.Data["max_ttl"])
	}

	// Creation statements MUST grant loopzero_api_app role (T6.3 spec).
	creation, _ := role.Data["creation_statements"].([]interface{})
	if len(creation) == 0 {
		t.Fatal("LIVE FAIL: creation_statements empty")
	}
	stmtFound := false
	for _, raw := range creation {
		if s, ok := raw.(string); ok && contains(s, "loopzero_api_app") {
			stmtFound = true
		}
	}
	if !stmtFound {
		t.Error("LIVE FAIL: creation_statements does NOT include 'loopzero_api_app' grant")
	}
}
