# M15 Orphan Bash Sweep (2026-07-10)

**Purpose:** T1.2 — identify shipped `loopzero-ops seed <name>` Go subcommands whose bash counterparts still exist in `infra/scripts/local/` OR `infra/helmfile/scripts/` — those bash files are orphans (M15-W1+W2+W3 duplication debt) and Wave A candidates for immediate delete.

## Shipped Go seeds (25 subcommands)

Extracted from `cmd/loopzero-ops/internal/seed/*.go` (excluding `_test.go`, `seed.go` framework, `seed_registry.go`):

- admin_hydra_client
- admin_loopzero_dev
- argocd_gitea_creds
- argocd_token
- broker
- creds
- env (subcommand infra, not seed)
- gitea_token
- github_app_dev
- github_webhook_fixture
- hydra_urls
- kube (infra)
- loki_cross_cluster
- minio
- networking_cross_cluster_coredns
- patch_kratos
- portforward (infra)
- secrets_seed_dev
- stripe_api_keys
- stripe_webhook_endpoint
- stripe_webhook_secret
- tenant_fixture
- vault_cross_cluster_addr
- vault_role_id_sync

## Remaining seed-*.sh files in repo (5 files)

Command: `find infra -name "seed-*.sh"`
Output:
1. `infra/helmfile/scripts/seed-local-user.sh` (~187 LoC) — no shipped Go equivalent; REVIEW-NEEDED, likely KEEP or Wave B.
2. `infra/scripts/local/seed-chaos-mesh-canonical.sh` (~65 LoC) — no shipped Go equivalent; KEEP-OPERATOR per 2026-06-23 T1.1.
3. `infra/scripts/local/seed-demo-data.sh` — no shipped Go equivalent; KEEP-OPERATOR per 2026-05-29 T6.1.
4. `infra/scripts/local/seed-post-cleanroom-all.sh` (~120 LoC) — orchestrator that calls the shipped Go seeds; KEEP-OPERATOR (master orchestrator, entry point for post-Cleanroom chain).
5. `infra/scripts/openbao/seed-loopzero-secrets.sh` — no shipped Go equivalent visible; KEEP-OPERATOR (OpenBao secret provisioning, cluster-integration).

## Orphans identified

**Zero orphans found.** All 5 remaining seed-*.sh files serve a legitimate purpose:
- 4 have no shipped Go equivalent (require future migration in Wave C or later).
- 1 is the master orchestrator that CALLS the shipped Go seeds (not a duplicate).

## Consequence for Wave A candidate list

T2.1 remains the ONLY Wave A candidate: `infra/scripts/local/reset-for-e2e.sh` (sponsor 2026-06-08 mandate).

T2.2 (orphan delete lot) becomes a no-op for this sweep. If future sessions ship additional Go seeds (currently 25) with existing bash counterparts, T2.2 pattern is re-usable.

## Delta to plan expectations

Plan estimated ~5 Wave A files; empirical Wave A count = 1. This tightens Wave A execution to a single delete operation. Reserves Wave B budget for higher-impact migrations.

## Cross-references

- Plan: `knowledge-base/plans/operator-glue-go-migration-final-plan.md` v1.1
- T1.1 baseline: `knowledge-base/audits/M15-pre-migration-baseline-2026-07-10.md`
