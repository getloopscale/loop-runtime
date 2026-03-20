# 🧩 STEP 6 — Correção arquitetural (GitOps)

@sprint-2-final-report.md

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
