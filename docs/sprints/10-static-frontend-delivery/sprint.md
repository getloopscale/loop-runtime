# Sprint 10 — Static Frontend Delivery

> **Decision:** MEET-2026-03-26-009 + Staff Review (8 critical points)
> **Scope:** Eliminate pod waste for static frontends via S3 + Traefik proxy
> **Prerequisite:** Fase 0 (DeliveryActivator + unified pipeline) must complete before Fase 1
> **Principle:** One pipeline, one state machine, one set of SSE events. The delivery step varies — everything else is identical.

**Objetivo:** Frontends estáticos (`render: static`) são servidos diretamente do object storage via Traefik proxy, eliminando pods permanentes e reduzindo custo de compute a zero para assets que não precisam de runtime.

**Definition of Done:** `loopzero deploy` em monorepo com backend (container) + frontend Vite (`render: static`) completa no DigitalOcean com ambas URLs acessíveis, rollback funcional em <5s, integrity probe validando checksums SHA256, e zero pods para o frontend.

---

## Design Constraints (from Staff Review)

### C1 — One Pipeline, Different Execution Step
**NOT** two separate DAGs. One DAG with a polymorphic final step:

```
clone → install → build → deliver_artifact
                              |
                   ┌──────────┴──────────┐
                   │                      │
            build_image (BuildKit)   upload_assets (mc/s3)
```

The DAG is identical. Only the `deliver_artifact` step changes based on `DeliveryModel`. Metrics, retries, observability — all unified under `deliver_artifact` semantics.

### C2 — Same Phases, Same Events, Same State Machine
`DeliveryActivator` implementations MUST follow the same semantic phases:

```
Phase A: create release (READY)
Phase B: deliver (push manifests OR upload assets)
Phase C: record result (PENDING or FAILED)
Phase D: verify (ArgoCD sync OR HTTP probe)
Phase E: finalize (ACTIVE or FAILED)
```

SSE events are delivery-agnostic:
- `deploy.delivering` (not `deploy.syncing` or `deploy.uploading`)
- `deploy.verifying` (not `deploy.health_check` or `deploy.probing`)
- `deploy.completed`

If the two activators diverge in phase count or event names → it's two products, not one.

### C3 — S3 Lifecycle Is Mandatory (Not "Nice to Have")
- Retention policy: keep last 10 releases per (tenant, project, app, environment)
- GC via control plane loop (not manual, not S3 lifecycle policy alone)
- Pinned releases (exempt from GC) for rollback safety
- Budget alert: S3 storage cost per tenant

### C4 — CDN Is Phase 2, But Phase 2 Is NOT Optional
Traefik→S3 is the **temporary architecture**. CDN is the **target architecture**. The roadmap MUST have a concrete trigger for Phase 2 (e.g., "when any tenant has >1000 requests/day to static assets"). Phase 2 is not "someday" — it's "when threshold X is met."

### C5 — Health Check Must Verify Asset Integrity (Real Checksums)
`GET / → 200` is insufficient. HEAD-check is insufficient (proves existence, not integrity).

Post-deploy probe must do **real content verification**:
1. GET `asset-manifest.json` from deployed URL (contains file list + SHA256 checksums)
2. GET critical assets (at minimum: `index.html`, first JS bundle, first CSS bundle)
3. Compute SHA256 of downloaded content
4. Compare computed hash against manifest hash
5. Deploy is NOT ACTIVE until integrity check passes

This catches: corrupted upload, truncated files, S3 eventual consistency serving stale content, file swapped with same name. HEAD cannot catch any of these.

**Scope limit:** verify at most 3 critical assets (index.html + 1 JS + 1 CSS). Not the full manifest. Balance between integrity confidence and probe latency.

### C6 — Observability Must Be Symmetric
Container apps have: `build_duration`, `deploy_duration`, `pod_cpu`, `pod_memory`
Static apps MUST have: `build_duration`, `upload_duration`, `upload_size_bytes`, `delivery_latency`, `probe_duration`

Same Grafana dashboard, different panels. Not two dashboards.

### C7 — Secrets Enforcement: Structural Rules + Soft Guidance
Two levels of enforcement:

**Hard rule (structural, reliable):**
- `secrets` declared on `render: static` app → **error, exit 1**. This is structurally impossible (CDN can't inject runtime secrets). Zero false positives.

**Soft guidance (heuristic, warning only):**
- `env` keys matching `*_KEY`, `*_SECRET`, `*_TOKEN` on `render: static` → **warning** suggesting review. This is guidance, not enforcement. High false positive risk (`GOOGLE_MAPS_KEY` is a public key). Users can ignore.
- Documentation: "Build-time vs Runtime variables" guide

### C8 — Upload Constraints (Concurrency, Size, Timeout)
Upload step must have explicit operational limits:
- **Max artifact size:** 500MB per app (same as source bundle limit). Enforced in upload step.
- **Upload timeout:** 120s per app. Argo step `activeDeadlineSeconds`.
- **Retry policy:** 2 retries with exponential backoff (same as BuildKit retries). Idempotent — re-uploading same files to same path is safe.
- **Parallelism:** governed by existing DAG parallelism (Workflow `parallelism: 4`). No new limit needed — upload steps compete for the same slots as BuildKit steps.
- **Quota per tenant:** S3 storage quota per tenant (e.g., 10GB). Enforced by asset GC + monitoring alert. Not hard-rejected at upload time (complexity not justified for v1).

### C9 — Distributed Consistency Contract (S3 × Gitea × ArgoCD)
The static delivery flow depends on 3 external systems reaching consistent state. The **invariant order** is:

```
1. Upload ALL assets to S3 path /{release-id}/     ← S3 is consistent
2. Upload asset-manifest.json to same path          ← manifest proves upload complete
3. Push IngressRoute + Middleware to Gitea           ← Git commit = desired state declared
4. ArgoCD sync confirms IngressRoute applied         ← Traefik now routes to S3 path
5. Integrity probe verifies content via GET+SHA256   ← proves end-to-end chain works
6. Release → ACTIVE                                  ← only after step 5 passes
```

**Invariant:** Step N+1 NEVER starts before Step N completes. No concurrent execution.
**Failure at any step:** Release stays in PENDING/FAILED. Reconcile loop retries from the failed step. Assets in S3 are immutable (never partially overwritten).
**Race condition:** Environment lock (`withEnvironmentLock`) serializes all mutations for (tenant, project, env). Two concurrent deploys are serialized, never interleaved.

### C10 — Rollback Cache Consistency
- Assets use content-hash filenames (`main.abc123.js`) → immutable, cache forever
- `index.html` has `Cache-Control: no-cache, must-revalidate` → always fresh from S3
- Rollback changes which `index.html` is served (Traefik path rewrite points to old release-id)
- Old hashed JS/CSS assets are still in S3 (not deleted by GC within retention window)
- No cache invalidation needed — the architecture is inherently cache-safe because `index.html` always references the correct hashed assets for its version

---

## Fase 0 — Unified Pipeline + DeliveryActivator (1 sprint)

**Objetivo:** Refatorar sem mudar comportamento. Todos os testes passam. Deploy E2E idêntico ao antes.

### 0.1 — DeliveryActivator Interface
```go
// api/internal/services/delivery.go
type DeliveryActivator interface {
    // Activate runs the 5-phase release activation.
    // Phases are semantically identical regardless of delivery model:
    //   A: create release, B: deliver, C: record, D: verify, E: finalize
    Activate(ctx context.Context, build *db.Build, result BuildCallbackRequest) error

    // Rollback reverts to a previous release.
    Rollback(ctx context.Context, release *db.Release) error
}
```
- **Done:** Interface compila. Nenhum implementador ainda.

### 0.2 — ContainerDeliveryActivator
- **Arquivo:** `api/internal/services/container_delivery.go` (novo)
- **Ação:** Mover as 5 fases de `release_activator.go`. Mesma lógica, mesma interface.
- **Done:** `ContainerDeliveryActivator.Activate()` e `.Rollback()` passam todos os testes existentes.

### 0.3 — BuildOrchestrator Dispatch
- **Ação:** `o.delivery.Activate()` em vez de `o.activateRelease()`. Factory resolve qual activator baseado em `build.DeliveryModel`.
- **Done:** Zero `if deliveryModel == "x"` no orchestrator. Polimorfismo via interface.

### 0.4 — RollbackOrchestrator Dispatch
- **Ação:** `o.delivery.Rollback()` em vez de lógica inline.
- **Done:** Rollback E2E funciona identicamente.

### 0.5 — Campo `rendering` no AppUnit
- **Arquivos:** CLI `app-unit.ts` + API `app_unit.go`
- **Valores:** `'static' | 'server'`. Default: `'server'`.
- **Done:** `normalizeApp` resolve rendering. Testes cobrem.

### 0.6 — Campo `render` no loopzero.yaml
- **Arquivos:** CLI `loopzero-yaml.ts` + API `validator.go`
- **Done:** Parse + validação. Round-trip test.

### 0.7 — DeliveryModel no BuildPlan + Release
- **Ação:** `AppBuildPlan.DeliveryModel`, `releases.delivery_model` column, DB migration.
- **Done:** `CreateBuildPlan` resolve delivery model a partir de rendering.

### 0.8 — SSE Events Delivery-Agnostic
- **Ação:** Renomear eventos:
  - `deploy.syncing` → `deploy.delivering`
  - `deploy.health_check` → `deploy.verifying`
  - `deploy.active` → `deploy.live`
- **Done:** CLI deploy-renderer atualizado. Testes cobrem.

### 0.9 — Unified DAG com Polymorphic Deliver Step
- **Arquivo:** `api/internal/argo/workflow_builder.go`
- **Ação:** DAG sempre tem o mesmo shape. O step `deliver-artifact-{app}` é BuildKit para container ou upload para static. Dependências idênticas.
- **Done:** Workflow para `render: static` tem mesmo DAG, diferente step final.

### 0.10 — Métricas Unificadas
- **Arquivo:** `api/internal/metrics/metrics.go`
- **Ação:** Renomear para semântica unificada:
  - `loopzero_artifact_delivery_duration_seconds{delivery_model="container|static"}`
  - `loopzero_artifact_delivery_size_bytes{delivery_model="container|static"}`
  - `loopzero_delivery_probe_duration_seconds{delivery_model="container|static"}`
- **Done:** Métricas existem com label `delivery_model`.

### 0.11 — Testes de Regressão
- **Done:** `go test ./...` + `npm test` all pass. Deploy E2E no DO idêntico ao antes.

**Exit Gate:** Zero mudança visível ao usuário. Mesmo CLI output, mesmos SSE events (renomeados), mesmo deploy flow.

---

## Fase 1 — Static Asset Delivery (1-2 sprints)

**Objetivo:** `render: static` faz upload de assets para S3, Traefik serve via IngressRoute proxy. Sem pod.

### 1.1 — Bucket S3 + Lifecycle Policy
- **Arquivo:** `infra/pulumi/src/components/infra.ts`
- **Ação:** Bucket `loopzero-static-assets` com versioning. Lifecycle policy: manter últimas 10 releases por (tenant, project, app, env). Pin support para releases protegidas.
- **Done:** Bucket provisionado. Policy configurada.

### 1.2 — Upload-Assets Step no Workflow
- **Arquivo:** `api/internal/argo/workflow_builder.go`
- **Ação:** Step `deliver-artifact-{app}` para static: inline template com `mc cp --recursive dist/ s3://.../{release-id}/`. Gera `asset-manifest.json` com file list + SHA256 checksums.
- **Done:** Static apps têm upload step no mesmo DAG que container apps.

### 1.3 — StaticDeliveryActivator.Activate()
- **Arquivo:** `api/internal/services/static_delivery.go`
- **Ação:** Mesmas 5 fases semânticas do ContainerDeliveryActivator:
  - A: create release (READY) — idêntico
  - B: push IngressRoute + Middleware para Gitea — análogo ao push de manifests
  - C: record result (PENDING) — idêntico
  - D: ArgoCD sync + integrity probe — análogo ao sync + health check
  - E: finalize (ACTIVE) — idêntico
- **Done:** Mesmos SSE events (`deploy.delivering`, `deploy.verifying`, `deploy.live`).

### 1.4 — renderStaticFrontendApp()
- **Arquivo:** `api/internal/manifests/renderer.go`
- **Ação:** IngressRoute (Traefik → S3 proxy) + Middleware (path rewrite `/{release-id}/`) + Middleware (SPA fallback: 404 → index.html) + Certificate. SEM Deployment/Service.
- **Done:** Testes unitários validam manifesto gerado.

### 1.5 — S3 Proxy Service (shared)
- **Arquivo:** Pulumi bootstrap ou Helm
- **Ação:** ExternalName Service `static-s3-proxy` no workload cluster → DO Spaces endpoint.
- **Done:** Service deployed, testado com curl.

### 1.6 — SPA Routing Middleware
- **Ação:** Traefik Middleware que serve `index.html` para paths sem extensão (client-side routing). Pattern: `replacePath` on 404.
- **Done:** Vite SPA com react-router funciona em paths arbitrários.

### 1.7 — Post-Deploy Integrity Probe (Real Checksums)
- **Arquivo:** `api/internal/services/static_delivery.go`
- **Ação:**
  1. GET `asset-manifest.json` da URL deployada (contém file list + SHA256)
  2. Parse manifest, identificar critical assets: `index.html` + primeiro JS bundle + primeiro CSS bundle
  3. GET cada critical asset (max 3)
  4. Computar SHA256 do body recebido
  5. Comparar hash computado com hash no manifest
  6. Deploy NÃO é ACTIVE até todos os checksums baterem
  7. Timeout: 30s total para o probe (3 assets × 10s cada max)
- **Done:** Deploy com upload parcial, arquivo corrompido, ou truncado é detectado e NÃO ativa. Teste unitário com mock de asset corrompido (hash mismatch).

### 1.8 — StaticDeliveryActivator.Rollback()
- **Arquivo:** `api/internal/services/static_delivery.go`
- **Ação:** Push IngressRoute com `/{previous-release-id}/` no path rewrite. ArgoCD sync. Assets antigos são imutáveis no S3 (content-hash filenames). `index.html` com `no-cache` garante que browser sempre busca a versão correta.
- **Done:** Rollback sub-second. Sem cache invalidation necessário.

### 1.9 — Secrets Validation
- **Arquivos:** `cli/src/commands/deploy.ts`, `cli/src/adapters/validate-config.ts`
- **Ação:**
  - **Hard error (structural):** `secrets` declared on `render: static` → exit 1. Message: "Static frontends cannot use runtime secrets. Move to env (build-time) or use render: server."
  - **Soft warning (heuristic, guidance only):** `env` keys matching `*_KEY`, `*_SECRET`, `*_TOKEN` on `render: static` → warning. NOT an error. High false-positive risk (public API keys are common). Message: "Warning: env key 'X' looks like a secret. Static frontends expose env at build time (visible in JS bundles). Review if this is intentional."
- **Done:** Hard error tested. Warning tested. Warning does NOT block deploy.

### 1.10 — CLI Output
- **Arquivo:** `cli/src/output/deploy-renderer.ts`, `cli/src/commands/logs.ts`, `cli/src/commands/status.ts`
- **Ação:**
  - Deploy JSON: `apps[].delivery: "cluster" | "static"`
  - Status: label `(static)` para apps estáticos
  - Logs: "Static frontend — no runtime logs. Use `loopzero logs --build <id>`" (exit 0)
  - Rollback: unified output, sem menção a S3/CDN
- **Done:** Testes cobrem.

### 1.11 — Observability
- **Arquivo:** `api/internal/metrics/metrics.go`, `infra/helm/monitoring/`
- **Ação:**
  - `loopzero_artifact_delivery_duration_seconds{delivery_model="static"}` — upload duration
  - `loopzero_artifact_delivery_size_bytes{delivery_model="static"}` — total upload size
  - `loopzero_delivery_probe_duration_seconds{delivery_model="static"}` — integrity probe time
  - Blackbox-exporter probe para todas URLs estáticas ativas
  - Alerta `LoopZeroStaticAssetDown` (critical, 3min)
  - Grafana: mesmo dashboard de deploy, painéis separados por `delivery_model`
- **Done:** Métricas visíveis no Prometheus. Alerta configurado.

### 1.12 — Asset GC Controller
- **Arquivo:** `api/internal/services/asset_gc.go` (novo)
- **Ação:** Loop periódico (1h) que:
  1. Lista releases por (tenant, project, app, env)
  2. Mantém últimas 10 + qualquer release pinned
  3. Deleta assets S3 de releases antigas
  4. Log structured com tamanho liberado
- **Done:** GC testado com releases fictícias.

### 1.13 — Validação DO
- **Ação:** Monorepo (Express `type: server` + Vite `render: static`) deployado no DO.
- **Checklist:**
  - [ ] Backend em pod, frontend em S3
  - [ ] Ambas URLs acessíveis com TLS
  - [ ] SPA routing funciona (deep link → index.html)
  - [ ] `loopzero rollback` funciona para ambos
  - [ ] Integrity probe com GET + SHA256 detecta asset corrompido e rejeita deploy
  - [ ] Integrity probe aceita deploy com checksums corretos
  - [ ] Métricas `delivery_model=static` visíveis no Grafana
  - [ ] Alerta `LoopZeroStaticAssetDown` dispara se URL retorna 5xx
  - [ ] GC executa e mantém últimas 10 releases
  - [ ] Upload > 500MB rejeitado
  - [ ] Consistência: upload parcial não resulta em ACTIVE (ordem S3→Gitea→ArgoCD→probe respeitada)

**Exit Gate:** Tudo acima checked. `loopzero deploy` com monorepo misto funciona end-to-end no DO.

---

## Fase 2 — CDN Edge Layer

**Trigger:** Qualquer tenant com >1000 requests/dia para assets estáticos, OU latência p95 > 500ms para assets.

| # | Task | Descrição |
|---|------|-----------|
| 2.1 | Cloudflare proxy | DNS CNAME para CF, proxy para Traefik origin |
| 2.2 | Cache purge on deploy | Cloudflare API purge após `deploy.live` |
| 2.3 | Edge TLS | CF managed TLS, remover cert-manager para static |
| 2.4 | CDN metrics | Hit rate, TTFB, cache staleness via CF analytics |
| 2.5 | Auto-detect render mode | Framework analysis + build output analysis |
| 2.6 | Multi-region | CF edge serve de PoP mais próximo |

**Fase 2 NÃO é opcional.** É obrigatória quando o trigger é atingido.

---

## Decisões Arquiteturais

| Decisão | Justificativa | Review Point |
|---------|---------------|-------------|
| Um DAG, step polimórfico | Evita dois pipelines com métricas/retries/observability divergentes | C1 |
| Mesmas fases semânticas | DeliveryActivator implementations são simétricos. Divergir = dois produtos. | C2 |
| GC controlado por control plane | S3 lifecycle policy não é suficiente (não sabe quais releases são pinned) | C3 |
| CDN é obrigatório, não opcional | Traefik→S3 é arquitetura temporária com trigger concreto para evolução | C4 |
| Integrity probe com GET + SHA256 | HEAD prova existência, não integridade. GET + checksum prova conteúdo correto. | C5 |
| Métricas simétricas com label | Um dashboard, `delivery_model` como dimensão, não dois dashboards | C6 |
| Secrets: hard rule + soft guidance | `secrets` em static = erro (structural). Heurística `*_KEY` = warning (guidance). | C7 |
| Upload constraints explícitos | 500MB max, 120s timeout, 2 retries, quota por tenant | C8 |
| Consistency contract S3×Gitea×ArgoCD | Ordem invariante: upload → manifest → Gitea → ArgoCD → probe → ACTIVE | C9 |
| Content-hash + no-cache index.html | Rollback é pointer swap, sem cache invalidation | C10 |
