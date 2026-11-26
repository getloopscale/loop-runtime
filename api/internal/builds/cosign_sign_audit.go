// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// T1.2 mitigation #3 of MEET-2026-05-12 launch readiness — per-sign
// audit log for cosign supply-chain transparency.
//
// Every container image that ships through LoopZero's build pipeline is signed
// by cosign (keyed in v1.0). Per the meeting's security veto resolution,
// each signing event MUST persist an `image.signed` row to audit_log so:
//   - Auditors can reconstruct who signed what + when, per tenant.
//   - SBOM + signature correlation is possible by joining audit_log on
//     build_id + image_digest.
//   - Detection of a missing audit row signals a pipeline regression
//     (e.g., cosign step skipped, signature missing).
//
// Wired in OnBuildCompleted after the build transitions to SUCCEEDED:
// for each app whose callback reports ImageDigestSHA256 + ImageSignature,
// we emit one audit entry. Fire-and-forget — audit-write failures log
// but never block deploy completion (EC-NEW-12 fail-open posture).

// emitCosignSignAudits emits one `image.signed` audit_log entry per app
// in result.Apps that has both a cryptographic digest AND a signature.
// Apps without supply-chain fields (legacy workflows, dev mode) are
// silently skipped — absence of audit row is itself the signal.
//
// Best-effort: errors are logged at WARN; the function never returns an
// error so it cannot be misused to block the build callback path.
func emitCosignSignAudits(ctx context.Context, audit ports.AuditLogger, build *buildAuditContext, apps []deploycontracts.AppRelease, correlationID string) {
	if audit == nil || build == nil {
		return
	}

	for _, app := range apps {
		// Skip apps without supply-chain data, such as a legacy build
		// (pre-Sprint-6) whose workflow declared no attestation outputs.
		if app.ImageDigestSHA256 == "" || app.ImageSignature == "" {
			continue
		}
		// A reused image was signed by the earlier build that produced it,
		// which emitted its audit entry then; this build signed nothing.
		if app.ImageReused {
			continue
		}

		signedAt := time.Now().UTC().Format(time.RFC3339)
		if app.ImageSignedAt != nil && !app.ImageSignedAt.IsZero() {
			signedAt = app.ImageSignedAt.UTC().Format(time.RFC3339)
		}

		metadata := map[string]any{
			"build_id":  build.BuildID,
			"app":       app.Name,
			"image_ref": app.ImageDigest, // tag (e.g., registry/org/app:commit)
			"digest":    app.ImageDigestSHA256,
			"signature": app.ImageSignature,
			"signed_at": signedAt,
		}
		if app.SBOMS3Key != "" {
			metadata["sbom_s3_key"] = app.SBOMS3Key
			metadata["sbom_format"] = app.SBOMFormat
		}

		err := audit.LogAudit(ctx,
			build.TenantID,
			"image.signed",
			build.CreatedBy,
			"image",
			app.ImageDigestSHA256, // resourceID = digest (immutable, searchable)
			correlationID,
			metadata,
		)
		if err != nil {
			// Fail-open: log but never bubble up. A missing audit row is
			// detectable via `count(audit_log{action='image.signed'})` rate
			// diverging from `count(builds{status='SUCCEEDED'})` — that
			// drift is the security signal, not the audit-write error.
			slog.Warn("cosign sign audit emission failed",
				"build_id", build.BuildID,
				"app", app.Name,
				"digest", app.ImageDigestSHA256,
				"error", err)
		}
	}
}

// buildAuditContext carries the build fields needed to construct an audit
// entry. Defined here (not inline) so OnBuildCompleted's call site stays
// readable.
type buildAuditContext struct {
	BuildID   string
	TenantID  string
	CreatedBy string
}
