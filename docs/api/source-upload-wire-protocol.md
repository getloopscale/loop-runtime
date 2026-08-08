---
type: api-reference
title: "`POST /api/v1/source/upload` — Wire Contract"
---

# `POST /api/v1/source/upload` — Wire Contract

**Plan reference:** v1.2 T6.1 (cutover-deep-review-hardening)
**ADRs:** D4 (intermediate two-phase upload mitigation), D9 (multipart field order)
**Updated:** 2026-06-06

## Endpoint

```
POST /api/v1/source/upload
Authorization: Bearer <JWT>
Content-Type: multipart/form-data
```

The route is mounted inside the auth-protected chi group via
`registerProtectedRoutes()`. Both Bearer JWT and the per-tenant membership
check are mandatory.

## Multipart fields (in order)

Order is **part of the contract**. The server peeks the first part name
through `MultipartReader` (without consuming the body) and rejects
violations with HTTP 400 + code `MULTIPART_FIELD_ORDER` so the client can
upgrade.

| Order | Field name      | Type                       | Limit  | Required |
|-------|-----------------|----------------------------|--------|----------|
| 1     | `services_json` | text or file (UTF-8 JSON)  | 256 KB | yes      |
| 2     | `bundle`        | binary (`application/octet-stream`) | 500 MB | yes      |

### `services_json`

- Must be a JSON document validated by
  `api/internal/source/services.schema.json`.
- Supported versions:
  - **v2** (current): top-level `project` is required.
  - **v1** (deprecated, sunset loopzerokit 0.6.0): omits `project`; server
    falls back to the legacy `services-bundle` project name and emits a
    deprecation warning so operators can plan the cutover (`loopzerokit
    migrate services-json-v1-to-v2`).
- Hard cap: 256 KB. Larger payloads return HTTP 413 + code
  `SERVICES_JSON_TOO_LARGE`.

### `bundle`

- A git bundle (`git bundle create -1 HEAD`) OR a tar.gz of a working
  tree.
- Hard cap: 500 MB. Larger payloads return HTTP 413 + code
  `PAYLOAD_TOO_LARGE`.

## Error codes

| HTTP | Code                       | Origin                                           |
|------|----------------------------|--------------------------------------------------|
| 400  | `MULTIPART_FIELD_ORDER`    | Field order violation (bundle before services_json). |
| 400  | `INVALID_TENANT_OR_PROJECT`| DNS-1123 violation downstream (T1.2).            |
| 401  | `UNAUTHORIZED`             | Missing or invalid bearer token.                 |
| 413  | `PAYLOAD_TOO_LARGE`        | Content-Length / bundle exceeds 500 MB.          |
| 413  | `SERVICES_JSON_TOO_LARGE`  | services.json field exceeds 256 KB.              |
| 422  | `VALIDATION_FAILED`        | Schema rejection (E-008 / E-009).                |
| 500  | `SOURCE_UPLOAD_FAILED`     | Gitea push / object-store upload failed.         |

## Why this contract

- **Order:** the server can reject an oversized or malformed
  `services_json` BEFORE it commits to draining the (potentially huge)
  bundle to disk. This is the in-scope mitigation for H2 (deferred
  two-phase upload, ADR D4).
- **Per-field caps:** the global 500 MB body cap is still enforced via
  `http.MaxBytesReader`. The 256 KB cap on `services_json` exists so a
  malicious / buggy client cannot drive the API into OOM territory by
  shipping a tiny bundle and a 400 MB env block.
- **Schema versioning:** clients should bump to v2 immediately to opt
  out of the deprecation warning + secure their Gitea repo name via the
  explicit `project` field.

## CLI invariants

`cli/src/adapters/source-upload.ts`:

```typescript
formData.append('services_json', jsonBlob, 'services.json');
formData.append('bundle', bundleBlob, path.basename(bundlePath));
```

The CLI test
`cli/tests/unit/adapters/source-upload.test.ts` enforces this order via
the `Plan v1.2 T5.1 — preserves error cause` block (the FormData append
order matches the wire spec; deviation would surface as
`MULTIPART_FIELD_ORDER` at the API).

## References

- Plan v1.2 T6.1 — `cutover-deep-review-hardening` plan §Phase 6.
- ADR-0004 — Intermediate mitigation for two-phase upload.
- ADR-0009 — Multipart field order.
