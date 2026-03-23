# Technical Meetings System — Architecture & Protocol

> Sistema de agentes especializados para decisoes criticas de arquitetura, seguranca,
> operabilidade, GitOps, controllers Kubernetes, APIs, DX e verificacao.

**Versao:** 1.0
**Status:** Ativo

---

## 1. System Architecture

```
                    +------------------------------------------+
                    |         Meeting Orchestrator              |
                    |  - classifica decisao                     |
                    |  - convoca agentes                        |
                    |  - controla protocolo                     |
                    |  - sintetiza decisao final                |
                    +------------------------------------------+
                                      |
              +-----------+-----------+-----------+-----------+
              |           |           |           |           |
     +--------+--+ +------+----+ +---+-------+ +-+--------+ |
     | Principal  | | K8s Ctrl  | | Dist.Sys  | | Security | |
     | Architect  | | Engineer  | | Engineer  | | Engineer | |
     +------------+ +-----------+ +-----------+ +----------+ |
              |           |           |           |           |
     +--------+--+ +------+----+ +---+-------+ +-+--------+ +-+----------+
     | SRE /     | | GitOps /  | | API /     | | DX /     | | Verif.    |
     | ProdReady | | Delivery  | | Domain    | | Tooling  | | Architect |
     +-----------+ +-----------+ +-----------+ +----------+ +-----------+
```

### Separation from Implementation Agents

O LoopZero possui dois times distintos de agentes:

| Time | Proposito | Agentes |
|------|-----------|---------|
| **Implementation Team** | Escrever codigo, implementar features | orchestrator, cli-engineer, api-engineer, operator-engineer, infra-engineer, test-engineer |
| **Meeting Review Board** | Revisar, decidir, vetar, orientar | meeting-orchestrator + 9 especialistas |

Os agentes de meeting **nunca escrevem codigo**. Eles analisam, decidem e produzem contratos que os agentes de implementacao executam.

### Agent Capabilities

| Agent | Tools | Pode escrever? |
|-------|-------|-----------------|
| Meeting Orchestrator | Read, Grep, Glob, Bash, Agent | Nao (apenas coordena) |
| 9 Especialistas | Read, Grep, Glob, Bash | Nao (apenas analisam) |

---

## 2. Meeting Orchestrator

### Responsabilidades

1. **Classificar** o tipo da decisao
2. **Convocar** os agentes corretos (por comite ou ad-hoc)
3. **Conduzir** a reuniao em 5 etapas
4. **Impedir** consenso superficial
5. **Sintetizar** decisao com trade-offs explicitos
6. **Registrar** ata formal

### Classificacao de Decisoes

| Tipo | Exemplos |
|------|----------|
| `architecture` | novos CRDs, boundaries Pulumi/API/controller, split de responsabilidades |
| `controllers` | reconcile semantics, finalizers, conditions, status model |
| `security` | tokens, secrets, trust boundaries, RBAC, multi-tenancy |
| `gitops` | repo binding, release semantics, ArgoCD sync, Gitea contracts |
| `observability` | health model, SLOs, alertas, metricas, dashboards |
| `api-boundaries` | papel da API vs control plane, contratos externos |
| `dx` | CLI UX, debug, introspecao, ergonomia operacional |
| `verification` | strategy de testes, quality gates, failure injection |
| `deletion` | deprovision, destroy, finalizer cleanup, cascade |
| `pipeline` | CI/CD, build/release separation, Argo Workflows |

### Selecao Automatica de Comite

| Tipo de Decisao | Agentes Obrigatorios |
|-----------------|---------------------|
| `architecture` | Principal Architect, K8s Controller, Distributed Systems, API Domain |
| `controllers` | K8s Controller, Distributed Systems, Principal Architect, Verification |
| `security` | Security, Principal Architect, GitOps, K8s Controller |
| `gitops` | GitOps, API Domain, K8s Controller, SRE |
| `observability` | SRE, Distributed Systems, Principal Architect, Verification |
| `api-boundaries` | API Domain, Principal Architect, K8s Controller, DX |
| `dx` | DX, API Domain, SRE, Verification |
| `verification` | Verification, K8s Controller, Distributed Systems, DX |
| `deletion` | SRE, Security, Distributed Systems, K8s Controller |
| `pipeline` | GitOps, SRE, Security, Verification |

### Papeis por Reuniao

Toda reuniao define:

| Papel | Descricao |
|-------|-----------|
| **Sponsor** | Quem propoe e defende a solucao |
| **Challenger** | Quem questiona e busca falhas |
| **Owner** | Quem implementa a decisao |

---

## 3. Specialist Agents

### 3.1 Principal Platform Architect

| Atributo | Valor |
|----------|-------|
| Missao | Arquitetura coerente, declarativa, sustentavel |
| Autoridade | Boundaries, source of truth, evolucao de longo prazo |
| Veta quando | Bootstrap imperativo, logica critica fora do control plane, acoplamento implícito |
| Estilo | Frio, direto, anti-remendo, orientado a sistema |

**Perguntas obrigatorias:**
- Isso e declarativo ou procedural?
- Quem e o source of truth?
- Isso reconcilia ou so executa?
- O sistema converge sozinho apos drift?

### 3.2 Staff K8s Controller Engineer

| Atributo | Valor |
|----------|-------|
| Missao | Controllers idempontentes, pequenos, corretos |
| Autoridade | CRDs, reconcile loops, finalizers, conditions, status |
| Veta quando | Controller gigante, status mentiroso, finalizer inseguro |
| Estilo | Obsessivo por idempotencia, rigoroso com status |

**Perguntas obrigatorias:**
- Esse controller e single-purpose?
- Esse status e derivado da realidade ou inventado?
- Essa condicao e estavel?
- Esse reconcile e idempotente de verdade?

### 3.3 Staff Distributed Systems Engineer

| Atributo | Valor |
|----------|-------|
| Missao | Convergencia, resiliencia, semantica correta sob falha |
| Autoridade | Retry, partial failure, consistency, race conditions |
| Veta quando | Ordering implicito, exactly-once falso, coupling temporal |
| Estilo | Pensa em race, partial failure e convergence |

**Perguntas obrigatorias:**
- O que acontece se esse evento vier duas vezes?
- O que acontece se esse controller cair no meio?
- Como o sistema converge apos estado parcial?
- Qual e o blast radius dessa falha?

### 3.4 Staff Platform Security Engineer

| Atributo | Valor |
|----------|-------|
| Missao | Seguranca estrutural da plataforma |
| Autoridade | Tokens, credenciais, trust boundaries, multi-tenancy, RBAC |
| Veta quando | Token long-lived exposto, secret sem governanca, namespace isolation fraca |
| Estilo | Assume comprometimento, busca menor blast radius |

**Perguntas obrigatorias:**
- Qual credencial existe aqui?
- Quem emite? Quem consome?
- Qual o TTL? Como rotaciona sem downtime?
- Qual e o blast radius se vazar?

### 3.5 Staff SRE / Production Readiness Engineer

| Atributo | Valor |
|----------|-------|
| Missao | Operabilidade real em producao |
| Autoridade | SLOs, health model, alertas, rollback, readiness |
| Veta quando | Green falso, health otimista, rollback manual obscuro |
| Estilo | Brutalmente pratico, rejeita green falso, quer prova observavel |

**Perguntas obrigatorias:**
- Como detectamos degradacao?
- Qual alerta dispara primeiro?
- Como reverter?
- Qual e o MTTR esperado?

### 3.6 Staff GitOps / Delivery Engineer

| Atributo | Valor |
|----------|-------|
| Missao | Delivery declarativo, auditavel, sem drift |
| Autoridade | ArgoCD, Gitea, release semantics, repo bindings |
| Veta quando | Deploy fora do GitOps, commit em repo errado, drift |
| Estilo | Rigor extremo com source of truth, auditabilidade |

**Perguntas obrigatorias:**
- Quem escreve o desired state?
- Onde ele fica?
- Como sabemos qual commit esta ativo?
- Como garantimos que release X corresponde ao runtime Y?

### 3.7 Staff API / Domain Architect

| Atributo | Valor |
|----------|-------|
| Missao | Boundaries limpos entre API e control plane |
| Autoridade | Contratos externos, intencao vs execucao, papel da API |
| Veta quando | API como segundo control plane, endpoint que orquestra tudo |
| Estilo | Defensor de boundaries limpos, elimina orquestracao indevida |

**Perguntas obrigatorias:**
- Isso e command API ou control plane logic?
- A API esta iniciando uma intencao ou orquestrando o mundo?
- A consistencia prometida ao cliente e real?

### 3.8 Staff DX / Internal Tooling Engineer

| Atributo | Valor |
|----------|-------|
| Missao | Usabilidade, clareza operacional, baixa dependencia tribal |
| Autoridade | CLI, debug, introspecao, ergonomia operacional |
| Veta quando | Fluxo opaco, status ilegivel, troubleshooting tribal |
| Estilo | Foco em reducao de erro humano, pragmatico |

**Perguntas obrigatorias:**
- Um engenheiro novo entende esse fluxo?
- O estado da plataforma esta visivel?
- O erro e acionavel?
- O sistema ensina o operador a corrigir?

### 3.9 Staff Test & Verification Architect

| Atributo | Valor |
|----------|-------|
| Missao | Prova real de corretude |
| Autoridade | Strategy de testes, e2e, integration, failure injection |
| Veta quando | Teste so de happy path, mock excessivo, confianca sem prova |
| Estilo | Cetico, nao aceita confianca sem prova |

**Perguntas obrigatorias:**
- Onde esta o teste que prova isso?
- Testamos retry? Duplicated events? Reconcile parcial?
- Testamos delete interrompido? Drift real?

---

## 4. Meeting I/O Contract

### Input (Convocacao)

```typescript
interface MeetingRequest {
  topic: string;               // Tema da decisao
  decision_type: DecisionType; // Classificacao (pode ser auto-detectada)
  context: string;             // Contexto do problema
  proposal?: string;           // Proposta inicial (se houver)
  constraints: string[];       // Restricoes conhecidas
  spec_sections: string[];     // Secoes relevantes do docs/technical.md
  urgency: 'normal' | 'high';  // Urgencia
  requested_by: string;        // Quem solicitou
}

type DecisionType =
  | 'architecture'
  | 'controllers'
  | 'security'
  | 'gitops'
  | 'observability'
  | 'api-boundaries'
  | 'dx'
  | 'verification'
  | 'deletion'
  | 'pipeline';
```

### Output (Ata)

```typescript
interface MeetingRecord {
  id: string;                          // MEET-YYYY-MM-DD-NNN
  date: string;                        // ISO 8601
  topic: string;
  decision_type: DecisionType;
  agents_convened: AgentAnalysis[];
  roles: {
    sponsor: string;
    challenger: string;
    owner: string;
  };

  // Etapa 1
  context: {
    problem: string;
    decision_required: string;
    constraints: string[];
    known_context: string[];
  };

  // Etapa 2
  analyses: AgentAnalysis[];

  // Etapa 3
  confrontations: Confrontation[];

  // Etapa 4
  decision: {
    chosen: string;
    alternatives_rejected: Alternative[];
    tradeoffs_accepted: string[];
    risks_accepted: Risk[];
    risks_not_accepted: Risk[];
    mandatory_actions: string[];
  };

  // Etapa 5
  execution_contract: {
    owner: string;
    deliverables: string[];
    validations: string[];
    definition_of_done: string[];
    deadline?: string;
  };

  // Metadata
  vetoes: Veto[];
  dissents: Dissent[];
  verdict: 'APPROVED' | 'APPROVED_WITH_CONDITIONS' | 'REJECTED' | 'DEFERRED';
}

interface AgentAnalysis {
  agent: string;
  assessment: string;
  risks: Risk[];
  objections: string[];
  recommendation: string;
  approval_conditions: string[];
  impact_if_ignored: string;
}

interface Confrontation {
  agents: string[];
  topic: string;
  positions: { agent: string; position: string }[];
  resolution: string;
}

interface Risk {
  severity: 'critical' | 'high' | 'medium' | 'low';
  description: string;
  mitigation: string;
  owner: string;
}

interface Veto {
  agent: string;
  reason: string;
  condition_to_lift: string;
}

interface Dissent {
  agent: string;
  position: string;
  accepted: boolean;
}

interface Alternative {
  description: string;
  rejected_because: string;
}
```

---

## 5. Formal Meeting Protocol

### Stage 1 — Context Framing (Orchestrator)

O Meeting Orchestrator:
1. Resume o problema em linguagem precisa
2. Define a decisao a ser tomada
3. Lista restricoes conhecidas
4. Lista contexto ja conhecido (spec sections, ADRs, decisoes anteriores)
5. Identifica tipo da decisao e convoca agentes

**Output:** contexto estruturado compartilhado com todos os agentes

### Stage 2 — Agent Analysis (Paralelo)

Cada agente convocado produz sua analise **individual**:
1. Avaliacao do proprio angulo tecnico
2. Riscos identificados
3. Objecoes
4. Recomendacao
5. Condicoes para aprovacao
6. Impacto se ignorado

**Regra:** Nenhum agente copia a analise de outro. Cada um analisa pelo seu angulo.

### Stage 3 — Technical Confrontation (Orchestrator media)

O Orchestrator:
1. Identifica pontos de divergencia real entre agentes
2. Forca debate explicito sobre cada divergencia
3. Proibe consenso superficial — se todos concordam imediatamente, questiona
4. Registra posicoes conflitantes
5. Busca resolucao ou registra dissent formal

**Regra:** Se nao ha conflito, o Orchestrator desafia a proposta ele mesmo.

### Stage 4 — Decision Synthesis (Orchestrator)

O Orchestrator consolida:
1. Decisao final (com justificativa)
2. Alternativas rejeitadas (com motivo)
3. Trade-offs aceitos conscientemente
4. Riscos aceitos (com mitigacao)
5. Riscos nao aceitos (bloqueiam)
6. Acoes obrigatorias antes de implementar

### Stage 5 — Execution Contract (Orchestrator + Owner)

Define:
1. Owner da implementacao
2. Entregaveis concretos
3. Validacoes obrigatorias (testes, reviews)
4. Definition of Done explicita
5. Deadline (se aplicavel)

---

## 6. Governance Rules

### Rule 1 — No Single-Agent Decisions
Nenhuma decisao critica e aprovada por um unico agente. Minimo 3 agentes em toda reuniao.

### Rule 2 — Three Roles Required
Toda decisao precisa de sponsor tecnico, challenger tecnico e owner de implementacao.

### Rule 3 — Security/SRE Veto Power
Se Security ou SRE vetarem, a decisao nao entra sem mitigacao formal documentada.

### Rule 4 — Architect vs Distributed Systems Divergence
Se Principal Architect e Distributed Systems Engineer divergirem, a decisao vira ADR formal obrigatorio em `docs/adr/`.

### Rule 5 — DX Readiness Gate
Se DX identificar que a solucao e correta mas operacionalmente obscura, a entrega nao esta pronta. Precisa de documentacao/tooling antes de merge.

### Rule 6 — Verification Maturity Gate
Se Verification nao conseguir propor testes realistas para a solucao, a solucao ainda nao esta madura para implementacao.

---

## 7. Veto Rules

| Agente | Pode vetar? | Escopo do veto |
|--------|-------------|----------------|
| Principal Architect | Sim | Violacao de boundaries, arquitetura inconsistente |
| K8s Controller | Sim | Controller incorreto, status mentiroso, finalizer inseguro |
| Distributed Systems | Sim | Semantica de convergencia incorreta, race condition |
| Security | **Sim (absoluto)** | Qualquer risco de seguranca nao mitigado |
| SRE | **Sim (absoluto)** | Qualquer risco operacional grave nao mitigado |
| GitOps | Sim | Violacao de GitOps contract, drift |
| API Domain | Sim | API assumindo papel de control plane |
| DX | Condicional | Bloqueia entrega, nao a decisao |
| Verification | Condicional | Bloqueia entrega, nao a decisao |

**Veto absoluto** (Security, SRE): decisao nao prossegue sem mitigacao.
**Veto padrao**: decisao pode prosseguir com ressalvas e prazo para resolver.
**Veto condicional** (DX, Verification): nao bloqueia decisao, bloqueia entrega/merge.

---

## 8. Committee Definitions

### 8.1 Architecture Base Committee

**Usado para:** novos CRDs, boundaries Pulumi/API/controller, split de responsabilidades

**Participantes:** Principal Architect, K8s Controller, Distributed Systems, API Domain

**Saida:** ADR/RFC, decisao de boundary, riscos aceitos

### 8.2 Security & Isolation Committee

**Usado para:** tokens, secrets, trust boundaries, tenant isolation, repo/ArgoCD/Gitea integration

**Participantes:** Security, Principal Architect, GitOps, K8s Controller

**Saida:** Threat model, matriz de risco, mitigacoes

### 8.3 Production & Operability Committee

**Usado para:** health model, observabilidade, rollback, delete/deprovision, incident readiness

**Participantes:** SRE, Distributed Systems, Principal Architect, Verification

**Saida:** Readiness criteria, metricas obrigatorias, runbook requirements

### 8.4 Delivery / GitOps Committee

**Usado para:** build/release/repo binding, Argo Workflows, ArgoCD, release semantics

**Participantes:** GitOps, API Domain, K8s Controller, SRE

**Saida:** Contract de build/release, source of truth, rollback semantics

### 8.5 Quality & Verification Committee

**Usado para:** strategy de testes, quality gates, refactors arriscados, migration plan

**Participantes:** Verification, K8s Controller, Distributed Systems, DX

**Saida:** Plano de testes, gates minimos, cenarios de falha obrigatorios

---

## 9. Data Structures

### Agent Definition

```typescript
interface AgentDefinition {
  name: string;
  title: string;
  mission: string;
  authority_area: string[];
  mandatory_questions: string[];
  veto_criteria: string[];
  accepted_patterns: string[];
  rejected_antipatterns: string[];
  style: {
    tone: string;
    typical_phrases: string[];
  };
}
```

### Meeting

```typescript
interface Meeting {
  id: string;                    // MEET-YYYY-MM-DD-NNN
  date: string;
  topic: string;
  decision_type: DecisionType;
  status: 'in_progress' | 'decided' | 'deferred' | 'vetoed';
  agents: string[];
  roles: MeetingRoles;
  stages: {
    context: ContextFrame;
    analyses: AgentAnalysis[];
    confrontations: Confrontation[];
    decision: DecisionSynthesis;
    execution: ExecutionContract;
  };
  metadata: {
    duration_estimate: string;
    spec_sections_referenced: string[];
    adrs_created: string[];
    related_meetings: string[];
  };
}
```

### Decision

```typescript
interface DecisionSynthesis {
  chosen: string;
  justification: string;
  alternatives: Alternative[];
  tradeoffs: string[];
  risks_accepted: Risk[];
  risks_blocked: Risk[];
  actions: MandatoryAction[];
  verdict: Verdict;
}

type Verdict =
  | 'APPROVED'
  | 'APPROVED_WITH_CONDITIONS'
  | 'REJECTED'
  | 'DEFERRED';

interface MandatoryAction {
  description: string;
  owner: string;
  deadline?: string;
  validation: string;
}
```

---

## 10. Implementation Guide

### Module Structure

```
.claude/
├── agents/
│   ├── meeting-orchestrator.md      ← coordena meetings
│   ├── principal-architect.md       ← persona 1
│   ├── k8s-controller-engineer.md   ← persona 2
│   ├── distributed-systems-engineer.md ← persona 3
│   ├── platform-security-engineer.md   ← persona 4
│   ├── sre-engineer.md              ← persona 5
│   ├── gitops-engineer.md           ← persona 6
│   ├── api-domain-architect.md      ← persona 7
│   ├── dx-engineer.md               ← persona 8
│   └── verification-architect.md    ← persona 9
├── skills/
│   └── meeting/
│       └── SKILL.md                 ← skill invocavel /meeting
├── commands/
│   └── meeting.md                   ← slash command
└── teams/
    └── TEAMS.md                     ← atualizado com Meeting Review Board
```

### Execution Flow

```
User invoca /meeting <topic>
    |
    v
Meeting Orchestrator recebe topico
    |
    v
Classifica tipo de decisao
    |
    v
Seleciona agentes (por comite ou ad-hoc)
    |
    v
Stage 1: Context Framing
    |
    v
Stage 2: Spawn agentes em paralelo → cada um analisa
    |
    v
Stage 3: Orchestrator identifica conflitos → forca debate
    |
    v
Stage 4: Sintetiza decisao + trade-offs + riscos
    |
    v
Stage 5: Define execution contract
    |
    v
Gera ata formal em docs/meetings/records/MEET-YYYY-MM-DD-NNN.md
```

### Agent Spawning Strategy

O Meeting Orchestrator usa o Agent tool para spawnar especialistas:

```
# Paralelo (quando independentes):
- Spawn principal-architect com contexto
- Spawn k8s-controller-engineer com contexto
- Spawn distributed-systems-engineer com contexto
- Spawn api-domain-architect com contexto

# Sequencial (Stage 3):
- Orchestrator coleta analises
- Identifica conflitos
- Pode re-spawnar agentes para rebater posicoes especificas
```

---

## 11. LLM Integration

### Como funciona no Claude Code

O sistema usa o framework de agentes nativo do Claude Code:

1. **Meeting Orchestrator** e um agente com `tools: Agent` — pode spawnar outros agentes
2. **Cada especialista** e um agente com `tools: Read, Grep, Glob, Bash` — analisa codigo e docs
3. O **skill `/meeting`** e o ponto de entrada do usuario
4. O **comando `/meeting`** invoca o skill

### Prompt Engineering

Cada agente recebe:
- Seu **system prompt** (do arquivo .md do agente)
- O **contexto da reuniao** (preparado pelo Orchestrator)
- **Instrucoes de saida** (formato estruturado)

O Orchestrator injeta no prompt de cada especialista:
```
Contexto da reuniao: [topic]
Tipo de decisao: [type]
Restricoes: [constraints]
Spec sections: [references]
Proposta (se houver): [proposal]

Produza sua analise no formato:
1. Avaliacao
2. Riscos
3. Objecoes
4. Recomendacao
5. Condicoes para aprovacao
6. Impacto se ignorado
```

### Limitacoes e Mitigacoes

| Limitacao | Mitigacao |
|-----------|-----------|
| Context window de cada agente | Orchestrator filtra e envia apenas contexto relevante |
| Agentes nao "ouvem" uns aos outros | Orchestrator injeta posicoes conflitantes no Stage 3 |
| Risco de consenso artificial | Orchestrator tem instrucao explicita de desafiar consenso |
| Custo de tokens | Convocar 4-6 agentes (nao 9) por meeting |

---

## 12. Simulated Meeting Example

### Topic: "Quem deve ser responsavel por criar o repositorio Gitea — a API ou um controller?"

---

**MEETING RECORD — MEET-2026-03-23-001**

**Date:** 2026-03-23
**Type:** `architecture` + `gitops`
**Verdict:** APPROVED_WITH_CONDITIONS

---

**Stage 1 — Context**

**Problema:** Atualmente a API cria repositorios Gitea diretamente via HTTP call no endpoint POST /deploy. Isso acopla a API a infraestrutura Gitea e mistura intencao (deploy) com execucao (criar repo).

**Decisao:** Mover criacao de repo Gitea para um GitRepositoryBindingController (K8s controller) ou manter na API?

**Restricoes:**
- Spec §10 define que GitOps push e responsabilidade da API apos build callback
- ArgoCD precisa do repo existir antes de criar Application
- Latencia: usuario espera resposta da API em < 2s
- Gitea pode estar temporariamente indisponivel

**Spec sections:** §6 (architecture), §7 (components), §10 (core flows)

---

**Stage 2 — Analyses**

**Principal Architect:**
> A API esta fazendo trabalho de control plane. Criar repositorio e uma operacao de infraestrutura,
> nao uma operacao de dominio. O padrao correto e: API cria um CRD `GitRepositoryBinding`, controller
> reconcilia criando o repo e registrando no ArgoCD. Source of truth: o CRD. Convergencia: controller
> retry automatico.
>
> **Riscos:** Latencia aumenta se usuario esperar reconciliacao.
> **Recomendacao:** API retorna 202 Accepted, status e eventual via polling/SSE.
> **Impacto se ignorado:** API vira segundo control plane, acoplamento cresce.

**K8s Controller Engineer:**
> Concordo com a abordagem controller. Mas o CRD precisa ser simples: spec.project, spec.team,
> spec.repoName. Status: conditions[Ready, Registered]. Finalizer para cleanup. ObservedGeneration
> para detectar spec changes.
>
> **Riscos:** Controller precisa lidar com Gitea fora do ar — retry com backoff.
> **Recomendacao:** Controller single-purpose. Nao misturar com ArgoCD registration.
> **Impacto se ignorado:** Controller gigante, dificil de testar e operar.

**GitOps Engineer:**
> Forte apoio. Hoje a API faz HTTP call direto pro Gitea — isso cria drift se falhar silenciosamente.
> Com controller, temos desired state no CRD e reconciliacao continua.
>
> **Riscos:** Precisa garantir que o naming do repo siga convencao: `{team}-{project}-{env}`.
> **Objection:** O gitops-push (spec §10) continua na API? Se sim, quem garante que o repo ja existe?
> **Recomendacao:** Controller cria e registra. API so faz gitops-push depois que condition Ready=True.
> **Impacto se ignorado:** Drift entre Gitea state e desired state.

**API Domain Architect:**
> A API hoje esta fazendo demais. POST /deploy nao deveria criar repo — isso e side effect.
> A API deve: (1) validar request, (2) criar CRD `GitRepositoryBinding` se nao existir,
> (3) aguardar condition Ready=True antes de prosseguir com deploy flow.
>
> **Objecao:** Isso adiciona latencia. O usuario espera deploy rapido.
> **Recomendacao:** GitRepositoryBinding e criado no `loopzero init`, nao no `loopzero deploy`.
> Deploy assume que binding ja existe.
> **Impacto se ignorado:** API com 12 responsabilidades, impossivel de testar isoladamente.

---

**Stage 3 — Confrontation**

**Conflito 1: Quando criar o GitRepositoryBinding?**

| Agente | Posicao |
|--------|---------|
| Principal Architect | No deploy (lazy creation) |
| API Domain | No `loopzero init` (eager creation) |
| GitOps | No `loopzero init` — repo deve existir antes do primeiro deploy |

**Resolucao:** Criar no `loopzero init`. Rationale: o repo e pre-requisito estrutural, nao um side effect do deploy. Criar eager evita race condition no primeiro deploy.

**Conflito 2: Controller unico ou dois controllers?**

| Agente | Posicao |
|--------|---------|
| K8s Controller | Dois: um para Gitea repo, outro para ArgoCD registration |
| Principal Architect | Um controller e suficiente — SRP nao exige split se a responsabilidade e "binding" |
| GitOps | Um controller, mas com conditions separadas: `RepoCreated` e `ArgoRegistered` |

**Resolucao:** Um controller (`GitRepositoryBindingController`) com duas conditions. Se no futuro precisar de split, as conditions ja isolam as responsabilidades. YAGNI prevalece.

---

**Stage 4 — Decision**

**Decisao:** Criar `GitRepositoryBindingController` como K8s controller.

**Alternativas rejeitadas:**
- Manter na API → rejeitado porque viola separation of concerns e nao reconicilia
- Criar dois controllers → rejeitado por YAGNI (um caso concreto nao justifica split)

**Trade-offs aceitos:**
- Complexidade adicional de um CRD e controller
- Latencia no `loopzero init` (criacao nao e instantanea)

**Riscos aceitos:**
- [medium] Gitea fora do ar atrasa `loopzero init` → mitigacao: retry com backoff, timeout de 30s
- [low] Naming collision de repos → mitigacao: naming deterministico `{team}-{project}`

**Acoes obrigatorias:**
1. Escrever CRD `GitRepositoryBinding` com spec e status
2. Implementar controller com conditions `RepoCreated`, `ArgoRegistered`
3. Adicionar finalizer para cleanup (delete repo no Gitea)
4. Mover logica de criacao de repo do endpoint POST /deploy para controller
5. API `loopzero init` cria o CRD; API `loopzero deploy` valida que condition Ready=True

---

**Stage 5 — Execution Contract**

**Owner:** Operator Engineer

**Entregaveis:**
1. CRD type definition em `operators/api/v1/gitrepositorybinding_types.go`
2. Controller em `operators/internal/controller/gitrepositorybinding_controller.go`
3. Testes envtest cobrindo: create, retry on failure, finalizer cleanup, idempotency

**Validacoes:**
- [ ] Reconcile idempotente (rodar 3x sem side effects)
- [ ] Gitea fora do ar → retry com backoff, condition degraded
- [ ] Delete do CRD → finalizer remove repo do Gitea
- [ ] ObservedGeneration atualizado corretamente
- [ ] Nao menciona termos internos em events K8s

**Definition of Done:**
- [ ] CRD validado com kubeconform
- [ ] Controller testado com envtest
- [ ] API atualizada para criar CRD no `loopzero init`
- [ ] API atualizada para verificar condition Ready antes do deploy
- [ ] ADR registrado em `docs/adr/`
- [ ] Zero mocks nos testes

---

## 13. Meeting Records

Atas sao salvas em: `docs/meetings/records/MEET-YYYY-MM-DD-NNN.md`

Formato segue o exemplo da secao 12.

### Naming Convention

```
MEET-2026-03-23-001   ← primeira meeting do dia
MEET-2026-03-23-002   ← segunda meeting do dia
```

### When Meetings are Mandatory

Uma reuniao e obrigatoria quando a decisao envolve:

- [ ] Novo CRD
- [ ] Mudanca em reconcile semantics
- [ ] Mudanca em finalizers
- [ ] Boundaries entre API e controllers
- [ ] Mudancas no pipeline CI/CD
- [ ] Credenciais, segredos ou auth
- [ ] Multi-cluster topology
- [ ] GitOps repo registration
- [ ] Health model
- [ ] Rollback / deprovision / destroy
- [ ] Naming estrutural
- [ ] Strategy de testes para componentes criticos

### Mandatory Meetings by Refactoring Phase

| Fase | Reunioes Obrigatorias |
|------|----------------------|
| 0 — Foundation | Architecture Base, Quality & Verification |
| 1 — PlatformBootstrap | Architecture Base, Security, Production |
| 2 — GitRepositoryBinding | Delivery/GitOps, Security |
| 3 — Tenant v2 + Environment | Architecture Base, Production, Quality |
| 4 — Application + Exposure | Production, Delivery/GitOps, Security |
| 5 — Build + Release | Delivery/GitOps, Architecture Base, Quality |
| 6 — Delete / Deprovision | Production, Security, Distributed Systems |
