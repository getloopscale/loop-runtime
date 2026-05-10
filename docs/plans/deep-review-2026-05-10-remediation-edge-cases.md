# Edge Case Review — deep-review-2026-05-10-remediation

Data: 2026-05-10
Plano: docs/plans/deep-review-2026-05-10-remediation-plan.md
Tasks analisadas: 27
Edge cases encontrados: 8 (MUST FIX: 2, SHOULD TEST: 4, DOCUMENT: 2)

## MUST FIX

### EC-1: T0.1 — runtimeClientForPreview returns nil when CellClientFactory is nil AND preview has no cellID annotation
- **Task afetada:** T0.1
- **Familia:** State
- **Cenario:** Legacy PreviewEnvironment CRs criados antes do multi-cell não têm annotation `loopzero.io/cell-id`. Se `CellClientFactory` é nil (single-cell legacy deploy) E o CR não tem annotation, `runtimeClientForPreview` deve retornar `r.RuntimeClient` como fallback. O plano assume isso mas não testa o nil-factory path.
- **Impacto:** Se o helper retorna nil em vez de fallback → nil pointer dereference → controller panic
- **Fix sugerido:** Adicionar ao TDD de T0.1: `TestRuntimeClientForPreview_NilFactory_ReturnsFallback — factory=nil → returns r.RuntimeClient (not nil)`

### EC-2: T0.4 — domain_provisioner.go e build_reconcile_complete.go usam GetByID (unscoped) em paths internos que NÃO têm tenant context
- **Task afetada:** T0.4
- **Familia:** Permission
- **Cenario:** O plano diz "manter GetByID para uso interno" e "adicionar comment". Porém `domain_provisioner.go:308,450` e `build_reconcile_complete.go:150` chamam `GetByID` de dentro de services que podem ser invocados tanto por HTTP handlers quanto por background workers. Se alguém refatorar e remover o `GetByID` "deprecated" sem verificar TODOS os 10+ internal callers, builds/releases ficam inacessíveis.
- **Impacto:** Remoção prematura do `GetByID` quebra 10+ internal services
- **Fix sugerido:** Em vez de apenas comment, adicionar `// Deprecated: Use GetByIDForTenant for HTTP handlers. This method is required by convergence loops, reaper, and domain_provisioner.` + renomear para `GetByIDInternal` para tornar o intent explícito no nome.

## SHOULD TEST

### EC-3: T1.1 — SecretHandler lock key quando `environment` é vazio (secrets globais de projeto)
- **Task afetada:** T1.1
- **Familia:** Input
- **Cenario:** `loopzero secret set` permite secrets sem environment (project-level). Se o lock key usa `env=""`, o advisory lock hash pode colidir com outro tenant que também tem env vazio.
- **Teste sugerido:** `TestSecretSet_EmptyEnv_LockKeyUnique` — verify lock key includes project_id even when env is empty, preventing cross-project collision

### EC-4: T1.3 — Supersession falha por constraint violation (prev release já foi superseded por outro concurrent)
- **Task afetada:** T1.3
- **Familia:** Concurrency
- **Cenario:** Duas ativações concorrentes (polling path + callback path) ambas tentam superseder o mesmo prevRelease. Uma sucede, outra falha com constraint error. O fix (return error) faz a segunda rollback e re-tentar. No retry, prevRelease já está SUPERSEDED → UpdateStatus falha novamente → loop.
- **Teste sugerido:** `TestActivateRelease_PrevAlreadySuperseded_Succeeds` — if prev is already SUPERSEDED, skip supersession step (not an error)

### EC-5: T2.2 — Bundle digest verification step falha em builds com `build: dockerfile` (user Dockerfile, não loopzero-packs)
- **Task afetada:** T2.2
- **Familia:** Format
- **Cenario:** UserDockerfileStrategy não usa o bundle da mesma forma — o Dockerfile é extraído diretamente. Se o verify step assume formato de bundle padrão, builds `dockerfile` podem falhar na verificação.
- **Teste sugerido:** `TestWorkflowBuilder_DockerfileMode_DigestVerifyStillWorks` — verify the digest step works for both standard and dockerfile builds

### EC-6: T0.5 — Policy deny mode bloqueia pulls de imagens de init containers do próprio Argo Workflow
- **Task afetada:** T0.5
- **Familia:** Resource
- **Cenario:** Argo Workflow templates usam init containers com imagens como `alpine:3`, `busybox`, `bitnami/git`. Se deny mode rejeita imagens sem assinatura cosign e essas imagens de terceiros não estão assinadas, TODOS os builds param.
- **Teste sugerido:** `kubectl run test-unsigned --image=alpine:3 --rm -it` no workload cluster APÓS flip → deve funcionar se o policy tem exception para system namespaces e build namespaces

## DOCUMENT

### EC-7: T0.3 — LOOPZERO_API_PORT é dead env var (Go binary NÃO a lê)
- **Risco aceito:** Grep confirma zero matches em código Go para `LOOPZERO_API_PORT`. O binário usa porta hardcoded 3001 ou `config.go` com outra variável. A env var no values.yaml é lixo de iteração anterior — não causa CrashLoopBackOff (binary ignora). Severidade real é LOW (dead code), não CRITICAL. Fix ainda é correto (remover dead env var), mas prioridade pode ser rebaixada para P2.

### EC-8: T3.4 — Remover ArgoCD app pode causar resource orphaning se Helmfile não reconciliar imediatamente
- **Risco aceito:** Se o ArgoCD app for deletado com `cascade=true` (default), o ArgoCD vai deletar os resources que ele gerencia. Se Helmfile L2 não re-aplicar antes do próximo sync, há um gap onde admission-policies não existe no cluster. Mitigação: deletar o ArgoCD app com `cascade=false` (orphan policy) para que os resources permaneçam e Helmfile assuma ownership no próximo `helmfile sync`.

## Padrões Sistêmicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Sim | T0.1 — runtimeClientForPreview exists, never called |
| Correct code in wrong place | Não | — |
| Project name vs ID | Não | — |
| ArgoCD notifiers not services | Não | — |
| Single ArgoCD App per tenant | Não | — |
| CF scan imports conflicting apex | Não | — |

## Resumo

| Task | Edges | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------|----------|-------------|----------|
| T0.1 | 1 | 1 (nil factory) | 0 | 0 |
| T0.2 | 0 | 0 | 0 | 0 |
| T0.3 | 1 | 0 | 0 | 1 (dead env var) |
| T0.4 | 1 | 1 (internal callers) | 0 | 0 |
| T0.5 | 1 | 0 | 1 (init containers) | 0 |
| T1.1 | 1 | 0 | 1 (empty env) | 0 |
| T1.2 | 0 | 0 | 0 | 0 |
| T1.3 | 1 | 0 | 1 (already superseded) | 0 |
| T1.4 | 0 | 0 | 0 | 0 |
| T1.5 | 0 | 0 | 0 | 0 |
| T1.6 | 0 | 0 | 0 | 0 |
| T1.7 | 0 | 0 | 0 | 0 |
| T2.1 | 0 | 0 | 0 | 0 |
| T2.2 | 1 | 0 | 1 (dockerfile mode) | 0 |
| T2.3 | 0 | 0 | 0 | 0 |
| T3.1-T3.4 | 1 | 0 | 0 | 1 (orphan) |
| T4.1-T4.3 | 0 | 0 | 0 | 0 |
| T5.* | 0 | 0 | 0 | 0 |

**Veredicto: PLANO PRECISA DE AJUSTE (2 MUST FIX)**

Os 2 MUST FIX são menores — EC-1 adiciona 1 teste ao TDD de T0.1, EC-2 renomeia o método deprecated para tornar intent explícito. Nenhum requer mudança arquitetural ou nova task.
