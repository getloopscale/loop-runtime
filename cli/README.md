# `@loopzero/cli` — deploy CLI for LoopZeroCloud

The `loopzero` CLI is the deploy companion to the [LoopZeroKit OSS framework](https://github.com/loopzero/loopzerokit). It authenticates with LoopZeroCloud, uploads your application bundle + `.loopzero/services.json` artifact, streams the build/deploy events, and surfaces logs and rollback controls.

## Install

```bash
# In your project (recommended — version-pinned in package.json):
npm install --save-dev @loopzero/cli

# Or globally:
npm install -g @loopzero/cli
```

You also need LoopZeroKit installed so `loopzero deploy` can build the `.loopzero/services.json` artifact automatically. In typical LoopZeroKit projects it is already a dev dependency:

```bash
npm install loopzerokit
```

## Quickstart (Plan v1.1 zero-config)

```bash
loopzero login                    # authenticate once per machine
loopzero                          # zero-config deploy — builds + uploads in one step
```

`loopzero` without a positional subcommand routes to `loopzero deploy`. When `.loopzero/services.json` is missing the CLI invokes `loopzerokit build --target loopzero-cloud` automatically as a subprocess (see `runLoopZerokitBuild` source for the exact spawn defaults).

The legacy two-step sequence still works unchanged:

```bash
loopzerokit build --target loopzero-cloud   # explicit build
loopzero deploy --prebuilt              # upload only — skip auto-build
```

## CI/CD with prebuilt artifacts

CI pipelines that split build and deploy across stages (Vercel-style) use the `--prebuilt` or `--skip-build` flag:

```yaml
jobs:
  build:
    steps:
      - run: npm ci
      - run: npx loopzerokit build --target loopzero-cloud
      - uses: actions/upload-artifact@v4
        with:
          path: .loopzero/services.json
  deploy:
    needs: build
    steps:
      - run: npm ci
      - uses: actions/download-artifact@v4
      - run: npx @loopzero/cli deploy --prebuilt
        env:
          LOOPZERO_TOKEN: ${{ secrets.LOOPZERO_TOKEN }}
```

`--prebuilt` is an alias of `--skip-build` (Vercel parity). Both forms are accepted; choose whichever your team's CI templates use.

## Tuning subprocess defaults

Three environment variables tune the `loopzerokit build` subprocess that `loopzero deploy` runs:

| Variable | Default | Purpose |
|----------|---------|---------|
| `LOOPZEROKIT_BUILD_TIMEOUT_MS` | `600000` (10 min) | Maximum build duration. Long-running monorepo builds often need to extend this. |
| `LOOPZEROKIT_BIN` | (auto-resolved) | Absolute path to the `loopzerokit` binary. Useful when `node_modules/.bin/loopzerokit` cannot be auto-located (Docker volumes, non-standard installs). |
| `LOOPZEROKIT_NON_INTERACTIVE` | `1` (forced) | Always `1` in the subprocess — prevents transitive npm tools from blocking on prompts. Set by the CLI; do not override. |

## Custom `distDir`

When `loopzero.config.ts` declares a non-default `distDir`, the auto-discovery for `.loopzero/services.json` will not find the artifact. Use `--services-json <path>` to point at the explicit emit location:

```bash
# loopzero.config.ts → distDir: 'out'
loopzero deploy --services-json ./out/services.json
```

## Rebuilding after `loopzero.config.ts` edits

The auto-build trigger only runs when `.loopzero/services.json` is absent. If you edit `loopzero.config.ts` and want the new shape on LoopZeroCloud, delete the cached artifact first:

```bash
rm -rf .loopzero && loopzero deploy
# OR
loopzerokit build --target loopzero-cloud && loopzero deploy --prebuilt
```

This matches Vercel's "trust the artifact" semantics — staleness detection across `package.json`, source files, and service definitions is intentionally not in scope (per Plan v1.1 ADR D2).

## All commands

```
loopzero                       # zero-config deploy (Plan v1.1 T4.1)
loopzero deploy [opts]         # build + upload + stream events
loopzero login                 # OAuth flow
loopzero logout                # clear local credentials
loopzero whoami                # current identity + tenant
loopzero logs [opts]           # tail or fetch build/runtime logs
loopzero rollback [opts]       # restore a previous release
loopzero promote [opts]        # staging → production
loopzero status [opts]         # show current release state
# ... and more — `loopzero --help` for the full list
```

## Breaking change disclosure (Plan v1.1 — intentional)

`loopzero` without a positional subcommand previously emitted `Unknown command: undefined. Run: loopzero --help` and exited 1. From v1.1 onward it delegates to `loopzero deploy` per Vercel-parity zero-config. Scripts that relied on the previous behavior for usage discovery should migrate to `loopzero --help`.

To detect the version boundary in a shell script:

```bash
loopzero --version | awk -F. '{ if ($1 == 0 && $2 < 2) print "old"; else print "new"; }'
```

## Source

This package lives in [loopzero-cloud/loopzero/cli](https://github.com/loopzero/loopzero-cloud). See the `docs/architecture/` folder for the integration flow with the LoopZeroKit framework.
