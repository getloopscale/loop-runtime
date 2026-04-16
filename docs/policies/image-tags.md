# Image Tag Policy

**Status:** enforced policy
**Owner:** Infrastructure + Platform
**Roadmap:** Item 01 Correction H

---

## Rule

**All container images deployed to DigitalOcean MUST be referenced by an
immutable, content-addressable tag.** The two acceptable forms are:

1. **Git SHA tags** — e.g. `registry.digitalocean.com/loopzero-infra-registry/loopzero-api:81ae4e3`
2. **SHA256 digests** — e.g. `registry.digitalocean.com/loopzero-infra-registry/loopzero-api@sha256:589c25725970fb3b2c42311eeb203cf9ce2533e70f10a1534cc73343afa664d6`

**The `:dev`, `:latest`, and `:main` tags are forbidden as deploy references**
because they are mutable and break cache invalidation on DOKS nodes.

---

## Why

On a DOKS cluster the kubelet caches images by `<repo>:<tag>` identity. When
the tag is mutable (`:dev`), the kubelet does not know that the underlying
SHA changed and will serve the stale image even with `imagePullPolicy: Always`
— because `Always` only guarantees a pull attempt, not a bypass of the
node-level content cache when the manifest digest is unchanged.

This was the root cause of the 2026-04-09 L1 load-test incident
(Correction H in `infra/tests/load/CORRECTIONS.txt`): a new API image was
pushed under `:dev`, and several runtime pods kept serving the previous
image until the deployment was forcibly updated with
`kubectl set image ...:<sha>`.

A SHA-based tag is self-invalidating: a new commit produces a new tag, the
kubelet sees a new identity, and the pull path is exercised deterministically.

## What CI must do

- **`build-images.yml`** pushes every API / operator / dashboard image
  under the commit SHA (short form, 7+ hex chars). The `:dev` floating tag
  may still be pushed for humans to read, but production Deployments must
  not reference it.
- **Pulumi values** reference images via a Pulumi stack config key
  (e.g. `loopzero:apiImageTag`) that is set by the CI workflow to the commit
  SHA on each merge. Never hard-code `:dev`.
- **Manual image pushes** (roadmap 100k iter 4 used
  `REDACTED_SECRET_054`) must also be SHA-suffixed.

## What runtime deployments must do

- `loopzero-api`, `tenant-controller`, `loopzero-cloud`, `loopzero-dashboard`, and the
  Argo Workflow CWTs (`loopzero-clone-runner`, `loopzero-packs-runner`,
  `loopzero-buildkit`, `loopzero-summary`) must all reference immutable tags in
  their Helm values / Pulumi output.
- When updating a Deployment manually, always use
  `kubectl set image deploy/<name> <container>=<repo>:<sha>`, never
  `kubectl rollout restart`, because restart alone will not pull a new
  image if the tag identity hasn't changed.

## What tenant builds must do

Tenant-facing build pipelines already emit SHA-tagged images —
`loopzero-forge` computes the image reference as
`<registry>/<tenant>/<app>:<commit_sha>` in every `build-image-*` step
(see `infra/helm/argo-workflows/templates/` CWT definitions). This
policy makes the platform-side rule explicit and matches tenant build
behavior.

## Enforcement

- CI blocks new references to `:dev` / `:latest` / `:main` in Deployment
  manifests (Helm values and rendered YAML).
- Reviews check that any manual `kubectl set image` command uses a SHA
  tag — no exceptions.
- The `build-images.yml` workflow MUST set the Deployment image to a SHA
  tag, not `:dev`, when updating Pulumi state.

## Known exception: cluster bootstrap

During initial Pulumi bootstrap the first image tag does not yet exist
in the registry. The bootstrap workflow creates the repository with a
placeholder tag and then overwrites it with the first real build's SHA.
This is a single, documented, transient state — subsequent deploys use
SHA tags.

## References

- `infra/tests/load/CORRECTIONS.txt` § H
- `CLAUDE.md` "Common Mistakes" table (image cache on DOKS)
- `docs/roadmap/01-l1-defect-remediation/roadmap.md` Correction H
