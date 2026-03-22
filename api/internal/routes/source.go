package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
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
	store    source.SourceStore
	objStore ObjectUploader // optional: upload bundle to object storage for build cluster access
}

// NewSourceHandler creates a new SourceHandler.
func NewSourceHandler(store source.SourceStore, opts ...func(*SourceHandler)) *SourceHandler {
	h := &SourceHandler{store: store}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
		if ve, ok := err.(*source.ValidationError); ok {
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

	// Extract bundle file and buffer it (needed for dual-write to Gitea + Spaces)
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

	bundleData, err := io.ReadAll(bundleFile)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Extract project name from loopzero.yaml (supports both 'project' and 'name' fields)
	projectName := cfg.Project
	if projectName == "" {
		projectName = cfg.Name
	}

	// Push bundle to source store (Gitea) — repo: loopzero-source-{tenant}-{project}
	commitSHA, err := h.store.PushBundle(ctx, uc.TenantID, projectName, bytes.NewReader(bundleData))
	if err != nil {
		slog.Error("source upload failed", "error", err, "team_id", uc.TenantID)
		writeLoopZeroError(w, loopzeroerrors.ErrSourceUploadFailed)
		return
	}

	// Upload bundle to object storage (Spaces/MinIO) for build cluster access.
	// The CWT clone step downloads from: loopzero-builds/{tenant}/source/{build_id}.bundle
	// We use commit_sha as a temporary key; the build submission will copy/rename it.
	if h.objStore != nil {
		objKey := fmt.Sprintf("%s/source/%s.bundle", uc.TenantID, commitSHA)
		if uploadErr := h.objStore.Upload(ctx, "loopzero-builds", objKey, bytes.NewReader(bundleData)); uploadErr != nil {
			slog.Warn("object store upload failed (non-fatal)", "error", uploadErr, "key", objKey)
		}
	}

	// Sort app names for deterministic output
	sort.Strings(appNames)

	resp := SourceUploadResponse{
		CommitSHA: commitSHA,
		Apps:      appNames,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

