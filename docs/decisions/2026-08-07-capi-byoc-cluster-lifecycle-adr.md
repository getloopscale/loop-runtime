---
type: decision
title: "Cluster API como ciclo de vida de cluster para BYOC multi-cloud e multi-país"
---

# ADR — Cluster API (CAPI) como ciclo de vida de cluster para BYOC

**Date:** 2026-08-07
**Tier:** T3 (arquitetura + stack + fluxo crítico — `.claude/rules/pre-release-governance.md`)
**Status:** **PROPOSED** — não implementar antes da ratificação em reunião T3
**Meeting:** *pendente* — este documento é o diagnóstico e a pauta
**Supersedes:** nada. **Interage com:** ADR-0008 / ADR-0021 D3 (placement sticky)

> **Status é PROPOSED por contrato, não por cautela.** A governança exige reunião e ADR
> ratificado **antes** do código para mudanças T3. Nenhum item deste ADR autoriza
> implementação até o campo `Status` mudar para RATIFIED com registro de reunião.

## Context

### O requisito que motiva a decisão

O produto passará a ser provisionado **na cloud do próprio cliente (BYOC)**, em
**múltiplos países**. Isso torna o isolamento um **requisito de produto** e a soberania
de dados um **requisito externo** — as duas condições sob as quais os contra-indicadores
de escala de fleet deixam de se aplicar (`.claude/skills/multi-cluster-fleet-specialist/references/scaling-decision.md § 3`).

Some-se o driver estrutural de **version lifecycle**: o LoopZero tem **8 CRDs**
(`operators/api/v1/*_types.go`), e CRDs são cluster-scoped — uma única versão global por
cluster. Com clientes em clusters próprios, o rollout independente por cliente deixa de
ser otimização e vira a única forma segura de versionar.

### O estado medido em 2026-08-07

Medido por grep no repositório (Regra 12 do `CLAUDE.md` — adoção se verifica com grep,
nunca de memória). Re-medir antes de citar: os comandos estão em
`.claude/skills/multi-cluster-fleet-specialist/references/loopzero-evidence.md`.

| Fato | Valor | Evidência |
|---|---|---|
| CAPI adotado | **não** | `grep -r "cluster\.x-k8s\.io"` → **0 hits** |
| Clusters no dev loop | 5 | `infra/k3d/*.yaml` |
| CRDs | 8 | `operators/api/v1/*_types.go` |
| Operators (binários split) | 4 | `operators/cmd/{tenant,app,preview,platform}-operator` |
| VAPs | 10 | `infra/helm/admission-policies/templates/vap-*.yaml` |
| VAPs que bloqueiam `DELETE` | **1** (só NetworkPolicies de plataforma) | `vap-protect-platform-networkpolicies.yaml` |
| Cascade delete na `Application` | **ativo** | `operators/internal/argocd/real_provisioner.go:205` |
| Sync do ArgoCD por tenant | manual (`automated: nil`) | `real_provisioner.go:220` |
| Rebalanceamento entre cells | **inexistente** | `grep -rniE "rebalanc" api/internal` → **0 hits** |
| Releases Helm declaradas (L2) | 101 (mgmt 54 · runtime 28 · build 11 · vault 8) | `infra/helmfile/*.helmfile.yaml.gotmpl` |
| Cadeia canônica de seeds | 27 steps | `infra/scripts/local/seed-post-cleanroom-all.sh` |
| Fase do produto | **pre-release** | `.claude/rules/pre-release-governance.md:15` |

### O que o LoopZero já possui do padrão-alvo

Três dos quatro elementos do padrão repetível de fleet management já existem:
source of truth declarativa (Helmfile L2 + gitops + banco), delivery layer (Argo CD),
management cluster (`mgmt`), e customização como custom controller (8 CRDs + 4 operators,
com `no-new-bash` fechando a alternativa).

Dois ativos específicos encurtam o caminho:

1. **`ClusterCredential` já é o registro de cluster remoto** — modela `APIServer`,
   `SecretRef`, `Scope{ArgoCD, Operators}`, `TLS`, `CellID`, com
   `Status.Connected` + `LastCheckedAt`. É o papel que o *operator custom de health watch*
   cumpre no desenho de referência.
2. **O LoopZero já faz rendered manifests** (`api/internal/manifests/renderer.go` →
   `api/internal/gitops/gitea_pusher.go`) — que é a recomendação **retroativa** do
   precedente BYOC estudado, após arrependimento com GitOps dinâmico.

## Decisions

### D1 — Adotar Cluster API como ciclo de vida de cluster para BYOC

Clusters de cliente passam a ser descritos como CRs reconciliados (`Cluster`,
`MachineDeployment`, `Machine`, mais os tipos do provider) no management cluster.

**Rationale:** o gargalo do BYOC é o tempo até *workload-ready* em conta de terceiro. O
precedente estudado saiu de **1–2 semanas por cluster BYOC** para **~15 minutos**, com
human-in-the-loop reduzido à entrada inicial de dados. Terraform é adequado para recursos
estáticos e inadequado para ambientes dinâmicos — e BYOC multi-cliente é dinâmico por
definição.

**Rejected alternatives:**

- *Terraform/Terragrunt por cliente* — é o estado anterior do precedente (2–3 dias por
  cluster interno, 1–2 semanas em BYOC). Não escala para N clientes self-service.
- *Scripts de provisionamento* — proibido por `no-new-bash` (D3) e pelo elemento 4 do
  padrão: customização vira custom controller, nunca runbook.
- *Não fazer nada / seguir com 2 clusters próprios* — não atende BYOC nem soberania.

### D2 — Providers do v1: CAPA (AWS) e CAPDO (DigitalOcean)

**Rationale:** decisão do sponsor em 2026-08-07. CAPA é provider oficial maduro e alinhado
ao destino da re-plataforma própria (AM5/EKS); CAPDO cobre clientes em DigitalOcean.

**Risco declarado e aceito — CAPDO:** o README upstream declara estado **Alpha**, *"may
not be production ready. There is no backwards-compatibility guarantee at this point"*.
Numa camada que provisiona nós, quebra de compatibilidade não é inconveniência de upgrade.
O precedente registra *first-adopter risk* materializado: um bug de Provider ID no CAPZ
(provider **maduro**) causou perda de **~60% dos nós de produção** de uma vez.

**Mitigações que D2 torna obrigatórias:**

- versão do CAPDO **pinada por digest/tag explícita**, nunca faixa flutuante;
- **caminho de fork documentado** antes do primeiro cliente em DO (o precedente manteve
  fork do provider por meses para aplicar hot fix);
- **tier de suporte distinto** do CAPA — CAPDO não recebe o mesmo compromisso de SLA
  enquanto permanecer Alpha upstream;
- teste de unicidade de identificador de nó **entre node pools e entre clusters** antes de
  qualquer cliente real (é exatamente a classe do bug do precedente).

**Rejected alternatives:**

- *Só CAPA no v1* — recomendação técnica original; rejeitada por necessidade comercial de
  atender clientes em DO.
- *CAPZ (Azure) no v1* — adiado; entra quando houver cliente concreto.

### D3 — Manter rendered manifests; **não** adotar ApplicationSet dinâmico

O caminho `renderer.go → gitea_pusher.go → Argo CD` permanece. Geração dinâmica de
`Application` por plugin generator fica **fora de escopo**.

**Rationale:** é a autocrítica central do precedente — GitOps dinâmico produziu *"um sprawl
grande e frouxamente conectado de YAML que poucos engenheiros conseguiam acompanhar"*, e
o autor recomendaria hoje o rendered manifest pattern. O trade-off é nomeado: GitOps
dinâmico compra elasticidade e paga em legibilidade. Com o time medido em `ROADMAP.md`
(**1 developer + par LLM**), legibilidade é a moeda escassa.

**Rejected alternatives:** replicar o diagrama do precedente literalmente — replicaria a
escolha que ele próprio desfaria.

### D4 — Guardrail `DELETE` default-deny é **pré-requisito bloqueante** (P0)

VAP default-deny em `DELETE` sobre recursos de fronteira — `Application`, `Tenant`,
`Environment`, `ClusterCredential`, e `Cluster` / `MachineDeployment` quando existirem —
com escape hatch explícito para as SAs que legitimamente removem, na forma dos
`matchConditions` já usados em `vap-tenant-namespace`.

**Este item é T2, não depende de nada do CAPI, e deve estar em produção ANTES do primeiro
objeto `Cluster` existir.**

**Rationale:** o LoopZero tem hoje os dois ingredientes do incidente conhecido — cascade via
`resources-finalizer.argocd.argoproj.io` (`real_provisioner.go:205`) e **nenhum** VAP
bloqueando `DELETE` dos recursos de fronteira. No precedente, a cascata
`CRD deletado → Applications → recursos CAPI → nós` **danificou o cluster de um cliente**.
Sob BYOC isso deixa de ser incidente interno e vira destruição de infraestrutura em conta
de terceiro, com consequência contratual.

O sync manual (`automated: nil`) e o `gitops.Remove` FATAL são amortecedores reais, mas
**não fecham o buraco**: a cascata dispara no delete do objeto, independentemente de
auto-sync estar desligado.

**Custo declarado:** o guardrail torna tear-downs tediosos. Quem o construiu no precedente
disse as duas coisas — que doeu, e que fez dormir melhor.

### D5 — Credencial de cloud do cliente: bootstrap IAM pelo cliente, sem chave de longa duração

O cliente executa um bootstrap de IAM na própria conta; o management cluster **assume role**.
Nenhuma credencial de longa duração de cliente é persistida do nosso lado.

**Rationale:** o precedente alcançou provisionamento cross-account pelo mesmo processo do
interno *"desde que o cliente tenha feito o bootstrap de IAM"*. A alternativa concentra, num
único cluster, chaves permanentes capazes de destruir infraestrutura de N clientes.

**Consequência que precisa ficar explícita:** o `mgmt` deixa de ser o cluster que guarda
Gitea/PG/MinIO/ArgoCD e passa a ser o cluster que detém autoridade para criar e destruir
infraestrutura em contas de terceiros. O modelo de ameaça dele muda e precisa ser revisto.

### D6 — Placement ganha um segundo modo; ADR-0008 / ADR-0021 **não** são revogadas

O placement por bin-packing em cells (`api/internal/placement/engine.go`, `maxPerCell`
default 500) permanece para tenants da nossa infra. BYOC introduz um modo distinto:
o cliente vai para **o cluster dele**, sem bin-packing e sem escolha de cell.

**Rationale:** são dois problemas diferentes. Revogar a stickiness quebraria continuidade
de DNS, afinidade de estado e auditabilidade, que são os motivos registrados da ADR-0021 D3.

**Consequência medida que a reunião precisa absorver:** não existe rebalanceamento nem
drain (`grep -rniE "rebalanc" api/internal` → **0 hits**); o único caminho que move um
tenant é `loopzero admin tenants pin`, manual e por tenant. Portanto **a elasticidade é de mão
única**: scale-up sob demanda funciona, scale-down não. Qualquer expectativa de redução de
custo por encolhimento de fleet depende de trabalho que **não** está neste ADR.

## Questões abertas — pauta da reunião T3

| # | Questão | Por que só a reunião decide |
|---|---|---|
| Q1 | **Onde mora o management cluster?** Um global · um por jurisdição (hierarquia de 3 níveis: raiz Terraform → mgmt regional → clusters de cliente) · um por cliente | Depende do que os contratos de soberania exigem: se nem o plano de controle pode tocar dado fora do país, a topologia é forçada. Multiplica custo operacional pelo número de jurisdições |
| Q2 | Qual o **contrato de bootstrap IAM** que o cliente executa, por provider? | Superfície de segurança + documento voltado ao cliente |
| Q3 | **Quem opera o upgrade** dos clusters de cliente, e com que janela? | Em BYOC não controlamos a janela. Versão do K8s × versão do contrato CAPI × disponibilidade do cliente |
| Q4 | O `mgmt` atual absorve o papel, ou nasce um cluster de operações separado? | O precedente usou tecnologia deliberadamente *"chata e estática"* na raiz, onde o blast radius é máximo |
| Q5 | Observabilidade e suporte **cross-cloud e cross-conta** | Loki/Tempo/OTel já existem; alcançar conta de terceiro é outro problema |
| Q6 | Escopo de dado que **não pode** sair do país, por jurisdição | Determina Q1. Requisito externo, não escolha de engenharia |

## Consequences

**Positive:**

- Habilita BYOC, multi-país e multicloud — sem eles não há produto para esses clientes.
- Rollout de versão independente por cliente, resolvendo o limite de granularidade imposto
  pelos 8 CRDs cluster-scoped.
- Provisionamento self-service com human-in-the-loop reduzido à entrada de dados.
- Decisão tomada em **pre-release**, quando compatibilidade retroativa não é exigida e o
  redesenho limpo é a política declarada.

**Negative / Trade-offs:**

- **Upgrade em duas dimensões, permanente:** versão do Kubernetes × versão do contrato CAPI
  (v1alpha3 → v1alpha4 → v1beta1 exigiram migração de CRD), com matriz por provider.
- **Bootstrap circular:** o management cluster precisa existir antes do CAPI; o pivô
  (`clusterctl move`) tem janela de indisponibilidade real. O LoopZero já tem a versão local
  desta armadilha registrada (`CLAUDE.md § Bootstrap circular X-fetches-X`).
- **Cascata com raio maior:** `Cluster` → `MachineDeployment` → `Machine` → instância real.
- **First-adopter risk no CAPDO** (D2), com custo potencial de manter fork.
- **Custo operacional adicional no `mgmt`**, que não é custo de cluster mas é custo.
- O CAPI **não reduz** o caminho crítico de *workload-ready*: as 101 releases Helm e os 27
  steps de seed permanecem. Ele substitui a criação do cluster, não a habilitação dele.

**Constraints introduced:**

- Nenhum objeto `Cluster` pode existir antes de D4 estar em produção.
- CAPDO permanece em tier de suporte distinto enquanto for Alpha upstream.
- Provider novo exige teste de unicidade de identificador de nó antes de cliente real.
- Toda customização de provisionamento vira custom controller — nunca script (`no-new-bash`).

## Sequência proposta (pós-ratificação)

| Ordem | Item | Tier | Depende de |
|---|---|---|---|
| 1 | VAP default-deny em `DELETE` (D4) | T2 | nada — **pode começar antes da reunião** |
| 2 | Ratificação deste ADR | T3 | reunião |
| 3 | Contrato de bootstrap IAM do cliente (D5) | T2/T3 | Q2 |
| 4 | CAPI core + **um** provider (CAPA), contra cliente-piloto | T3 | 1, 2, 3 |
| 5 | CAPDO, com as mitigações de D2 | T3 | 4 verde |

## Cross-references

- Skill de decisão de fleet: `.claude/skills/multi-cluster-fleet-specialist/SKILL.md`
- Contra-indicadores e razões legítimas: `references/scaling-decision.md`
- Precedentes e falhas catastróficas: `references/case-studies.md`
- Maturidade de providers e custo operacional do CAPI: `references/cncf-ecosystem.md`
- Comandos de re-medição: `references/loopzero-evidence.md`
- Governança T0-T3: `.claude/rules/pre-release-governance.md`
- Placement sticky: `api/internal/placement/engine.go`, ADR-0008 / ADR-0021 D3
