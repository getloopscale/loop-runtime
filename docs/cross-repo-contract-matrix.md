# Cross-Repo Contract Matrix (M5)

The decomposition seams are HTTP/OpenAPI contracts. This matrix keeps them honest
forever: each seam has a consumer-driven contract test, and a single runner
executes them all across the umbrella repos.

## The seams

| Seam | Consumer | Provider | Contract | Test |
|---|---|---|---|---|
| cli↔engine | loopzero-cli | loopzero (engine API) | `loopzero-cli/tests/contract/engine.yaml` (vendored OpenAPI snapshot) | `loopzero-cli/tests/contract/engine-contract.test.ts` (every CLI-called `/api/v1` path ⊆ snapshot) + engine-side `loopzero/api/internal/contract/cli_consumer_test.go` |
| cloud↔engine | loopzero-cloud | loopzero (engine API) | opaque `/engine/*` proxy + Ed25519 service-auth | `loopzero-cloud/internal/auth/service_signer_test.go` (serviceauth handshake) |
| cloud↔loopzero-data | loopzero-cloud | loopzero-data (themory) | `loopzero-cloud/openapi/themory.yaml` | `loopzero-cloud/internal/themory/contract_test.go` |
| engine OpenAPI self-conformance | — | loopzero (engine API) | `loopzero/api/openapi/engine.yaml` | `loopzero/api/internal/contract/{spec_loads,engine_conformance}_test.go` |

## Run all seams

```bash
cd loopzero && task quality:contract:cross-repo
```

Runs (from the engine root, umbrella layout):
1. engine OpenAPI + cli-consumer contract (`loopzero/api`).
2. cloud↔loopzero-data + cloud↔engine contracts (`../loopzero-cloud`) — SKIP if absent.
3. cli↔engine contract (`../loopzero-cli`) — SKIP if absent.

Absent siblings are SKIPPED with an explicit message (never a false PASS).

## CI invocation

GitHub Actions was removed (it is not this project's CI platform). Any CI invokes
`task quality:contract:cross-repo` from a checkout that has the sibling repos
present (or pinned). The per-repo halves also run inside each repo's own CI:
- engine: `task quality:contract` (the cli-consumer + engine-conformance halves).
- loopzero-cloud: `go test ./internal/themory/... ./internal/auth/...`.
- loopzero-cli: `npm test -- tests/contract/engine-contract.test.ts`.

## Durable guarantee

The acyclic-graph gate (`loopzero-ops quality acyclic-graph`) enforces the dependency
DIRECTION; this matrix enforces the seam CONTRACTS. Together they keep the
decomposition both acyclic and behavior-compatible across repos.
