package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"

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

// SourceHandler provides HTTP handlers for source upload operations.
type SourceHandler struct {
	store source.SourceStore
}

// NewSourceHandler creates a new SourceHandler.
func NewSourceHandler(store source.SourceStore) *SourceHandler {
	return &SourceHandler{store: store}
}

// Upload handles POST /api/v1/source/upload.
// Accepts multipart form with "bundle" (file) and "loopzero_yaml" (text field).
func (h *SourceHandler) Upload(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
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

	// Extract loopzero_yaml field
	loopzeroYAMLContent := r.FormValue("loopzero_yaml")
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

	// Extract bundle file
	bundleFile, _, err := r.FormFile("bundle")
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"bundle file is required",
			400,
		))
		return
	}
	defer bundleFile.Close()

	// Extract project name from loopzero.yaml (supports both 'project' and 'name' fields)
	projectName := cfg.Project
	if projectName == "" {
		projectName = cfg.Name
	}

	// Push bundle to source store — repo: loopzero-source-{tenant}-{project}
	commitSHA, err := h.store.PushBundle(r.Context(), uc.TenantID, projectName, bundleFile)
	if err != nil {
		slog.Error("source upload failed", "error", err, "tenant_id", uc.TenantID)
		writeLoopZeroError(w, loopzeroerrors.ErrSourceUploadFailed)
		return
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

// uploadErrorResponse is used to render validation errors with field context.
type uploadErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// writeUploadError writes an upload-specific error response.
func writeUploadError(w http.ResponseWriter, status int, code, message, field string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := uploadErrorResponse{
		Code:    code,
		Message: message,
	}
	if field != "" {
		resp.Field = field
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// parseContentLength extracts the Content-Length header as int64. Returns -1 if missing/invalid.
func parseContentLength(r *http.Request) int64 {
	cl := r.Header.Get("Content-Length")
	if cl == "" {
		return -1
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return -1
	}
	return n
}
