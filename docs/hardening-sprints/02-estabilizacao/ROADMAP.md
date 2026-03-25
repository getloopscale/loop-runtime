# Sprint 2 — ESTABILIZAÇÃO

**Objetivo:** TLS funcional em todos os 4 serviços. Sistema comprovadamente estável.
**Confiança:** MÉDIA (causa raiz TLS desconhecida até investigar)
**Esforço:** ~2h
**Pré-requisito:** GATE-SPRINT-1 passou
**Rollback:** Depende do fix — documentado após investigação.

---

## Tasks

### Task 2.1 — Investigar causa raiz do TLS

**Estado atual:** 1/4 certs Ready (apenas `tls-api`). Certs `tls-argocd`, `tls-git`, `tls-grafana` falhando.

**Investigação (NÃO mudar nada antes de entender):**

```bash
# 1. Estado dos certificados
kubectl -n platform-system get certificates -o wide

# 2. Requests pendentes
kubectl -n platform-system get certificaterequests -o wide

# 3. Orders ACME
kubectl -n platform-system get orders -o wide

# 4. Challenges ACME (onde provavelmente está o problema)
kubectl -n platform-system get challenges -o wide

# 5. Describe do cert falhando (ver Events e Conditions)
kubectl -n platform-system describe certificate tls-argocd

# 6. Logs do cert-manager
kubectl -n platform-system logs -l app.kubernetes.io/name=cert-manager --tail=100

# 7. ClusterIssuer
kubectl get clusterissuers -o wide
kubectl describe clusterissuer letsencrypt-prod

# 8. DNS resolve (cada subdomínio deve apontar para o Traefik LB)
dig +short argocd.dev.loopzero.example
dig +short git.dev.loopzero.example
dig +short grafana.dev.loopzero.example

# 9. HTTP-01 challenge routing (Traefik precisa rotear /.well-known/acme-challenge/)
curl -sf http://argocd.dev.loopzero.example/.well-known/acme-challenge/test
```

**Diagnóstico esperado (uma destas):**

| Causa | Evidência | Fix |
|---|---|---|
| HTTP-01 challenge não chega ao solver | Challenge em `pending`, Traefik não roteia | Ajustar Ingress/IngressRoute para ACME solver |
| Let's Encrypt rate limiting | Order com erro "too many certificates" | Esperar reset (1 semana) ou usar staging issuer |
| DNS não resolve | `dig` não retorna IP ou retorna IP errado | Corrigir registro DNS no DigitalOcean |
| ClusterIssuer misconfigured | Issuer com status False | Corrigir configuração do ClusterIssuer |

### Task 2.2 — Aplicar fix

**Ação:** Depende do diagnóstico acima. Documentar causa raiz e fix aplicado.

**Causa raiz:** _________________________________
**Fix aplicado:** _________________________________

### Task 2.3 — Validação

**Gate 2.3:**
- [ ] `kubectl get certificates -A` → 4/4 Ready=True
- [ ] `curl -sf https://argocd.dev.loopzero.example` → sem erro TLS
- [ ] `curl -sf https://git.dev.loopzero.example` → sem erro TLS
- [ ] `curl -sf https://grafana.dev.loopzero.example` → sem erro TLS

---

## GATE-SPRINT-2

- [ ] TLS 4/4 Ready
- [ ] 3 deploys consecutivos estáveis (re-validar GATE-SPRINT-1)
- [ ] ArgoCD UI acessível via HTTPS
- [ ] Grafana acessível via HTTPS
- [ ] Gitea acessível via HTTPS

**SE QUALQUER ITEM FALHAR → NÃO AVANÇAR PARA SPRINT 3.**
