# API SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`
- `Critério de aprovação`
- `Evidência esperada`
- `Notas LoopZero`

Para uma PaaS, API SOTA não é só “tem rotas e responde rápido”. Ela precisa preservar simplicidade externa enquanto orquestra múltiplos planos internos com segurança.

Regra transversal de validação:

- testes unitários e de integração são necessários;
- para mutações e leituras críticas do control plane, a barra inclui evidência em ambiente parity alto ou execução real com dependências concretas.

## Gate API-G01: Product-Oriented HTTP Surface

`Objetivo`

Garantir que a API fale a linguagem do produto, não a linguagem das dependências internas.

`Critério de aprovação`

- rotas e payloads representam conceitos de deploy, project, team, preview, domain, release;
- detalhes internos não viram contrato acidental;
- respostas e erros são compreensíveis por CLI e dashboard.

`Evidência esperada`

- handlers em `routes/`;
- payloads e erros com semântica de produto;
- ausência de vazamento de mecanismos internos.

`Notas LoopZero`

- esse gate está alinhado às regras do repo sobre não expor termos internos ao usuário.

## Gate API-G02: Transport / Orchestration / Persistence Separation

`Objetivo`

Evitar handlers que façam tudo ao mesmo tempo.

`Critério de aprovação`

- routes recebem e respondem HTTP;
- services orquestram caso de uso;
- db/stores persistem e consultam estado;
- integrações externas não ficam acopladas diretamente ao handler.

`Evidência esperada`

- `routes/`, `services/`, `db/` e integrations com papéis distintos;
- wiring em `cmd/` e `server/`.

`Notas LoopZero`

- esse é um dos gates mais importantes para sustentação da pasta no longo prazo.

## Gate API-G03: Idempotent Mutations

`Objetivo`

Permitir replay seguro de mutações e proteger clientes contra duplicação acidental.

`Critério de aprovação`

- endpoints mutantes críticos exigem ou suportam `Idempotency-Key`;
- replay da mesma intenção devolve resultado consistente;
- payload diferente com mesma key gera conflito estruturado.

`Evidência esperada`

- middleware de idempotência;
- store persistente de records;
- testes de miss/hit/mismatch.

`Notas LoopZero`

- `api/internal/middleware/idempotency.go` já implementa o núcleo desse gate.

## Gate API-G04: Correct Concurrency Boundary

`Objetivo`

Serializar exatamente onde a plataforma precisa impedir corrida operacional.

`Critério de aprovação`

- mutações sobre `(tenant, project, environment)` usam `WithEnvironmentLock`;
- a checagem de concorrência ocorre dentro da seção crítica;
- lock é transaction-scoped, nunca session-scoped.

`Evidência esperada`

- `api/internal/lock/environment_lock.go`;
- chamadas de locker nos serviços;
- testes de concorrência e crash recovery.

`Notas LoopZero`

- esse gate é inegociável no LoopZero.

## Gate API-G05: Persist Intent Before External Effects

`Objetivo`

Evitar que efeitos externos aconteçam sem trilha persistida de intenção.

`Critério de aprovação`

- o sistema grava estado mínimo necessário antes de workflow, push GitOps ou sync externo;
- retry/reconcile posterior consegue retomar fluxo com base em estado persistido;
- erro após efeito externo ainda deixa rastreabilidade suficiente.

`Evidência esperada`

- stores usados antes de side effects;
- orchestrators estruturados;
- testes de retry/reconcile.

`Notas LoopZero`

- esse gate é central para builds, release activation e callbacks.

## Gate API-G06: Short Synchronous Path, Long Async Path

`Objetivo`

Manter a API responsiva sem perder controle do workflow de longa duração.

`Critério de aprovação`

- requests síncronos validam, persistem intenção e disparam fluxo;
- trabalho pesado segue para workflow, reconcile loop ou callback;
- estado final pode ser acompanhado por status, logs ou SSE.

`Evidência esperada`

- `build_orchestrator`, routes de stream, callbacks e reconcile loops;
- respostas 202/fluxos assíncronos quando cabível.

`Notas LoopZero`

- deploy é o principal exemplo desse gate.

## Gate API-G07: Structured Error Contract

`Objetivo`

Fornecer erros consumíveis por humanos e por clientes automatizados.

`Critério de aprovação`

- erros têm código estável e mensagem útil;
- conflitos, validação, auth e indisponibilidade são distinguíveis;
- handlers 404/405 e erros gerais seguem shape previsível.

`Evidência esperada`

- uso de `pkg/errors`;
- JSON error envelopes consistentes;
- testes de rotas e middleware.

`Notas LoopZero`

- RFC 9457 é referência forte de evolução aqui, mesmo que o envelope atual seja próprio.

## Gate API-G08: Observability Propagation

`Objetivo`

Permitir rastreabilidade ponta a ponta da intenção do usuário até os efeitos internos.

`Critério de aprovação`

- correlation id é criado ou propagado;
- tracing middleware envolve requests;
- spans e logs críticos carregam contexto suficiente;
- serviços e locks preservam contexto observável.

`Evidência esperada`

- `middleware.CorrelationID`;
- `middleware.OtelHTTP`;
- uso de context em stores e services.

`Notas LoopZero`

- esse gate é essencial para debug de cadeia CLI → API → workflow → runtime.

## Gate API-G09: Backpressure And Degradation Semantics

`Objetivo`

Fazer a API degradar de forma explícita quando a plataforma está sob pressão.

`Critério de aprovação`

- pool saturation, admission limits e maintenance mode geram resposta controlada;
- mutações são bloqueadas quando a plataforma não consegue processá-las com segurança;
- comportamento degradado é observável.

`Evidência esperada`

- PG circuit breaker middleware;
- admission package;
- maintenance middleware.

`Notas LoopZero`

- em uma PaaS, “aceitar tudo e falhar depois” é pior do que rejeitar cedo com semântica correta.

## Gate API-G10: Multi-Cluster And Multi-Cell Awareness

`Objetivo`

Orquestrar múltiplos clusters e cells sem confundir o contrato externo.

`Critério de aprovação`

- placement e client resolution ficam encapsulados;
- rotas e serviços não assumem single-cell implicitamente;
- falhas de placement e discovery são tratadas como parte do sistema.

`Evidência esperada`

- `placement/` e `cellmgr/`;
- setup do cell router;
- testes de routing e stale checker.

`Notas LoopZero`

- este gate sobe de importância à medida que a plataforma expande cells.

## Gate API-G11: Integration Boundaries Via Interfaces

`Objetivo`

Impedir que dependências externas dominem o design interno.

`Critério de aprovação`

- workflows, GitOps, sync, source upload e static routing entram por interfaces pequenas;
- implementação concreta é resolvida no wiring;
- testes podem substituir dependências externas por doubles.

`Evidência esperada`

- interfaces em `services/`;
- setup em `cmd/`;
- testes unitários com fakes.

`Notas LoopZero`

- esse gate é particularmente importante para Argo, GitHub, Cloudflare e GitOps.

## Gate API-G12: Domain State Discipline

`Objetivo`

Evitar que lifecycle e status se dispersem em strings ad hoc.

`Critério de aprovação`

- lifecycle relevante usa `pkg/statemachine` ou helper equivalente;
- stores e services respeitam o contrato de estado;
- transições inválidas não são absorvidas silenciosamente.

`Evidência esperada`

- uso de `pkg/statemachine`;
- testes de transition e regressão.

`Notas LoopZero`

- isso conecta a API ao restante da plataforma e reduz divergência com operators.

## Gate API-G13: Test Matrix Covers Systemic Risks

`Objetivo`

Provar que os principais riscos arquiteturais estão cobertos.

`Critério de aprovação`

- há testes de concorrência, idempotência, crash recovery e tracing;
- routes, middleware e stores têm cobertura direcionada;
- fluxos core têm integração suficiente para detectar regressões estruturais.

`Evidência esperada`

- `api/tests/integration/*`;
- `api/tests/unit/*`;
- `api/apitest/server.go`.

`Notas LoopZero`

- os testes existentes já mostram preocupação forte com esses riscos. O gate agora é preservá-la.

## Gate API-G14: Migration Discipline

`Objetivo`

Tratar schema e evolução de banco como parte do contrato da API.

`Critério de aprovação`

- migrações são forward-only;
- stores acompanham mudanças de schema explicitamente;
- a API não depende de mudanças manuais ocultas no banco.

`Evidência esperada`

- `api/internal/db/migrations/*.up.sql`;
- testes de migration.

`Notas LoopZero`

- para uma PaaS, database drift e migration improvisada são risco direto de indisponibilidade.

## Gate API-G15: Read APIs Reflect Aggregated Reality

`Objetivo`

Garantir que endpoints de leitura mostrem estado útil, não apenas ecos fragmentados de subsistemas.

`Critério de aprovação`

- status, logs, health e listagens agregam sinais necessários para o usuário agir;
- endpoints de leitura não exigem que o cliente reconstrua mentalmente o sistema inteiro;
- leitura multi-cell ou cross-system é encapsulada.

`Evidência esperada`

- `routes/status.go`, `routes/logs*.go`, `health/*`, `cross_cell_aggregator.go`;
- testes de read paths.

`Notas LoopZero`

- esse gate é importante porque a CLI depende muito da qualidade das leituras.

## Ordem De Prioridade

1. `API-G03`, `API-G04`, `API-G05`, `API-G06`, `API-G07`
2. `API-G02`, `API-G08`, `API-G09`, `API-G11`, `API-G12`
3. `API-G01`, `API-G10`, `API-G13`, `API-G14`, `API-G15`

Justificativa:

- primeiro, segurança de mutação e contrato básico;
- depois, observabilidade, degradação e boundaries internos;
- por fim, superfície completa e evolução sustentada.
