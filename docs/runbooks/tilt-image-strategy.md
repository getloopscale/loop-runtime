# Tilt Image Pinning Strategy

**Plan:** docs/plans/deep-review-remediation-plan.md (T8.3 + EC-16)
**Closes (with EC-16 documentation):** find_infra_005 MEDIUM

---

## Tension

Tilt offers `live_update` for hot-reload during dev. This requires images
that **change frequently** (the API binary on every Go file save). But
pinning images for reproducibility (T8.3) requires images that **don't
change without explicit version bump**.

Both goals are correct in their own scope. The split:

## Pinning rule per image class

| Image class | Strategy | Examples |
|---|---|---|
| **Service deployments** (long-running) | `:latest` against k3d-loopzero-registry, with Tilt `live_update` | loopzero-api, loopzero-tenant-operator, loopzero-app-operator, loopzero-preview-operator, loopzero-platform-operator |
| **Pipeline-step images** (batch jobs in Argo Workflows) | Pinned tag (e.g., `:v0.1.0`) | loopzero-clone-runner, loopzero-syft-scanner, loopzero-cosign-signer, loopzero-dockerfile-lint, loopzero-packs-runner |
| **Third-party** (PostgreSQL, Gitea, MinIO, etc.) | Pinned via Helm chart values | postgresql:16, gitea:1.22.6 |

## Why this works

- Service deployments live behind `Deployment` controllers with rolling
  update. Tilt's `live_update` watches files, rebuilds locally, and pushes
  the new image into k3d-loopzero-registry as `:latest`. The Deployment's
  `imagePullPolicy: Always` ensures the new image is pulled. Tag mutability
  is the **feature** — that's what enables hot-reload.
- Pipeline-step images run inside Argo Workflows submitted at deploy time.
  They are referenced by env-var name (e.g., `LOOPZERO_CLONE_IMAGE`) which is
  resolved by the API at workflow submit. If a developer accidentally
  rebuilds `loopzero-clone-runner:latest` locally and pushes a broken version,
  EVERY subsequent deploy in their cluster fails — and the failure mode is
  opaque (Argo Workflow step exits non-zero, no link to "you rebuilt the
  step image"). Pinning eliminates this footgun.

## When to bump the pinned tags

Pipeline-step images are versioned via the image's own Dockerfile + a
Taskfile target (`task images:build:clone-runner` etc.). Bump procedure:

1. Increment the image's version tag (e.g., v0.1.0 → v0.1.1).
2. Build + push: `task images:build:clone-runner TAG=v0.1.1`.
3. Update `infra/tilt/api-deployment.yaml` env var to the new tag.
4. Commit both changes in the same PR (image build + Tilt manifest).

## Production parity

In production (DigitalOcean), the API references DOCR-published images
with explicit tags via Pulumi config (`infra/pulumi/src/control-plane.ts`
or equivalent). Tilt's pinning matches the production model — the
`:latest` deviation is local-dev-only and applies ONLY to service
deployments where Tilt's `live_update` is the explicit value-add.

## References

- find_infra_005 (deep-review-remediation Phase 5)
- T8.3 + EC-16 of docs/plans/deep-review-remediation-plan.md
- CLAUDE.md Rule 10 — "Local equals production"
