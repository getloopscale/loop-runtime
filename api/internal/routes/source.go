// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

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

	"github.com/getloopscale/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero/api/internal/source"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
)

// maxBundleSize is the maximum allowed bundle size (500MB).
const maxBundleSize int64 = 500 * 1024 * 1024

// Plan v1.2 T6.1 (H2 mitigation, EC-4 v1.1) — per-field size budgets.
// The legacy ParseMultipartForm path buffers 32MB in memory regardless of
// whether the payload is a tiny services.json or a multi-hundred-MB bundle;
// adding explicit per-field caps prevents a malicious or buggy client from
// driving the API into OOM territory while we ship the proper two-phase
// upload protocol (deferred to v4.1 per ADR D4).
const (
	maxServicesJSONSize int64 = 256 * 1024 // 256 KB — realistic for 20+ services × 50 env vars
	// Plan v1.2 T7.1 (L2) — extracted from the in-line `32 << 20` literal that
	// ParseMultipartForm used to receive. 32 MB is the in-memory threshold:
	// anything larger (up to maxBundleSize) is spooled to a temp file. The
	// value is a compromise between request latency (in-memory parsing is
	// faster) and heap pressure (large bundles must not stay in memory).
	maxInMemoryMultipart int64 = 32 << 20
)

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
// Plan B v3.1 T-B.3 (2026-06-05): accepts multipart form with "bundle" (file)
// and "services_json" (file or text field). The legacy "loopzero_yaml" field is
// rejected with actionable E-009 error per ADR D4 + blueprint D8.
func (h *SourceHandler) Upload(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	// Check Content-Length before parsing body (fast reject for oversized requests).
	if r.ContentLength > maxBundleSize {
		writeLoopZeroError(w, loopzeroerrors.ErrPayloadTooLarge)
		return
	}

	// Limit the request body to maxBundleSize to prevent reading oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, maxBundleSize)

	// Plan v1.2 T6.1 (EC-5) — wire-protocol contract: the first multipart
	// part MUST be `services_json` so the server can validate the manifest
	// before accepting the (potentially huge) bundle stream. We peek the
	// header via MultipartReader without consuming the body — ParseMultipartForm
	// below re-reads from the same request.
	if firstPartName, peekErr := peekFirstMultipartFieldName(r); peekErr == nil && firstPartName != "" {
		if firstPartName != "services_json" {
			writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
				Code: "MULTIPART_FIELD_ORDER",
				Message: "multipart field order violation: expected `services_json` first, got `" + firstPartName +
					"`. Upgrade your LoopZeroKit CLI (≥0.5.0) — see https://loopzerocloud.dev/docs/api/source-upload-wire-protocol",
			})
			return
		}
	}

	// Parse multipart form (up to maxInMemoryMultipart, rest goes to temp files).
	if err := r.ParseMultipartForm(maxInMemoryMultipart); err != nil {
		// MaxBytesReader returns a specific error when the body is too large.
		if err.Error() == "http: request body too large" {
			writeLoopZeroError(w, loopzeroerrors.ErrPayloadTooLarge)
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	// Plan B v3.1 T-B.3 (2026-06-05) — services_json is the canonical wire
	// field post LoopZeroKit-only cutover. Accept both text field (FormValue)
	// and file upload (FormFile). Legacy loopzero_yaml field is rejected with
	// actionable E-009 error per ADR D4 + blueprint D8.
	servicesJSONContent := r.FormValue("services_json")
	if servicesJSONContent == "" {
		// Fallback: try reading as file upload (how the CLI sends it)
		if jsonFile, _, err := r.FormFile("services_json"); err == nil {
			defer func() { _ = jsonFile.Close() }()
			// Plan v1.2 T6.1 — apply per-field 256KB cap on the file-upload
			// path. The text-field path is already bounded by 32MB form parse
			// + the explicit size check that follows. Read +1 to detect
			// truncation past the cap.
			jsonBytes, readErr := io.ReadAll(io.LimitReader(jsonFile, maxServicesJSONSize+1))
			if readErr == nil {
				servicesJSONContent = string(jsonBytes)
			}
		}
	}

	// Plan v1.2 T6.1 (EC-4) — explicit per-field size check on services_json.
	// Rejects payloads that would survive the form-level 32MB cap but blow up
	// downstream validators / log lines / DB columns.
	if int64(len(servicesJSONContent)) > maxServicesJSONSize {
		writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code: "SERVICES_JSON_TOO_LARGE",
			Message: fmt.Sprintf(
				"services.json field exceeds the %d KB cap. Reduce env var count / value length per `loopzerokit migrate services-json-v1-to-v2`.",
				maxServicesJSONSize/1024,
			),
		})
		return
	}

	// EC-B3-7 (v3.1): legacy loopzero_yaml rejection with actionable migration hint.
	// Uses VALIDATION_FAILED code so HTTP transport maps to 422 (E-009 + E-008
	// codes from the plan are surfaced em the message body — the registry-
	// driven status registry in pkg/errors/http only knows registered codes).
	if servicesJSONContent == "" {
		if legacy := r.FormValue("loopzero_yaml"); legacy != "" {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"VALIDATION_FAILED",
				"E-009: legacy loopzero_yaml not supported. This API version requires `.loopzero/services.json` emitted by LoopZeroKit ≥0.5.0. "+
					"Upgrade LoopZeroKit and re-run `loopzerokit build --target loopzero-cloud` to regenerate the bundle.",
				422,
			))
			return
		}
		// EC-B3-7 (v3.1) — truncated/missing payload returns 422 with E-008 hint.
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"VALIDATION_FAILED",
			"E-008: services.json is required (multipart form field `services_json`).",
			422,
		))
		return
	}

	// Validate services.json via the embedded JSON Schema 7 native validator.
	// T3.1 (plan simplify-deploy-loopzerokit-only v2.0): replaces the deprecated
	// *LoopZeroYAML bridge. The handler needs only project name + sorted app names,
	// both derived from the native manifest below.
	manifest, err := source.ValidateServicesJSONNative(servicesJSONContent)
	if err != nil {
		var ve *source.ValidationError
		if errors.As(err, &ve) {
			// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T2.3 (closes
			// loopzero-cloud#29 multi-field error aggregation): convert the
			// aggregated source.FieldError slice into the transport-layer
			// errorshttp.FieldError shape and surface ALL violations in the
			// response body's `errors[]` extension member (RFC 9457 § 3.2),
			// not just the first one. The legacy ve.Message scalar continues
			// to feed the `detail` field for backward-compat with consumers
			// that haven't migrated to the array.
			fieldErrors := make([]errorshttp.FieldError, 0, len(ve.Errors))
			for _, fe := range ve.Errors {
				fieldErrors = append(fieldErrors, errorshttp.FieldError{
					Field:    fe.Field,
					Message:  fe.Message,
					Expected: fe.Expected,
					Got:      fe.Got,
				})
			}
			writeLoopZeroErrorWithFieldErrors(w, loopzeroerrors.NewLoopZeroError(
				"VALIDATION_FAILED",
				ve.Message,
				422,
			), fieldErrors)
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Extract bundle file and stream to temp file (CODE-003 fix: avoids triple-buffering
	// up to 1.5GB per request by writing once to disk instead of io.ReadAll).
	bundleFile, _, err := r.FormFile("bundle")
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Bundle file is required",
			400,
		))
		return
	}
	defer func() { _ = bundleFile.Close() }()

	// Write bundle to temp file once — Gitea and S3 read from disk, not memory.
	tmpFile, err := os.CreateTemp("", "loopzero-bundle-*.tar.gz")
	if err != nil {
		slog.Error("source upload: failed to create temp file", "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	if _, err := io.Copy(tmpFile, bundleFile); err != nil {
		slog.Error("source upload: failed to write temp file", "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Project name + sorted app names derived from the native manifest (T3.1).
	// ProjectNameFromManifest preserves the v1 "services-bundle" legacy fallback.
	projectName := source.ProjectNameFromManifest(manifest)
	appNames := source.AppNamesFromManifest(manifest)

	// Rewind temp file for Gitea push
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

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
			writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
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
			writeLoopZeroError(w, loopzeroerrors.ErrDeployInProgress)
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
			writeLoopZeroError(w, loopzeroerrors.NewSourceUploadFailedFromPushReason(
				pushErr.Reason.String(), pushErr.Detail))
			return
		}
		// Fallback: untyped failure (downstream helper that didn't get
		// wrapped). Emit the neutral generic fallback per Sprint 4.
		slog.Error("source upload failed", "error", err, "team_id", uc.TenantID)
		writeLoopZeroError(w, loopzeroerrors.ErrSourceUploadFailed)
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
				writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
				return
			}
			defer func() { _ = gitBundleFile.Close() }()
			uploadReader = gitBundleFile
		} else {
			if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
				writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
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
			writeLoopZeroError(w, loopzeroerrors.ErrSourceUploadFailed)
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

// peekFirstMultipartFieldName clones the request body via http.Request.GetBody
// (set by net/http when the body is repeatable) and walks the first multipart
// part's header to retrieve the field name. When the body cannot be cloned
// (the typical streaming case), the function returns an empty string and a
// nil error — the contract is best-effort and silent failures are acceptable
// because ParseMultipartForm will still process the original stream.
//
// Plan v1.2 T6.1 (EC-5) — used to enforce the services_json-first wire
// contract documented in docs/api/source-upload-wire-protocol.md.
func peekFirstMultipartFieldName(r *http.Request) (string, error) {
	if r.GetBody == nil {
		// Request body isn't repeatable — accept the order silently and
		// rely on downstream validation. Returning ("", nil) means the
		// handler will treat the order as conformant.
		return "", nil
	}
	clone, err := r.GetBody()
	if err != nil {
		return "", err
	}
	defer func() { _ = clone.Close() }()

	// Cap the peek read at 16KB so a malicious client cannot drag the
	// process into reading the whole body twice.
	peekReader := io.LimitReader(clone, 16*1024)
	tmpReq := &http.Request{
		Header: r.Header.Clone(),
		Body:   io.NopCloser(peekReader),
	}
	multipartReader, mrErr := tmpReq.MultipartReader()
	if mrErr != nil {
		return "", nil
	}
	part, partErr := multipartReader.NextPart()
	if partErr != nil {
		return "", nil
	}
	defer func() { _ = part.Close() }()
	return part.FormName(), nil
}

