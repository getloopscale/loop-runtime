---
plan_id: 2026-06-04-dev-public-docr-fix-forward
created_at: 2026-06-04
created_by: paulo
version: v1.0
status: PROPOSED
change_tier: T2  # infra (helmfile env), Helm values, Taskfile, K8s Secret provisioning
predecessors:
  - 2026-06-04-dev-public-domain-canonicalization-plan.md  # established dev-public env originally for Harbor local
related_memories:
  - feedback_dev_public_trio_100pct_mandate_2026_06_04
  - feedback_canonical_vs_codified_workaround
  - reference_canonical_100pct_requires_design_changes
  - reference_dev_public_canonicalization_2026_06_04
---

# Plan: Dev-Public DOCR Fix-Forward (2026-06-04)

## Goal

Run the canonical 4-phase trio (`/cleanroom + /dogfood --full + /chaos-test --full + /live-proof`) against env `dev-public` with **zero mock / zero bypass / zero workaround codified**, and achieve a factually verifiable 100% PASS state. Path P1 (DOCR REAL) chosen by sponsor on 2026-06-04: real image push to DigitalOcean Container Registry, real pull from 4 k3d clusters, manifests identical to production. Cost ceiling: $5/mo DOCR Basic plan.

## Context

### Why this plan exists

Session `e6517a8c` (worktree babbage) crashed 2026-06-04 16:40:09 UTC with SIGKILL exit 137 during `LOOPZERO_HELMFILE_ENV=dev-public task infra:install:mgmt` — host OOM (15GiB RAM, 148MiB free, swap=0). Sponsor reconnected ~17:25 with a new session and mandated fix-forward to 100% real, four explicit prohibitions: no mock, no bypass, no workaround codified.

Pre-crash audit of the surviving working tree (6 yaml diffs + 1 new script, 82 inserts total) found that the in-flight implementation was REGRESSING canonical defaults from `charts/loopzero-api/values.yaml` (which already declares `global.registry: registry.digitalocean.com/loopzero-system` + `pullPolicy: IfNotPresent`) into a `pullPolicy: Never + k3d image import` pattern — exactly the workaround signal listed in memory `feedback_canonical_vs_codified_workaround` § "Workarounds presentes".

### Why P1 over P2/P3

Sponsor explicitly chose P1 (DOCR REAL) on 2026-06-04 over the alternatives:

- **P2 (Harbor local, $0)**: design declared by `infra/helmfile/environments/dev-public.yaml` line 56 (`registries.docrPrefix: "harbor.loopzero.local/loopzero-platform/"`). Valid but exercises a different registry tech stack than prod.
- **P3 (k3d image import + DOCR-shaped tag, $0)**: matches anti-pattern from `feedback_canonical_vs_codified_workaround` — disqualified.
- **P1 (DOCR REAL, $5/mo)**: image transits the network from `docker buildx --push` to `kubectl image pull` exactly as in prod. Maximum prod parity at minimal cost.

## Out of scope

This plan does NOT modify tenant routing. Per `dev-public.yaml` env design, user tenants deployed via `loopzero deploy` continue to resolve at `*.loopzero.local` (internal Caddy/cluster). Only the platform plane (auth/dashboard/cloud surfaces) uses public `*-dev.loopzero.example`.

This plan does NOT introduce DO Managed PostgreSQL or DOKS — sponsor mandate forbids "provisioning K8s by cost". Local k3d + embedded PG (`inClusterPostgres: true`) remain.

This plan does NOT modify production helmfile env (`prod.yaml`).

## Architecture (post-plan steady-state)

See diagram in associated session transcript / runbook. Summary:

- 4 k3d clusters local (mgmt + build + workload + runtime-2) with `do-registry` imagePullSecret in loopzero-system, loopzero-build, loopzero-workload, loopzero-runtime-2 namespaces.
- DOCR holds 5 platform images (`loopzero-api`, `loopzero-{tenant,app,preview,platform}-operator`) tagged `:dev` + 5 worker images (clone-runner, packs, dockerfile-lint, syft-scanner, cosign-signer) already canonical.
- `task images:push:platform:dev-public` builds + pushes the 5 platform images.
- Caddy on host droplet + Cloudflare DNS serve `*-dev.loopzero.example` with Let's Encrypt TLS.

## Tasks

### Phase 0 — Pre-flight (READY before Phase 1)

**T0.1** — Tier 1 host resource liberation. **DONE 2026-06-04 17:30.** Stopped k3d-loopzero-s9 + k3d-loopzero-vault, drop_caches, +4GiB swap. RAM: 148MiB free → 1.3GiB free + 4GiB swap. Evidence: this session transcript.

**T0.2** — Sponsor decision P1 vs P2 vs P3. **DONE 2026-06-04.** Sponsor chose P1.

### Phase 1 — DOCR foundation

**T1.1** — `doctl registry login` + verify quota.
- DoD: `doctl registry get` returns the registry; `doctl registry repository list-tags loopzero-api --format Tag --no-header` returns at least empty (200, not 401/404).
- Evidence: command exit 0 + JSON output captured.

**T1.2** — Edit `infra/helmfile/environments/dev-public.yaml`:
- Change `registries.docrPrefix` from `"harbor.loopzero.local/loopzero-platform/"` to `"registry.digitalocean.com/loopzero-system/"`.
- Keep `harborPrefix` (Harbor remains as **secondary** option in `additionalPrefixes` if user wants to run Harbor locally in parallel; not required).
- DoD: helmfile template renders without error; rendered output for `loopzero-api` Deployment shows `image: registry.digitalocean.com/loopzero-system/loopzero-api:dev`.
- Evidence: `helmfile -e dev-public template` golden test.

**T1.3** — New task `images:push:platform:dev-public` in `taskfiles/infra.yml`:
- For each of 5 images (loopzero-api, loopzero-tenant-operator, loopzero-app-operator, loopzero-preview-operator, loopzero-platform-operator):
  - `docker buildx build --push --tag registry.digitalocean.com/loopzero-system/<image>:dev -f <Dockerfile> <ctx>`
- DoD: `task images:push:platform:dev-public` runs end-to-end on cold cache, produces 5 manifests in DOCR, total ≤8min.
- Evidence: `doctl registry repository list-tags <each>` shows `dev` tag with recent push timestamp.

**T1.4** — `do-registry` imagePullSecret provisioning across 4 k3d clusters:
- New helper or extension to `generate-secrets.sh dev-public mgmt|build|workload|runtime-2` that:
  - Calls `doctl registry kubernetes-manifest --namespace <ns>` for each (loopzero-system, loopzero-build, loopzero-workload, loopzero-runtime-2)
  - `kubectl --context k3d-loopzero-<cluster> apply -f -`
- Idempotent (re-run no-ops if Secret exists and not expired).
- DoD: All 4 contexts have `kubectl get secret do-registry -n <ns>` returning the docker-registry-typed Secret.
- Evidence: 4 `kubectl get secret do-registry -o yaml` outputs in log.

### Phase 2 — Helmfile values cleanup

**T2.1** — Rewrite `infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl`:
- Remove `image.pullPolicy: Never` (let chart default `IfNotPresent` apply; for fast iteration, override to `Always` only here).
- Remove `image.tag: dev` (chart default is already `dev`).
- Keep `service.type: ClusterIP` (Caddy proxies via host net; NodePort is unnecessary).
- Keep `env.JWKS_URL`, `env.JWT_ISSUER` overrides.
- Remove `env.REGISTRY_INSECURE: "true"` — incompatible with DOCR HTTPS.
- DoD: file ≤10 lines + comment; renders manifest with image `registry.digitalocean.com/loopzero-system/loopzero-api:dev`, pullPolicy `Always`, imagePullSecrets `[do-registry]`.

**T2.2** — Rewrite the 4 operator values yamls (`loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`):
- Remove `image.tag`, `pullPolicy: Never`, `global.imagePullSecrets: []` (chart defaults canonical).
- Set `global.imagePullSecrets: ["do-registry"]`.
- Override `image.pullPolicy: Always` (dev iteration).
- For `loopzero-tenant-operator`: keep `config.giteaUrl`, `config.argocdNamespace`, `config.ingressNamespace`, `kubeconfig.{build,runtime}.existingConfigMap`. Remove `config.baseDomain` (use env-injected via helmfile gotmpl), `config.insecureSkipTls` (chart default `false` correct for LE TLS via Caddy edge).
- DoD: each file ≤15 lines; rendered manifest shows DOCR image + `Always` pull + `do-registry` secret.

**T2.3** — Verify `infra/helmfile/runtime.helmfile.yaml.gotmpl` registries allowlist change is still correct after T1.2.
- The current diff adds `"k3d-loopzero-registry:5000/"` to `additionalPrefixes` for dev-public env. After T1.2, dev-public images come from DOCR (already in allowlist by default).
- DECISION: keep the k3d-loopzero-registry entry — it covers the case where BuildKit (tenant builds) pushes to k3d-registry for fast tenant iteration. It does NOT conflict with DOCR being the platform-plane registry.
- DoD: helmfile template renders both registries in the rendered tenant-controller policy.

### Phase 3 — Workaround removal

**T3.1** — Delete `infra/scripts/local/generate-dev-public-secrets.sh`. Its 3 secret operations migrate to canonical paths:
- `loopzero-service-auth` (random) → already handled by `generate-secrets.sh <env> mgmt` via `get_or_generate`.
- `loopzero-tenant-operator GITEA_TOKEN` → emitted by `generate-secrets.sh dev-public mgmt` as part of the existing `loopzero-platform-secrets` flow.
- `loopzero-minio-credentials AWS_SECRET_ACCESS_KEY` → already handled in build cluster path by canonical script.
- If any of the 3 is genuinely missing from `generate-secrets.sh dev-public mgmt|build`, ADD it there (do not recreate as a parallel script).
- DoD: `git status` shows the file deleted; `task setup` with `LOOPZERO_HELMFILE_ENV=dev-public` still produces all 3 secrets without invoking the deleted script.
- Allowlist consequence: `.claude/rules/shell-scripts-allowlist.md` entry for `generate-dev-public-secrets.sh` must also be removed.

### Phase 4 — Taskfile wiring

**T4.1** — Wire `task images:push:platform:dev-public` (T1.3) + `task do-registry:provision` (T1.4) into `task infra:install:mgmt` when `LOOPZERO_HELMFILE_ENV=dev-public`:
- Before `helmfile sync`, run images:push + do-registry:provision.
- Order: `doctl login → push images → provision pullSecret → generate-secrets → helmfile sync`.
- DoD: `LOOPZERO_HELMFILE_ENV=dev-public task setup` runs end-to-end fresh from `task clean:all` to `task doctor 38/0` in one shot.
- Evidence: LIVE timed run captured in `.claude/knowledge-base/audits/2026-06-04-dev-public-docr-cleanroom/`.

### Phase 5 — Trio canonical execution

**T5.1** — `/cleanroom` canonical contra dev-public.
- DoD: `task clean:all && LOOPZERO_HELMFILE_ENV=dev-public task setup && task doctor` returns GREEN (38/0 minimum, ideally 41/0 per memory `reference_full_green_validation_2026_05_25`).
- Evidence: captured kubectl outputs, task doctor JSON.

**T5.2** — `/dogfood --full` canonical.
- DoD: Ginkgo 132/132 PASS (zero functional FAIL; SKIP allowed only for documented prod-only scenarios).
- Pre-known gaps (per memory `reference_canonical_100pct_requires_design_changes`): JWT refresh-on-401, Tilt resource pinning, chaos scaling, loopzero-cloud Tiltfile. Sponsor pre-authorized fixing each.
- Evidence: Ginkgo JUnit XML + raw log.

**T5.3** — `/chaos-test --full` canonical.
- DoD: 10/10 chaos scenarios PASS with SLO held during chaos window.
- Evidence: chaos-mesh PodChaos manifests applied + recovery proof.

**T5.4** — `/live-proof` canonical.
- DoD: 9 assertions PASS (oidc/stripe/resend × source/k8s/LIVE).
- Evidence: live-proof JSON output.

### Phase 6 — Audit & memory consolidation

**T6.1** — Write evidence audit at `.claude/knowledge-base/audits/2026-06-04-dev-public-docr-trio-GREEN/` with `SUMMARY.md` + per-phase evidence files.

**T6.2** — Memory entries:
- Promote `feedback_dev_public_trio_100pct_mandate_2026_06_04` from "mandate active" to "mandate fulfilled" if trio PASS.
- New reference `reference_dev_public_docr_trio_GREEN_2026_06_04` if applicable.
- Update MEMORY.md index lines.

**T6.3** — CHANGELOG.md entry under `[Unreleased]` → Changed: env `dev-public` registry source migrated from Harbor local to DigitalOcean Container Registry (real push/pull, $5/mo cost).

## Definition of Done (plan-level)

Plan is COMPLETE only when ALL of:

1. Phase 1-4 tasks closed with 3-evidence-source per CLAUDE.md Rule 11 (commit+test+LIVE log).
2. Phase 5 trio executed against `dev-public` with verified 100% factual PASS (no SKIP masking failure; no mock; no bypass).
3. Phase 6 audit + memory + CHANGELOG written.
4. Git tree CLEAN after final commit (no `M ` or `??` lines remaining from this plan).
5. Sponsor explicit ratification ("trio GREEN aceito") OR honest negative report with specific gaps if any phase stuck.

## Edge cases (review pre-execution)

**EC-1 OOM repeats during helmfile sync** — Phase 0 T0.1 mitigates. If happens again, abort and report — do NOT keep retrying without RAM action.

**EC-2 DOCR token expires mid-session** — `doctl registry login` issues short-lived token. If push fails 401 after ≥30min, re-login. Build step must capture token age.

**EC-3 imagePullSecret stale across clusters** — `do-registry` Secret has expiry timestamp. If cluster takes >X hours to pull, may need re-provision. T1.4 idempotency catches this on next `task setup`.

**EC-4 Image tag `:dev` is mutable — race with concurrent operator** — for single-operator session this is OK. If multi-operator future, switch to commit SHA tags. Out of scope this plan.

**EC-5 DOCR quota exhausted (500MB free or 50GB Basic)** — 5 images × ~100MB ≈ 500MB so free tier IS NOT enough. Plan ASSUMES sponsor upgraded to Basic ($5/mo). T1.1 must verify quota first.

**EC-6 `do-registry` Secret missing pull permissions** — `doctl registry kubernetes-manifest` mints a pull-only token by default. If push from cluster (e.g., kaniko in build plane) is needed, separate push token required. Out of scope this plan; BuildKit uses host docker socket, not k8s.

**EC-7 Cleanroom destroys the do-registry Secret along with cluster** — `task clean:all` recreates k3d. Bootstrap MUST re-provision the Secret as part of mgmt setup BEFORE the helmfile sync (otherwise pods stuck `ImagePullBackOff`). T4.1 order enforces this.

**EC-8 Sponsor revokes DOCR mid-execution** — fallback to P2 (Harbor local) requires editing env file back. Document the revert path in T6.1.

**EC-9 chaos-mesh in workload cluster missing `do-registry`** — chaos manifests reference images from `chaos-mesh.org` registry, unaffected. But if any test inject pod with platform image, would fail. Verify.

## Estimated runtime

| Phase | Estimate |
|---|---|
| Phase 1 (DOCR foundation) | 30-45min (login + 5 image pushes cold) |
| Phase 2 (yaml cleanup) | 15-20min |
| Phase 3 (workaround removal) | 10min |
| Phase 4 (Taskfile wiring) | 30min (incl. testing) |
| Phase 5 (trio) | 2-3h (cleanroom ~30min + dogfood ~1h + chaos ~50min + live-proof ~5min) |
| Phase 6 (audit) | 20min |
| **Total** | **~4-5h** |

## Approval & sign-off

This plan is T2 per pre-release governance (changes helmfile env file + Helm chart values + Taskfile + K8s Secret provisioning). Per `.claude/rules/pre-release-governance.md`, T2 normally requires a meeting + ADR.

**Sponsor override 2026-06-04**: sponsor pre-authorized fix-forward execution via verbatim mandate "POde corrija todos os problemas e gaps voce dee parar e corrigir e continuar ate que todos esteja corrigirdos". This plan documents the design decision (P1 over P2/P3) and serves as the substituting decision record in lieu of a formal MEET-NNN.md. Plan version v1.0 frozen at creation; any amendment requires new version bump + MEET-ASYNC-AMENDMENT-NNN record.
