# ADR-0014: Supply-chain attestation wiring (CHG-2026-04-25-ATTESTATION-WIRE)

**Status:** Accepted (expedited path; see governance note)
**Date:** 2026-04-25
**Driver:** End-to-end persistence + retrieval of cosign signature data, closing a gap discovered in the 2026-04-25 E2E session where `image_signed_at` was always NULL despite the cosign step succeeding.

## Context

Three columns on `releases` (`image_digest`, `image_signature`, `image_signed_at`) existed since migration 000001 but **zero Go code read or wrote them**. The endpoint `GET /api/v1/builds/:id/attestation` was wired in the router but the `AttestationStore` interface had no concrete implementation — every call returned 404 even for builds that existed and were signed.

This was confirmed empirically: workflow `loopzero-build-build-dab84f4669bc75d0-926sv` Succeeded with cosign `sign-image-api` Completed, yet release `rel-8ba44af2eb782793` showed `image_signed_at IS NULL`.

The root cause was a complete absence of data flow from Argo workflow → API → DB:
1. The cosign step signed the image but only logged success — no Argo output parameters were declared, so the digest was destroyed when the pod ended.
2. The workflow status parser (`api/internal/argo/workflow_submitter.go:GetStatus`) only extracted `Phase` and `FinishedAt`, not per-node outputs.
3. `AppRelease` had no fields for signature data.
4. `phaseACreateRelease` did not populate the three signature columns when creating the release row.
5. `AttestationStore` had no concrete implementation.

## Decision

Wire the chain end-to-end via 7 phases (the implementation plan lives at `docs/plans/CHG-2026-04-25-ATTESTATION-WIRE-plan.md`):

1. **CWT outputs** — `cosignSignTemplate` writes `signature` (full pinned reference, e.g., `registry/app:tag@sha256:abc`) and `digest` (bare `sha256:abc`) to container-local paths under `/tmp` and declares them as `outputs.parameters`. `buildImageTemplate` writes the BuildKit metadata digest defensively to `/tmp/build-digest.txt`.
2. **WorkflowStatus parser** — `extractStepOutputs(wfObject)` walks `status.nodes[*]`, picks Pod-type nodes only, and indexes outputs by `displayName` (which equals the template name, e.g., `sign-image-api`).
3. **Domain types** — `AppRelease` gains `ImageDigestSHA256`, `ImageSignature`, `ImageSignedAt *time.Time`. `db.Release` gains the same three fields as `*string`/`*time.Time` so NULL stays NULL on legacy rows. A shared `scanReleaseFields` helper centralizes the column list across nine SELECT call sites.
4. **Reconciler enrichment** — `enrichAppReleasesWithAttestation(apps, status)` populates each `AppRelease` from the workflow outputs. `time.Now().UTC()` (API insert time) is used for `ImageSignedAt`, **not** the workflow finish time (see Trust model).
5. **Persistence** — `phaseACreateRelease` reads `result.Apps[0]` and writes the three columns when present. `ImageSignedAt` is set **only when `ImageSignature` is also non-empty** — a timestamp without a verifiable signature reference is meaningless.
6. **AttestationStore impl** — `db.PostgresAttestationStore` reads the latest release for a build (via `ORDER BY created_at DESC LIMIT 1`) and maps the columns to `routes.AttestationInfo`. `GetBuildAttestationForTenant` enforces tenant scoping; cross-tenant reads return `ErrBuildNotFound` (NOT `ErrForbidden`) to avoid existence leaks.
7. **E2E + observability** — smoke script asserts `signed=true` and well-formed sha256 after deploy. `loopzero_release_signed_total{tenant, environment, signed}` counter increments per release insert.

## Trust model

The integrity claim — "this release row's `image_signed_at` corresponds to a real cosign signature for the digest at `image_digest`" — depends on a chain of guarantees:

1. **Cosign output files live in the cosign container's own `/tmp`**, not on `/workspace` (shared with BuildKit). Argo's `outputs.parameters.path` reads from the specific container's filesystem, so no other container in the same pod can forge a signature by writing to that path.
2. **The cosign step exits non-zero if `cosign sign` fails.** When that happens, the script exits before writing the output files, the workflow node reports failure, and downstream sees no outputs (signature persisted as NULL).
3. **The signature reference itself is verifiable client-side.** A consumer of `/attestation` can run `cosign verify` against the registry using the public key and the `image_signature` value to validate the claim. The DB persistence is observability — not a substitute for verification.
4. **`image_signed_at` is set at API insert time**, not workflow finish time. Pod clocks aren't authoritative; the trustworthy timestamp is when the row was committed to PG. This trades clock fidelity for trust integrity.

## Backward compatibility

All three columns are nullable. Workflows pre-this-change emit no output parameters; the parser returns empty maps; the enricher leaves `AppRelease` signature fields at their zero value; `phaseACreateRelease` writes NULL columns; the `/attestation` endpoint returns `signed: false`. **No legacy release is misread as "signed but unverifiable".**

The CHG-2026-04-25-ATTESTATION-WIRE rollout requires the API and the CWT chart to upgrade together — an old API with new CWTs would parse outputs but ignore them; a new API with old CWTs would never see outputs and persist NULLs. Both states are individually safe (no deploys break); only the combination delivers the feature.

## Failure mode policy

Signature persistence is **non-fatal** to the release insert. Rationale: signing-data corruption or a pgx error around the new columns must not break deploys, because the release is the source of truth for the running system; attestation is observability only. If `phaseACreateRelease`'s signature persistence path silently produces NULL columns, the deploy still succeeds — the metric `loopzero_release_signed_total{signed="false"}` and downstream alerts catch the regression.

This was challenged in the (expedited) Production Design review: should Phase 5 fail-fast instead, so a NULL column post-CWT-upgrade is treated as a code bug? **No** — that would couple deploy availability to attestation persistence and violate Rule 6 (every mutation under env lock); attestation is opportunistic data and the current contract is correct.

## Multi-app builds

Today's release row carries one set of signature columns (`image_*`). Multi-app builds persist `Apps[0]`'s signature only — call this the "primary app". The choice of `Apps[0]` is deterministic given the build submission order from CLI, but **arbitrary from a security standpoint**: an admission policy that requires "ALL images for a release are signed" cannot be built on top of this schema as-is.

When admission policy lands (Sprint 6 remainder), this decision must be revisited:
- Option A: extend `releases` with a sidecar table (`release_app_attestations(release_id, app_name, digest, signature, signed_at)`).
- Option B: store the per-app signatures in `releases.artifacts` JSONB (lower migration cost).

Both are out of scope for this change, recorded as a known limitation tracked under "Sprint 6 remainder — admission policy".

## Alternatives rejected

| Alternative | Why rejected |
|---|---|
| Lazy `cosign verify` against the registry per `/attestation` request | Slow (network call per request); creates DOCR-availability dependency in the read path; harder to reason about caching; can't power synchronous admission policy. |
| Log scraping of cosign stdout | Fragile; race-prone; reinvents Argo output params with a worse channel. Violates "Don't reinvent the wheel" — Argo gives us this for free. |
| Pure in-API derivation (`image_signed_at = NOW()` if workflow Succeeded) | Lies about `image_digest` (tag, not sha256). Produces attestation that fails `cosign verify`. Rejected by user as "mentir confortavelmente". |
| Pass workflow finish time as `image_signed_at` | Pod clocks aren't authoritative; multi-cluster sync can drift; trust integrity demands API-side timestamp. |

## Governance note

This change is T2 per pre-release-governance.md (touches API contract + persistence shape + build pipeline simultaneously). It was approved via the **expedited path**, mirroring CHG-2026-04-22-004 (B10 universal TLS): user-authorized in pre-release phase via Ralph loop invocation. Async reviewers (Platform Security, Pipeline Reviewer / Helena Duarte, Production Design Reviewer) sign off post-merge in PR comments.

The expedited path was chosen because:
- The defective state (a route always returning 404) is itself a failed contract — fixing it adds correctness, not novelty.
- The trust model is simple and reviewable in code (no new auth surfaces, no new credentials).
- Backward compatibility is provably safe (NULL stays NULL).
- The plan + 8-phase DoD chain is reviewable BEFORE merge.

## Consequences

- `GET /api/v1/builds/:id/attestation` now returns real data instead of always 404. CI checks against this endpoint that previously got false-negatives will start exercising the real path.
- `loopzero_release_signed_total` metric becomes the canonical observability signal for "is signing actually happening?". An unexpected drop in `signed="true"` indicates a regression in CWT outputs or the workflow status parser.
- Sprint 6 remainder (admission policy that rejects unsigned images) is unblocked from a data-model standpoint. The multi-app limitation must be addressed in that change.
- The CWT contract gains two declared output parameters; pinning Argo Workflows version becomes more important. The chart's `Chart.yaml` should constrain Argo to a known-compatible range.

## Related

- **Plan:** `docs/plans/CHG-2026-04-25-ATTESTATION-WIRE-plan.md`
- **Migration:** `api/internal/db/migrations/000001_initial_schema.up.sql` (columns existed since the beginning)
- **Sprint:** Sprint 6 — Security + Supply chain (cosign signing was the first half; this closes the persistence/observability second half)
- **Predecessors:**
  - ADR-0011 — replicas + plan caps (set the pattern for "API contract + state shape + tests as one change")
  - CHG-2026-04-22-004 — B10 universal TLS (precedent for expedited T2 path)
