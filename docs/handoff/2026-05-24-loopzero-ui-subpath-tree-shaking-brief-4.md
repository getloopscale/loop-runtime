# Brief #4: Fix subpath exports so tree-shaking actually works (build pipeline defect)

> **Audience:** the `@loopzerokit/ui` maintainer team (or a Claude/Codex agent in the `loopzero-ui` repo).
> **Output:** tsup config update + `package.json#exports` migration + 1 ADR + bundle-delta evidence.
> **Issued by:** LoopZeroCloud dashboard team (consumer), 2026-05-24.
> **Priority:** MEDIUM — not blocking Phase 0 (already shipped via MEET-ASYNC-AMENDMENT-002 with a 240 KB brotli hard gate that current 178 KB brotli passes). Ship before LoopZeroCloud scales past **6-8 more pages** OR when any other consumer hits the same wall.
> **Companion to:** Brief #1 (`2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md`) + Brief #2 (`2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief-2.md`) + Brief #3 (`2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief-3.md`).
> **Tracks:** LoopZeroCloud `MEET-ASYNC-AMENDMENT-2026-05-24-002` § Follow-up #2.

---

## TL;DR for the impatient

`@loopzerokit/ui@0.9.0-next.0` declares dozens of subpath exports (`./alert`, `./button`, `./card`, …) in `package.json#exports`, BUT they ALL resolve to the same `./dist/index.js` (the full barrel). Result: `import { Alert } from '@loopzerokit/ui/alert'` is **identical** to `import { Alert } from '@loopzerokit/ui'` for the bundler — both pull in the entire 240 KB minified barrel and rely on tree-shaking to drop unused code, which fails because of barrel re-exports + `sideEffects: ["**/*.css"]`.

Empirical impact on LoopZeroCloud dashboard (consumer using ~30 of 116 components):

```
Current  @loopzerokit/ui chunk:  43.05 KB brotli (240.56 KB minified)
Expected with real subpath:  ~28-33 KB brotli (~150-180 KB minified)
Savings:                     10-15 KB brotli per consumer
```

The fix is structural — it requires adding tsup `entry` map entries per primitive + updating `package.json#exports` to point to the per-component dist files. The pattern is **already used correctly** for `whiteboard/index`, `slide/index`, and `slide/plugins/*` — this brief asks to extend it to the rest of the primitives.

---

## Quality bar (same as Briefs #1/#2/#3)

This brief does NOT add new components. It changes the **publishing pipeline**. Constraints:

- Zero breaking changes to the barrel API — `import { X } from '@loopzerokit/ui'` keeps working
- Subpath imports must be **smaller**, not just "exist" — verify with bundle-delta evidence
- Per-component dist files must NOT import each other through the barrel (would defeat tree-shaking)
- CSS bundling unchanged — `dist/styles.css` + `dist/components.css` remain barrel-level (per-component CSS would explode the network waterfall)
- `sideEffects: ["**/*.css"]` stays — CSS imports must NOT be tree-shaken
- Each new dist subpath gets a typed entry in `package.json#exports`
- Existing isolated bundles (`whiteboard`, `slide`, `slide-deck`, `vite-plugin`, `preset-v3-legacy`) untouched — they already do this right

---

## Empirical evidence (LoopZeroCloud measurement, 2026-05-24)

### The defect

`cloud/dashboard/package.json` uses **barrel imports only**:

```ts
import { Card, Button, Avatar, Badge, Alert, ConfirmDialog, DangerZone,
         EmptyState, Input, Select, Table, Toaster, ThemeProvider,
         PlanBadge, UsageMeter, StatusDot, CopyButton, CodeBlock,
         ProjectCard, violetForge } from '@loopzerokit/ui';
```

After running `vite build` with manual chunk splitting:

```
dist/assets/loopzero-ui-ujXAx18L.js          240.56 KB │ gzip: 52.28 KB │ brotli: 43.05 KB
```

This 240 KB minified contains **all 116 components**, not just the ~30 the dashboard actually uses.

### Proof the subpath exports are cosmetic

Inspecting `node_modules/@loopzerokit/ui/package.json` (relevant excerpt):

```json
{
  "exports": {
    ".": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
    "./alert": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
    "./button": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
    "./card": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
    "./avatar": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
    "...80+ more entries...": "...all point to ./dist/index.js..."
  }
}
```

All subpath entries point to **the same** `./dist/index.js`. There is no per-component dist file. Switching the consumer to `import { Alert } from '@loopzerokit/ui/alert'` would change the import specifier but produce **byte-identical** bundle output.

Confirmed by inspecting `dist/`:

```
$ ls dist/ | head
components.css
fonts/
fonts-cdn.css
fonts.css
index.d.ts
index.js
preset.css
slide/         ← isolated subpath (correct pattern)
slide-deck/    ← isolated subpath (correct pattern)
whiteboard/    ← isolated subpath (correct pattern)
```

No `dist/alert/`, `dist/button/`, `dist/card/` etc. exist.

### Why tree-shaking from the barrel fails

Tree-shaking ESM modules works **only if**:

1. The bundler can statically determine which exports are used (✅ Vite/Rollup does this)
2. The library uses pure ESM with no side effects in module evaluation (⚠️ partial — `sideEffects: ["**/*.css"]` correctly preserves CSS but `index.ts` itself runs side effects)
3. **Barrel re-exports are transparent to the bundler** (❌ — this is the failure mode)

The barrel `src/index.ts` does roughly:

```ts
export * from './components/primitives/alert/index.js';
export * from './components/primitives/button/index.js';
// ... 100+ more
```

When Vite/Rollup analyzes the consumer's `import { Alert } from '@loopzerokit/ui'`, it must trace through the entire barrel chain to determine which leaves are actually used. This works **in loopzerory** but breaks in practice because:

- `forwardRef<HTMLDivElement, AlertProps>(...)` is treated as a side-effectful call expression by Rollup's conservative analysis
- Compound components attached via `Object.assign` (`Card.Header = ...`) trigger the bailout
- Even one `console.log` or `if (process.env...)` inside the barrel chain disables shaking for entire subtrees
- CSS-in-JS or theme-token imports cascade through the barrel

Empirically: **0 KB** is dropped from the @loopzerokit/ui chunk regardless of how few components LoopZeroCloud imports.

---

## The fix (3 phases, staged)

### Phase 1 — Auto-glob tsup entries per primitive

Update `tsup.config.ts` to emit one bundle per primitive folder:

```ts
import { defineConfig } from "tsup";
import { readdirSync } from "node:fs";
import { join } from "node:path";

// Auto-discover all primitive + composite folders that ship a top-level
// `index.ts`. Skips folders without an entry file. Names use kebab-case
// matching the existing `package.json#exports` keys.
function discoverEntries(baseDir: string, prefix: string): Record<string, string> {
  const entries: Record<string, string> = {};
  for (const dirent of readdirSync(baseDir, { withFileTypes: true })) {
    if (!dirent.isDirectory()) continue;
    const indexTs = join(baseDir, dirent.name, "index.ts");
    try {
      // existsSync inline so we skip folders with no entry (drafts, themes)
      readdirSync(join(baseDir, dirent.name)).includes("index.ts") &&
        (entries[`${prefix}/${dirent.name}/index`] = indexTs);
    } catch { /* skip */ }
  }
  return entries;
}

const primitiveEntries = discoverEntries(
  "src/components/primitives",
  "primitives",
);
const compositeEntries = discoverEntries(
  "src/components/composites",
  "composites",
);

export default defineConfig({
  entry: {
    index: "src/index.ts",
    // Existing isolated bundles (unchanged):
    "whiteboard/index": "src/components/primitives/whiteboard/index.ts",
    "slide/index": "src/components/primitives/slide/index.ts",
    "slide/plugins/shiki/index": "src/components/primitives/slide/plugins/shiki/index.ts",
    "slide/plugins/math/index": "src/components/primitives/slide/plugins/math/index.ts",
    "slide/plugins/mermaid/index": "src/components/primitives/slide/plugins/mermaid/index.tsx",
    "slide/plugins/emoji/index": "src/components/primitives/slide/plugins/emoji/index.ts",
    "slide-deck/index": "src/components/composites/slide-deck/index.ts",
    "vite-plugin": "src/vite-plugin.ts",
    "preset-v3-legacy": "src/preset-v3-legacy.ts",
    // NEW — auto-discovered primitives + composites:
    ...primitiveEntries,
    ...compositeEntries,
  },
  format: ["esm"],
  dts: true,
  sourcemap: true,
  clean: true,
  splitting: true,                  // ← CHANGE: was false; enable chunk splitting so
                                    //    shared dependencies (forwardRef, cn) dedupe
                                    //    into a common chunk instead of being inlined
                                    //    into every primitive bundle
  treeshake: true,
  external: [
    "react",
    "react-dom",
    /^react\/jsx-runtime/,
    // ... existing externals ...
  ],
  // onSuccess unchanged — CSS bundling stays barrel-level.
});
```

**Resulting `dist/` after this phase:**

```
dist/
├── index.js                  ← still the full barrel (back-compat)
├── index.d.ts
├── _chunks/                  ← shared chunks (forwardRef wrappers, cn helper, …)
├── primitives/
│   ├── alert/
│   │   ├── index.js          ← Alert + AlertProps only
│   │   └── index.d.ts
│   ├── button/
│   ├── card/
│   └── ... (one folder per primitive)
├── composites/
│   ├── account-menu/
│   ├── project-card/
│   └── ...
├── whiteboard/, slide/, slide-deck/   (unchanged)
└── components.css, tokens.css, ... (unchanged)
```

### Phase 2 — Migrate `package.json#exports` to point at the new files

Replace cosmetic entries:

```diff
- "./alert": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
+ "./alert": { "types": "./dist/primitives/alert/index.d.ts", "import": "./dist/primitives/alert/index.js" },

- "./button": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
+ "./button": { "types": "./dist/primitives/button/index.d.ts", "import": "./dist/primitives/button/index.js" },

- "./account-menu": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
+ "./account-menu": { "types": "./dist/composites/account-menu/index.d.ts", "import": "./dist/composites/account-menu/index.js" },
```

Migration script (generates the map from the new `dist/` tree, no hand-editing needed):

```ts
// scripts/regen-subpath-exports.ts
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const pkg = JSON.parse(readFileSync("package.json", "utf-8"));
const exports: Record<string, unknown> = {
  ".": { types: "./dist/index.d.ts", import: "./dist/index.js" },
  "./styles.css": "./dist/styles.css",
  // ... preserve all CSS + theme + isolated subpath entries ...
};

for (const layer of ["primitives", "composites"] as const) {
  for (const dir of readdirSync(`dist/${layer}`, { withFileTypes: true })) {
    if (!dir.isDirectory()) continue;
    const key = `./${dir.name}`;
    exports[key] = {
      types: `./dist/${layer}/${dir.name}/index.d.ts`,
      import: `./dist/${layer}/${dir.name}/index.js`,
    };
  }
}

pkg.exports = exports;
writeFileSync("package.json", JSON.stringify(pkg, null, 2) + "\n");
```

Wire into `package.json#scripts`:

```diff
  "scripts": {
-   "build": "tsup",
+   "build": "tsup && pnpm tsx scripts/regen-subpath-exports.ts",
```

### Phase 3 — Bundle-delta evidence (the acceptance criterion)

Before merging, MUST produce a measurable savings report against LoopZeroCloud dashboard as the canonical consumer. Workflow:

```bash
# In loopzero-ui/, build with Phase 1 + 2 changes
cd loopzero-ui && pnpm build

# Link into LoopZeroCloud dashboard
cd ../loopzero/cloud/dashboard && pnpm link ../../../loopzero-ui

# Switch top 10 imports to subpath (manual edit in 13 page files)
sed -i 's|import { Alert } from "@loopzerokit/ui"|import { Alert } from "@loopzerokit/ui/alert"|g' src/**/*.tsx
# ... repeat for top 10 ...

# Measure
npm run build && npm run size
```

Expected outcome (passing condition for merge):

```
BEFORE (barrel only):
  loopzero-ui chunk:  43.05 KB brotli  /  240.56 KB minified

AFTER (subpath top 10):
  loopzero-ui chunk:  ≤33 KB brotli   /  ≤180 KB minified
  Savings:        ≥10 KB brotli   /  ≥60 KB minified
```

If savings < 8 KB brotli, the migration is **not worth shipping** — investigate why tree-shaking still fails (probably leftover side-effects in primitive `index.ts` files; fix or document why).

---

## Acceptance criteria for merge

- [ ] `tsup.config.ts` emits per-primitive + per-composite dist files
- [ ] `dist/primitives/<name>/index.js` exists for every primitive folder in `src/components/primitives/`
- [ ] `dist/composites/<name>/index.js` exists for every composite folder in `src/components/composites/`
- [ ] `package.json#exports` entries point to per-component dist files (NO duplicates pointing to `./dist/index.js`)
- [ ] `import { X } from '@loopzerokit/ui'` (barrel) still works identically
- [ ] `import { X } from '@loopzerokit/ui/<x>'` (subpath) works and resolves to smaller dist file
- [ ] **Bundle-delta evidence**: LoopZeroCloud `loopzero-ui` chunk drops ≥10 KB brotli after switching its top 10 imports to subpath
- [ ] `pnpm test` passes — no test regressions
- [ ] `pnpm typecheck` passes — types still resolve via subpath
- [ ] `pnpm ladle build` passes — Ladle stories still resolve all imports
- [ ] CHANGELOG entry under `[Unreleased]` documenting the new subpath structure + bundle delta
- [ ] ADR in `.claude/knowledge-base/decisions/` (or repo equivalent) titled `subpath-exports-per-component` documenting:
  - Decision (auto-glob per-component dist via tsup)
  - Alternatives rejected (manual entry list; single barrel; pre-bundled UMD)
  - Trade-offs (more dist files = slightly larger `node_modules`; mitigated by being ESM-only)
  - Tree-shaking validation methodology

---

## Anti-patterns the team should avoid

Based on Brief #1/#2/#3 quality bar:

- ❌ Adding new subpath entries to `package.json#exports` WITHOUT corresponding dist files (the current defect — exports pretending to work)
- ❌ Per-component CSS bundles (explodes network waterfall; CSS stays barrel)
- ❌ Removing the main barrel `import { X } from '@loopzerokit/ui'` (breaking change; many consumers, including LoopZeroCloud, use it)
- ❌ Vendoring `react` / `react-dom` / `jsx-runtime` into per-component bundles (must stay `external`)
- ❌ Per-component re-export back through the barrel inside primitive `index.ts` (e.g., `export * from '../../../index.js'`) — defeats the purpose
- ❌ Shipping without bundle-delta evidence — "subpath works" is not the goal; "subpath measurably reduces consumer bundle" is
- ❌ Manual per-primitive `tsup` entries hand-maintained (drift inevitable; use auto-glob)

---

## Why this is MEDIUM priority (not high)

LoopZeroCloud's current state per `MEET-ASYNC-AMENDMENT-2026-05-24-002`:

```
Current TOTAL initial JS:  178.28 KB brotli
Hard gate (CI):            240.00 KB brotli  (26% headroom)
Per-chunk @loopzerokit/ui:      43.05 KB brotli  / cap 50 KB  (14% headroom)
```

We have **enough headroom** to ship Phase 0 of the consumer migration WITHOUT the subpath fix. The fix becomes a hard requirement when:

- LoopZeroCloud adds **6-8 more pages** (each ~2-5 KB brotli of incremental loopzero-ui surface) → approaches the per-chunk cap
- A second consumer adopts @loopzerokit/ui and asks the same question
- Any consumer reports `pnpm install` time complaints (per-component subpaths SHRINK `node_modules` a bit)
- @loopzerokit/ui itself adds Tier 2 components (charts, code editor, rich text) that would bloat the barrel beyond reasonable per-route inclusion

Until then, the existing 240 KB hard gate at the consumer side catches regressions.

---

## Related work + references

- LoopZeroCloud MEET amendment: `MEET-ASYNC-AMENDMENT-2026-05-24-002-dashboard-bundle-gate-redefinition.md` § Follow-up #2 (this brief is the named follow-up)
- LoopZeroCloud size-limit config: `cloud/dashboard/package.json#size-limit` (9-entry gate including `@loopzerokit/ui` cap)
- Existing correct pattern in `tsup.config.ts`: `whiteboard/index`, `slide/index`, `slide/plugins/*`, `slide-deck/index`
- Industry precedents for per-component subpath:
  - `@radix-ui/react-*` — each primitive is its own published package
  - `lucide-react` — supports `import { Icon } from 'lucide-react/icons/icon-name'` for tree-shaking
  - `@mui/material` — `import { Button } from '@mui/material/Button'` is the recommended pattern for bundle size
  - `react-aria-components` — per-component subpath imports
  - `@headlessui/react` — barrel only; documented bundle-size trade-off
- Tree-shaking deep-dive: `https://webpack.js.org/guides/tree-shaking/` + Rollup's `treeshake.moduleSideEffects` docs
- LoopZeroCloud's 30-component usage inventory (for migrating top 10 imports in Phase 3):

```
Used:  Avatar, Badge, Alert, Button, Card, CodeBlock, ConfirmDialog,
       CopyButton, DangerZone, EmptyState, Input, PlanBadge,
       ProjectCard, Select, Sidebar, StatusDot, Table, ThemeProvider,
       Toaster, Topnav, UsageMeter, violetForge, type Project,
       type PlanTier, type UsageMetric, type StatusKind
       (~30 distinct exports across 13 dashboard pages)

NOT used (86+ components in the @loopzerokit/ui catalog):
       Whiteboard, Slide*, AgentComposer, AgentEditor, ChatComposer,
       all other AI-agent primitives + composites — these are pulled
       INTO the barrel for nothing.
```

The savings come from the 86+ unused components being correctly dropped, not from optimizing the 30 used ones.

---

## What sponsor (LoopZero team) ratifying this brief would mean

If the @loopzerokit/ui maintainer accepts this brief and ships a `0.10.0-next.0` (or `0.9.1`) release with the fix:

1. LoopZeroCloud opens a follow-up plan to migrate its 30 imports to subpath form
2. Bundle gate `cap 50 KB → cap 35 KB` for the @loopzerokit/ui chunk in `cloud/dashboard/package.json#size-limit`
3. Total hard gate `240 KB → 220 KB` (matching the original Sponsor "ideal" before MEET-001 / MEET-002 floor)
4. CHANGELOG entry crediting the upstream fix
5. Closes follow-up #2 in MEET-002 + closes the only remaining LOC-style "wish I could shrink this" gap from MEET-001

If the maintainer pushes back on the migration cost ("100+ primitives = lots of dist files"), the staged Phase 3 alternative is: ship subpath ONLY for the top 30 most-imported components first (the ones LoopZeroCloud + future consumers will hit), defer the long tail. Half the benefit at quarter the work.

---

## Companion to prior briefs

| Brief | Subject | Status |
|-------|---------|--------|
| #1 (2026-05-23) | 8 missing primitives | ✅ shipped in `@loopzerokit/ui@0.7.0` |
| #2 (2026-05-23) | 4 more primitives (Table, Topnav, CodeBlock, …) | ✅ shipped in `@loopzerokit/ui@0.8.0-next.0` |
| #3 (2026-05-23) | 2 deferred primitives (Alert, Pagination) | ✅ shipped in `@loopzerokit/ui@0.9.0-next.0` |
| #4 (this brief, 2026-05-24) | Subpath exports tree-shaking fix | ⏳ proposed |

The previous 3 briefs added **features** to fill LoopZeroCloud's component gaps. This brief fixes a **publishing pipeline defect** that becomes visible once a consumer integrates and measures bundles. Same quality bar, different surface.

---

## Suggested commit titles for the maintainer

If shipped in a single PR:

```
fix(build): emit per-component dist files so subpath exports actually tree-shake

- tsup.config.ts: auto-glob primitives + composites into per-component entries
- package.json#exports: regenerate via scripts/regen-subpath-exports.ts to
  point at per-component dist files instead of all aliasing the barrel
- Bundle-delta evidence: LoopZeroCloud dashboard loopzero-ui chunk drops X KB brotli
  after migrating top 10 imports to subpath form
- Barrel `import { X } from '@loopzerokit/ui'` remains for back-compat
- See @loopzero/loopzero/docs/handoff/2026-05-24-loopzero-ui-subpath-tree-shaking-brief-4.md
```

If split (recommended for review):

```
1. chore(build): add scripts/regen-subpath-exports.ts (no-op until tsup updated)
2. feat(build): emit per-component dist bundles via tsup auto-glob (Phase 1)
3. fix(pkg): regenerate package.json#exports to point at per-component dist (Phase 2)
4. docs: ADR for subpath exports + CHANGELOG entry with bundle-delta evidence (Phase 3)
```

---

## Questions for the maintainer to answer in the PR description

- Did `splitting: true` cause any new chunks to leak React internals? (Should not — `external: ["react"]` prevents)
- How does `dts: true` interact with per-component entries? (tsup should emit per-component `.d.ts` automatically; verify type resolution from `'@loopzerokit/ui/alert'` works)
- Any primitives whose `index.ts` does cross-primitive imports (e.g., `card/index.ts` importing from `button/index.ts`)? Those need to be cleaned up OR resolved via shared chunk
- Did `pnpm pack` size of the published `.tgz` grow significantly? (Acceptable trade-off but worth measuring)
- Any consumers other than LoopZeroCloud + LoopZeroKit that need to be told about the new subpath structure?

---

Brief ends here. The LoopZeroCloud dashboard is happy to be the bundle-delta canary for verification — `cd cloud/dashboard && pnpm link <loopzero-ui-path> && npm run size` produces deterministic measurements your CI can grep.
