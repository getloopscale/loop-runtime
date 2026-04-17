# LoopZero TLS Strategy

**Status:** Approved (2026-04-17)
**Context:** Resolver rate limit Let's Encrypt + eliminar fragilidade de ACME challenges para UIs internos, sem abandonar cert-manager.

## Princípio

> **Terceirize a autoridade emissora certa para cada camada, mas preserve o gerenciador declarativo.**

cert-manager continua como plano de controle declarativo. Trocamos apenas os **issuers** por camada.

## Camadas TLS do LoopZero

| Camada | Domínios | Issuer | Justificativa |
|---|---|---|---|
| **Edge → user apps** | `{tenant}-{project}-{app}-{env}.dev.loopzero.example` | **CF Universal SSL** (automático) | Proxied, grátis, cobertura global, zero ops |
| **Edge → custom domains** | `aquim.ai`, etc. | **CF Zone Management** (per-zone) | Por-tenant, onboarding via DNS, WAF/DDoS incluso |
| **Edge → UIs internos** | `argocd.dev.loopzero.example`, `cloud.dev.loopzero.example`, `auth.dev.loopzero.example`, `harbor.dev.loopzero.example` | **CF Origin CA** | Tráfego só via CF proxy, sem rate limit, cert longo |
| **Load test / dev** | `tenant-loadtest-*.dev.loopzero.example` | **LE Staging** | Sem rate limit, untrusted por design |
| **Trust público direto** | (exceção) | LE Prod | Apenas onde navegador precisa validar sem passar por CF |

## Issuers configurados

### 1. `letsencrypt-prod` (existing)
- **Escopo:** domínios que requerem trust público direto (sem CF proxy)
- **Rate limit:** 5 certs/168h por identifier set
- **Uso recomendado:** **reservar para casos excepcionais** — a maioria dos domains do LoopZero passa por CF Edge

### 2. `letsencrypt-staging` (novo, `infra/manifests/cluster-issuers/letsencrypt-staging.yaml`)
- **Escopo:** load tests (L1-L5), dev experimentation
- **Rate limit:** ~30k certs/week por account (não se atinge)
- **Trust:** emitido por "Fake LE Intermediate X1" — untrusted por browsers
- **Uso:** qualquer Certificate com identifier que será recriado repetidamente

### 3. `cloudflare-origin-ca` (novo — requer provisionamento de chave)
- **Escopo:** UIs internos (argocd, cloud, auth, harbor) — proxied via CF Tunnel
- **Rate limit:** nenhum (via CF API)
- **Validade:** até 15 anos (mas ver seção "Rotation Policy" abaixo)
- **Trust:** aceito apenas por Cloudflare — OK porque origin recebe só de CF
- **Integração:** `origin-ca-issuer` ([cloudflare/origin-ca-issuer](https://github.com/cloudflare/origin-ca-issuer)) — **external cert-manager integration, not in-tree**

## Rotation Policy (CF Origin CA)

Cloudflare Origin CA permite certs de até 15 anos — **não significa rotação eterna**.

**Policy:**
- **Validity:** emitir com 5 anos máximo (não 15)
- **Renewal:** cert-manager renova automaticamente aos 2/3 de validade (config via `spec.duration` + `spec.renewBefore`)
- **Audit anual:** revisar se Origin CA key (service key) precisa rotation
- **Service key rotation:** se comprometida ou anualmente em rotação disciplinada

Razão: certificados de 15 anos violam princípio de "least lifetime" de supply chain security. Mesmo que CF permita, operacionalmente rotacionamos.

## Migration Plan

### Imediato (esta sessão)
- [x] LE Staging ClusterIssuer criado
- [x] origin-ca-issuer instalado (operator running)
- [ ] Origin CA key provisionada (Paulo → CF dashboard)
- [ ] Migration: `tls-argocd`, `tls-cloud` → Origin CA
- [ ] Migration: `tls-auth`, `tls-harbor` → Origin CA (se existentes)

### Próxima sessão
- [ ] Pulumi code referencing `cloudflare-origin-ca` ClusterIssuer (em vez de `letsencrypt-prod`) para UIs internos
- [ ] Documentar em ADR
- [ ] Setup monitoring: alert se cert prestes a expirar em < 30 dias

## Provisioning Steps (Origin CA key)

```bash
# 1. Get Origin CA Key from CF dashboard:
#    https://dash.cloudflare.com/profile/api-tokens
#    Section: "Origin CA Key" → Click "View"
#    (different from regular API tokens — legacy "v4 auth" key)

# 2. Create secret in origin-ca-issuer namespace
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt -n origin-ca-issuer \
  create secret generic cloudflare-origin-ca-key \
  --from-literal=key='<ORIGIN_CA_KEY_FROM_DASHBOARD>'

# 3. Apply ClusterOriginIssuer
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt \
  apply -f infra/manifests/cluster-issuers/cloudflare-origin-ca.yaml

# 4. Wait for ready
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt \
  get clusteroriginissuers.cert-manager.k8s.cloudflare.com -w

# 5. Migrate a Certificate (example: tls-argocd)
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt \
  -n platform-system patch certificate tls-argocd --type=json -p='[
    {"op":"replace","path":"/spec/issuerRef","value":{
      "group":"cert-manager.k8s.cloudflare.com",
      "kind":"ClusterOriginIssuer",
      "name":"cloudflare-origin-ca"
    }},
    {"op":"replace","path":"/spec/duration","value":"43800h"}
]'
# duration: 43800h = 5y (not 15y, per rotation policy above)
```

## Fallback Procedure (Origin CA outage)

Origin CA depends on (1) Cloudflare API available, (2) `origin-ca-issuer` operator running, (3) `cloudflare-origin-ca-key` secret valid. If any breaks, new certs cannot be issued. Existing certs keep working until their validity (5y cap) expires.

### Emergency fallback steps

**Scenario:** Origin CA issuer broken at 3am, a fresh tenant needs a cert for `argocd.{domain}`.

**Runbook:**

1. **Diagnose** — is it local or upstream?
   ```bash
   kubectl describe clusterissuer cloudflare-origin-ca
   kubectl -n origin-ca-issuer logs -l app=origin-ca-issuer --tail=50
   # If the operator is up but CF API returning errors, it's upstream.
   ```

2. **Temporary fallback to `letsencrypt-prod`** (for UIs that must be up NOW):
   ```bash
   # Patch the specific Certificate to use LE Prod
   kubectl -n platform-system patch certificate <cert-name> --type=json -p='[
     {"op":"replace","path":"/spec/issuerRef","value":{
       "name":"letsencrypt-prod",
       "kind":"ClusterIssuer"
     }},
     {"op":"remove","path":"/spec/duration"}
   ]'
   ```
   Accept: LE Prod has 5 certs/168h rate limit per identifier set. Only do this for the UIs actually down. **Do not** mass-migrate — you'll hit the rate limit.

3. **Disable CF-only middleware on those routes temporarily** (so LE HTTP-01 challenge reaches the origin):
   ```bash
   # Remove cloudflare-ip-allowlist and require-cloudflare-proxy middleware
   # from affected IngressRoute(s). Restore after Origin CA is back.
   ```

4. **After Origin CA recovers** — migrate back:
   ```bash
   # Revert the patch above. New Origin CA cert issued; LE cert stays until revoked.
   ```

### Non-emergency fallback

If Origin CA is persistently broken (operator deprecated, CF API changes), migrate entire fleet back to LE Prod permanently. Track in ADR. Accept the rate limit trade-off (mitigate with DNS-01 wildcard certs + ARI).

## Security hardening

### Origin-only access enforcement

Origin CA certs **are only trusted by Cloudflare**. If traffic bypasses CF (direct IP access, internal debug, health check misconfigured), the TLS handshake fails and the origin exposure is also wrong.

Defense-in-depth via Traefik middleware:

- **`require-cloudflare-proxy`** — rejects requests missing `CF-Connecting-IP` header (set by CF on all proxied requests). See `infra/manifests/cluster-issuers/cf-origin-ca-middleware.yaml`.
- **`cloudflare-ip-allowlist`** — restricts to CF IP ranges. Snapshot 2026-04-17; **re-validate quarterly** at https://www.cloudflare.com/ips.

Applied to IngressRoutes serving UIs with Origin CA:
```yaml
spec:
  routes:
  - match: Host(`argocd.dev.loopzero.example`)
    middlewares:
    - name: cloudflare-ip-allowlist
    - name: require-cloudflare-proxy
```

### Origin CA key management

The Origin CA key can issue certs for **any domain in the account's zones** — it's a sensitive credential. Storage hierarchy:

1. **Preferred:** External Secrets Operator (ESO) sourcing from Vault/SOPS/DO Spaces encrypted. See `infra/manifests/cluster-issuers/cf-origin-ca-externalsecret.yaml`.
2. **Acceptable:** `kubectl create secret` **only** during bootstrap, then rotate into ESO. Audit trail in runbook.
3. **Never:** commit to git, store in ConfigMap, log in build output.

ESO sync interval: 1h. Rotation in backend propagates automatically — no pod restart needed.

### Secret rotation (Origin CA key)

- **Frequency:** annual (scheduled) or immediately on suspected compromise.
- **Process:** generate new key in CF dashboard → update backend → ESO syncs → origin-ca-issuer picks up on next reconcile → certs issued with new key going forward. Old certs remain valid (signed by CF intermediate, not the service key).
- **Zero downtime:** existing certs unaffected; only *new issuances* use new key.

## Observability

### Metrics

cert-manager exports these (already scraped by Prometheus):
- `certmanager_certificate_expiration_timestamp_seconds{name, namespace}` — notAfter timestamp
- `certmanager_certificate_ready_status{name, namespace, condition}` — Ready condition
- `certmanager_clusterissuer_ready_status{name, condition}` — Issuer health

### Alerts (3 added — `infra/manifests/cluster-issuers/tls-alerts.yaml`)

| Alert | Trigger | Severity | Rationale |
|---|---|---|---|
| `LoopZeroCertApproachingRotationCap` | cert < 180 days to expire | warning | Origin CA certs don't expire in 90d like LE — forget-and-break risk |
| `LoopZeroClusterIssuerNotReady` | issuer `Ready=False` for 15min | critical | Blocks new cert issuance; early signal before Certificates fail |
| `LoopZeroCertStuckNotReady` | cert `Ready=False` for 30min | warning | Catches DNS-01 hangs, rate limit hits, race conditions |

## Referências

- [cert-manager External Issuers](https://cert-manager.io/docs/configuration/external/)
- [cloudflare/origin-ca-issuer](https://github.com/cloudflare/origin-ca-issuer)
- [CF Origin CA docs](https://developers.cloudflare.com/ssl/origin-configuration/origin-ca/)
- [LE Rate Limits](https://letsencrypt.org/docs/rate-limits/)
- [LE Staging](https://letsencrypt.org/docs/staging-environment/)

## ADR References

- Complementa ADR sobre custom domains (CF Zone Management) — mesma arquitetura de "CF cuida do edge"
- Não altera GitOps, CRDs, RBAC — só troca `issuerRef` em Certificates existentes
