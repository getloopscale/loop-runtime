# Roadmap: Resilient ArgoCD Client — Token Management & Error Classification

> Plano executável baseado no MEET-2026-03-28-013.
> Resolve definitivamente: token provisioning, error classification, observability, auto-renewal.

---

## Grafo de Dependências

```
Phase 0.1 (fix bootstrap token) ──────> Phase 0.2 (fix operator token)
                                              └──> Phase 0.3 (converge provisioning paths)

Phase 1.1 (ErrArgoCDUnauthorized) ──> Phase 1.2 (SyncClient typed errors)
                                           └──> Phase 1.3 (circuit breaker classification)
                                                      └──> Phase 1.4 (emit circuit metrics)
                                                      └──> Phase 1.5 (auth failure counter)
Phase 1.6 (deep health auth distinction) ── independente
Phase 1.7 (startup token validation) ── depende de 1.1
Phase 1.8 (production runbook) ── independente

Phase 2.1 (TokenProvider interface) ──> Phase 2.2 (StaticTokenProvider)
                                            └──> Phase 2.3 (SecretWatchTokenProvider)
                                                      └──> Phase 2.4 (SyncClient uses provider)
                                                                └──> Phase 2.5 (single-flight 401 renewal)
                                                                └──> Phase 2.6 (health uses same provider)
Phase 2.7 (token_age metric) ── independente
```

**Ordem de execução:**
```
Sequencial:  0.1 → 0.2 → 0.3 (fix root cause first)
Paralelo:    1.1+1.6+1.8 (independentes)
Sequencial:  1.1 → 1.2 → 1.3 → 1.4+1.5 (chain)
Sequencial:  1.1 → 1.7 (startup validation)
Sequencial:  2.1 → 2.2+2.3 → 2.4 → 2.5+2.6
```

---

## Phase 0 — Fix Token Provisioning (BLOQUEANTE)

> Root cause: bootstrap armazena admin password como ARGOCD_TOKEN.
> Sem isto, nenhuma outra melhoria importa.

### Task 0.1: Fix bootstrap.ts — gerar apiKey JWT em vez de armazenar password

**Componente:** infra (Pulumi)
**Spec:** MEET-2026-03-28-013, Security VETO #1
**Depende de:** nenhuma
**Estimativa:** M (meio dia)

#### O que implementar

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — linhas 1180-1192

O bootstrap Job atualmente lê o admin password e armazena como `ARGOCD_TOKEN`:
```bash
--from-literal="ARGOCD_TOKEN=${ARGOCD_PASS:-admin}"
```

Deve ser substituído por um fluxo que:
1. Garante que a conta `loopzero-api` existe com capability `apiKey`
2. Gera um token via `/api/v1/account/loopzero-api/token`
3. Armazena o JWT no Secret

Modelo correto (de `provision-api-tokens.sh` linhas 49-82):
```bash
# 1. Login as admin
ARGOCD_SESSION=$(curl -s http://argocd-server:80/api/v1/session \
  -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASS\"}" | jq -r .token)

# 2. Generate apiKey token for loopzero-api account
ARGOCD_TOKEN=$(curl -s http://argocd-server:80/api/v1/account/loopzero-api/token \
  -H "Authorization: Bearer $ARGOCD_SESSION" \
  -d '{"name":"platform-deploy"}' | jq -r .token)

# 3. Store JWT (not password) in Secret
kubectl create secret generic loopzero-platform-tokens \
  --from-literal="ARGOCD_TOKEN=$ARGOCD_TOKEN" \
  --from-literal="GITEA_TOKEN=$GITEA_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -
```

#### Critérios de aceite

- [ ] `loopzero-platform-tokens` contém JWT (starts with `eyJ`), NÃO admin password
- [ ] JWT tem `sub: loopzero-api:apiKey` no payload
- [ ] Token funciona: `curl -H "Authorization: Bearer $TOKEN" /api/v1/applications` retorna 200
- [ ] Admin password NÃO aparece em nenhum Secret consumido pela API
- [ ] Bootstrap Job passa em ambiente limpo (destroy + recreate)

#### Riscos

- ArgoCD server pode não estar ready quando o Job executa → retry loop necessário
- A conta `loopzero-api` pode não existir se o Helm chart não aplicou o `accounts.loopzero-api: apiKey` → verificar order of operations

---

### Task 0.2: Fix PlatformBootstrap controller — usar apiKey, não session

**Componente:** operators
**Spec:** MEET-2026-03-28-013, Security VETO #4
**Depende de:** nenhuma
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** `operators/internal/controller/platformbootstrap_argocd.go`

O controller chama `createArgoCDSession` (endpoint `/api/v1/session`) que gera JWT com 24h TTL. Deve chamar `/api/v1/account/loopzero-api/token` que gera apiKey sem expiração.

#### Critérios de aceite

- [ ] Controller gera token via `/api/v1/account/loopzero-api/token`
- [ ] Token gerado NÃO expira (payload `exp: null`)
- [ ] Teste unitário valida endpoint correto

---

### Task 0.3: Convergir provisioning paths

**Componente:** infra + operators
**Depende de:** 0.1, 0.2
**Estimativa:** P (uma sessão)

#### O que implementar

Garantir que `provision-api-tokens.sh` (local), `bootstrap.ts` (Pulumi), e `PlatformBootstrap controller` (operator) usam o MESMO padrão de token generation. Documentar em ADR.

#### Critérios de aceite

- [ ] Os 3 paths geram apiKey token via `/api/v1/account/loopzero-api/token`
- [ ] ADR documentado em `docs/adr/`

---

## Phase 1 — Error Classification + Observability

> Sem isto, 401 abre circuit breaker e mascara o problema real.

### Task 1.1: Definir ErrArgoCDUnauthorized

**Componente:** pkg
**Depende de:** nenhuma
**Estimativa:** P (15 min)

#### O que implementar

**Arquivo:** `pkg/errors/errors.go`

```go
// ErrArgoCDUnauthorized is returned when ArgoCD rejects the API token (401/403).
// This is NOT a transient failure — the token is invalid and must be renewed.
// The circuit breaker MUST NOT count this toward the open threshold.
ErrArgoCDUnauthorized = &LoopZeroError{
    Code:       "ARGOCD_UNAUTHORIZED",
    Message:    "ArgoCD rejected the API token — check ARGOCD_TOKEN configuration",
    HTTPStatus: 401,
}
```

#### Teste primeiro

```go
func TestErrArgoCDUnauthorized_IsDistinctFromPlatformDegraded(t *testing.T) {
    assert.NotEqual(t, ErrArgoCDUnauthorized.Code, ErrPlatformDegraded.Code)
    assert.Equal(t, 401, ErrArgoCDUnauthorized.HTTPStatus)
}
```

---

### Task 1.2: SyncClient retorna erros tipados

**Componente:** api (argocd)
**Depende de:** 1.1
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** `api/internal/argocd/sync_client.go`

Em TODOS os métodos que fazem HTTP requests (TriggerSync, WaitForSync, GetAppStatus, GetSyncRevision), verificar `resp.StatusCode == 401 || resp.StatusCode == 403` e retornar `loopzeroerrors.ErrArgoCDUnauthorized` em vez de `fmt.Errorf("argocd sync: status %d: %s", ...)`.

```go
if resp.StatusCode == 401 || resp.StatusCode == 403 {
    return loopzeroerrors.ErrArgoCDUnauthorized
}
```

#### Critérios de aceite

- [ ] TriggerSync retorna `ErrArgoCDUnauthorized` em 401
- [ ] WaitForSync retorna `ErrArgoCDUnauthorized` em 401
- [ ] GetAppStatus retorna `ErrArgoCDUnauthorized` em 401
- [ ] Testes unitários para cada método

---

### Task 1.3: Circuit breaker classifica erros

**Componente:** api (services)
**Depende de:** 1.2
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** `api/internal/services/pg_circuit_breaker.go`

Modificar `recordResult` para NÃO contar `ErrArgoCDUnauthorized` como failure:

```go
func (cb *CircuitBreakerSyncClient) recordResult(err error) {
    // Auth failures are NOT transient — do not count toward circuit breaker
    if errors.Is(err, loopzeroerrors.ErrArgoCDUnauthorized) {
        cb.logger.Error("argocd auth failure (not counted toward circuit breaker)",
            "error", err)
        return
    }
    // ... rest of existing logic for transient failures
}
```

#### Critérios de aceite

- [ ] 401 NÃO incrementa failureCount
- [ ] 401 NÃO abre o circuito
- [ ] 503 CONTINUA abrindo o circuito (regression test)
- [ ] `ErrArgoCDUnauthorized` é propagado ao caller (não mascarado como ErrPlatformDegraded)

---

### Task 1.4: Emitir métrica do circuit breaker (corrigir phantom alert)

**Componente:** api (services)
**Depende de:** 1.3
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** `api/internal/services/pg_circuit_breaker.go`

Registrar e emitir gauge `loopzero_argocd_circuit_state`:
- 0 = closed (normal)
- 1 = open (rejecting)
- 2 = half-open (probing)

```go
var circuitStateGauge = prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "loopzero_argocd_circuit_state",
    Help: "ArgoCD circuit breaker state (0=closed, 1=open, 2=half-open)",
})
```

Atualizar em `recordResult` e `preCheck` quando o estado muda.

#### Critérios de aceite

- [ ] Gauge exposta em :9090/metrics
- [ ] Valor muda corretamente entre estados
- [ ] Alert `LoopZeroArgoCDCircuitOpen` agora dispara quando circuito abre

---

### Task 1.5: Métrica de auth failures

**Componente:** api (argocd)
**Depende de:** 1.2
**Estimativa:** P (15 min)

#### O que implementar

Counter `loopzero_argocd_auth_failures_total` incrementado cada vez que `ErrArgoCDUnauthorized` é retornado.

---

### Task 1.6: Deep health distingue auth de conectividade

**Componente:** api (routes)
**Depende de:** nenhuma
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** `api/internal/routes/health.go`

O `HTTPServiceChecker.Check` atualmente retorna erro genérico para qualquer status >= 400. Modificar para distinguir:

```go
if resp.StatusCode == 401 || resp.StatusCode == 403 {
    return fmt.Errorf("%s: auth_failed", c.name)
}
if resp.StatusCode >= 400 {
    return fmt.Errorf("%s: status %d", c.name, resp.StatusCode)
}
```

No `DeepHealth`, usar o prefixo "auth_failed" para reportar:
```json
{"argocd": "auth_failed", "database": "connected", "gitea": "connected", "status": "degraded"}
```

#### Critérios de aceite

- [ ] 401 → `"argocd": "auth_failed"`
- [ ] Connection refused → `"argocd": "unreachable"`
- [ ] 200 → `"argocd": "connected"`
- [ ] Testes unitários para os 3 cenários

---

### Task 1.7: Validação de token no startup

**Componente:** api (cmd)
**Depende de:** 1.1
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** `api/cmd/main.go`

Após construir o `SyncClient`, fazer uma chamada `GET /api/v1/session/userinfo` para validar o token. Se retornar 401, NÃO marcar o pod como Ready:

```go
if err := syncClient.ValidateToken(ctx); err != nil {
    logger.Error("ARGOCD_TOKEN is invalid — pod will not become Ready",
        "error", err, "suggestion", "regenerate token: see docs/runbooks/argocd-token-regeneration.md")
    // Return error from readiness probe
}
```

#### Critérios de aceite

- [ ] API com token inválido NÃO fica Ready
- [ ] Log ERROR com sugestão de runbook
- [ ] API com token válido inicia normalmente

---

### Task 1.8: Runbook de produção

**Componente:** docs
**Depende de:** nenhuma
**Estimativa:** P (30 min)

#### O que implementar

**Arquivo:** `docs/runbooks/argocd-token-regeneration.md`

Documentar o procedimento completo para DigitalOcean:
1. Diagnóstico (como saber que o token está inválido)
2. Geração de novo token via kubectl exec
3. Atualização do Secret
4. Rollout restart da API
5. Verificação

---

## Phase 2 — Token Provider Pattern

> Elimina restart requirement para rotação de tokens.

### Task 2.1: TokenProvider interface

**Componente:** api (argocd)
**Depende de:** Phase 1 completa
**Estimativa:** P (uma sessão)

#### O que implementar

**Arquivo:** novo `api/internal/argocd/token_provider.go`

```go
// TokenProvider supplies ArgoCD API tokens.
// Implementations handle caching, renewal, and Secret watching.
type TokenProvider interface {
    // Token returns the current valid token.
    // Implementations may read from a Secret, cache, or static string.
    Token() string
}
```

Duas implementações:
- `StaticTokenProvider` — wrapa uma string (para testes e local dev)
- `SecretWatchTokenProvider` — lê de file (mounted Secret), re-lê a cada 60s e on-demand após 401

#### Critérios de aceite

- [ ] Interface definida
- [ ] StaticTokenProvider funciona para testes existentes
- [ ] SecretWatchTokenProvider lê de arquivo e detecta mudanças

---

### Task 2.4: SyncClient usa TokenProvider

**Componente:** api (argocd)
**Depende de:** 2.1
**Estimativa:** M (meio dia)

#### O que implementar

Substituir `token string` por `tokenProvider TokenProvider` no `SyncClient`. Em cada request:

```go
req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())
```

#### Critérios de aceite

- [ ] Todos os testes existentes passam (usando StaticTokenProvider)
- [ ] SecretWatchTokenProvider funciona em integração
- [ ] Token mudança é refletida na próxima request (sem restart)

---

### Task 2.5: Single-flight renewal on 401

**Componente:** api (argocd)
**Depende de:** 2.4
**Estimativa:** P (uma sessão)

#### O que implementar

Quando `ErrArgoCDUnauthorized` é detectado:
1. Trigger re-read do Secret (via TokenProvider)
2. Se token mudou → retry a operação UMA vez
3. Se token não mudou → propagate error (Secret não foi atualizado ainda)
4. Use `singleflight.Group` para evitar stampede

```go
var renewGroup singleflight.Group

func (c *SyncClient) renewAndRetry(ctx context.Context, op func() error) error {
    err := op()
    if !errors.Is(err, loopzeroerrors.ErrArgoCDUnauthorized) {
        return err
    }
    // Single-flight: only one goroutine renews
    _, renewErr, _ := renewGroup.Do("renew", func() (interface{}, error) {
        return nil, c.tokenProvider.Refresh()
    })
    if renewErr != nil {
        return err // renewal failed, propagate original error
    }
    return op() // retry with new token
}
```

#### Critérios de aceite

- [ ] 401 triggers re-read do Secret
- [ ] Se token mudou → retry succeed
- [ ] Se token não mudou → error propagated
- [ ] Concurrent 401s → single renewal (test with goroutines)

---

## Exit Gates

### Phase 0 Exit Gate
```bash
# Token is JWT, not password
kubectl get secret loopzero-platform-tokens -n platform-system -o jsonpath='{.data.ARGOCD_TOKEN}' | base64 -d | head -c 3
# Expected: eyJ (JWT prefix)

# Token works
curl -H "Authorization: Bearer $TOKEN" http://argocd-server/api/v1/applications
# Expected: 200 OK
```

### Phase 1 Exit Gate
```bash
# Circuit breaker metric exists
curl -s localhost:9090/metrics | grep loopzero_argocd_circuit_state
# Expected: loopzero_argocd_circuit_state 0

# Auth failure metric exists
curl -s localhost:9090/metrics | grep loopzero_argocd_auth_failures_total
# Expected: loopzero_argocd_auth_failures_total 0

# Deep health shows auth distinction
# (with invalid token)
curl /api/v1/health/deep
# Expected: {"argocd": "auth_failed", ...}
```

### Phase 2 Exit Gate
```bash
# Rotate token in Secret
kubectl create secret generic loopzero-platform-tokens --from-literal=ARGOCD_TOKEN=$NEW_TOKEN --dry-run=client -o yaml | kubectl apply -f -

# API picks up new token WITHOUT restart (wait 60s for Secret propagation)
sleep 60
loopzero deploy --json
# Expected: success (API auto-refreshed token)
```

---

## Estimativa Total

| Phase | Tasks | Esforço | Bloqueio |
|-------|-------|---------|----------|
| **Phase 0** | 3 | 1 dia | Desbloqueia segurança |
| **Phase 1** | 8 | 2 dias | Desbloqueia observability + on-call |
| **Phase 2** | 5 | 3 dias | Desbloqueia zero-downtime rotation |
| **Total** | **16 tasks** | **~6 dias** | |
