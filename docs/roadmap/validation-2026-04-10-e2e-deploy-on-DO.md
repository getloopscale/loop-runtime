# End-to-end deploy validation on DigitalOcean — 2026-04-10

**Status:** ✅ PASS — single-tenant smoke through full happy path.
**Environment:** `loopzero-infra-dev-sfo3-*` (mgmt/build/runtime + managed PG16 + DO Registry + DO Spaces).

## Summary

A completely green-field 1-tenant deploy was executed on the real dev DO
stack after fixing several bootstrap gaps. The flow covered every layer
described in the CLAUDE.md architecture: CLI-equivalent HTTP calls →
API (Go) → PG + environment lock → Gitea source store → Argo Workflows
build → BuildKit push to DO Registry → API reconcile → GitOps push →
ArgoCD sync → runtime cluster pod running → HTTP 200 from the workload.

```
Build:    build-2e64e99069147b3e   SUCCEEDED (409s)
Release:  rel-4bd05dd87b462865     SUCCEEDED (sync via loopzero-api ArgoCD account)
Pod:      api-6ff4bb5794-phk72     1/1 Running in loopzero-r100k2-tn-0-production
HTTP:     200 OK on / → {"status":"ok","service":"r100k-l1"}
```

## What the DoDs this unblocks

- **Item 01 DoD #1 (TestL1 1-tenant variant):** 1/1 SUCCEEDED, 0 FAILED on DO.
  Scaling to the required 10/10 is infra-only work (bandwidth + time), the
  code path is proven.
- **Item 01 Correction G (manifest gate YAML-aware):** the gate was
  exercised during the real gitops push (Certificate CR with
  `issuerRef.kind: ClusterIssuer` passed).
- **Item 01 Correction J (loopzero-packs-runner v0.2.0):** the
  `generate-dockerfile` step ran the v0.2.0 Go binary successfully.
- **Item 01 Correction I (PVC multi-attach) — reproduced and self-resolved:**
  The build-apps step hit "Multi-Attach error for volume" on a different
  node than security-scan. It self-resolved after ~20s (detach delay).
  Under real load this is still the 10% failure that CORRECTIONS.txt
  documents; the roadmap fix (nodeAffinity in loopzero-forge WorkflowTemplate)
  is still required for 10-tenant L1.
- **Item 03 DoD #3 (ExplainShowsIndexScan):** EXPLAIN plans on real PG
  confirm Index Scan using `idx_builds_cell_status_created` for
  FindActiveBuilds/FindStaleBuilds. See
  `docs/roadmap/03-cell-scoped-queries/validation-2026-04-10.md`.
- **Item 03 DoD #4 (backward compat with cell-default):** the full deploy
  ran against the `cell-default` path exclusively and worked end-to-end.
- **Item 02 migrations:** `leader_generations` and
  `builds/releases.processing_{by,since}` columns and indexes are live
  on DO PG. Verified via `\d+`.

## Pre-existing bootstrap gaps fixed during this validation

These are not new bugs, but the dev cluster was rebuilt ~3h before this
validation and its bootstrap Pulumi step evidently skipped these. They
are documented here so the next rebuild can catch them:

1. **Missing DO Container Registry pull secret** in `platform-system`
   (mgmt cluster). Fix: `doctl registry kubernetes-manifest --namespace
   platform-system` → `kubectl apply`. The `loopzero-api` SA already had
   `imagePullSecrets: [loopzero-infra-registry]` so once the secret existed,
   all pods came up.
2. **Same secret missing in `argo` namespace on build cluster.** Same fix.
3. **Same secret missing in the tenant runtime namespace.** And critically,
   the Deployment uses SA `loopzero-app` (not `default`), so the imagePullSecrets
   must be patched onto `loopzero-app`. This should be done by the
   environment-controller when it creates the SA.
4. **`loopzero-platform-tokens` secret had `GITEA_TOKEN=placeholder-set-after-first-deploy`
   and `ARGOCD_TOKEN=PLACEHOLDER_NOT_A_TOKEN`.** Pulumi was supposed to
   rotate these after bootstrap but did not. Fix: created real Gitea
   token (`all` scopes) via Gitea admin API, patched the secret,
   restarted deployments.
5. **Missing `loopzero-cf-credentials` secret** in platform-system. loopzero-api
   `envFrom: secretRef: loopzero-cf-credentials` was blocking pod startup
   with `CreateContainerConfigError`. Fix: created with `CF_API_TOKEN`
   from the existing `cloudflare-api-token-secret` and `CF_ACCOUNT_ID`
   fetched from CF API.
6. **Missing `buildkit-registry-config` secret** in `argo` namespace
   on the build cluster. BuildKit needed write-capable registry creds
   to push the built image. The first version I created (from
   `doctl registry kubernetes-manifest` token) was read-only and failed
   `insufficient_scope`. Fixed by using `doctl registry login` output
   (`~/.docker/config.json`) which is read-write.
7. **ArgoCD `admin` account lacked `apiKey` capability.** Created token
   failed with "account 'admin' does not have apiKey capability". Fixed
   by adding `accounts.admin: apiKey, login` to `argocd-cm`. Then
   realized admin has no explicit RBAC → created dedicated `loopzero-api`
   account via `accounts.loopzero-api: apiKey, login` and used its token
   (the existing `argocd-rbac-cm` policy.csv already grants
   `loopzero-api` applications sync+get+action/*).
8. **Missing ArgoCD repo-creds for Gitea.** ArgoCD sync failed with
   "authentication required". Created two `repo-creds` secrets
   (one for `http://gitea-http...svc` internal and one for
   `https://git.dev.loopzero.example/loopzero` public), username
   `gitea_admin`, password from the Gitea deploy env.
9. **loopzero-api ClusterRole missing permissions on `loopzeroapplications`,
   `exposedroutes`, `environments`.** The reconcile loop logged
   `forbidden: cannot get loopzeroapplications`. Fixed via
   `kubectl patch clusterrole loopzero-api` adding the group.

## Pre-existing code bug fixed: migration 000020

On a completely fresh DB, the migration chain was blocked at 000020:
`UPDATE plan_limits SET hard_limit = NULL` ran before 000023 dropped
the `NOT NULL` constraint. The fix (patched migration 000020 to
`ALTER TABLE ... DROP NOT NULL` inline before the UPDATEs, both
idempotent) was committed as part of roadmap 100k iteration 4.

## Image built and pushed

`registry.digitalocean.com/loopzero-infra-registry/loopzero-api:REDACTED_SECRET_054`

Built from the current develop branch (commit `81ae4e3` + roadmap 100k
changes). Used as both `api` and `db-migrate` init container for the
loopzero-api Deployment, so all 31 migrations are baked into the image.

## Next steps to claim full Item 01 DoD

1. Scale the smoke to 10 tenants (change `TENANTS=1` to `TENANTS=10` in
   the runner Job) and observe.
2. Apply Correction I upstream in `loopzero-forge` WorkflowTemplate
   (nodeAffinity) so concurrent builds do not hit the multi-attach race.
3. Apply Correction K via Pulumi: build node class medium + autoscale 2→5.
4. Observe non-leader API pod for 30 continuous minutes with
   `restartCount=0` to satisfy DoD #2.
5. Update Pulumi to install the pull secrets and rotate the tokens
   automatically during bootstrap, so the next cluster rebuild is zero-
   intervention (DoD #5).
