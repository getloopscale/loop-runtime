# Plan — move source-bundle upload off the API pod

**Created:** 2026-09-07 · **Source:** system-design audit findings `SD-14.1`, `SD-15.11`, and the
`body-limit` half of `SD-04.6` · **Tier:** T2 (`api/internal/` is an unconditional governance
trigger) · **Status:** DRAFT — needs a decision at § 2 before any code

## 1. What is true today

`POST /api/v1/source/upload` is served by `loopzero-api` itself (`api/internal/server/router.go:359`).
Every deploy pushes its bundle through the pod that also serves the control-plane API:

| Fact | Value | Source |
|---|---|---|
| max bundle | 500 MB | `api/internal/config/config.go:469` |
| held in memory | 32 MiB, rest spills to pod disk | `charts/loopzero-api/values.yaml` `sourceMultipartMaxMemory` |
| pod limits | 500m CPU / 256Mi | `charts/loopzero-api/values.yaml` |
| replicas | 2 | same |
| edge body cap | **none on this route** | `body-limit` Middleware exists and is deliberately unattached |
| consumer | `cwt-clone.yaml:55` reads `{tenant}/source/{build-id}.bundle` from MinIO | |

**A correction the audit had to make about itself, which matters for this plan:** the claim that
"the presign helpers already exist and are unused" is only half true. `Presign` exists twice
(`api/internal/objectstore/s3_adapter.go:35`, `api/internal/s3/uploader.go:134`) and its canonical
request is hardcoded to **`GET`** (`uploader.go:168`). It is used for SBOM download
(`build_reasons/build_sbom.go:140`). **There is no PUT presign.** The work is larger than wiring up
something that already exists.

## 2. The decision this plan cannot make for you

`docs/wiki/discoveries/blueprints/tenant-blob-storage-blueprint.md § D1` already decided
**proxy-through-API over presign** for the customer-facing tenant blob storage, with reasons: raw S3
presign leaks the endpoint and bucket, and moves quota enforcement out of the request path.

Those reasons apply here too. So the question is not "presign is better" — it is whether the source
bundle differs enough from tenant blobs to warrant the opposite answer:

| | tenant blobs (blueprint) | source bundles (this path) |
|---|---|---|
| object size | user-controlled, typically small | up to 500 MB, every deploy |
| frequency | user traffic | 2/day per active tenant (capsim ESTIMATE) |
| quota enforcement | needed per tenant | already bounded by plan tier upstream |
| endpoint exposure | to end users | to the CLI, which is already authenticated |

**Option A — keep proxying, bound it properly.** Accept the API as the data path; attach the
`body-limit` middleware with a cap that reflects the real maximum; keep one auth surface and one
audit point. Cheapest, and consistent with the blueprint.

**Option B — presigned PUT for this path only.** Extend the signer to sign `PUT` with object,
method, size and expiry constrained; the CLI uploads straight to the store; the API keeps the
metadata write and a completion callback. Removes bulk bytes from the API tier.

Both are defensible. **Whichever is chosen must be recorded as an ADR**, because the finding was
never "this is wrong" — it was that nothing recorded the choice.

## 3. Work — Option A

- **A1.** Decide the real maximum bundle size from measurement, not from the current 500 MB literal.
  `mc ls --recursive local/loopzero-builds` gives the distribution.
- **A2.** Set the Traefik `body-limit` `maxRequestBodyBytes` to that value and attach it to the
  `api.<domain>` route alongside `rate-limit` (`infra/helm/loopzero-api/templates/ingressroute.yaml`, one
  line — the comment there already explains why it is currently absent).
- **A3.** Test: a request above the cap is rejected at the edge with 413, not by the pod.
- **A4.** ADR recording that the API is deliberately the data path, citing the blueprint's reasoning
  and the size measurement.

## 4. Work — Option B

- **B1.** Extend `Uploader.Presign` to take an HTTP method. The canonical request hardcodes `GET`
  at `uploader.go:168`; `PUT` needs the method threaded through and the signature recomputed. **RED
  first:** a test asserting a `PUT`-presigned URL round-trips against MinIO.
- **B2.** Constrain the signature: object key, method, `content-length-range`, short expiry. An
  unconstrained presign is the leak the blueprint rejected.
- **B3.** New endpoint returning the presigned PUT plus the key the build will read.
- **B4.** Completion callback: the API records the bundle only after the object exists, so a failed
  upload cannot leave a build pointing at a missing key.
- **B5.** CLI change in `../loopzero-cli` — **this crosses a repository boundary** and is why B is not a
  local change. The CLI must upload to the URL and then call the callback.
- **B6.** Keep the proxy path until the CLI floor supports the new one; delete it after.
- **B7.** ADR recording the divergence from the blueprint, with the size/frequency reasons.

## 5. What must not happen

- **Attaching `body-limit` at its current 10 MiB while the proxy path is live.** It would reject
  every deploy over 10 MiB at the edge. This is called out in the ingressroute comment and is the
  single most likely way to break production while "fixing" a finding.
- **Presigning without constraints.** A URL that permits any object or any size is worse than the
  proxy it replaces.
- **Shipping B without the CLI.** The endpoint would exist and nothing would call it — the exact
  defect class this audit spent its length reporting.

## 6. Related

- Audit: `system-design-output/final_report.md` — `SD-14.1`, `SD-15.11`, `SD-04.6`
- Draft ADR: `system-design-output/adr-suggestions/0002-extend-the-blob-storage-upload-decision.md`
- Prior decision for the sibling subsystem: `docs/wiki/discoveries/blueprints/tenant-blob-storage-blueprint.md` § D1
