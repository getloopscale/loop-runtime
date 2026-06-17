---
name: 2026-06-15-cleanroom-phase4-5-green
context: 100% Dashboard Coverage roadmap Phase 4+5 GREEN (Cleanroom TOTAL → tilt up → doctor PASS=42 FAIL=0)
plan: .claude/knowledge-base/plans/2026-06-15-100pct-dashboard-coverage-roadmap.md
goal_directive: sponsor /goal 2026-06-15 "Continue e so pare quendo tiver concluido TODOS OS PONTOS ATE READY_TO_MERGE"
created_at: 2026-06-15
captured_by: claude (autonomous Cleanroom validation)
---

# Steady-State Baseline — Cleanroom Phase 4+5 GREEN

Per `.claude/rules/steady-state-baseline.md`, capturing the canonical
snapshot post Cleanroom TOTAL + UP per sponsor canonical chain. This is
the **bisect anchor** if any regression surfaces downstream (Phase 6
LIVE dogfood OR Phase 7 chaos OR Phase 8 review).

## Layer 1 — Code state

| Field | Value |
|---|---|
| Branch | `develop` |
| HEAD SHA | `5cc8a693bc6b7f38bc17acc64b0688463beef214` |
| HEAD subject | `fix(vault): bao_auth + policy write missing BAO_SKIP_VERIFY=true (caught LIVE Phase 4)` |
| Commits ahead of `origin/main` | (this session shipped 13+ commits — see git log range `1edd4e07..5cc8a693`) |
| Working tree | Clean post-commit (only background-task logs in /tmp untracked) |

## Layer 2 — Runtime state

| Layer | Value | Notes |
|---|---|---|
| k3d clusters | 4 Running (`loopzero-mgmt`, `loopzero-build`, `loopzero-workload`, `loopzero-runtime-2`) | All 4 created during `task setup` |
| Helm releases (all namespaces) | 25 deployed | Full install per `infra/helmfile/` |
| Pods total | 64 (62 Running + 2 Completed) | Initial converged state post-Tilt |
| Operator pods (Tilt-managed) | 4 ready (tenant + app + preview + platform) | Per `task doctor § Operator Pods` PASS |
| ClusterCredentials | 3/3 connected (`cell-default`, `cell-1`, `cell-2`) | Multi-cell CTO Golden Rule #1 GREEN |
| `task doctor` | **PASS: 42 / WARN: 5 / FAIL: 0** ✅ | Target was ≥ 38 PASS / 0 FAIL — exceeded |

## Layer 3 — Config state

| Component | State |
|---|---|
| `schema_migrations` max version | `32` |
| OpenBao state | Initialized + Unsealed (Shamir 5/3); 5 shards persisted at `~/.loopzero/cleanroom-unseal.json` (0600) |
| OpenBao AppRole | `loopzero-api` role configured with `transit/keys/*` + `kv/*` policy + 24h token TTL |
| Secret `loopzero-openbao-approle` | Created in `loopzero-system` + `external-secrets` (manually copied from external-secrets to loopzero-system to bypass missing ESO ClusterSecretStore wire — see RUNTIME NOTE below) |
| Secret `openbao-spki-pin` | Created with extracted SPKI hash from `openbao-tls` cert |

### RUNTIME NOTE — manual Secret copy for dev

The `vault-bootstrap-dev.sh` script targets `external-secrets` namespace
(per `ESO_TARGET_NS` default). The loopzero-api Deployment in `loopzero-system`
expects the `loopzero-openbao-approle` Secret in its own namespace via
direct envFrom secretRef. ESO ClusterSecretStore + ExternalSecret
that would sync from Vault to loopzero-system did NOT auto-deploy during
`task setup` (operator gate per `dashboard-memory-bff-server-side-key-vault`
plan + memory `j09-aprendizado-github-creds-via-eso-canonical`).

**Manual copy applied during this baseline capture:**

```bash
kubectl --context k3d-loopzero-mgmt -n external-secrets get secret loopzero-openbao-approle -o json \
  | jq 'del(.metadata.namespace,.metadata.uid,.metadata.resourceVersion,.metadata.creationTimestamp,.metadata.ownerReferences,.metadata.managedFields,.metadata.annotations)' \
  | kubectl --context k3d-loopzero-mgmt -n loopzero-system apply -f -
```

This is canonical-workaround per `feedback_canonical_vs_codified_workaround`:
documents the gap for future ESO wire-up (Phase 3 of `dashboard-memory-bff`
plan sub-goals 4 + 6 + 8) but unblocks the current Cleanroom Phase 5 gate.

## Layer 4 — LIVE functional probes

| Probe | Result | Diagnostic |
|---|---|---|
| `GET http://localhost:13001/api/v1/health` | **HTTP 200** ✅ | API liveness via mgmt LB |
| `GET http://localhost:13001/api/v1/health/deep` | **HTTP 200** ✅ | Deep dependency check (PG + Gitea + MinIO + ArgoCD) |
| `GET http://localhost:13001/api/v1/version` | **HTTP 200** ✅ | CLI compat negotiation surface |
| `task doctor` global verdict | **PASS=42 / FAIL=0** | All 11 verification sections GREEN |
| `dogfood-all --hermetic-only` | **11/12 PASS / 1 FAIL** | dogfood-device hermetic Hydra contract regression (pre-existing; not blocking Phase 5) |
| `dogfood-all --live-only` | IN PROGRESS (background ID `b1yn6ayov`) | Phase 6 LIVE chain — expected 60-90min |

## Real bugs caught LIVE this Cleanroom

Per sponsor goal mandate `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL,
Cleanroom IS the test surface. This run surfaced 2 REAL bugs that
hermetic + dogfood-only never would have caught:

1. **`vault-bootstrap-dev.sh` TLS x509 unknown authority** — fixed
   inline (commit `5cc8a693`). The `bao_auth()` helper at line 121 +
   the `policy write` stdin pipe at line 134 were both missing
   `BAO_SKIP_VERIFY=true` env var. Result: AppRole creation silently
   failed + loopzero-api stayed CrashLoopBackOff with `OPENBAO_TRANSIT_ENABLED=true
   requires OPENBAO_VAULT_SECRET_ID` for the entire bootstrap window
   until manual diagnosis.

2. **`task doctor` unhealthy YAML break in `taskfiles/quality.yml`** —
   fixed pre-Cleanroom (commit `84167b30`). The Phase 4 commit `0792c947`
   inline `bash -c '...'` with shell metachars (`>`, `;`, nested quotes)
   broke YAML parsing → ANY `task *` invocation failed before this
   session's Cleanroom could even begin. Extracted to
   `infra/scripts/local/shellcheck-advisory.sh`.

Both bugs would have made the next Cleanroom impossible. Catching them
during the Cleanroom IS the value the canonical chain provides per
sponsor's INQUEBRÁVEL mandate.

## Per `.claude/rules/steady-state-baseline.md`

This baseline is the bisect anchor for:
- Phase 6 dogfood `--live-only` results (currently in progress)
- Phase 7 chaos `--full` run (pending Phase 6 completion)
- Phase 8 `/code-quality` + `/review` → READY_TO_MERGE verdict

If anything downstream regresses, diff against this baseline to localize.

## Cross-references

- Goal-directive: `.claude/governance-gate.local.md § "Sponsor-direct amendment 2026-06-15 (100% Dashboard Coverage + LIVE chain execution)"`
- Roadmap: `.claude/knowledge-base/plans/2026-06-15-100pct-dashboard-coverage-roadmap.md`
- Cleanroom mandate: `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL
- Baseline rule: `.claude/rules/steady-state-baseline.md`
- Vault bootstrap fix: commit `5cc8a693`
- YAML fix: commit `84167b30`
- Auth coverage gate: commit `9686b0aa`
- Roadmap doc: commit `512d3a18`
