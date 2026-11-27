// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// captureAuditLogger records LogAudit calls for assertion.
type captureAuditLogger struct {
	entries []capturedAudit
	err     error
}

type capturedAudit struct {
	tenantID      string
	action        string
	actor         string
	resourceType  string
	resourceID    string
	correlationID string
	metadata      map[string]any
}

func (c *captureAuditLogger) LogAudit(_ context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error {
	if c.err != nil {
		return c.err
	}
	c.entries = append(c.entries, capturedAudit{
		tenantID:      tenantID,
		action:        action,
		actor:         actor,
		resourceType:  resourceType,
		resourceID:    resourceID,
		correlationID: correlationID,
		metadata:      metadata,
	})
	return nil
}

// -----------------------------------------------------------------------------
// emitCosignSignAudits
// -----------------------------------------------------------------------------

func TestEmitCosignSignAudits_NilAuditor_NoOp(t *testing.T) {
	// Must not panic. Caller should never know whether auditor was wired.
	emitCosignSignAudits(context.Background(), nil, &buildAuditContext{}, []deploycontracts.AppRelease{}, "")
}

func TestEmitCosignSignAudits_EmptyApps_NoEntries(t *testing.T) {
	audit := &captureAuditLogger{}
	emitCosignSignAudits(context.Background(), audit, &buildAuditContext{
		BuildID:   "bld-1",
		TenantID:  "tn-1",
		CreatedBy: "u-1",
	}, []deploycontracts.AppRelease{}, "corr-1")

	if len(audit.entries) != 0 {
		t.Errorf("want 0 entries for empty apps, got %d", len(audit.entries))
	}
}

func TestEmitCosignSignAudits_EmitsOnePerSignedApp(t *testing.T) {
	audit := &captureAuditLogger{}
	now := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)

	apps := []deploycontracts.AppRelease{
		{
			Name:              "api",
			ImageDigest:       "registry/org/api:abc",
			ImageDigestSHA256: "sha256:1111",
			ImageSignature:    "sig-api",
			ImageSignedAt:     &now,
			SBOMS3Key:         "sbom/api-abc.json",
			SBOMFormat:        "cyclonedx-json",
		},
		{
			Name:              "worker",
			ImageDigest:       "registry/org/worker:abc",
			ImageDigestSHA256: "sha256:2222",
			ImageSignature:    "sig-worker",
		},
	}

	emitCosignSignAudits(context.Background(), audit, &buildAuditContext{
		BuildID:   "bld-x",
		TenantID:  "tn-x",
		CreatedBy: "u-x",
	}, apps, "corr-x")

	if len(audit.entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(audit.entries))
	}

	// Verify first entry (api)
	e := audit.entries[0]
	if e.action != "image.signed" {
		t.Errorf("action = %q, want image.signed", e.action)
	}
	if e.tenantID != "tn-x" {
		t.Errorf("tenantID = %q, want tn-x", e.tenantID)
	}
	if e.actor != "u-x" {
		t.Errorf("actor = %q, want u-x", e.actor)
	}
	if e.resourceType != "image" {
		t.Errorf("resourceType = %q, want image", e.resourceType)
	}
	if e.resourceID != "sha256:1111" {
		t.Errorf("resourceID = %q, want sha256:1111 (digest used as canonical id)", e.resourceID)
	}
	if e.correlationID != "corr-x" {
		t.Errorf("correlationID = %q, want corr-x", e.correlationID)
	}
	if e.metadata["digest"] != "sha256:1111" {
		t.Errorf("metadata.digest = %v, want sha256:1111", e.metadata["digest"])
	}
	if e.metadata["signature"] != "sig-api" {
		t.Errorf("metadata.signature = %v, want sig-api", e.metadata["signature"])
	}
	if e.metadata["app"] != "api" {
		t.Errorf("metadata.app = %v, want api", e.metadata["app"])
	}
	if e.metadata["build_id"] != "bld-x" {
		t.Errorf("metadata.build_id = %v, want bld-x", e.metadata["build_id"])
	}
	if e.metadata["sbom_s3_key"] != "sbom/api-abc.json" {
		t.Errorf("metadata.sbom_s3_key missing/wrong: %v", e.metadata["sbom_s3_key"])
	}
	if e.metadata["signed_at"] != "2026-05-12T14:00:00Z" {
		t.Errorf("metadata.signed_at = %v, want explicit RFC3339 timestamp from ImageSignedAt",
			e.metadata["signed_at"])
	}
}

func TestEmitCosignSignAudits_SkipsAppsWithoutSupplyChain(t *testing.T) {
	audit := &captureAuditLogger{}
	apps := []deploycontracts.AppRelease{
		// No signature → skip
		{Name: "static-frontend", ImageDigestSHA256: "sha256:abc"},
		// No digest → skip
		{Name: "legacy", ImageSignature: "sig"},
		// Both present → emit
		{Name: "api", ImageDigestSHA256: "sha256:def", ImageSignature: "sig-api"},
	}

	emitCosignSignAudits(context.Background(), audit, &buildAuditContext{
		BuildID: "bld", TenantID: "tn", CreatedBy: "u",
	}, apps, "")

	if len(audit.entries) != 1 {
		t.Fatalf("want 1 entry (only fully-signed app), got %d", len(audit.entries))
	}
	if audit.entries[0].metadata["app"] != "api" {
		t.Errorf("emitted for wrong app: %v", audit.entries[0].metadata["app"])
	}
}

func TestEmitCosignSignAudits_FailOpenOnLogError(t *testing.T) {
	// Auditor returns error — function must NOT panic, NOT return error,
	// just log internally.
	audit := &captureAuditLogger{err: errors.New("DB connection refused")}
	apps := []deploycontracts.AppRelease{
		{Name: "api", ImageDigestSHA256: "sha256:def", ImageSignature: "sig-api"},
	}

	// If this panics or otherwise misbehaves, the test framework reports it.
	emitCosignSignAudits(context.Background(), audit, &buildAuditContext{
		BuildID: "bld", TenantID: "tn", CreatedBy: "u",
	}, apps, "")

	// captureAuditLogger.err short-circuits before appending; entries empty
	// is the expected state. The signal is: function returned without
	// panicking and without surfacing the error to the caller.
	if len(audit.entries) != 0 {
		t.Errorf("captureAuditLogger should short-circuit on err, got %d entries",
			len(audit.entries))
	}
}

func TestEmitCosignSignAudits_NilBuildContext_NoPanic(t *testing.T) {
	audit := &captureAuditLogger{}
	emitCosignSignAudits(context.Background(), audit, nil, []deploycontracts.AppRelease{
		{Name: "api", ImageDigestSHA256: "sha256:def", ImageSignature: "sig"},
	}, "")
	// Pass: function returned without panicking. No assertion on entries
	// because behavior is "do nothing" — important is that the caller
	// can never crash the build by passing a nil context.
}

func TestEmitCosignSignAudits_DefaultsSignedAtToNow_WhenAppHasNoTimestamp(t *testing.T) {
	audit := &captureAuditLogger{}
	apps := []deploycontracts.AppRelease{
		{Name: "api", ImageDigestSHA256: "sha256:def", ImageSignature: "sig"},
		// No ImageSignedAt set
	}
	emitCosignSignAudits(context.Background(), audit, &buildAuditContext{
		BuildID: "bld", TenantID: "tn", CreatedBy: "u",
	}, apps, "")

	if len(audit.entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(audit.entries))
	}
	signedAt, _ := audit.entries[0].metadata["signed_at"].(string)
	if signedAt == "" {
		t.Error("signed_at must default to a timestamp string, got empty")
	}
	// Loose check: parses as RFC3339 (no need to compare with real wall clock).
	if _, err := time.Parse(time.RFC3339, signedAt); err != nil {
		t.Errorf("signed_at = %q, not RFC3339: %v", signedAt, err)
	}
}
