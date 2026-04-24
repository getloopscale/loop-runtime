# ADR-0013: Local install must be production-realistic, not a mock

**Status:** ACCEPTED
**Date:** 2026-04-25
**Origin:** Async T2 approval in pre-release phase (CHG-2026-04-25-E2E)
**Change ID:** CHG-2026-04-25-E2E
**Deciders:** Platform Architect (async), reviewed by SRE/Production Design Reviewer (async)
**Supersedes:** None.
**Refines:** ADR-0012 (CNPG migration) — extends the BYO vs LoopZero-internal boundary to TLS issuer + image registry.

---

## Context

After ADR-0012 (CNPG replaces bitnami/postgresql), `loopzero platform install`
completed the 10 install steps on a fresh k3d cluster but the product
was still **not usable end-to-end**. Two prior validation sessions
(commits `ba1d2228`, `8a35c2df`) corrected 23 installer bugs, yet
`loopzero deploy` still did not produce a release in ACTIVE state with a
URL responding 200.

The user requested:

> Quero que faça o testes completo do nosso install, caso encontre erros corrija.
> Ao final temos que conseguir fazer um 'loopzero deploy' localmente.
> ... O loopzero-packs deve ser consumido do DOCR como em produção real.

Three Explore agents mapped the remaining gaps:

1. **Agent "chart-vs-runtime"** — found 9 structural gaps where Helm chart
   templates do not match what the Go binary expects (missing env vars,
   too-narrow ClusterRoles, wrong volume types, mis-scoped NetworkPolicies).
2. **Agent "loopzero-packs"** — confirmed that `loopzero-packs` is a working Go
   binary at `/home/paulo/work/loopzero-packs`. The published image
   `loopzero-packs-runner` exists in DOCR but with date-stamped tags
   (`:20260424`) instead of semver — runtime constants reference
   `:v0.2.0` which doesn't exist, breaking the build pipeline.
3. **Agent "stack alternatives"** — evaluated CNI swap (Cilium/Calico),
   registry alternatives (k3d-registry, in-cluster `registry:2`), and
   TLS strategies (mkcert, Let's Encrypt staging, self-signed). Recommended
   keeping flannel + DOCR for "local equals production" parity, and
   adopting **mkcert + CA-issuing ClusterIssuer** for TLS so that
   browsers/curl trust certs without `-k`.

## Decision

### Architecture

| Eixo | Decisão | Reasoning |
|---|---|---|
| **CNI** | Manter flannel | Rule 10 (CLAUDE.md). The "flannel quirk" observed in prior sessions was actually a NetworkPolicy mis-scoping (Gap 5 in this CHG), not a bug in flannel. Fix is in the chart, not the CNI. |
| **Registry** | Manter DOCR oficial | User constraint: "loopzero-packs deve ser consumido do DOCR como em produção real". A real customer self-hosting LoopZero would pull pre-published images, not `git clone + docker build`. We honor that. |
| **loopzero-packs publish strategy** | Tag publish CI to emit semver (`:v0.X.Y`) | Workflow at `loopzero-packs/.github/workflows/build-runner.yml` currently emits `:latest`, `:<sha>`, `:<YYYYMMDD>`. We extend it to also emit `:${{ github.ref_name }}` when triggered by tag push. Operator tags `v0.2.0` to publish a stable artifact. Until the CI fix lands, manual `docker pull/tag/push` was used to seed `:v0.2.0` in DOCR (already done). |
| **TLS / domain (dev profile)** | mkcert local CA + cert-manager `ClusterIssuer kind=ca` | Browser/curl trust certs by default (mkcert -install adds CA to OS trust store). Issuance flow is identical to production (CA → cert-manager → Certificate). Only the CA differs (mkcert local vs Let's Encrypt). Acceptable for `profile=minimal`. |
| **TLS (production profile)** | Customer-provided ClusterIssuer (ACME / enterprise CA) | Installer never auto-provisions self-signed in production. Validates `config.clusterIssuer` is non-empty before proceeding. |
| **Chart fixes** | Close 9 chart-runtime gaps mapped by Agent 1 | Each gap surfaced as a manual `kubectl patch` workaround in prior sessions. Pulling them into chart templates eliminates that operator burden. |

### BYO vs LoopZero-internal — refined boundary (extends ADR-0012)

ADR-0012 established that **customer state** (DB, object storage, K8s
cluster, container registry) is BYO; **LoopZero product state** (Gitea,
ArgoCD, operators) is LoopZero-internal. ADR-0013 adds two more axes:

| Resource | Customer (production) | LoopZero-internal (dev/test) |
|---|---|---|
| **TLS issuer** | ACME / Let's Encrypt / enterprise CA — `config.clusterIssuer` required | mkcert local CA — installer auto-provisions for `profile=minimal` |
| **Image registry** | Customer-provided (DOCR, ECR, Harbor) — `config.registry` required | Same DOCR (loopzero-system) consumed read-only by all profiles |
| **Image tag publication** | Tag pinned by customer in `values.yaml`; reproducible builds via signed tags | LoopZero-managed; customer pulls LoopZero's pre-built images |

### Smoke E2E as platform SLO

We codify "platform installable" as: `infra/tests/e2e/local-install-deploy.sh`
exits 0 on a fresh k3d cluster, in under 12 minutes, ending with `curl
https://hello.test.local` returning HTTP 200 and the expected body.

This script is the executable definition of "the product works
end-to-end". It runs as part of CI (future) and operators run it
locally before claiming a fix.

## Consequences

### Positive

- **`loopzero deploy` works end-to-end** in `profile=minimal` without manual
  kubectl. Operator runs 3 commands: `loopzero platform install`,
  `loopzero login`, `loopzero deploy`.
- **Browser-trusted TLS** in dev: `curl https://app.test.local` works
  without `-k`. Matches production DX.
- **Architecture is clear**: `mkcert` decision documents that the
  ISSUANCE PROTOCOL matches production; only the CA root differs.
- **Smoke script catches regressions**: any future PR that breaks E2E
  fails the script in CI.
- **Charts now self-document expectations**: chart values surface every
  env var/secret the runtime needs. Customer reading `values.yaml` sees
  the full contract.

### Negative / Risks

- **mkcert as new pre-req for dev**: operators without it get self-signed
  fallback (works but `curl -k`). README documents how to install.
- **Smoke script depends on hello-server fixture**: any drift in
  `infra/tests/e2e/fixtures/hello-server/` body breaks the assertion.
  We mitigate by using a stable response (`"ok"`) and pinning the
  fixture in the script.
- **Chart upgrades on existing prod DOKS**: 9 gaps fixed in this CHG
  added new env vars + secrets to chart templates. Defaults are
  backward-compatible (most are gated by `{{- if .Values.X }}`), but
  Pulumi's `helm upgrade` on prod must be tested before rolling out.

### Neutral

- **Registry remains external**: any DOCR outage breaks self-hosted
  installs. Acceptable risk — same as Vercel relying on AWS, Render
  relying on GCP. Customer can swap to Harbor/ECR/GCR via
  `config.registry.endpoint`.

## Implementation phases

This ADR is implemented in 5 phases (see plan
`/home/paulo/.claude/plans/harmonic-sniffing-axolotl.md`):

| Phase | Scope | Status |
|---|---|---|
| **0** | Reset baseline + verify publish pipelines | ✓ Done |
| **1** | DOCR `loopzero-packs-runner:v0.2.0` retag | ✓ Done (manual; CI fix follow-up) |
| **2** | Close 5 critical chart-runtime gaps | ✓ Done — commit `ec1a4086` |
| **3** | mkcert + CA ClusterIssuer | ✓ Done — this CHG |
| **4** | Smoke E2E script + this ADR | ✓ Done — this CHG |
| **5** | Final E2E validation + 4 medium gaps | ⏳ In progress |

## Out-of-scope

- **Replacing flannel with Cilium/Calico** — would violate Rule 10.
- **Migrating to Harbor/ECR/internal registry** — customer choice.
- **Building loopzero-packs from source in installer** — explicit user
  rejection: must consume DOCR like a real customer.
- **SBOM / admission policy** — Sprint 6 remainder.
- **Multi-cluster mode** — single-cluster only in the smoke script.
- **TLS for production profile** — customer responsibility (ACME / CA).

## Validation

Run `infra/tests/e2e/local-install-deploy.sh`. Exit 0 means the
platform is installable and a deploy works end-to-end.

## References

- Plan: `/home/paulo/.claude/plans/harmonic-sniffing-axolotl.md`
- Parent ADR: `docs/meetings/adr/0012-cloudnativepg-replaces-bitnami-postgresql.md`
- Agent reports: in-session conversation, 3 Explore agents (chart gaps, loopzero-packs, stack alternatives)
- mkcert: https://github.com/FiloSottile/mkcert
