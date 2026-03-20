# 🐘 STEP 4 — PostgreSQL Hardening

@sprint-2-final-report.md

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
