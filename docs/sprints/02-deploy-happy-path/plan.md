# 📦 PROGRAMA DE HARDENING — EXECUÇÃO

## Estrutura

Cada Step tem:

* Owner sugerido
* Duração estimada
* Dependências
* Entregáveis objetivos
* Evidência obrigatória
* Gate de aprovação (meu critério)

---

# 🧱 STEP 0 — Consolidação e Gate Definition

## Owner

Tech Lead + Platform Lead

## Duração

1–2 dias

## Dependências

Nenhuma

---

## Execução

Criar documento único:

```
/ops/hardening-plan.md
```

### Tabela obrigatória

| ID | Tipo | Descrição | Severidade | Owner | Evidência | Status |
| -- | ---- | --------- | ---------- | ----- | --------- | ------ |

Preencher com:

* G1, G4, G6, G8, G9
* M4, M6
* T1, T2, T3
* O1–O5
* build cache
* PostgreSQL HA
* rate limiting
* observabilidade

---

## Classificação obrigatória

* **P0** → bloqueia early adopters
* **P1** → sustenta carga real
* **P2** → escala e maturidade

---

## Evidência obrigatória

* Cada item tem:

  * como validar
  * teste associado
  * métrica associada

---

## Gate de aprovação (meu)

### Diagnóstico

Se isso virar só uma lista → rejeitado.

### Impacto

Sem isso, execução vira caos paralelo.

### Proposta

Aprovo apenas se:

* 100% dos gaps estão mapeados
* nenhum item está “sem owner”
* evidência está definida

---

# 🔐 STEP 1 — Segurança (Secrets + Credenciais)

## Owner

Backend + Platform

## Duração

3–4 dias

## Dependências

Step 0

---

## Execução

### 1. Secrets via K8s

Remover:

```yaml
env:
  - name: DATABASE_URL
    value: ...
```

Substituir por:

```yaml
envFrom:
  - secretRef:
      name: app-secrets
```

---

### 2. Secret hash rollout

Adicionar annotation:

```yaml
metadata:
  annotations:
    loopzero.io/secret-hash: <sha256>
```

---

### 3. Ephemeral credentials

Implementar:

* TTL (ex: 15min–1h)
* refresh automático
* invalidação

---

### 4. Escopo mínimo

Revisar:

* Gitea token
* ArgoCD access
* kubeconfig de build

---

## Testes obrigatórios

* deploy com secret
* alteração de secret → rollout
* credencial expirada
* credencial inválida

---

## Evidência

* manifest gerado (sem secret inline)
* log de refresh de credencial
* teste passando em CI

---

## Gate de aprovação

### Diagnóstico

Se ainda existir secret inline → falhou.

### Impacto

Segurança fraca + drift.

### Proposta

Aprovo apenas se:

* zero secret inline
* rollout automático funcionando
* credenciais expiram de fato

---

# 🔄 STEP 2 — Recuperação e Consistência

## Owner

Backend (crítico)

## Duração

4–5 dias

## Dependências

Step 1

---

## Execução

### 1. Startup Reconciler

No boot:

* buscar builds:

  * RUNNING
  * QUEUED

Regras:

| Estado externo | Ação      |
| -------------- | --------- |
| concluído      | finalizar |
| inexistente    | FAILED    |
| ativo          | reattach  |

---

### 2. Circuit breaker no Postgres

Tabela:

```
circuit_breakers(
  key,
  state,
  updated_at
)
```

---

### 3. Multi-réplica consistente

Todas instâncias:

* leem estado do DB
* escrevem via lock

---

### 4. Callback E2E

Garantir fluxo:

```
Argo → callback → API → build complete → release
```

---

## Testes obrigatórios

* crash da API durante deploy
* restart com build órfão
* duas réplicas concorrendo
* Argo indisponível

---

## Evidência

* log de reconciliação no boot
* build recuperado automaticamente
* breaker abrindo/fechando

---

## Gate de aprovação

### Diagnóstico

Sem reconciler = sistema não é resiliente.

### Impacto

Build preso = suporte manual.

### Proposta

Aprovo apenas se:

* restart não deixa lixo
* multi-réplica consistente
* breaker compartilhado

---

# 🚦 STEP 3 — Controle de Capacidade (Pipeline)

## Owner

Platform / Infra

## Duração

5–7 dias

## Dependências

Step 2

---

## Execução

### 1. Concurrency control

Implementar:

* limite global
* limite por tenant

---

### 2. Fila explícita

Não pode ser implícita no Argo.

Opções:

* DB queue
* semaphore com advisory lock

---

### 3. Cache

* layer cache
* dependency cache

---

### 4. Gitea optimization

* shallow clone
* evitar clone por step

---

## Métricas obrigatórias

* queue depth
* wait time
* build duration
* cache hit rate

---

## Teste obrigatório

Simular:

* 20 builds simultâneos
* burst

---

## Evidência

* gráfico de fila
* tempo médio antes/depois
* cache funcionando

---

## Gate de aprovação

### Diagnóstico

Pipeline é gargalo nº1.

### Impacto

Sem controle → fila explode.

### Proposta

Aprovo apenas se:

* existe fila visível
* limites ativos
* comportamento sob carga conhecido

---

# 🐘 STEP 4 — PostgreSQL Hardening

## Owner

Platform / DBA

## Duração

4–6 dias

## Dependências

Step 3

---

## Execução

### 1. HA

* réplica
* failover definido

---

### 2. Backup + Restore

* WAL
* PITR

---

### 3. Teste real

* restaurar ambiente inteiro

---

### 4. Métricas

* connections
* lock wait
* latency
* replication lag

---

## Evidência

* restore executado
* failover simulado

---

## Gate de aprovação

### Diagnóstico

Hoje é SPOF.

### Impacto

DB down = plataforma down.

### Proposta

Aprovo apenas se:

* restore validado
* failover testado
* alertas ativos

---

# 📊 STEP 5 — Observabilidade

## Owner

Platform

## Duração

3–5 dias

## Dependências

Step 4

---

## Execução

### Métricas obrigatórias

* build per step
* retry count
* breaker state
* callback latency

---

### Logs

* estruturados
* correlation-id

---

### CLI

Mensagens claras:

* quota exceeded
* build failed
* breaker open

---

### Dashboards

* visão geral
* pipeline
* por tenant

---

## Evidência

* incidente simulado debugado via dashboard

---

## Gate de aprovação

### Diagnóstico

Sem observabilidade = cegueira.

### Impacto

Incidente vira tentativa e erro.

### Proposta

Aprovo apenas se:

* qualquer falha é diagnosticável em <10min

---

# 🧩 STEP 6 — Correção arquitetural (GitOps)

## Owner

Staff / Senior Backend

## Duração

5–7 dias

## Dependências

Step 5

---

## Execução

Separar:

| Antes          | Depois       |
| -------------- | ------------ |
| build faz push | API faz push |

---

### Novo fluxo

```
build → artifact
API → decide release
API → commit git
ArgoCD → sync
```

---

## Testes

* retry sem rebuild
* falha no git
* Argo degradado

---

## Evidência

* pipeline desacoplado
* retry correto

---

## Gate de aprovação

### Diagnóstico

Hoje está acoplado errado.

### Impacto

Escala = dor crescente.

### Proposta

Aprovo se:

* build não muta GitOps
* retry não recompila

---

# 📅 Timeline consolidado

| Semana | Steps           |
| ------ | --------------- |
| 1      | Step 0 + Step 1 |
| 2      | Step 2          |
| 3      | Step 3          |
| 4      | Step 4          |
| 5      | Step 5          |
| 6      | Step 6          |

---

# 🚨 Regras de execução (não negociáveis)

1. **Nada de paralelizar Steps críticos**

   * Step 2 antes de Step 3 é obrigatório

2. **Sem evidência = não concluído**

3. **Sem teste de falha = incompleto**

4. **Sem métrica = não operacional**

5. **Sem rollback = risco não aceito**

---

# 🔍 Como vou revisar cada entrega

Para cada Step você vai me trazer:

* PRs
* logs
* métricas
* resultado de testes

E eu vou responder:

## Diagnóstico

O que ainda está errado.

## Impacto

Risco residual real.

## Proposta

* ✅ aprovado
* ⚠️ aprovado com restrição
* ❌ reprovado

---

# 🧠 Veredito final

## Diagnóstico

Agora vocês têm um plano que transforma dívida técnica em execução objetiva.

## Impacto

Se seguir isso, vocês:

* sobrevivem early adopters
* evitam incidentes clássicos
* constroem base sólida para escala

Se não seguir:

* vão aprender tudo isso via incidente em produção

