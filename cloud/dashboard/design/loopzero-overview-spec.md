# Prompt: LoopZero Dashboard — Tela "Overview"

> Cole o conteúdo da seção "## Prompt" em diante no Claude Design / Claude Code.

## Paleta LoopZero (tokens)

| Token | Valor | Uso |
|---|---|---|
| `--primary` | `#7C3AED` | Ação/marca: botões primários, links, item ativo, logo |
| `--primary-soft` | `#a78bfa` | Texto de link/realce sobre dark, ícone do item ativo |
| `--accent` | `#C96442` | Acento pontual: badges de status, dots, destaques |
| `--bg-dark` | `#0A0A0A` | Fundo da página (modo escuro — padrão) |
| `--bg-light` | `#FFFFFF` | Fundo da página (modo claro) |
| `--surface` | `#0D0D0D` | Superfície de cards (dark) |
| `--surface-active` | `#1c1226` | Fundo do item de nav ativo (dark) |
| `--border` | `#1F1F1F` | Bordas e divisores (dark) |
| `--track` | `#1A1A1A` | Trilho de barra de progresso |
| `--text` | `#EDEDED` | Texto primário |
| `--text-secondary` | `#A1A1A1` | Texto secundário |
| `--text-tertiary` | `#6E6E6E` | Hints/timestamps |

**Regra de cor:** roxo = ação e marca; coral = status/acento pontual, NUNCA fundo de área grande. Mantenha o dark respirando.

---

## Prompt

Build a single-page **dark-mode SaaS dashboard "Overview" screen** for a product called **LoopZero** (a CLI-first Kubernetes PaaS — "Vercel for developers who have a backend"). Desktop-first (min width 1280px), React + Tailwind. Pixel-faithful to the structure below. Default theme is dark; also expose a light variant.

### Design tokens

Primary `#7C3AED` (actions, brand, active nav, links, logo). Primary-soft `#a78bfa` (link text and active icon on dark). Accent `#C96442` (status pills, indicator dots, pointed highlights ONLY — never large fills). Page bg dark `#0A0A0A` / light `#FFFFFF`. Card surface `#0D0D0D`. Borders `#1F1F1F`. Progress track `#1A1A1A`. Text `#EDEDED` / secondary `#A1A1A1` / tertiary `#6E6E6E`. Radius 8px (cards/inputs/buttons), 6px (small pills). Flat surfaces — no gradients, no shadows except focus rings.

### Layout: fixed left sidebar (240px) + fluid main area

#### Sidebar (240px, full height, right border 1px `#1F1F1F`)

1. **Account switcher**: 24px circular avatar (primary fill) + name `paulohenriquevn` (medium) + outlined pill `Hobby` in accent color + selector chevron at right edge.
2. **Search**: full-width outlined input, magnifier + placeholder `Find…`, small `F` hint chip at right.
3. **Primary nav** (icon 16px + label). Active item `Projects` uses `--surface-active` bg with a **2px left border in primary** and the icon tinted primary-soft; others are secondary text. Order: Projects *(active)*, Deployments, Logs, Analytics, Speed Insights, Observability *(right chevron)*, Firewall, CDN.
4. Divider.
5. **Secondary nav**: Environment Variables, Domains, Integrations, Storage, Flags, Agent *(chevron)*, AI Gateway *(chevron)*, Sandboxes, Workflows.
6. Divider, then **bottom group**: Usage, Support, Settings *(chevron)*.
7. **Footer pinned to bottom**: second 24px avatar + `paulohenrique-vn` + a `⋯` icon + a bell icon with an accent unread dot.

#### Main area

**Top bar** (sticky, 1px bottom border, ~52px, padding-x 24px): left `All Projects` + selector chevron; center `Overview` (medium); right `⋯` icon button.

**Toolbar** (padding-top 24px): full-width search input (`Search Projects…`) + square filter icon button + grid/list view toggle pair (grid active) + **`Add New…`** button on far right with trailing chevron, **filled in primary `#7C3AED`, white text**.

**Body grid**: two columns, gap 24px. Left (~1.2fr): Usage then Alerts. Right (~1fr): Projects then Recent Previews.

##### Usage card
Section label `Usage`. Card (surface bg, 1px border, radius 12px). Header: `Last 30 days` (medium) + outlined `Upgrade` pill tinted **primary-soft**. Metric rows, each = name (secondary) + value (secondary) + a 4px progress track (`#1A1A1A`, rounded, 0% fill). Each name has a tiny `(i)` icon. Metrics:
- `Fast Data Transfer` — `0 / 100 GB`
- `Fast Origin Transfer` — `0 / 10 GB`
- `Edge Requests` — `0 / 1M`
- `Edge Request CPU Duration` — `0 / 1h`
Centered chevron-down "expand" affordance half-overlapping the bottom border.

##### Alerts card
Section label `Alerts`. Centered empty state: bold `Get alerted for anomalies`; secondary subtitle `Automatically monitor your projects for anomalies and get notified.`; **`Upgrade to Pro`** button filled in **primary**, white text, centered.

##### Projects card
Section label `Projects`. One project row: 30px rounded-square logo (primary fill with a `T` glyph — LoopZero brand) + name `project-u7r43` (medium) + subdomain `project-u7r43.loopzero.app` (secondary) + small **accent dot** status indicator + `⋯` at right. Below: blue/primary-soft link `Connect Git Repository`, then tertiary `24h ago`.

##### Recent Previews card
Section label `Recent Previews`. Centered empty state: faint eye icon (~22px, tertiary) + secondary text `Preview deployments that you have recently visited or created will appear here.`

### States to preserve
Fresh/empty account: all usage bars at 0%, single project with no Git connected (hence `Connect Git Repository`), both upgrade CTAs visible (free `Hobby` tier). Generous whitespace, flat high-contrast dark aesthetic, subtle `#1F1F1F` borders. Coral appears ONLY on the `Hobby` pill and the project status dot — everywhere else action color is the purple primary.

### Deliverable
Single responsive component file, dark by default with a light-mode variant driven by the tokens above. Use a generic outline icon set; the brand glyph is the `T` mark on primary.
