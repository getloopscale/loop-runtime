# Edge Case Review — 2026-05-22-shell-script-strategic-plan

**Data:** 2026-05-22
**Plano:** docs/plans/2026-05-22-shell-script-strategic-plan.md
**Branch:** refactor/shell-scripts-strategic @ `893232c3`
**Tasks analisadas:** 20 (T0.1, T1.1, T1.2, T2.1-T2.4, T3.1-T3.2, T4.1-T4.4, T5.1-T5.3, T6.1-T6.4)
**Edge cases encontrados:** 10 (MUST FIX: 4, SHOULD TEST: 3, DOCUMENT: 3)

---

## MUST FIX

### EC-01: helmfile `set:` block via `exec kubectl` falha em fresh cleanroom (race postgresql ↔ gitea)

- **Task afetada:** T2.2 + T2.3
- **Família:** Timing
- **Cenário:** O plano propõe `set: PASSWD: '{{ exec "kubectl" ... get secret postgresql ... }}'` para gitea. Mas em fresh cleanroom (`task setup` cold start) a sequência é: criar cluster → install postgresql (chart Bitnami cria Secret post-install) → install gitea. Se a invocação do helmfile gitea acontecer em uma chamada `task` SEPARADA da postgresql install (per-selector pattern proposto no T2.3 "UMA invocation per release"), `helmfile needs:` NÃO ajuda porque `needs:` só funciona entre releases da MESMA invocação helmfile.
- **Impacto:** Primeiro `task setup` falha com `error: secret "postgresql" not found` no exec kubectl do gitea sync. Cleanroom não converge no primeiro try — exatamente o bug que o plano quer corrigir.
- **Fix sugerido:** T2.3 deve usar **MASTER sync** (single `helmfile sync` sem `--selector`) por cluster, deixando `needs:` declarar dependência postgresql → gitea. Adicionar sub-task T2.3.a: "Validate ordering — postgresql release tem `needs: []`, gitea release tem `needs: [postgresql]`; helmfile master sync respeita topological order".

### EC-02: `kubectl ... | base64 -d` quebra em macOS (flag é `-D`, não `-d`)

- **Task afetada:** T2.3
- **Família:** Format
- **Cenário:** O pattern proposto `export LOOPZERO_DB_PASSWORD=$(kubectl ... | base64 -d)` usa GNU coreutils `base64`. macOS BSD `base64` aceita apenas `-D` (capital D); `-d` é silenciosamente diferente (data flag deprecated). Mac contributors veem export vazio + helmfile falha com `requiredEnv "LOOPZERO_DB_PASSWORD" is not set`.
- **Impacto:** `task setup` falha em Mac (cohort não-trivial de operadores LoopZero); developer perde 30+ min debugando até descobrir que é diferença BSD/GNU.
- **Fix sugerido:** Substituir `base64 -d` por `openssl base64 -d -A` (portable POSIX) **OU** usar `kubectl get secret ... -o go-template='{{.data.password | base64decode}}'` (decoded direto pelo kubectl, zero pipe). Adicionar sub-task T2.3.b: "Validate cross-platform — `task setup` PASS em Mac + Linux via CI matrix OR documentar Linux-only com CI gate".

### EC-03: T0.1 regex caller detection MISSES dynamic invocations `bash "$SCRIPT"`

- **Task afetada:** T0.1 + T1.1
- **Família:** Input
- **Cenário:** Regex `<script-name>(?:\.sh)?\b` não match `bash "$SCRIPT_VAR"`, `bash "$(get-script).sh"`, ou `source "$DIR/$NAME.sh"`. Existem scripts assim no LoopZero (e.g., dispatchers em `infra/scripts/lib/`). Audit marca script como `DELETE-DEAD` (0 callers) mas script é VIVO via interpolation.
- **Impacto:** Phase 1.2 deleta script vivo → fresh cleanroom quebra silently. Bug só descoberto via `task setup` falhar em next run.
- **Fix sugerido:** Adicionar sub-task T1.1.a: "Anti-falso-positivo — antes de marcar `dead-set-validated.csv`, rodar `grep -rE 'bash[[:space:]]+["\$]' infra/ taskfiles/ Taskfile.yml`; qualquer script cujo basename apareça em variable interpolation é re-classificado como `REVIEW-NEEDED`". Adicionar test em T0.1 TDD: `TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead`.

### EC-04: T6.2 git diff requer `fetch-depth: 0` no checkout — falsos positivos em massa se não

- **Task afetada:** T6.2
- **Família:** Boundary
- **Cenário:** `git diff --name-only --diff-filter=A origin/main...HEAD` requer histórico completo da base branch. GitHub Actions default checkout é `fetch-depth: 1` (shallow clone). Sem fetch-depth: 0, `origin/main` resolve para single commit; diff vira "tudo é novo" → gate marca TODOS os 30+ scripts allowlistados como violations → CI fica permanentemente vermelho.
- **Impacto:** Gate de proteção vira anti-gate (sempre falha → operadores aprendem a ignorar/desabilitar). Pior cenário: gate é desabilitado por frustração e regra D3 fica letra-morta.
- **Fix sugerido:** T6.2 acceptance criteria adicionar: "CI job invoca `actions/checkout@v4` com `fetch-depth: 0` E gate roda `git fetch origin main` explicitamente antes do diff". Adicionar test: `TestCheck_ShallowClone_FailsWithClearError` — quando base-ref unreachable, retorna erro `BASE_REF_UNREACHABLE` com mensagem actionable ("Set fetch-depth: 0 in checkout").

---

## SHOULD TEST

### EC-05: Pulumi auto.SelectStack erro genérico sem LoopZeroError typing

- **Task afetada:** T5.1
- **Teste sugerido:** `TestAutoAPI_StackNotFound_TypedError` — fixture chama `auto.SelectStack(ctx, "nonexistent-stack", ws)`; assert error wrapping é `LoopZeroError{Code: "PULUMI_STACK_NOT_FOUND"}` (não generic `fmt.Errorf`). Sem tipagem, callers em CI não conseguem distinguir "stack ausente esperado" vs "passphrase wrong" vs "workspace corrupt".

### EC-06: T6.2 não testa file rename (git status R)

- **Task afetada:** T6.2
- **Teste sugerido:** `TestCheck_RenamedBashOutsideAllowlist_Blocked` — fixture diff com `R100 old.sh new.sh` onde `new.sh` não está na allowlist; assert gate falha com mensagem clara ("Rename detected: add 'new.sh' to allowlist OR keep old name"). Plan acceptance menciona "Renamed file (R status): treated as new + old" mas TDD não tem o teste.

### EC-07: T0.1 symlinks não testados

- **Task afetada:** T0.1
- **Teste sugerido:** `TestScanRepo_HandlesSymlinks_NoCycle` — fixture com symlink `infra/scripts/alias.sh -> ../legacy/real.sh`; assert audit (a) lista uma vez (não duas), (b) detecta callers de `alias.sh` E `real.sh` como callers do "mesmo" script. Plan menciona `filepath.EvalSymlinks` em deep dive mas não tem TDD para isso.

---

## DOCUMENT

### EC-08: T6.3 AST walker não pega shell-wrap via variable

- **Risco aceito:** `cli := "pulumi"; shellrun.Runner.Run(ctx, cli, ...)` escapa o walker (analisa só string literals). Fix seria full data-flow analysis — desproporcional ao dano. Code review humano + golangci-lint custom rule são o backstop. Documentar limitação no help do gate: "Detecta apenas literais; refactor para variable é code review job".

### EC-09: T2.2 secret rotation não restarta gitea pod

- **Risco aceito:** Se postgres password rotaciona, `helmfile sync` re-renderiza values mas K8s não reinicia pod gitea (rollingUpdate só dispara em template hash change, e set: via exec produz hash NOVO mas só na PRÓXIMA sync). Documentar em `docs/operations/secret-rotation.md`: "Após rotation, run `kubectl --context k3d-loopzero-mgmt rollout restart deployment/gitea`".

### EC-10: T4.2 JWT TTL=1h vs Ginkgo full journey ~30min

- **Risco aceito:** Full Ginkgo suite roda 30+min; JWT TTL=1h dá margem mas builds lentos (CI cold cache) podem chegar perto. Não vale aumentar TTL globalmente (security). Documentar em `dev-loop.md`: "Re-mint JWT (`bash infra/helmfile/scripts/seed-local-user.sh`) antes de full Ginkgo run se sessão > 30min".

---

## Padrões Sistêmicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Parcial | T0.1 wires em quality_subcmd ✓; T6.2/T6.3 explicitamente wired em ci.yml ✓; mas T5.2 `go generate` não tem CI gate forçando regen (drift risk) |
| Correct code in wrong place | Não | Audit + gates em `pkg/quality/` (coerente com 16 gates já migrados) |
| Project name vs project ID | N/A | Plano não toca deploy flow |
| ArgoCD notifiers not services | N/A | Plano não toca ArgoCD config |
| Single ArgoCD App per tenant | N/A | Plano não toca tenant routing |
| Lock discipline (withEnvironmentLock) | N/A | Plano não toca mutações API |
| Rule 3 (termos internos CLI) | N/A | Plano não toca CLI output |
| Multi-cell sticky placement | N/A | Plano não toca placement |

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 2 | 1 (EC-03) | 1 (EC-07) | 0 |
| T1.1 | 1 | 1 (EC-03 shared) | 0 | 0 |
| T1.2 | 0 | 0 | 0 | 0 |
| T2.1 | 0 | 0 | 0 | 0 |
| T2.2 | 2 | 1 (EC-01) | 0 | 1 (EC-09) |
| T2.3 | 2 | 2 (EC-01 shared + EC-02) | 0 | 0 |
| T2.4 | 0 | 0 | 0 | 0 |
| T3.1 | 0 | 0 | 0 | 0 |
| T3.2 | 0 | 0 | 0 | 0 |
| T4.1 | 0 | 0 | 0 | 0 |
| T4.2 | 1 | 0 | 0 | 1 (EC-10) |
| T4.3 | 0 | 0 | 0 | 0 |
| T4.4 | 0 | 0 | 0 | 0 |
| T5.1 | 1 | 0 | 1 (EC-05) | 0 |
| T5.2 | 0 | 0 | 0 | 0 |
| T5.3 | 0 | 0 | 0 | 0 |
| T6.1 | 0 | 0 | 0 | 0 |
| T6.2 | 2 | 1 (EC-04) | 1 (EC-06) | 0 |
| T6.3 | 1 | 0 | 0 | 1 (EC-08) |
| T6.4 | 0 | 0 | 0 | 0 |

**Veredicto:** **PLANO PRECISA DE AJUSTE**

Os 4 MUST FIX (EC-01, EC-02, EC-03, EC-04) são bloqueantes:
- EC-01 + EC-02 atacam o **success criterion principal do plano** (cleanroom funcional). Se T2.3 não usa master-sync + portable base64, primeiro `task setup` em Mac OU em CI quebra exatamente como hoje, e o plano falha em seu objetivo number-one.
- EC-03 ataca a **safety net de Phase 1** (delete sem quebrar nada). Sem detecção de invocação dinâmica, audit produz falsos-positivos e deletion em massa quebra silently.
- EC-04 ataca o **mecanismo de proteção long-term** (gate D3). Sem `fetch-depth: 0`, gate vira ruído e D3 fica letra-morta após primeira sessão de frustração.

Os 3 SHOULD TEST são improvements de robustez. Os 3 DOCUMENT são riscos aceitos conscientemente.

### Ações requeridas antes do plano ser final

1. **T2.3** — adicionar sub-task T2.3.a (master-sync ordering) e T2.3.b (portable base64 / CI matrix)
2. **T2.2** — adicionar nota explícita sobre `needs:` precisar estar na mesma helmfile invocation
3. **T1.1** — adicionar sub-task T1.1.a (dynamic-invocation grep antes de validation)
4. **T0.1** — adicionar tests `TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead` e `TestScanRepo_HandlesSymlinks_NoCycle` ao TDD
5. **T6.2** — adicionar acceptance criterion "CI job usa `actions/checkout@v4` com `fetch-depth: 0`" e test `TestCheck_ShallowClone_FailsWithClearError` + `TestCheck_RenamedBashOutsideAllowlist_Blocked`
6. **T5.1** — adicionar test `TestAutoAPI_StackNotFound_TypedError`
7. **DOCUMENT section** — adicionar nota EC-09 em `docs/operations/secret-rotation.md` (a criar) e EC-10 em `dev-loop.md` (a criar em T3.1)

Após esses ajustes, o plano deve ser bumped para v1.1 e re-revisar via `/cross-validation` no fim da implementação.
