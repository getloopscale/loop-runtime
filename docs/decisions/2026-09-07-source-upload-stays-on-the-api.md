# Source bundles keep going through the API, and the edge bound is made real

- **Status:** PROPOSED
- **Date:** 2026-09-07
- **Tier:** T2 (`api/internal/` data path, Helm middleware)
- **Source:** system-design audit `SD-14.1` / `SD-15.11`; plan `docs/plans/2026-09-07-source-upload-path-plan.md`

## Context

`POST /api/v1/source/upload` is served by `loopzero-api` itself. Every deploy pushes up to 500 MB
(`SOURCE_MAX_BUNDLE_SIZE`) through a pod limited to 500m CPU / 256Mi across 2 replicas, holding
32 MiB in memory and spilling the rest to pod disk.

The audit's finding was **not** that this is wrong. It was that nothing recorded the choice, so the
intermediary path read as a default nobody revisited — and that no edge bound protected it.

## Decision

**Keep the API as the data path. Record it as a choice. Make the edge bound real.**

Concretely: the Traefik `body-limit` Middleware is raised from 10 MiB to the actual maximum the
application accepts, and attached to the `api.<domain>` route alongside `rate-limit`.

## Why this and not presigned upload

1. **Precedent, on the same question, in this repository.**
   `docs/wiki/discoveries/blueprints/tenant-blob-storage-blueprint.md` § D1 already decided
   proxy-through-API over presign for the customer-facing blob storage, with reasons that apply
   unchanged here: raw S3 presign leaks the endpoint and the bucket, and moves quota enforcement out
   of the request path. Deciding the opposite for a sibling path needs a reason stronger than
   "bytes are large", and there is not one.
2. **One auth surface, one audit point.** The upload is already authenticated and already inside the
   environment lock's blast radius. A presigned URL is a second credential with its own scope,
   expiry and revocation story.
3. **Presign does not exist yet.** The audit claimed twice that "the presign helpers exist and are
   unused". They exist and sign **`GET`**, hardcoded at `api/internal/s3/uploader.go:168`, used for
   SBOM download. There is no PUT presign. Option B is building a signer, not wiring one.
4. **Option B crosses a repository boundary.** The CLI in `../loopzero-cli` would have to upload to the
   URL and call a completion callback. `rules/autonomy-envelope.md` says work that grows past its
   item is registered as new items, not folded in — so B is not a variant of this fix, it is a
   different piece of work.

## Alternatives rejected

- **Presigned PUT (Option B of the plan).** Rejected on (1)–(4). Its trigger is named below.
- **Attach the existing 10 MiB `body-limit` as-is.** Rejected as actively harmful: it would reject
  every deploy over 10 MiB at the edge while the app accepts 500 MB. This is the most likely way to
  break production while "fixing" the finding, and it is why the middleware is currently attached
  for rate-limit only, with that reason written at the attachment site.
- **Leave the route unbounded.** Rejected: it is the status quo and it is the finding. Without an
  edge cap the pod's 256Mi is the only thing between a request and OOM.

## Who is affected, and what would break

- **`../loopzero-cli`** — unaffected. The endpoint, the method and the size ceiling are unchanged; a
  bundle that uploads today uploads tomorrow.
- **`loopzero-api`** — unaffected in code. This is a middleware attachment plus a value change.
- **What changes:** a request larger than the configured maximum is rejected at the edge with 413
  instead of being read into the pod and rejected there.
- **The risk, stated plainly:** if the edge cap is set below `SOURCE_MAX_BUNDLE_SIZE`, legitimate
  deploys fail at the edge with an error the API never sees and cannot explain. The cap and the
  config value must move together; that coupling is the one thing to test.

## What would reverse this decision

Any of:

- A measured mean bundle size that makes the API tier's CPU or memory the deploy bottleneck.
- `loopzero-api` replica count rising for reasons traceable to upload traffic rather than request rate.
- A second consumer of bundles that is not the build plane, making the API an awkward relay.

## Verification

- The edge rejects above the cap; the app still accepts up to `SOURCE_MAX_BUNDLE_SIZE`.
- A test that fails if the two numbers diverge — the coupling in "the risk" above is the assertion
  worth having, and it is the one this ADR would be judged by later.

## Status of implementation

The attachment and the value change need `infra/helm/traefik/middlewares.yaml` added to the
governance `## Approved Scope`; the standing approval covers the IngressRoute but not the Middleware
definition. Until then this ADR records the decision — which is what closes `SD-15.11`, since that
finding was about the absence of a recorded decision, not about the mechanism.
