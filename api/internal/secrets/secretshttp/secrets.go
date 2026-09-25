// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package secretshttp serves the tenant secret endpoints: /api/v1/secrets and its
// bulk sibling, plus the internal secret-fetch endpoint the build plane calls.
//
// Fifth family to leave the flat `routes` package (B-080, slice-28), and the
// first chosen by measurement rather than by counting names. A compiler probe —
// move the files, build the module, read what breaks — put it at more broken
// references than billing, teams or preview, and it was still the right pick:
// 879+ lines in three files is the largest mass removed per unit of risk on the
// board.
//
// That probe under-reported, which is worth carrying: Go does not compile a
// package that depends on a failed one, and it stops at ten errors per package.
// A single build measures a floor. The real consumer set came out of iterating
// the build until it closed.
//
// Depends on two leaves and no sibling family: `httpio` for the response
// writers, `routestest` for request helpers — and that one only from _test.go,
// because its helpers mint an owner-role identity.
package secretshttp

import (
	"context"
	"encoding/json"
	"errors"

	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/secrets"
	loopzerovault "github.com/getloopscale/loopzero/pkg/vault"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/correlation"
	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	auditctx "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	SecretMetadata = ports.SecretMetadata
	SecretStore    = secrets.SecretStore
	SecretK8sSync  = secrets.SecretK8sSync
	AuditLogger    = ports.AuditLogger
)

// SecretEnvLocker is the narrow interface SecretHandler needs to wrap
// mutations in `pg_advisory_xact_lock` (T1.1 of deep-review-2026-05-10-
// remediation-plan). Defined locally per ISP — accepts any locker the
// services layer exposes, including `shared.EnvironmentLocker` and
// `adapters.PoolEnvironmentLocker`.
//
// Required: every secret mutation — single, bulk and delete — runs under
// this lock (CLAUDE.md Rule 6). NewSecretHandler refuses a nil locker, so a
// mis-wired server cannot silently mutate secrets unlocked.
type SecretEnvLocker interface {
	WithLock(ctx context.Context, tenant, project, env string, fn func(tx dbtx.DBTX) error) error
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
	GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error)
}

// SecretPlanLimiter — narrow interface (ISP) for the plan-limit gate,
// satisfied directly by *enforcement.PlanLimiter. The plan caps secrets PER
// ENVIRONMENT (plan_limits metric "secrets_per_env"), so the gate asks for
// the count an environment would reach, never a tenant-wide total. J-22 T1.3.
type SecretPlanLimiter interface {
	CheckValue(ctx context.Context, tenantID, metric string, requested int) (enforcement.LimitResult, error)
}

// perEnvironmentLimitMetric is the plan_limits metric the gate checks.
const perEnvironmentLimitMetric = "secrets_per_env"

// SecretHandler provides HTTP handlers for secret operations.
type SecretHandler struct {
	store          SecretStore
	k8sSync        SecretK8sSync        // optional — nil disables K8s sync
	audit          AuditLogger          // optional — nil disables audit logging
	locker         SecretEnvLocker      // required — serializes every mutation with deploys (Rule 6)
	planLimits     SecretPlanLimiter    // nil only where no plan is enforced (no database: dev/test)
	projectChecker SecretProjectChecker // optional — nil disables T1.2 cross-tenant binding (legacy/test path)
}

// transitFailureStatus classifies an encryption failure for the per-environment
// sync status.
//
// A policy denial and a network timeout are opposite facts for whoever is on
// call: the denial is PERMANENT and needs an operator to widen a Vault policy;
// the timeout is transient and clears itself. Both reported "failed" until
// 2026-08-13, so the operator waited for something that was never going to
// improve — the treat-every-error-alike shape Rule 8 forbids (B-072).
//
// The typed error already existed and had no reader: ErrTransitKeyCreateFailed
// appeared nowhere outside its own declaration and its own test.
//
// Extracted as a function so the classification is testable without staging a
// whole bulk upload — the branch shipped untested in its first version.
func transitFailureStatus(err error) string {
	if errors.Is(err, loopzerovault.ErrTransitKeyCreateFailed) {
		return "denied"
	}
	return "failed"
}

// NewSecretHandler creates a SecretHandler. The store and the environment
// locker are both required: a secret mutation outside the (tenant, project,
// env) lock races a deploy capturing config, so a nil locker is a wiring bug
// and is refused here rather than degraded to an unlocked path.
func NewSecretHandler(store SecretStore, locker SecretEnvLocker) *SecretHandler {
	if store == nil || locker == nil {
		panic("secrets: NewSecretHandler requires a SecretStore and a SecretEnvLocker")
	}
	return &SecretHandler{store: store, locker: locker}
}

// SetAuditLogger configures audit logging for sensitive operations.
func (h *SecretHandler) SetAuditLogger(audit AuditLogger) {
	h.audit = audit
}

// SetK8sSync configures the K8s sync for creating Secrets and ExternalSecret CRs.
func (h *SecretHandler) SetK8sSync(sync SecretK8sSync) {
	h.k8sSync = sync
}

// SetPlanLimiter configures the plan-limit gate for secret mutations.
// J-22 T1.3: Bulk consults the gate BEFORE invoking store.Set; denied
// requests return 402 PLAN_LIMIT_EXCEEDED without store/audit calls. The
// router wires it from server.Config.Guards.PlanLimiter.
func (h *SecretHandler) SetPlanLimiter(limiter SecretPlanLimiter) {
	h.planLimits = limiter
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

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
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
	if !h.assertSecretPlanLimit(ctx, w, uc.TenantID, project, req.Secrets) {
		return
	}

	httpio.WriteJSON(w, http.StatusOK, secretBulkResponse{Results: h.bulkProcess(ctx, uc, project, req.Secrets)})
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return req, false
	}
	if len(req.Secrets) > secretBulkMaxKeys {
		httpio.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"code":    "PAYLOAD_TOO_LARGE",
			"message": fmt.Sprintf("bulk request exceeds %d keys; split into multiple uploads", secretBulkMaxKeys),
		})
		return req, false
	}
	seen := make(map[string]struct{}, len(req.Secrets))
	for _, e := range req.Secrets {
		if _, dup := seen[e.Name]; dup {
			httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
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
	httpio.WriteJSON(w, http.StatusNotFound, map[string]string{
		"code":    "PROJECT_NOT_FOUND",
		"message": "project not found",
	})
	return false
}

// assertSecretPlanLimit implements the J-22 T1.3 plan-limit gate. For every
// environment the batch touches it counts the secrets the environment already
// holds plus the ones the batch would ADD (an existing name is an update, not
// an addition) and asks the plan whether that total fits "secrets_per_env".
// Returns true to continue; false when 402 PLAN_LIMIT_EXCEEDED (or a 500 for
// an unreadable store) was written.
//
// The count is taken before the per-environment lock, so two concurrent
// uploads can each fit alone and together exceed the cap (EC-10).
func (h *SecretHandler) assertSecretPlanLimit(
	ctx context.Context,
	w http.ResponseWriter,
	tenantID, project string,
	secrets []secretBulkEntry,
) bool {
	if h.planLimits == nil {
		return true
	}
	for env, names := range bulkNamesByEnv(secrets) {
		existing, err := h.store.ListNames(ctx, tenantID, project, env)
		if err != nil {
			slog.Error("secret bulk: cannot count existing secrets for the plan gate",
				"tenant", tenantID, "project", project, "env", env, "error", err)
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
			return false
		}
		requested := len(existing) + countNewNames(existing, names)
		res, err := h.planLimits.CheckValue(ctx, tenantID, perEnvironmentLimitMetric, requested)
		if err != nil {
			// The limiter fails closed: an unreadable plan is a refusal,
			// never a pass and never a guessed 402.
			slog.Error("secret bulk: plan limit cannot be read; refusing the batch",
				"tenant", tenantID, "project", project, "env", env, "error", err)
			var te *loopzeroerrors.LoopZeroError
			if !errors.As(err, &te) {
				te = loopzeroerrors.ErrPlatformDegraded
			}
			httpio.WriteLoopZeroError(w, te)
			return false
		}
		if res.Allowed {
			continue
		}
		httpio.WriteJSON(w, http.StatusPaymentRequired, map[string]any{
			"code": "PLAN_LIMIT_EXCEEDED",
			"message": fmt.Sprintf("plan %q allows %d secrets per environment; %q would reach %d",
				res.Plan, res.Limit, env, requested),
			"current": len(existing),
			"limit":   "secrets",
			"max":     res.Limit,
			"plan":    res.Plan,
		})
		return false
	}
	return true
}

// bulkNamesByEnv groups the valid key names of a batch by environment.
func bulkNamesByEnv(secrets []secretBulkEntry) map[string][]string {
	byEnv := map[string][]string{}
	for _, e := range secrets {
		if !secretNameRegex.MatchString(e.Name) {
			continue
		}
		for _, env := range e.Envs {
			byEnv[env] = append(byEnv[env], e.Name)
		}
	}
	return byEnv
}

// countNewNames counts batch names with no existing app-less row: bulk writes
// app "", so only those rows are overwritten rather than added.
func countNewNames(existing []SecretMetadata, names []string) int {
	have := make(map[string]struct{}, len(existing))
	for _, m := range existing {
		if m.App == "" {
			have[m.Name] = struct{}{}
		}
	}
	added := 0
	for _, n := range names {
		if _, ok := have[n]; !ok {
			added++
		}
	}
	return added
}

// bulkEnvAggregates collects per-env outcomes for a single bulk entry.
// T4.2 of plan 2026-06-11-code-review-remediation v1.2 + ADR D6.
type bulkEnvAggregates struct {
	syncStatusPerEnv map[string]string
	firstErr         error
	storeFailures    int
	syncFailures     int
	syncSuccesses    int
}

func (a *bulkEnvAggregates) fail(env, status string, err error) {
	if a.firstErr == nil {
		a.firstErr = err
	}
	a.syncStatusPerEnv[env] = status
	a.storeFailures++
}

// bulkEntryState is one entry of the batch as it moves through the phases.
type bulkEntryState struct {
	entry secretBulkEntry
	valid bool
	aggs  bulkEnvAggregates
}

// bulkWrite is one (entry, env) row waiting to be persisted. value is the
// plaintext: the store is the single encryption boundary, and it encrypts
// through transit or refuses — it never persists plaintext.
type bulkWrite struct {
	state     *bulkEntryState
	env       string
	value     []byte
	persisted bool
}

// bulkProcess runs the batch in four phases:
//
//  1. validate each key and group each (key, env) write by environment;
//  2. persist, one (project, env) group at a time, INSIDE the environment
//     lock, so a deploy capturing that environment's config sees either all
//     of the batch's writes for it or none (CLAUDE.md Rule 6);
//  3. sync to the workload cluster OUTSIDE the lock (EC-3: PG is canonical,
//     K8s is eventual, and a slow API server must not hold the lock);
//  4. fold per-env outcomes into one result per key, and audit.
//
// EC-01 continue-best-effort holds throughout: one env failing never stops
// the others. len(results) == len(entries) is the response invariant.
func (h *SecretHandler) bulkProcess(ctx context.Context, uc *auth.UserContext, project string, entries []secretBulkEntry) []secretBulkResult {
	states := make([]*bulkEntryState, len(entries))
	groups := map[string][]*bulkWrite{}
	var envOrder []string
	for i, entry := range entries {
		st := &bulkEntryState{
			entry: entry,
			valid: secretNameRegex.MatchString(entry.Name),
			aggs:  bulkEnvAggregates{syncStatusPerEnv: make(map[string]string, len(entry.Envs))},
		}
		states[i] = st
		if !st.valid {
			continue
		}
		for _, env := range entry.Envs {
			if _, seen := groups[env]; !seen {
				envOrder = append(envOrder, env)
			}
			groups[env] = append(groups[env], &bulkWrite{state: st, env: env, value: []byte(entry.Value)})
		}
	}

	for _, env := range envOrder {
		h.persistEnvGroup(ctx, uc.TenantID, project, env, groups[env])
	}
	for _, env := range envOrder {
		for _, wr := range groups[env] {
			if wr.persisted {
				h.syncBulkWrite(ctx, uc.TenantID, project, wr)
			}
		}
	}

	results := make([]secretBulkResult, 0, len(states))
	for _, st := range states {
		results = append(results, h.bulkEntryResult(ctx, uc, project, st))
	}
	return results
}

// persistEnvGroup writes every row of one (project, env) group inside the
// environment lock. A lock that cannot be taken (a deploy holds it past the
// timeout) fails every row of THIS group only.
func (h *SecretHandler) persistEnvGroup(ctx context.Context, tenantID, project, env string, group []*bulkWrite) {
	lockErr := h.locker.WithLock(ctx, tenantID, project, env, func(_ dbtx.DBTX) error {
		for _, wr := range group {
			if _, err := h.store.Set(ctx, tenantID, project, env, "", wr.state.entry.Name, wr.value); err != nil {
				// The store keeps the transit cause in its error chain, so a
				// policy denial (permanent) still reads as "denied" and a
				// timeout (transient) as "failed" — B-072.
				wr.state.aggs.fail(env, transitFailureStatus(err), err)
				continue
			}
			wr.persisted = true
		}
		return nil
	})
	if lockErr == nil {
		return
	}
	slog.Warn("secret bulk: environment lock not acquired",
		"tenant", tenantID, "project", project, "env", env, "error", lockErr)
	for _, wr := range group {
		if !wr.persisted {
			wr.state.aggs.fail(env, "failed", lockErr)
		}
	}
}

// syncBulkWrite materializes one persisted row on the workload cluster
// (D2 ADR). Without a workload sync the row stays canonical in PG and is
// reported "pending"; B-176 requires the same skip metric the single-set
// path emits, once per persisted row, because
// sum(loopzero_secret_externalsecret_total) is the trigger docs/decisions.md § 2
// relies on to notice the secrets table stopped being empty.
func (h *SecretHandler) syncBulkWrite(ctx context.Context, tenantID, project string, wr *bulkWrite) {
	aggs := &wr.state.aggs
	name := wr.state.entry.Name
	if h.k8sSync == nil {
		metrics.SecretExternalSecretTotal.WithLabelValues("skip").Inc()
		aggs.syncStatusPerEnv[wr.env] = "pending"
		return
	}
	if err := h.k8sSync.CreateSecret(ctx, tenantID, project, wr.env, "", name, wr.state.entry.Value); err != nil {
		aggs.syncStatusPerEnv[wr.env] = "failed"
		aggs.syncFailures++
		slog.Warn("k8sSync.CreateSecret failed for bulk entry env",
			"name", name, "env", wr.env, "tenant", tenantID, "error", err)
		return
	}
	aggs.syncStatusPerEnv[wr.env] = "synced"
	aggs.syncSuccesses++
}

// bulkEntryResult folds one entry's per-env outcomes into its result and
// writes one SECRET_CREATED audit row when at least one env persisted.
func (h *SecretHandler) bulkEntryResult(ctx context.Context, uc *auth.UserContext, project string, st *bulkEntryState) secretBulkResult {
	result := secretBulkResult{
		Name:       st.entry.Name,
		Status:     "created",
		SyncStatus: "pending",
	}
	if !st.valid {
		result.Status = "failed"
		result.Error = "INVALID_KEY"
		result.ErrorCode = "INVALID_KEY"
		return result
	}
	h.bulkAggregateStatus(&result, st.entry, st.aggs)
	result.SyncStatusPerEnv = st.aggs.syncStatusPerEnv

	if h.audit != nil && result.Status == "created" {
		if auditErr := h.audit.LogAudit(ctx, uc.TenantID, "SECRET_CREATED", uc.UserID,
			"secret", st.entry.Name, "",
			map[string]any{"envs": st.entry.Envs, "project": project}); auditErr != nil {
			slog.Error("failed to write audit log for secret bulk",
				"name", st.entry.Name, "error", auditErr)
		}
	}
	return result
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
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if secrets == nil {
		secrets = []SecretMetadata{}
	}

	httpio.WriteJSON(w, http.StatusOK, secretListResponse{Secrets: secrets})
}

// Set handles POST /api/v1/secrets.
// Creates or updates a secret. Value is NEVER returned in the response.
func (h *SecretHandler) Set(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req secretSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	// Validate name format
	if !secretNameRegex.MatchString(req.Name) {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_SECRET_NAME",
			"message": "Secret name must be uppercase with underscores (e.g., DATABASE_URL). Got: '" + req.Name + "'.",
		})
		return
	}

	// Validate required fields
	if req.Value == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("value is required"))
		return
	}
	if req.Environment == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}
	if req.App == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("app is required"))
		return
	}

	// Use project name from query or default
	project := r.URL.Query().Get("project")

	// B-059: declare the resource id for the audit row (was read out of
	// the query string by the middleware).
	auditctx.SetResourceID(ctx, project)

	// T1.1 of deep-review-2026-05-10-remediation-plan: serialize mutations
	// per (tenant, project, env) so concurrent `loopzero secret set` + a deploy
	// capturing config cannot race.
	var created bool
	err := h.locker.WithLock(ctx, uc.TenantID, project, req.Environment, func(_ dbtx.DBTX) error {
		var setErr error
		created, setErr = h.store.Set(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, []byte(req.Value))
		return setErr
	})
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			httpio.WriteJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress for this environment. Retry shortly.",
			})
			return
		}
		// #138 — a secrets backend that cannot encrypt is NOT a generic
		// server fault. It has a distinct cause, owner and remedy, and
		// collapsing it into INTERNAL_ERROR is what forced the issue author
		// to guess at the subsystem from which project happened to work.
		// 503, not 500: the request was valid and a dependency was down.
		if errors.Is(err, loopzeroerrors.ErrSecretsBackendUnavailable) {
			slog.Error("secrets backend unavailable on set",
				"error", err.Error(),
				"tenant_id", uc.TenantID,
				"project", project,
				"environment", req.Environment,
				"secret_name", req.Name,
			)
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrSecretsBackendUnavailable)
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
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
	//
	// B-176: bulkProcessEnvs handles the same condition and must emit the same
	// series. It did not, and the gap was found by reading the two paths side
	// by side rather than by anything failing. Change one of these two sites
	// and TestExternalSecretSkip_BothWritePathsEmitWhenSyncIsNil fails, which
	// is the point of asserting them in one test.
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

	httpio.WriteJSON(w, http.StatusOK, secretSetResponse{
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

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("secret name is required"))
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}

	// T1.1 of deep-review-2026-05-10-remediation-plan: same lock as Set.
	var deleted bool
	err := h.locker.WithLock(ctx, uc.TenantID, project, env, func(_ dbtx.DBTX) error {
		var delErr error
		deleted, delErr = h.store.Delete(ctx, uc.TenantID, project, env, app, name)
		return delErr
	})
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			httpio.WriteJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress for this environment. Retry shortly.",
			})
			return
		}
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if !deleted {
		httpio.WriteJSON(w, http.StatusNotFound, map[string]string{
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

	httpio.WriteJSON(w, http.StatusOK, secretDeleteResponse{
		Name:    name,
		Deleted: true,
	})
}
