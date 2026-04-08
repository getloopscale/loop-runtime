# Custom Domains v2 — Cloudflare Zone Management

> Modelo Vercel: LoopZero cria zona CF por domínio, usuário só muda nameservers.
> Validado com aquim.ai em 2026-04-08.

## Arquitetura

```
loopzero domain add empresa.com
       │
       ▼
┌─ LoopZero API ────────────────────────────────────────────┐
│ 1. CF API: POST /zones {name: "empresa.com"}          │
│ 2. CF API: POST /zones/{id}/dns_records/scan          │
│ 3. Review scan results + add CNAME @ → cname.dev...   │
│ 4. Return nameservers to user                         │
│ 5. Poll zone activation                               │
│ 6. Create IngressRoute for Traefik routing             │
└───────────────────────────────────────────────────────┘
       │
       ▼
Tráfego: CF edge (WAF/DDoS/TLS) → CF Tunnel → Traefik → Pod
```

## O que muda vs v1

| Aspecto | v1 (atual) | v2 (CF Zones) |
|---------|-----------|---------------|
| TLS | cert-manager + Let's Encrypt | CF Universal SSL (automático) |
| WAF/DDoS | Nenhum (ou só via tunnel) | CF edge completo |
| DNS verification | TXT `_loopzero-verify` manual | CF zone ownership (nameservers) |
| CNAME check | Background health checker | CF gerencia (zone ativa = válido) |
| Apex domains | Não funciona | Funciona (CF CNAME flattening) |
| Email preservation | N/A | DNS scan + import automático |
| Certificate in manifests | Certificate CR no gitops | Removido (CF cuida) |
| User effort | A/CNAME + TXT + esperar cert | Só mudar nameservers |

## DB Schema Changes

### Migration 000026: Add CF zone columns

```sql
ALTER TABLE custom_domains
    ADD COLUMN cf_zone_id TEXT,
    ADD COLUMN cf_zone_status TEXT DEFAULT 'pending'
        CHECK (cf_zone_status IN ('pending', 'active', 'failed', 'removed')),
    ADD COLUMN cf_nameservers TEXT[],
    ADD COLUMN dns_records_imported BOOLEAN NOT NULL DEFAULT false;

-- Update status CHECK to include new states
-- New lifecycle: pending_ns → ns_active → provisioning → active
ALTER TABLE custom_domains DROP CONSTRAINT IF EXISTS custom_domains_status_check;
ALTER TABLE custom_domains ADD CONSTRAINT custom_domains_status_check
    CHECK (status IN (
        'pending_ns',
        'ns_active',
        'provisioning',
        'active',
        'degraded',
        'failed',
        'removing'
    ));

COMMENT ON COLUMN custom_domains.cf_zone_id IS 'Cloudflare zone ID for this domain';
COMMENT ON COLUMN custom_domains.cf_nameservers IS 'Assigned CF nameservers the user must configure';
COMMENT ON COLUMN custom_domains.cf_zone_status IS 'CF zone activation: pending → active (NS propagated)';
```

## State Machine

```
loopzero domain add empresa.com
       │
       ▼
  ┌─────────┐     NS not propagated     ┌─────────┐
  │pending_ns│ ─────────────────────────→│ failed   │ (timeout 48h)
  └────┬─────┘                           └─────────┘
       │ CF zone active
       ▼
  ┌──────────┐     No active release     ┌──────────┐
  │ ns_active │ ──────── wait ──────────→│ ns_active│ (retry on deploy)
  └────┬──────┘                          └──────────┘
       │ Deploy triggers provisioning
       ▼
  ┌──────────────┐   gitops push fail    ┌─────────┐
  │ provisioning  │ ───────────────────→ │ failed   │
  └──────┬────────┘                      └─────────┘
         │ IngressRoute synced
         ▼
    ┌─────────┐
    │  active  │ ←── steady state
    └─────────┘

  loopzero domain remove empresa.com → removing → CF zone deleted → row deleted
```

---

## Fases e Microtasks

### Fase 0: CF Zone Client (Go package)
> Novo pacote para interagir com CF Zone + DNS APIs

#### 0.1 — Criar `api/internal/cfzone/client.go`

**O que:** Client HTTP para CF Zone Management API.

**Métodos:**
- `CreateZone(ctx, accountID, domain) → (zoneID, nameservers, error)`
- `GetZoneStatus(ctx, zoneID) → (status, error)`
- `TriggerActivationCheck(ctx, zoneID) → error`
- `DeleteZone(ctx, zoneID) → error`
- `ScanDNSRecords(ctx, zoneID) → ([]DNSRecord, error)`
- `AcceptScannedRecords(ctx, zoneID, recordIDs) → error`
- `CreateDNSRecord(ctx, zoneID, record) → error`
- `ListDNSRecords(ctx, zoneID) → ([]DNSRecord, error)`
- `DeleteDNSRecord(ctx, zoneID, recordID) → error`

**Aceite:**
- [ ] Todos os métodos implementados com error handling tipado
- [ ] Timeout configurável (default 30s)
- [ ] Rate limiting respeitado (CF: 1200 req/5min)
- [ ] Testes unitários com HTTP mock para cada método
- [ ] Nenhuma dependência de biblioteca HTTP externa (stdlib only)

**DoD:**
- [ ] `go test ./api/internal/cfzone/...` passa
- [ ] 100% dos métodos têm teste
- [ ] Error types distinguem: rate limit, auth, not found, conflict

#### 0.2 — Criar `api/internal/cfzone/types.go`

**O que:** Types para DNS records, zone status, etc.

```go
type DNSRecord struct {
    ID      string
    Type    string // A, AAAA, CNAME, MX, TXT, etc.
    Name    string
    Content string
    TTL     int
    Priority *int   // MX only
    Proxied  *bool  // CNAME/A only
}

type ZoneInfo struct {
    ID          string
    Name        string
    Status      string // pending, active, moved, deleted
    Nameservers []string
}
```

**DoD:**
- [ ] Types cobrem todos os record types que CF suporta
- [ ] JSON tags corretos para marshal/unmarshal da CF API

---

### Fase 1: DB Migration + Domain Store Update
> Adicionar colunas CF e atualizar state machine

#### 1.1 — Migration 000026

**Arquivo:** `api/internal/db/migrations/000026_custom_domains_cf_zones.up.sql`

**DoD:**
- [ ] Migration aplica sem erro em DB limpo e em DB com dados existentes
- [ ] Domínios v1 existentes mantêm status (backward compatible)
- [ ] `task db:migrate` passa

#### 1.2 — Atualizar DomainStore

**Arquivo:** `api/internal/db/domain_store.go`

**Mudanças:**
- `Create()` → aceita `cf_zone_id`, `cf_nameservers`
- `UpdateCFZoneStatus()` → novo método
- `FindPendingNS()` → novo método (domains em `pending_ns`)
- `FindNSActive()` → novo método (domains em `ns_active`)
- `TransitionStatus()` → atualizar para novos estados

**DoD:**
- [ ] Todos os novos métodos têm testes unitários
- [ ] Queries usam índices existentes ou novos
- [ ] Backward compatible (v1 domains não quebram)

---

### Fase 2: Domain Routes Rewrite
> POST /domains cria zona CF em vez de gerar TXT

#### 2.1 — Rewrite `POST /api/v1/domains`

**Arquivo:** `api/internal/routes/domains.go`

**Fluxo novo:**
1. Validar domain (existente)
2. `cfzone.CreateZone(accountID, domain)` → zoneID, nameservers
3. `cfzone.ScanDNSRecords(zoneID)` → records encontrados
4. `cfzone.AcceptScannedRecords(zoneID, allIDs)` → importa todos
5. `cfzone.CreateDNSRecord(zoneID, CNAME @ → cname.dev.loopzero.example, proxied)` → routing
6. Salvar no DB: status=`pending_ns`, cf_zone_id, cf_nameservers
7. Retornar: nameservers + dns_records importados

**Response novo:**
```json
{
  "ok": true,
  "domain": "empresa.com",
  "status": "pending_ns",
  "nameservers": ["chance.ns.cloudflare.com", "mina.ns.cloudflare.com"],
  "dns_records_imported": 14,
  "dns_records": [
    {"type": "MX", "name": "@", "value": "mx1.google.com", "priority": 1},
    ...
  ],
  "action_required": "Change your nameservers at your DNS provider"
}
```

**Aceite:**
- [ ] Domain criado no CF com zona funcional
- [ ] DNS scan executa e importa records
- [ ] CNAME @ → cname.dev.loopzero.example adicionado (proxied)
- [ ] Retorna nameservers e lista de records importados
- [ ] Teste: domain duplicado → 409
- [ ] Teste: CF API failure → 502 + rollback (deleta zona)
- [ ] Teste: domain bloqueado (platform zone) → 422

**DoD:**
- [ ] `go test ./api/internal/routes/... -run TestDomain` passa
- [ ] Teste de integração com CF API mock

#### 2.2 — Novo endpoint `POST /api/v1/domains/{domain}/dns-records`

**O que:** Permite ao usuário adicionar/editar DNS records manualmente.

**Request:**
```json
{
  "records": [
    {"type": "TXT", "name": "@", "content": "google-site-verification=abc123"},
    {"type": "CNAME", "name": "blog", "content": "external-blog.ghost.io"}
  ]
}
```

**Aceite:**
- [ ] Adiciona records na zona CF
- [ ] Rejeita records que conflitem com o CNAME @ do LoopZero
- [ ] Rejeita records perigosos (NS, SOA)
- [ ] Teste: adicionar MX válido → 200
- [ ] Teste: sobrescrever CNAME @ → 422 ("protected record")

**DoD:**
- [ ] Endpoint funcional com testes

#### 2.3 — Novo endpoint `GET /api/v1/domains/{domain}/dns-records`

**O que:** Lista DNS records atuais da zona (para o CLI mostrar).

**DoD:**
- [ ] Retorna lista de records da zona CF
- [ ] Filtrado por tenant (não pode ver zona de outro tenant)

#### 2.4 — Rewrite `DELETE /api/v1/domains/{domain}`

**Mudança:** Além de remover do DB, deleta a zona CF.

**Fluxo:**
1. Marcar status=`removing`
2. `cfzone.DeleteZone(zoneID)`
3. Remover IngressRoute do gitops
4. Deletar row do DB

**DoD:**
- [ ] Zona CF deletada
- [ ] IngressRoute removido do cluster
- [ ] Teste: delete domain → zona CF gone

#### 2.5 — Remover `POST /api/v1/domains/{domain}/verify`

**O que:** Endpoint de verificação TXT não é mais necessário (CF verifica via nameservers).

**DoD:**
- [ ] Endpoint retorna 410 Gone com mensagem de migração
- [ ] Nenhum código referencia verificação TXT

---

### Fase 3: Domain Provisioner Rewrite
> Background service que monitora zones e provisiona

#### 3.1 — CF Zone Activation Poller

**Arquivo:** `api/internal/services/domain_provisioner.go`

**Novo loop:** `processPendingNS()`
- Query domains com status=`pending_ns`
- Para cada: `cfzone.GetZoneStatus(zoneID)`
- Se `active` → transicionar para `ns_active`
- Se pending > 48h → transicionar para `failed` com reason

**DoD:**
- [ ] Polling a cada 60s
- [ ] Timeout 48h para NS propagation
- [ ] Teste: zone goes active → status transitions
- [ ] Teste: timeout → fails with reason

#### 3.2 — Provisioning (ns_active → active)

**Mudança:** Quando há release ACTIVE e domain é `ns_active`:
1. Render manifests COM IngressRoute para o custom domain
2. **SEM Certificate** (CF cuida do TLS)
3. Push para gitops
4. Transicionar → `active`

**Arquivo:** `api/internal/manifests/renderer.go`

**Mudança em `buildIngressResources()`:**
- Custom domains com `cf_zone_id` não geram Certificate CR
- Só geram IngressRoute
- TLS no IngressRoute usa passthrough ou nenhum (CF termina TLS no edge)

**DoD:**
- [ ] IngressRoute criado sem Certificate
- [ ] ArgoCD sync não bloqueia (sem Certificate Progressing)
- [ ] Teste: render com CF domain → sem Certificate no output

#### 3.3 — Remover CNAME health checker

**O que:** O health checker atual verifica se o CNAME aponta para nós. Com CF zones, isso não é necessário (CF gerencia).

**Mudança:** Desabilitar CNAME check para domains com `cf_zone_id` (manter para legacy v1).

**DoD:**
- [ ] Health checker ignora CF-managed domains
- [ ] Legacy v1 domains continuam sendo verificados

---

### Fase 4: CLI Domain Commands
> UX do loopzero domain add/status/remove/dns

#### 4.1 — Rewrite `loopzero domain add <domain>`

**Arquivo:** `cli/src/commands/domain.ts` (novo) ou `cli/src/index.ts`

**Fluxo:**
1. `POST /api/v1/domains` com `{domain, project, app, environment}`
2. Mostrar tabela de DNS records importados
3. Mostrar nameservers
4. Mostrar instruções por provider (GoDaddy, Namecheap, Hostinger)
5. Sugerir `loopzero domain status <domain>` para verificar

**Aceite:**
- [ ] Output mostra records importados em tabela formatada
- [ ] Mostra nameservers com destaque
- [ ] `--json` retorna structured output
- [ ] Exit code 0 em sucesso

**DoD:**
- [ ] Teste unitário cobre output format
- [ ] `--json` schema matches spec

#### 4.2 — Rewrite `loopzero domain status <domain>`

**Fluxo:**
1. `GET /api/v1/domains` filtrado
2. Mostrar: status, nameservers, TLS, WAF
3. Se `pending_ns` → "Waiting for nameserver propagation"
4. Se `active` → mostrar URL + TLS info

**DoD:**
- [ ] Teste para cada status possível
- [ ] `--json` output

#### 4.3 — Novo `loopzero domain dns <domain>`

**Fluxo:**
1. `GET /api/v1/domains/{domain}/dns-records`
2. Mostrar tabela de DNS records
3. Subcommands: `loopzero domain dns add`, `loopzero domain dns remove`

**Aceite:**
- [ ] `loopzero domain dns list empresa.com` → tabela formatada
- [ ] `loopzero domain dns add empresa.com --type TXT --name @ --value "v=..." `
- [ ] `loopzero domain dns remove empresa.com --id <record-id>`
- [ ] Validação: não permite remover CNAME @ (protegido)

**DoD:**
- [ ] Testes unitários para cada subcommand
- [ ] `--json` output

#### 4.4 — `loopzero domain remove <domain>`

**Mudança:** Adicionar confirmação (domínio + zona CF serão deletados).

```
$ loopzero domain remove empresa.com

  ⚠ This will:
    - Remove empresa.com from your project
    - Delete the Cloudflare DNS zone
    - Your nameservers will stop resolving

  Type "empresa.com" to confirm: empresa.com

  ✓ Domain removed
```

**DoD:**
- [ ] Confirmação obrigatória (bypass com `--force` apenas em CI)
- [ ] Zona CF deletada
- [ ] Teste unitário

---

### Fase 5: Config + Wiring
> Conectar tudo no main.go

#### 5.1 — Config fields

**Arquivo:** `api/internal/config/config.go`

**Novos campos:**
```go
CFAccountID        string `env:"CF_ACCOUNT_ID"`
CFAPIToken         string `env:"CF_API_TOKEN"`
CFZoneManagement   bool   `env:"CF_ZONE_MANAGEMENT" default:"true"`
```

**DoD:**
- [ ] Config valida que CF_ACCOUNT_ID e CF_API_TOKEN existem quando CF_ZONE_MANAGEMENT=true
- [ ] Fallback para v1 quando CF_ZONE_MANAGEMENT=false

#### 5.2 — Wire in main.go

**Arquivo:** `api/cmd/main.go`

**Mudanças:**
- Criar `cfzone.Client` com token do secret
- Injetar no `DomainHandler`
- Injetar no `DomainProvisioner`

**DoD:**
- [ ] API server inicia com CF zone client
- [ ] Graceful fallback se CF credentials não disponíveis (local dev)

---

### Fase 6: Testes E2E
> Validação completa do fluxo

#### 6.1 — Teste E2E: domain add → activate → access

**Steps:**
1. `loopzero domain add test-e2e-{random}.com` (domínio de teste)
2. Verificar zona criada no CF
3. Simular NS activation (ou usar domínio real de teste)
4. `loopzero deploy`
5. Verificar IngressRoute criado (sem Certificate)
6. Verificar `loopzero domain status` mostra active
7. `loopzero domain remove` → zona deletada

**DoD:**
- [ ] Teste passa em CI (com CF API token)
- [ ] Cleanup: zona CF deletada no teardown

#### 6.2 — Teste: email preservation

**Steps:**
1. Criar zona para domínio com MX records
2. Verificar MX importados corretamente
3. Verificar SPF/DKIM importados
4. `dig MX` retorna records corretos após NS propagation

**DoD:**
- [ ] MX records preservados após domain add

---

## Sequenciamento

```
Fase 0 (CF client)
    ↓
Fase 1 (DB migration + store) ──→ Fase 3 (provisioner rewrite)
    ↓                                    ↓
Fase 2 (API routes) ─────────────→ Fase 5 (wiring)
    ↓                                    ↓
Fase 4 (CLI commands) ───────────→ Fase 6 (E2E)
```

**Estimativa:** Fases 0-1 podem ser paralelas com Fase 4.
**Dependência crítica:** Fase 2 depende de Fase 0+1. Fase 3 depende de Fase 2.

## Riscos

| Risco | Mitigação |
|-------|-----------|
| CF API rate limit (1200/5min) | Batch operations, cache zone status |
| DNS scan incompleto | Edit manual + warning no CLI |
| NS propagation lenta (TLD .com vs .ai) | Timeout 48h, polling + user notification |
| Zona CF limpa acidentalmente | Soft delete (status=removing, delay 24h) |
| Email quebra | Scan + verify MX/SPF antes de retornar NS |
| CF account suspension | Alert + fallback docs |

## Inventário de Código Afetado (~3.500 linhas)

| Arquivo | Linhas | Mudança |
|---------|--------|---------|
| `api/internal/routes/domains.go` | 543 | Rewrite POST, DELETE. Novo: dns-records endpoints. Remover: verify |
| `api/internal/services/domain_provisioner.go` | 353 | Novo: NS poller. Mudar: provisioning sem Certificate |
| `api/internal/services/domain_health_checker.go` | 323 | Desabilitar CNAME check para CF domains |
| `api/internal/db/domain_store.go` | 474 | Novos métodos: FindPendingNS, FindNSActive, UpdateCFZoneStatus |
| `api/internal/manifests/renderer.go:808-857` | ~50 | Remover Certificate CR para CF domains. Manter IngressRoute |
| `api/internal/services/manifest_builder.go` | 84 | Adaptar BuildManifestApps para CF domains |
| `api/internal/domain/validation.go` | 156 | Manter (validação FQDN continua igual) |
| `api/internal/db/migrations/000024_*` | 67 | Inalterado (backward compat) |
| `api/internal/config/config.go:119-122` | 4 | Adicionar CFZoneManagement bool |
| `api/cmd/main.go:587-621` | 35 | Wire cfzone.Client |
| `cli/src/commands/domain.ts` | 372 | Rewrite add/status/remove. Novo: dns subcommand |
| `cli/src/index.ts:1371-1540` | 170 | Atualizar wiring |
| `pkg/statemachine/domain.go` | 85 | Novos estados: pending_ns, ns_active |
| **NOVO** `api/internal/cfzone/client.go` | ~200 | CF Zone + DNS API client |
| **NOVO** `api/internal/cfzone/types.go` | ~50 | Types |
| **NOVO** `api/internal/db/migrations/000026_*` | ~20 | CF zone columns |

### Decisões Arquiteturais Preservadas (do v1)

- **Three-Phase Provisioning** (F-002): Lock → Render → Unlock → Push → Status
- **Atomic Claiming** (FIND-001): `FOR UPDATE SKIP LOCKED` para HA
- **CAS Transitions** (CRIT-01): Compare-and-swap previne TOCTOU
- **Shared Manifest Builder** (F-010): `BuildManifestApps()` compartilhado
- **Domain-Derived Resource Names**: IngressRoute names baseados no domain hash
- **Formal State Machine** (pkg/statemachine): Transições validadas

## O que é REMOVIDO

- `verification_token` column (não mais necessário — CF valida via nameservers)
- `POST /api/v1/domains/{domain}/verify` endpoint (retorna 410 Gone)
- cert-manager Certificate para custom domains no manifest renderer
- CNAME health checker para CF-managed domains
- `tls_status`, `tls_expires_at` columns (CF gerencia TLS)
- `DNSVerifier` / `VerifyTXT` (verificação TXT não necessária)
