---
name: loki-federation
description: PROPOSED Loki cross-cluster federation design (T4.6 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap). Engineering-pre-validated; LIVE rollout requires drill stack.
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T4.6
---

<!-- PROPOSED STATE -->

# Loki Cross-Cluster Federation — Design Proposal

> **Plan reference:** T4.6 of `docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Closes:** M08 Observability MEDIUM gap — "Loki cross-cluster siloed. Single Loki em build cluster. Runtime/workload Promtail push to build Loki via hardcoded IP. Phase 3 ADR-0009 sem implementation."

## AS-IS state (verified 2026-05-27)

**Single Loki instance:**
- Loki Helm release lives em the **build cluster** (per audit M08; `infra/helm/loki/values.yaml`)
- Promtail DaemonSets em all clusters ship to build-Loki via TEMPLATED push URL (EC-8 of edge-case-review — `infra/helm/promtail/values.yaml.gotmpl` already uses `.Environment.Values | getOrNil "loki.pushUrl"` per commit `ef7a7c16`)
- mgmt + runtime + drill clusters all push to a SINGLE Loki

**Resulting query topology:**
```
Operator/Grafana ──► Loki (build cluster) ◄── Promtail (mgmt)
                              ◄── Promtail (runtime)
                              ◄── Promtail (drill)
                              ◄── Promtail (build, local)
```

**Strengths of AS-IS:**
- Single-pane query (LogQL grep works across all clusters)
- Simple operational topology (1 Loki to maintain)
- Tenant labels distinguish source cluster via `cluster_id` label

**Weaknesses (the gap):**
- Loki em build cluster = SPOF for log retrieval (if build cluster down, lose all logs)
- Build cluster sized for builds + Loki — cross-team coupling
- Network round-trip mgmt → build cluster for every operator log query
- No log isolation per cluster (Loki RBAC alone, no physical separation)

## Three federation options evaluated

### Option A — Move Loki to mgmt cluster (single-instance, relocation)

- **Effort:** ~4h (helmfile mgmt release + Promtail URL repointing + 24h soak)
- **Pros:** Preserves single-pane query; eliminates build-cluster SPOF for log query path; logs stay near Grafana
- **Cons:** Same SPOF model (just moved to mgmt); does NOT solve cross-cluster availability
- **Verdict:** Tactical fix, NOT true federation

### Option B — Federation primary + remote-write backends (Phase 3 ADR-0009 vision)

- **Effort:** ~16h (3 Loki instances + remote write config + cross-cluster auth + dashboard rework)
- **Architecture:**
  ```
  Grafana (mgmt) ──► Loki-PRIMARY (mgmt) ◄── remote_write ── Loki-REPLICA (build)
                                          ◄── remote_write ── Loki-REPLICA (runtime)
                                          ◄── remote_write ── Loki-REPLICA (drill)
  ```
- **Pros:** True HA — query continues even if 1 cluster down; per-cluster log isolation; primary aggregates writes
- **Cons:** 3× resource footprint (~3GB memory, ~6 CPUs); complex auth (cross-cluster mTLS via cert-manager); operational burden 3× Loki upgrades + rotations
- **Verdict:** Long-term right answer but requires sponsor sign-off (T3 — architecture change)

### Option C — Migrate Loki to mgmt + add Loki Ruler federation (recommended this plan)

- **Effort:** ~6h engineering + 24h LIVE soak (drill stack)
- **Architecture:**
  ```
  Grafana (mgmt) ──► Loki (mgmt) ◄── Promtail (all clusters)
                              │
                              └── Loki Ruler (recording rules + alerts)
                              └── Promtail (mgmt itself — currently missing on mgmt)
  ```
- **Pros:**
  - 80% of Option B's benefit (single source of truth, mgmt-local, no SPOF on build)
  - 30% of effort
  - Existing Helm release patterns
  - Promtail templating already in place (EC-8)
- **Cons:** Still single Loki — true HA requires Option B follow-up
- **Verdict:** Best Phase 1 — operator can ship in 1 day with measurable benefit; Option B is the v2.x evolution

## Proposed implementation (Option C)

### Step 1 — Add Loki to mgmt helmfile

```yaml
# infra/helmfile/mgmt.helmfile.yaml.gotmpl
- name: loki
  chart: ../helm/loki
  namespace: loopzero-monitoring
  needs:
    - loopzero-system/cert-manager
    - loopzero-system/traefik
  values:
    - ../helm/loki/values.yaml
    - {{ .Environment.Values | getOrNil "loki.mgmt.overridesPath" | default "" }}
```

Values override for mgmt-specific storage backend:
```yaml
# infra/helmfile/values/{env}/loki-mgmt.yaml.gotmpl
loki:
  storage:
    s3:
      endpoint: {{ requiredEnv "S3_ENDPOINT" }}
      access_key_id: ...
      bucketnames: loopzero-logs-mgmt
```

### Step 2 — Repoint all Promtails to mgmt-Loki

Update environment values per cluster:
```yaml
# infra/helmfile/values/dev/promtail.yaml
loki:
  pushUrl: http://loki.loopzero-monitoring.svc.cluster.local:3100/loki/api/v1/push  # via mgmt API

# infra/helmfile/values/drill/promtail.yaml
loki:
  pushUrl: http://loki-mgmt-internal-lb.drill.loopzero.example/loki/api/v1/push

# infra/helmfile/values/prod/promtail.yaml
loki:
  pushUrl: https://loki-internal.loopzero.cloud/loki/api/v1/push
```

Cross-cluster requires:
- mgmt-Loki exposed via internal LB (NetworkPolicy admit-from-cluster-A,B,C)
- TLS via cert-manager (LetsEncrypt internal CA OR self-signed)
- Optional: Hydra auth on Loki for tenant isolation

### Step 3 — Drop build-cluster Loki

After 7-day mgmt-Loki soak shows ZERO drift:
```yaml
# infra/helmfile/build.helmfile.yaml.gotmpl
# DELETE loki release entry
```

### Step 4 — Add Promtail to mgmt cluster

Currently mgmt has no Promtail (Loki was on build, no self-scrape). Post-move:
```yaml
# infra/helmfile/mgmt.helmfile.yaml.gotmpl
- name: promtail
  chart: ../helm/promtail
  namespace: loopzero-monitoring
  needs: [loopzero-monitoring/loki]
  values:
    - ../helm/promtail/values.yaml.gotmpl
```

## Operator HARD STOP

LIVE validation requires:
1. Drill stack provision (per `drill-test SKILL.md`)
2. Helmfile sync com new Loki location
3. 24h soak: verify zero log loss across cluster restarts
4. Cross-cluster grep test: `loopzero logs --build <id>` returns logs em < 5s

## Rollback criteria

Within 24h soak window:
- If `loopzero logs` SLO breaks (p95 > 5s) → revert helmfile change, restore build-Loki
- If Promtail push errors > 0.1% → revert + investigate network policies
- If mgmt cluster CPU saturation > 80% → resize mgmt nodepool first

## Trade-off documented

This delivers ~80% of true HA benefit at 30% effort. **True HA via Option B is documented as v2.x follow-up.** Justification: pre-customer-launch, single Loki on mgmt is acceptable risk; post-launch traffic justifies HA investment.

## Related

- M08 audit doc (gap evidence)
- ADR-0009 (original federation vision)
- `infra/helm/promtail/values.yaml.gotmpl` (templating already implemented per EC-8)
- `infra/helm/loki/values.yaml` (current build-Loki config to relocate)
- CLAUDE.md "Path to maturity 4.0" gate (a) operator-run drill GREEN
