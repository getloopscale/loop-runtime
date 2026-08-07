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

### D7 — Um management cluster global; jurisdição não é imposição externa

**Decisão:** um único management cluster provisiona clientes em qualquer país.

**Rationale:** resolve-se a partir de Q6, respondida pelo sponsor em 2026-08-07 (grill
`.claude/knowledge-base/grills/capi-byoc-topology-grill.md`): **apenas o dado do cliente é
restrito, e o BYOC já o garante por construção** — ele nunca sai da conta do cliente. O
metadado de controle (estado do cluster, ARN de role, logs de reconciliação, métricas) pode
cruzar fronteira, e nenhum contrato hoje exige o contrário. Removida essa imposição, o custo
de jurisdições múltiplas deixou de ter comprador: pagá-lo agora seria antecipar um requisito
que ninguém tem (rung 1 da parsimony ladder).

**Alternativas rejeitadas:** (a) *um mgmt por jurisdição* — hierarquia de 3 níveis do caso
Ditto; correto quando a soberania alcança o plano de controle, o que Q6 diz não ser o caso,
e multiplica o custo operacional por jurisdição; (b) *um mgmt na conta de cada cliente* —
isolamento máximo, mas multiplica o upgrade por cliente sem controlar a janela de nenhum, e
inviabiliza observabilidade central; nenhum dos dois casos estudados escolheu isso.

**Consequências:** o management concentra autoridade para criar e destruir infraestrutura em
N contas de terceiros — mitigado por D5 (assume-role, sem chave de longa duração) e D4
(guardrail de deleção, já em produção). Se um contrato futuro exigir soberania do plano de
controle, a migração para mgmt regional é possível sem reescrever o modelo: o placement por
cliente já é sticky e o roteamento é lookup, como o `DynamicRouter` faz hoje. **O gatilho é
contratual, não técnico** — revisitar quando aparecer.

### D8 — O CAPI roda num cluster de operações separado, não no `mgmt` atual

**Decisão:** nasce um cluster dedicado ao ciclo de vida de clusters. O `mgmt` atual não
absorve o papel.

**Rationale:** precedente interno direto, não analogia. O OpenBao roda em cluster
`loopzero-vault` **separado**, e instalá-lo no `mgmt` é **proibido em todos os tiers** —
*"violação de topologia, não atalho"* (`.claude/rules/cleanroom-up-live-test-mandate.md`,
`Jeito A INQUEBRÁVEL`). A credencial que cria e destrói infraestrutura em contas de clientes
é da mesma classe que a chave de unseal do vault. O `mgmt` atual roda ~50 releases — o
produto inteiro (PostgreSQL, Gitea, MinIO, ArgoCD, Harbor, Ory, observabilidade, os 4
operators, loopzero-api, loopzero-cloud); somar a autoridade sobre a frota de clientes a isso faz um
comprometimento do `mgmt` virar comprometimento de todos os clientes de uma vez.

**Alternativas rejeitadas:** (a) *o `mgmt` absorve* — reusaria ArgoCD, Postgres e
observabilidade já instalados (rung 4, e o CAPI é só mais um conjunto de CRDs reconciliados,
que é o que o `mgmt` já faz), mas concentra produto e autoridade no mesmo blast radius;
(b) *separar só quando houver cliente real* — migrar um plano de controle com estado é mais
caro que nascer separado, e o gatilho tende a escorregar.

**Consequências:** um sexto cluster para operar, com HA, backup e DR próprios. O bootstrap
dele é questão aberta (ver Q7).

### D9 — Não adotar OCM nem Sveltos agora; o modelo pull fica registrado como gatilho

**Decisão:** nenhum dos dois entra. O ArgoCD L3 permanece a camada de distribuição.

**Rationale:** medido em 2026-08-07 — **nenhum dos dois provisiona clusters**; ambos
distribuem para clusters existentes, então ficam acima do CAPI e não resolvem nenhuma questão
aberta deste ADR. Colidem com o ArgoCD já adotado, e seriam uma **terceira** delivery layer
(o elemento 2 do padrão repetível pede uma; o LoopZero já tem duas por design, ADR D1). O
*label-based matching* do Sveltos é GitOps dinâmico com outro nome — a autocrítica exata do
caso Ditto, cuja recomendação retroativa é o rendered manifest pattern que o LoopZero já pratica.
Maturidade não sustenta a aposta para um caminho que carregará infraestrutura de cliente:
OCM é CNCF **Sandbox desde 2021-11-09** (≈5 anos sem promoção); **Sveltos não é projeto
CNCF** (ausente da lista de Sandbox), com `addon-controller` em 553 stars, contato único de
mantenedor e oferta enterprise ao lado. Ambos Apache-2.0; ambos **0 hits** no repositório.

**Alternativas rejeitadas:** (a) *adotar OCM como camada de distribuição BYOC* — o ganho real
(modelo pull) não justifica hoje uma terceira delivery layer; (b) *adotar Sveltos* — mesmo
custo estrutural, com maturidade menor.

**Consequências e gatilho de revisita:** o modelo **pull** (`klusterlet` no OCM, pull mode no
Sveltos) é substantivo para BYOC — o agente no cluster do cliente busca, e o hub não precisa
de acesso de rede ao cluster. O alcance é parcial e precisa ser dito com exatidão:
provisionar exige credencial na conta do cliente de qualquer forma; o pull só reduz a
superfície da fase de **distribuição**. **Gatilho:** se um cliente recusar dar ao nosso hub
acesso de rede ao cluster dele, o pull deixa de ser preferência e vira requisito — e o OCM
entra na pauta por mérito próprio, re-medido naquela data.

### D10 — Contrato de bootstrap IAM: o padrão de referência da indústria, incluindo a validação ativa

**Decisão:** o onboarding BYOC na AWS segue o padrão prescrito por AWS Well-Architected
SEC03-BP09 e pelo guia do Datadog Security Labs para provedores SaaS. Resolve Q2.

**Os oito itens obrigatórios:**

1. **`ExternalId` UUID aleatório, único por cliente, gerado por NÓS.** Nunca fornecido pelo
   cliente — se fosse, um cliente poderia alcançar a conta de outro informando o role ARN
   alheio. Não é segredo, mas não pode ser adivinhável (nunca telefone, nome ou account ID),
   é *read-only* após criado, e nunca reutilizado entre clientes.
2. **Validação ativa no onboarding.** Tentar assumir a role **sem** o `ExternalId`; se a
   chamada funcionar, **recusar a configuração como vulnerável** (*"you should refuse such a
   vulnerable role"*). Esta é a diferença entre o padrão e sua imitação: pesquisa de campo
   mediu **37%** dos vendors sem `ExternalId` correto e **mais 15%** que validam na UI mas
   **não no backend** — ou seja, ~52% erram exatamente este item.
3. **Policy exata, nunca gerenciada.** `ReadOnlyAccess` concede leitura total de S3/DynamoDB;
   `SecurityAudit` expõe *user data* de EC2 e variáveis de ambiente de Lambda. A policy é
   documentada por feature e gerada conforme o que o cliente habilitou.
4. **Dois artefatos:** CloudFormation com *quick-create link* (um clique, menor taxa de erro,
   trilha auditável) **e** recurso de Terraform provider. Não um ou outro.
5. **Session policies** restringindo cada serviço nosso ao escopo mínimo, independentemente do
   que a role concede — defesa em profundidade do nosso lado.
6. **Drift detection** no stack do cliente como prática contínua de auditoria.
7. **Revogação medida.** Estar preparado para revogar acesso, e **medir a operação**: tempo,
   pessoas envolvidas, custo, impacto colateral. Um plano de revogação nunca exercitado não é
   um plano.
8. **Auditar o uso da role E o datastore de `ExternalId`s** — o datastore é alvo por si só.

**Anti-patterns explicitamente proibidos:** pedir IAM *user* em vez de role; confiar na conta
inteira em vez de uma role específica; policies gerenciadas superprivilegiadas; rodar a
integração na conta de management da AWS Organization; múltiplos tenants por conta AWS sem
restrição.

**Rationale:** o padrão não é preferência estética — cada item existe porque sua ausência já
produziu incidente em produtor conhecido. O dado dos 52% mostra que o item 2 é o que mais
falha, e é o único que exige **teste ativo** em vez de configuração.

**Alternativas rejeitadas:** (a) *só módulo Terraform publicado* — estreita o funil de
onboarding a quem usa Terraform, contra o padrão de mercado; (b) *a CLI cria a role* — exigiria
credencial administrativa na conta do cliente no momento do bootstrap, que é exatamente o que
o contrato quer evitar; (c) *só documentação + console manual* — a fonte AWS registra que
automação reduz erro e cria a trilha auditável que o manual não tem.

**Consequências:** o **datastore de `ExternalId`s precisa de dono** — o broker de credenciais
Model B do `loopzero-cloud` é o candidato natural, já existente. E abre a tensão D11.

### D11 — D5 não é uniformemente alcançável entre os providers de D2

**Decisão:** registrar explicitamente que a garantia "sem chave de longa duração" (D5) vale
para AWS e **não** para DigitalOcean, em vez de deixar a descoberta para o primeiro cliente.

**Evidência medida (2026-08-07):**

| Provider | Mecanismo de delegação disponível | D5 satisfeito? |
|---|---|---|
| AWS | `sts:AssumeRole` cross-account com `ExternalId` → credencial **temporária** | **sim** |
| DigitalOcean | OAuth com escopos, ou Personal Access Token (`dop_v1_`). **Sem equivalente de STS/assume-role** | **parcialmente** — o melhor caso (OAuth com escopo, revogável) ainda deixa um token de longa duração do nosso lado |

**Rationale:** D2 escolheu CAPA + CAPDO e D5 prometeu ausência de credencial de longa duração.
As duas decisões não são simultaneamente satisfazíveis em DO, porque o mecanismo não existe na
plataforma — não é questão de implementação. Este é um argumento contra o CAPDO **independente**
do já registrado em D2 (estado Alpha): mesmo que o provider amadurecesse amanhã, a lacuna de
delegação permaneceria.

**Alternativas para a reunião T3 decidir:** (a) aceitar o token de longa duração em DO, com
escopo mínimo via OAuth, rotação declarada e revogação medida; (b) adiar o DO até haver cliente
concreto que o exija; (c) exigir do cliente DO uma conta/projeto dedicado, limitando o raio do
token.

**Consequências:** o contrato de bootstrap tem **duas formas materialmente diferentes** por
provider, e a promessa de segurança feita ao cliente **não pode ser a mesma nos dois**. Dizer
ao cliente DO que "não guardamos credenciais de longa duração" seria falso.

### D12 — Terraform/Terragrunt cria o cluster de operações; `clusterctl move` só para migração

**Decisão:** o cluster de operações de D8 nasce como unit de Terragrunt. O `clusterctl move`
fica reservado ao cenário de **migração** de management cluster. Resolve Q7.

**Rationale — o que a fonte primária diz** (`cluster-api.sigs.k8s.io/clusterctl/commands/move`):

- *"clusterctl move has been designed and developed around the bootstrap use case … currently
  this is the **only use case verified by Cluster API E2E tests**"*;
- *"has **not** been designed for being used as a backup/restore solution"*;
- *"All `Status` fields are **irreversibly lost**"* durante o move;
- race conditions em upgrade/scaling/remediation *"have **never been investigated**"*.

A doc de conceitos do CAPI **não normatiza** como criar o management cluster — apenas o define.
Não existe recomendação oficial de "use X", e o case study com CAPA (SuperOrbital) **não elege
vencedor**: reporta que ambos os caminhos têm complicações, e registra que o pivô puro *"removes
Terraform's role, complicating resource deletion"*.

O que desempata para o LoopZero não é a documentação — é que `loopzero-infra-modules` e `loopzero-infra-live`
já existem e o AM5 entrega EKS de qualquer forma: o cluster de operações vira mais uma unit,
sem ferramenta nova (rung 4 da parsimony ladder). E dá DR declarativo e remoção limpa, que é
exatamente o que a doc afirma que o `move` **não** oferece.

Cada ferramenta fica no cenário em que é testada: Terraform na raiz estática (muda pouco, blast
radius máximo), `clusterctl move` na migração (o caso que os testes E2E cobrem), CAPI
reconciliando os clusters de **cliente**, abaixo da raiz.

**Alternativas rejeitadas:** (a) *bootstrap efêmero + `clusterctl move` como caminho principal* —
legítimo por ser o único caso verificado, mas deixaria o DR do cluster mais crítico da plataforma
sem caminho suportado; (b) *só Terraform, sem documentar o `move`* — perde o caminho de migração
justamente onde ele é testado.

**Consequências:** o cluster de operações precisa de HA, backup e DR próprios (consenso uniforme
das fontes). O `move` precisa de pipeline de validação customizado antes de qualquer uso real,
como a própria doc recomenda para cenários fora do bootstrap.

### D13 — Upgrade dirigido por nós, com janela escolhida pelo cliente e prazo máximo

**Decisão:** nós determinamos **qual** versão e **quando** ela fica disponível; o cliente escolhe
a janela dentro de um limite (a definir em contrato, ordem de 30 dias), findo o qual aplicamos.
Resolve Q3.

**Rationale:** hoje **não existe versão por tenant** — a versão é global da plataforma (medido:
nenhum conceito de `tenantVersion`/`chartVersion` por tenant no código). O BYOC inverte isso, e
a inversão precisa de limite: se cada cliente decidir livremente, o número de versões vivas em
produção cresce sem teto, a matriz de suporte fica impraticável e **um CVE crítico não tem
caminho de aplicação forçada** — o vendor descobre que não controla o próprio risco. No outro
extremo, atualizar sem a janela do cliente contradiz a promessa que motiva o BYOC: se ele comprou
controle sobre o próprio ambiente, mexer nele fora da janela dele tem consequência contratual,
não apenas de percepção.

**Três upgrades distintos, com donos distintos:**

| O quê | Onde roda | Raio |
|---|---|---|
| Kubernetes do cluster do cliente | conta do cliente | um cliente |
| LoopZero (8 CRDs, operators, charts) | conta do cliente | um cliente |
| **Contrato CAPI** | nosso cluster de operações | **todos de uma vez** |

O terceiro não tem janela de cliente para respeitar — e é o mais perigoso, porque a matriz de
compatibilidade é bidimensional (versão do Kubernetes × versão do contrato CAPI, por provider).

**Alternativas rejeitadas:** (a) *cliente controla totalmente* — fragmentação ilimitada, sem
caminho para correção de segurança forçada; (b) *nós controlamos sem janela* — quebra a promessa
BYOC em conta de terceiro; (c) *por tier de plano* — alinha custo ao preço e é o que a maioria
dos vendores faz, mas mantém dois regimes vivos; fica registrada como evolução possível quando
os tiers BYOC existirem.

**Consequências:** é preciso construir agendamento, notificação e o mecanismo de aplicação no
prazo — nenhum dos três existe hoje. O prazo máximo precisa constar do contrato, não só do código.

### D14 — Só telemetria de plataforma atravessa a fronteira

**Decisão:** métricas e eventos dos **nossos** componentes (status de reconcile dos operators,
saúde do cluster, versão instalada, resultado de deploy, latência de build) são enviados ao nosso
plano de observabilidade. Logs e traces da **aplicação do cliente** permanecem no cluster dele.
Resolve Q5.

**Rationale:** aplica à telemetria a linha que D7 já traçou — metadado de controle cruza, dado do
cliente não. Logs de aplicação **são** dado do cliente; tratá-los como telemetria operacional
contradiria a premissa do BYOC. Ao mesmo tempo, mandar nada tornaria os **7 SLOs declarados**
(`infra/sloth/`) não-mensuráveis para clientes BYOC e deixaria o suporte às cegas, com todo
diagnóstico virando pedido de acesso ao cliente.

**Alternativas rejeitadas:** (a) *nada sai, só health agregado* — máxima aderência à promessa, mas
custa os SLOs e o suporte; (b) *tudo vem para nós* — contradiz D7 e só seria defensável com
consentimento explícito e por escrito; (c) *plataforma sempre, app opcional* — cobre os dois
mundos e vira item de contrato, mas exige dois caminhos de coleta e prova auditável de que o
opt-out não envia nada; fica registrada como evolução quando um cliente pedir suporte profundo.

**Consequências e risco declarado:** a separação precisa ser feita **na origem**. A stack já
instalada (Loki, Tempo, OTel, Promtail, monitoring, blackbox, OpenCost) foi construída para um
mundo onde tudo é nosso; um Promtail mal escopado no cluster do cliente **vaza log de aplicação
sem que ninguém perceba**. Isso pede um teste que prove a ausência de vazamento, na mesma forma
do zero-leak que já existe para secrets — não uma revisão de configuração.

## Questões abertas — pauta da reunião T3

> **Atualizado 2026-08-07** pelo grill `capi-byoc-topology`: Q1, Q4 e Q6 estão **resolvidas**
> (viraram D7 e D8). Q7 foi aberta como consequência de D8. Restam Q2, Q3, Q5 e Q7.

| # | Questão | Por que só a reunião decide |
|---|---|---|
| ~~Q1~~ | ~~Onde mora o management cluster?~~ | ✅ **RESOLVIDA 2026-08-07 → D7** (um mgmt global) |
| ~~Q2~~ | ~~Contrato de bootstrap IAM por provider~~ | ✅ **RESOLVIDA 2026-08-07 → D10** (padrão AWS/Datadog, 8 itens, com validação ativa do ExternalId) **+ D11** (a promessa de D5 não vale para DO) |
| ~~Q3~~ | ~~Quem opera o upgrade dos clusters de cliente~~ | ✅ **RESOLVIDA → D13** |
| ~~Q4~~ | ~~O `mgmt` atual absorve, ou nasce um cluster de operações separado?~~ | ✅ **RESOLVIDA 2026-08-07 → D8** (cluster separado, pelo precedente `Jeito A` do vault) |
| ~~Q5~~ | ~~Observabilidade cross-cloud e cross-conta~~ | ✅ **RESOLVIDA → D14** |
| ~~Q6~~ | ~~Escopo de dado que não pode sair do país~~ | ✅ **RESOLVIDA 2026-08-07** — só o dado do cliente, que o BYOC já garante. Metadado de controle pode cruzar. Destravou Q1 |
| ~~Q7~~ | ~~Quem cria o cluster de operações? (bootstrap circular)~~ | ✅ **RESOLVIDA 2026-08-07 → D12** (Terraform na raiz; `move` só para migração) |

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
