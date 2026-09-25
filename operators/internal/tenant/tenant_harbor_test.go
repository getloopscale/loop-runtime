// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

func TestBuildDockerConfigJSON(t *testing.T) {
	got, err := buildDockerConfigJSON("harbor.example.com", "robot$build-acme", "s3cr3t")
	if err != nil {
		t.Fatalf("buildDockerConfigJSON returned error: %v", err)
	}
	// Must be valid JSON
	if !json.Valid([]byte(got)) {
		t.Fatalf("buildDockerConfigJSON produced invalid JSON: %s", got)
	}
	// Verify structure
	var parsed harborDockerConfig
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("failed to unmarshal docker config: %v", err)
	}
	auth, ok := parsed.Auths["harbor.example.com"]
	if !ok {
		t.Fatal("expected auths entry for harbor.example.com")
	}
	if auth.Username != "robot$build-acme" {
		t.Errorf("username = %q, want %q", auth.Username, "robot$build-acme")
	}
	if auth.Password != "s3cr3t" {
		t.Errorf("password = %q, want %q", auth.Password, "s3cr3t")
	}
	wantAuth := base64.StdEncoding.EncodeToString([]byte("robot$build-acme:s3cr3t"))
	if auth.Auth != wantAuth {
		t.Errorf("auth = %q, want %q", auth.Auth, wantAuth)
	}
}

func TestBuildDockerConfigJSON_SpecialChars(t *testing.T) {
	// Verify JSON-unsafe characters are properly escaped by json.Marshal.
	specialCases := []struct {
		name     string
		password string
	}{
		{"quotes and backslash", `pass"word\slash`},
		{"newline and tab", "pass\nword\ttab"},
		{"carriage return", "pass\rword"},
		{"null byte", "pass\x00word"},
		{"unicode control", "pass\b\fword"},
	}
	for _, tc := range specialCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildDockerConfigJSON("harbor.example.com", "robot$test", tc.password)
			if err != nil {
				t.Fatalf("buildDockerConfigJSON returned error for password %q: %v", tc.password, err)
			}
			if !json.Valid([]byte(got)) {
				t.Fatalf("invalid JSON for password %q: %s", tc.password, got)
			}
			var parsed harborDockerConfig
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("unmarshal failed for password %q: %v\njson: %s", tc.password, err, got)
			}
			auth := parsed.Auths["harbor.example.com"]
			if auth.Password != tc.password {
				t.Errorf("password roundtrip failed: got %q, want %q", auth.Password, tc.password)
			}
		})
	}
}

// TestHarborOperations_NilClient_AreNoOps verifies that every Harbor-touching
// method on TenantReconciler short-circuits to nil error when HarborClient
// is nil. Phase 3 helper: was 4 near-clone test functions each setting up
// the same r=TenantReconciler{HarborClient: nil} + tenant and asserting
// no error. The TestSecretExists_NilClient_NoOp case in the original code
// actually exercised the same ensureHarborRobotAccounts path, so it's
// represented here as a comment rather than a duplicate entry.
func TestHarborOperations_NilClient_AreNoOps(t *testing.T) {
	type harborCall func(r *TenantReconciler, tenant *loopzerov1.Tenant) error
	cases := []struct {
		name string
		call harborCall
	}{
		{"ensureHarborProject", func(r *TenantReconciler, tenant *loopzerov1.Tenant) error {
			return r.ensureHarborProject(context.TODO(), tenant)
		}},
		{"ensureHarborRobotAccounts", func(r *TenantReconciler, tenant *loopzerov1.Tenant) error {
			return r.ensureHarborRobotAccounts(context.TODO(), tenant)
		}},
		{"deleteHarborProject", func(r *TenantReconciler, tenant *loopzerov1.Tenant) error {
			return r.deleteHarborProject(context.TODO(), tenant)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &TenantReconciler{HarborClient: nil}
			tenant := &loopzerov1.Tenant{}
			tenant.Name = "test-tenant"
			if err := c.call(r, tenant); err != nil {
				t.Errorf("expected nil error for nil HarborClient, got: %v", err)
			}
		})
	}
}
