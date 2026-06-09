# Edge Case Review — 2026-05-23-dashboard-loopzero-ui-migration

**Data:** 2026-05-23
**Plano:** `docs/plans/2026-05-23-dashboard-loopzero-ui-migration-plan.md`
**Tasks analisadas:** 23 (T0.1-T0.3, T1.1-T1.4, T2.1-T2.3, T3.1-T3.5, T4.1-T4.5, T5.1-T5.3, T6.1-T6.6)
**Edge cases encontrados:** 18 (MUST FIX: 8, SHOULD TEST: 7, DOCUMENT: 3)

> **Disclaimer honesto**: Os 3 primeiros MUST FIX (EC-1, EC-2, EC-3) são bugs que eu mesmo introduzi ao escrever o plano. A revisão verificou os exports reais de `@loopzerokit/ui` e a presença de `dark` class no DOM via grep, confirmando que assumi APIs que não existem ou estratégia de tema que não funciona como descrito.

---

## MUST FIX (8)

### EC-1: `@loopzerokit/ui` não exporta JS preset para Tailwind v3

- **Task afetada:** T0.3
- **Família:** Integration / Format
- **Cenário:** T0.3 propõe `import preset from '@loopzerokit/ui/preset'` e `presets: [preset]` em `tailwind.config.ts`. Verificação via `grep -E '"./preset"' loopzero-ui/package.json` retornou **vazio**. Os únicos exports `preset` são CSS files (`./preset.css` mapeado para `./dist/preset.css`). Tailwind v3 `presets: []` espera **objeto JS**, não CSS file.
- **Impacto:** `npm run build` quebra com erro `Cannot find module '@loopzerokit/ui/preset'` ou TypeScript error de import.
- **Fix sugerido:** Reescrever T0.3 — remover tentativa de Tailwind JS preset. O design system flui via CSS variables de `tokens.css` + `styles.css` (importados em T0.2). Manter `tailwind.config.ts` local mínimo com `content` paths + `theme.extend.colors` cobrindo tokens não-cobertos pelas CSS vars. Em Phase 6 (T6.2), remover tokens duplicados pelas CSS vars.

### EC-2: `darkMode: 'class'` + remoção parcial do `dark` class quebra Tailwind dark utilities

- **Task afetada:** T0.2
- **Família:** State / Integration
- **Cenário:** T0.2 task 3 remove `class="dark"` apenas de `<html>` (index.html), mas grep mostrou que o class também está em 2 `<div>`s do `app.tsx` (linhas 25 e 34). Tailwind config tem `darkMode: 'class'` — depende de algum ancestor ter `.dark` class para utilities `dark:*` funcionarem. Se `ThemeProvider` de `@loopzerokit/ui` setar tema via `data-theme="..."` em vez de adicionar `.dark` class, todas as utilities `dark:bg-surface-0` etc. param de funcionar mesmo com tema "dark" ativo.
- **Impacto:** UI carrega sem cores corretas (text-white some, backgrounds ficam transparentes). Quebra visual silenciosa.
- **Fix sugerido:** ANTES de T0.2, ler `loopzero-ui/src/themes/theme-provider.tsx` para confirmar a estratégia. Duas opções compatíveis: (a) configurar `darkMode: ['class', '[data-theme*="dark"]']` para Tailwind reconhecer ambos OU (b) garantir que ThemeProvider adiciona `.dark` class no mode dark. Atualizar T0.2 task 3 para remover `class="dark"` dos 3 sites (html + 2 divs em app.tsx) E adicionar verificação de que ThemeProvider seta class compatível.

### EC-3: Plan não captura baseline de bundle ANTES de T0.1

- **Task afetada:** T6.3 (depende de pre-step ausente)
- **Família:** State / Timing
- **Cenário:** T6.3 valida bundle size ±10% mas plano não inclui pre-step para capturar baseline ANTES da primeira mudança. Sem baseline gravada, "delta ±10%" é não-verificável.
- **Impacto:** T6.3 não pode ser executada com rigor; gate de aceitação fica vazio.
- **Fix sugerido:** Adicionar T0.0 ao plano como pre-step: `cd cloud/dashboard && npm run build && du -sh dist/assets/*.js > /tmp/dashboard-bundle-baseline.txt && git rev-parse HEAD >> /tmp/dashboard-bundle-baseline.txt`. T6.3 compara contra esse arquivo.

### EC-4: OIDC vanilla DOM `form.submit()` em LoginPage MUST ser preservado verbatim

- **Task afetada:** T3.1
- **Família:** State / Integration / Format
- **Cenário:** `login.tsx:75-94` cria `<form>` element via `document.createElement`, popula com 3 hidden inputs (`csrf_token`, `method=oidc`, `provider=github|google`), e dispara `form.submit()`. Esse é um **browser-level form POST** (não fetch) — Kratos exige isso porque o redirect 302 do OIDC provider precisa ser seguido pelo browser, não pelo fetch API. Substituir por `<Button onClick={() => fetch(action, {method:'POST', body:...})}>` quebra silenciosamente: fetch retorna o redirect mas browser nunca navega para GitHub/Google.
- **Impacto:** Login OIDC quebra. Usuários ficam presos em `/login` após clicar GitHub/Google.
- **Fix sugerido:** Adicionar ao T3.1 task 2 nota explícita: "Preservar `submitOIDC` HANDLER VERBATIM (createElement form + form.submit). Apenas o `<Button>` que dispara o handler é substituído. NÃO substituir form.submit por fetch."

### EC-5: `ProjectCard` composite pode requerer fields opcionais no shape atual

- **Task afetada:** T4.2
- **Família:** Format / Integration
- **Cenário:** Interface local `Project` (projects.tsx:9-15) tem fields `apps?`, `last_deploy?`, `environment?`, `created_at?` — todos opcionais. `ProjectCard` em `@loopzerokit/ui` pode exigi-los como required (status, deployedAt, branch, env). Se requerido e endpoint retornar undefined, ProjectCard pode renderizar "undefined" visivelmente ou crashar via TypeScript type error em build.
- **Impacto:** Página `/projects` quebra em build OR renderiza "undefined" para projetos sem apps/last_deploy.
- **Fix sugerido:** T4.2 task 1 atualizar para "Ler tipo `ProjectCardProps` em `node_modules/@loopzerokit/ui/dist/index.d.ts`. Para cada field obrigatório que o backend não retorna, definir default no adapter (ex: `last_deploy ?? 'never'`, `environment ?? 'production'`)."

### EC-6: Remoção de tokens Tailwind locais (T6.2) sem validação que ninguém os usa

- **Task afetada:** T6.2
- **Família:** Boundary / State
- **Cenário:** T6.2 remove `theme.extend.colors.loopzero`/`surface` de `tailwind.config.ts`. Se qualquer página/componente ainda tiver `className="bg-loopzero-500"` ou `className="bg-surface-2"`, a class fica inválida (Tailwind a remove do bundle) e elemento perde a cor — visual regression silenciosa.
- **Impacto:** Páginas perdem cor de fundo/borda; aparência quebrada sem erro de build.
- **Fix sugerido:** T6.2 task 1 reescrever para "ANTES de remover tokens, `grep -rE 'loopzero-[0-9]|surface-[0-4]' cloud/dashboard/src/ --include='*.tsx'` DEVE retornar zero matches. Se retornar, mapear cada ocorrência para token equivalente em `@loopzerokit/ui` (ex: `surface-0` → token-do-design-system) e fixar nas páginas ANTES de deletar."

### EC-7: Screens importados de `@loopzerokit/ui` podem conter termos internos (Rule 3 violation)

- **Task afetada:** T3.1, T3.2, T3.3, T3.4 (todas que importam patterns de `@loopzerokit/ui/screens`)
- **Família:** Integration / Format
- **Cenário:** `@loopzerokit/ui` é framework-agnostic e seus `screens/login.stories.tsx`, `chat-home.stories.tsx`, etc. podem incluir microcopy genérica ("Sign in with your provider") ou até mesmo references a "Mem0", "agent runtime", "MCP" — termos do mundo agent que NÃO são apropriados para um dashboard de PaaS. Rule 3 do CLAUDE.md proíbe vocabulário interno.
- **Impacto:** Smoke test T6.5 pode passar (não detecta `Kubernetes|ArgoCD|cell`), mas dashboard fica com strings "agent-shaped" inadequadas ao contexto LoopZeroCloud PaaS.
- **Fix sugerido:** Adicionar ao T6.5 nota: "Smoke test estende grep para incluir termos out-of-context: `grep -E 'Mem0|MCP|agent runtime|tool call|skill'` no DOM rendered. Se aparecer, customizar children/labels nos componentes importados (eles aceitam props customizáveis)."

### EC-8: Plano marca `@loopzerokit/ui@next` mas T0.1 task 1 usa tag móvel

- **Task afetada:** T0.1
- **Família:** State / Timing
- **Cenário:** T0.1 task 1 prescreve `npm install @loopzerokit/ui@next` — tag `@next` é móvel, novas versões podem ser publicadas durante o plano (a versão atual `0.6.3-next.0` pode virar `0.6.4-next.0` mid-implementation). Diferença de minor em pre-release pode trazer breaking change não-anunciado. Notas operacionais do plano mencionam "Pin exata versão em package.json até completar" mas T0.1 task 1 não enforce.
- **Impacto:** Mid-plan, alguém roda `npm install` e pega versão diferente do que outros devs têm. Dashboard renderiza inconsistentemente entre devs.
- **Fix sugerido:** T0.1 task 1 reescrever para `npm install @loopzerokit/ui@0.6.3-next.0 --save-exact` (pin exato). Em Phase 6 (T6.1 ou nova T6.7), trocar para `^` permitindo atualizações.

---

## SHOULD TEST (7)

### EC-9: Replace-all de imports pode deixar dangling refs

- **Task afetada:** T1.1, T1.2, T1.3
- **Teste sugerido:** `test_no_legacy_ui_imports` em `cloud/dashboard/tests/no-legacy-imports.test.ts` — `grep -r "from '@/components/ui/" src/ | wc -l` deve retornar 0 ao final da Phase 1.

### EC-10: Focus rings + keyboard navigation preservation após Button swap

- **Task afetada:** T1.1
- **Teste sugerido:** `test_button_keyboard_focusable` — renderiza `<Button>`, dispatcha `Tab` keyboard event, asserta `document.activeElement === button`. Se `@loopzerokit/ui` Button não tiver focus ring visível, a11y regression.

### EC-11: Multi-source error merging em LoginPage

- **Task afetada:** T3.1
- **Teste sugerido:** `test_login_displays_all_error_sources` — mock flow com `ui.messages=[flowErr]`, `nodes.find(password).messages=[pwErr]`, `nodes.find(identifier).messages=[idErr]`, asserta que pelo menos UM erro renderiza (não silenciosamente perde os 3).

### EC-12: CSRF token extraction via FormField

- **Task afetada:** T3.1, T3.2, T3.3, T3.4
- **Teste sugerido:** `test_kratos_form_preserves_csrf` — mock flow com `csrf_token` em nodes, submit form, asserta que POST body inclui `csrf_token=<value>`. Se `FormField` filtra hidden inputs, CSRF some e Kratos rejeita.

### EC-13: BillingPage 259 → ≤140 LOC pode strip lógica condicional

- **Task afetada:** T4.4
- **Teste sugerido:** `test_billing_renders_upgrade_cta_when_at_cap` — mock billing data com `usage = cap`, asserta CTA "Upgrade plan" visível. Se cut agressivo eliminar essa branch, usuários no cap não veem como upgradar.

### EC-14: `DeploymentRow` composite signature divergence

- **Task afetada:** T4.3
- **Teste sugerido:** `test_environments_renders_deployment_with_partial_data` — mock deployment com `branch=undefined`, asserta render sem crash. Se DeploymentRow requer branch, environments incompletos quebram.

### EC-15: Bundle bloat por duplicação Radix após remoção de deps locais

- **Task afetada:** T6.1
- **Teste sugerido:** `test_no_duplicate_radix_in_bundle` — após `npm install`, `npm ls @radix-ui/react-slot` deve retornar UMA versão (não duas). Se duas versões coexistirem (uma via `@loopzerokit/ui`, outra residual local), bundle infla. Ou usar `npm dedupe` como parte da task.

---

## DOCUMENT (3)

### EC-16: ThemeProvider localStorage persistence com tema único

- **Risco aceito:** ThemeProvider geralmente persiste tema escolhido em `localStorage` para sobreviver reloads. Com tema único violet-forge dark, isso é redundante mas não prejudicial. Single-theme plan (D1) significa que mesmo se localStorage tiver `aurora-terminal` salvo de teste antigo, `defaultTheme="violet-forge"` no provider força. Ainda assim, é digno de documentação: usuários migrando de teste local para produção podem ver fração de segundo de tema "errado" se ThemeProvider ler localStorage antes de aplicar default. Aceitável — fixar requer `<ThemeScript>` no head (já mencionado como follow-up em T0.2 deep dive).

### EC-17: Decorações da LoginPage atual (blur background) podem ser perdidas

- **Risco aceito:** `login.tsx:161` tem `<div className="absolute top-1/4 -left-32 w-96 h-96 bg-accent/[0.04] rounded-full blur-[120px]" />` — efeito decorativo puramente visual. Refactor para usar `screens/login` pattern de `@loopzerokit/ui` pode não incluir esse blur. Sem perda funcional. Se desejarmos restaurar, é trivial (1 div). Documentar como "intencionalmente simplificado" em commit message de T3.1.

### EC-18: Páginas órfãs `account.tsx`, `error.tsx` permanecem com visual antigo

- **Risco aceito:** D6 do plano formaliza que essas páginas não são refatoradas. Após plano concluído, dashboard terá inconsistência visual onde elas forem acessíveis. Aceitável porque NÃO são roteadas em produção. Se um dia forem reativadas, abrir mini-plano de follow-up `2026-XX-XX-dashboard-orphan-pages-cleanup-plan.md`.

---

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Sim (no plano) | EC-1 (preset JS não existe), EC-2 (dark class strategy) — bugs no próprio plano |
| Correct code in wrong place | Não | — |
| Project name vs project ID | Não (escopo UI, não toca essa camada) | — |
| ArgoCD notifiers not services | N/A | Plano não toca ArgoCD |
| Single ArgoCD App per tenant | N/A | Plano não toca ArgoCD |
| CF scan imports conflicting apex | N/A | Plano não toca CF |
| External lib API drift | Sim | EC-5 (ProjectCard), EC-14 (DeploymentRow), EC-12 (FormField CSRF) — `@loopzerokit/ui` é dependência externa pré-release |
| Cache invalidation timing | Não | Tailwind preset não tem cache layer relevante |

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.0 (NEW pre-step) | 1 | 1 (EC-3) | 0 | 0 |
| T0.1 | 1 | 1 (EC-8) | 0 | 0 |
| T0.2 | 1 | 1 (EC-2) | 0 | 1 (EC-16) |
| T0.3 | 1 | 1 (EC-1) | 0 | 0 |
| T1.1 | 1 | 0 | 2 (EC-9, EC-10) | 0 |
| T1.2 | 0 | 0 | 1 (EC-9) | 0 |
| T1.3 | 0 | 0 | 1 (EC-9) | 0 |
| T1.4 | 0 | 0 | 0 | 0 |
| T2.1 | 0 | 0 | 0 | 0 |
| T2.2 | 0 | 0 | 0 | 0 |
| T2.3 | 0 | 0 | 0 | 0 |
| T3.1 | 3 | 1 (EC-4) | 2 (EC-11, EC-12) | 1 (EC-17) |
| T3.2-T3.5 | 1 | 0 | 1 (EC-12) | 0 |
| T4.1 | 0 | 0 | 0 | 0 |
| T4.2 | 1 | 1 (EC-5) | 0 | 0 |
| T4.3 | 1 | 0 | 1 (EC-14) | 0 |
| T4.4 | 1 | 0 | 1 (EC-13) | 0 |
| T4.5 | 0 | 0 | 0 | 0 |
| T5.x | 0 | 0 | 0 | 0 |
| T6.1 | 1 | 0 | 1 (EC-15) | 0 |
| T6.2 | 1 | 1 (EC-6) | 0 | 0 |
| T6.5 | 1 | 1 (EC-7) | 0 | 0 |
| Cross-cutting (orphans) | 1 | 0 | 0 | 1 (EC-18) |

**Total**: 18 edge cases (8 MUST FIX, 7 SHOULD TEST, 3 DOCUMENT)

---

## Veredicto

**PLANO PRECISA DE AJUSTE** — 8 MUST FIX itens precisam ser incorporados antes da implementação começar.

### Ajustes obrigatórios no plano

1. **Adicionar T0.0 (NEW pre-step)**: capturar baseline bundle ANTES de T0.1 (EC-3)
2. **Reescrever T0.1 task 1**: pinar versão exata `@loopzerokit/ui@0.6.3-next.0 --save-exact` (EC-8)
3. **Reescrever T0.2 task 3**: validar estratégia do ThemeProvider + Tailwind `darkMode: 'class'`, remover `class="dark"` dos 3 sites (html + 2 divs em app.tsx), não apenas html (EC-2)
4. **Reescrever T0.3 inteira**: remover tentativa de Tailwind JS preset (não existe); design system flui via CSS imports de T0.2 + tokens locais reduzidos em T6.2 (EC-1)
5. **Adicionar à T3.1 task 2**: nota explícita "preservar OIDC vanilla DOM `form.submit` verbatim" (EC-4)
6. **Atualizar T4.2 task 1**: ler tipo `ProjectCardProps`, adapter com defaults para fields opcionais (EC-5)
7. **Reescrever T6.2 task 1**: pre-validar grep zero matches antes de remover tokens (EC-6)
8. **Adicionar à T6.5**: smoke test grep estendido para termos agent-shaped fora de contexto (EC-7)

### Recomendações não-bloqueantes

- Adicionar os 7 testes SHOULD TEST aos TDD sections respectivos
- Documentar os 3 risks aceitos em notas operacionais do plano

### Próximo passo

O plano principal (`docs/plans/2026-05-23-dashboard-loopzero-ui-migration-plan.md`) deve ser **bumped para v1.1** com os 8 fixes incorporados antes de invocar `/implement` ou começar Phase 0. Esta revisão deveria ser citada no commit message do bump (`Co-discovered-by: /edge-case-plan EC-1 through EC-8`).

A boa notícia: 3 dos 8 MUST FIX são bugs no próprio plano (EC-1, EC-2, EC-3) e foram pegos ANTES de qualquer linha de código ser escrita. Esse é exatamente o propósito do `/edge-case-plan`. Os 5 outros são edges reais da migração para uma dependência externa pré-release.
