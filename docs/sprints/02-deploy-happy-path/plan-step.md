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
