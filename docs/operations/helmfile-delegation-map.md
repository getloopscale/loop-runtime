# Helmfile Delegation Map (Phase 2 T2.1)

> Autonomous prep audit for `docs/plans/2026-05-22-script-cleanup-plan.md` Phase 2.
> Maps each `DELEGATE-HELMFILE`-classified script to the helmfile release that
> should own it, and **flags the ones that have no clean helmfile equivalent
> (→ KEEP)**. Read-only — no infra change. The LIVE implementation (T2.2) is an
> OPERATOR HARD STOP because it rewrites the cleanroom bring-up path.

**Generated:** 2026-05-22 · **Source:** `docs/scripts-audit/2026-05-22.csv` (disposition=DELEGATE-HELMFILE)

## Scope

After the T0.1 detector hardening (line-start helm detection), the
`DELEGATE-HELMFILE` set is **5 scripts** (was 12 before false-positive removal).
Helmfile environments: `mgmt`, `build`, `runtime` (`infra/helmfile/*.helmfile.yaml.gotmpl`).

## The hard truth about secret-generator delegation

Three of the five are **credential generators + injectors**, not bare-helm
wrappers. They (a) GENERATE a random/deterministic secret value, then (b) `kubectl
apply` it into a namespace BEFORE the consuming chart syncs. Helmfile has **no
native secret-generator** — it can template `set:`/`secrets:` from existing
values, but it cannot mint a random password. So "delegate to helmfile" for these
is NOT a simple `set:` block. The honest options are:

- **Option A — thin pre-sync hook:** keep a tiny generator and call it from
  helmfile `hooks: [{events: [presync], command: ...}]`. This RELOCATES bash, it
  does not eliminate it — net script count unchanged. Rejected for the ≤30 goal.
- **Option B — ESO generators:** adopt External Secrets Operator `Password` /
  `ACRAccessToken` generator CRDs so the secret is minted declaratively by ESO,
  then PushSecret'd to the consuming namespaces. This DOES eliminate the bash but
  is a **T2/T3 architecture decision** (new CRDs, determinism contract change,
  cleanroom re-validation) — out of scope for an autonomous task; requires a MEET.

**Determinism constraint (load-bearing):** per the 2026-05-22 cleanroom session,
credential determinism is the dominant cleanroom-regression theme (PG password,
MinIO root, gitea admin must be deterministic across rebuilds or charts that
hardcode them break — SOURCE_UPLOAD_FAILED, clone signature mismatch). Any ESO
generator migration MUST preserve deterministic seeds, not random-per-sync.

## Mapping table

| Script | LOC | Secrets it owns | Target release(s) | Delegatable? | Disposition |
|---|---:|---|---|---|---|
| `infra/helmfile/scripts/generate-secrets.sh` | 391 | `loopzero-db-credentials`, `gitea-admin`, `loopzero-api-tokens`, `argocd-refresh-credentials`, `minio-credentials`, `velero-s3-credentials`, `harbor-admin`, `harbor-redis-credentials`, `loopzero-cluster-kubeconfigs` (ns: loopzero-system, argocd, velero-system) | `postgresql`, `gitea`, `loopzero-api`, `argocd`, `minio`, `velero`, `harbor` (mgmt) | **Partial** — needs Option B (ESO generators) to eliminate; Option A only relocates | KEEP until ESO-generator MEET (T2/T3) |
| `infra/helmfile/scripts/generate-build-secrets.sh` | 120 | `cosign-signing-key`, `buildkit-registry-config`, `minio-credentials` (build cluster) | `harbor`/buildkit on build env | **Partial** — same generator gap | KEEP until ESO-generator MEET |
| `infra/helmfile/scripts/generate-runtime-secrets.sh` | 140 | `cloudflared-tunnel-token`, `loopzero-eso-source`, `minio-credentials`, `static-assets-s3` (runtime cluster) | `cloudflared`, `external-secrets`, `static-assets` (runtime) | **Partial** — same generator gap | KEEP until ESO-generator MEET |
| `infra/helmfile/scripts/bootstrap-patch-admission-policies.sh` | 29 | (no secret) patches the warn-mode ClusterImagePolicy allow-list to add the `k3d-loopzero-registry` prefix (B13) | `admission-policies` (mgmt) | **YES** — fold the registry prefix into the chart's `.Values.registries.*` per-environment (k3d adds the local registry) | DELEGATE → values, then delete |
| `infra/scripts/local/k3d-cross-cluster-heal.sh` | 1005 | (no secret) runs `helm upgrade loopzero-build-cwts` at L392 as ONE of ~18 heal steps (H1-H18) recovering LIVE drift | n/a — heal recovers drift declaratively-unexpressible | **NO** — heal is operator recovery tooling, not a declarative install (parent D5). Heuristic false-positive (one real helm line among LIVE recovery logic) | KEEP-OPERATOR (reclassify in allowlist) |

## Conclusions

1. **Only 1 of 5 is a clean autonomous-after-LIVE delegation:**
   `bootstrap-patch-admission-policies.sh` → move the registry prefix into the
   `admission-policies` chart values per environment. This is the one Phase 2
   item that does NOT need a MEET. Still LIVE-gated (cleanroom must re-converge),
   so it remains an OPERATOR HARD STOP for the delete step.
2. **3 secret-generators are blocked on a design decision** (Option B / ESO
   generators) — they are NOT a simple `set:` and CANNOT be eliminated without a
   T2/T3 MEET that also preserves the determinism contract. They stay bash for now.
3. **1 (`k3d-cross-cluster-heal.sh`) is a heuristic false-positive** → KEEP-OPERATOR.
   Recommend the allowlist row + the CSV disposition both record this.
4. **Revised Phase 2 reduction estimate: 1 script** (not the plan's optimistic
   ~25). The big bootstrap reduction the plan anticipated does not exist once the
   secret-generation/determinism reality is accounted for. This is the honest
   number; the plan's ~25 conflated string-mention false-positives + generators
   that can't be declaratively eliminated.

## Operator next steps (LIVE, T2.2)

1. Fold the k3d registry prefix into `infra/helm/admission-policies` values
   (per-environment), delete `bootstrap-patch-admission-policies.sh`, run
   `task clean:all && task setup && task doctor` to confirm no admission regression.
2. Re-validate `/dogfood full` + `/chaos-test --full` (no regression).
3. For the 3 secret-generators: open a T2/T3 MEET on ESO generator adoption +
   determinism contract before any deletion. Do NOT relocate to a bash hook
   (Option A) — that does not advance the ≤30 goal.
4. Reclassify `k3d-cross-cluster-heal.sh` as KEEP-OPERATOR in the allowlist.
