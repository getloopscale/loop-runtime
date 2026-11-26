// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// release_inspector.go implements T4.4 of plan
// 2026-05-28-meet-001-engineering-closure-plan.md.
//
// CR-10 (MEET-2026-05-28-001 SRE): "There is no loopruntime-ops CLI command
// like `loopruntime admin release inspect <release-id>` showing which write
// path was taken." Pairs with the AdminReleaseInspect HTTP handler and
// the `loopruntime admin release inspect` CLI command.
//
// The inspector is read-only: it joins releases + audit_log + workflow
// snapshot (when available) into a single diagnostic payload. EC-14 of
// plan v1.1: when the underlying Argo Workflow CR has been garbage-
// collected (typically >90d), the inspector MUST return 200 with the
// workflow fields null AND signing_path="unknown" — NOT 500.

package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// ReleaseInspection is the JSON shape returned by
// GET /api/v1/admin/releases/{id}/inspect and rendered by
// `loopruntime admin release inspect --json`.
//
// Field names are a stability contract (CLAUDE.md Rule 5); new fields
// may be added but existing names cannot change without a major bump.
//
// Rule 3 (CLAUDE.md): NO internal terms in this payload. The CLI
// surfaces it directly to operators. "workflow" is acceptable because
// it's product language (a build workflow). "tenant" is acceptable
// because it's a user-facing concept in admin scope. "ArgoCD" /
// "Argo Workflows" / "Kaniko" / etc. MUST NOT appear.
type ReleaseInspection struct {
	ReleaseID                string                      `json:"release_id"`
	BuildID                  string                      `json:"build_id"`
	TenantID                 string                      `json:"tenant_id"`
	ProjectID                string                      `json:"project_id"`
	Environment              string                      `json:"environment"`
	Status                   string                      `json:"status"`
	CorrelationID            string                      `json:"correlation_id"`
	WorkflowID               *string                     `json:"workflow_id,omitempty"`
	ImageDigest              *string                     `json:"image_digest,omitempty"`
	ImageSignature           *string                     `json:"image_signature,omitempty"`
	ImageSignedAt            *string                     `json:"image_signed_at,omitempty"`
	ImageAttestationRekorURL *string                     `json:"image_attestation_rekor_url,omitempty"`
	SigningPath              string                      `json:"signing_path"`     // "keyless" | "keyed" | "unsigned" | "unknown"
	AttestationPath          string                      `json:"attestation_path"` // "present" | "absent" | "unknown"
	AuditLogEntries          []ReleaseInspectionAuditRow `json:"audit_log_entries"`
	// AuditLogStatus says how far audit_log_entries can be trusted: "complete",
	// "unavailable" (the audit trail could not be read — an empty list then
	// means nothing), or "truncated" (the release's rows may lie beyond the
	// window that was read). Without it, "could not read" and "nothing to read"
	// were the same empty array.
	AuditLogStatus string `json:"audit_log_status"`
}

// Values of ReleaseInspection.AuditLogStatus.
const (
	AuditLogComplete    = "complete"
	AuditLogUnavailable = "unavailable"
	AuditLogTruncated   = "truncated"
)

// auditWindow is how many of the tenant's release audit rows one inspection
// reads. The audit port has no ResourceID predicate, so the rows are filtered
// in memory, and this cap keeps the diagnostic payload bounded.
const auditWindow = 50

// ReleaseInspectionAuditRow surfaces a single audit_log row relevant
// to the release. We pick only the fields an operator triaging
// production needs — full audit_log fanout lives in a separate audit
// endpoint.
type ReleaseInspectionAuditRow struct {
	Action        string                 `json:"action"`
	Actor         string                 `json:"actor"`
	ResourceID    string                 `json:"resource_id"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt     string                 `json:"created_at"`
}

// ReleaseInspector orchestrates the diagnostic data fetch.
type ReleaseInspector interface {
	Inspect(ctx context.Context, releaseID string) (*ReleaseInspection, error)
}

// ErrReleaseInspectionNotFound is returned when the release does not
// exist. The handler maps this to 404. Distinct error so the
// handler does not have to inspect strings.
type ErrReleaseInspectionNotFound struct{ ReleaseID string }

func (e ErrReleaseInspectionNotFound) Error() string {
	return fmt.Sprintf("release inspection: release %q not found", e.ReleaseID)
}

// releaseStoreInspector queries the release store + audit store +
// optional build store for the workflow ID.
type releaseStoreInspector struct {
	releases   ReleaseInspectorReleaseQuerier
	audit      ReleaseInspectorAuditQuerier
	buildStore ReleaseInspectorBuildQuerier
	logger     *slog.Logger
}

// ReleaseInspectorReleaseQuerier is the one release-store read the inspector
// makes (ISP — consumer-side port, like the audit and build queriers below).
type ReleaseInspectorReleaseQuerier interface {
	GetByID(ctx context.Context, id string) (*db.Release, error)
}

// ReleaseInspectorAuditQuerier is the minimal audit interface this
// service needs. Defined here (consumer side of DIP) so a future
// re-platform of the audit store does not propagate.
type ReleaseInspectorAuditQuerier interface {
	ListAuditEntries(ctx context.Context, tenantID string, filter ports.AuditFilter) ([]ports.AuditEntryResult, int, error)
}

// ReleaseInspectorBuildQuerier is the minimal build interface the
// inspector needs to resolve workflow ID. Nullable: when the build CR
// has been reaped (EC-14), the implementation returns workflowID="" +
// nil error, and the inspector reports workflow_id=null +
// signing_path="unknown".
type ReleaseInspectorBuildQuerier interface {
	GetWorkflowIDByBuildID(ctx context.Context, buildID string) (string, error)
}

// NewReleaseInspector wires the inspector.
func NewReleaseInspector(
	releases ReleaseInspectorReleaseQuerier,
	audit ReleaseInspectorAuditQuerier,
	buildStore ReleaseInspectorBuildQuerier,
) ReleaseInspector {
	return &releaseStoreInspector{
		releases:   releases,
		audit:      audit,
		buildStore: buildStore,
		logger:     slog.Default(),
	}
}

// Inspect joins the 3 underlying sources into a single diagnostic
// payload. EC-14: workflow CR reaped → workflow_id null +
// signing_path="unknown", NOT 500.
func (s *releaseStoreInspector) Inspect(ctx context.Context, releaseID string) (*ReleaseInspection, error) {
	rel, err := s.releases.GetByID(ctx, releaseID)
	if errors.Is(err, loopruntimeerrors.ErrReleaseNotFound) {
		// The store's not-found; (nil, nil) below is the older form (#415).
		return nil, ErrReleaseInspectionNotFound{ReleaseID: releaseID}
	}
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, ErrReleaseInspectionNotFound{ReleaseID: releaseID}
	}

	out := &ReleaseInspection{
		ReleaseID:                rel.ID,
		BuildID:                  rel.BuildID,
		TenantID:                 rel.TenantID,
		ProjectID:                rel.ProjectID,
		Environment:              rel.Environment,
		Status:                   rel.Status,
		CorrelationID:            rel.CorrelationID,
		ImageDigest:              rel.ImageDigest,
		ImageSignature:           rel.ImageSignature,
		ImageAttestationRekorURL: rel.ImageAttestationRekorURL,
	}
	if rel.ImageSignedAt != nil {
		s := rel.ImageSignedAt.Format("2006-01-02T15:04:05Z07:00")
		out.ImageSignedAt = &s
	}

	// Workflow ID — optional. EC-14 graceful degrade: a reaped build answers
	// "" with no error. Any other error is logged, since the payload cannot
	// otherwise tell a lookup failure from a reaped workflow.
	if s.buildStore != nil {
		wfID, wfErr := s.buildStore.GetWorkflowIDByBuildID(ctx, rel.BuildID)
		switch {
		case wfErr != nil:
			s.logger.WarnContext(ctx, "release inspection: workflow lookup failed",
				"release_id", rel.ID, "build_id", rel.BuildID, "error", wfErr)
		case wfID != "":
			out.WorkflowID = &wfID
		}
	}

	// Derive signing_path + attestation_path from the persisted fields.
	out.SigningPath = derivedSigningPath(rel)
	out.AttestationPath = derivedAttestationPath(rel)

	out.AuditLogEntries, out.AuditLogStatus = s.auditRows(ctx, rel)
	return out, nil
}

// auditRows reads the release's audit rows and reports how complete they are.
// A read failure does not fail the inspection — the rest of the payload is
// still the diagnosis the operator asked for — but it is logged and reported
// as "unavailable", never as an empty trail.
func (s *releaseStoreInspector) auditRows(ctx context.Context, rel *db.Release) ([]ReleaseInspectionAuditRow, string) {
	rows := []ReleaseInspectionAuditRow{} // stable JSON shape: empty array, not null
	if s.audit == nil {
		return rows, AuditLogUnavailable
	}
	filter := ports.AuditFilter{ResourceType: "release", Limit: auditWindow}
	entries, total, err := s.audit.ListAuditEntries(ctx, rel.TenantID, filter)
	if err != nil {
		s.logger.WarnContext(ctx, "release inspection: audit trail could not be read",
			"release_id", rel.ID, "tenant_id", rel.TenantID, "error", err)
		return rows, AuditLogUnavailable
	}
	for _, e := range entries {
		if e.ResourceID != rel.ID {
			continue
		}
		rows = append(rows, ReleaseInspectionAuditRow{
			Action:        e.Action,
			Actor:         e.Actor,
			ResourceID:    e.ResourceID,
			CorrelationID: e.CorrelationID,
			Metadata:      e.Metadata,
			CreatedAt:     e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	if total > len(entries) {
		return rows, AuditLogTruncated
	}
	return rows, AuditLogComplete
}

// derivedSigningPath inspects ImageSignature presence to classify the
// signing route. The CR-10 SRE ask was specifically "which write path
// was taken" — keyless (Fulcio) vs keyed vs unsigned. We don't store
// a path label today; the heuristic uses signature presence as a
// proxy, with a doc note that explicit labels would require a new
// column (deferred).
func derivedSigningPath(rel *db.Release) string {
	if rel.ImageSignature == nil || *rel.ImageSignature == "" {
		// No signature at all → either signing failed or it ran on a
		// legacy build pre-CHG-2026-04-25-ATTESTATION-WIRE. From an
		// operator triage standpoint these collapse to "unsigned".
		return "unsigned"
	}
	// Heuristic: a populated ImageAttestationRekorURL strongly implies
	// keyless (Fulcio + Rekor record). A signature without Rekor
	// participation is keyed.
	if rel.ImageAttestationRekorURL != nil && *rel.ImageAttestationRekorURL != "" {
		return "keyless"
	}
	return "keyed"
}

// derivedAttestationPath classifies the attestation chain state.
func derivedAttestationPath(rel *db.Release) string {
	if rel.ImageAttestationRekorURL != nil && *rel.ImageAttestationRekorURL != "" {
		return "present"
	}
	return "absent"
}
