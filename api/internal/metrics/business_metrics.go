// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Business metrics — observability gaps resolution plan (Phase 4A).
var (
	// DeployLeadTime measures the total time from build creation to release ACTIVE
	// plus health probe success. This is the executive metric: "how long until the
	// user's code is live?"
	DeployLeadTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopruntime_deploy_lead_time_seconds",
		Help:    "Total time from build creation to healthy URL (deploy lead time).",
		Buckets: []float64{30, 60, 120, 180, 300, 600, 900, 1800},
	}, []string{"tenant", "environment"})

	// FalseSuccessTotal counts releases that were marked ACTIVE but later detected
	// as unhealthy by the convergence loop's health check. A non-zero rate indicates
	// problems with health probes or deployment reliability.
	FalseSuccessTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_false_success_total",
		Help: "Releases marked ACTIVE that later became unhealthy (false success).",
	}, []string{"tenant", "environment"})

	// RetryAmplificationTotal counts total retry attempts per operation type.
	// High values indicate cascading failures or misconfigured timeouts.
	RetryAmplificationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_retry_amplification_total",
		Help: "Total retry attempts by operation type.",
	}, []string{"operation"})

	// SecretExternalSecretTotal counts ExternalSecret CR create/update calls
	// from SecretK8sManager (cell-0-cluster path).
	//
	// dogfood-2026-05-08-fixes T2.3 (F-2 instrumentation, ADR D3).
	//
	// Cardinality: ONLY label is `result` ∈ {success, fail, skip} — 3 series
	// total. NO tenant label (would be unbounded with N tenants); per EC-3
	// of the edge-case review.
	//
	// Use:
	//   - "success" — ExternalSecret CR created or updated successfully
	//   - "fail" — SecretK8sManager.CreateSecret returned an error. ANY of its
	//     error exits, not only the runtime client's (RBAC, network, etc.):
	//     also the mgmt-cluster Secret write and the namespace derivation.
	//     B-177 widened this from "runtime client returned error", which had
	//     been the description AND the implementation.
	//   - "skip" — manager nil at boot (CELL_0_KUBECONFIG missing) so the
	//     entire path was bypassed. THIS IS THE INVISIBLE-FAILURE SIGNAL
	//     that F-2 was originally caused by.
	//
	// The invariant behind those three, and the reason B-176 and B-177 both
	// exist: `POST /secrets` persists the row BEFORE any of this runs, so
	// every way out of the sync path must move exactly one series. An exit
	// that counts nothing leaves a row in `secrets` while
	// sum(loopruntime_secret_externalsecret_total) reads zero — and that sum is what
	// the B-126 canary deferral
	// (docs/decisions.md § 2)
	// uses as its trigger. Adding an early return here without an increment
	// re-opens that hole. TestCreateSecret_EveryExitMovesTheCounter
	// (api/internal/k8s/secretsync) is the guard.
	//
	// Alert opportunity: rate(loopruntime_secret_externalsecret_total{result="skip"}[5m]) > 0
	// indicates the API silently no-ops `loopruntime secret set` calls.
	SecretExternalSecretTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_secret_externalsecret_total",
		Help: "ExternalSecret CR create/update outcomes from loopruntime-api workload sync path.",
	}, []string{"result"})

	// SecretAuditWriteFailedTotal counts cases where the broker secret-fetch
	// audit write (SecretReadAuditAdapter.WriteAudit) returned error.
	// T2.3 of plan 2026-06-10-j22-layer2-code-review-fixes (closes HIGH #16
	// silent audit error on cross_tenant_attempt): the prior code path did
	// `_ = AuditWriter.WriteAudit(...)` discarding the error on the most
	// security-critical event (EC-07 violation). Without this counter, audit
	// pipeline failures (e.g., audit_log INSERT timeout, advisory lock
	// contention) would mask forensic evidence of cross-tenant attempts.
	//
	// Cardinality: ONLY label is `event_type` ∈ {secret_read_broker,
	// cross_tenant_attempt} — 2 series total. NO tenant label
	// (would be unbounded with N tenants).
	//
	// Alert opportunity:
	//   rate(loopruntime_secret_audit_write_failed_total{event_type="cross_tenant_attempt"}[5m]) > 0
	//   indicates the security audit trail is breaking for the very events
	//   it exists to record. Page immediately.
	SecretAuditWriteFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_secret_audit_write_failed_total",
		Help: "Broker secret-fetch audit write failures (T2.3 closes HIGH #16).",
	}, []string{"event_type"})

	// SecretDecryptFailedTotal counts failures on the secret READ path.
	//
	// SCOPE — both readers, and the list is the whole list:
	//
	//   - PostgresSecretStore.decryptFromStore
	//     (api/internal/db/store/secret_wrapper.go), the single dispatcher
	//     every decrypt inside the store passes through. Its only non-test
	//     caller is ListForResync, a deploy-time hook.
	//   - rawSecretReader.ReadSecret (api/cmd/setup_secret_fetch.go), the
	//     broker endpoint /internal/v1/secret-fetch/... that External Secrets
	//     Operator calls for EVERY user secret. It queries the secrets table
	//     itself and calls Transit directly — it does NOT go through the store
	//     dispatcher, whatever the InternalSecretReader interface comment used
	//     to claim.
	//
	// The second reader arrived with B-175. Between B-126 and that item this
	// counter covered resync only while reading as though it covered
	// everything, so a revoked key on the path that serves users left the
	// series at zero and an operator would have concluded nothing broke. That
	// is the failure this SCOPE list exists to prevent: when a third reader
	// appears, it is named here or the counter starts lying again.
	//
	// B-126: until this counter existed, a decrypt failure was reported ONLY
	// as a per-request error. A revoked or rotated Transit key
	// (transit/keys/loopruntime-tenants-<id>) makes every ciphertext of that tenant
	// undecryptable at once, and per-request errors do not add up into
	// anything: the failure looked like one user's broken secret, not like a
	// key that is gone. Aggregating the same failures turns that into a
	// visible step change.
	//
	// What this counter does NOT do, said plainly: it still needs somebody to
	// read a secret. A tenant whose secrets are not being read stays silent
	// however revoked its key is. Detection that does not wait for a reader is
	// the canary, deferred with a named trigger — see
	// docs/decisions.md.
	//
	// Cardinality: ONLY label is `reason` ∈ {empty_ciphertext,
	// vault_transit_error, aes_legacy_error} — 3 series total. NO tenant
	// label, for the same reason SecretExternalSecretTotal (EC-3) and
	// SecretAuditWriteFailedTotal state above: it would be unbounded with N
	// tenants. The tenant identity survives in the error chain the caller
	// logs, which is where a free-form, high-cardinality value belongs.
	//
	// The three reasons split the two failures whose responses differ
	// (B-126 DoD bullet 2 — restoring a key and restoring a backup are not
	// the same operation):
	//
	//   - "vault_transit_error"  — Transit refused or was unreachable. Key
	//     side. A revoked key lands here; so does an unreachable OpenBao,
	//     because the store talks to Transit through a consumer-defined
	//     interface and deliberately does not import pkg/vault to tell the
	//     typed errors apart. Distinguishing the two is the operator's first
	//     step, from the logged error, not the metric's.
	//   - "aes_legacy_error"     — a pre-T10.3 AES row would not open. Data
	//     side, or a lost/rotated local AES key. Nothing writes this format
	//     any more (B-162), so a non-zero rate here is a legacy row that has
	//     become unreadable.
	//   - "empty_ciphertext"     — the EC-06 NULL/empty row guard fired. Data
	//     side: a row exists whose encrypted_value is empty, which no write
	//     path in the tree can produce.
	//
	// Alert opportunity:
	//   sum(rate(loopruntime_secret_decrypt_failed_total{reason="vault_transit_error"}[5m])) > 0
	//   means secrets that were readable are not readable now. Page: the
	//   ciphertext is orphaned until the key comes back. The `sum` is what
	//   makes the query cover both readers without naming either.
	SecretDecryptFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_secret_decrypt_failed_total",
		Help: "Secret read-path decrypt failures, by reason (B-126).",
	}, []string{"reason"})

	// WorkqueueTerminalWriteFailTotal counts cases where the workqueue
	// consumer's terminal-state write (Complete or Fail) FAILED — the
	// underlying DB INSERT/UPDATE returned error. Without this metric,
	// such failures are silent: handler.Complete fails → item stays
	// InFlight → re-dequeued → handler runs twice → duplicate side-effects.
	//
	// T1.7 of deep-review-remediation-plan (closes find_workqueue_silent_db_errors HIGH).
	//
	// Label `terminal` ∈ {complete, fail} indicates which path failed:
	//   - "complete" — successful handler followed by FAILED Complete write
	//                  (most dangerous: leads to duplicate handler invocation)
	//   - "fail"     — failed handler followed by FAILED Fail write
	//                  (orphan rows accumulate in workqueue table)
	//
	// Alert opportunity: sum(rate(loopruntime_workqueue_terminal_write_fail_total[5m])) > 0
	// indicates the queue is in an at-least-once-becomes-at-most-once degenerate
	// state and operators should investigate immediately.
	WorkqueueTerminalWriteFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_workqueue_terminal_write_fail_total",
		Help: "Workqueue consumer terminal-state DB write failures (Complete/Fail).",
	}, []string{"terminal"})

	// CellPinnerCRPatchFailTotal counts CellPinner.Pin operations where the
	// PostgreSQL UPDATE on tenants.cell_id succeeded but the subsequent
	// Tenant CR MergePatch FAILED. Result: DB and CR diverged. The operator
	// reads tenant.Spec.CellID from the CR (not the DB) for new
	// EnvironmentReconciler operations, so until the CR is patched manually
	// the tenant's new environments land on the OLD cell.
	//
	// T1.5 of deep-review-remediation-plan (closes find_arch_005 MEDIUM).
	//
	// Alert opportunity: any non-zero rate over 5min indicates divergence.
	CellPinnerCRPatchFailTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_cell_pinner_cr_patch_fail_total",
		Help: "CellPinner DB-updated-but-CR-patch-failed events (DB↔CR divergence window).",
	})

	// ReleaseAppAttestationSaveFailTotal counts failures to persist per-app
	// attestation rows during ContainerDelivery.handle. The release proceeds
	// (best-effort), but the cosign + SBOM provenance is missing from the DB
	// for the affected release. Operators need this signal to detect a
	// regression in the attestation pipeline; previously the error was
	// discarded by `_ = saveErr` (find_code_005 / CODE-005 MEDIUM).
	//
	// T6.5 of deep-review-remediation-plan.
	ReleaseAppAttestationSaveFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_release_attestation_save_fail_total",
		Help: "Per-app attestation persistence failures during release container delivery.",
	}, []string{"environment"})

	// AuditLogPrunerDeletedTotal counts the audit_log rows the retention pruner
	// has DELETEd since process start. T7.3 of deep-review-remediation-plan +
	// ADR-0047. Use rate() to derive prunes/sec; sum() over scrape window to
	// see total volume. Healthy state: trend in flat steps every nightly run.
	AuditLogPrunerDeletedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_audit_log_pruner_deleted_total",
		Help: "audit_log rows deleted by the retention pruner (1-year window per ADR-0047).",
	})

	// AuditLogPrunerFailTotal counts pruner runs that errored during a batch
	// (DB error, missing trigger bypass var, deadlock). Alert opportunity:
	//   increase(loopruntime_audit_log_pruner_fail_total[24h]) > 0
	AuditLogPrunerFailTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_audit_log_pruner_fail_total",
		Help: "audit_log pruner run failures (per batch).",
	})

	// DomainVerifyRateLimitedTotal counts POST /api/v1/domains/{domain}/verify
	// requests rejected by the per-(tenant, domain) rate limiter wired in
	// m3-verify-rate-limit-middleware (J11D7 LOCKED ADR: 1 request / 30s).
	// Label cardinality bounded by tenant count (acceptable per existing
	// metrics.go conventions; revisit at >10k tenants).
	DomainVerifyRateLimitedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_domain_verify_rate_limited_total",
		Help: "Total POST /verify requests rejected by the per-(tenant, domain) rate limiter.",
	}, []string{"tenant"})

	// DomainZoneCleanupFailuresTotal counts compensating DNS-provider calls
	// that failed while setting up a custom domain: a zone the request created
	// and could not delete (operation="delete_zone") is left with no domain
	// pointing at it; a record it could not delete (operation="delete_record")
	// is a conflicting or duplicate record left in the zone.
	DomainZoneCleanupFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_domain_zone_cleanup_failures_total",
		Help: "Total compensating DNS-provider deletes that failed during custom domain setup.",
	}, []string{"operation"})
)
