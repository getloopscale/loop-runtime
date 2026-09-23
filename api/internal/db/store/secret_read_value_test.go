// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// The broker External Secrets Operator calls reads one secret through the
// store, so it decrypts with the same dispatcher - and the same failure
// vocabulary and metrics - as every other secret read.
func TestReadValue_DecryptsTheScopedSecret(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-secret-read"
	seedGCTenant(t, ctx, pool, tenantID)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM secrets WHERE tenant_id=$1`, tenantID) })
	for _, row := range []struct{ env, value string }{
		{"production", "vault:v1:ct(prod-value)"},
		{"staging", "vault:v1:ct(staging-value)"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, tenant_id, project_id, environment, app, name, encrypted_value)
		       VALUES ($1,$2,'web',$3,'api','DB_URL',$4)`, "sec-"+row.env, tenantID, row.env, []byte(row.value)); err != nil {
			t.Fatalf("seed secret: %v", err)
		}
	}
	s, err := NewPostgresSecretStoreWithVault(&db.PgxAdapter{Pool: pool}, &mockVault{}, nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := s.ReadValue(ctx, tenantID, "web", "production", "DB_URL")
	if err != nil {
		t.Fatalf("ReadValue: %v", err)
	}
	if string(got) != "prod-value" {
		t.Errorf("value = %q, want the production row's plaintext", got)
	}
}

func TestReadValue_AbsentSecretIsNotFound(t *testing.T) {
	ctx, pool := meteringPool(t)
	s, err := NewPostgresSecretStoreWithVault(&db.PgxAdapter{Pool: pool}, &mockVault{}, nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	before := decryptFailuresTotal(t)
	if _, err := s.ReadValue(ctx, "tn-secret-absent", "web", "production", "NOPE"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
	if after := decryptFailuresTotal(t); after != before {
		t.Errorf("an absent secret moved the decrypt-failure counter by %v; it is not a decrypt failure", after-before)
	}
}
