// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/correlation"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	SecretMetadata = domain.SecretMetadata
	SecretStore    = domain.SecretStore
	SecretK8sSync  = domain.SecretK8sSync
	AuditLogger    = domain.AuditLogger
)

// SecretEnvLocker is the narrow interface SecretHandler needs to wrap
// mutations in `pg_advisory_xact_lock` (T1.1 of deep-review-2026-05-10-
// remediation-plan). Defined locally per ISP — accepts any locker the
// services layer exposes, including `shared.EnvironmentLocker` and
// `adapters.PoolEnvironmentLocker`.
//
// Nil-safe: when handler.locker is nil, Set/DeleteSecret fall back to
// the legacy non-locked path. Production wiring must inject the real
// locker; this fallback keeps unit-test fakes simple.
type SecretEnvLocker interface {
	WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error
}

// SecretTransit — narrow interface (ISP) for the OpenBao transit encryption
// adapter consumed by the Bulk write path (T2.3 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2).
//
// When wired, Bulk MUST encrypt every entry.Value via Encrypt BEFORE
// persisting to PG so the secrets.encrypted_value BYTEA column never
// stores plaintext. Tenant-scoped (per-tenant transit key per D4 ADR).
//
// Production injects *adapters.VaultTransitAdapter (which wraps
// *pkg/vault.TransitClient). Nil-safe: when transitClient is nil, Bulk
// falls back to the legacy path where the existing AES key in
// secret_wrapper.go encrypts at the store layer (legacy/test path).
type SecretTransit interface {
	Encrypt(ctx context.Context, tenantID string, plaintext []byte) (ciphertext string, err error)
}

// SecretProjectChecker — narrow interface (ISP) for the tenant-scoped
// project existence check. T1.2 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2: Bulk MUST resolve the
// owning tenant of the requested project BEFORE invoking store.Set or
// k8sSync so a JWT carrying tenant_id=tn-X cannot trick the API into
// encrypting tenant_id=tn-Y's value with tn-X's transit key.
//
// EC-03 absorption: the tenant-scoped GetByName returns nil for both
// "cross-tenant attempt" and "genuinely-nonexistent" paths — making the
// 404 response byte-identical across both. This is the defense against
// observable response discrepancy (CWE-204 / existence-leak).
//
// Production wires the existing *store.PostgresProjectStore via the same
// `&store.PostgresProjectStore{DB:...}` shape (per project_types.go
// constructor convention) — a project-checker adapter is NOT needed
// because PostgresProjectStore.GetByName already matches this signature.
type SecretProjectChecker interface {
	GetByName(ctx context.Context, tenantID, name string) (*domain.ProjectResult, error)
}

// SecretPlanLimiter — narrow interface (ISP) for the plan-limit gate.
// Production wires *enforcement.PlanLimiter via an adapter; tests pass a
// fake. Returns:
//   - allowed: false when adding `adding` secrets would exceed the cap
//   - current: current secret count for the tenant
//   - max: hard limit per the tenant's plan
//   - plan: tenant's plan name ("free" / "pro" / "team" / "enterprise")
//
// J-22 T1.3.
type SecretPlanLimiter interface {
	IsAllowedToCreate(ctx context.Context, tenantID string, adding int) (allowed bool, current int, max int, plan string)
}

// SecretHandler provides HTTP handlers for secret operations.
type SecretHandler struct {
	store          SecretStore
	k8sSync        SecretK8sSync        // optional — nil disables K8s sync
	audit          AuditLogger          // optional — nil disables audit logging
	locker         SecretEnvLocker      // optional — nil disables environment lock (legacy path)
	planLimits     SecretPlanLimiter    // optional — nil disables plan-limit gate (legacy/test path)
	projectChecker SecretProjectChecker // optional — nil disables T1.2 cross-tenant binding (legacy/test path)
	transitClient  SecretTransit        // optional — nil disables T2.3 transit encrypt (legacy AES path)
}

// NewSecretHandler creates a new SecretHandler.
func NewSecretHandler(store SecretStore) *SecretHandler {
	return &SecretHandler{store: store}
}

// SetAuditLogger configures audit logging for sensitive operations.
func (h *SecretHandler) SetAuditLogger(audit AuditLogger) {
	h.audit = audit
}

// SetK8sSync configures the K8s sync for creating Secrets and ExternalSecret CRs.
func (h *SecretHandler) SetK8sSync(sync SecretK8sSync) {
	h.k8sSync = sync
}

// SetLocker configures the environment lock for secret mutations. When set,
// Set/DeleteSecret acquire a pg_advisory_xact_lock on the (tenant, project,
// env) key BEFORE invoking the store mutation, serializing with concurrent
// `loopzero deploy` flows that hold the same lock.
//
// Sprint A T1.1 (closes comp-001 CRITICAL): the locker was added to the
// handler struct in deep-review-2026-05-10-remediation-plan but never
// wired from server.Config. This setter mirrors SetK8sSync exactly so
// server/router.go can inject the locker post-construction.
//
// Lock-domain parity (EC-2): inject *lock.PoolLocker (via
// adapters.PoolEnvironmentLocker) — same advisory key as
// lock.WithEnvironmentLock used by build handlers. Any other locker
// implementation breaks cross-feature serialization.
//
// K8s sync OUTSIDE lock (EC-3, intentional design): h.k8sSync.CreateSecret
// runs AFTER mutate returns AND AFTER the lock releases — PG is canonical,
// K8s is eventual, and the lock has 5s timeout. Moving K8s sync inside
// mutate is a REGRESSION VECTOR (would kill secret Sets every time K8s
// API server is slow).
func (h *SecretHandler) SetLocker(locker SecretEnvLocker) {
	h.locker = locker
}

// SetPlanLimiter configures the plan-limit gate for secret mutations.
// J-22 T1.3: when wired, Bulk consults the gate BEFORE invoking store.Set;
// denied requests return 402 PLAN_LIMIT_EXCEEDED without store/audit calls.
// Nil-safe: when planLimits is nil, Bulk skips the check (legacy/test path).
func (h *SecretHandler) SetPlanLimiter(limiter SecretPlanLimiter) {
	h.planLimits = limiter
}

// SetTransitClient configures the OpenBao transit encrypt path (T2.3 of
// plan 2026-06-10-j22-openbao-transit-eso-sync v1.2). When wired, the
// Bulk handler encrypts every entry.Value via transit.Encrypt BEFORE
// invoking store.Set so PG never receives plaintext bytes.
//
// Nil-safe: when transitClient is nil, Bulk falls back to the legacy
// path where the existing AES-GCM wrapper in secret_wrapper.go encrypts
// at the store layer (legacy/test path). D6 ADR fail-loud refusal at
// boot time (T2.5) ensures production never lands on the legacy path
// silently — operator must opt-in via LOOPZERO_DEV=true or set
// OPENBAO_TRANSIT_ENABLED=true.
func (h *SecretHandler) SetTransitClient(client SecretTransit) {
	h.transitClient = client
}

// SetProjectChecker configures the cross-tenant binding check (T1.2 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2). When wired, Bulk resolves
// the requested project's owning tenant via the tenant-scoped GetByName
// query; a nil result returns 404 PROJECT_NOT_FOUND with a body that is
// BYTE-IDENTICAL between cross-tenant attempts (project exists for another
// tenant) and genuinely-nonexistent paths (project missing entirely). This
// is the EC-03 absorption defending against CWE-204 observable response
// discrepancy / existence-leak.
//
// Nil-safe: when projectChecker is nil, Bulk skips the check (legacy/test
// path). Production wiring MUST inject the real *store.PostgresProjectStore so
// the cross-tenant guard fires before transit encryption ever begins.
func (h *SecretHandler) SetProjectChecker(checker SecretProjectChecker) {
	h.projectChecker = checker
}

// secretListResponse is the JSON response for GET /api/v1/secrets.
type secretListResponse struct {
	Secrets []SecretMetadata `json:"secrets"`
}

// secretSetRequest is the JSON body for POST /api/v1/secrets.
type secretSetRequest struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Environment string `json:"environment"`
	App         string `json:"app"`
}

// secretSetResponse is the JSON response for POST /api/v1/secrets.
// Value is NEVER included in the response.
type secretSetResponse struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	App         string `json:"app"`
	Created     bool   `json:"created"`
}

// secretDeleteResponse is the JSON response for DELETE /api/v1/secrets/{name}.
type secretDeleteResponse struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// secretNameRegex validates secret names: uppercase letters, digits, underscores.
var secretNameRegex = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// --- J-22 T1.1: bulk endpoint ---

// secretBulkEntry is one entry in a bulk request — name + value + envs scope.
type secretBulkEntry struct {
	Name  string   `json:"name"`
	Value string   `json:"value"`
	Envs  []string `json:"envs"`
}

// secretBulkRequest is the JSON body for POST /api/v1/secrets/bulk.
// Project is read from the body first, falling back to ?project= query.
type secretBulkRequest struct {
	Project string            `json:"project"`
	Secrets []secretBulkEntry `json:"secrets"`
}

// secretBulkResult is a single per-key outcome inside the bulk response.
//
// Status (aggregate across envs for this key):
//   - "created"       — at least one env's store.Set + k8sSync succeeded
//   - "failed"        — all envs failed at store.Set
//
// SyncStatus (aggregate across envs):
//   - "pending"  — k8sSync not wired (legacy behaviour)
//   - "synced"   — every env's k8sSync succeeded
//   - "partial"  — at least one env synced, at least one failed
//   - "failed"   — all envs failed at k8sSync (or status itself == "failed")
//
// SyncStatusPerEnv (EC-02 absorption) — per-env granular state. Each env in
// entry.Envs produces one entry: "synced" | "failed" | "pending". UI surfaces
// this when SyncStatus == "partial".
//
// ErrorCode (EC-01 absorption) — populated when Status == "failed". Stable
// machine identifier (e.g. "STORE_SET_FAILED", "INVALID_KEY") for UI to
// disambiguate from the free-text Error.
type secretBulkResult struct {
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	Error            string            `json:"error,omitempty"`
	ErrorCode        string            `json:"error_code,omitempty"`
	SyncStatus       string            `json:"sync_status"`
	SyncStatusPerEnv map[string]string `json:"sync_status_per_env,omitempty"`
}

// secretBulkResponse holds the bulk POST response. Invariant per plan
// 2026-06-06-dashboard-journey-j22-secrets-crud § T1.1: len(Results)
// MUST equal len(request.Secrets), regardless of per-key success.
type secretBulkResponse struct {
	Results []secretBulkResult `json:"results"`
}

// secretBulkMaxKeys caps the size of a bulk request per EC-18. Matches
// the Pro plan limit of 100 secrets so a single batch cannot bypass
// plan enforcement.
const secretBulkMaxKeys = 100

// Bulk handles POST /api/v1/secrets/bulk?project={name}.
//
// Per plan T1.1, response shape MUST be {results: [{name, status,
// error?, sync_status}]} with one entry per input key, enforcing
// len(results) == len(request.secrets).
//
// EC-18 (size cap): > 100 keys returns 413 PAYLOAD_TOO_LARGE without
// invoking the store.
//
// EC-16 (input validation): names violating secretNameRegex return
// status="failed" without aborting the batch.
//
// EC absorptions deferred to subsequent iterations:
// - EC-9 (per-key savepoint atomicity)
// - EC-10 (pg_advisory_xact_lock race-safe limit gate via SecretEnvLocker)
// - EC-11 (ESO sync_status polling)
// - EC-12 (audit log atomicity)
// - EC-14/15 (duplicate keys + whitespace preservation handled by Set)
// Plan 2026-06-15-code-review-49-findings-remediation T5.4 (R2-F3 HIGH):
// CC 19 → ≤ 10 via 3 helper extracts (decodeAndValidateBulkRequest +
// assertSecretProjectBinding + assertSecretPlanLimit).
func (h *SecretHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	req, ok := decodeAndValidateBulkRequest(w, r)
	if !ok {
		return
	}

	project := req.Project
	if project == "" {
		project = r.URL.Query().Get("project")
	}

	if !h.assertSecretProjectBinding(ctx, w, uc.TenantID, uc.UserID, project) {
		return
	}
	if !h.assertSecretPlanLimit(ctx, w, uc.TenantID, req.Secrets) {
		return
	}

	results := make([]secretBulkResult, 0, len(req.Secrets))
	for _, entry := range req.Secrets {
		results = append(results, h.bulkProcessEntry(ctx, uc, project, entry))
	}

	writeJSON(w, http.StatusOK, secretBulkResponse{Results: results})
}

// decodeAndValidateBulkRequest decodes the JSON body + enforces:
//   - PAYLOAD_TOO_LARGE when over secretBulkMaxKeys
//   - DUPLICATE_KEY_IN_BATCH (EC-14 backend defense in depth)
//
// Returns (req, true) to continue OR (zero, false) when the response was
// already written.
func decodeAndValidateBulkRequest(w http.ResponseWriter, r *http.Request) (secretBulkRequest, bool) {
	var req secretBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return req, false
	}
	if len(req.Secrets) > secretBulkMaxKeys {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"code":    "PAYLOAD_TOO_LARGE",
			"message": fmt.Sprintf("bulk request exceeds %d keys; split into multiple uploads", secretBulkMaxKeys),
		})
		return req, false
	}
	seen := make(map[string]struct{}, len(req.Secrets))
	for _, e := range req.Secrets {
		if _, dup := seen[e.Name]; dup {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"code":    "DUPLICATE_KEY_IN_BATCH",
				"message": fmt.Sprintf("key %q appears more than once in the same batch", e.Name),
			})
			return req, false
		}
		seen[e.Name] = struct{}{}
	}
	return req, true
}

// assertSecretProjectBinding implements T1.2 cross-tenant binding check.
// Returns true to continue, false when 404 was written. Per EC-03,
// emits byte-identical PROJECT_NOT_FOUND whether the project belongs to
// another tenant OR does not exist — canonical defense against CWE-204
// (observable response discrepancy / existence-leak).
func (h *SecretHandler) assertSecretProjectBinding(
	ctx context.Context,
	w http.ResponseWriter,
	tenantID, userID, project string,
) bool {
	if h.projectChecker == nil || project == "" {
		return true
	}
	proj, err := h.projectChecker.GetByName(ctx, tenantID, project)
	if err == nil && proj != nil {
		return true
	}
	if h.audit != nil {
		if auditErr := h.audit.LogAudit(ctx, tenantID, "PROJECT_LOOKUP_FAILED",
			userID, "secret", project, "",
			map[string]any{"reason": "cross_tenant_or_not_found"}); auditErr != nil {
			slog.Error("failed to write audit log for cross-tenant lookup",
				"project", project, "error", auditErr)
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{
		"code":    "PROJECT_NOT_FOUND",
		"message": "project not found",
	})
	return false
}

// assertSecretPlanLimit implements J-22 T1.3 plan-limit gate. Returns
// true to continue, false when 402 PLAN_LIMIT_EXCEEDED was written.
// EC-10 race-safe lock remains DEFERRED.
func (h *SecretHandler) assertSecretPlanLimit(
	ctx context.Context,
	w http.ResponseWriter,
	tenantID string,
	secrets []secretBulkEntry,
) bool {
	if h.planLimits == nil {
		return true
	}
	var adding int
	for _, e := range secrets {
		if !secretNameRegex.MatchString(e.Name) {
			continue
		}
		adding += len(e.Envs)
	}
	if adding == 0 {
		return true
	}
	allowed, current, max, plan := h.planLimits.IsAllowedToCreate(ctx, tenantID, adding)
	if allowed {
		return true
	}
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"code":    "PLAN_LIMIT_EXCEEDED",
		"message": fmt.Sprintf("plan %q allows %d secrets, currently at %d", plan, max, current),
		"current": current,
		"limit":   "secrets",
		"max":     max,
		"plan":    plan,
	})
	return false
}

// bulkEnvAggregates collects per-env outcomes for a single bulk entry.
// Extracted from SecretHandler.Bulk so the entry-level + env-level
// processing pipelines are independently legible + testable.
// T4.2 of plan 2026-06-11-code-review-remediation v1.2 + ADR D6.
type bulkEnvAggregates struct {
	syncStatusPerEnv map[string]string
	firstErr         error
	storeFailures    int
	syncFailures     int
	syncSuccesses    int
}

// bulkProcessEntry handles one batch entry (one secret key, N envs).
// Encapsulates the per-entry pipeline:
//  1. Validate the key name; INVALID_KEY short-circuits without touching
//     the store.
//  2. Run the per-env transit-encrypt → store.Set → k8sSync triad
//     (continue-best-effort per EC-01).
//  3. Aggregate per-env outcomes into the entry's Status/SyncStatus
//     following the union-state contract (EC-02).
//  4. Emit one SECRET_CREATED audit row per successful entry.
//
// Returns a fully-populated secretBulkResult ready to append.
func (h *SecretHandler) bulkProcessEntry(ctx context.Context, uc *middleware.UserContext, project string, entry secretBulkEntry) secretBulkResult {
	result := secretBulkResult{
		Name:       entry.Name,
		Status:     "created",
		SyncStatus: "pending",
	}
	if !secretNameRegex.MatchString(entry.Name) {
		result.Status = "failed"
		result.Error = "INVALID_KEY"
		result.ErrorCode = "INVALID_KEY"
		return result
	}

	aggs := h.bulkProcessEnvs(ctx, uc, project, entry)
	h.bulkAggregateStatus(&result, entry, aggs)
	result.SyncStatusPerEnv = aggs.syncStatusPerEnv

	// Audit log per-key (T1.2 J-22 invariant): one row per logical
	// SECRET_CREATED action when at least one env persisted to PG.
	// INVALID_KEY short-circuit handled above; non-"created" Status
	// implies storeFailures == len(entry.Envs) so no audit.
	if h.audit != nil && result.Status == "created" {
		if auditErr := h.audit.LogAudit(ctx, uc.TenantID, "SECRET_CREATED", uc.UserID,
			"secret", entry.Name, "",
			map[string]any{"envs": entry.Envs, "project": project}); auditErr != nil {
			slog.Error("failed to write audit log for secret bulk",
				"name", entry.Name, "error", auditErr)
		}
	}
	return result
}

// bulkProcessEnvs runs the transit→store→k8sSync triad for every env
// of one entry. Implements EC-01 continue-best-effort: a failure for
// one env MUST NOT short-circuit the others. Returns the aggregates
// the caller folds into the entry-level result.
func (h *SecretHandler) bulkProcessEnvs(ctx context.Context, uc *middleware.UserContext, project string, entry secretBulkEntry) bulkEnvAggregates {
	aggs := bulkEnvAggregates{
		syncStatusPerEnv: make(map[string]string, len(entry.Envs)),
	}
	for _, env := range entry.Envs {
		// T2.3 (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2):
		// when transit is wired, encrypt the plaintext via OpenBao
		// BEFORE persisting to PG. On transit failure, MUST NOT
		// persist plaintext — increment storeFailures + skip the
		// store.Set + skip k8sSync for this env (continue-best-effort
		// per EC-01 still respected; remaining envs unaffected).
		valueBytes := []byte(entry.Value)
		if h.transitClient != nil {
			ct, terr := h.transitClient.Encrypt(ctx, uc.TenantID, valueBytes)
			if terr != nil {
				if aggs.firstErr == nil {
					aggs.firstErr = terr
				}
				aggs.syncStatusPerEnv[env] = "failed"
				aggs.storeFailures++
				continue
			}
			valueBytes = []byte(ct)
		}
		if _, err := h.store.Set(ctx, uc.TenantID, project, env, "", entry.Name, valueBytes); err != nil {
			if aggs.firstErr == nil {
				aggs.firstErr = err
			}
			aggs.syncStatusPerEnv[env] = "failed"
			aggs.storeFailures++
			continue
		}

		// store.Set succeeded for this env; now invoke k8sSync per D2 ADR
		// (k8sSync runs OUTSIDE lock; PG row is canonical even on sync
		// failure — eventual consistency via ESO ExternalSecret CR).
		if h.k8sSync == nil {
			aggs.syncStatusPerEnv[env] = "pending"
			continue
		}
		if syncErr := h.k8sSync.CreateSecret(ctx, uc.TenantID, project, env, "", entry.Name, entry.Value); syncErr != nil {
			aggs.syncStatusPerEnv[env] = "failed"
			aggs.syncFailures++
			slog.Warn("k8sSync.CreateSecret failed for bulk entry env",
				"name", entry.Name, "env", env, "tenant", uc.TenantID, "error", syncErr)
			continue
		}
		aggs.syncStatusPerEnv[env] = "synced"
		aggs.syncSuccesses++
	}
	return aggs
}

// bulkAggregateStatus folds per-env aggregates into the entry-level
// Status + SyncStatus per the union-state contract (EC-02):
//   - Status: "failed" iff EVERY env's store.Set failed; else "created"
//     (PG row canonical in at least one env)
//   - SyncStatus: "synced" / "partial" / "failed" / "pending"
//   - Error/ErrorCode reflect the first failure for diagnostics
func (h *SecretHandler) bulkAggregateStatus(result *secretBulkResult, entry secretBulkEntry, aggs bulkEnvAggregates) {
	// Aggregate Status.
	if aggs.storeFailures == len(entry.Envs) {
		result.Status = "failed"
		if aggs.firstErr != nil {
			result.Error = aggs.firstErr.Error()
		}
		result.ErrorCode = "STORE_SET_FAILED"
	} else if aggs.storeFailures > 0 {
		// At least one env persisted to PG but at least one failed.
		// Status stays "created" (consistent with prior contract: caller
		// asked to create and we did create at least one), but Error
		// surfaces the first store failure for diagnostics.
		result.Status = "created"
		if aggs.firstErr != nil {
			result.Error = aggs.firstErr.Error()
		}
		result.ErrorCode = "STORE_SET_PARTIAL"
	}

	// Aggregate SyncStatus:
	//   - k8sSync wired AND all envs synced → "synced"
	//   - k8sSync wired AND any env failed  → "partial" or "failed"
	//   - k8sSync NOT wired                  → "pending" (legacy)
	switch {
	case h.k8sSync == nil:
		result.SyncStatus = "pending"
	case aggs.syncFailures == 0 && aggs.storeFailures == 0:
		result.SyncStatus = "synced"
	case aggs.syncSuccesses == 0:
		result.SyncStatus = "failed"
	default:
		result.SyncStatus = "partial"
	}
}

// List handles GET /api/v1/secrets?env={env}&project={project}.
// Returns secret names only — NEVER values.
func (h *SecretHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	env := r.URL.Query().Get("env")
	project := r.URL.Query().Get("project")

	// Red Team loopzero-cloud#92 absorption (2026-06-27): missing-parent-guard.
	// Without this check, GET /api/v1/secrets?project=<nonexistent> returned
	// 200 with an empty list. CWE-204 defense + UX: the dashboard's Secrets
	// page rendered an "Add secret" form on top of a deleted project. Same
	// `assertSecretProjectBinding` helper used by Set/Bulk/DeleteSecret —
	// emits byte-identical PROJECT_NOT_FOUND whether the project belongs to
	// another tenant or does not exist anywhere (no existence leak).
	if !h.assertSecretProjectBinding(ctx, w, uc.TenantID, uc.UserID, project) {
		return
	}

	secrets, err := h.store.ListNames(ctx, uc.TenantID, project, env)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if secrets == nil {
		secrets = []SecretMetadata{}
	}

	writeJSON(w, http.StatusOK, secretListResponse{Secrets: secrets})
}

// Set handles POST /api/v1/secrets.
// Creates or updates a secret. Value is NEVER returned in the response.
func (h *SecretHandler) Set(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req secretSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	// Validate name format
	if !secretNameRegex.MatchString(req.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_SECRET_NAME",
			"message": "Secret name must be uppercase with underscores (e.g., DATABASE_URL). Got: '" + req.Name + "'.",
		})
		return
	}

	// Validate required fields
	if req.Value == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("value is required"))
		return
	}
	if req.Environment == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}
	if req.App == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("app is required"))
		return
	}

	// Use project name from query or default
	project := r.URL.Query().Get("project")

	// T1.1 of deep-review-2026-05-10-remediation-plan: serialize mutations
	// per (tenant, project, env) so concurrent `loopzero secret set` + a deploy
	// capturing config cannot race. Falls back to non-locked path when
	// locker is nil (tests + boot path).
	var created bool
	mutate := func(_ db.DBTX) error {
		var setErr error
		created, setErr = h.store.Set(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, []byte(req.Value))
		return setErr
	}
	var err error
	if h.locker != nil {
		err = h.locker.WithLock(ctx, uc.TenantID, project, req.Environment, mutate)
	} else {
		err = mutate(nil)
	}
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress for this environment. Retry shortly.",
			})
			return
		}
		// V9 L18: mirror mapBuildError + promote.go pattern — log unexpected
		// errors for diagnosis. The silent-swallow pre-L18 made every
		// secret-set failure indistinguishable from any other 5xx.
		slog.Error("unexpected error in secret set handler",
			"error", err.Error(),
			"type", fmt.Sprintf("%T", err),
			"tenant_id", uc.TenantID,
			"project", project,
			"environment", req.Environment,
			"app", req.App,
			"secret_name", req.Name,
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// MEET-2026-03-31-024: Audit log for secret operations
	if h.audit != nil {
		action := "SECRET_UPDATED"
		if created {
			action = "SECRET_CREATED"
		}
		if auditErr := h.audit.LogAudit(ctx, uc.TenantID, action, uc.UserID,
			"secret", req.Name, correlation.Read(r),
			map[string]any{"environment": req.Environment, "app": req.App, "project": project}); auditErr != nil {
			slog.Error("failed to write audit log for secret set", "name", req.Name, "error", auditErr)
		}
	}

	// Create K8s Secret on mgmt + ExternalSecret CR on workload for ESO sync.
	// dogfood-2026-05-08-fixes T2.3 (F-2): emit "skip" metric when k8sSync is
	// nil — that path was the silent-failure cause, formerly invisible. Now
	// `rate(loopzero_secret_externalsecret_total{result="skip"}[5m]) > 0` alerts
	// that the API is silently no-opping `loopzero secret set`.
	if h.k8sSync != nil {
		if syncErr := h.k8sSync.CreateSecret(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, req.Value); syncErr != nil {
			// Log but don't fail — PG is source of truth, K8s sync is eventual
			slog.Error("failed to sync secret to K8s", "name", req.Name, "error", syncErr)
		}
	} else {
		metrics.SecretExternalSecretTotal.WithLabelValues("skip").Inc()
		slog.Warn("ExternalSecret sync skipped — k8sSync nil at boot",
			"tenant", uc.TenantID,
			"app", req.App,
			"environment", req.Environment,
			"secret_name", req.Name)
	}

	writeJSON(w, http.StatusOK, secretSetResponse{
		Name:        req.Name,
		Environment: req.Environment,
		App:         req.App,
		Created:     created,
	})
}

// DeleteSecret handles DELETE /api/v1/secrets/{name}?environment={env}&app={app}.
func (h *SecretHandler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("secret name is required"))
		return
	}

	env := r.URL.Query().Get("environment")
	app := r.URL.Query().Get("app")
	project := r.URL.Query().Get("project")

	// Sprint A T1.1 EC-1: validate environment query param. Without this,
	// with the locker wired, empty-env DELETEs produce lock key
	// "{tenant}:{project}:" (trailing colon) — two concurrent empty-env
	// deletes contend on that single bucket → bogus DEPLOY_IN_PROGRESS
	// 409s, and the store.Delete matches L19 PK rows that include the
	// empty-env slot. Mirror the Set handler validation pattern.
	if env == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}

	// T1.1 of deep-review-2026-05-10-remediation-plan: same lock as Set.
	var deleted bool
	mutate := func(_ db.DBTX) error {
		var delErr error
		deleted, delErr = h.store.Delete(ctx, uc.TenantID, project, env, app, name)
		return delErr
	}
	var err error
	if h.locker != nil {
		err = h.locker.WithLock(ctx, uc.TenantID, project, env, mutate)
	} else {
		err = mutate(nil)
	}
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress for this environment. Retry shortly.",
			})
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"code":    "SECRET_NOT_FOUND",
			"message": "Secret '" + name + "' not found.",
		})
		return
	}

	// MEET-2026-03-31-024: Audit log for secret deletion
	if h.audit != nil {
		if auditErr := h.audit.LogAudit(ctx, uc.TenantID, "SECRET_DELETED", uc.UserID,
			"secret", name, correlation.Read(r),
			map[string]any{"environment": env, "app": app, "project": project}); auditErr != nil {
			slog.Error("failed to write audit log for secret delete", "name", name, "error", auditErr)
		}
	}

	// Remove K8s Secret from mgmt + ExternalSecret CR from workload
	if h.k8sSync != nil {
		if syncErr := h.k8sSync.DeleteSecret(ctx, uc.TenantID, project, env, app, name); syncErr != nil {
			slog.Error("failed to delete secret from K8s", "name", name, "error", syncErr)
		}
	}

	writeJSON(w, http.StatusOK, secretDeleteResponse{
		Name:    name,
		Deleted: true,
	})
}
