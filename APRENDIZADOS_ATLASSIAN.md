# Aprendizados Aplicáveis ao LoopZero — 8 anos de plataforma na Atlassian

> Documento de referência derivado da reflexão de um engenheiro que passou 8 anos construindo a plataforma de load balancing centralizado da Atlassian (vídeo YouTube `55pTFVoclvE`).
>
> O LoopZero resolve **a mesma classe de problema** (broker de provisionamento + control plane dinâmico + plataforma multi-tenant), apenas para deploy de aplicações em vez de load balancing. Este documento extrai as lições aplicáveis e propõe ações concretas.

**Data:** 2026-05-21
**Origem:** `/home/paulo/Projetos/transcricao/55pTFVoclvE_aprendizados.md`

---

## Sumário

1. [Self-service forçado via remoção do caminho antigo](#1-self-service-forçado-via-remoção-do-caminho-antigo)
2. [Templates + contexto dinâmico = base do control plane](#2-templates--contexto-dinâmico--base-do-control-plane)
3. [Sidecar para concerns transversais na borda](#3-sidecar-para-concerns-transversais-na-borda)
4. [Async-first com fila entre API e worker](#4-async-first-com-fila-entre-api-e-worker)
5. [Imagem padronizada + IaC declarativa = fundação do scale](#5-imagem-padronizada--iac-declarativa--fundação-do-scale)
6. [Churn no codebase como smell](#6-churn-no-codebase-como-smell)
7. [Onboarding para on-call ≠ documentação para devs](#7-onboarding-para-on-call--documentação-para-devs)
8. [Compliance é tedioso mas inevitável](#8-compliance-é-tedioso-mas-inevitável)
9. [Pergunta-âncora dos 12 meses](#9-pergunta-âncora-dos-12-meses)
10. [Building é fácil; mudar ao longo do tempo é difícil](#10-building-é-fácil-mudar-ao-longo-do-tempo-é-difícil)
11. [Lições não-técnicas](#11-lições-não-técnicas)
12. [Plano de ação priorizado](#12-plano-de-ação-priorizado)

---

## 1. Self-service forçado via remoção do caminho antigo

### O que aprenderam na Atlassian

A plataforma anteriormente provia **load balancing básico** a todo serviço por padrão. Para resolver o problema de **exposições públicas acidentais**, a equipe **removeu** o caminho básico. Ficou assim:

- Não era mais possível expor um serviço publicamente pelo load balancer básico.
- Para expor publicamente, era **obrigatório passar pela infra centralizada** e **configurar explicitamente** a intenção.

Resultado: declaração explícita de intenção elimina erros silenciosos.

### Aplicação no LoopZero

O `loopzero.yaml` já é o ponto único de entrada, mas:

- Existem caminhos legados/escapatórias para usuários self-hosted (manifests Kubernetes diretos).
- O **admission policy DENY mode** está marcado como pendente no README (Sprint 9 com cosign keyless + Fulcio).

**A lição valida o caminho:** flipar para DENY não é "feature de segurança extra" — é o que **cria a plataforma de verdade**. Sem isso, o LoopZero é apenas uma alternativa entre outras formas de fazer deploy no mesmo cluster.

### Ação concreta

- Acelerar Sprint 9 (admission DENY mode + cosign keyless verification).
- Bloquear deploy de workloads que **não passam pela CRD `Application`** via ValidatingWebhook na admission.
- Documentar **explicitamente** quais escapatórias serão fechadas e quando.

---

## 2. Templates + contexto dinâmico = base do control plane

### O que aprenderam na Atlassian

O **Sovereign** (Envoy control plane open source criado pelo autor) operava assim:

```
[broker DB] ──┐
              ├──> Sovereign (templates + context) ──> Envoy proxies
[S3/outros] ──┘                                          ↓
                                                    Config dinâmica em runtime
```

- Dev manda **JSON pequeno** com parâmetros simples.
- Parâmetros são **validados** primeiro.
- Templates renderizam **configuração Envoy completa** (access logs, routing, headers, redirects, etc.).
- Contexto vem de **múltiplas fontes** dinâmicas.
- Envoy faz polling do Sovereign e atualiza em runtime.

**A descoberta central do autor:** ~70% do trabalho de engenharia real depois que o motor funciona está em **validar parâmetros simples** para garantir que produzam recursos válidos. Sem isso, "qualquer rota pode despachar para qualquer cluster" — risco de segurança e caos.

### Aplicação no LoopZero

O padrão já existe nos operadores (CRD → manifests). O ponto a internalizar é **onde concentrar esforço de engenharia**:

- Auditar `api/internal/...` e schemas de `loopzero.yaml`: parâmetros simples produzem **sempre** recursos válidos?
- Quais classes de inputs maliciosos ou malformados podem gerar manifests que comprometem outros tenants?
- Validação de **invariantes cross-app dentro de um mesmo tenant** (ex: dois apps querendo o mesmo subdomain, scaling acima do plan limit em runtime).

### Ação concreta

- Sessão dedicada de threat modeling no schema do `loopzero.yaml`.
- Lista de invariantes que precisam ser validadas **antes** dos parâmetros chegarem aos templates dos operadores.
- Property-based testing nos validators (já há infra de teste; adicionar `testing/quick` ou `gopter` para Go).

---

## 3. Sidecar para concerns transversais na borda

### O que aprenderam na Atlassian

Concerns transversais foram resolvidos em **sidecars separados do proxy principal**:

| Concern | Implementação |
|---|---|
| **DDoS protection** | CloudFront (camada anterior) |
| **Access logs** | Network filter nativo do Envoy (HCM) |
| **Autenticação** | Sidecar próprio em Rust |
| **Autorização** | Sidecar contribuído por outro time |
| **Rate limiting** | Sidecar contribuído por outro time |

**Lições:**

- Sidecars **recebem configuração dinâmica over the wire**.
- AMI provisioning baixa e configura os sidecars junto com o Envoy.
- Modelo permite que **múltiplos times contribuam** sem tocar no core do proxy.

### Aplicação no LoopZero

Hoje o LoopZero usa **Traefik** para tudo no edge. Concerns como rate limit por plano (Starter/Pro/Team) já são enforced via middleware HTTP 402 na API Go, mas:

- Esse enforcement está **longe do tráfego** — toda request bate na API Go antes de ser barrada.
- Em escala, isso vira custo desnecessário.

**Oportunidades:**

- Migrar enforcement de plano para **Traefik middleware** ou **EnvoyFilter** no ingress de cada cell.
- Se um dia surgir necessidade de cross-team contribution (ex: time de billing adiciona throttling), o modelo de sidecar deixa isso possível sem refator do core.
- Considerar sidecar de **observability unificado** (próximo tópico).

### Ação concreta

- Spike de 1 semana: mover enforcement de plan limits para Traefik middleware com config dinâmica via CRD.
- Medir latência antes/depois e custo de CPU na API Go.

---

## 4. Async-first com fila entre API e worker

### O que aprenderam na Atlassian

Open Service Broker tinha arquitetura **sempre assíncrona**:

```
Client → FastAPI (web) → SQS → Worker → Provisioning Task
                                       ↓
                                  DynamoDB
                                       ↑
                          Client faz polling para status
```

- API **não executa trabalho pesado** — só drop task na fila.
- Worker processa, escreve no DB.
- Cliente faz polling.

**Lição implícita:** escolheram **DynamoDB + SQS + polling**, não Redis Streams, não Kafka. Simples e battle-tested.

### Aplicação no LoopZero

LoopZero já segue este padrão (Argo Workflows + SSE). **Validação do design.**

**Ponto a internalizar:** a **PG work queue** mencionada no README (com leader fencing + build admission gate) é o equivalente correto. **Não trocar por algo mais "moderno" sem motivo concreto** — `Não Reinvente a Roda` aplica também a stack de mensageria.

### Ação concreta

- Nenhuma — apenas reforçar a decisão atual.
- Se aparecer pressão para mover para Kafka/NATS, exigir motivo concreto antes de migrar.

---

## 5. Imagem padronizada + IaC declarativa = fundação do scale

### O que aprenderam na Atlassian

Stack de imagem:

- **HashiCorp Packer** + **Salt Stack** → AMI única.
- AMI continha: Envoy + logging agents + security/hardening + network tuning + containers + tracing + **observability agent unificado** (cobrindo logging, tracing, metrics).
- **CloudFormation** referenciava a AMI e provisionava ~**2000 proxies** em ~**13 regiões**.

### Aplicação no LoopZero

Análogo direto: imagens base dos pods de runtime + Pulumi (5 projetos: `pulumi/`, `pulumi-infra/`, `pulumi-platform/`, `pulumi-drill/`, `pulumi-shared/`).

**Aprendizado novo a aplicar:** observability agent unificado.

Atualmente o LoopZero lista (README):

- Prometheus + Grafana + Loki + Tempo + OpenTelemetry — separados.

Atlassian consolidou em **um único agent**. Isso reduz:

- Superfície de manutenção.
- Sidecars/DaemonSets para gerenciar.
- Tempo de debug (uma fonte só de telemetria por host).

### Ação concreta

- Avaliar consolidação em **OpenTelemetry Collector único** como DaemonSet (recebe logs + métricas + traces, dispatch para Loki/Prometheus/Tempo).
- ADR para registrar a decisão (manter separado **ou** consolidar) com motivo.

---

## 6. Churn no codebase como smell

### O que aprenderam na Atlassian

> "Áreas que mais sofrem mudanças (churn) se tornam previsíveis. Churn é um smell: indica que aquela parte vai continuar crescendo em tamanho e complexidade. Algo precisa ser feito ali."

Conforme você muda, fica **mais difícil mudar** depois. Coisas se acoplam — mudar em uma área afeta outra → trabalho de detangling.

### Aplicação no LoopZero

LoopZero tem **35 comandos CLI, 8 CRDs, 4 operadores, 5 projetos Pulumi, 48 workflows CI**. Áreas com muito churn são candidatas óbvias a problemas.

### Ação concreta

```bash
# Rodar no repo loopzero
git log --since="6 months ago" --pretty=format: --name-only \
  | sort | uniq -c | sort -rn | head -30
```

Para os 3 hotspots mais altos:

- Auditar acoplamento (quem depende deles?).
- Decidir: **refactor antes de mais features**, ou aceitar dívida explicitamente.
- Registrar em ADR.

---

## 7. Onboarding para on-call ≠ documentação para devs

### O que aprenderam na Atlassian

> "A parte difícil não é a doc inicial — é manter o conhecimento operacional quando pessoas saem."

Cenários específicos que precisavam estar documentados:

- AWS outage → DynamoDB inacessível, e agora?
- SQS parada → não consegue fazer provisioning, qual o impacto?
- Proxy recebe **configuração válida mas que destrói tráfego** → como detectar e mitigar?
- O que **cada log message significa**.
- Quais **métricas checar** quando algo vai mal.

### Aplicação no LoopZero

O README lista muita capacidade técnica, mas faltam runbooks por cenário. Exemplos óbvios para o LoopZero:

- Cell-1 perde `ClusterCredential` (já há heal H12+H13 — registrar no runbook).
- PG advisory lock fica stuck.
- ArgoCD desync persistente em uma cell.
- `cosign sign` falha mas build sucede — qual o impacto?
- Argo Rollouts abort em produção: como retomar?
- KEDA HPA ownership transfer falha — como recuperar?

### Ação concreta

Criar `docs/runbooks/` com um arquivo por cenário no formato:

```markdown
# Runbook: <Cenário>

## Sintoma
O que o operador observa.

## Métricas para confirmar
- `metric_name_1` < X
- Alert em <dashboard>

## Causas prováveis
1. ...
2. ...

## Ação
1. Comando 1
2. Comando 2
3. Verificar com X

## Pós-incidente
- Atualizar este runbook
- Abrir issue se padrão se repetir
```

Lista inicial sugerida (10 runbooks):

1. Cell perde ClusterCredential
2. PG advisory lock stuck
3. ArgoCD desync persistente
4. cosign signing failure
5. Argo Rollouts aborted em produção
6. KEDA HPA ownership transfer falhou
7. Build stuck em Argo Workflows
8. ESO secret sync falha
9. Cloudflare Tunnel desconectou
10. PG work queue backlog crescendo

---

## 8. Compliance é tedioso mas inevitável

### O que aprenderam na Atlassian

Após features técnicas vieram **requisitos de compliance**. Trabalho descrito como **"tedious and boring"**. Não envolveu construir coisas novas — só **checklist ticking** sobre o que já existia.

### Aplicação no LoopZero

README já lista pendências:

- SBOM CycloneDX ✅ (Sprint 6).
- Admission policy DENY mode (pendente).
- SSO/SAML (Phase 3).
- SOC2 não mencionado.

**Aviso do autor:** esse trabalho **não para** depois do primeiro lote. Compliance é uma **classe permanente** de trabalho, não tarefa pontual.

### Ação concreta

- Designar **orçamento fixo de capacity por sprint** para compliance (ex: 10% do time).
- Manter `docs/compliance.md` com matriz: requisito → status → evidência → próximo audit.
- Aceitar que sprints inteiros podem ser dedicados a compliance sem entregar features novas.

---

## 9. Pergunta-âncora dos 12 meses

### O que aprenderam na Atlassian

Na entrevista, o autor perguntou aos entrevistadores:

> "Pense daqui a 12 meses olhando retrospectivamente. O que eu teria que ter alcançado para você dizer que foi uma boa decisão me contratar?"

A resposta definiu o trabalho dele por 2 anos: construir o Open Service Broker. Acordo explícito de expectativas eliminou ambiguidade.

### Aplicação no LoopZero

Hoje o README tem muitas métricas operacionais:

- 80/80 GREEN warm dogfood.
- P50=648ms / P95=705ms rollback.
- 12/12 ADR gates LIVE GREEN.

Mas falta uma **âncora de outcome** explícita. Métricas operacionais são meio; outcomes são fim.

### Ação concreta

Definir 2-3 métricas de outcome para 12 meses (até maio/2027). Sugestões a discutir:

- **N clientes pagantes ativos** (Starter/Pro/Team/Enterprise).
- **N deploys/dia** em produção (não em dogfood).
- **% de deploys que completam sem intervenção humana**.
- **Tempo médio de onboarding novo cliente** (login → primeiro deploy).

Adicionar à raiz do README como **seção dedicada** acima de "What Works".

---

## 10. Building é fácil; mudar ao longo do tempo é difícil

### O que aprenderam na Atlassian

> "Building something is easy. Changing it and making sure that you can still change it over time is difficult. Things start to get coupled, and all of a sudden when you change something in one area, it affects another."

Possível otimismo cauteloso do autor: **talvez um LLM possa fazer o detangling**. Mas ele evita ser otimista demais.

### Aplicação no LoopZero

O Stability Contract no README cobre o **externo** (CLI, JSON, exit codes). E o **interno**?

- CRDs mudam → quantos operadores e quantos comandos CLI quebram?
- Schemas de `loopzero.yaml` evoluem → como migrações são tratadas para clientes existentes?
- Quando uma shared lib em `pkg/` muda assinatura, qual o blast radius?

### Ação concreta

- Definir **Stability Contract interno** (mesmo que mais frouxo que o externo).
- Adicionar checks de **breaking change detection** entre versões de CRDs e shared libs no CI.
- Documentar **política de deprecation interna**: quanto tempo de aviso antes de remover símbolo público em `pkg/`.

---

## 11. Lições não-técnicas

### 11.1 Diplomacia e conflitos

> "Tive conflitos reais com algumas pessoas — mas ainda respeito essas pessoas. Conflitos não significam desrespeito."

- Self-awareness + awareness do outro.
- Antecipar conflitos antes que estourem.
- Foi fonte de **grande estresse** e **afetou performance** do autor.

**Aplicação:** se o time do LoopZero crescer, prever rituais de **conflict resolution explícitos** (1:1s estruturados, retros honestas, mediação quando necessário).

### 11.2 Mentoring vs treinamento — distinção honesta

O autor admite que **mentoring é difícil para ele**, mesmo sendo bom em treinar e simplificar conceitos. Diferença:

- **Treinamento:** apontar lacunas e entregar entendimento sobre um problema específico.
- **Mentoring:** balancear "não dar resposta direta" vs "não deixar tão travado que frustra".

Causa raiz que ele identifica: **nunca foi mentorado**, então falta modelo.

**Aplicação:** se o LoopZero tiver interns ou júniors, distinguir explicitamente quando se está **treinando** (entregar entendimento) vs **mentorando** (fazer pensar). Os dois são válidos, mas misturar atrapalha.

### 11.3 Maintenance burden de código vibe-coded / AI-assisted

> "Será interessante observar o que acontece com apps vibe coded e gerados por IA. Pessoas que não entendem o que criaram vão enfrentar a carga de manutenção depois. A carga não aparece no início — só com o tempo."

**Aplicação:** o LoopZero usa Claude Code e tem CLAUDE.md robusto. Vale auditar periodicamente:

- O time **entende** o código que foi gerado pela IA?
- Quem é o **dono** de cada subsistema — humano ou só "o que a IA fez na sprint X"?
- Decisões arquiteturais foram registradas em ADRs **com motivação humana**, ou só commit messages de "implementei X"?

---

## 12. Plano de ação priorizado

| # | Ação | Esforço | Impacto | Princípio aplicado |
|---|------|---------|---------|--------------------|
| 1 | Flipar admission policy para DENY mode (Sprint 9) | M | Alto | Self-service forçado |
| 2 | Criar `docs/runbooks/` com 10 cenários de falha | M | Alto | Onboarding on-call |
| 3 | Rodar análise de churn em `api/` e `operators/`, identificar 3 hotspots | S | Médio | Churn como smell |
| 4 | Definir 2-3 métricas de outcome (não operacionais) no README | S | Alto | Pergunta-âncora 12 meses |
| 5 | Auditar camada de validação de `loopzero.yaml` por classes de exploits | M | Alto | Templates + validação |
| 6 | Avaliar consolidação em OTel Collector único (vs 4 sistemas) | L | Médio | Observability unificada |
| 7 | Spike: mover plan limit enforcement para Traefik middleware | M | Médio | Sidecar transversal |
| 8 | Definir Stability Contract interno + deprecation policy em `pkg/` | M | Médio | Mudar ao longo do tempo |
| 9 | Designar 10% de capacity por sprint para compliance | S | Alto | Compliance inevitável |
| 10 | Auditar entendimento humano do código AI-assisted (1x/trimestre) | S | Médio | Manutenção pós-IA |

**Legenda esforço:** S = ≤1 semana, M = 1-3 semanas, L = 1-2 meses.

---

## Referências

- Vídeo original: YouTube `55pTFVoclvE`
- Documento fonte: `/home/paulo/Projetos/transcricao/55pTFVoclvE_aprendizados.md`
- Princípios universais aplicáveis: `~/.claude/CLAUDE.md` (seções 9-13 sobre SOLID, KISS, YAGNI, DRY, Não Reinvente a Roda)

---

**Documento mantido para referência. Atualizar conforme ações forem executadas e novos aprendizados surgirem.**
