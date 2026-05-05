# Charts Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `charts/` e os gates objetivos para considerar os Helm charts publicados do LoopZero em padrão SOTA no contexto de uma PaaS como a nossa.

No LoopZero, `charts/` não é só uma forma de empacotar YAML. Ele é parte do contrato operacional da plataforma porque define:

- como os componentes são instalados e atualizados;
- quais defaults de segurança e operação entram em produção;
- como CRDs, operators, API, cloud e observabilidade são distribuídos;
- como day-2 é entregue via GitOps;
- como o supply-chain é provado (cosign keyless + key-based dual-signing já operacional);
- como drift entre bootstrap (`infra/helm/`) e published (`charts/`) é (ou deveria ser) detectado.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa dos charts, papéis, fronteiras e relação com `infra/helm/` |
| [sota-gates.md](./sota-gates.md) | 19 gates SOTA com critérios de aprovação, evidências e status atual (5 NEW: CH-G15-G19) |
| [research-basis.md](./research-basis.md) | 14 fontes externas (Bitnami, Sigstore, Crossplane, helm-unittest, helm-diff, etc) + evidências internas |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark contra Bitnami, Sigstore, Crossplane, CloudNativePG, Elastic; score 7.2/10 |
| [improvement-roadmap.md](./improvement-roadmap.md) | **NEW** — Roadmap concreto: 5 quick-wins + 6 Sprint 9 targets + 5 NEW gates + 10 anti-patterns identificados |

## Resumo Executivo

No LoopZero, `charts/` só deve ser considerado SOTA quando:

1. Publica componentes com defaults seguros e comportamento previsível.
2. Mantém separação clara entre library chart, charts operacionais, CRDs, umbrella chart e observabilidade/policies.
3. Preserva compatibilidade de values e segurança de upgrade (com `helm-diff` em CI gate).
4. Expõe labels, probes, recursos, RBAC, métricas e supply-chain provenance de forma consistente.
5. Não empurra lógica de produto para dentro dos templates.
6. Permite governança GitOps e day-2 sem divergência silenciosa com `infra/helm/` (drift detector automatizado, não convenção).
7. Trata CRDs, policies, monitoring e SBOM como parte do contrato da plataforma.

## SOTA Score Atual: 7.2 / 10

**Pontos fortes (FAANG-bar):**
- cosign keyless + key-based dual-signing operacional (Fulcio + Rekor + verify step)
- `loopzero-common` library chart com 16 helpers, adoção pervasiva nos 9 charts deployáveis
- `values.schema.json` em 12/12 charts deployáveis com `helm lint --strict` em CI
- CRD lifecycle separado (`loopzero-crds`) com `crd-sync` job em CI
- Umbrella chart com CI rewrite `file://` → `oci://` no publish (solving standalone install)
- Operator multi-binary split (4 charts isolados pós-ADR-0023)

**Gaps a fechar (Sprint 9 alvo: 9.0/10):**
- Ghost charts em `loopzero-platform/Chart.lock` (loopzero-controller, loopzero-tenant-controller sem `Chart.yaml`) — quick-win #1
- Drift regressivo `loopzero-admission-policies` vs `infra/helm/admission-policies` (-5 templates) — quick-win #2
- Sem drift detector automatizado entre `infra/helm/` e `charts/` — quick-win #3
- helm-unittest em apenas 5/13 charts — quick-win #5
- Sem `chart-testing (ct)` install/upgrade matrix — Sprint 9 #2
- Sem matriz K8s (kubeconform 1.31.0 only) — Sprint 9 #2
- Sem SBOM attestation por chart (paridade pendente com container images Sprint 6) — Sprint 9 #5
- `loopzero-common` faltam 6 helpers Bitnami-style (`tplvalues.render`, `kubeVersion`, `checksumAnnotations`, etc) — Sprint 9 #1

## Leitura Recomendada

- Para entender a decomposição atual dos charts: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças em chart: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de Helm + Kubernetes + Sigstore aplicada aqui: [research-basis.md](./research-basis.md)
- Para manter a discussão no contexto de uma PaaS como o LoopZero: [paas-benchmark.md](./paas-benchmark.md)
- **Para executar melhorias concretas:** [improvement-roadmap.md](./improvement-roadmap.md)
