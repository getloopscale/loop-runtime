# Dogfood Pre-Gate Checklist

> Executar ANTES de cada sessão de dogfood. Se qualquer item P0 falhar, o dogfood NÃO prossegue.
> Baseado na análise de padrões recorrentes identificados em Sprint 0 → Sprint 2 → pré-produção.

---

## Gate 1 — Wiring Real (Padrão #1: "Implementado mas não wired")

**Severidade: P0 — Bloqueante**

O padrão mais recorrente do projeto: código correto + testes passando, mas `main.go` instancia `Fake*` em vez de implementações reais.

```bash
# DEVE retornar 0 resultados. Qualquer match é bloqueante.
grep -rn 'Fake\|Mock\|Stub\|InMemory' api/cmd/main.go api/cmd/server.go 2>/dev/null

# Verificar que todas as interfaces do domínio têm implementação real no wiring
grep -rn 'func New' api/cmd/main.go api/internal/server/ 2>/dev/null | head -20
```

- [ ] `main.go` / `server.go` não contém nenhum `Fake*`, `Mock*`, `Stub*`, `InMemory*`
- [ ] `WorkflowSubmitter` aponta para Argo Workflows real
- [ ] `GitOpsPusher` aponta para Gitea real
- [ ] `SyncClient` está wrapped com `CircuitBreakerSyncClient`
- [ ] `EnvironmentLocker` usa a implementação com timeout detection + panic recovery
- [ ] `SourceStore` aponta para MinIO real

---

## Gate 2 — Auth Exit Codes (Padrão #2: Exit codes de auth errados)

**Severidade: P0 — Bloqueante**

Aparece em **toda** execução de dogfood. Auth validation espalhada em múltiplos code paths.

```bash
# Compilar CLI antes de testar
cd cli && npm run build && cd ..

# Testar sem token — DEVE retornar exit code 4
loopzero deploy 2>/dev/null; echo "EXIT: $?"
# Esperado: EXIT: 4

# Testar whoami sem token — DEVE retornar exit code 4
loopzero whoami 2>/dev/null; echo "EXIT: $?"
# Esperado: EXIT: 4

# Verificar que source-upload NÃO tem mapeamento de exit code próprio
grep -rn 'mapStatusToExitCode\|exitCode\|process.exit' cli/src/ --include="*.ts" | grep -v node_modules
```

- [ ] `loopzero deploy` sem token retorna exit code `4` (não `3`, não `0`)
- [ ] `loopzero whoami` sem token retorna exit code `4`
- [ ] Não existe mapeamento de exit code duplicado fora de `client.ts`
- [ ] Auth check acontece ANTES de qualquer I/O (upload, SSE connection)

---

## Gate 3 — Callbacks Best-Effort (Padrão #3: Callback 400 cascading)

**Severidade: P0 — Bloqueante**

Um callback rejeitado com 400 causa cascata: ghost builds → CLI trava → error_message vazio.

```bash
# Verificar que callback endpoints NUNCA retornam 400 para payloads internos
grep -rn 'http.StatusBadRequest\|400\|BadRequest' api/internal/routes/ --include="*.go" | grep -i callback

# Verificar que callback tem parsing best-effort
grep -rn 'callback' api/internal/routes/ --include="*.go" -l
```

- [ ] Endpoint de callback do Argo Workflows usa parsing best-effort (nunca rejeita com 400)
- [ ] Se o payload é malformado, callback loga warning e tenta extrair o que conseguir
- [ ] Build transitions para FAILED (não fica RUNNING para sempre) quando callback reporta erro
- [ ] `error_message` é preenchido via `SetErrorMessage` em todos os paths de falha

---

## Gate 4 — Zero Erros Silenciosos (Padrão #4: `_ = err`)

**Severidade: P1 — Crítico**

```bash
# DEVE retornar 0 resultados em código de produção.
# Exclui testes e código gerado.
grep -rn '_ = .*err\|_ = .*Err\|_ = .*Error' \
  api/internal/ api/cmd/ operators/internal/ \
  --include="*.go" \
  | grep -v '_test.go' \
  | grep -v 'vendor/' \
  | grep -v 'generated'
```

- [ ] Zero `_ = err` em código de produção (api/internal/, api/cmd/, operators/internal/)
- [ ] `eventStore.Append` erros são tratados (não descartados)
- [ ] `store.Store` em idempotency middleware tem tratamento de erro
- [ ] `json.Marshal` erros são logados com contexto

---

## Gate 5 — NetworkPolicy por Componente (Padrão #5: Bloqueio total de egress)

**Severidade: P1 — Crítico**

```bash
# Listar todas as NetworkPolicies
find infra/ -name "*.yaml" -o -name "*.yml" | xargs grep -l 'NetworkPolicy' 2>/dev/null

# Verificar que não existe policy global DNS-only
grep -A 20 'kind: NetworkPolicy' infra/**/*.yaml 2>/dev/null | grep -B5 -A15 'allow-dns'
```

- [ ] Não existe NetworkPolicy global "DNS-only" que bloqueia todo egress HTTPS
- [ ] API tem NetworkPolicy permitindo egress para: GitHub (443), Gitea, MinIO, ArgoCD, build cluster
- [ ] Cada componente declara suas necessidades de egress individualmente
- [ ] Políticas testadas com `kubectl exec` + `curl` de dentro do pod

---

## Gate 6 — Binary Rebuilt (Padrão #6: Fix causa regressão)

**Severidade: P1 — Crítico**

```bash
# Verificar que o binário CLI é recente (menos de 5 minutos)
ls -la cli/dist/ 2>/dev/null | head -5

# Rebuildar tudo antes do dogfood
cd cli && npm run build && cd ..
cd api && go build ./cmd/... && cd ..

# Verificar versão/timestamp do binário
loopzero --version 2>/dev/null
```

- [ ] `npm run build` executado no CLI após qualquer fix
- [ ] `go build` executado na API após qualquer fix
- [ ] Testes executados contra o binário **reconstruído**, não o antigo
- [ ] Após aplicar fix durante dogfood: parar → rebuildar → re-testar

---

## Gate 7 — Testes Passando (Baseline)

**Severidade: P0 — Bloqueante**

```bash
task test:unit
task lint
task validate:manifests
```

- [ ] `task test:unit` passa 100%
- [ ] `task lint` passa sem warnings
- [ ] `task validate:manifests` passa (kubeconform --strict)

---

## Resumo de Decisão

| Gates P0 falhando | Decisão |
|-------------------|---------|
| 0 | Prosseguir com dogfood |
| 1+ | **PARAR** — corrigir antes de prosseguir |

| Gates P1 falhando | Decisão |
|-------------------|---------|
| 0 | Prosseguir normalmente |
| 1-2 | Prosseguir com ressalva documentada |
| 3+ | **PARAR** — risco alto de falsos positivos no dogfood |

---

## Histórico de Padrões

| Padrão | Sprints afetados | Vezes visto |
|--------|-----------------|-------------|
| #1 Implementado mas não wired | Sprint 1, 2 | 3+ instâncias |
| #2 Auth exit codes errados | Sprint 1, 2, pré-prod | Todo dogfood |
| #3 Callback 400 cascading | Sprint 2 | 1 root → 4 findings |
| #4 Erros silenciosos `_ = err` | Sprint 2 | 5+ instâncias |
| #5 NetworkPolicy bloqueio total | Pré-produção | 1 instância |
| #6 Fix causa regressão | Sprint 1, 2 | 2 confirmadas |

> Checklist criado em 2026-03-18 baseado na análise de 40+ sessões de trabalho.
> Atualizar este checklist quando novos padrões recorrentes forem identificados.
