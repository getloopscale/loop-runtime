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
