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
// Accepts multipart form with "bundle" (file) and "loopzero_yaml" (text field).
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

	// Parse multipart form (up to 32MB in memory, rest goes to temp files).
	if err := r.ParseMultipartForm(32 << 20); err != nil {
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

	// Extract loopzero_yaml — accept both text field (FormValue) and file upload (FormFile).
	// The CLI sends it as a file upload (Blob with filename), while curl/tests may
	// send it as a text field. Both are valid multipart encodings.
	loopzeroYAMLContent := r.FormValue("loopzero_yaml")
	if loopzeroYAMLContent == "" {
		// Fallback: try reading as file upload (how the CLI sends it)
		if yamlFile, _, err := r.FormFile("loopzero_yaml"); err == nil {
			defer func() { _ = yamlFile.Close() }()
			yamlBytes, readErr := io.ReadAll(yamlFile)
			if readErr == nil {
				loopzeroYAMLContent = string(yamlBytes)
			}
		}
	}
	if loopzeroYAMLContent == "" {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"VALIDATION_FAILED",
			"loopzero.yaml is required",
			422,
		))
		return
	}

	// Validate loopzero.yaml
	cfg, appNames, err := source.ValidateLoopZeroYAML(loopzeroYAMLContent)
	if err != nil {
		var ve *source.ValidationError
		if errors.As(err, &ve) {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"VALIDATION_FAILED",
				ve.Message,
				422,
			))
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

	// Extract project name from loopzero.yaml (supports both 'project' and 'name' fields)
	projectName := cfg.Project
	if projectName == "" {
		projectName = cfg.Name
	}

	// Rewind temp file for Gitea push
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Push bundle to source store (Gitea) — repo: loopzero-source-{tenant}-{project}
	result, err := h.store.PushBundle(ctx, uc.TenantID, projectName, tmpFile)
	if err != nil {
		// Plan T2.1 (2026-05-14, F-03 fix): when concurrent deploys race on
		// the same project's source repo, gitea rejects the second push with
		// "cannot lock ref" / "remote rejected" / "failed to update ref".
		// This is the SAME root cause as a concurrent deploy already in
		// progress — emit DEPLOY_IN_PROGRESS so the user sees one consistent
		// error code instead of SOURCE_UPLOAD_FAILED + DEPLOY_IN_PROGRESS for
		// the same race. Distinct from real upload failures (network, MinIO
		// 503, malformed bundle) which keep SOURCE_UPLOAD_FAILED.
		msg := err.Error()
		if strings.Contains(msg, "cannot lock ref") ||
			strings.Contains(msg, "remote rejected") ||
			strings.Contains(msg, "failed to update ref") ||
			strings.Contains(msg, "stale info") {
			slog.Info("source upload race resolved as DEPLOY_IN_PROGRESS",
				"team_id", uc.TenantID, "project", projectName, "error", msg)
			writeLoopZeroError(w, loopzeroerrors.ErrDeployInProgress)
			return
		}
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

