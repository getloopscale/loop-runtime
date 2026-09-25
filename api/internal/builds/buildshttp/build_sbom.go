// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildshttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// AppSBOMSummary is one entry in the /builds/:id/sbom response array.
type AppSBOMSummary struct {
	AppName        string `json:"app_name"`
	SBOMFormat     string `json:"sbom_format,omitempty"`
	ComponentCount int    `json:"component_count,omitempty"`
	GeneratedAt    string `json:"generated_at,omitempty"`
	DownloadURL    string `json:"download_url,omitempty"`
}

// BuildSBOMResponse is the JSON shape of GET /api/v1/builds/{id}/sbom.
//
// Stability contract per Rule 5: field names are stable; new fields can
// be appended but never renamed/removed without a major version bump.
type BuildSBOMResponse struct {
	BuildID string           `json:"build_id"`
	Apps    []AppSBOMSummary `json:"apps"`
}

// AppSBOMInfo mirrors store.AppAttestationInfo without coupling routes to db.
type AppSBOMInfo struct {
	AppName            string
	SBOMS3Key          string
	SBOMFormat         string
	SBOMComponentCount int
	SBOMGeneratedAt    string
	Signed             bool
}

// AppSBOMStore is the contract for reading per-app SBOM metadata.
// Implementations: db.PostgresAppAttestationStore.
type AppSBOMStore interface {
	GetBuildAppSBOMs(ctx context.Context, tenantID, buildID string) ([]AppSBOMInfo, error)
}

// SBOMURLGenerator builds presigned download URLs for SBOM blobs in S3.
// Implementations: s3.Uploader (Presign method).
type SBOMURLGenerator interface {
	Presign(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

// BuildSBOMHandler serves GET /api/v1/builds/:id/sbom.
//
// Sprint 6 / T1.2 — exposes per-app SBOM metadata + presigned download
// URLs. The SBOM blob itself lives in S3 (bucket: loopzero-static-assets);
// the DB only stores the key + counts.
type BuildSBOMHandler struct {
	store      AppSBOMStore
	urlSigner  SBOMURLGenerator
	bucketName string
	urlTTL     time.Duration
	logger     *slog.Logger
}

// NewBuildSBOMHandler wires the handler. urlSigner may be nil (no S3
// configured); in that case download_url is omitted from the response.
func NewBuildSBOMHandler(
	store AppSBOMStore,
	urlSigner SBOMURLGenerator,
	bucketName string,
	logger *slog.Logger,
) *BuildSBOMHandler {
	return &BuildSBOMHandler{
		store:      store,
		urlSigner:  urlSigner,
		bucketName: bucketName,
		urlTTL:     time.Hour, // D2: presigned URLs expire in 1h
		logger:     logger,
	}
}

// Get returns SBOM metadata + presigned URLs for all apps in the build.
//
// Tenant isolation: the tenant ID is read from middleware (request claims).
// Cross-tenant queries return 404 (no existence leak per ADR-0014).
//
// Empty result (no SBOMs yet generated) returns 404 — not an empty array,
// because /sbom on a build that has none is operationally indistinguishable
// from a not-found build (consistent with /attestation behavior).
func (h *BuildSBOMHandler) Get(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, `{"error":"build_id is required"}`, http.StatusBadRequest)
		return
	}

	uc := auth.GetUserContext(r.Context())
	if uc == nil || uc.TenantID == "" {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	tenantID := uc.TenantID

	infos, err := h.store.GetBuildAppSBOMs(r.Context(), tenantID, buildID)
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
			http.Error(w, `{"error":"build not found"}`, http.StatusNotFound)
			return
		}
		h.logger.Error("sbom lookup failed",
			"build_id", buildID,
			"tenant_id", tenantID,
			"error", err,
		)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Filter out apps without an SBOM (legacy releases, signing only).
	var apps []AppSBOMSummary
	for _, in := range infos {
		if in.SBOMS3Key == "" {
			continue
		}
		summary := AppSBOMSummary{
			AppName:        in.AppName,
			SBOMFormat:     in.SBOMFormat,
			ComponentCount: in.SBOMComponentCount,
			GeneratedAt:    in.SBOMGeneratedAt,
		}
		if h.urlSigner != nil && h.bucketName != "" {
			url, signErr := h.urlSigner.Presign(r.Context(), h.bucketName, in.SBOMS3Key, h.urlTTL)
			if signErr != nil {
				h.logger.Warn("sbom presign failed (returning metadata only)",
					"build_id", buildID,
					"app", in.AppName,
					"error", signErr,
				)
			} else {
				summary.DownloadURL = url
			}
		}
		apps = append(apps, summary)
	}

	if len(apps) == 0 {
		http.Error(w, `{"error":"no SBOM available for this build"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(BuildSBOMResponse{
		BuildID: buildID,
		Apps:    apps,
	})
}
