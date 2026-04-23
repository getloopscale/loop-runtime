// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"encoding/json"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
)

func TestHasStaticApps_OnlyWorkers(t *testing.T) {
	result := BuildCallbackRequest{
		Apps: []AppRelease{
			{Name: "bg", Type: "worker"},
			{Name: "cron", Type: "worker"},
		},
	}
	if hasStaticApps(result) {
		t.Error("expected false when only workers")
	}
}

func TestIsStaticRelease_EmptyArtifacts(t *testing.T) {
	r := &db.Release{Artifacts: json.RawMessage(`{}`)}
	if isStaticRelease(r) {
		t.Error("expected false for empty JSON object")
	}
}

func TestIsStaticRelease_DeliveryContainer(t *testing.T) {
	r := &db.Release{Artifacts: json.RawMessage(`{"delivery":"container"}`)}
	if isStaticRelease(r) {
		t.Error("expected false for container delivery")
	}
}

func TestParseArgoAppName_ValidSimple(t *testing.T) {
	tenant, env, ok := ParseArgoAppName("tn-loopzero-production")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tenant != "tn-loopzero" {
		t.Errorf("tenant = %q, want tn-loopzero", tenant)
	}
	if env != "production" {
		t.Errorf("env = %q, want production", env)
	}
}

func TestParseArgoAppName_MultiSegmentTenant(t *testing.T) {
	tenant, env, ok := ParseArgoAppName("tn-loopzero-e2e-staging")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tenant != "tn-loopzero-e2e" {
		t.Errorf("tenant = %q, want tn-loopzero-e2e", tenant)
	}
	if env != "staging" {
		t.Errorf("env = %q, want staging", env)
	}
}

func TestParseArgoAppName_NoHyphen(t *testing.T) {
	_, _, ok := ParseArgoAppName("nohyphen")
	if ok {
		t.Error("expected ok=false for no hyphen")
	}
}

func TestParseArgoAppName_EmptyString(t *testing.T) {
	_, _, ok := ParseArgoAppName("")
	if ok {
		t.Error("expected ok=false for empty string")
	}
}

func TestParseArgoAppName_TrailingHyphen(t *testing.T) {
	_, _, ok := ParseArgoAppName("tenant-")
	if ok {
		t.Error("expected ok=false for trailing hyphen")
	}
}

func TestParseArgoAppName_LeadingHyphen(t *testing.T) {
	_, _, ok := ParseArgoAppName("-env")
	if ok {
		t.Error("expected ok=false for leading hyphen")
	}
}

func TestErrActivationAlreadyHandled_Message(t *testing.T) {
	if ErrActivationAlreadyHandled == nil {
		t.Error("sentinel error should not be nil")
	}
	if ErrActivationAlreadyHandled.Error() == "" {
		t.Error("sentinel error should have message")
	}
}

func TestRollbackDeliveryRequest_Fields(t *testing.T) {
	req := RollbackDeliveryRequest{
		TenantID:      "tn-1",
		ProjectID:     "proj-1",
		ProjectName:   "my-app",
		Environment:   "production",
		TargetRelease: &db.Release{ID: "rel-old"},
		CorrelationID: "corr-123",
	}
	if req.TenantID != "tn-1" {
		t.Error("expected tn-1")
	}
	if req.TargetRelease.ID != "rel-old" {
		t.Error("expected rel-old")
	}
}
