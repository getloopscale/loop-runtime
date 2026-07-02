# LoopZero Engine — Codebase Architecture Audit

**Target:** `/home/paulo/Projetos/loopzero/loopzero-cloud/loopzero` (Go monorepo — control plane engine)
**Mode:** full (phases 1–6) · **Severity threshold:** medium · **Date:** 2026-07-02
**Auditor:** loop-codebase-architect (chief-architect-organizer + specialists)
**Source of truth:** `architect-output/codebase-architect.db`

---

## 1. Executive summary

The LoopZero engine is a **structurally sound, pragmatically-layered Go monorepo** mid-way through a
deliberate decomposition into `loopzero-cli` / `loopzero-cloud` / `loopzero-contracts` (per its ADR + ROADMAP).
The dependency graph is **acyclic** (compile-verified), the **domain layer is clean** (18 DIP
interfaces, zero infrastructure leakage), and every design pattern in use (Repository, DIP, State
Machine, Adapter, Strategy, Composition-Root) is **applied correctly with no pattern theater**.

The weaknesses are **localized and low-risk**: a handful of oversized flat packages, one god file,
and a placement/boundary issue where 14.6k LOC of CI tooling lives inside the module designated as
the stable shared-contracts base. None are architectural defects; all are behavior-preserving
cleanups best done **before** the `loopzero-contracts` extraction cut.

**Overall score: 77 / 100 → Verdict: `REFACTOR LIGHTLY`.**

> Intentional design decisions were respected and NOT flagged: the dual Helm chart system
> (`infra/helm/` bootstrap vs `charts/` published), split operator binaries, and decomposition
> seams (`go.work` already using `../loopzero-contracts`, `cloud/` already extracted).

---

## 2. Architecture score (0–100)

| # | Dimension | Weight | Score | % | Key finding |
|---|---|---|---|---|---|
| 1 | Folder Clarity | 20 | 15 | 75% | Screaming top-level, but 3 flat god-packages + generic `shared/` |
| 2 | Cohesion | 20 | 13 | 65% | ~38k LOC in 3 flat packages (db/services/routes) |
| 3 | Coupling | 20 | 17 | 85% | **Acyclic graph + clean domain = strength** |
| 4 | Pattern Fit | 15 | 13 | 87% | Pragmatic patterns applied correctly |
| 5 | Testability | 10 | 8 | 80% | Excellent DIP-driven test seams |
| 6 | Scalability | 10 | 7 | 70% | Extension seams good; flat packages are the drag |
| 7 | Onboarding Clarity | 5 | 4 | 80% | Screaming top-level offset by flat mega-packages |
| | **Total** | **100** | **77** | | **REFACTOR LIGHTLY** |

**Verdict bands:** Keep (85–100) · **Refactor Lightly (70–84)** · Refactor Boundaries (50–69) · Major Cleanup (<50).

---

## 3. Detected architecture

Pragmatic **layered Go monorepo** (multi-module `go.work`):

```
interface       api/internal/routes (41) · api/internal/middleware (21) · api/internal/server
      ↓
application      api/internal/services (56) · operators/internal/{tenant,app,preview,platform}
      ↓
domain           api/internal/domain (18 interfaces) · pkg/statemachine (11 machines)
      ↑
infrastructure   api/internal/{db,argo,argocd,manifests,github,k8s,cfzone,s3,loki} · infra/
shared           pkg/{errors,naming,enforcement,vault,conditions,id,logging,...}
tooling          pkg/quality (77 gates) · cmd/loopzero-ops
```

- **Modules registered:** 28 · **Go modules (`go.work`):** api, operators, operators/api, pkg,
  cmd/loopzero-ops, infra/scripts, infra/tests, + external `../loopzero-contracts`.
- **Cycle status:** `go build ./...` passes with **zero import cycles** (Go compile-enforced).

**Tooling limitation (honest):** graph tools (goda/scc/tokei/madge) are ABSENT. LOC + import graph
were derived from `go build`, `wc -l`, and `grep` on import blocks. Cycle-freedom is authoritative
(compiler); LOC counts are whitespace-inclusive `wc` figures. Install `goda`+`scc` to quantify
Ca/Ce/instability and cyclomatic complexity in a future run.

---

## 4. Top findings (medium+, evidence-backed)

| # | Dim | Severity | Conf | Finding | Evidence |
|---|---|---|---|---|---|
| 1 | cohesion | medium | 90% | **Three oversized flat packages (~38k LOC)** | `api/internal/db` 70 files/14.8k · `services` 56 files/12.3k · `routes` 41 files/10.8k |
| 2 | cohesion | medium | 90% | **`quality_subcmd.go` god file** wiring all 77 gates | `cmd/loopzero-ops/quality_subcmd.go` = 2909 LOC (largest hand-written file) |
| 3 | boundaries | medium | 85% | **CI tooling inside the shared-contracts base** | `pkg/quality` = 14.6k LOC (~59% of pkg module), consumed only by cmd/loopzero-ops |
| 4 | naming | medium | 85% | **Generic god-folder** | `operators/internal/shared` = 800 LOC/9 files under generic name |
| 5 | cohesion | medium | 82% | **`tenant` reconciles two CRDs** (Tenant + Environment) | `operators/internal/tenant` = 3.3k LOC/13 files incl `environment_controller.go` |
| 6 | coupling | medium | 80% | Gates hardcode app-internal import paths as strings | `pkg/quality/acyclicgraph/check.go` scans literal `loopzero/api/internal/...` |

### Structural strengths (recorded honestly)

- **No import cycles** anywhere (compile-verified) — and the `pkg/quality` string matches of
  `api/operators` paths were verified to be **search-pattern literals**, not real upward imports
  (crying-wolf avoided).
- **Clean domain layer** — `api/internal/domain` imports no infrastructure; 18 interfaces
  (`ProjectStore`, `SecretStore`, `CFZoneManager`, `CellPlacementEngine`, `LogQuerier`, …) form the
  DIP seams that infrastructure implements. Correct dependency direction.
- **Patterns applied correctly, no theater** — Repository over sqlc, State Machine (11 machines,
  invalid-transition-panics), Strategy (`LanguageStrategy`/`ResolveStrategyForApp`), Adapter (argo/
  argocd/github/cf/s3/loki), Composition-Root (`server/router.go`). Full Hexagonal wisely NOT
  adopted (`missing_not_needed`).

---

## 5. Cohesion analysis (1–5 per key module)

| Module | LOC | Files | Cohesion | Note |
|---|---|---|---|---|
| `pkg/statemachine` | 2.9k | 27 | 5 | 11 machines, one concern |
| `pkg/errors` · `pkg/naming` | — | — | 5 | Single-responsibility shared libs |
| `api/internal/domain` | 799 | 6 | 4 | Interfaces + model, clean |
| `api/internal/argo` · `argocd` · `manifests` · `github` | — | — | 4 | Bounded adapters |
| `pkg/quality` | 14.6k | 78 | 4 | Good per-gate cohesion, **wrong placement** |
| `api/internal/db` | 14.8k | 70 | 3 | Flat: generated + wrappers + hand queries mixed |
| `api/internal/services` | 12.3k | 56 | 3 | Flat: 86 structs, no domain grouping |
| `api/internal/routes` | 10.8k | 41 | 3 | Flat handler namespace |
| `operators/internal/tenant` | 3.3k | 13 | 3 | Two CRD controllers in one package |
| `cmd/loopzero-ops` | 3.2k | 5 | 2 | 2909-LOC god file dominates |

---

## 6. Coupling & boundary analysis

- **Circular deps:** none (authoritative — `go build ./...`).
- **Direction:** correct — domain depends inward; adapters implement domain interfaces; composition
  root wires concretes at the top.
- **Soft coupling (medium):** `pkg/quality` gates embed app-internal import paths as string
  literals → the shared-lib module carries app-layout knowledge; an `api/internal` rename silently
  breaks a `pkg/` gate.
- **Boundary misfit (medium):** tooling (`pkg/quality`) sits in the shared-contracts layer that
  ROADMAP designates as `loopzero-contracts` (imports nothing). Extraction would drag CI-only code into
  the contracts module.

---

## 7. Proposed structure (behavior-preserving)

Keep the layered monorepo. Three targeted interventions before the `loopzero-contracts` cut:

1. **Break the flat god-packages** — `api/internal/db → db/{generated,repository,store}` by
   aggregate; `api/internal/services → services/{build,release,deploy,tenant,…}` by domain.
2. **Relocate tooling out of the shared base** — `pkg/quality → tools/quality-gates` (or
   `cmd/loopzero-ops/internal/gates`); externalize scanned paths to config.
3. **Split mixed-responsibility / rename generics** — extract `operators/internal/environment`
   from `tenant`; rename `operators/internal/shared`.

No layer redesign, no new patterns, no big-bang rewrite.

---

## 8. Incremental migration plan (8 steps, safe-first)

| # | Type | Step | Behavior | Risk | Validation |
|---|---|---|---|---|---|
| 1 | rename | `operators/internal/shared` → `reconcilehelpers` | none | low | `go build ./... && go test ./...` |
| 2 | split | extract `operators/internal/environment` from tenant | none | low | `go build ./... && go test ./internal/environment/...` |
| 3 | move | relocate `pkg/quality` out of shared base | none | medium | `go build ./... && task quality:gates` |
| 4 | rewire | rewire gate importers + externalize scanned paths | none | medium | `task quality:gates` |
| 5 | split | `api/internal/db` → generated/repository/store | none | medium | `cd api && go test ./internal/db/...` |
| 6 | split | group `api/internal/services` by domain | none | medium | `cd api && go test ./internal/services/...` |
| 7 | pattern | `quality_subcmd.go` → per-gate self-registration | minor | low | `task quality:gates` |
| 8 | verify | full verification after each step | none | low | `go build ./... && go test ./... && task quality:gates && task lint` |

7 of 8 steps are `behavior_change=none` (import-path-only). Step 7 changes the gate *registration
mechanism* only; gate behavior is identical.

---

## 9. Validation checklist

- [ ] `go build ./...` green after every step
- [ ] `go test ./...` green after every step
- [ ] `task quality:gates` green (steps 3, 4, 7 especially)
- [ ] `task lint` clean
- [ ] No new import cycles (`go build` is authoritative)
- [ ] `pkg/` LOC drops materially after step 3 (contracts base stays lean)

---

## 10. Final verdict

### `REFACTOR LIGHTLY` — score 77/100

The engine is architecturally healthy: acyclic, correctly layered, well-patterned, testable. The
findings are **maintainability polish, not structural debt** — a small set of behavior-preserving
package splits and one tooling relocation that also de-risks the imminent `loopzero-contracts`
extraction. Prioritize steps 1–4 (low/medium risk, high leverage before the contracts cut); steps
5–6 (db/services splits) are medium-risk import-rewiring worth scheduling deliberately.

---

*All findings persisted in `architect-output/codebase-architect.db`. This report is generated from
that database (the source of truth).*
