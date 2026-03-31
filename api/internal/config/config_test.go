// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"testing"
	"time"
)

func TestValidate_RejectsMaxConnsLessThanMinConns(t *testing.T) {
	cfg := Config{
		DBMaxConns: 5,
		DBMinConns: 10,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for DBMaxConns < DBMinConns, got nil")
	}
	if want := "DB_MAX_CONNS (5) must be >= DB_MIN_CONNS (10)"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestValidate_AcceptsValidPoolConfig(t *testing.T) {
	cfg := Config{
		DBMaxConns: 25,
		DBMinConns: 5,
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for valid pool config, got %v", err)
	}
}

func TestValidate_AcceptsEqualPoolConfig(t *testing.T) {
	cfg := Config{
		DBMaxConns: 10,
		DBMinConns: 10,
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for equal pool config, got %v", err)
	}
}

func TestValidate_AcceptsZeroPoolConfig(t *testing.T) {
	// Zero values should not trigger validation (unset config)
	cfg := Config{}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for zero config, got %v", err)
	}
}

func TestValidate_ProductionRejectsDefaultCredentials(t *testing.T) {
	cfg := Config{
		Env:            "production",
		MinIOAccessKey: "loopzero-admin",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for default MinIO credentials in production")
	}
}

func TestValidate_ProductionRejectsInsecureRegistry(t *testing.T) {
	cfg := Config{
		Env:              "production",
		RegistryInsecure: true,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for insecure registry in production")
	}
}

func TestValidate_WarnsReleasePendingLessThanSyncTimeout(t *testing.T) {
	// This should warn but not error (cross-field validation)
	cfg := Config{
		ReleasePendingTimeout: 1 * time.Minute,
		ArgocdSyncTimeout:     5 * time.Minute,
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected warning (not error) for timeout mismatch, got %v", err)
	}
}
