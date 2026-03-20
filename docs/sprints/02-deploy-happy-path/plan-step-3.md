# 🔄 STEP 2 — Recuperação e Consistência

@sprint-2-final-report.md

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
