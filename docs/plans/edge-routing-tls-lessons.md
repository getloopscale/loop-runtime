# Edge Routing & TLS — Operational Lessons

**Session:** 2026-04-16 / 2026-04-17 (scale-50k L1→L2)
**Scope:** Aprendizados práticos de operar TLS + edge routing do LoopZero em produção DO. Capturados durante troubleshooting ao vivo, não do design teórico.
**Purpose:** Consulta rápida para próxima pessoa que toque esta camada. Erros listados foram cometidos aqui — não repita.

---

## 1. CF routing é **híbrido** — não assumir CF Tunnel universal

### O que descobrimos

Todos os subdomínios de `loopzero.example` parecem "passar por Cloudflare". Na prática, há **dois caminhos distintos**:

| Tipo de hostname | DNS | Caminho do request | Cert apresentado |
|---|---|---|---|
| Explícitos (api, auth, git, grafana, argocd, cloud, harbor) | **A record direto** → 24.144.71.120 | Client → Traefik LB direto | LE Prod (trusted) |
| Wildcard `*.dev.loopzero.example` | CNAME proxied → cfargotunnel.com | Client → CF Edge → cloudflared → Traefik | CF Universal SSL (trusted) |
| Custom user domains | CF Zone Management (per-domain) | Client → CF Edge → tunnel | CF Edge cert |

### Por que isso importa

Harbor quebrou (404) durante L2 por **não ter A record explícito**. Caiu só no wildcard → precisava de cloudflared → pod cloudflared **não existe no cluster** → 404.

Fix: adicionar A record explícito para hostnames críticos, não confiar só no wildcard.

### Regra prática

**Antes de declarar um hostname "pronto":**
```bash
dig +short <hostname> @1.1.1.1
# Se vier CF IP (104.26.x, 172.67.x) → depende de cloudflared OU CF edge SSL
# Se vier 24.144.71.120 → Traefik direto

curl -sv -o /dev/null https://<hostname>/ 2>&1 | grep -i "issuer"
# Confirma QUEM emitiu o cert que o browser vê
```

---

## 2. Origin CA ≠ "só trocar issuer"

### A tentação

cert-manager + Origin CA parece plug-and-play:
```yaml
issuerRef:
  group: cert-manager.k8s.cloudflare.com
  kind: OriginIssuer
  name: cloudflare-origin-ca
```
Troca isso em qualquer Certificate e pronto, né? **Não.**

### O que realmente acontece

Origin CA emite certs assinados por `CloudFlare Origin SSL Certificate Authority` — **não existe em trust store público**. Apenas a Cloudflare valida esse cert ao fazer handshake com o origin.

Cadeia esperada:
```
Browser → CF Edge (cert = CF Universal SSL, TRUSTED)
        → CF proxy → Origin (cert = Origin CA, TRUSTED by CF)
        → Traefik → App
```

Se o client pula CF (A record direto), ele recebe o cert Origin CA **e rejeita** porque não há CA pública na cadeia.

### Pré-requisitos para Origin CA funcionar

1. **DNS proxied=true** (orange cloud) — CF é intermediário, não bypass
2. **CF SSL mode = Full (Strict)** — CF valida o cert do origin
3. **Traefik serve o Origin CA cert** ao receber handshake do CF
4. **Client nunca acessa origin direto** (ou recebe untrusted warning)

### Quando Origin CA é a escolha certa

- Apps **sempre proxied** via CF (wildcard + CF Zone Management)
- UIs internas onde acesso direto é **explicitamente bloqueado**
- Tolerância zero a rate limit de LE (alta rotatividade)

### Quando **NÃO** usar Origin CA

- Subdomínios com A record direto (bypass)
- UIs que dev/ops precisam acessar fora de CF
- Health checks externos (uptime monitoring)
- Debug direto via IP

**Regra prática:** se `dig` retorna IP do Traefik LB em vez de IP CF, Origin CA **não vai funcionar** para esse hostname.

---

## 3. Rate limit de Let's Encrypt aparece em DEV, não em prod

### Sintoma
`too many certificates (5) already issued for this exact set of identifiers in the last 168h0m0s`

### Por que dev é pior que prod
- **Produção:** cada custom domain = identifier único → sem contenção de rate limit shared
- **Dev/CI:** mesmos hostnames recriados repetidamente (tests, refactors, resets) → bate no 5/168h

### Solução correta

| Ambiente | Issuer |
|---|---|
| dev local / CI | LE Staging (~30k certs/week) ou Pebble |
| prod | LE Prod (rate limits realistas para casos únicos) |
| origin-only | CF Origin CA (sem rate limit, ver §2) |

### Hack temporário
Quando LE Prod já bateu rate limit:
- LE Staging issue certs **untrusted por design** ("Fake LE Intermediate X1")
- Browser/curl com `-k` funciona; user normal vê warning
- Aceitável por até 1 semana até rate limit resetar

---

## 4. cert-manager external issuers têm **duas CRDs**

### Exemplo — origin-ca-issuer v0.10.0

A primeira vez que tentei usar `ClusterOriginIssuer` dei 404. Só `OriginIssuer` (namespace-scoped) existia. Mas nos logs do operator:
```
failed to wait for clusteroriginissuer caches to sync
```

Operator **precisa ambas** as CRDs instaladas, mesmo que você use só uma. O manifest upstream tem:
```
deploy/crds/cert-manager.k8s.cloudflare.com_originissuers.yaml
deploy/crds/cert-manager.k8s.cloudflare.com_clusteroriginissuers.yaml
```

Esqueci a segunda. Operator loop-crashed.

### Lição

Ao instalar operator externo: **aplique TODAS as CRDs do release**, não só a que você vai usar. Verifique com:
```bash
kubectl get crd | grep <operator-domain>
kubectl -n <operator-ns> logs -l app=<operator> | grep "cache"
```

---

## 5. Approver RBAC de cert-manager para issuers externos

### O que cert-manager faz

cert-manager tem um **approver controller** que aprova `CertificateRequest` automaticamente para issuers conhecidos. Para issuers externos (como Origin CA), ele **não tem permissão por default**.

### Sintoma

```
admission webhook "webhook.cert-manager.io" denied the request:
status.conditions: Forbidden: user "system:serviceaccount:loopzero-system:mgmt-cert-manager"
does not have permissions to set approved/denied conditions for issuer
{cloudflare-origin-ca OriginIssuer cert-manager.k8s.cloudflare.com}
```

### Fix

O upstream origin-ca-issuer fornece um `role-approver.yaml` com ClusterRoleBinding. **Default subject é `cert-manager/cert-manager` (SA) — se seu cert-manager usa outro SA (ex: `loopzero-system/mgmt-cert-manager`), PATCH o binding.**

```bash
kubectl patch clusterrolebinding cert-manager-controller-approve:cert-manager-k8s-cloudflare-com \
  --type=json -p='[{
    "op":"replace","path":"/subjects","value":[{
      "kind":"ServiceAccount",
      "name":"mgmt-cert-manager",
      "namespace":"loopzero-system"
    }]
  }]'
```

### Lição

Ao instalar external issuer: `grep -r <default-sa-name>` nos manifests de RBAC upstream. Se seu cert-manager usa SA customizado, patch OR reescreva.

---

## 6. Origin CA **duration** é negociada, não imposta

### Tentativa

```yaml
spec:
  duration: 43800h   # 5y per rotation policy
  renewBefore: 2160h
```

### Resultado real

Cert emitido: `notBefore=2026-04-17, notAfter=2029-04-16` — **~3 anos, não 5**.

Cloudflare Origin CA tem limites por tier de conta:
- Free/Pro: até 15y mas default varia
- Enterprise: mais opções

A API aceita `duration` no CSR mas **CF decide o valor final**. Seu `spec.duration` é sugestão.

### Lição

**Não assumir duration = settings.duration**. Validar com:
```bash
kubectl get secret <tls-secret> -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -noout -dates
```

Se duration real ≠ esperada, ajustar rotation policy baseado no observado (não no solicitado).

---

## 7. Traefik hot-reload de cert é quase instantâneo

### Surpresa positiva

Após mudar cert (delete secret → cert-manager recria), esperava precisar restart Traefik. **Não:** Traefik detecta mudança no Secret e troca cert em **< 5s** sem restart de pod.

### Verificação
```bash
# Before: old cert
kubectl delete secret tls-argocd-secret -n loopzero-system
sleep 10
# After: new cert, zero downtime
echo | openssl s_client -connect argocd.dev.loopzero.example:443 -servername argocd.dev.loopzero.example 2>/dev/null | \
  openssl x509 -noout -dates
```

### Lição

Para rotation/migration: **delete do secret é suficiente**, Traefik re-reads Secret watcher. Não precisa chamar `/api/reload` nem rolling restart.

Alta frequência de rotation (ex: Origin CA anual) é **zero-downtime**.

---

## 8. Checklist antes de migrar TLS issuer

Antes de mudar `issuerRef` de qualquer `Certificate` em prod:

- [ ] Confirmar **rota do request** até o origin (direct vs proxied)
- [ ] Confirmar **cert que browser vê** hoje (`openssl s_client`)
- [ ] Checar se target issuer tem **CRDs instaladas** (all of them)
- [ ] Checar se `approver RBAC` aponta para seu SA correto
- [ ] Se Origin CA: confirmar **CF SSL mode = Full (Strict)** e **DNS proxied**
- [ ] Testar em **1 cert low-criticality** primeiro
- [ ] Ter **rollback procedure** documentada (issuer anterior + restore command)

### Se você está com pressa

Qualquer item desses sem checar aumenta chance de regressão. Se o cert atual funciona, **não migre em prod sem testar em staging antes**. A lição desta sessão foi aplicar direto em prod (argocd/cloud) e descobrir os pré-requisitos tarde — 30min de downtime adicional para reverter.

---

## 9. Matriz de decisão — qual issuer para qual caso

```
         ┌─ Proxied via CF? ─ yes ─ Origin CA (ver §2)
         │
Cert novo┤
         │                                    ┌─ dev/CI? ─ yes ─ LE Staging
         └─ A record direto ─ trust público? ──┤
                                              └─ prod? ─ yes ─ LE Prod
                                                            (watch rate limit)
```

---

## 10. Things que demos como certo e não deveríamos ter

1. **"Cloudflare proxy tá ativo pra tudo"** — não, é híbrido (§1)
2. **"Universal SSL cobre qualquer subdomínio"** — só proxied=true
3. **"Origin CA funciona em qualquer origin"** — só se client passa por CF (§2)
4. **"15y validity = esqueça rotação"** — errado, operational discipline (policy 5y)
5. **"LE rate limit = problema de escala"** — não, é de dev workflow (§3)
6. **"Instalar um CRD do operator basta"** — não, todas as CRDs (§4)
7. **"cert-manager approva CRs automaticamente"** — sim pra in-tree, não pra external (§5)
8. **"Traefik precisa restart pra trocar cert"** — não (§7)
9. **"duration no spec = duration no cert"** — é sugestão (§6)

---

## 11. Ferramentas úteis para debug

```bash
# 1. Qual cert o browser realmente vê
echo | openssl s_client -connect <host>:443 -servername <host> 2>/dev/null | \
  openssl x509 -noout -issuer -subject -dates

# 2. Rota do request (é CF ou direto?)
dig +short <host> @1.1.1.1          # 1.1.1.1 evita cache local

# 3. Cert salvo no K8s Secret
kubectl get secret <tls-secret> -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -noout -text | head -20

# 4. Status de Certificate
kubectl describe certificate <cert-name>

# 5. CertificateRequest (etapa intermediária)
kubectl get certificaterequest -A
kubectl describe certificaterequest <cr-name>

# 6. Logs do issuer externo
kubectl logs -n <operator-ns> -l app=<operator> --tail=100

# 7. Lista de CRDs de um operator
kubectl get crd | grep <domain>

# 8. Approver RBAC
kubectl get clusterrolebinding | grep approve
```

---

## 12. Referências

- [CF Origin CA docs](https://developers.cloudflare.com/ssl/origin-configuration/origin-ca/)
- [cert-manager external issuers](https://cert-manager.io/docs/configuration/external/)
- [cloudflare/origin-ca-issuer](https://github.com/cloudflare/origin-ca-issuer)
- [LE rate limits](https://letsencrypt.org/docs/rate-limits/)
- [`tls-strategy.md`](tls-strategy.md) — strategy documento + implementação
- [`scale-50k-l2-learnings.md`](scale-50k-l2-learnings.md) — contexto L2 completo
- Session commits: `44b6114`, `0db30a7`, `6487c4c`
