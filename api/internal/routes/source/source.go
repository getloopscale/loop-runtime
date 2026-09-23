// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package source serves the bundle upload endpoint: the LoopZeroKit ↔ LoopZeroCloud
// seam, where the CLI hands over `.loopzero/services.json` plus the source bundle.
//
// The LAST family to leave the flat `routes` package (B-080), and with it the
// package itself: `api/internal/routes` no longer exists as Go code, only as the
// directory holding its families and leaves.
//
// # Why moving it needed a decision
//
// `CLAUDE.md` locks this file: any change to the LoopZeroKit ↔ LoopZeroCloud seam must be
// preceded by reading `docs/loopzerokit-loopzerocloud-seam.md`, which
// is the ONLY surviving copy of the contract — the LoopZeroKit-side mirror is gone.
//
// Read, the answer was that the contract is BEHAVIOURAL: `services_json` must be
// the first multipart field (else 400 MULTIPART_FIELD_ORDER), 256KB caps it (else
// 413), and the in-memory parse ceiling is 32MB. None of that depends on the
// package. What the move DID cost was the document, which cited `routes/source`
// in four places including inside its ASCII diagram — a diagram pointing at a
// path that no longer exists is worse than none, because it looks current. The
// four citations moved in the same commit.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-source-family-extraction
//
// # On the name
//
// This package and `api/internal/buildplane/source` (the services.json validator) share a
// name, which is not a conflict for the compiler but is one for a reader. Files
// importing both alias the ROUTES side as `sourceroutes`, following the tree's
// convention of suffixing whichever party is not the subject of the file.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/getloopscale/loopzero/api/internal/routes/httpio"

	"github.com/getloopscale/loopzero/api/internal/buildplane/source"
	"github.com/getloopscale/loopzero/api/internal/correlation"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// maxBundleSize is the maximum allowed bundle size (500MB).
const maxBundleSize int64 = 500 * 1024 * 1024

// maxServicesJSONSize caps the services_json part (Plan v1.2 T6.1, EC-4):
// 256 KB is realistic for 20+ services × 50 env vars, and a per-field cap
// under the body cap gives the client a specific code rather than a generic
// 413. The bundle is never held in memory — it is streamed to a temp file.
const maxServicesJSONSize int64 = 256 * 1024

// SourceUploadResponse is the JSON response for a successful source upload.
type SourceUploadResponse struct {
	CommitSHA string   `json:"commit_sha"`
	Apps      []string `json:"apps"`
}

// ObjectUploader uploads objects to S3-compatible storage (Spaces/MinIO).
type ObjectUploader interface {
	Upload(ctx context.Context, bucket, key string, data io.Reader) error
}

// SourceHandler provides HTTP handlers for source upload operations.
type SourceHandler struct {
	store        source.SourceStore
	objStore     ObjectUploader // optional: upload bundle to object storage for build cluster access
	sourceBucket string         // S3 bucket for source bundles (default: "loopzero-builds")
}

// NewSourceHandler creates a new SourceHandler.
func NewSourceHandler(store source.SourceStore, opts ...func(*SourceHandler)) *SourceHandler {
	h := &SourceHandler{store: store, sourceBucket: "loopzero-builds"}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithSourceBucket configures the S3 bucket name for source bundles.
func WithSourceBucket(bucket string) func(*SourceHandler) {
	return func(h *SourceHandler) {
		if bucket != "" {
			h.sourceBucket = bucket
		}
	}
}

// WithObjectUploader sets an optional object store for uploading bundles
// to S3-compatible storage (Spaces/MinIO) for build cluster access.
func WithObjectUploader(u ObjectUploader) func(*SourceHandler) {
	return func(h *SourceHandler) {
		h.objStore = u
	}
}

// Upload handles POST /api/v1/source/upload.
// Plan B v3.1 T-B.3 (2026-06-05): accepts a multipart form whose first part
// is "services_json" (file or text field) followed by "bundle" (file). The
// legacy "loopzero_yaml" field is rejected with actionable E-009 error per ADR D4
// + blueprint D8.
func (h *SourceHandler) Upload(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	// Check Content-Length before parsing body (fast reject for oversized requests).
	if r.ContentLength > maxBundleSize {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrPayloadTooLarge)
		return
	}

	// Limit the request body to maxBundleSize to prevent reading oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, maxBundleSize)

	// The wire contract (field order, per-field caps, schema) is enforced
	// while streaming, before the bundle is read: see readUploadForm.
	form, rej := readUploadForm(r)
	if rej != nil {
		if len(rej.fields) > 0 {
			httpio.WriteLoopZeroErrorWithFieldErrors(w, rej.err, rej.fields)
		} else {
			httpio.WriteLoopZeroError(w, rej.err)
		}
		return
	}
	defer form.close()
	manifest, tmpFile := form.manifest, form.bundle

	// Project name + sorted app names derived from the native manifest (T3.1).
	// ProjectNameFromManifest preserves the v1 "services-bundle" legacy fallback.
	projectName := source.ProjectNameFromManifest(manifest)
	appNames := source.AppNamesFromManifest(manifest)

	// Push bundle to source store (Gitea) — repo: loopzero-source-{tenant}-{project}
	result, err := h.store.PushBundle(ctx, uc.TenantID, projectName, tmpFile)
	if err != nil {
		// Plan v1.2 T1.2 (cutover-deep-review-hardening): DNS-1123 violation
		// on tenantID/projectID — surface as 400 with field-specific message
		// so the user can correct the input. Detected via errors.Is on the
		// sentinel exported by internal/source.
		if errors.Is(err, source.ErrInvalidTenantOrProject) {
			slog.Info("source upload rejected: invalid tenant or project ID",
				"team_id", uc.TenantID, "project", projectName, "error", err.Error())
			httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
				Code:    "INVALID_TENANT_OR_PROJECT",
				Message: err.Error(),
			})
			return
		}
		// Plan T2.1 (2026-05-14, F-03 fix): when concurrent deploys race on
		// the same project's source repo, gitea rejects the second push with
		// "cannot lock ref" / "remote rejected" / "failed to update ref".
		// This is the SAME root cause as a concurrent deploy already in
		// progress — emit DEPLOY_IN_PROGRESS so the user sees one consistent
		// error code instead of SOURCE_UPLOAD_FAILED + DEPLOY_IN_PROGRESS for
		// the same race. Distinct from real upload failures (network, MinIO
		// 503, malformed bundle) which keep SOURCE_UPLOAD_FAILED.
		//
		// EC-3 absorption: Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.3
		// preserves this discriminator UNCHANGED. Order matters: gitea-race
		// substring matching takes precedence OVER the new typed-error mapping
		// below, because PushBundle wraps gitea-race errors in a typed
		// *PushBundleError{Reason: ReasonRemoteRejectedPush} that we want to
		// surface as DEPLOY_IN_PROGRESS 409 (the F-03 fix) — NOT as
		// SOURCE_UPLOAD_FAILED 500. Calling source.IsGiteaRaceCondition(err)
		// works because *PushBundleError.Error() embeds the underlying cause
		// message; the substring set survives the typed wrap.
		if source.IsGiteaRaceCondition(err) {
			slog.Info("source upload race resolved as DEPLOY_IN_PROGRESS",
				"team_id", uc.TenantID, "project", projectName, "error", err.Error())
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrDeployInProgress)
			return
		}
		// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.3 (closes
		// loopzero-cloud#31): typed PushBundleError mapping — emit a specific
		// actionable Message per failure mode instead of the misleading
		// legacy "Ensure your git history is clean" string.
		if pushErr, ok := source.AsPushBundleError(err); ok {
			slog.Error("source upload failed (typed)",
				"team_id", uc.TenantID, "project", projectName,
				"reason", pushErr.Reason.String(), "detail", pushErr.Detail,
				"cause", err.Error())
			// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1: thread
			// the correlation ID so when Sprint 7 scrub collapses Detail
			// to placeholder-only, the operator gets "Reference: <id>.
			// Contact support if it persists." (closes #35 Red Team
			// original suggestion).
			httpio.WriteLoopZeroError(w, pushErr.UserError(correlation.Read(r)))
			return
		}
		// Fallback: untyped failure (downstream helper that didn't get
		// wrapped). Emit the neutral generic fallback per Sprint 4.
		slog.Error("source upload failed", "error", err, "team_id", uc.TenantID)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrSourceUploadFailed)
		return
	}

	// Clean up the git bundle temp dir if one was created (tar.gz flow).
	if result.GitBundlePath != "" {
		defer func() { _ = os.RemoveAll(filepath.Dir(result.GitBundlePath)) }()
	}

	// Upload bundle to object storage (Spaces/MinIO) for build cluster access.
	if h.objStore != nil {
		objKey := fmt.Sprintf("%s/source/%s.bundle", uc.TenantID, result.SourceID)

		// For tar.gz sources, PushBundle already created a git bundle (in GitBundlePath).
		// For git bundle sources, reuse the original uploaded bundle.
		var uploadReader io.Reader
		if result.GitBundlePath != "" {
			gitBundleFile, openErr := os.Open(result.GitBundlePath)
			if openErr != nil {
				httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
				return
			}
			defer func() { _ = gitBundleFile.Close() }()
			uploadReader = gitBundleFile
		} else {
			if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
				httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
				return
			}
			uploadReader = tmpFile
		}

		// CHG-2026-04-29-DOGFOOD iter 9 — when the object store is
		// configured the upload is part of the build contract: the
		// clone-repo step pulls the bundle from this exact key and a
		// missing object causes opaque pipeline failures. The previous
		// `non-fatal in dev` branch returned 200 to the CLI on upload
		// failure, then the workflow failed on bundle download —
		// silent-success is exactly what error-handling rules forbid.
		// Local must equal production (Rule 10): the upload either
		// succeeds or the request fails with ErrSourceUploadFailed.
		if uploadErr := h.objStore.Upload(ctx, h.sourceBucket, objKey, uploadReader); uploadErr != nil {
			slog.Error("object store upload failed", "error", uploadErr, "key", objKey)
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrSourceUploadFailed)
			return
		}
	}

	// Sort app names for deterministic output
	sort.Strings(appNames)

	resp := SourceUploadResponse{
		CommitSHA: result.SourceID,
		Apps:      appNames,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
