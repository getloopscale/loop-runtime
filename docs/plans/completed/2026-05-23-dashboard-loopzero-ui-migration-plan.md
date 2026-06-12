# Plan: Dashboard LoopZeroCloud → @loopzero/ui Migration

> **Version 1.3** — Refactor completo do `cloud/dashboard/` (SPA React 19 + Vite 6) para consumir `@loopzero/ui@0.9.0-next.0` (Violet Forge dark) como design system canônico, **incluindo upgrade inline de Tailwind v3 → v4** (T0.0.bis). Substitui ~70% do código UI hand-rolled por composites/primitives testados + moderniza stack CSS para CSS-first config canônica de Tailwind v4. Preserva camadas críticas (auth Kratos→JWT, BroadcastChannel cross-tab, runtime config Twelve-Factor, mesma origem MEET-040). Resultado esperado: UX consistente, **~1.500 LOC eliminadas (-49%)**, stack moderna sem dívida técnica, identidade visual coerente com ecossistema LoopZero, gate "polish visual GA-ready" do Sprint 9 destravado.

## Prerequisite (RESOLVIDO 2026-05-23 — Opção A ratificada pelo sponsor)

**Decisão**: Tailwind v4 upgrade INLINE neste plano via nova task **T0.0.bis** (entre T0.0 e T0.1). Razão sponsor (paulo): "não precisamos de retrocompatibilidade, faça o que for mais FAANG". Coerente com pre-release bias do `.claude/rules/pre-release-governance.md` § "Pre-Release Bias" (preferir redesign limpo sobre patches incrementais).

Alternativas rejeitadas e por quê:

| Opção | Rejeitada porque |
|---|---|
| ~~B — Bridge `preset-v3-legacy` + `--legacy-peer-deps`~~ | Produz dívida técnica imortal; "code smell aceito" vira "código permanente"; team termina fazendo upgrade de qualquer jeito em 6 meses, com mais accretion em cima da bridge |
| ~~C — Plano separado de Tailwind v4 antes deste~~ | Atrasa Sprint 9 sem ganho real; complexidade não justifica split em 2 planos |

**Status:** RESOLVIDO ✅ — T0.0.bis adicionada à Phase 0; Phase 0 pode iniciar quando este plano for aprovado.

## Changelog

- **v1.4 (2026-05-24)** — Phases 2-5 marcadas SUBSUMED pelo plano sucessor `2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md v1.2` (executado nas iterações ralph loop). Validação T-H.4 do plano sucessor disparou este bump:
  - **Phase 2 (layout shell)**: SUBSUMED — T2.1 substituída por T-C.1 v1.2 (AppSidebar), T2.2 substituída por T-B.2 v1.2 (DashboardLayout + PageMetaProvider), T2.3 substituída por T-E.5 v1.2 (Alert primitive)
  - **Phase 3 (auth pages)**: SUBSUMED — T3.1-T3.5 substituídas por T-G.1-T-G.5 v1.2 (5 commits)
  - **Phase 4 (core pages)**: SUBSUMED — T4.1-T4.5 substituídas por T-D.1 + T-E.1 + T-E.2 + T-E.3 + T-E.4 v1.2
  - **Phase 5 (secondary pages)**: SUBSUMED — T5.1-T5.3 substituídas por T-F.1-T-F.3 v1.2
  - **Phase 1 ✅ DONE** durante v1.2 execution (commits 6c849f07, 0a992944, 09bbbb17, 4ec9f571 etc.)
  - **Phase 6 PERMANECE pendente** — agora deve rodar APÓS v1.2 visual redesign para fazer cleanup (T6.1 remove deps redundantes per D-3 v1.2, T6.2 prune local @theme tokens, T6.3 final bundle measure agora avalia estado pós-redesign não pós-Phase-5)

- **v1.3 (2026-05-23 23:55 UTC)** — Sponsor ratificou Opção A do prerequisite (Tailwind v4 inline). Mudanças:
  - **Prerequisite RESOLVIDO** — seção atualizada, opções B/C documentadas como rejeitadas com razão
  - **Nova T0.0.bis**: Tailwind v3 → v4 upgrade (entre T0.0 e T0.1). Inclui codemod oficial `@tailwindcss/upgrade`, nova dep `@tailwindcss/vite`, reescrita `tailwind.config.ts` → CSS-first `@theme {}` em `index.css`, migração `darkMode: 'class'` strategy
  - **T0.1 ajustada**: removida nota "PREREQUISITE BLOQUEADOR" (agora resolvido por T0.0.bis); install command segue `@loopzero/ui@0.9.0-next.0`
  - **T0.3 simplificada**: removida menção a `preset-v3-legacy` (não usamos); design system flui via `@loopzero/ui/preset` (Tailwind v4 canônico)
  - **Nova ADR D8**: Tailwind v4 inline upgrade — rationale + consequences documentadas
  - **Edge-Case-Review status**: re-run RECOMENDADO antes de implementação (v1.3 introduz nova task funcional T0.0.bis; v1.1 EC review não cobre)

- **v1.2 (2026-05-23 23:50 UTC)** — Bump para refletir delivery completa upstream de `@loopzero/ui`:
  - **Versão pinned**: `0.6.3-next.0` → `0.9.0-next.0` (3 minor releases shipados no mesmo dia: 0.7.0 Brief #1 + 0.8.0 Brief #2 + 0.9.0 Brief #3 = 14 primitives novos, 116 componentes totais)
  - **ThemeProvider API**: corrigida de `<ThemeProvider defaultTheme="violet-forge" defaultMode="dark">` (que NÃO existe) para `<ThemeProvider initial="violet-forge">` (canônica per llms.txt — temas encodam mode internamente)
  - **Taxonomy nota**: 4 de 12 specced "primitives" viraram composites upstream (UsageMeter, AccountMenu, CodeBlock, ConfirmDialog) — taxonomy gate hard-fail correto (importam outros componentes). Zero impacto consumer-side; barrel import unchanged
  - **Prerequisite Tailwind v4**: adicionada seção bloqueadora acima — peer-dep obrigatório descoberto via llms.txt
  - **LOC target revisado**: 1.800 → 1.500 (-49% vs -37% original) porque StatusDot/Timestamp/Table/DangerZone/CopyButton/CodeBlock/Alert deletam mais boilerplate que estimado
  - **Fontes mudam**: dashboard atual usa DM Sans + JetBrains Mono + Cabinet Grotesk; `@loopzero/ui` locked em Geist Sans + Geist Mono. Adoção implica mudança visual real (não cosmética)

- **v1.1 (2026-05-23)** — Incorpora 8 MUST FIX do `/edge-case-plan` review (`docs/plans/2026-05-23-dashboard-loopzero-ui-migration-edge-cases.md`):
  - **EC-1**: T0.3 reescrita — `@loopzero/ui` não exporta JS preset; design system flui via CSS imports
  - **EC-2**: T0.2 reescrita — remover `class="dark"` dos 3 sites (html + 2 divs em app.tsx), validar estratégia do ThemeProvider × Tailwind `darkMode: 'class'`
  - **EC-3**: Nova T0.0 (pre-step) — capturar baseline bundle ANTES de qualquer mudança
  - **EC-4**: Nota explícita em T3.1 — preservar OIDC vanilla DOM `form.submit` verbatim
  - **EC-5**: T4.2 — ler `ProjectCardProps`, adapter com defaults para fields opcionais
  - **EC-6**: T6.2 — pre-validar grep zero matches antes de remover tokens locais
  - **EC-7**: T6.5 — smoke test grep estendido para termos "agent-shaped" fora de contexto
  - **EC-8**: T0.1 — pinar versão exata `@loopzero/ui@0.6.3-next.0 --save-exact`

## Context

### O que existe hoje

- `cloud/dashboard/src/` tem **2.914 LOC** em 32 arquivos `.tsx/.ts`
- **15 páginas** em `pages/` (12 roteadas + 3 órfãs: `account.tsx`, `error.tsx`, `recovery.tsx` — esta última referenciada por `<Link to="/recovery">` em `login.tsx:227` sem rota correspondente, bug latente)
- **3 primitivas locais** mínimas (`ui/badge.tsx`, `ui/button.tsx`, `ui/card.tsx`) com ~115 LOC — stubs shadcn-style
- **5 dependências Radix** instaladas individualmente (`react-avatar`, `react-dropdown-menu`, `react-separator`, `react-slot`, `react-tooltip`)
- **Framer Motion + Lucide React** importados manualmente em cada página com variants `container`/`item` repetidas
- **Vocabulário visual hand-rolled**: classes como `bg-white/[0.04] border border-white/[0.06] group-hover:border-accent/20 group-hover:bg-accent/5 transition-all duration-300` repetidas em ≥6 páginas; SVG do Google logo inline em `login.tsx`; divider via `<div className="relative my-6">` em 2 lugares

### O que motiva o trabalho

- **Violação Regra 9 (Não Reinvente a Roda)**: `@loopzero/ui` foi criado precisamente para servir surfaces como esta. Composites diretamente aplicáveis: `DeploymentRow`, `ProjectCard`, `EnvVarEditor`, `DomainConfig`, `PreviewEnvCard`, `RollbackUI`, `CommandPalette`, `PermissionModal`. Primitives: `Avatar`, `Badge`, `Button`, `Card`, `Dialog`, `EmptyState`, `FormField`, `Input`, `Checkbox`, `AuditLogEntry`, `BuildLogStream`, `CostMeter`.
- **Sprint 9 (Polish + Launch)**: gate (a) requer "polish visual GA-ready". Dashboard atual tem inconsistências visuais entre páginas (cada uma com Tailwind ad-hoc).
- **`@loopzero/ui@next` já publicado**: `0.9.0-next.0` em npm, install limpo. **116 componentes** (89 primitives + 27 composites — todos os 14 specced via Briefs #1/#2/#3 incorporados), **1.577 testes** passando, **237 stories** axe-validadas, peer-deps `react` + `react-dom` + `tailwindcss ^4.0.0`.
- **Coerência de ecossistema**: LoopZeroCloud é o consumidor-alvo da auxiliary "Build" (UI) descrita em `loopzero-ui/PITCH.md`. Fechar esse loop estratégico foi planejado desde a criação de `@loopzero/ui`.

### Evidência concreta

- `cloud/dashboard/src/pages/overview.tsx:47-67` — 21 linhas de classes Tailwind hand-rolled para 1 stat card; `StatsTile` em `@loopzero/ui` faz em 3
- `cloud/dashboard/src/pages/login.tsx:200` — SVG do Google logo inline com 4 paths; `@loopzero/ui` tem `<SocialAuthButton provider="google">` (parte de `screens/login`)
- `cloud/dashboard/src/pages/projects.tsx:51-88` — 38 linhas de motion + card + badge ad-hoc por projeto; `ProjectCard` composite faz em 1 linha
- `cloud/dashboard/src/components/ui/{badge,button,card}.tsx` — 115 LOC reescrevendo o que `@loopzero/ui` já expõe via barrel

### Inventário de @loopzero/ui (verificado via `ls loopzero-ui/src/components/{primitives,composites}`)

```
primitives/ (81): agent-error-card, agent-event, agent-handoff, agent-profile, agent-starting-state,
                  agent-streaming, artifact-preview, attachment-chip, audit-log-entry,
                  auto-compact-notice, avatar, badge, browser-controls, build-log-stream, button,
                  capability-indicator, card, chat-thread, checkbox, context-card,
                  context-window-bar, cost-meter, created-files-card, cron-job-card, dialog,
                  diff-viewer, empty-state, folder-context-card, folder-selector, form-field,
                  hook-config, hook-event-log, input, ... (+49 outros)

composites/ (21): agent-composer, agent-editor, agent-stream, agent-timeline, approval-card,
                  chat-composer, chat-message, command-palette, cron-jobs-list, deployment-row,
                  domain-config, env-var-editor, mcp-server-list, permission-modal,
                  preview-env-card, preview-panel, project-card, rollback-ui, rule-editor,
                  skill-editor, skills-list, slide-deck, task-header

screens/: login.stories.tsx, chat-home.stories.tsx, task-completed.stories.tsx,
          task-running.stories.tsx, code-workspace.stories.tsx, loopzero-code-shell.tsx
```

## Objective

**Done = todas as 12 páginas roteadas do dashboard renderizam usando primitives/composites de `@loopzero/ui` no tema Violet Forge dark, com auth flow intacto, build de produção verde, vitest verde, smoke test manual nas 12 páginas sem regressão visual percebida, bundle size dentro de ±10% do atual, e zero termos internos na UI (Rule 3).**

Metas mensuráveis:

1. **LOC reduzidas**: `cloud/dashboard/src/` cai de ~2.914 para ≤1.800 LOC (corte ≥37%)
2. **Dependências consolidadas**: 5 pacotes Radix individuais → 1 dependência `@loopzero/ui`
3. **Primitivas locais eliminadas**: `src/components/ui/` deletado (3 arquivos)
4. **Coerência visual**: 12 páginas compartilham mesmas primitives (StatsTile, Card, Button, FormField, EmptyState, Badge, Avatar)
5. **Tema único**: Violet Forge dark via `<ThemeProvider defaultTheme="violet-forge" defaultMode="dark">` em `main.tsx`, sem toggle
6. **Acessibilidade**: vitest-axe passa para cada página (mirror do gate `@loopzero/ui`)
7. **Bug bonus**: rota `/recovery` adicionada ao `app.tsx` (corrige link quebrado em `login.tsx:227`)

## ADRs

### D1 — Tema único Violet Forge dark (sem toggle multi-tema)

- **Decisão**: O dashboard renderiza apenas Violet Forge dark. Sem `<ThemeSwitcher>`, sem suporte a Classic Paper ou Aurora Terminal.
- **Rationale**: Decisão explícita do sponsor (paulo, 2026-05-23). Reduz escopo (sem testes em 3 temas), reduz superfície de a11y a validar, reduz risco de contraste degradado em light mode. Coerência com PITCH.md ("AI workspace energy" do Violet Forge alinha com LoopZeroCloud-as-agent-runtime).
- **Consequences**:
  - ✅ Escopo de teste 3× menor
  - ✅ Decisão visual estável, sem A/B
  - ❌ Usuários light-mode-only ficam sem alternativa (aceito; pode ser adicionado em sprint futuro via T-NEW)
  - 🔒 Trava: adicionar segundo tema requer novo plano T2

### D2 — Install via `npm install @loopzero/ui@next` (não copy-paste shadcn)

- **Decisão**: Pacote npm completo, importações via barrel `@loopzero/ui`. Sem `npx shadcn add` por componente.
- **Rationale**: Atualizações automáticas via Renovate; tree-shake nativo; alinhamento com peer-deps React 19; Apache-2.0 compatível com LoopZero. Copy-paste só faz sentido se quisermos modificar componentes localmente — não é o caso.
- **Consequences**:
  - ✅ Atualizações sob Renovate (CLAUDE.md INF-G18)
  - ✅ Bundle otimizado via tree-shake
  - ❌ Não posso modificar componentes; tenho que aceitar API upstream OU contribuir
  - 🔒 Dist-tag `@next` significa pré-release; aceitamos churn de minor versions até v1.0 GA

### D3 — Preservar `src/lib/{api,auth,kratos,config}.ts` sem modificação

- **Decisão**: Camada de dados/auth/runtime config **NÃO É TOCADA**. Apenas o que renderiza UI.
- **Rationale**: Camadas existentes são corretas, testadas, documentam incident-driven design (MEET-040, §3.3 BroadcastChannel, §3.6 toast antes de redirect, MEET-037 OIDC verified, etc.). Mexer aumenta blast radius sem benefício. Princípio: refactor visual ≠ refactor arquitetural.
- **Consequences**:
  - ✅ Zero regressão em auth/JWT/cross-tab/runtime config
  - ✅ Testes `auth.test.ts` (131 LOC) seguem verdes sem mudança
  - ❌ Limitação: se `@loopzero/ui` espera shape de Form X e nosso `kratos.ts` retorna Y, temos adaptador entre eles (aceito)

### D4 — Manter `framer-motion` e `lucide-react` como dependências diretas

- **Decisão**: Não remover Framer Motion nem Lucide React mesmo após migração. `@loopzero/ui` usa-os internamente como peer-deps; usamos para customização local quando primitiva não atende.
- **Rationale**: `@loopzero/ui` exporta primitives mas pode não cobrir TODOS os micro-momentos visuais (transição de página, ícone custom raro). Manter as duas libs reduz cantos vivos. Lucide é leve (~250KB tree-shaken para ~20 ícones).
- **Consequences**:
  - ✅ Flexibilidade preservada
  - ❌ Duas formas de motion (Framer direto + animations expostas via primitives) — mitigação: usar Framer só em telas não-cobertas

### D5 — Tailwind: usar preset de `@loopzero/ui`, manter `tailwind.config.ts` local mínimo

- **Decisão**: `@import "@loopzero/ui/tokens.css"` + `@import "@loopzero/ui/styles.css"` em `src/index.css`. `tailwind.config.ts` local mantém só extensões específicas do dashboard (se houver).
- **Rationale**: Tokens (cores `loopzero.*`, `surface.*`, `accent`, fontes display) ficam centralizados no design system. Mudanças de marca propagam sem edit local. Coerência cross-projeto.
- **Consequences**:
  - ✅ Branding mudou em `@loopzero/ui` → propaga
  - ❌ Tokens custom do dashboard (se necessário) precisam ser nomeados defensivamente para não colidir

### D6 — Não migrar `account.tsx`, `error.tsx`, `recovery.tsx` (páginas órfãs)

- **Decisão**: Páginas que existem em `pages/` mas não são roteadas em `app.tsx` recebem refactor **apenas se forem roteadas durante o plano**. `recovery.tsx` SERÁ roteada (corrige bug `Link to="/recovery"`); `account.tsx` e `error.tsx` ficam intocadas, marcadas para revisão pós-plano.
- **Rationale**: YAGNI. Refatorar código não-alcançável é desperdício. `recovery` é exceção porque o link já existe em produção.
- **Consequences**:
  - ✅ Escopo enxuto: 12 + 1 (recovery) = 13 páginas refatoradas, não 15
  - ❌ Inconsistência visual transitória em `account/error` (aceito — eles não são vistos em produção)

### D8 — Tailwind v3 → v4 upgrade INLINE neste plano (Opção A do Prerequisite)

- **Decisão**: Phase 0 inclui task T0.0.bis que migra dashboard de Tailwind v3 para v4 ANTES de instalar `@loopzero/ui`. Sem bridge `preset-v3-legacy`, sem `--legacy-peer-deps`.
- **Rationale**: `@loopzero/ui@0.9.0-next.0` requer `tailwindcss ^4.0.0` como peer-dep. Pre-release bias do `.claude/rules/pre-release-governance.md` favorece redesign limpo sobre patches. Sponsor (paulo, 2026-05-23) ratificou explicitamente: "não precisamos de retrocompatibilidade, faça o que for mais FAANG".
- **Consequences**:
  - ✅ Stack moderna desde início (CSS-first config, Lightning CSS, container queries built-in, source detection automática)
  - ✅ Sem dívida técnica acumulada por bridge
  - ✅ Caminho canônico documentado em llms.txt usa Tailwind v4
  - ✅ Codemod oficial `@tailwindcss/upgrade` automatiza ~80% da migração
  - ❌ Phase 0 cresce 1 task (T0.0.bis) — ~30% effort adicional
  - ❌ Risco de quebras visuais sutis (defaults mudam entre v3/v4 — line-height, ring-offset, etc.)
  - 🔒 Após upgrade, NÃO há caminho de volta sem refazer trabalho

### D7 — Testes: vitest + @testing-library/react para wiring; NÃO duplicar a11y/visual de `@loopzero/ui`

- **Decisão**: Cada página ganha um teste de wiring (renderiza, dispara fetch correto, mostra loading/empty/error states). NÃO testamos a11y das primitivas (`@loopzero/ui` já tem 126 stories axe-validadas) nem snapshots visuais (volatilidade alta).
- **Rationale**: KISS + Não Reinvente. Nossos testes asseguram que ligamos os tubos corretamente; o design system garante que os tubos funcionam.
- **Consequences**:
  - ✅ Suite de testes pequena e focada
  - ❌ Regressão visual só detectada por smoke test manual (aceito para Sprint 9 polish; pode ser endereçada com Chromatic/Playwright em sprint futuro)

## Dependency Graph

```
Phase 0: Setup
   │ T0.0      capture bundle baseline
   │ T0.0.bis  Tailwind v3 → v4 upgrade (NEW v1.3)
   │ T0.1      install @loopzero/ui@0.9.0-next.0
   │ T0.2      ThemeProvider initial="violet-forge" + CSS imports
   │ T0.3      validate Tailwind v4 + @loopzero/ui co-existence
   │
   ▼
Phase 1: UI Primitives Swap                ◄── desbloqueia tudo abaixo
   │ delete src/components/ui/{badge,button,card}.tsx
   │ replace all imports across 15+ files
   │
   ├─────────────┬──────────────┬──────────────┐
   ▼             ▼              ▼              ▼
Phase 2:    Phase 3:        Phase 4:       Phase 5:
Layout      Auth pages      Core pages     Secondary pages
Shell       (4 pages)       (5 pages)      (3 pages)
            (+ recovery)    │              │
            │               │              │
            └───────────────┴──────────────┘
                            │
                            ▼
                       Phase 6: Cleanup + Validation
                       │ remove old deps, bundle check, vitest, smoke test
                       │
                       ▼
                       Dogfood Gate
```

**Paralelização possível**: Phases 3, 4, 5 podem rodar em paralelo após Phase 1+2. Phase 6 é bloqueador final.

---

## Phase 0: Setup

**Objective:** Trazer `@loopzero/ui` para o projeto, configurar `ThemeProvider`, importar tokens/styles, ajustar Tailwind, sem quebrar build atual.

### T0.0 — Capture bundle baseline (NEW pre-step, addresses EC-3)

#### Objective
Capturar tamanho do bundle atual ANTES de qualquer mudança para que T6.3 possa comparar com rigor.

#### Evidence
- Sem baseline persistida, "delta ±10%" é não-verificável (EC-3)

#### Files to edit
```
docs/reviews/2026-05-23-dashboard-bundle-baseline.txt (NEW) — output do build atual
```

#### Tasks
1. `cd cloud/dashboard && npm run build`
2. Capturar: `du -sh dist/assets/*.js > docs/reviews/2026-05-23-dashboard-bundle-baseline.txt`
3. Anexar `git rev-parse HEAD` ao arquivo para rastreabilidade
4. Commitar baseline

#### Acceptance Criteria
- [ ] Arquivo `docs/reviews/2026-05-23-dashboard-bundle-baseline.txt` existe com tamanhos e commit hash
- [ ] Build verde

#### DoD
- [ ] Commit `chore(dashboard): capture bundle baseline before @loopzero/ui migration`

---

### T0.0.bis — Tailwind v3 → v4 upgrade (NEW v1.3, addresses Prerequisite Opção A, depends on T0.0)

#### Objective
Migrar dashboard de Tailwind v3.4 para v4.0 ANTES de instalar `@loopzero/ui@0.9.0-next.0`. Sem esta task, T0.1 falha com `EPEERINVALID` (peer-dep `tailwindcss ^4.0.0` requerido). Usa codemod oficial `@tailwindcss/upgrade` para automatizar ~80% do trabalho; resto é manual (estratégia darkMode, validação visual).

#### Evidence
- `@loopzero/ui@0.9.0-next.0` peerDependencies: `"tailwindcss": "^4.0.0"` (confirmado em llms.txt § Peer dependencies)
- Dashboard `package.json`: `"tailwindcss": "^3.4.0"` (atual)
- Sponsor ratificou Opção A em 2026-05-23: "não precisamos de retrocompatibilidade, faça o que for mais FAANG" → ADR D8
- Pre-release bias do `.claude/rules/pre-release-governance.md` § "Pre-Release Bias" favorece redesign limpo sobre patches incrementais

#### Files to edit
```
cloud/dashboard/package.json       — bump tailwindcss + adicionar @tailwindcss/vite
cloud/dashboard/vite.config.ts     — adicionar tailwindcss() ao plugins array
cloud/dashboard/tailwind.config.ts — DELETAR ou reduzir a stub mínimo (Tailwind v4 é CSS-first)
cloud/dashboard/postcss.config.js  — remover plugin tailwindcss (substituído por @tailwindcss/vite); manter apenas autoprefixer se ainda necessário OU deletar arquivo
cloud/dashboard/src/index.css      — substituir @tailwind directives por @import "tailwindcss"; mover theme.extend para @theme {} block
```

#### Deep file dependency analysis

- **`package.json`**: bump `"tailwindcss": "^3.4.0"` → `"tailwindcss": "^4.0.0"`. Adicionar `"@tailwindcss/vite": "^4.0.0"` em devDependencies (peer-dep opcional listado em `@loopzero/ui` per llms.txt).
- **`vite.config.ts`** (16 LOC atual): adicionar `import tailwindcss from '@tailwindcss/vite';` e incluir em `plugins`. Esse plugin substitui o PostCSS-based pipeline com Lightning CSS (~5x faster, native autoprefixing).
- **`tailwind.config.ts`** (62 LOC atual): Tailwind v4 é CSS-first. `theme.extend.colors` (paleta `loopzero.*`/`surface.*`) → `@theme {}` em `index.css`. Keyframes/animations idem para CSS. **Arquivo TS pode ser deletado** se `@tailwindcss/vite` auto-detecta content (default). Conservar apenas se houver content paths customizados.
- **`postcss.config.js`** (atual `module.exports = { plugins: { tailwindcss: {}, autoprefixer: {} } }`): plugin `tailwindcss` removido (agora via Vite plugin). Lightning CSS handle autoprefixing — `autoprefixer` redundante. **Arquivo provavelmente deletável**.
- **`src/index.css`**: substituir `@tailwind base/components/utilities` por `@import "tailwindcss";`. Em T0.2, este arquivo recebe `@import "@loopzero/ui/tokens.css"` + `@import "@loopzero/ui/styles.css"` adicionais. Tokens locais migram para `@theme {}` block (a serem prunados em T6.2 quando confirmado uso zero).

#### Deep Dives

- **Codemod oficial**: `npx @tailwindcss/upgrade@latest` automatiza ~80% — bumpa package.json, atualiza `@tailwind` directives, converte `tailwind.config.ts` extends para `@theme {}` block em CSS. **MAS** não migra darkMode strategy nem PostCSS config — esses são manuais.
- **Breaking changes a verificar** (não-exaustivo per Tailwind v4 changelog):
  - `text-base` line-height default mudou (1.5rem → 1.5)
  - `ring-offset-*` removido como utility default
  - Opacity shorthand `bg-opacity-XX` removido (use `bg-color/XX`)
  - Plugin API completamente novo (`@plugin` directive em CSS)
- **darkMode strategy em v4**: por default suporta `:where(.dark, .dark *)` ancestral. Combinado com T0.2 ThemeProvider que adiciona `.dark` automaticamente, o `class="dark"` manual dos 3 sites (T0.2 task 4-5) é redundante e será removido em T0.2.
- **Container queries built-in**: v4 ship sem plugin separado. Dashboard atual não usa, sem impacto.

#### Tasks

1. Capturar screenshot baseline visual das 13 rotas em Chrome dark mode (referência pós-upgrade)
2. Rodar codemod oficial: `cd cloud/dashboard && npx @tailwindcss/upgrade@latest`
3. Inspecionar diff resultante. Validar mudanças em `package.json`, `index.css`, `tailwind.config.ts`.
4. Adicionar `@tailwindcss/vite` se codemod não o fez: `npm install @tailwindcss/vite@^4 --save-dev`
5. Atualizar `vite.config.ts` para incluir o plugin:
   ```ts
   import { defineConfig } from 'vite';
   import react from '@vitejs/plugin-react';
   import tailwindcss from '@tailwindcss/vite';
   import path from 'path';

   export default defineConfig({
     plugins: [react(), tailwindcss()],
     resolve: { alias: { '@': path.resolve(__dirname, './src') } },
     build: { outDir: 'dist', sourcemap: false },
   });
   ```
6. Limpar `postcss.config.js` — remover plugin `tailwindcss`. Se restar só `autoprefixer`, deletar arquivo (Lightning CSS já handles autoprefixing).
7. Verificar `src/index.css` pós-codemod — deve ter `@import "tailwindcss";` no topo. Tokens locais devem estar em `@theme {}` block.
8. `npm run build` — DEVE compilar sem erro. Se falhar, ler erro do Vite plugin e ajustar.
9. `npm run dev` + smoke test visual: comparar com screenshots baseline (task 1). Esperado: paleta intacta, layout idêntico. Diferenças sutis aceitáveis (line-height base, default font weights).
10. Commit incremental: `chore(dashboard): upgrade tailwindcss v3.4 → v4.0 via official codemod`

#### TDD

```
RED: N/A — upgrade task; validação via build verde + smoke visual contra baseline screenshots
GREEN: npm run build exits 0; npm run dev abre; 13 rotas renderizam visualmente equivalente ao baseline
REFACTOR: ajustar tokens em @theme {} se codemod migrou subóptimo
VERIFY: npm run build && diff -r <baseline-screenshots> <new>  (diferenças apenas em line-height/font defaults)
```

#### Acceptance Criteria

- [ ] `package.json`: `"tailwindcss": "^4.0.0"` + `"@tailwindcss/vite": "^4.0.0"` em devDependencies
- [ ] `vite.config.ts`: importa `tailwindcss` de `@tailwindcss/vite` e inclui no `plugins` array
- [ ] `postcss.config.js`: NÃO referencia plugin `tailwindcss` (removido OU arquivo deletado)
- [ ] `tailwind.config.ts`: deletado OU reduzido a stub mínimo
- [ ] `src/index.css`: começa com `@import "tailwindcss";`
- [ ] `npm run build` exits 0 sem erros de tailwind/postcss
- [ ] 13 rotas em smoke test visual aparecem equivalente ao baseline (regressões sutis OK, grosseiras NÃO)
- [ ] Delta de bundle vs T0.0 baseline: aceitar +/-15% nesta etapa (Lightning CSS muda layout do output)

#### DoD

- [ ] Tasks 1-10 completas
- [ ] Build + smoke visual verdes
- [ ] Commit `chore(dashboard): upgrade tailwindcss v3.4 → v4.0 via official codemod`

#### Riscos identificados

| Risco | Mitigação |
|---|---|
| Codemod não cobre 100% | Diff manual no PR — qualquer mudança fora do escopo migrada à mão |
| `darkMode: 'class'` strategy quebra | T0.2 valida estratégia do ThemeProvider antes; se incompatível, ajustar CSS `@variant dark` em `index.css` |
| Plugins de Tailwind v3 não compatíveis | Dashboard atual NÃO usa plugins (verificado em tailwind.config.ts) — risco nulo |
| Lightning CSS diferente de PostCSS-autoprefixer em edge cases | Smoke test pega regressões visuais; reverter para autoprefixer no PostCSS se needed |
| Fontes Cabinet Grotesk no `@theme {}` não compatível | T0.2 substitui por Geist de qualquer jeito (D1); aceitar regressão tipográfica intermediária |

---

### T0.1 — Install `@loopzero/ui` (PINNED version, addresses EC-8)

#### Objective
Adicionar `@loopzero/ui@next` ao `package.json`, verificar peer-deps, atualizar `package-lock.json`.

#### Evidence
- `npm view @loopzero/ui version` retorna `0.1.0-next.0` (latest), `0.9.0-next.0` (next) — verificado em 2026-05-23 23:37 UTC
- `loopzero-ui/package.json` declara peer-deps apenas em `react ^19 || ^18` (compatível com nosso `^19.0.0`)

#### Files to edit
```
cloud/dashboard/package.json       — adicionar @loopzero/ui ao dependencies, remover Radix individuais
cloud/dashboard/package-lock.json  — regenerar via npm install
```

#### Deep file dependency analysis
- `package.json`: dependencies hoje tem 5 entries Radix + framer-motion + lucide-react + clsx + class-variance-authority + tailwind-merge. As Radix individuais e CVA podem ser removidas se `@loopzero/ui` re-exporta o equivalente. `clsx` + `tailwind-merge` ficam (usados localmente para classnames condicionais).
- `package-lock.json`: regen automático

#### Deep Dives
- **Peer-dep mismatch**: Se `@loopzero/ui` exigir React 18 e dashboard usa 19, npm warning. Verificar com `npm install --dry-run` antes de commit.
- **Dist-tag escolhido**: `@next` (não `@latest`) — `0.9.0-next.0` contém os 14 primitives entregues via Briefs #1/#2/#3 + os 102 originais; `latest=0.1.0-next.0` é stub pré-release
- **Tamanho**: `@loopzero/ui` declara bundle size budget ±5%; tree-shake via barrel deve manter delta razoável

#### Tasks
1. **PREREQUISITE**: T0.0.bis (Tailwind v4 upgrade) DEVE estar completa. Sem isso, `npm install @loopzero/ui` falha com `EPEERINVALID` (peer-dep `tailwindcss ^4.0.0` requerido).
2. `cd cloud/dashboard && npm install @loopzero/ui@0.9.0-next.0 --save-exact` (PINNED — EC-8). Tag `@next` é móvel; novas versões podem ser publicadas mid-plano.
3. Validar que `package.json` tem `"@loopzero/ui": "0.9.0-next.0"` (sem `^` nem `~`)
4. Auditar `package.json` para remover deps redundantes que `@loopzero/ui` traz transitivamente (manter `framer-motion`, `lucide-react`, `clsx`, `tailwind-merge` per D4). Confirmar via llms.txt § "Already bundled (do NOT install separately)" que Radix individuais (`@radix-ui/react-{avatar,dropdown-menu,separator,slot,tooltip}`) podem ser removidos — são bundled deps em `@loopzero/ui`
5. Rodar `npm install` para atualizar lock
6. Validar com `npm run build` que ainda compila (sem mudança de código ainda)
7. **Em Phase 6 (T6.1 ou nova T6.7)**: trocar para `^0.9.0-next.0` permitindo Renovate atualizações

#### TDD
```
RED:     N/A — install task, sem teste comportamental
GREEN:   `npm install` exits 0, `npm run build` exits 0 com warnings <= baseline
REFACTOR: None
VERIFY:  cd cloud/dashboard && npm install && npm run build && npm test
```

#### Acceptance Criteria
- [ ] `package.json` lista `@loopzero/ui: 0.9.0-next.0` (pinned exato — sem `^`) em dependencies
- [ ] `npm install` exits 0 sem ERR
- [ ] `npm run build` exits 0 (bundle ainda funciona, deps não-conflitantes)
- [ ] `npm test` (vitest) verde — `auth.test.ts` continua passando
- [ ] `package-lock.json` regenerado e commitado

#### DoD
- [ ] Tasks 1-4 completas
- [ ] Build verde
- [ ] Commit `feat(dashboard): install @loopzero/ui@next as design system foundation`

---

### T0.2 — Wire `ThemeProvider` + tokens.css + styles.css (REWRITTEN, addresses EC-2)

#### Objective
Envolver `<App />` com `<ThemeProvider defaultTheme="violet-forge" defaultMode="dark">`, importar CSS de design system, garantir que estratégia do ThemeProvider **coexista** com Tailwind `darkMode: 'class'` configurado em `tailwind.config.ts:4`.

#### Evidence
- `loopzero-ui/README.md` quickstart prescreve esse padrão
- `loopzero-ui/src/themes/violet-forge.ts` existe e está canônico
- Tailwind config tem `darkMode: 'class'` — depende de `.dark` class em algum ancestor
- **EC-2 confirmou**: `dark` class está em 3 sites (`index.html` linha 2, `app.tsx:25`, `app.tsx:34`) — não apenas em html
- Se ThemeProvider setar tema via `data-theme` SEM adicionar `.dark` class, todas as utilities `dark:bg-surface-0` etc. param de funcionar silenciosamente

#### Files to edit
```
cloud/dashboard/src/main.tsx       — envolver <App /> com <ThemeProvider>
cloud/dashboard/src/index.css      — importar tokens.css + styles.css de @loopzero/ui
cloud/dashboard/src/app.tsx        — remover className="dark" dos 2 divs (linhas 25 e 34)
cloud/dashboard/index.html         — remover class="dark" do <html>
cloud/dashboard/tailwind.config.ts — possivelmente ajustar darkMode para reconhecer estratégia do ThemeProvider
```

#### Deep file dependency analysis
- `main.tsx` (10 LOC) recebe wrapping; mudança trivial
- `index.css`: vai receber dois `@import` de `@loopzero/ui` no topo
- `app.tsx`: dois divs com `className="dark bg-surface-0 ..."` — manter classes funcionais (`bg-surface-0`, etc.) e remover `dark` apenas se ThemeProvider compensar
- `index.html`: remover `class="dark"` do `<html lang="en" class="dark">`
- `tailwind.config.ts`: se ThemeProvider usa `data-theme="violet-forge"` em vez de class, atualizar `darkMode: ['class', '[data-theme*="dark"]']` para Tailwind reconhecer ambos

#### Deep Dives
- **Critical pre-flight**: ANTES de executar tasks, ler `node_modules/@loopzero/ui/dist/themes/theme-provider.*` (após T0.1 install) para confirmar a estratégia exata. Três cenários possíveis:
  - **(A)** ThemeProvider adiciona `.dark` class ao `<html>` quando `defaultMode="dark"` → **nenhuma mudança no Tailwind config necessária**, só remover os 3 sites manuais
  - **(B)** ThemeProvider seta `data-theme="violet-forge"` E adiciona `.dark` class → mesmo caminho (A)
  - **(C)** ThemeProvider seta APENAS `data-theme` sem class → ajustar Tailwind para `darkMode: ['class', '[data-theme*="dark"]']`
- **FOUC**: SPAs Vite raramente sofrem FOUC porque CSS é inlined. Aceitar mínimo FOUC em primeira pintura — não bloqueador.
- **Single-theme**: passar `defaultMode="dark"` permanente; sem toggle UI (D1).

#### Tasks
1. Em `index.css`, adicionar como primeiras linhas:
   ```css
   @import "@loopzero/ui/tokens.css";
   @import "@loopzero/ui/styles.css";
   ```
2. Em `main.tsx`, importar `ThemeProvider` de `@loopzero/ui` e envolver. **API CORRIGIDA v1.2** per llms.txt: usa `initial="violet-forge"` (tema encode mode internamente — `violet-forge` JÁ é dark), NÃO `defaultTheme + defaultMode` (que não existe):
   ```tsx
   import { ThemeProvider } from '@loopzero/ui';
   <StrictMode>
     <ThemeProvider initial="violet-forge">
       <App />
     </ThemeProvider>
   </StrictMode>
   ```
   Considerar também adicionar `<Toaster />` no mesmo nível (já que `@loopzero/ui` exporta `Toast + Toaster + useToast` per inspeção de `primitives/toast/`) para substituir o hack inline em `lib/api.ts:108-111` que cria `<div>` manualmente para notificação de session expired.
3. **Pre-flight**: ler `node_modules/@loopzero/ui/dist/themes/theme-provider.*` (TS declaration ou JS source) para identificar qual cenário (A/B/C) aplica
4. Remover `class="dark"` de `<html>` em `index.html`
5. Remover `className="dark"` dos 2 divs em `app.tsx` (linhas 25 e 34) — manter outras classes
6. Se cenário (C): atualizar `tailwind.config.ts` para `darkMode: ['class', '[data-theme*="dark"]']`
7. Smoke test visual: `npm run dev`, abrir browser, verificar:
   - paleta Violet Forge renderizando
   - backgrounds escuros, texto branco (`text-white` deve funcionar)
   - DevTools: `document.documentElement` deve ter `dark` class OU data attribute

#### TDD
```
RED:     test_theme_provider_wraps_app() em src/main.test.tsx (NEW): renderiza <App /> e
         asserta data-theme="violet-forge" + data-mode="dark" em algum elemento root
GREEN:   Implementar wrapping em main.tsx
REFACTOR: None
VERIFY:  npm test -- main
```

#### Acceptance Criteria
- [ ] `index.css` importa tokens.css + styles.css de `@loopzero/ui`
- [ ] `main.tsx` envolve `<App />` com `<ThemeProvider defaultTheme="violet-forge" defaultMode="dark">`
- [ ] `index.html` não tem mais `class="dark"` no `<html>` (delegado a ThemeProvider)
- [ ] `npm run dev` abre sem erro de console
- [ ] Visual: surface escura, accent verde, tipografia DM Sans/Cabinet Grotesk renderiza
- [ ] Test `test_theme_provider_wraps_app` verde

#### DoD
- [ ] Tasks 1-4 completas
- [ ] Build verde
- [ ] Commit `feat(dashboard): wire @loopzero/ui ThemeProvider with Violet Forge dark`

---

### T0.3 — Validate Tailwind co-existence with `@loopzero/ui` CSS tokens (REWRITTEN, addresses EC-1)

#### Objective
Garantir que `tailwind.config.ts` local **coexiste** corretamente com tokens CSS importados em T0.2. **Não há JS preset para importar** — descoberta da revisão EC-1: `@loopzero/ui/package.json` exports apenas CSS files (`./preset.css`, `./tokens.css`, `./styles.css`). Tailwind v3 `presets: []` config exige objeto JS que não existe.

#### Evidence
- `grep -E '"./preset"' loopzero-ui/package.json` → vazio (EC-1)
- Únicos exports relacionados a preset são CSS: `./preset.css`, `./tokens-v4.css` (v4-specific)
- Dashboard usa Tailwind v3 (`tailwindcss: ^3.4.0`)
- Design system flui via CSS variables definidas em `tokens.css` (importado em T0.2)

#### Files to edit
```
cloud/dashboard/tailwind.config.ts — manter mínimo: content paths + dark mode + tokens locais não cobertos por CSS vars
```

#### Deep file dependency analysis
- `tailwind.config.ts` (62 LOC): contém `theme.extend.colors` (paleta `loopzero`/`surface`/`accent`/`danger`/`warning`), fontes, keyframes/animations
- Estratégia correta: T0.2 traz CSS vars de `@loopzero/ui`; T0.3 mantém config local como está (sem adicionar preset inexistente); T6.2 valida quais tokens locais ainda são necessários e remove os duplicados
- **Não há mudança ativa em T0.3** — é uma task de validação que documenta o que NÃO fazer

#### Deep Dives
- **Por que não há preset JS?**: `@loopzero/ui` é design system primariamente CSS-first (tokens via CSS variables `--color-*`, utilities em `styles.css`). Tailwind config local lida com `content` paths e extensões locais; design tokens vêm via CSS, não via JS merge.
- **Tailwind v4 migration future**: `loopzero-ui` exporta `./tokens-v4.css` para v4 (que usa `@theme {}` no CSS). Quando dashboard upgradar para Tailwind v4, esta task vira "migrate from extend.colors para @theme in CSS".
- **Coexistência**: classes `bg-surface-0` (local Tailwind) e `bg-[var(--surface-0)]` (CSS var de @loopzero/ui) podem coexistir durante migração. T6.2 reconcilia.

#### Tasks
1. **Não modificar `tailwind.config.ts`** — preserve as is. T0.2 já traz design system via CSS imports.
2. Validar com `npm run build` que classes Tailwind ainda resolvem APÓS T0.2 ter rodado
3. Inspecionar `dist/assets/*.css` para confirmar que CSS vars de `@loopzero/ui` estão presentes (não removidas por PurgeCSS)
4. Documentar em comentário no topo de `tailwind.config.ts`: `// Design tokens vêm de @loopzero/ui via src/index.css. Local extend cobre apenas tokens transitórios até T6.2 cleanup.`

#### TDD
```
RED:     N/A — validation task, no behavior change
GREEN:   npm run build exits 0 após T0.2 + T0.3; dist CSS contém AMBOS classes locais E CSS vars de @loopzero/ui
REFACTOR: T6.2 fará cleanup
VERIFY:  npm run build && grep -E 'surface-0|--color-' cloud/dashboard/dist/assets/*.css
```

#### Acceptance Criteria
- [ ] `tailwind.config.ts` preservado (com comentário documentando estratégia CSS-first)
- [ ] `npm run build` exits 0
- [ ] CSS bundle contém CSS vars de `@loopzero/ui` (não purgadas)
- [ ] Sem import de preset JS inexistente

#### DoD
- [ ] Tasks 1-4 completas
- [ ] Build verde
- [ ] Commit `docs(dashboard): document tailwind.config strategy (CSS-first via @loopzero/ui)`

---

## Phase 1: UI Primitives Swap

**Objective:** Eliminar `src/components/ui/{badge,button,card}.tsx`. Substituir 30+ imports `@/components/ui/*` por `@loopzero/ui`.

### T1.1 — Replace `Button`

#### Objective
Trocar `import { Button } from '@/components/ui/button'` por `import { Button } from '@loopzero/ui'` em todos os arquivos. Validar API compatível.

#### Evidence
- `cloud/dashboard/src/components/ui/button.tsx` (42 LOC) implementa Button com variantes `primary | secondary | ghost | outline` e sizes `default | sm | lg | icon`
- `@loopzero/ui` exporta `Button` com API shadcn-compatible (mesmas variantes)
- Consumidores: `login.tsx`, `register.tsx`, `verification.tsx`, `recovery.tsx`, `device-success.tsx`, `team.tsx`, `billing.tsx`, `settings.tsx`, `profile.tsx`, e componentes — total ~25 sites

#### Files to edit
```
cloud/dashboard/src/components/ui/button.tsx     — DELETE
cloud/dashboard/src/pages/login.tsx              — update import
cloud/dashboard/src/pages/register.tsx           — update import
cloud/dashboard/src/pages/verification.tsx       — update import
cloud/dashboard/src/pages/recovery.tsx           — update import (também: rotear /recovery em app.tsx)
cloud/dashboard/src/pages/device-success.tsx     — update import
cloud/dashboard/src/pages/team.tsx               — update import
cloud/dashboard/src/pages/billing.tsx            — update import
cloud/dashboard/src/pages/settings.tsx           — update import
cloud/dashboard/src/pages/profile.tsx            — update import
cloud/dashboard/src/components/verification-banner.tsx — update import (se usa Button)
cloud/dashboard/src/components/sidebar.tsx       — update import (se usa Button)
```

#### Deep file dependency analysis
- `button.tsx` local: deletar
- Cada consumer: trocar `'@/components/ui/button'` → `'@loopzero/ui'`. Se props divergirem, ajustar (ex: `variant="secondary"` pode virar `variant="outline"` se nomes diferentes).
- `recovery.tsx`: precisa também ser roteada em `app.tsx` (T1.4) — bug fix oportunístico

#### Deep Dives
- **API divergence**: Se `@loopzero/ui` Button não aceitar `size="icon"` ou `variant="ghost"`, precisamos adaptar. Mapear divergências antes de delete: ler `loopzero-ui/src/components/primitives/button/button.tsx` API.
- **className prop**: Deve continuar funcionando — Radix-based Buttons sempre aceitam className.

#### Tasks
1. Ler API de `Button` em `@loopzero/ui` (via `node_modules/@loopzero/ui/dist/index.d.ts` após install)
2. Mapear divergências de variant/size names (criar tabela na PR)
3. Replace-all import path em todos os consumers
4. Ajustar variant/size props se divergirem
5. Deletar `src/components/ui/button.tsx`
6. `npm run build && npm test`

#### TDD
```
RED:     test_button_renders_after_migration() em src/pages/login.test.tsx (NEW):
         renderiza <LoginPage />, asserta `Sign in` button presente, click dispara submit
RED:     existing auth.test.ts — segue passando
GREEN:   Replace imports, ajustar props se necessário
REFACTOR: Após confirmado, deletar button.tsx
VERIFY:  npm test && npm run build
```

#### Acceptance Criteria
- [ ] `src/components/ui/button.tsx` deletado
- [ ] Nenhum arquivo importa de `@/components/ui/button`
- [ ] `npm run build` verde
- [ ] `npm test` verde (incluindo auth.test.ts)
- [ ] Smoke test: botão "Sign in" funciona em `/login`

#### DoD
- [ ] Tasks 1-6 completas
- [ ] Commit `refactor(dashboard): swap local Button for @loopzero/ui Button`

---

### T1.2 — Replace `Badge`

#### Objective
Trocar `Badge` local por `Badge` de `@loopzero/ui`. Mesmo procedimento de T1.1.

#### Evidence
- `src/components/ui/badge.tsx` (26 LOC) implementa variantes `default | success | danger | warning | outline`
- Consumidores: `overview.tsx`, `projects.tsx`, `environments.tsx`, `billing.tsx`, `domains.tsx`, etc.

#### Files to edit
```
cloud/dashboard/src/components/ui/badge.tsx     — DELETE
cloud/dashboard/src/pages/{overview,projects,environments,billing,domains,team,settings}.tsx — update imports
```

#### Tasks
1. Ler API de Badge em `@loopzero/ui`
2. Mapear variantes (provável compat total)
3. Replace-all imports
4. Deletar badge local
5. Build + test

#### TDD
```
RED:     test_badge_variants_render() — para cada variant ('success', 'danger', 'warning'),
         renderiza e asserta classes corretas aplicadas
GREEN:   Imports trocados, badge local deletado
REFACTOR: None
VERIFY:  npm test
```

#### Acceptance Criteria
- [ ] `src/components/ui/badge.tsx` deletado
- [ ] Build + test verdes

#### DoD
- [ ] Commit `refactor(dashboard): swap local Badge for @loopzero/ui Badge`

---

### T1.3 — Replace `Card`

#### Objective
Trocar `Card`, `CardContent`, `CardHeader`, `CardTitle` locais por equivalentes de `@loopzero/ui`.

#### Evidence
- `src/components/ui/card.tsx` (47 LOC) com subcomponentes
- Consumidores: TODAS as 12 páginas (Card é o building block central)

#### Files to edit
```
cloud/dashboard/src/components/ui/card.tsx — DELETE
cloud/dashboard/src/pages/*.tsx            — update imports (12 arquivos)
cloud/dashboard/src/components/sidebar.tsx — update se usa
cloud/dashboard/src/components/page-wrapper.tsx — update se usa
```

#### Tasks
1. Ler API de Card de `@loopzero/ui`
2. Replace-all imports
3. Deletar card local
4. Build + test

#### TDD
```
RED:     test_card_composition() — renderiza <Card><CardHeader><CardTitle/></CardHeader>
         <CardContent/></Card>, asserta semantic structure preservada
GREEN:   Imports trocados, card local deletado
REFACTOR: None
VERIFY:  npm test
```

#### Acceptance Criteria
- [ ] `src/components/ui/card.tsx` deletado
- [ ] Diretório `src/components/ui/` **deletado** (todos os 3 arquivos foram embora em T1.1-T1.3)
- [ ] Build + test verdes

#### DoD
- [ ] Commit `refactor(dashboard): swap local Card primitives for @loopzero/ui; delete src/components/ui/`

---

### T1.4 — Route `/recovery` (bug fix oportunístico)

#### Objective
Adicionar `<Route path="/recovery" element={<RecoveryPage />} />` ao `app.tsx`. Corrige bug do link em `login.tsx:227` que aponta para rota inexistente.

#### Evidence
- `login.tsx:227`: `<Link to="/recovery" className="hover:text-white/50">Forgot password?</Link>`
- `grep -E "Route path" cloud/dashboard/src/app.tsx` mostra 12 rotas; nenhuma é `/recovery`
- `pages/recovery.tsx` existe (143 LOC, Kratos recovery flow)

#### Files to edit
```
cloud/dashboard/src/app.tsx — adicionar <Route path="/recovery" /> nas rotas públicas
```

#### Tasks
1. Adicionar import `import { RecoveryPage } from '@/pages/recovery';` em `app.tsx`
2. Adicionar `<Route path="/recovery" element={<RecoveryPage />} />` ao grupo de rotas públicas (junto com login/register/verification)
3. Smoke test: clicar "Forgot password?" em `/login` agora vai para `/recovery`

#### TDD
```
RED:     test_recovery_route_renders() — renderiza <App /> com route='/recovery',
         asserta RecoveryPage está no DOM
GREEN:   Adicionar rota
REFACTOR: None
VERIFY:  npm test -- app
```

#### Acceptance Criteria
- [ ] `app.tsx` tem `<Route path="/recovery" element={<RecoveryPage />} />`
- [ ] Test `test_recovery_route_renders` verde
- [ ] Link "Forgot password?" em `/login` navega corretamente

#### DoD
- [ ] Commit `fix(dashboard): route /recovery to fix dangling link from login page`

---

## Phase 2: Layout Shell

**Objective:** Refatorar `sidebar`, `page-wrapper`, `verification-banner` usando primitives de `@loopzero/ui`. Layout consistente é prerequisite para todas as páginas internas parecerem coerentes.

### T2.1 — Refactor `Sidebar`

#### Objective
Substituir `components/sidebar.tsx` (109 LOC, hand-rolled nav + Radix DropdownMenu) por composição de primitives `@loopzero/ui` (`Sidebar` / `SidebarNav` / `Avatar` / `Tooltip` / `DropdownMenu`).

#### Evidence
- `sidebar.tsx` atual: 109 LOC, layout fixed-left 220px, items hard-coded, avatar user via Radix
- `@loopzero/ui` tem `Avatar`, `Tooltip`, `DropdownMenu` primitives (Radix-based); pode ter `Sidebar` composite ou montamos com `Card`/`Stack`

#### Files to edit
```
cloud/dashboard/src/components/sidebar.tsx — rewrite usando @loopzero/ui primitives
```

#### Deep file dependency analysis
- `sidebar.tsx` consumido por `app.tsx:35` (`<DashboardLayout>`)
- Nav items: hard-coded array; manter shape
- `useRequireAuth` hook continua emitindo session para avatar/email

#### Deep Dives
- **Composite ausente?**: Se `@loopzero/ui` não exportar `<Sidebar>` composite, construímos com `<aside>` + nossas primitives. Aceitável — é wiring, não reinvenção.
- **Logout dropdown**: usar `DropdownMenu` de `@loopzero/ui` (Radix-based, mesma API do Radix direto)

#### Tasks
1. Auditar exports de `@loopzero/ui` para `Sidebar`-related composites
2. Rewrite sidebar usando primitives apropriadas (manter rotas e logout)
3. Smoke test visual

#### TDD
```
RED:     test_sidebar_renders_nav() — asserta links 'Overview', 'Projects', 'Environments',
         'Team', 'Billing', 'Domains', 'Settings' presentes
RED:     test_sidebar_logout_dispatches() — click no avatar → dropdown → 'Logout'
         dispara fetch(/self-service/logout/browser)
GREEN:   Rewrite usando @loopzero/ui
REFACTOR: None
VERIFY:  npm test -- sidebar
```

#### Acceptance Criteria
- [ ] Sidebar usa primitives `@loopzero/ui`
- [ ] LOC reduzidas (target ≤60)
- [ ] Testes verdes
- [ ] Smoke test: navegar entre páginas funciona

#### DoD
- [ ] Commit `refactor(dashboard): rebuild Sidebar on @loopzero/ui primitives`

---

### T2.2 — Refactor `PageWrapper`

#### Objective
Substituir `components/page-wrapper.tsx` (30 LOC) por uso direto de primitive `PageHeader` de `@loopzero/ui` (ou Stack + Heading + Text).

#### Files to edit
```
cloud/dashboard/src/components/page-wrapper.tsx — rewrite/delete dependendo do que @loopzero/ui oferece
```

#### Tasks
1. Verificar se `@loopzero/ui` expõe `PageHeader` ou equivalente
2. Se sim: deletar wrapper local, atualizar imports nas 12 páginas
3. Se não: simplificar wrapper local usando `Heading` + `Text` primitives

#### TDD
```
RED:     test_page_wrapper_renders_title_description() — renderiza com title+description,
         asserta ambos visíveis
GREEN:   Rewrite ou substituir
REFACTOR: None
VERIFY:  npm test -- page-wrapper
```

#### Acceptance Criteria
- [ ] `page-wrapper.tsx` ou deletado ou simplificado para <15 LOC
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): simplify PageWrapper using @loopzero/ui primitives`

---

### T2.3 — Refactor `VerificationBanner`

#### Objective
Substituir `components/verification-banner.tsx` (27 LOC) usando `Alert` ou `Banner` primitive de `@loopzero/ui`.

#### Files to edit
```
cloud/dashboard/src/components/verification-banner.tsx — rewrite
```

#### Tasks
1. Localizar primitive equivalente em `@loopzero/ui`
2. Rewrite mantendo lógica (CTA "Verify now" → `/verification`)

#### TDD
```
RED:     test_verification_banner_renders_when_unverified() — asserta banner visível
RED:     test_verification_banner_cta_navigates() — click no CTA navega para /verification
GREEN:   Rewrite usando @loopzero/ui Alert
REFACTOR: None
VERIFY:  npm test -- verification-banner
```

#### Acceptance Criteria
- [ ] Banner usa primitive `@loopzero/ui`
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild VerificationBanner on @loopzero/ui Alert`

---

## Phase 3: Auth Pages (login, register, verification, recovery, device-success)

**Objective:** Refatorar 5 páginas de auth (4 originais + recovery que foi roteada em T1.4). Padrão visual coerente inspirado em `loopzero-ui/src/screens/login.stories.tsx`.

### T3.1 — Refactor `LoginPage`

#### Objective
Reescrever `pages/login.tsx` (244 LOC) usando `FormField`, `Button`, `Alert` de `@loopzero/ui`. Eliminar SVG inline do Google, blur background ad-hoc, divider manual. Manter fluxo Kratos intacto.

#### Evidence
- `login.tsx` atual: 244 LOC; ~80 LOC são pure visual scaffolding eliminável
- `loopzero-ui/src/screens/login.stories.tsx` mostra pattern canônico

#### Files to edit
```
cloud/dashboard/src/pages/login.tsx — rewrite
```

#### Deep Dives
- **Preservar**: Kratos flow loading (`useEffect` + `fetch self-service/login/browser`), CSRF token extraction, OIDC submit (form POST hidden), password submit, error display, "session expired" notification
- **Substituir**: `<input>` brutos por `FormField`, divider manual por primitive, SVG Google inline por `SocialAuthButton` (se houver) ou `Button` com ícone Lucide

#### Tasks
1. Identificar primitivas: `FormField` (email/password), `Button` (variantes social + primary), `Alert` (errors + session expired notice)
2. **CRITICAL (EC-4)**: Preservar `submitOIDC` handler VERBATIM — `document.createElement('form')` + `form.submit()` é um browser-level form POST que Kratos exige para seguir o redirect OAuth. Substituir por `fetch(action, {method:'POST'})` quebra silenciosamente: fetch retorna 302 mas browser nunca navega para GitHub/Google. Apenas o `<Button>` que dispara o handler é substituído por `@loopzero/ui` Button; o handler em si fica vanilla DOM.
3. Rewrite preservando: state, useEffects, submitPassword, navigation. submitOIDC permanece inalterado conforme task 2.
4. Eliminar: motion variants ad-hoc (usar transição padrão de `@loopzero/ui` se houver), blur div, classes Tailwind hand-rolled
5. Smoke test: login com email/senha + login GitHub OAuth (asserta que GitHub redirect funciona)

#### TDD
```
RED:     test_login_loads_kratos_flow() — asserta fetch(/self-service/login/browser) é chamado
RED:     test_login_submit_password() — fill email+password, click 'Sign in' → asserta POST
         com csrf_token + identifier + password
RED:     test_login_oidc_submits_form() — click 'Continue with GitHub' → asserta form.submit()
         com provider='github'
RED:     test_login_session_expired_notice() — searchParams=reason=expired → asserta notice visível
RED:     test_login_displays_flow_errors() — flow.ui.messages com error → asserta error visível
GREEN:   Rewrite usando @loopzero/ui
REFACTOR: None
VERIFY:  npm test -- login
```

#### Acceptance Criteria
- [ ] `login.tsx` reduzido de 244 para ≤120 LOC
- [ ] 5 testes RED verdes pós-implementação
- [ ] Smoke test: login email/senha funciona contra Kratos local
- [ ] Smoke test: login GitHub OAuth funciona

#### DoD
- [ ] Commit `refactor(dashboard): rebuild LoginPage on @loopzero/ui FormField+Button+Alert`

---

### T3.2 — Refactor `RegisterPage`

#### Objective
Mesmo padrão de T3.1 para `pages/register.tsx` (207 LOC).

#### Files to edit
```
cloud/dashboard/src/pages/register.tsx — rewrite
```

#### Tasks
1. Reuse padrão T3.1
2. Preservar Kratos registration flow

#### TDD
```
RED:     test_register_loads_kratos_flow()
RED:     test_register_submit_password()
RED:     test_register_oidc_signup()
GREEN:   Rewrite
VERIFY:  npm test -- register
```

#### Acceptance Criteria
- [ ] LOC ≤120
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild RegisterPage on @loopzero/ui`

---

### T3.3 — Refactor `VerificationPage`

#### Objective
Refatorar `pages/verification.tsx` (140 LOC) usando `EmptyState` ou `Alert` para estados de verificação.

#### Files to edit
```
cloud/dashboard/src/pages/verification.tsx — rewrite
```

#### TDD
```
RED:     test_verification_renders_email_input()
RED:     test_verification_submit_triggers_send_email()
RED:     test_verification_displays_success_state()
GREEN:   Rewrite
VERIFY:  npm test -- verification
```

#### Acceptance Criteria
- [ ] LOC ≤80
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild VerificationPage on @loopzero/ui`

---

### T3.4 — Refactor `RecoveryPage`

#### Objective
Refatorar `pages/recovery.tsx` (143 LOC). Agora roteada em T1.4 — primeira vez sendo testada visualmente.

#### Files to edit
```
cloud/dashboard/src/pages/recovery.tsx — rewrite
```

#### TDD
```
RED:     test_recovery_loads_kratos_flow()
RED:     test_recovery_submit_email()
RED:     test_recovery_displays_email_sent_state()
GREEN:   Rewrite
VERIFY:  npm test -- recovery
```

#### Acceptance Criteria
- [ ] LOC ≤80
- [ ] Testes verdes
- [ ] Smoke test: fluxo completo recovery funciona contra Kratos

#### DoD
- [ ] Commit `refactor(dashboard): rebuild RecoveryPage on @loopzero/ui`

---

### T3.5 — Refactor `DeviceSuccessPage`

#### Objective
Refatorar `pages/device-success.tsx` (21 LOC — já pequena) usando `EmptyState` de `@loopzero/ui` com variant success.

#### Files to edit
```
cloud/dashboard/src/pages/device-success.tsx — rewrite
```

#### TDD
```
RED:     test_device_success_renders_confirmation()
GREEN:   Rewrite
VERIFY:  npm test -- device-success
```

#### Acceptance Criteria
- [ ] LOC ≤30
- [ ] Teste verde

#### DoD
- [ ] Commit `refactor(dashboard): rebuild DeviceSuccessPage on @loopzero/ui EmptyState`

---

## Phase 4: Core Pages (overview, projects, environments, billing, domains)

**Objective:** Refatorar as 5 páginas centrais usando **composites** de `@loopzero/ui` (`ProjectCard`, `DeploymentRow`, `EnvVarEditor`, `DomainConfig`, `CostMeter`).

### T4.1 — Refactor `OverviewPage`

#### Objective
Substituir `pages/overview.tsx` (113 LOC) usando `StatsTile` (ou Card+Heading), `DeploymentRow` para recent deploys, `EmptyState` para empty.

#### Evidence
- `overview.tsx:31-66` reescreve stat tile na mão (21 LOC por tile × 3 = 63 LOC)
- `DeploymentRow` composite existe em `loopzero-ui/src/components/composites/deployment-row/`

#### Files to edit
```
cloud/dashboard/src/pages/overview.tsx — rewrite
```

#### Deep Dives
- **Endpoint actual**: `/api/v1/projects` via `useEngineApi`. Sem mudança.
- **Recent deploys**: hoje array vazio (comentário no código diz "Build history is per-project"). Manter empty state mas usar `EmptyState` primitive de `@loopzero/ui`.

#### Tasks
1. Identificar primitive equivalente a "stat tile" em `@loopzero/ui` (provável `Card` + `Stat`)
2. Usar `EmptyState` para "No deploys yet"
3. Rewrite preservando `useEngineApi` calls

#### TDD
```
RED:     test_overview_renders_loading_state()
RED:     test_overview_renders_project_count() — mock data → asserta count visível
RED:     test_overview_renders_empty_state_when_no_deploys()
GREEN:   Rewrite
VERIFY:  npm test -- overview
```

#### Acceptance Criteria
- [ ] LOC ≤60
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild OverviewPage with @loopzero/ui composites`

---

### T4.2 — Refactor `ProjectsPage`

#### Objective
Substituir `pages/projects.tsx` (92 LOC) usando **`ProjectCard` composite** (composite-perfeito-match).

#### Evidence
- `projects.tsx:51-88` reescreve project card na mão (~38 LOC por card)
- `ProjectCard` composite existe em `loopzero-ui/src/components/composites/project-card/`

#### Files to edit
```
cloud/dashboard/src/pages/projects.tsx — rewrite
```

#### Tasks
1. **CRITICAL (EC-5)**: Ler `ProjectCardProps` em `node_modules/@loopzero/ui/dist/index.d.ts`. Interface local `Project` (projects.tsx:9-15) tem `apps?`, `last_deploy?`, `environment?`, `created_at?` TODOS opcionais. Se `ProjectCard` requer algum, construir adapter com defaults sensatos:
   ```ts
   const cardProps = {
     name: project.name,
     environment: project.environment ?? 'production',
     lastDeploy: project.last_deploy ?? 'never',
     apps: project.apps ?? [],
   };
   ```
2. Rewrite usando `<ProjectCard {...cardProps} />` por projeto
3. Validar com TypeScript build: zero `Property X is missing` errors

#### TDD
```
RED:     test_projects_renders_loading()
RED:     test_projects_renders_empty_state()
RED:     test_projects_renders_project_cards() — mock 3 projects → asserta 3 ProjectCards
RED:     test_projects_card_navigates_on_click()
GREEN:   Rewrite usando ProjectCard
VERIFY:  npm test -- projects
```

#### Acceptance Criteria
- [ ] LOC ≤50
- [ ] Testes verdes
- [ ] Smoke test: clicar card navega corretamente

#### DoD
- [ ] Commit `refactor(dashboard): rebuild ProjectsPage with ProjectCard composite`

---

### T4.3 — Refactor `EnvironmentsPage`

#### Objective
Refatorar `pages/environments.tsx` (76 LOC) usando `DeploymentRow` composite.

#### Files to edit
```
cloud/dashboard/src/pages/environments.tsx — rewrite
```

#### TDD
```
RED:     test_environments_renders_deployment_rows()
RED:     test_environments_empty_state()
GREEN:   Rewrite
VERIFY:  npm test -- environments
```

#### Acceptance Criteria
- [ ] LOC ≤50
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild EnvironmentsPage with DeploymentRow`

---

### T4.4 — Refactor `BillingPage`

#### Objective
Refatorar `pages/billing.tsx` (259 LOC — a maior do projeto) usando `CostMeter` primitive + `Card` + `Button` para checkout/portal.

#### Files to edit
```
cloud/dashboard/src/pages/billing.tsx — rewrite
```

#### Deep Dives
- Preservar handlers de portal/checkout que chamam `/v1/billing/portal` e `/v1/billing/checkout`
- `CostMeter` primitive visualiza usage vs cap — ideal para displays de plan + uso atual

#### Tasks
1. Identificar `CostMeter` API
2. Rewrite preservando handlers de billing

#### TDD
```
RED:     test_billing_renders_current_plan()
RED:     test_billing_renders_cost_meter_with_usage()
RED:     test_billing_portal_button_calls_endpoint()
RED:     test_billing_checkout_button_calls_endpoint()
GREEN:   Rewrite
VERIFY:  npm test -- billing
```

#### Acceptance Criteria
- [ ] LOC ≤140
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild BillingPage with CostMeter primitive`

---

### T4.5 — Refactor `DomainsPage`

#### Objective
Refatorar `pages/domains.tsx` (182 LOC) usando **`DomainConfig` composite** (composite-perfeito-match).

#### Files to edit
```
cloud/dashboard/src/pages/domains.tsx — rewrite
```

#### TDD
```
RED:     test_domains_renders_list()
RED:     test_domains_add_domain_form()
RED:     test_domains_verification_state()
GREEN:   Rewrite usando DomainConfig
VERIFY:  npm test -- domains
```

#### Acceptance Criteria
- [ ] LOC ≤90
- [ ] Testes verdes

#### DoD
- [ ] Commit `refactor(dashboard): rebuild DomainsPage with DomainConfig composite`

---

## Phase 5: Secondary Pages (team, settings, profile)

**Objective:** Refatorar as 3 páginas restantes.

### T5.1 — Refactor `TeamPage`

#### Files to edit
```
cloud/dashboard/src/pages/team.tsx — rewrite (81 LOC) usando Avatar + Card + Button
```

#### TDD
```
RED:     test_team_renders_members_list()
RED:     test_team_invite_button_opens_modal()
GREEN:   Rewrite
VERIFY:  npm test -- team
```

#### Acceptance Criteria
- [ ] LOC ≤50

#### DoD
- [ ] Commit `refactor(dashboard): rebuild TeamPage on @loopzero/ui`

---

### T5.2 — Refactor `SettingsPage`

#### Files to edit
```
cloud/dashboard/src/pages/settings.tsx — rewrite (57 LOC) usando FormField + Card
```

#### TDD
```
RED:     test_settings_renders_form()
RED:     test_settings_save_calls_api()
GREEN:   Rewrite
VERIFY:  npm test -- settings
```

#### Acceptance Criteria
- [ ] LOC ≤40

#### DoD
- [ ] Commit `refactor(dashboard): rebuild SettingsPage on @loopzero/ui`

---

### T5.3 — Refactor `ProfilePage`

#### Files to edit
```
cloud/dashboard/src/pages/profile.tsx — rewrite (74 LOC) usando Avatar + FormField + Card
```

#### TDD
```
RED:     test_profile_renders_user_info()
RED:     test_profile_edit_dispatches()
GREEN:   Rewrite
VERIFY:  npm test -- profile
```

#### Acceptance Criteria
- [ ] LOC ≤50

#### DoD
- [ ] Commit `refactor(dashboard): rebuild ProfilePage on @loopzero/ui`

---

## Phase 6: Cleanup + Validation

**Objective:** Remover dependências obsoletas, tokens duplicados, validar bundle, smoke test final.

### T6.1 — Remove obsolete dependencies

#### Objective
Auditar `package.json` removendo deps que `@loopzero/ui` traz transitivamente.

#### Files to edit
```
cloud/dashboard/package.json — remove deps
cloud/dashboard/package-lock.json — regen
```

#### Tasks
1. `npm ls @radix-ui/react-avatar` etc. — se aparece como dep transitiva de `@loopzero/ui`, remover do package.json local
2. Manter `framer-motion`, `lucide-react`, `clsx`, `tailwind-merge` (D4)
3. `npm install` para atualizar lock

#### Acceptance Criteria
- [ ] `package.json` dependencies tem apenas: `@loopzero/ui`, `react`, `react-dom`, `react-router-dom`, `framer-motion`, `lucide-react`, `clsx`, `tailwind-merge`
- [ ] `npm install` exits 0

#### DoD
- [ ] Commit `chore(dashboard): remove redundant deps now provided by @loopzero/ui`

---

### T6.2 — Remove duplicate Tailwind tokens

#### Objective
Limpar `tailwind.config.ts` removendo paleta `loopzero.*`/`surface.*` agora fornecida pelo preset.

#### Files to edit
```
cloud/dashboard/tailwind.config.ts — remove theme.extend.colors duplicado
```

#### Tasks
1. **CRITICAL (EC-6)**: Pre-validação obrigatória ANTES de remover tokens — rodar:
   ```bash
   cd cloud/dashboard && grep -rE 'loopzero-[0-9]+|surface-[0-4]' src/ --include='*.tsx' --include='*.ts' | grep -v node_modules
   ```
   Resultado **DEVE ser zero matches**. Se retornar matches:
   - Mapear cada ocorrência para token equivalente em `@loopzero/ui` (CSS var ou classe)
   - Fixar nas páginas ANTES de prosseguir
   - Re-rodar grep até zero
2. Validar que páginas usam classes do preset/CSS vars (não tokens locais)
3. Remover `theme.extend.colors.loopzero`/`surface` de `tailwind.config.ts` apenas APÓS task 1 retornar zero
4. Manter `keyframes`/`animation` locais se ainda em uso (verificar com grep separado)

#### Acceptance Criteria
- [ ] `tailwind.config.ts` é menor (target ≤25 LOC)
- [ ] `npm run build` verde

#### DoD
- [ ] Commit `chore(dashboard): remove tailwind tokens duplicated by @loopzero/ui preset`

---

### T6.3 — Bundle size check

#### Objective
Validar que `dist/assets/*.js` está dentro de ±10% do baseline pré-refactor.

#### Tasks
1. Capturar baseline pré-refactor (commit antes do plano)
2. Rodar `npm run build` final
3. Comparar `du -sh dist/assets/*.js`

#### Acceptance Criteria
- [ ] Bundle size: |delta| ≤10% do baseline
- [ ] Se exceder: investigar (tree-shake quebrado?)

#### DoD
- [ ] Documentar bundle delta em commit message ou PR description

---

### T6.4 — Full vitest suite

#### Tasks
1. `npm test` na raiz de dashboard
2. Validar que TODOS os testes adicionados em T1-T5 estão verdes
3. Validar que `auth.test.ts` (preserved) continua verde

#### Acceptance Criteria
- [ ] `npm test` exits 0
- [ ] Coverage report: ≥ baseline (não menos)

#### DoD
- [ ] Test summary commit

---

### T6.5 — Manual smoke test (12 páginas + recovery)

#### Tasks
1. `task setup && tilt up` (k3d local)
2. Abrir `https://cloud.loopzero.local` em Chrome dark mode
3. Para cada uma das 13 rotas (12 originais + recovery):
   - Navegar
   - Validar render sem erro de console
   - Validar interactions principais (forms submit, buttons click)
4. Validar Rule 3 (zero termos internos de infra): rodar no Chrome DevTools console:
   ```js
   document.body.innerText.match(/Kubernetes|ArgoCD|Gitea|MinIO|Cosign|Pulumi|Helmfile|Tilt|Hydra|Kratos|pgvector|cell-\d/gi)
   ```
   Resultado **DEVE ser null**. Notar que `body.innerText` ignora href/src attributes (correto — URLs Kratos em `/self-service/login/browser` não são UI text).
5. **CRITICAL (EC-7)**: Validar zero termos "agent-shaped" out-of-context — `@loopzero/ui` é agent-first design system; componentes importados podem trazer microcopy default como "Mem0", "MCP", "agent runtime", "tool call", "skill". Rodar:
   ```js
   document.body.innerText.match(/Mem0|\bMCP\b|agent runtime|tool call|skill manager|sub-agent/gi)
   ```
   Se aparecer, customizar `children`/`labels` props nos componentes importados (eles aceitam override).

#### Acceptance Criteria
- [ ] 13 rotas renderizam sem erro
- [ ] Login flow completo OK (email+senha + GitHub OAuth)
- [ ] Console sem warnings de hidratação React
- [ ] Rule 3 limpo

#### DoD
- [ ] Smoke test log commitado em `docs/reviews/` ou anexado à PR

---

### T6.6 — Bundle e build de produção da imagem Docker

#### Objective
Validar que `cloud/dashboard/Dockerfile` ainda produz imagem funcional.

#### Tasks
1. `cd cloud/dashboard && docker build -t loopzero-dashboard:test .`
2. `docker run --rm -p 8080:8080 loopzero-dashboard:test`
3. `curl http://localhost:8080/` → asserta HTML válido com `<div id="root">` + `<script src="/__config.js">`
4. Validar `/__config.js` gerado pelo entrypoint

#### Acceptance Criteria
- [ ] Imagem build exits 0
- [ ] Container starts sem erro
- [ ] `/__config.js` renderiza valores corretos do ambiente

#### DoD
- [ ] Smoke test container commitado

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | @loopzero/ui não instalado | T0.1 | npm install @loopzero/ui@next |
| 2 | Sem ThemeProvider | T0.2 | Wire em main.tsx com Violet Forge dark |
| 3 | Tailwind sem preset | T0.3 | Importa @loopzero/ui/preset |
| 4 | Button hand-rolled | T1.1 | Swap por @loopzero/ui Button |
| 5 | Badge hand-rolled | T1.2 | Swap por @loopzero/ui Badge |
| 6 | Card hand-rolled | T1.3 | Swap por @loopzero/ui Card; deletar src/components/ui/ |
| 7 | Bug /recovery sem rota | T1.4 | Adicionar rota em app.tsx |
| 8 | Sidebar hand-rolled | T2.1 | Rebuild com primitives |
| 9 | PageWrapper hand-rolled | T2.2 | Simplificar ou substituir |
| 10 | VerificationBanner hand-rolled | T2.3 | Rebuild com Alert |
| 11 | LoginPage 244 LOC | T3.1 | Rewrite ≤120 LOC |
| 12 | RegisterPage 207 LOC | T3.2 | Rewrite ≤120 LOC |
| 13 | VerificationPage 140 LOC | T3.3 | Rewrite ≤80 LOC |
| 14 | RecoveryPage 143 LOC | T3.4 | Rewrite ≤80 LOC |
| 15 | DeviceSuccess 21 LOC | T3.5 | Rewrite com EmptyState |
| 16 | OverviewPage stat tiles ad-hoc | T4.1 | StatsTile + EmptyState + DeploymentRow |
| 17 | ProjectsPage project cards ad-hoc | T4.2 | ProjectCard composite |
| 18 | EnvironmentsPage rows ad-hoc | T4.3 | DeploymentRow composite |
| 19 | BillingPage 259 LOC | T4.4 | CostMeter + rewrite ≤140 LOC |
| 20 | DomainsPage 182 LOC | T4.5 | DomainConfig composite |
| 21 | TeamPage hand-rolled | T5.1 | Avatar + Card + Button |
| 22 | SettingsPage hand-rolled | T5.2 | FormField + Card |
| 23 | ProfilePage hand-rolled | T5.3 | Avatar + FormField + Card |
| 24 | Deps redundantes | T6.1 | Remover Radix individuais |
| 25 | Tokens Tailwind duplicados | T6.2 | Remover paleta local |
| 26 | Bundle size drift | T6.3 | Validar ±10% |
| 27 | Suite vitest | T6.4 | npm test verde |
| 28 | Smoke test 13 rotas | T6.5 | Manual em Chrome dark |
| 29 | Docker image OK | T6.6 | docker build + run |

**Coverage: 29/29 gaps cobertos (100%)**

## Global Definition of Done

- [ ] All phases (0-6) completed
- [ ] Todos os testes passam (`npm test` em `cloud/dashboard/`)
- [ ] Zero lint warnings (`npm run lint` no dashboard, se existir; tsc strict passa via `npm run build`)
- [ ] Backward compatibility preservada: rotas, /v1/auth/session/token, Kratos session, BroadcastChannel
- [ ] LOC do dashboard reduzidas de ~2.914 para ≤1.800 (gap ≥37%)
- [ ] Bundle size dentro de ±10% do baseline
- [ ] Zero termos internos na UI (Rule 3) — validado via smoke test em T6.5
- [ ] **Evidence**: N/A — este plano não afeta scores de maturity (UI-only refactor; maturity scores são de domínios backend per `.claude/knowledge-base/audits/2026-05-11/`)
- [ ] **Cross-validation `/cross-validation 2026-05-23-dashboard-loopzero-ui-migration` passou** — zero CRÍTICO, zero GRAVE
- [ ] **Dogfood Gate** abaixo PASSOU

## Dogfood Gate (MANDATORY)

> Plano UI-only requer dogfood com escopo adaptado: foco em fluxos visuais e auth E2E.

### Execução

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up
task doctor  # verify health

# 2. Build + deploy dashboard image
cd cloud/dashboard && docker build -t loopzero-dashboard:test .
# Apply manifest local que aponta para a image (depende de tilt config)

# 3. Smoke test manual completo (T6.5 estendido)
# Abrir https://cloud.loopzero.local em Chrome dark mode
# Para cada rota das 13: navegar + interagir + screenshot
```

### Pass Criteria

- [ ] `task doctor` reporta 38/0 (ou baseline atual)
- [ ] Login Kratos (email+senha) → token JWT obtido → dashboard renderiza Overview
- [ ] Login GitHub OAuth → mesmo resultado
- [ ] Logout em uma aba → outra aba detecta (BroadcastChannel) → redireciona /login
- [ ] Navegar todas as 13 rotas sem erro de console
- [ ] Recovery flow: clicar "Forgot password?" → renderiza RecoveryPage → submit email
- [ ] Verification banner aparece quando user.email_verified=false
- [ ] Rule 3: grep no DOM rendered não retorna `Kubernetes|ArgoCD|cell|pgvector|Hydra|Kratos|gitops`
- [ ] No regression: `cd cli && npm test` (CLI engine), `cd api && go test ./...` continuam verdes (dashboard não tocou backend)

### On Failure

S0/S1 findings → fix → re-run smoke test. S2-S4 documentados em `docs/reviews/2026-05-23-dashboard-migration-dogfood.md`.

---

## Notas operacionais

### Riscos identificados

| Risco | Mitigação |
|---|---|
| API de primitive `@loopzero/ui` divergir do esperado | T0.1 audit + ler `.d.ts` antes de cada substituição |
| ThemeProvider conflita com `class="dark"` manual no `<html>` | T0.2 remove o class manual |
| Bundle size cresce >10% | T6.3 valida; mitigação: tree-shake check, lazy-load composites pesados |
| Smoke test descobre regressão visual grave | Aceitar reverter para baseline + iterar; pre-release bias permite redesign |
| `@loopzero/ui@next` recebe breaking change durante migração | Pin exata versão em package.json até completar; depois liberar Renovate |
| `@loopzero/memory` integration requested mid-plan | Recusar — fora de escopo (D6); abrir novo plano |
| Páginas órfãs (account, error) confundem usuário | Marcar para revisão pós-plano em backlog |

### Alternativas rejeitadas

| Alternativa | Por que rejeitada |
|---|---|
| Copy-paste shadcn registry de cada componente | D2 — perde update automático, complica manutenção |
| Suporte multi-tema com toggle | D1 — sponsor explicitamente recusou; escopo 3× maior |
| Manter componentes locais e apenas estilizar com tokens | KISS violado — duplica esforço de manutenção |
| Migrar para Next.js durante o refactor | YAGNI — fora do escopo, T3 separado |
| Migrar para pnpm workspace + file:../loopzero-ui | Sponsor escolheu npm install puro; CI mais simples |
| Adicionar Memory editor (preparar para v0.5) | D6 — @loopzero/memory ainda private; não wirar UI sem backend |
| Reescrever páginas órfãs (account, error) | YAGNI — não roteadas, não consumidas |

### Edge cases capturados (Pre Edge Case Review)

Estes serão expandidos via `/edge-case-plan` após save do plano:

1. **Hidratação React 19 com ThemeProvider**: se ThemeProvider lê `localStorage` em SSR-mismatched, hydration warning. Mitigation: `<ThemeScript>` se necessário.
2. **CSS variable cascade**: `tokens.css` → `styles.css` → `tailwind.config.ts` preset → utility classes. Verificar ordem em `index.css`.
3. **Form CSRF token**: `FormField` de `@loopzero/ui` pode não aceitar `hidden type=text` — verificar.
4. **Loading states cross-page**: `Loader2` (lucide) usado em 5+ páginas. `@loopzero/ui` pode ter Skeleton/Spinner próprio; usar para coerência.
5. **Recovery page sem testes hoje**: novo arquivo de teste; covered por T3.4 TDD.
6. **Bundle delta inesperado**: capturar baseline ANTES de T0.1 (commit hash); comparar ao final.
7. **i18n**: dashboard hoje é EN-only. `@loopzero/ui` pode ter strings hard-coded. Verificar se quebra UX em locale PT-BR.
8. **Print/responsive**: dashboard hoje assume desktop. Mobile não está em escopo deste plano (próximo plano se cliente pedir).

### Architecture Docs (post-plan)

> Este refactor é UI-only e não muda boundaries de Container/Componente entre cloud/, api/, operators/. A C4 documentação de `docs/architecture/` para `cloud/dashboard` é minimal e seria gerada via `/architecture-docs` em Mode 2 (AS-IS) e Mode 3 (PROPOSED). Para este plano, considera-se que o impacto arquitetural é encapsulado em (a) nova dependência externa `@loopzero/ui` no container `loopzero-dashboard` e (b) deleção do submódulo `src/components/ui/`. Skip pragmatic do `/architecture-docs` invocation registrado aqui.

### Voice anchors (Locked Narrative)

Apenas páginas marketing-facing (login, register, verification, recovery, device-success) seguem voz aspiracional (HERO/BODY tone). Páginas internas (overview, projects, environments, billing, domains, team, settings, profile) ficam técnico-direto. Microcopy específica:

- LoginPage HERO: "Where AI agents actually run." (já em uso em PITCH.md de cloud/)
- LoginPage sub: "Deploy your apps in under 5 minutes." (já no atual)
- EmptyState OverviewPage: "No deploys yet. Run `loopzero deploy` to get started." (already CR-9 clean)
- EmptyState ProjectsPage: "No projects yet. Run `loopzero init` to create your first." (already clean)
- VerificationBanner: "Verify your email to unlock all features." (gentle, no alarm)

Internal terms baniidos (re-statment de Rule 3 + CR-9): `Kubernetes`, `cluster`, `ArgoCD`, `Cosign`, `Argo Rollouts`, `KEDA`, `Gitea`, `MinIO`, `ESO`, `cell`, `Pulumi`, `Helmfile`, `Tilt`, `Ory`, `Traefik`, `Hydra`, `Kratos` (sim, Kratos é tecnologia interna — usar "Sign in" em vez de "Authenticate with Kratos").

---

## Status

**Created**: 2026-05-23
**Updated**: 2026-05-23 23:55 UTC (v1.3 — Opção A Tailwind v4 inline ratificada; T0.0.bis adicionada; ADR D8; T0.1+T0.3 ajustadas)
**Author**: claude-opus-4-7 via `/to-plan`
**Sponsor**: paulo (paulohenriquevn@gmail.com) — ratificou Opção A 2026-05-23
**Tier**: T3 (refactor stack frontend + nova dep `@loopzero/ui` + dep upgrade Tailwind v3→v4)
**Estimated effort**: 4-7 sessões de trabalho (Phase 0 cresce 1 task com T0.0.bis; Phases 3/4/5 paralelizáveis)
**Edge-Case-Review**: PASSED v1.1 (`docs/plans/2026-05-23-dashboard-loopzero-ui-migration-edge-cases.md`, 18 edges: 8 MUST FIX incorporados, 7 SHOULD TEST, 3 DOCUMENT). v1.2 não exigiu re-review (mecânico). **v1.3 RECOMENDA re-run** — T0.0.bis é nova task funcional com risk surface próprio (codemod outputs, breaking changes Tailwind v3→v4, darkMode strategy migration). Sponsor pode pular se aceitar smoke test em T0.0.bis task 9 como compensação.
**Upstream dependency status**: ✅ COMPLETO — 14 primitives requested entregues em `@loopzero/ui@0.9.0-next.0` (Briefs #1+#2+#3 shipados 2026-05-23). 116 componentes totais, 1.577 testes, 237 axe stories.
**Próximo gate**: governança T3 meeting (ou sponsor-direct-approval pattern conforme MEET-2026-05-13-ed25519-sponsor-approval.md precedent) → implementação Phase 0→6 (começando com T0.0 baseline + T0.0.bis Tailwind v4 upgrade) → `/cross-validation` → `/dogfood`
**Bloqueador atual**: nenhum técnico — pronto para iniciar Phase 0. Falta apenas autorização formal do sponsor para começar implementação.
