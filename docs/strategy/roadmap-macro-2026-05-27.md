---
name: roadmap-macro-2026-05-27
description: Roadmap macro do LoopZero — 28 itens enumerados (R1-R28) com decomposição engineering vs operator/sponsor residuals, priorizados P0/P1/P2/P3, refletindo estado pós-execução do plano 2026-05-26 maturity-roadmap.
type: strategy
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: post-T2.7/T2.3/T2.2/T0.2/T5.2 sponsor-direct synthesis
---

# Roadmap Macro — LoopZero (2026-05-27)

> **Snapshot pós-execução autônoma do plano 2026-05-26 maturity-roadmap.**
>
> Maturity atual: **3.62/4.0** (audit 2026-05-26). Teto alcançável pós-plano: **3.95-3.983** (ADR D3 cap — M11 #25 intractable per DB CHECK constraints).
>
> 5 commits shipped 2026-05-27: T2.7 Stripe Portal SDK (`8d21c738`), T2.3 image-digest warn-mode (`af0fb32c`), T2.2 Sigstore Pulumi component (`321e6471`), T0.2 CI platform ADR + POC (`728b1865`), T5.2 DRILL_KUBECONFIG_DIR fallback removal (`b4ccca00`).

## Executive Summary

| Nível | Status | Bloqueios principais |
|-------|--------|---------------------|
| Engineering-feature-complete | ~98% | T0.1 architecture-docs Mode 2 (operator skill invocation, ~30min) |
| Operator-ready | SIM, com caveats | 5 HARD STOPs do plano 2026-05-26 requerem sponsor flip |
| Prod canary-ready | **NÃO** | 9 itens P0 (4 sponsor meetings + 4 LIVE gates + 1 deadline) |
| Public GA-ready | **NÃO** | P0 acima + customer trial (R10) + maturity 3.95 push (R11) |

**Caminho crítico mínimo para canary:** ~2 semanas calendário, dominado por **sponsor agendamento**, não engineering. Engineering residual = ~2h (R3.g buildkit.go digest emit).

## Convenções

- **Prioridade:** P0 (canary blocker) · P1 (GA blocker) · P2 (v1.x compromisso defer) · P3 (post-GA/v2.x backlog)
- **Complexidade:** S (<1d) · M (<1wk) · L (<1mo) · XL (>1mo)
- **Bloqueio:** identifica gate real (Sponsor / Operator / Time / Budget / External / Engineering)
- **Sub-items (R*.a-z):** decomposição engineering JÁ DONE vs activation residuals

---

## Tabela Macro — Visão Consolidada

| # | Nome | Bloqueio | Complex. | Prioridade |
|---|------|----------|----------|-----------|
| **R1** | Cosign keyless **activation** (engineering done; R1.a-c shipped 2026-05-27) | Sponsor T2.2 + budget + DNS + operator orchestration | M | **P0** |
| **R2** | Drill prod-identical GREEN gate (a) | Operator + drill credentials | S | **P0** |
| **R3** | T2.3 image-digest **Deny-flip** (engineering done; R3.a-d shipped 2026-05-27) | 7-day soak + Sponsor T2 + buildkit.go change | S+M | **P0** |
| **R4** | T2.7 Stripe Portal **prod key flip** (engineering done; R4.a shipped 2026-05-27) | Sponsor sign-off em prod Stripe account | S | **P0** |
| **R5** | T0.2 CI platform decision + implementation (ADR + POC shipped 2026-05-27) | Sponsor T3 meeting + 2026-05-23 rescope | M | **P0** |
| **R6** | T0.1 architecture-docs Mode 2 (operator skill) | Operator skill invocation | S | **P0** |
| **R7** | `/dogfood full` cleanroom re-run | Operator + 60min + clean k3d | S | **P0** |
| **R8** | `/chaos-test --full` re-run | Operator + 60min | S | **P0** |
| **R9** | `/cross-validation` plano 2026-05-26 | Operator + ~30min | S | **P0** |
| **R10** | Customer trial 1-day gate (b) | 1 customer disposto + safety-net + sponsor | M | **P1** |
| **R11** | Phase 5 re-audit (maturity 3.62→3.95) | Operator + `/audit-modular` | S | **P1** |
| **R12** | Pro tier go-live (público) | R4+R5+R10 done | S | **P1** |
| **R13** | Architecture docs Mode 4 promotion (6 diff/→AS-IS) | Operator + R7 PASS | S | **P1** |
| **R14** | Dashboard FAANG tier 3 implementação | Engineering ~80h sequencial | XL | **P1** |
| **R15** | Real CI platform implementation (post-R5) | R5 done | M | **P1** |
| **R16** | Cron migration (drill watchdog + soaks + coverage) | R15 done | S | **P1** |
| **R17** | T5.3 Taskfile `--env <name>` extension (5 envs) | Operator priority post-canary | S | **P2** |
| **R18** | SSO/SAML Enterprise tier | Engineering ~3 weeks (Ory Hydra OIDC + SAML IDP) | L | **P2** |
| **R19** | Loki cross-cluster streaming | Operator architecture decision + ~6h eng | M | **P2** |
| **R20** | Pluto deprecated-API gate re-home | Engineering ~3h (mirror prodimagedigests pattern) | S | **P2** |
| **R21** | Slack/Botkube alerting destination | Sponsor channel + ~2h eng | S | **P2** |
| **R22** | M11 #25 string-aliases typed-enum migration | DB migration + data migration window | L | **P3** |
| **R23** | Falco / Tetragon runtime security | Engineering + eBPF cluster support | L | **P3** |
| **R24** | K8sGPT / HolmesGPT operator-assist | Engineering eval | M | **P3** |
| **R25** | Kubescape vs kube-bench eval | Operator security review | S | **P3** |
| **R26** | eBPF observability (Karpor/Pixie/DeepFlow/Alaz) | v2.x cells > 10 trigger | XL | **P3** |
| **R27** | Kueue para batch workloads | Product decision | M | **P3** |
| **R28** | Multi-region (regional cells além de SFO3) | Engineering + product/cost decision | XL | **P3** |

**Distribuição:** 9 P0 + 7 P1 + 5 P2 + 7 P3 = **28 itens**.

---

## Decomposição: itens com "engineering done, activation pending"

Os itens abaixo NÃO são novos trabalhos eng. São pipelines onde a primeira etapa (código) foi committed hoje, mas a activation real exige sponsor + operator + tempo.

### R1 — Cosign keyless cutover

| Sub | Descrição | Status | Quem |
|-----|-----------|--------|------|
| R1.a | Pulumi `Sigstore` ComponentResource (Fulcio+Rekor+CTLog+TUF) + 9/9 vitest tests | ✅ DONE `321e6471` | Eng autonomous |
| R1.b | Helmfile stanza commented-out (alternative path) | ✅ DONE `321e6471` | Eng autonomous |
| R1.c | Deadline warning script T-30d/T-15d/T-7d | ✅ DONE `321e6471` | Eng autonomous |
| R1.d | Sponsor T2.2 meeting + $30-50/mo budget approval | ⏳ PENDENTE | Sponsor |
| R1.e | DNS records `fulcio.<domain>` + `rekor.<domain>` | ⏳ PENDENTE | Operator |
| R1.f | `pulumi up` LIVE — wait ~20min TUF bootstrap + ACME issuance | ⏳ PENDENTE | Operator |
| R1.g | Charts flip `clusterImagePolicyKeyless.enabled=true` | ⏳ PENDENTE | Operator |
| R1.h | ConfigMap `loopzero-cosign-config` mode=keyless (clears deadline gate) | ⏳ PENDENTE | Operator |

**Hard deadline externo:** 2026-08-10 (T0+90d per ADR-2026-05-12). Script `check-cosign-cutover-deadline.sh` block-on-deadline armado. **74 dias restantes.**

### R3 — T2.3 image-digest enforcement

| Sub | Descrição | Status | Quem |
|-----|-----------|--------|------|
| R3.a | VAP `loopzero-require-image-digest` warn-mode (2 chart systems) | ✅ DONE `af0fb32c` | Eng autonomous |
| R3.b | `loopzero-common.image` helper digest preference | ✅ DONE `af0fb32c` | Eng autonomous |
| R3.c | `cmd/loopzero-ops/internal/quality/prodimagedigests` gate (warn-mode default, `--enforce` flag) wired em `task quality:gates` | ✅ DONE `af0fb32c` | Eng autonomous |
| R3.d | Prometheus alert `LoopZeroImageDigestWarnRatePresent` (info severity) | ✅ DONE `af0fb32c` | Eng autonomous |
| R3.e | Deploy updated admission-policies + loopzero-common via ArgoCD | ⏳ PENDENTE | Operator |
| R3.f | 7-day Phase 1 warn-mode soak (audit Prometheus + Grafana policy-controller dashboard) | ⏳ PENDENTE | Time |
| R3.g.1 | **Tenant apps** deploy emite digest via build_reconcile pipeline (`build_reconcile_complete.go:65-77` → `manifest_builder.go:70-77` → `naming.ImageDigestRef`) | ✅ **DONE 2026-04-29** (CHG-2026-04-29-DOGFOOD iter 8 — descoberto em recon 2026-05-27) | Eng pré-existente |
| R3.g.2 | Helm helper `loopzero-common.image` prefere `image.digest` quando set (charts) | ✅ DONE hoje `af0fb32c` | Eng autonomous |
| R3.g.3 | **Platform charts publishing pipeline** emite `image.digest` em `charts/loopzero-*/values.yaml` (DOCR push + digest extract + values rewrite) | ⏳ PENDENTE — depende de R5 (CI platform decidida) | Operator post-R5 |
| R3.h | Sponsor T2.3 meeting (Change Governor) | ⏳ PENDENTE | Sponsor |
| R3.i | Flip `requireImageDigestEnforce=true` em prod overrides | ⏳ PENDENTE | Operator |
| R3.j | `prod-image-digests` gate `--enforce` flag em CI (post-R15) | ⏳ PENDENTE | Operator |

**Live scan inicial:** 3 warn-mode violations detectadas (helm test pods `busybox:1.36`, `curlimages/curl:8.7.1`). Baixo impacto, ephemeral. Sponsor decide se warrants fix antes do Deny-flip.

**Discovery 2026-05-27 (recon pós-R3.a-d):** o tenant-side digest pipeline (R3.g.1) **já funciona desde 2026-04-29** (CHG-2026-04-29-DOGFOOD iter 8). `manifest_builder.go` fails-loud se digest ausente, e `build_reconcile_complete.go` propaga o digest do BuildKit para `naming.ImageDigestRef()`. O único gap real é R3.g.3 (platform charts publishing), que é gated por R5 — esses charts hoje têm `image.tag: dev` hardcoded em `charts/loopzero-*/values.yaml` porque nunca passam pelo build_reconcile pipeline; sobem via ArgoCD a partir do OCI registry de charts publicados.

### R4 — T2.7 Stripe Portal SDK

| Sub | Descrição | Status | Quem |
|-----|-----------|--------|------|
| R4.a | `PortalSessionCreator` DIP interface + `StripePortalSessionCreator` SDK wrapper + 5/5 tests + setup_server env wiring | ✅ DONE `8d21c738` | Eng autonomous |
| R4.b | Sponsor sign-off + Stripe prod account ready | ⏳ PENDENTE | Sponsor |
| R4.c | ESO Secret rotate `STRIPE_SECRET_KEY`: `sk_test_*` → `sk_live_*` + `BILLING_RETURN_URL` set | ⏳ PENDENTE | Operator |

**Status:** zero código adicional necessário. Sponsor flip do Secret = comportamento prod imediato.

### R5 — T0.2 CI platform decision

| Sub | Descrição | Status | Quem |
|-----|-----------|--------|------|
| R5.a | ADR-2026-05-27-T0.2 (3 cenários A/B/C com adoption + cost analysis) | ✅ DONE `728b1865` | Eng autonomous |
| R5.b | Gitea Actions POC: Helm overlay `.disabled` + workflow `.example` (Cenário A) | ✅ DONE `728b1865` | Eng autonomous |
| R5.c | Architecture diff `real-ci-active.md` atualizado | ✅ DONE `728b1865` | Eng autonomous |
| R5.d | Sponsor T3 meeting (Production Design + Security + SRE) escolhe cenário | ⏳ PENDENTE | Sponsor |
| R5.e | Operator activates POC: rename `.disabled→.yaml` + `.example→.yml` + helmfile add | ⏳ PENDENTE | Operator ~1h |
| R5.f | Branch protection update required check `quality` | ⏳ PENDENTE | Operator |
| R5.g | 30-day trial gate — if Cenário A ecosystem gap, swap to Cenário C (zero eng cost) | ⏳ PENDENTE | Operator + 30d soak |

### R6 — T0.1 architecture-docs Mode 2

| Sub | Descrição | Status | Quem |
|-----|-----------|--------|------|
| R6.a | 6 domínios (api, auth, build-pipeline, ci-cd, infra, observability) já têm `diff/` PROPOSED docs | ✅ DONE iter 1-4 | Eng autonomous |
| R6.b | Operator invoca `/architecture-docs Mode 2` para gerar AS-IS snapshot | ⏳ PENDENTE | Operator ~30min |
| R6.c | Operator invoca `/architecture-docs Mode 4` (promote diff/→AS-IS) pós-canary | ⏳ PENDENTE (= R13) | Operator |

---

## Caminho Crítico — Canary Launch

```
DIA 0 (hoje):
  ├─ R6 (operator: /architecture-docs Mode 2)     ─ 30min
  ├─ R7 (operator: /dogfood full)                 ─ 60min (paralelo)
  ├─ R8 (operator: /chaos-test --full)            ─ 60min (paralelo)
  └─ R9 (operator: /cross-validation)             ─ 30min (paralelo)

DIA 1:
  └─ R2 (operator: task drill:l2-scale)           ─ 60min ($0.32/hr)

DIA 2-7:
  ├─ R5.d sponsor T3 CI platform meeting           ─ agendamento
  ├─ R1.d sponsor T2.2 Cosign keyless meeting     ─ agendamento (paralelo)
  ├─ R3.h sponsor T2.3 Change Governor meeting    ─ agendamento (paralelo)
  ├─ R4.b sponsor T2.7 Stripe sign-off             ─ async approval (paralelo)
  └─ R3.f start 7-day warn-mode soak               ─ Day 1 of 7

DIA 8-14:
  ├─ R1.e DNS + R1.f pulumi up Sigstore           ─ ~20min após R1.d approval
  ├─ R3.g buildkit.go digest emit                  ─ 2h eng
  ├─ R5.e CI platform POC activate                 ─ 1h operator
  ├─ R4.c Stripe ESO key rotate                    ─ 5min operator
  └─ R3.f soak completes Day 7                     ─ T-0 soak

DIA 15:
  ├─ R1.g+R1.h Cosign mode=keyless                 ─ 30min operator
  ├─ R3.i requireImageDigestEnforce=true flip     ─ 5min operator
  └─ canary READY (depende customer trial R10)

DIA 16+:
  └─ R10 customer trial 1-day                       ─ External
```

**Total mínimo:** ~16 dias calendário (2 semanas + 1 dia buffer). Dominante: **sponsor agendamento** (R5.d + R1.d + R3.h em paralelo).

---

## Riscos identificados

| Risco | Probabilidade | Impacto | Mitigação |
|-------|---------------|---------|-----------|
| Cosign deadline 2026-08-10 perdida (R1) | Baixa se sponsor agenda em T-30d=2026-07-11 | Build failure em prod (script block-on-deadline armado) | Script T-30d/T-15d/T-7d warnings ativos + emergency-override flag |
| Customer trial sem candidato (R10) | Média (pre-release sem revenue) | Bloqueia GA público; canary OK | Pode rodar internal team como pseudo-customer; reduz signal |
| Cenário A Gitea Actions ecosystem gap (R5) | Média (ecosystem novo) | Swap para Cenário C custa 30min (POC files runner-agnostic) | 30-day trial gate definido no ADR |
| R3.f 7-day soak revela violations | Baixa (live scan = 3 helm-test pods só) | Atrasa Deny-flip por +N dias | Helm test pods exemptable; sponsor decide |
| Sponsor mass-meeting timing | Alta | Atrasa canary 1-2 semanas | Batch 4 meetings em 1 sessão T3 multi-tópico |
| ADR D3 cap em 3.95-3.983 (R11) | Certeza | Maturity nunca atinge 4.0 puro | Aceito formalmente; documentar GA como "3.95+" não "4.0" |

---

## Notas finais

- **Engineering eng remaining real (sem sponsor):** ~2h (R3.g buildkit.go digest emit). Tudo o mais é orchestration.
- **Sponsor decisions necessárias:** 4 (R1.d Cosign, R3.h digest enforce, R4.b Stripe prod, R5.d CI platform). Podem ser BATCH em 1 meeting T3 estendido.
- **Operator time total para canary:** ~8h spread across 16 dias (não é blocker).
- **External time:** customer trial (R10) — único item fora do controle LoopZero team.
- **Anti-pattern guard:** este roadmap NÃO inclui novo work autônomo Claude — todos itens são (a) sponsor decisions com diagnostics prontos, (b) operator LIVE actions, ou (c) já-shipped engineering aguardando activation.

## Related

- Plan principal: `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md` v1.1
- Diagnostic packages: `docs/plans/completed/2026-05-26-T*-change-governor-diagnostic.md` (4 docs)
- ADR-2026-05-27-T0.2: `.claude/knowledge-base/decisions/2026-05-27-T0.2-ci-platform-choice.md`
- Maturity baseline: `.claude/knowledge-base/audits/2026-05-26/SUMMARY.md`
- Stripe Portal SDK: `api/internal/services/stripe_portal.go`
- Sigstore component: `infra/pulumi-platform/src/services/sigstore.ts`
- Digest enforcement gate: `cmd/loopzero-ops/internal/quality/prodimagedigests/check.go`
- VAP digest warn-mode: `infra/helm/admission-policies/templates/vap-require-image-digest.yaml`
- POC Gitea Actions: `infra/helm/gitea/values-actions-poc.yaml.disabled` + `.gitea/workflows/quality.yml.example`
- Cosign deadline script: `infra/scripts/security/check-cosign-cutover-deadline.sh`
