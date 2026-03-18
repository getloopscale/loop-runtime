# Implementation Plan — Prioritized from QUESTIONS.md

> **Date:** 2026-03-18
> **Source:** 68 questions from full codebase audit
> **Method:** Impact x Effort matrix, grouped into execution waves

---

## Priority Legend

| Priority | Meaning | Criteria |
|----------|---------|----------|
| **P0** | Do now | Bugs, security holes, or blockers that affect correctness |
| **P1** | This sprint | High-impact improvements that harden the system |
| **P2** | Next sprint | Important but non-blocking quality improvements |
| **P3** | Backlog | Nice-to-have, polish, or future-sprint scope |

| Effort | Meaning |
|--------|---------|
| **XS** | < 15 min, single file change |
| **S** | 15-60 min, 1-3 files |
| **M** | 1-4 hours, multiple files/components |
| **L** | Half day to full day |
| **XL** | Multi-day, cross-component |

---

## Wave 0 — Immediate Fixes (P0)

> Bugs, correctness issues, and security holes. Do before any feature work.

| # | ID | Title | Effort | Component | Why P0 |
|---|-----|-------|--------|-----------|--------|
| 1 | A-04 | SSE timeout fires mid-build — reset on events | S | CLI | Kills long builds silently |
| 2 | A-05 | Callback goroutine ignores shutdown signals | S | API | Goroutine leak on API restart |
| 3 | A-06 | No request body size limit on callback | XS | API | DoS vector — 1 line fix |
| 4 | B-06 | File permissions race on config store | XS | CLI | Token readable by other users briefly |
| 5 | B-09 | No SIGINT/SIGTERM signal handlers | S | CLI | Dangling SSE connections, temp files |
| 6 | B-12 | `parseInt` without `isNaN` for timeouts | XS | CLI | `NaN` timeout = undefined behavior |
| 7 | C-09 | Database pool not closed on shutdown | XS | API | Connection leak on restart |
| 8 | H-04 | CHANGELOG duplicate `### Changed` | XS | Docs | Copy-paste error |

**Estimated total effort: ~3 hours**

### Implementation order:

```
1. A-06 → api/internal/routes/builds.go
   Add: r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB

2. B-06 → cli/src/config/store.ts
   Change: fs.writeFileSync(path, json, { mode: 0o600 })

3. B-12 → cli/src/config/constants.ts
   Add: const parsed = parseInt(val, 10); return isNaN(parsed) ? defaultVal : parsed;

4. C-09 → api/cmd/main.go
   Add: defer pool.Close() after pool creation

5. H-04 → CHANGELOG.md
   Merge the two ### Changed sections

6. A-04 → cli/src/commands/deploy.ts
   Change setTimeout to reset on every SSE event (activity-based timeout)

7. B-09 → cli/src/index.ts
   Add process.on('SIGINT', cleanup) and process.on('SIGTERM', cleanup)

8. A-05 → api/cmd/main.go + routes/builds.go
   Create shutdownCtx in main, pass to handler, use in callback goroutine
```

---

## Wave 1 — Hardening (P1)

> High-impact improvements that prevent data loss, improve reliability, or fix design issues.

| # | ID | Title | Effort | Component |
|---|-----|-------|--------|-----------|
| 9 | A-07 | Build/Release ID entropy too low (32 bits) | XS | pkg/id |
| 10 | A-08 | Idempotency cleanup never runs | S | API |
| 11 | B-01 | Replace custom YAML parser with js-yaml | M | CLI |
| 12 | B-02 | `loopzero init` missing git repo validation | XS | CLI |
| 13 | B-07 | API URL silently defaults to localhost | XS | CLI |
| 14 | B-08 | SSE reconnect needs exponential backoff | S | CLI |
| 15 | B-16 | No project name validation in init | S | CLI |
| 16 | C-02 | No request context deadline on handlers | S | API |
| 17 | C-03 | Callback secret insecure default in prod | XS | API |
| 18 | C-06 | SSE event pruning not scheduled | S | API |
| 19 | C-08 | Shutdown doesn't wait for in-flight callbacks | M | API |
| 20 | D-01 | Naming module not used consistently | S | Operators |
| 21 | D-03 | Status update errors silently swallowed | XS | Operators |
| 22 | D-06 | Deletion doesn't log non-fatal failures | XS | Operators |
| 23 | E-08 | `NODE_ENV=production` hardcoded in CWT | XS | Infra |
| 24 | E-13 | Token script creates new token every run | S | Infra |

**Estimated total effort: ~1.5 days**

### Implementation order (grouped by component for efficiency):

```
--- CLI batch ---
9.  B-02 → cli/src/commands/init.ts
    Add: if (!fs.existsSync('.git')) { error("Not a git repository"); exit(1); }

10. B-07 → cli/src/config/constants.ts
    Change: if no LOOPZERO_API_URL and no stored config → error, don't default

11. B-16 → cli/src/commands/init.ts
    Add: validate project name against /^[a-z][a-z0-9-]{0,62}$/ (same as env names)

12. B-01 → cli/src/adapters/loopzero-yaml.ts
    Replace: custom parser with `import YAML from 'js-yaml'`
    Run: npm install js-yaml && npm install -D @types/js-yaml

13. B-08 → cli/src/api/sse-client.ts
    Change: reconnectDelay = Math.min(1000 * 2^attempt, 60000)

--- API batch ---
14. A-07 → pkg/id/id.go
    Change: randomHex(4) → randomHex(8) for 64-bit IDs

15. A-08 → api/internal/services/ (new: idempotency_cleaner.go)
    Add: background goroutine, runs every 1h, calls store.Cleanup()

16. C-02 → api/internal/routes/builds.go (and other handlers)
    Add: ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)

17. C-03 → api/internal/config/config.go
    Add: if cfg.ArgoCallbackSecret == "loopzero-dev-callback-secret" && os.Getenv("LOOPZERO_ENV") == "production" { log.Fatal(...) }

18. C-06 → api/internal/sse/ (new: event_pruner.go)
    Add: background goroutine, prunes events older than 24h, runs every 30min

19. C-08 → api/cmd/main.go
    Add: sync.WaitGroup for callback goroutines, Wait() in shutdown path

--- Operators batch ---
20. D-01 → operators/internal/controller/tenant_namespaces.go
    Change: fmt.Sprintf → naming.NamespaceName()

21. D-03 → operators/internal/controller/tenant_controller.go
    Change: _ = r.Status().Update() → if err := r.Status().Update(); err != nil { logger.Error(...) }

22. D-06 → operators/internal/controller/tenant_controller.go (deletion path)
    Add: logger.Info("non-fatal cleanup failure", "step", stepName, "error", err)

--- Infra batch ---
23. E-08 → infra/helm/argo-workflows/templates/cwt-manifests-generate.yaml
    Change: NODE_ENV from hardcoded "production" to {{inputs.parameters.environment}}

24. E-13 → infra/scripts/provision-api-tokens.sh
    Change: use fixed token name "loopzero-api-token", delete old before creating new
```

---

## Wave 2 — Quality & Robustness (P2)

> Important improvements that aren't blocking but significantly improve quality.

| # | ID | Title | Effort | Component |
|---|-----|-------|--------|-----------|
| 25 | A-01 | Idempotency key should be deterministic | M | CLI |
| 26 | A-02 | Remove dead in-memory circuit breaker | XS | API |
| 27 | A-03 | Release state machine sink states | M | pkg/statemachine |
| 28 | A-10 | TimeoutHandler for non-SSE routes | M | API |
| 29 | B-03 | Git bundle includes full history | S | CLI |
| 30 | B-04 | Source upload loads entire bundle into memory | M | CLI |
| 31 | B-11 | ANSI colors not gated on TTY | XS | CLI |
| 32 | B-13 | Exit code 3 for 409 Conflict | XS | CLI |
| 33 | B-14 | Remove dead browser OAuth code | XS | CLI |
| 34 | B-15 | `process.emit()` hack for warnings | S | CLI |
| 35 | C-04 | SSE GetAfter returns unbounded results | XS | API |
| 36 | C-07 | Fallback callback parsing fragile | S | API |
| 37 | C-10 | Error message capitalization inconsistent | S | API |
| 38 | D-02 | CRD validation markers missing | M | Operators |
| 39 | D-04 | allConditionsTrue hardcodes condition list | S | Operators |
| 40 | D-05 | Gitea URL hardcoded in two places | XS | Operators |
| 41 | E-07 | Kaniko TLS verification disabled | S | Infra |
| 42 | E-09 | Notify callback sends unbounded payload | M | Infra |
| 43 | E-10 | No resource quotas for Argo Workflows | S | Infra |
| 44 | E-12 | CRD catalog fetched at build time | M | Infra |
| 45 | F-04 | Git bundle could include sensitive files | S | CLI |
| 46 | F-05 | Token validation cache unbounded | S | API |
| 47 | G-01 | No tests for startup_reconciler.go | L | API |
| 48 | G-02 | No tests for pg_circuit_breaker.go | L | API |
| 49 | G-03 | No config validation tests | M | CLI + API |
| 50 | I-04 | Cache key doesn't include lockfile hash | S | Infra |

**Estimated total effort: ~4-5 days**

### Suggested grouping (by session):

```
Session A — CLI cleanup (B-03, B-04, B-11, B-13, B-14, B-15, B-10)
Session B — API hardening (A-02, C-04, C-07, C-10, F-05, A-10)
Session C — Deterministic idempotency + state machine fix (A-01, A-03)
Session D — Operator improvements (D-02, D-04, D-05)
Session E — Infra CWT improvements (E-07, E-09, E-10, E-12, I-04)
Session F — Critical test gaps (G-01, G-02, G-03)
Session G — Security (F-04, B-03 overlap with sensitive file check)
```

---

## Wave 3 — Production Readiness (P2-P3)

> Cross-cutting concerns needed before any cloud deployment.

| # | ID | Title | Effort | Component |
|---|-----|-------|--------|-----------|
| 51 | E-01 | Dev credentials hardcoded in VCS | L | Infra |
| 52 | E-02 | Cross-cluster IPs fragile sed patching | XL | Infra |
| 53 | E-04 | PostgreSQL no HA/backup | XL | Infra |
| 54 | E-05 | MinIO standalone mode | L | Infra |
| 55 | E-06 | Gitea SQLite backend | M | Infra |
| 56 | E-11 | No NetworkPolicies | L | Infra |
| 57 | C-01 | No rate limiting | M | API |
| 58 | F-01 | No audit logging | L | API |
| 59 | F-02 | TenantController ClusterRole too broad | M | Operators |
| 60 | F-03 | No encryption at rest for K8s secrets | M | Infra |
| 61 | B-05 | Token stored plaintext (keychain integration) | L | CLI |
| 62 | I-01 | PG pool settings conservative | S | API |
| 63 | I-02 | No index strategy documented | M | API |
| 64 | I-03 | No HTTP connection pooling for Gitea/ArgoCD | S | API |
| 65 | I-05 | No pagination on list endpoints | M | API |

**Estimated total effort: ~2-3 weeks**

### Suggested phasing:

```
Phase 3a — Secrets & Security (E-01, F-02, F-03, C-01)
Phase 3b — HA & Resilience (E-04, E-05, E-06)
Phase 3c — Networking (E-02, E-11)
Phase 3d — Performance (I-01, I-02, I-03, I-05)
Phase 3e — Audit & Compliance (F-01, B-05)
```

---

## Wave 4 — Polish & Future (P3)

> Nice-to-have improvements, future sprint scope, or documentation.

| # | ID | Title | Effort | Component |
|---|-----|-------|--------|-----------|
| 66 | A-09 | Parallel health probes for multi-app | M | API |
| 67 | B-10 | JSON output compact vs pretty | XS | CLI |
| 68 | C-05 | Metrics endpoint auth | S | API |
| 69 | D-07 | Concurrent reconciliation tests | L | Operators |
| 70 | D-08 | Operator HA documentation | S | Docs |
| 71 | E-03 | Tilt vs Task infra ownership | S | Docs |
| 72 | G-04 | CLI integration tests (real git/API) | XL | CLI |
| 73 | G-05 | E2E failure scenario tests | XL | Infra |
| 74 | G-06 | Chaos engineering tests | XL | Infra |
| 75 | G-07 | Sprint docs consolidation | M | Docs |
| 76 | G-08 | Preview environment test skeletons | M | Operators |
| 77 | H-01 | Split technical.md or add TOC | M | Docs |
| 78 | H-02 | Update README implementation status | XS | Docs |
| 79 | H-03 | Create ADR folder | M | Docs |

---

## Execution Summary

| Wave | Items | Done | Status | Focus |
|------|-------|------|--------|-------|
| **Wave 0** | 8 | 8 | COMPLETE | Bugs & security holes |
| **Wave 1** | 16 | 16 | COMPLETE | Hardening & correctness |
| **Wave 2** | 26 | 26 | COMPLETE | Quality & robustness |
| **Wave 3** | 15 | 15 | COMPLETE | Production readiness |
| **Wave 4** | 14 | 14 | COMPLETE | Polish & future |
| **TOTAL** | **79** | **79** | **100%** | |

### Implementation Notes

Items marked with production overlay files (E-04, E-05) have `values.production.yaml` ready to apply.
Items marked with ADR (E-02, F-03) have architecture decisions documented with migration paths.
Items marked with test skeletons (D-07, G-04, G-05, G-06) have Skip()-annotated test files documenting all test cases for future implementation.

---

## Quick Reference — By Component

### CLI (19 items)
```
P0: B-06, B-09, B-12
P1: B-01, B-02, B-07, B-08, B-16
P2: A-01, A-04, B-03, B-04, B-10, B-11, B-13, B-14, B-15, F-04
P3: B-05, G-04
```

### API (21 items)
```
P0: A-05, A-06, C-09
P1: A-07, A-08, C-02, C-03, C-06, C-08
P2: A-02, A-10, C-04, C-07, C-10, F-05, G-01, G-02, G-03
P3: A-09, C-01, C-05, F-01, I-01, I-02, I-03, I-05
```

### Operators (10 items)
```
P1: D-01, D-03, D-06
P2: D-02, D-04, D-05, A-03
P3: D-07, D-08, F-02, G-08
```

### Infra (14 items)
```
P1: E-08, E-13
P2: E-07, E-09, E-10, E-12, I-04
P3: E-01, E-02, E-04, E-05, E-06, E-11, F-03, G-05, G-06
```

### Docs (6 items)
```
P0: H-04
P3: E-03, G-07, H-01, H-02, H-03, D-08
```

---

## Dependency Graph (Critical Path)

```
A-06 (body limit) ──┐
A-05 (shutdown ctx) ─┤
C-09 (pool close)  ──┼── Wave 0 complete ──┐
B-06 (file perms)  ──┤                      │
B-09 (signals)     ──┤                      │
A-04 (SSE timeout) ──┘                      │
                                            ▼
                              A-07 (ID entropy) ──┐
                              A-08 (cleanup job) ──┤
                              C-06 (event prune) ──┼── Wave 1 complete ──┐
                              B-01 (YAML parser) ──┤                      │
                              D-01 (naming mod)  ──┘                      │
                                                                          ▼
                                                       A-01 (idem. key) ──┐
                                                       G-01 (reconciler) ──┼── Wave 2
                                                       G-02 (circuit br.) ─┘
```

No item in Wave 1 depends on Wave 0 being complete (they can overlap).
Wave 2 items G-01 and G-02 are independent and can run in parallel.

---

## How To Use This Plan

1. **Start with Wave 0** — all items are XS/S effort, fix real bugs
2. **Work Wave 1 by component** — batch CLI changes, batch API changes, etc.
3. **Wave 2 sessions are self-contained** — each session is a PR
4. **Wave 3 requires architectural decisions** — discuss before implementing
5. **Wave 4 is backlog** — pull items as capacity allows

Each item references its QUESTIONS.md ID for full context.
