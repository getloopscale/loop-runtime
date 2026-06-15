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
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/source"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
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
//
// Plan 2026-06-15-code-review-49-findings-remediation T5.4 (R2-F3 HIGH):
// CC 34 → ≤ 12 via 5 helper extracts (parseAndValidateMultipart +
// acceptServicesJSON + readServicesJSONField + streamBundleToTemp +
// pushBundleToStore + uploadBundleToObjStore). Behavior preserved verbatim.
func (h *SourceHandler) Upload(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if err := parseAndValidateMultipart(w, r); err != nil {
		writeLoopZeroError(w, err)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	cfg, appNames, err := acceptServicesJSON(w, r)
	if err != nil {
		writeLoopZeroError(w, err)
		return
	}

	tmpFile, terr := streamBundleToTemp(w, r)
	if terr != nil {
		writeLoopZeroError(w, terr)
		return
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	projectName := cfg.Project
	if projectName == "" {
		projectName = cfg.Name
	}

	result, perr := h.pushBundleToStore(ctx, uc.TenantID, projectName, tmpFile)
	if perr != nil {
		writeLoopZeroError(w, perr)
		return
	}

	// Clean up the git bundle temp dir if one was created (tar.gz flow).
	if result.GitBundlePath != "" {
		defer func() { _ = os.RemoveAll(filepath.Dir(result.GitBundlePath)) }()
	}

	if oerr := h.uploadBundleToObjStore(ctx, uc.TenantID, result, tmpFile); oerr != nil {
		writeLoopZeroError(w, oerr)
		return
	}

	sort.Strings(appNames)
	resp := SourceUploadResponse{
		CommitSHA: result.SourceID,
		Apps:      appNames,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// parseAndValidateMultipart performs the multipart pre-flight: Content-Length
// fast reject + body size cap + EC-5 first-field-name peek + ParseMultipartForm.
// Returns nil to continue OR a typed LoopZeroError to write.
func parseAndValidateMultipart(w http.ResponseWriter, r *http.Request) *loopzeroerrors.LoopZeroError {
	if r.ContentLength > maxBundleSize {
		return loopzeroerrors.ErrPayloadTooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBundleSize)
	// EC-5: services_json MUST be the first multipart part so the server
	// validates the manifest before accepting the bundle stream.
	if firstPartName, peekErr := peekFirstMultipartFieldName(r); peekErr == nil && firstPartName != "" {
		if firstPartName != "services_json" {
			return &loopzeroerrors.LoopZeroError{
				Code: "MULTIPART_FIELD_ORDER",
				Message: "multipart field order violation: expected `services_json` first, got `" + firstPartName +
					"`. Upgrade your LoopZeroKit CLI (≥0.5.0) — see https://loopzerocloud.dev/docs/api/source-upload-wire-protocol",
			}
		}
	}
	if err := r.ParseMultipartForm(maxInMemoryMultipart); err != nil {
		if err.Error() == "http: request body too large" {
			return loopzeroerrors.ErrPayloadTooLarge
		}
		return loopzeroerrors.ErrInvalidRequest
	}
	return nil
}

// acceptServicesJSON reads + validates the services_json field. Returns
// the parsed config + app names OR a typed LoopZeroError to write. Implements
// the EC-B3-7 legacy loopzero_yaml rejection + per-field 256KB cap.
func acceptServicesJSON(w http.ResponseWriter, r *http.Request) (*source.LoopZeroYAML, []string, *loopzeroerrors.LoopZeroError) {
	servicesJSONContent := readServicesJSONField(r)
	if int64(len(servicesJSONContent)) > maxServicesJSONSize {
		return nil, nil, &loopzeroerrors.LoopZeroError{
			Code: "SERVICES_JSON_TOO_LARGE",
			Message: fmt.Sprintf(
				"services.json field exceeds the %d KB cap. Reduce env var count / value length per `loopzerokit migrate services-json-v1-to-v2`.",
				maxServicesJSONSize/1024,
			),
		}
	}
	if servicesJSONContent == "" {
		if legacy := r.FormValue("loopzero_yaml"); legacy != "" {
			return nil, nil, loopzeroerrors.NewLoopZeroError("VALIDATION_FAILED",
				"E-009: legacy loopzero_yaml not supported. This API version requires `.loopzero/services.json` emitted by LoopZeroKit ≥0.5.0. "+
					"Upgrade LoopZeroKit and re-run `loopzerokit build --target loopzero-cloud` to regenerate the bundle.",
				422)
		}
		return nil, nil, loopzeroerrors.NewLoopZeroError("VALIDATION_FAILED",
			"E-008: services.json is required (multipart form field `services_json`).",
			422)
	}
	cfg, appNames, err := source.ValidateServicesJSON(servicesJSONContent)
	if err != nil {
		var ve *source.ValidationError
		if errors.As(err, &ve) {
			return nil, nil, loopzeroerrors.NewLoopZeroError("VALIDATION_FAILED", ve.Message, 422)
		}
		return nil, nil, loopzeroerrors.ErrInternalError
	}
	return cfg, appNames, nil
}

// readServicesJSONField returns the services_json content from either
// the text field OR the file upload path. Honors the per-field 256KB
// cap on the file path (text path is bounded by form-level 32MB).
func readServicesJSONField(r *http.Request) string {
	servicesJSONContent := r.FormValue("services_json")
	if servicesJSONContent != "" {
		return servicesJSONContent
	}
	jsonFile, _, err := r.FormFile("services_json")
	if err != nil {
		return ""
	}
	defer func() { _ = jsonFile.Close() }()
	jsonBytes, readErr := io.ReadAll(io.LimitReader(jsonFile, maxServicesJSONSize+1))
	if readErr != nil {
		return ""
	}
	return string(jsonBytes)
}

// streamBundleToTemp reads the multipart `bundle` field + writes once to
// a temp file (CODE-003 fix — avoids triple-buffering up to 1.5GB).
// Caller MUST close + os.Remove the returned file.
func streamBundleToTemp(w http.ResponseWriter, r *http.Request) (*os.File, *loopzeroerrors.LoopZeroError) {
	bundleFile, _, err := r.FormFile("bundle")
	if err != nil {
		return nil, loopzeroerrors.NewLoopZeroError("MISSING_FIELD", "Bundle file is required", 400)
	}
	defer func() { _ = bundleFile.Close() }()

	tmpFile, err := os.CreateTemp("", "loopzero-bundle-*.tar.gz")
	if err != nil {
		slog.Error("source upload: failed to create temp file", "error", err)
		return nil, loopzeroerrors.ErrInternalError
	}
	if _, err := io.Copy(tmpFile, bundleFile); err != nil {
		slog.Error("source upload: failed to write temp file", "error", err)
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return nil, loopzeroerrors.ErrInternalError
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return nil, loopzeroerrors.ErrInternalError
	}
	return tmpFile, nil
}

// pushBundleToStore wraps the SourceStore.PushBundle call with the
// canonical error categorization: ErrInvalidTenantOrProject (400) +
// concurrent-race detection (DEPLOY_IN_PROGRESS) + generic upload
// failure (ErrSourceUploadFailed).
func (h *SourceHandler) pushBundleToStore(
	ctx context.Context,
	tenantID, projectName string,
	tmpFile *os.File,
) (source.PushResult, *loopzeroerrors.LoopZeroError) {
	result, err := h.store.PushBundle(ctx, tenantID, projectName, tmpFile)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, source.ErrInvalidTenantOrProject) {
		slog.Info("source upload rejected: invalid tenant or project ID",
			"team_id", tenantID, "project", projectName, "error", err.Error())
		return source.PushResult{}, &loopzeroerrors.LoopZeroError{
			Code:    "INVALID_TENANT_OR_PROJECT",
			Message: err.Error(),
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "cannot lock ref") ||
		strings.Contains(msg, "remote rejected") ||
		strings.Contains(msg, "failed to update ref") ||
		strings.Contains(msg, "stale info") {
		slog.Info("source upload race resolved as DEPLOY_IN_PROGRESS",
			"team_id", tenantID, "project", projectName, "error", msg)
		return source.PushResult{}, loopzeroerrors.ErrDeployInProgress
	}
	slog.Error("source upload failed", "error", err, "team_id", tenantID)
	return source.PushResult{}, loopzeroerrors.ErrSourceUploadFailed
}

// uploadBundleToObjStore uploads the bundle to object storage (Spaces/
// MinIO) for build cluster access. No-op when objStore is unwired.
// Per CHG-2026-04-29-DOGFOOD iter 9, the upload is part of the build
// contract — silent-success is forbidden (Rule 10 local = production).
func (h *SourceHandler) uploadBundleToObjStore(
	ctx context.Context,
	tenantID string,
	result source.PushResult,
	tmpFile *os.File,
) *loopzeroerrors.LoopZeroError {
	if h.objStore == nil {
		return nil
	}
	objKey := fmt.Sprintf("%s/source/%s.bundle", tenantID, result.SourceID)

	var uploadReader io.Reader
	if result.GitBundlePath != "" {
		gitBundleFile, openErr := os.Open(result.GitBundlePath)
		if openErr != nil {
			return loopzeroerrors.ErrInternalError
		}
		defer func() { _ = gitBundleFile.Close() }()
		uploadReader = gitBundleFile
	} else {
		if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
			return loopzeroerrors.ErrInternalError
		}
		uploadReader = tmpFile
	}
	if uploadErr := h.objStore.Upload(ctx, h.sourceBucket, objKey, uploadReader); uploadErr != nil {
		slog.Error("object store upload failed", "error", uploadErr, "key", objKey)
		return loopzeroerrors.ErrSourceUploadFailed
	}
	return nil
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

