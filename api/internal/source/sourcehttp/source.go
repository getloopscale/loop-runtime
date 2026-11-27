// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package sourcehttp serves the bundle upload endpoint: the LoopZeroKit ↔ LoopConsole
// seam, where the CLI hands over `.loopzero/services.json` plus the source bundle.
//
// The LAST family to leave the flat `routes` package (B-080), and with it the
// package itself: `api/internal/routes` no longer exists as Go code, only as the
// directory holding its families and leaves.
//
// # Why moving it needed a decision
//
// `CLAUDE.md` locks this file: any change to the LoopZeroKit ↔ LoopConsole seam must be
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
// This package and `api/internal/source` (the services.json validator) share a
// name, which is not a conflict for the compiler but is one for a reader. Files
// importing both alias the ROUTES side as `sourceroutes`, following the tree's
// convention of suffixing whichever party is not the subject of the file.
package sourcehttp

import (
	"context"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/getloopscale/loop-runtime/api/internal/correlation"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/source"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
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
	sourceBucket string         // S3 bucket for source bundles (default: "loop-runtime-builds")
	// envBlocklist is how a reserved services.json env key is handled. The
	// zero value enforces (fail-closed); WithEnvBlocklistMode sets it from
	// config.Config.EnvBlocklistMode.
	envBlocklist source.EnvBlocklistMode
}

// NewSourceHandler creates a new SourceHandler.
func NewSourceHandler(store source.SourceStore, opts ...func(*SourceHandler)) *SourceHandler {
	h := &SourceHandler{store: store, sourceBucket: "loop-runtime-builds"}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithEnvBlocklistMode sets how a services.json env key on the reserved
// blocklist is handled on upload (config.Config.EnvBlocklistMode).
func WithEnvBlocklistMode(mode source.EnvBlocklistMode) func(*SourceHandler) {
	return func(h *SourceHandler) {
		h.envBlocklist = mode
	}
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
// legacy "loop_runtime_yaml" field is rejected with actionable E-009 error per ADR D4
// + blueprint D8.
func (h *SourceHandler) Upload(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	// Check Content-Length before parsing body (fast reject for oversized requests).
	if r.ContentLength > maxBundleSize {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrPayloadTooLarge)
		return
	}

	// Limit the request body to maxBundleSize to prevent reading oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, maxBundleSize)

	// The wire contract (field order, per-field caps, schema) is enforced
	// while streaming, before the bundle is read: see readUploadForm.
	form, rej := readUploadForm(r, h.envBlocklist)
	if rej != nil {
		if len(rej.fields) > 0 {
			httpio.WriteLoopRuntimeErrorWithFieldErrors(w, rej.err, rej.fields)
		} else {
			httpio.WriteLoopRuntimeError(w, rej.err)
		}
		return
	}
	defer form.close()

	publisher := bundlePublisher{store: h.store, objects: h.objStore, bucket: h.sourceBucket}
	projectName := source.ProjectNameFromManifest(form.manifest)
	sourceID, perr := publisher.publish(ctx, uc.TenantID, projectName, form.bundle, correlation.Read(r))
	if perr != nil {
		httpio.WriteLoopRuntimeError(w, perr)
		return
	}

	appNames := source.AppNamesFromManifest(form.manifest)
	sort.Strings(appNames)
	httpio.WriteJSON(w, http.StatusOK, SourceUploadResponse{CommitSHA: sourceID, Apps: appNames})
}
