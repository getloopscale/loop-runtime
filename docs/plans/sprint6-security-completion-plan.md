# Plan: Sprint 6 Completion — SBOM Generation + Admission Policy

> **Version 1.0** — Completa o Sprint 6 (Security + Supply Chain) implementando os dois itens restantes: geração de SBOM via syft no pipeline de build e ativação do sigstore policy-controller para verificação criptográfica de imagens no runtime cluster. Hoje, cosign assina toda imagem mas o runtime aceita imagens não-assinadas — é "trancar a fechadura sem fechar a porta" (SB-3, CVSS 7.2). Este plano fecha o gap de supply chain, elevando a postura de segurança de "signing-only" para "sign + verify + audit".

## Context

### O que existe hoje

**cosign signing (DONE):** Todo container image é assinado após o build via `api/internal/argo/cosign.go`. O workflow Argo executa `sign-image-{app}` dependendo de `build-image-{app}`. Assinatura é persistida no DB (`releases.image_digest`, `image_signature`, `image_signed_at`) via `enrichAppReleasesWithAttestation()`. Endpoint `GET /api/v1/builds/:id/attestation` retorna dados reais. Métrica `loopzero_release_signed_total{signed}` monitora regressão.

**ValidatingAdmissionPolicies (DONE):** 6 VAPs deployadas via `charts/loopzero-admission-policies/`:
- `loopzero-require-image-signature` — digest pinning (`@sha256:`) em Audit+Deny
- `loopzero-restrict-registries` — limita registries (DOCR + Harbor)
- `loopzero-disallow-latest-tag` — proíbe `:latest`
- `loopzero-require-resource-limits` — exige CPU/memory limits
- `loopzero-protect-platform-networkpolicies` — protege NPs
- `loopzero-argocd-managed-by` — protege ArgoCD resources

**Sigstore policy-controller (PREPARED, NOT WIRED):** Código pronto em `infra/pulumi/src/components/runtime-policy-controller.ts` com `installPolicyController()` e `createClusterImagePolicy()`. NÃO está importado em `runtime-bootstrap.ts`.

### O que falta

1. **SBOM (syft)** — Zero código. Nenhum step de geração de SBOM no pipeline. Nenhuma persistência. Nenhum endpoint.
2. **Admission policy (verificação criptográfica)** — VAPs fazem digest pinning, mas NÃO verificam a assinatura cosign. `policy-controller` pronto mas não ativado. SB-3 do MEET-057 (CVSS 7.2).

### Evidências

- MEET-2026-04-22-057 §3: "SB-3: Cosign signs but runtime doesn't verify" — Security VETO ativo
- MEET-2026-03-30-022 §4: "Install Kyverno on workload cluster with verifyImages" — APPROVED_WITH_CONDITIONS
- ADR-0014 §Multi-app builds: "When admission policy lands, this decision must be revisited" (per-app attestation)
- `docs/maturity-assessment.md`: Security 3.0/5, "syft SBOM missing, admission policy doesn't reject unsigned images"
- MEET-2026-03-30-022 §SRE: "Start in Audit mode everywhere. Move to Enforce after 2 weeks of clean logs"

## Objective

**Done = o runtime cluster verifica criptograficamente que toda imagem em namespace de tenant foi assinada por cosign, e todo build gera um SBOM (syft) persistido e consultável.**

Goals:
1. Syft SBOM generation adicionado como step no Argo Workflow, output persistido no S3 e referenciado no DB
2. Endpoint `GET /api/v1/builds/:id/sbom` retorna metadata do SBOM
3. Sigstore policy-controller ativado no runtime cluster em **Audit mode** (fail-open)
4. ClusterImagePolicy verifica assinaturas cosign (key-based, não keyless) para imagens do registry LoopZero
5. Per-app attestation schema (`release_app_attestations` table) para multi-app builds
6. Métrica `loopzero_sbom_generated_total{tenant, environment}` para observabilidade

## ADRs

### D1 — Syft como ferramenta de SBOM (não BuildKit built-in)

**Decision:** Usar syft (Anchore) como step separado no Argo Workflow, não o SBOM built-in do BuildKit.

**Rationale:** BuildKit suporta `--sbom=true` desde v0.11, mas gera SPDX apenas para o build context (layers), não para dependências da aplicação. Syft escaneia a imagem final pós-build e produz CycloneDX/SPDX completo incluindo OS packages + app dependencies. É o padrão de facto usado por Harbor, Trivy, e Grype.

**Consequences:** Adiciona ~30s ao pipeline (scan de imagem). Requer imagem `loopzero-syft-scanner` pre-built (padrão SEC-006: no runtime downloads). Output em CycloneDX JSON, armazenado no S3 (MinIO/DO Spaces).

### D2 — SBOM no S3, não no DB

**Decision:** Armazenar o SBOM completo no S3 (MinIO em dev, DO Spaces em prod). DB guarda apenas metadata (path, format, component count, generation timestamp).

**Rationale:** SBOMs podem ter 1-10MB dependendo do número de pacotes. Guardar no PostgreSQL como JSONB é viável mas polui o backup, dificulta queries, e viola o padrão existente (source bundles já vão para S3). O metadata no DB permite queries rápidas ("quantos componentes?") sem carregar o SBOM inteiro.

**Consequences:** Endpoint `/builds/:id/sbom` retorna metadata + presigned URL para download. CLI pode oferecer `loopzero build sbom <id>` no futuro.

### D3 — Key-based verification (não keyless)

**Decision:** policy-controller verifica usando a chave pública cosign, não keyless (Fulcio/Rekor).

**Rationale:** O pipeline de build atual (`cosign.go`) usa key-based signing (`--key /cosign/cosign.key`). Mudar para keyless requer OIDC provider no build cluster, que não existe. A chave pública é derivável da chave privada já em `cosign-signing-key` Secret. Consistência: sign key-based, verify key-based.

**Consequences:** A chave pública cosign deve ser distribuída ao runtime cluster como Secret. `runtime-policy-controller.ts` precisa ser atualizado: trocar `keyless` por `key` na ClusterImagePolicy. Rotação de chave cosign requer update em 2 clusters (build + runtime).

### D4 — Audit mode primeiro, Enforce depois

**Decision:** policy-controller em Audit mode por 2 semanas antes de Enforce.

**Rationale:** Per MEET-2026-03-30-022 §SRE: "Start in Audit mode everywhere. Move to Enforce after 2 weeks of clean logs." Imagens pré-Sprint-6 podem não ter assinatura (backward compat). Audit permite validar sem bloquear deploys existentes.

**Consequences:** Durante o período de Audit, imagens não-assinadas ainda são admitidas mas logadas. Enforce é uma mudança de configuração (Helm values), não de código.

### D5 — Per-app attestation via sidecar table

**Decision:** Nova tabela `release_app_attestations(release_id, app_name, digest, signature, signed_at)` para multi-app builds.

**Rationale:** ADR-0014 identifica que `releases` só guarda `Apps[0]` signature. Para admission policy que exige "ALL images signed", precisamos de per-app data. Sidecar table (Option A do ADR-0014) é preferível a JSONB (Option B) porque permite queries SQL diretas, foreign key para `releases`, e índices independentes.

**Consequences:** Migration forward-only. `phaseACreateRelease` popula a sidecar table para todos os apps, não só `Apps[0]`. Endpoint `/builds/:id/attestation` retorna array de attestations.

### D6 — loopzero-syft-scanner imagem pre-built

**Decision:** Pre-build imagem `loopzero-syft-scanner` com syft baked-in, análogo a `loopzero-cosign-signer`.

**Rationale:** SEC-006 (no runtime binary downloads). Cosign signer já segue este padrão. Syft binary checksum-verified no Dockerfile.

**Consequences:** Nova imagem em `infra/images/syft-scanner/Dockerfile`. CI build via `build-images.yml`. Constante `imageSyftScanner` em `constants.go`.

## Dependency Graph

```
Phase 0 (Foundation) ──▶ Phase 1 (SBOM Pipeline) ──▶ Phase 3 (Wiring + Observability)
         │                                                       │
         │                                                       ▼
         ▼                                               Phase 4 (E2E + Validation)
Phase 2 (Admission Policy)──────────────────────────────────────▶│
```

- Phase 0 e Phase 2 podem rodar em **paralelo** (independentes)
- Phase 1 depende de Phase 0 (imagem syft + constantes)
- Phase 3 depende de Phase 1 (SBOM persistence) e Phase 2 (policy-controller)
- Phase 4 depende de Phase 3 (tudo wired)

---

## Phase 0: Foundation — Per-app Attestation + Syft Image

**Objective:** Criar a infraestrutura base: tabela per-app attestation, imagem syft scanner, e constantes.

### T0.1 — Migration: release_app_attestations table

#### Objective
Criar tabela sidecar para attestation per-app, resolvendo limitação do ADR-0014 (apenas `Apps[0]`).

#### Evidence
ADR-0014 §Multi-app builds: "only `Apps[0]`'s signature persists... admission policy that requires 'ALL images signed' cannot be built on this schema."

#### Files to edit
```
api/internal/db/migrations/000005_release_app_attestations.up.sql — (NEW) forward-only migration
api/internal/db/attestation_store.go — adicionar methods para per-app attestation
api/internal/db/attestation_store_test.go — (NEW) testes da store
```

#### Deep file dependency analysis
- `000005_release_app_attestations.up.sql` — Nova migration. Seguindo padrão da `000004_preview_environments.up.sql`: RLS, triggers, constraints. Foreign key para `releases(id)`.
- `attestation_store.go` — Hoje tem `PostgresAttestationStore.GetBuildAttestation()` que lê de `releases`. Adicionar `SaveAppAttestation()` e `GetBuildAppAttestations()` que operam na nova tabela.
- `attestation_store_test.go` — Não existe. Criar testes contra Postgres real (padrão do projeto: sem mocks).

#### Deep Dives
**Schema:**
```sql
CREATE TABLE release_app_attestations (
  id          TEXT PRIMARY KEY DEFAULT 'raa-' || gen_random_hex(16),
  release_id  TEXT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
  app_name    TEXT NOT NULL,
  image_digest    TEXT NOT NULL,       -- sha256:...
  image_signature TEXT,                -- pinned ref tag@sha256:...
  signed_at       TIMESTAMPTZ,         -- API-side timestamp
  sbom_s3_key     TEXT,                -- S3 path para SBOM (Phase 1)
  sbom_format     TEXT,                -- cyclonedx-json | spdx-json
  sbom_component_count INTEGER,        -- quick metadata
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (release_id, app_name)
);
```
**Invariants:** `release_id` deve existir. `app_name` é unique per release. `image_digest` sempre começa com `sha256:`.

#### Tasks
1. Escrever migration SQL com FK, RLS, CHECK constraints
2. Implementar `SaveAppAttestation(ctx, releaseID, []AppAttestation) error`
3. Implementar `GetBuildAppAttestations(ctx, buildID, tenantID) ([]AppAttestation, error)`
4. Atualizar `enrichAppReleasesWithAttestation` para chamar `SaveAppAttestation` para TODOS os apps
5. Atualizar endpoint `/builds/:id/attestation` para retornar array de per-app attestations

#### TDD
```
RED:     TestSaveAppAttestation_PersistsAllApps — insere 3 app attestations, verifica count
RED:     TestSaveAppAttestation_UniqueConstraint — mesmo (release_id, app_name) retorna erro
RED:     TestGetBuildAppAttestations_ReturnsAllApps — insere 3, busca por build_id, verifica 3
RED:     TestGetBuildAppAttestations_CrossTenantReturns404 — tenant A não vê attestation de tenant B
RED:     TestSaveAppAttestation_DigestFormat — rejeita digest sem prefix sha256:
GREEN:   Implementar SaveAppAttestation + GetBuildAppAttestations
REFACTOR: Extrair shared scan helpers se necessário
VERIFY:  cd api && go test ./internal/db/... -run TestAppAttestation -v
```

#### Acceptance Criteria
- [ ] Migration aplica sem erro em PG 16
- [ ] RLS isola attestations por tenant
- [ ] FK cascade deleta attestations quando release é deletada
- [ ] Per-app attestation persiste para builds multi-app
- [ ] Cross-tenant read retorna 404 (não 403)
- [ ] `/builds/:id/attestation` retorna array com todos os apps

#### DoD
- [ ] Migration forward-only aplicada
- [ ] Testes contra Postgres real passando
- [ ] `cd api && go test ./internal/db/... -v` green
- [ ] Zero novos warnings de lint

---

### T0.2 — Syft scanner image

#### Objective
Criar imagem Docker pre-built com syft binary para uso no Argo Workflow (SEC-006: no runtime downloads).

#### Evidence
Padrão estabelecido em `infra/images/cosign-signer/Dockerfile` — checksum-verified binary baked into Alpine.

#### Files to edit
```
infra/images/syft-scanner/Dockerfile — (NEW) imagem com syft
api/internal/argo/constants.go — adicionar imageSyftScanner
```

#### Deep file dependency analysis
- `Dockerfile` — Segue exatamente o padrão de `cosign-signer/Dockerfile`: Alpine base, download com checksum, non-root user.
- `constants.go` — Source of truth para imagens. Adicionar constante análoga a `imageCosignSigner`.

#### Deep Dives
**Syft version:** v1.18.1 (latest stable, Apache 2.0). Checksum verificado via SHA256 no Dockerfile.
**Output format:** CycloneDX JSON (padrão para SBOM interchange, suportado por Grype/Trivy/Harbor).
**Image size:** ~50MB (syft binary é ~45MB).

#### Tasks
1. Criar `infra/images/syft-scanner/Dockerfile` com syft checksum-verified
2. Adicionar `imageSyftScanner` em `constants.go`
3. Adicionar build step em `.github/workflows/build-images.yml`

#### TDD
```
RED:     TestConstants_SyftScannerImageDefined — verifica constante não-vazia
GREEN:   Adicionar constante
REFACTOR: None expected
VERIFY:  cd api && go test ./internal/argo/... -run TestConstants -v
```

#### Acceptance Criteria
- [ ] `docker build infra/images/syft-scanner/` sucede
- [ ] Syft binary checksum verificado no build
- [ ] Container roda como non-root (UID 1000)
- [ ] Constante `imageSyftScanner` definida em constants.go
- [ ] build-images.yml inclui syft-scanner

#### DoD
- [ ] Imagem builda localmente
- [ ] Constante existe e é referenciada
- [ ] CI workflow atualizado

---

## Phase 1: SBOM Generation Pipeline

**Objective:** Adicionar step de geração de SBOM (syft) no Argo Workflow, após cosign signing, com output persistido no S3.

### T1.1 — Syft template no Argo Workflow

#### Objective
Gerar inline container template que executa syft na imagem pós-build, produzindo SBOM em CycloneDX JSON.

#### Evidence
Padrão em `cosign.go`: inline template com outputs, security context, volume mounts. O SBOM deve ser gerado APÓS signing (dependência: `sign-image-{app}`).

#### Files to edit
```
api/internal/argo/syft.go — (NEW) syftScanTemplate() análogo a cosignSignTemplate()
api/internal/argo/syft_test.go — (NEW) testes do template
api/internal/argo/workflow_builder.go — adicionar syft step após cosign no DAG
```

#### Deep file dependency analysis
- `syft.go` — Nova função `syftScanTemplate(appName, imageTag string, insecureRegistry bool) *forge.Container`. Segue o padrão exato de `cosign.go`: lê digest do metadata file, executa syft, escreve output para `/tmp/sbom.json`. Output declarado como Argo parameter.
- `workflow_builder.go` — Hoje tem `cosignSignTemplate()` inline templates (L139-141, L149-151) e DAG tasks (L335-336, L351-352). Adicionar `syftScanTemplate()` após cosign. Nova DAG task `sbom-{app}` dependendo de `sign-image-{app}`.
- `syft_test.go` — Testes análogos a `cosign_test.go`: outputs em `/tmp` (não `/workspace`), insecure flag, security context.

#### Deep Dives
**DAG sequence atualizada:**
```
build-image-{app} → sign-image-{app} → sbom-{app} → [mirror-harbor-{app}]
```

**Syft command:**
```bash
syft scan registry:{imageTag}@${DIGEST} -o cyclonedx-json=/tmp/sbom-{app}.json
```

**Output:** CycloneDX JSON escrito em `/tmp/sbom-{app}.json`. Declarado como Argo output artifact (não parameter — pode ser >1MB).

**S3 upload:** O template faz upload do SBOM para MinIO/S3 usando aws-cli (já disponível via `imageAWSCLI`). Path: `s3://loopzero-static-assets/sbom/{tenant}/{project}/{commit}/{app}.cdx.json`.

**Alternativa considerada:** Upload no finalize DAG. Rejeitado: se o step falha, output se perde. Melhor fazer inline.

#### Tasks
1. Criar `syftScanTemplate()` em `syft.go`
2. Adicionar testes em `syft_test.go`
3. Adicionar inline template em `workflow_builder.go` (após cosign)
4. Adicionar DAG task `sbom-{app}` dependendo de `sign-image-{app}`
5. Configurar upload do SBOM para S3 dentro do template

#### TDD
```
RED:     TestSyftScanTemplate_DeclaresOutputs — verifica output artifact declarado
RED:     TestSyftScanTemplate_OutputsOnLocalTmp — paths em /tmp, não /workspace
RED:     TestSyftScanTemplate_Insecure_EmitsFlag — --allow-insecure-registry quando RegistryInsecure=true
RED:     TestSyftScanTemplate_SecurityContext_NonRoot — UID 1000, RunAsNonRoot: true
RED:     TestSyftScanTemplate_UploadsToS3 — script contém aws s3 cp
RED:     TestWorkflowBuilder_IncludesSBOMStep — Build() inclui sbom-{app} no DAG
RED:     TestWorkflowBuilder_SBOMDependsOnSign — sbom-{app} depende de sign-image-{app}
GREEN:   Implementar syftScanTemplate + wiring no workflow_builder
REFACTOR: Extrair S3 path builder se pattern repetir com cosign
VERIFY:  cd api && go test ./internal/argo/... -v
```

#### Acceptance Criteria
- [ ] `syftScanTemplate()` gera template válido
- [ ] Output em `/tmp`, não `/workspace`
- [ ] DAG: `build → sign → sbom → [mirror]`
- [ ] S3 upload integrado no template
- [ ] Non-root execution (UID 1000)
- [ ] Insecure flag passado quando `RegistryInsecure=true`
- [ ] SBOM step é **non-fatal** ao deploy (continueOn.failed para não bloquear)

#### DoD
- [ ] Todos os testes passando
- [ ] `cd api && go test ./internal/argo/... -v` green
- [ ] Template gera workflow válido via `Build()`
- [ ] SBOM step presente no DAG com dependências corretas

---

### T1.2 — SBOM persistence e endpoint

#### Objective
Persistir metadata do SBOM no DB (via sidecar table de T0.1) e expor endpoint `GET /api/v1/builds/:id/sbom`.

#### Evidence
Padrão de `attestation_store.go` e `routes/build_attestation.go`. SBOM completo no S3, metadata no DB.

#### Files to edit
```
api/internal/services/build_reconcile_complete.go — extrair SBOM metadata do workflow output
api/internal/db/attestation_store.go — método UpdateAppSBOM()
api/internal/routes/build_sbom.go — (NEW) handler GET /builds/:id/sbom
api/internal/routes/build_sbom_test.go — (NEW) testes do handler
api/internal/routes/setup_routes.go — registrar nova rota
```

#### Deep file dependency analysis
- `build_reconcile_complete.go` — Hoje `enrichAppReleasesWithAttestation()` (L182-229) popula signature. Estender para popular SBOM S3 key a partir do output do syft step.
- `attestation_store.go` — Adicionar `UpdateAppSBOM()` para atualizar `sbom_s3_key`, `sbom_format`, `sbom_component_count` na tabela `release_app_attestations`.
- `build_sbom.go` — Novo handler análogo a `build_attestation.go`. Retorna metadata + presigned URL do S3.
- `setup_routes.go` — Registrar `GET /api/v1/builds/:id/sbom`.

#### Deep Dives
**Response shape:**
```json
{
  "build_id": "...",
  "apps": [
    {
      "app_name": "api",
      "sbom_format": "cyclonedx-json",
      "component_count": 142,
      "generated_at": "2026-04-26T10:30:00Z",
      "download_url": "https://presigned-s3-url..."
    }
  ]
}
```

**Presigned URL:** Gerado via S3 client existente em `api/internal/s3/`. TTL: 1 hora.

#### Tasks
1. Adicionar `SBOMPath`, `SBOMFormat`, `SBOMComponentCount` a `AppRelease`
2. Extrair SBOM metadata do workflow output em `enrichAppReleasesWithAttestation()`
3. Implementar `UpdateAppSBOM()` na attestation store
4. Criar handler `GET /builds/:id/sbom` com presigned URL
5. Registrar rota em setup_routes.go

#### TDD
```
RED:     TestUpdateAppSBOM_PersistsMetadata — salva sbom_s3_key, format, count
RED:     TestGetBuildSBOM_ReturnsAllApps — retorna SBOM metadata para todos os apps
RED:     TestGetBuildSBOM_CrossTenantReturns404 — isolamento
RED:     TestGetBuildSBOM_NoBuild_Returns404 — build inexistente
RED:     TestBuildSBOMHandler_ReturnsPresignedURL — verifica download_url presente
GREEN:   Implementar persistence + handler
REFACTOR: None expected
VERIFY:  cd api && go test ./internal/db/... ./internal/routes/... -v
```

#### Acceptance Criteria
- [ ] SBOM metadata persistido per-app
- [ ] Endpoint retorna metadata + presigned URL
- [ ] Cross-tenant retorna 404
- [ ] Presigned URL tem TTL de 1h
- [ ] Response shape é `--json` contract (estável)

#### DoD
- [ ] Todos os testes passando
- [ ] `cd api && go test ./... -v` green
- [ ] Rota registrada e funcional

---

## Phase 2: Admission Policy — Signature Verification

**Objective:** Ativar sigstore policy-controller no runtime cluster para verificação criptográfica de assinaturas cosign.

### T2.1 — Gerar e distribuir chave pública cosign

#### Objective
Extrair chave pública da chave privada cosign existente e distribuí-la ao runtime cluster como Secret.

#### Evidence
D3: key-based verification. Chave privada em `cosign-signing-key` Secret no `argo` namespace do build cluster. Chave pública precisa estar no runtime cluster para policy-controller verificar.

#### Files to edit
```
infra/pulumi/src/components/build-secrets.ts — extrair/configurar cosign public key
infra/pulumi/src/components/runtime-policy-controller.ts — montar chave pública como Secret
infra/pulumi/src/config/stack.ts — adicionar cosignPublicKeyB64 config
```

#### Deep file dependency analysis
- `build-secrets.ts` — Hoje cria `cosign-signing-key` Secret com `cosign.key` (private) e `cosign.password`. Não manipula chave pública.
- `runtime-policy-controller.ts` — Hoje usa `keyless` com Fulcio/Rekor. Precisa ser alterado para `key` com a chave pública.
- `stack.ts` — Adicionar `cosignPublicKeyB64` para a chave pública (pode ser não-secret — é pública).

#### Deep Dives
**Geração da chave pública:**
```bash
cosign public-key --key cosign.key > cosign.pub
cat cosign.pub | base64 -w0  # → para Pulumi config
```

**Distribuição:** Pulumi cria Secret `cosign-public-key` no namespace `cosign-system` do runtime cluster com a chave pública.

#### Tasks
1. Adicionar `cosignPublicKeyB64` em `stack.ts`
2. Criar Secret `cosign-public-key` no runtime cluster via Pulumi
3. Atualizar `createClusterImagePolicy()` para usar `key` ao invés de `keyless`

#### TDD
```
RED:     (Pulumi preview) — verifica Secret criado no output
GREEN:   Implementar criação do Secret
REFACTOR: None expected
VERIFY:  cd infra/pulumi && npx pulumi preview --stack dev
```

#### Acceptance Criteria
- [ ] Chave pública distribuída ao runtime cluster
- [ ] Secret criado no namespace `cosign-system`
- [ ] ClusterImagePolicy usa key-based verification
- [ ] Pulumi preview mostra recursos corretos

#### DoD
- [ ] Pulumi preview sucede sem erros
- [ ] Secret criado com chave pública válida
- [ ] ClusterImagePolicy atualizada

---

### T2.2 — Ativar policy-controller no runtime-bootstrap

#### Objective
Wiring do `installPolicyController()` no `runtime-bootstrap.ts`, ativando o policy-controller em Audit mode.

#### Evidence
`runtime-policy-controller.ts` L12-17: "To activate: 1. Import in runtime-bootstrap.ts, 2. Call after installESO, 3. Run pulumi up". SB-3 do MEET-057.

#### Files to edit
```
infra/pulumi/src/components/runtime-bootstrap.ts — importar e chamar installPolicyController
infra/pulumi/src/components/runtime-policy-controller.ts — atualizar para key-based + Audit mode
```

#### Deep file dependency analysis
- `runtime-bootstrap.ts` — Hoje importa ESO, KEDA, monitoring. Adicionar import de `installPolicyController` e `createClusterImagePolicy`.
- `runtime-policy-controller.ts` — Mudar `failurePolicy: "Fail"` para `"Ignore"` (Audit mode = fail-open). Mudar `keyless` para `key` (D3). Adicionar referência ao Secret da chave pública.

#### Deep Dives
**Audit mode:** `failurePolicy: Ignore` + `validationActions: [Audit]` no ClusterImagePolicy. Imagens não verificadas são admitidas mas logadas. Após 2 semanas sem violações: mudar para `Fail` (tarefa manual, não automatizada neste plano).

**ClusterImagePolicy atualizada:**
```yaml
spec:
  images:
    - glob: "registry.digitalocean.com/loopzero-infra-registry/**"
  authorities:
    - key:
        secretRef:
          name: cosign-public-key
          namespace: cosign-system
```

**Multi-cell:** `bootstrapRuntimeCluster2()` também precisa de policy-controller. Mas por D4 (Audit first), ativar apenas no cluster 1 inicialmente.

#### Tasks
1. Atualizar `runtime-policy-controller.ts`: `failurePolicy: "Ignore"`, key-based verification
2. Importar `installPolicyController` em `runtime-bootstrap.ts`
3. Chamar após `installESO()`
4. Chamar `createClusterImagePolicy()` com registry endpoint
5. NÃO ativar em `bootstrapRuntimeCluster2()` (Audit no cluster 1 primeiro)

#### TDD
```
RED:     (Pulumi preview) — verifica policy-controller Helm release no output
RED:     (Pulumi preview) — verifica ClusterImagePolicy CR no output
GREEN:   Wiring completo
REFACTOR: None expected
VERIFY:  cd infra/pulumi && npx pulumi preview --stack dev
```

#### Acceptance Criteria
- [ ] policy-controller instalado no runtime cluster
- [ ] ClusterImagePolicy criada com key-based verification
- [ ] failurePolicy: Ignore (Audit mode)
- [ ] Apenas tenant namespaces (`loopzero.io/managed-by: environment-controller`) são verificados
- [ ] System namespaces não afetados
- [ ] Pulumi preview mostra recursos corretos

#### DoD
- [ ] Pulumi preview sucede
- [ ] policy-controller healthy no cluster
- [ ] ClusterImagePolicy aplicada
- [ ] Webhook latency < 500ms p99

---

### T2.3 — Métricas e alertas do policy-controller

#### Objective
Adicionar scrape do policy-controller no Prometheus e criar alertas para violations e latência do webhook.

#### Evidence
MEET-2026-03-30-022 §Distributed Systems: "Monitor Kyverno verifyImages webhook latency — alert if p99 > 1s"

#### Files to edit
```
infra/helm/admission-policies/manifests/policy-controller-alerts.yaml — (NEW) PrometheusRule
infra/helm/monitoring/values-runtime.yaml — adicionar scrape config para policy-controller
docs/runbooks/alerts/loopzero-policy-controller-*.md — (NEW) runbooks para alertas
```

#### Deep file dependency analysis
- `policy-controller-alerts.yaml` — Novo PrometheusRule com alertas: webhook latency > 500ms (warning), > 1s (critical), violation count > 0 (info).
- `values-runtime.yaml` — Adicionar serviceMonitor ou additionalScrapeConfigs para `cosign-system` namespace.
- Runbooks — Seguem padrão de `docs/runbooks/alerts/loopzero-*.md` existentes.

#### Tasks
1. Adicionar scrape config para policy-controller metrics
2. Criar PrometheusRule com 3 alertas
3. Escrever runbooks para cada alerta

#### TDD
```
RED:     TestPromQLSyntax_PolicyControllerAlerts — validar PromQL dos alertas
GREEN:   Criar alertas com PromQL válido
REFACTOR: None expected
VERIFY:  promtool check rules policy-controller-alerts.yaml
```

#### Acceptance Criteria
- [ ] Prometheus scrapes policy-controller metrics
- [ ] 3 alertas criados com PromQL válido
- [ ] Runbooks existem para cada alerta
- [ ] `promtool check rules` passa

#### DoD
- [ ] Alertas deployados
- [ ] Metrics visíveis no Prometheus
- [ ] Runbooks no diretório correto

---

## Phase 3: Wiring + Observability

**Objective:** Conectar SBOM pipeline e admission policy ao fluxo existente, adicionar métricas.

### T3.1 — Workflow output extraction para SBOM

#### Objective
Estender `extractStepOutputs()` e `enrichAppReleasesWithAttestation()` para capturar SBOM metadata do workflow.

#### Evidence
`build_reconcile_complete.go` L182-229: `enrichAppReleasesWithAttestation()` já extrai signature. Precisa extrair SBOM S3 key análogamente.

#### Files to edit
```
api/internal/services/build_reconcile_complete.go — estender enrichment para SBOM
api/internal/services/build_reconcile_complete_test.go — testes do enrichment
```

#### Deep file dependency analysis
- `build_reconcile_complete.go` — `enrichAppReleasesWithAttestation()` busca `sign-image-{name}` nos outputs. Adicionar busca de `sbom-{name}` para extrair S3 key, format, component count.

#### Tasks
1. Estender `enrichAppReleasesWithAttestation()` para popular campos SBOM
2. Chamar `SaveAppAttestation()` com dados completos (signature + SBOM)
3. Adicionar testes

#### TDD
```
RED:     TestEnrichAppReleases_WithSBOM — verifica campos SBOM populados
RED:     TestEnrichAppReleases_SBOMMissing_GracefulDegradation — sem SBOM step, campos nil
RED:     TestEnrichAppReleases_BothSignatureAndSBOM — ambos presentes
GREEN:   Implementar enrichment
REFACTOR: None expected
VERIFY:  cd api && go test ./internal/services/... -v
```

#### Acceptance Criteria
- [ ] SBOM metadata extraído do workflow output
- [ ] Graceful degradation quando SBOM step ausente
- [ ] Persistido via `SaveAppAttestation()`

#### DoD
- [ ] Testes passando
- [ ] `cd api && go test ./internal/services/... -v` green

---

### T3.2 — Métricas de SBOM e attestation

#### Objective
Adicionar métricas Prometheus para SBOM generation e per-app attestation.

#### Evidence
Padrão existente: `loopzero_release_signed_total{signed}`. Expandir para SBOM e per-app.

#### Files to edit
```
api/internal/metrics/metrics.go — adicionar counters
api/internal/services/container_delivery.go — incrementar counters
```

#### Deep file dependency analysis
- `metrics.go` — Registrar `loopzero_sbom_generated_total{tenant, environment}` e `loopzero_release_app_attestation_total{tenant, environment, app, signed}`.
- `container_delivery.go` — `phaseACreateRelease()` incrementa os counters após persistir.

#### Tasks
1. Registrar novos counters em metrics.go
2. Incrementar em container_delivery.go
3. Adicionar testes

#### TDD
```
RED:     TestSBOMMetric_Incremented — verifica counter após release com SBOM
RED:     TestAppAttestationMetric_PerApp — verifica counter por app
GREEN:   Implementar counters
REFACTOR: None expected
VERIFY:  cd api && go test ./internal/metrics/... ./internal/services/... -v
```

#### Acceptance Criteria
- [ ] `loopzero_sbom_generated_total` incrementado per release com SBOM
- [ ] `loopzero_release_app_attestation_total` incrementado per app
- [ ] Métricas visíveis em /metrics

#### DoD
- [ ] Testes passando
- [ ] Métricas registradas e incrementadas

---

## Phase 4: E2E Validation

**Objective:** Validar o fluxo end-to-end: build → sign → SBOM → persist → verify no runtime.

### T4.1 — Smoke E2E: SBOM generation

#### Objective
Validar que um deploy completo gera SBOM, persiste metadata, e o endpoint retorna dados corretos.

#### Evidence
Padrão em `infra/tests/e2e/local-install-deploy.sh`. Precisa de E2E análogo para SBOM.

#### Files to edit
```
infra/tests/e2e/supply-chain-e2e.sh — (NEW) script de validação E2E
```

#### Tasks
1. Script que faz deploy, espera completion
2. Verifica `GET /builds/:id/attestation` retorna per-app attestations
3. Verifica `GET /builds/:id/sbom` retorna metadata + download URL
4. Verifica SBOM no S3 é CycloneDX JSON válido
5. Verifica policy-controller não bloqueou o deploy (Audit mode)

#### TDD
```
RED:     Script retorna exit 1 quando SBOM ausente
GREEN:   Pipeline completo gera SBOM
VERIFY:  bash infra/tests/e2e/supply-chain-e2e.sh
```

#### Acceptance Criteria
- [ ] Deploy completo gera SBOM per-app
- [ ] Attestation endpoint retorna per-app data
- [ ] SBOM endpoint retorna metadata + download URL
- [ ] SBOM no S3 é válido (CycloneDX JSON parseable)
- [ ] policy-controller em Audit mode não bloqueia deploy

#### DoD
- [ ] Script E2E passa contra k3d local
- [ ] Resultados documentados

---

### T4.2 — Documentação e CHANGELOG

#### Objective
Atualizar maturity assessment, CHANGELOG, e docs com o Sprint 6 completo.

#### Files to edit
```
CHANGELOG.md — entrada [Unreleased] para Sprint 6 completion
docs/maturity-assessment.md — atualizar Security score e Feature Completion Matrix
```

#### Tasks
1. Adicionar entrada no CHANGELOG com todas as mudanças
2. Atualizar Security score de 3.0 para 3.5+ (com justificativa)
3. Marcar Sprint 6 como ✅ na Feature Completion Matrix
4. Atualizar Known Issues (remover "syft SBOM missing" e "admission policy doesn't reject unsigned")

#### Acceptance Criteria
- [ ] CHANGELOG atualizado com referências corretas
- [ ] Maturity assessment reflete novo estado
- [ ] Sprint 6 marcado como completo

#### DoD
- [ ] Docs atualizados
- [ ] Sem inconsistências entre CLAUDE.md e maturity-assessment.md

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | SB-3: Cosign signs but runtime doesn't verify (CVSS 7.2) | T2.1, T2.2 | policy-controller key-based verification em Audit mode |
| 2 | SBOM (syft) not implemented | T0.2, T1.1, T1.2 | Syft step no pipeline + S3 storage + endpoint |
| 3 | ADR-0014: Only Apps[0] signature persisted | T0.1 | Sidecar table `release_app_attestations` per-app |
| 4 | Admission policy doesn't reject unsigned images | T2.2 | policy-controller Audit mode (Enforce em 2 semanas) |
| 5 | SBOM no endpoint/persistence | T1.2, T3.1 | `/builds/:id/sbom` com presigned URL |
| 6 | No observability for SBOM/admission | T2.3, T3.2 | Métricas + alertas + runbooks |
| 7 | Key distribution for verification | T2.1 | Chave pública cosign no runtime cluster |
| 8 | Multi-app attestation for admission policy | T0.1, T3.1 | Per-app attestation wired end-to-end |

**Coverage: 8/8 gaps covered (100%)**

## Global Definition of Done

- [ ] All phases completed (0-4)
- [ ] All Go tests passing (`cd api && go test ./...`)
- [ ] All operator tests passing (`cd operators && go test ./...`)
- [ ] Zero lint warnings (`task lint`)
- [ ] `task validate:manifests` passes
- [ ] Pulumi preview sucede sem erros
- [ ] policy-controller healthy no runtime cluster (Audit mode)
- [ ] SBOM gerado para build de teste e persistido no S3
- [ ] E2E script `supply-chain-e2e.sh` passa
- [ ] CHANGELOG atualizado
- [ ] Maturity assessment atualizado (Security 3.0 → 3.5+)
- [ ] Sprint 6 status atualizado para ✅ no CLAUDE.md
- [ ] No internal terms in any new CLI output (Rule 3)
- [ ] SB-3 Security VETO resolved (MEET-057)
