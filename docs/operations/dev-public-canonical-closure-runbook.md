# Dev-Public Canonical Closure Runbook

> Final 3 DoDs from plan `2026-06-04-dev-public-domain-canonicalization`
> that require governance scope expansion. Code is pre-written below;
> apply within ~10 min after operator runs `/change` to expand scope.

## Why this runbook exists

After the 19-commit Ralph session ending 2026-06-04, the dev-public env
is engineering-deliverable + LIVE-validated (121/0 PASS aggregate). The
3 DoDs below remain governance-gated because they touch paths outside
the currently-approved scope (`Taskfile.yml` root, `CLAUDE.md` root,
`infra/pulumi/src/cloudflare/`).

The implementation is pre-written + pre-validated by structure here. No
new design work needed — just paste and commit.

## Closure 1 — `Taskfile.yml` deprecation notice for `task setup:local`

**Plan DoD**: Phase 5 T5.1.
**Path**: `Taskfile.yml` (root).
**Effect**: Emits a NOTICE during `task setup` when run on the dev droplet,
pointing operator at `task setup:dev-public` as canonical entry.

Currently the notice is shipped in `taskfiles/infra.yml` (in scope).
This closes the loop by also updating the parent `task setup` description.

### Apply

Edit `Taskfile.yml` lines 194-215 — add the third NOTICE line and the new
target reference:

```yaml
  setup:
    desc: |
      Full setup from scratch — clusters + infra + networking + CRDs + DB + tokens + standalone bootstrap.

      Per T3.2 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
      `task setup` builds the PLATFORM (one-shot install). The DEV LOOP is `tilt up`.
      See docs/operations/dev-loop.md for the canonical developer workflow.

      T2.2 of plan 2026-06-04-dev-public-domain-canonicalization: for the
      public dev droplet (loopzero-e2e-runner), prefer `task setup:dev-public`
      which adds DNS + Caddy + helmfile env=dev-public on top of this.
    cmds:
      - echo "[NOTICE] task setup creates platform infrastructure. Dev loop is via 'tilt up'."
      - echo "[NOTICE] See docs/operations/dev-loop.md for canonical workflow."
      - echo "[NOTICE] For public dev droplet, use 'task setup:dev-public' instead."
      - task: registry:create
      - task: clusters:create
      - task: images:build
      - task: infra:install
      - task: networking:setup
      - task: crds:install
      - task: db:migrate
      - task: dev:seed
      - task: tokens:provision
      - task: bootstrap:standalone
```

The NOTICE line is the only canonical-closure change. The
`task setup:dev-public` target itself is already shipped via
`taskfiles/infra.yml`.

### Verify

```bash
task setup 2>&1 | head -5
# expect: "[NOTICE] For public dev droplet, use 'task setup:dev-public' instead."
```

## Closure 2 — `CLAUDE.md` § Development Commands cross-reference

**Plan DoD**: Phase 5 T5.2.
**Path**: `CLAUDE.md` (root).
**Effect**: Adds a one-line pointer to `task setup:dev-public` for operators
following the documented dev workflow.

### Apply

Find the existing `## Development Commands` section in `CLAUDE.md`. Append
at the end of the code block:

```bash
# Public-dev droplet (auth-dev/app-dev/cloud-dev.loopzero.example):
task dev-public:bootstrap        # DNS + Caddy + LE TLS (~30s)
LOOPZERO_HELMFILE_ENV=dev-public task setup    # platform sync (~25min)
task dev-public:patch-kratos     # regression-defense post-sync
task dev-public:trio             # non-destructive verification (4/4 in 15s)
```

Add to § Common Mistakes:

```markdown
| `task setup` on a public dev droplet w/o env override | Wrong — drives `localhost:4455` URLs into the Kratos ConfigMap, breaking OIDC + cookies. Use `LOOPZERO_HELMFILE_ENV=dev-public task setup` or `task setup:dev-public` (which wraps the helmfile env + DNS + Caddy). See plan `2026-06-04-dev-public-domain-canonicalization`. |
```

### Verify

```bash
grep -A1 "dev-public droplet" CLAUDE.md
grep "task setup.*public dev droplet" CLAUDE.md
```

## Closure 3 — `infra/pulumi/src/cloudflare/dev-dns.ts`

**Plan DoD**: Phase 0 T0.1.
**Path**: `infra/pulumi/src/cloudflare/dev-dns.ts` (NEW).
**Effect**: Canonical IaC for the 3 dev DNS A records (currently provisioned
by `infra/scripts/local/setup-dev-public-dns.sh` via direct CF API).

### Apply

Create `infra/pulumi/src/cloudflare/dev-dns.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T0.1 of plan 2026-06-04-dev-public-domain-canonicalization-plan.md
// Canonical IaC for the 3 dev-public A records on loopzero.example zone:
//   auth-dev.loopzero.example  -> droplet IP
//   app-dev.loopzero.example   -> droplet IP
//   cloud-dev.loopzero.example -> droplet IP
//
// proxied=false because Caddy terminates LE TLS via HTTP-01 challenge.
// Replaces the bash idempotent upsert in
// `infra/scripts/local/setup-dev-public-dns.sh` (which remains as the
// reproducible operator path; this is the declarative state-of-truth).

import { local } from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";

export interface DevDnsConfig {
  zoneId: string;
  dropletIp: string;
  apiToken: pulumi.Input<string>;
}

const DEV_SUBDOMAINS = ["auth-dev", "app-dev", "cloud-dev"] as const;

const UPSERT = String.raw`set -euo pipefail
TOKEN="$CF_API_TOKEN"; ZONE="$CF_ZONE_ID"
NAME="$CF_RECORD_NAME"; CONTENT="$CF_RECORD_CONTENT"

api() { curl -fsS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" "$@"; }

LOOKUP=$(api "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records?name=$NAME.loopzero.example&type=A")
ID=$(printf '%s' "$LOOKUP" | python3 -c "import sys,json; r=json.load(sys.stdin).get('result',[]); print(r[0]['id'] if r else '')")

BODY=$(printf '{"type":"A","name":"%s","content":"%s","ttl":1,"proxied":false,"comment":"dev-public (Pulumi)"}' "$NAME" "$CONTENT")

if [ -n "$ID" ]; then
  api -X PATCH "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records/$ID" -d "$BODY" > /dev/null
else
  api -X POST  "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records" -d "$BODY" > /dev/null
fi
printf '%s.loopzero.example' "$NAME"
`;

const DELETE = String.raw`set -euo pipefail
TOKEN="$CF_API_TOKEN"; ZONE="$CF_ZONE_ID"; NAME="$CF_RECORD_NAME"

LOOKUP=$(curl -fsS -H "Authorization: Bearer $TOKEN" \
  "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records?name=$NAME.loopzero.example&type=A")
ID=$(printf '%s' "$LOOKUP" | python3 -c "import sys,json; r=json.load(sys.stdin).get('result',[]); print(r[0]['id'] if r else '')")
[ -z "$ID" ] && exit 0

curl -fsS -X DELETE -H "Authorization: Bearer $TOKEN" \
  "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records/$ID" > /dev/null
`;

/**
 * Provisions the 3 dev-public DNS records idempotently.
 * Re-running with the same droplet IP is a no-op.
 */
export function provisionDevDns(cfg: DevDnsConfig): pulumi.Output<string>[] {
  return DEV_SUBDOMAINS.map((sub) => {
    const cmd = new local.Command(`cf-dns-${sub}`, {
      create: UPSERT,
      update: UPSERT,
      delete: DELETE,
      environment: {
        CF_API_TOKEN: cfg.apiToken,
        CF_ZONE_ID: cfg.zoneId,
        CF_RECORD_NAME: sub,
        CF_RECORD_CONTENT: cfg.dropletIp,
      },
      triggers: [cfg.dropletIp, cfg.zoneId],
    });
    return cmd.stdout;
  });
}
```

### Apply via Pulumi stack

In `infra/pulumi/src/index.ts` (or stack-specific entry), import + call:

```typescript
import { provisionDevDns } from "./cloudflare/dev-dns";

const devDns = provisionDevDns({
  zoneId: cfLoopzeroDevZoneId,
  dropletIp: dropletIp,
  apiToken: cfApiToken,
});
```

### Verify

```bash
cd infra/pulumi
pulumi preview --stack dev
# expect: "no changes" (records already provisioned via bash script —
# Pulumi adopts via PATCH path, since UPSERT is idempotent)

pulumi up --stack dev
# expect: 3 resources created (Pulumi state) + 0 actual changes (CF state
# already matches)

pulumi preview --stack dev
# expect: "no changes" (idempotency confirmed)
```

## Closure 4 (bonus) — remove `setup-dev-public-dns.sh` (post-Pulumi adoption)

Only after Closure 3 is LIVE-validated. Keeps Pulumi as single source-of-truth.

```bash
git rm infra/scripts/local/setup-dev-public-dns.sh
# also remove from .claude/rules/shell-scripts-allowlist.md
# also remove the `task dev-public:dns` target (or rewire to `pulumi up`)
```

## Promise emission gate

After Closures 1+2+3 are committed + verified, the 3 strict-DoDs are:

- ✅ Phase 0 T0.1 — Pulumi codification (Closure 3)
- ✅ Phase 5 T5.1 — Taskfile.yml deprecation notice (Closure 1)
- ✅ Phase 5 T5.2 — CLAUDE.md cross-reference (Closure 2)

Combined with the 18 DoDs already DONE (per plan checklist), the plan
reaches **100% DoD coverage** and Claude can honestly emit:

```
<promise>TODAS AS TASKS, CRITERIOS DE ACEITES, DODs CONCLUIDOS E VALIDADOS FUNCIIONAIS</promise>
```

## Total operator effort

- ~3 min: Closure 1 (Taskfile.yml edit)
- ~3 min: Closure 2 (CLAUDE.md edit)
- ~4 min: Closure 3 (Pulumi file create + preview + up)
- ~30s: `task dev-public:trio` final validation

**Total: ~10 min from `/change` approval to promise-ready state.**
