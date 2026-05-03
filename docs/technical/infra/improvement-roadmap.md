# Infra Improvement Roadmap

**Research date:** 2026-05-03
**Researcher:** Claude (SOTA Research Skill)
**Current SOTA score:** 7.3/10
**Target SOTA score:** 9.0/10

## Executive Summary

A infraestrutura do LoopZero atinge 11/15 gates SOTA e está alinhada com as melhores práticas da indústria em 4/6 dimensões (A). A arquitetura de 3 camadas (Pulumi L1 → Helmfile L2 → ArgoCD L3) é idêntica ao padrão FAANG e validada por Humanitec, Azure Landing Zones e AWS EKS Blueprints. Os gaps principais são: (1) chart testing insuficiente (sem helm-unittest, sem values.schema.json), (2) pipeline supply chain incompleto (cosign keyless pendente, GitHub Actions não pinados por SHA), e (3) drift correction manual (sem auto-healing para apps non-root). A maturidade operacional é excepcional (75 regression protections, 60 runbooks), mas a automação de rollout e validação de drift precisa subir de nível.

## Competitive Position

| Dimensão | LoopZero | Best-in-class | Gap | Effort |
|----------|------|---------------|-----|--------|
| Layered bootstrap (L1/L2/L3) | 5/5 | 5/5 (Azure Landing Zones) | Nenhum | — |
| Local multi-cluster parity | 5/5 | 5/5 (Linkerd k3d) | Nenhum | — |
| Task orchestration | 4/5 | 5/5 (split includes) | Taskfile monolítico 2335 linhas | S |
| Dev loop (Tilt) | 5/5 | 5/5 (ArgoCD project) | Nenhum | — |
| GitOps engine (ArgoCD) | 5/5 | 5/5 (Intuit) | Nenhum | — |
| IaC testing | 3/5 | 5/5 (Pulumi dogfood) | Sem mock-based unit tests, sem property tests | M |
| Chart testing | 2/5 | 5/5 (Bitnami) | Sem helm-unittest, sem values.schema.json, sem Polaris | M |
| OCI chart distribution | 3/5 | 5/5 (Bitnami+Chainguard) | Sem cosign signing nos charts, sem digest pinning | S |
| CI/CD pipeline security | 3/5 | 5/5 (OpenSSF) | Actions pinados por tag (não SHA), sem OIDC para DO | M |
| Pipeline determinism | 4/5 | 5/5 (NTT Labs) | Sem SOURCE_DATE_EPOCH, sem SLSA provenance | S |
| Drift correction | 3/5 | 5/5 (Flux auto-remediation) | Manual intervention required | M |
| Upgrade rollout automation | 3/5 | 5/5 (Argo Rollouts) | Sem canary/blue-green automatizado | L |
| Multi-env values layering | 5/5 | 5/5 (Codefresh ref arch) | Nenhum | — |
| Operational maturity | 5/5 | 5/5 | 75 protections, 60 runbooks — excepcional | — |
| Security defaults (charts) | 3/5 | 5/5 (Bitnami) | Nem todos os 33 charts com securityContext | M |

## Quick Wins (1-2 sessions each)

1. **Pin GitHub Actions by SHA** — Trocar `actions/checkout@v4` por `actions/checkout@<sha>` em todos os 19 workflows. Motivação: supply chain attack tj-actions 2025 demonstrou o risco. → Elimina vetor de supply chain attack nos workflows.

2. **Set `SOURCE_DATE_EPOCH` em BuildKit builds** — Adicionar `--build-arg SOURCE_DATE_EPOCH=$(git log -1 --format=%ct)` nos Argo Workflow templates. → Melhora reprodutibilidade de builds (bit-for-bit quando possível).

3. **Adicionar cosign signing ao Helm chart OCI publish** — No `helm-charts.yml`, após `helm push`, adicionar `cosign sign oci://registry.../helm/loopzero-api:$VERSION`. → Fecha o gap de supply chain nos charts (imagens já são assinadas).

4. **Adicionar Polaris audit ao CI** — `polaris audit --helm-chart charts/loopzero-api --format pretty` no `helm-charts.yml`. → Detecta gaps de security context, probes e resource limits automaticamente.

5. **Split Taskfile em includes por domínio** — Criar `taskfiles/k3d.yml`, `taskfiles/helm.yml`, `taskfiles/test.yml`, `taskfiles/infra.yml` e usar `includes:` no Taskfile.yml raiz. → Reduz o monólito de 2335 linhas sem mudar a interface (`task k3d:heal` continua funcionando).

## Sprint 9 Targets

1. **Migrar para cosign keyless signing** — Substituir key-based por OIDC-based com Fulcio certificates. GitHub Actions OIDC integration: `permissions: id-token: write` + `cosign sign --yes`. Rekor transparency log para audit trail. → Elimina key management, adiciona audit trail grátis. → Arquivos: Argo Workflow templates, `build-images.yml`, `helm-charts.yml`.

2. **Adicionar `values.schema.json` a todos os 15 published charts** — Definir JSON Schema para cada chart em `charts/*/values.schema.json`. `helm lint` e `ct lint --validate-chart-schema` validam automaticamente. → Catch de misconfiguration no lint, antes de qualquer deploy. → Arquivos: `charts/*/values.schema.json` (15 novos arquivos).

3. **Adicionar helm-unittest tests para charts críticos** — Instalar `helm-unittest` plugin, criar testes para: security context rendering, ServiceMonitor conditional, NetworkPolicy toggling, image tag resolution. Prioridade: `loopzero-api`, `loopzero-controller`, operator charts. → Prova de corretude dos templates sem cluster. → Arquivos: `charts/*/tests/` (novos diretórios).

4. **Auditar securityContext em todos os 33 bootstrap charts** — Garantir `runAsNonRoot: true`, `drop: [ALL]`, `readOnlyRootFilesystem: true` em todos os containers de `infra/helm/`. → Fecha o gate INF-G08 (PARTIAL → MET). → Arquivos: `infra/helm/*/values.yaml` ou `templates/deployment.yaml`.

5. **Extrair reusable workflows para CI** — Criar `.github/workflows/reusable-setup-go.yml`, `reusable-setup-helm.yml`, `reusable-setup-node.yml`. Referenciados via `uses: ./.github/workflows/reusable-setup-go.yml`. → Reduz duplicação entre ci.yml, build-images.yml, helm-charts.yml. → Arquivos: `.github/workflows/` (3 novos + 5-6 atualizados).

## Post-Launch

1. **SLSA provenance attestation** — `cosign attest --predicate slsa.json` em cada imagem e chart. → Compliance com SLSA Level 2+, cada artefato tem provenance verificável.

2. **ArgoCD ApplicationSets para multi-cell** — Quando Cell-2 ativar, usar matrix generator (`environment × cell`) para gerar Applications automaticamente. → Reduz boilerplate de Application por cell/env.

3. **Automated canary rollout** — Deploy para dev → smoke tests automatizados → parity tests → promote staging → promote prod. Integrar com ArgoCD progressive delivery ou Argo Rollouts. → Fecha gate INF-G12 (PARTIAL → MET).

4. **Auto-drift correction** — Habilitar auto-sync para apps non-root (manter root-app manual). Definir SLA para correção manual: <1h. → Fecha gate INF-G13 (PARTIAL → MET).

5. **Pulumi mock-based unit tests** — Usar `pulumi.runtime.setMocks()` para testar resource properties sem cloud calls (DOKS cluster config, CF Tunnel, DO Spaces). → Catch config drift antes de `pulumi preview`.

6. **Pulumi Policy as Code** — Invariantes organizacionais: "all DOKS clusters must be v1.32+", "all DO droplets must have monitoring enabled", "no public-facing LB without CF Tunnel". → Enforcement automático de standards.

## New SOTA Gates Proposed

### Gate INF-G16: Supply Chain Integrity for Charts
**Objective:** Garantir que charts Helm publicados via OCI sejam assinados e verificáveis.
**Approval criteria:** Todos os charts em `charts/` são assinados com cosign após push; ArgoCD Applications podem verificar assinaturas; charts pinados por digest em Applications de produção.
**Expected evidence:** `cosign verify` passa para todos os charts no registry; CI workflow inclui sign step; Application manifests referenciam chart por digest.
**FAANG reference:** Bitnami + Chainguard publicam todos os charts com cosign signatures e SBOMs como OCI artifacts.

### Gate INF-G17: Chart Schema Validation
**Objective:** Garantir que valores incorretos sejam rejeitados no lint, não no deploy.
**Approval criteria:** Todos os 15 published charts têm `values.schema.json`; `helm lint --strict` e `ct lint --validate-chart-schema` passam no CI; chart updates incluem schema update.
**Expected evidence:** `values.schema.json` existe em cada chart; CI rejeita valores inválidos.
**FAANG reference:** Bitnami inclui JSON Schema em todos os charts. `helm lint` valida automaticamente.

### Gate INF-G18: CI Pipeline Supply Chain Security
**Objective:** Garantir que o pipeline CI não seja vulnerável a supply chain attacks.
**Approval criteria:** Todas as GitHub Actions pinadas por SHA (não tag); OIDC para autenticação com cloud providers (sem long-lived tokens); permissions explícitas e mínimas em cada workflow.
**Expected evidence:** Nenhum `@v4` ou `@v3` em workflows — apenas `@sha256:...`; `permissions:` declaradas em todos os workflows; sem secrets de longa duração para DO/CF.
**FAANG reference:** GitHub Security Roadmap 2026 recomenda SHA pinning após tj-actions supply chain attack de 2025. OpenSSF Scorecard penaliza tag-based pinning.

### Gate INF-G19: Build Reproducibility
**Objective:** Same commit → same artifact, verificável criptograficamente.
**Approval criteria:** `SOURCE_DATE_EPOCH` setado em todas as builds BuildKit; base images pinadas por digest; SLSA provenance attestation em cada artefato.
**Expected evidence:** Duas builds do mesmo commit produzem images com mesmo digest (ou diferença explicável e documentada); `cosign verify-attestation` passa para SLSA provenance.
**FAANG reference:** NTT Labs demonstrou bit-for-bit reproducible builds com BuildKit + SOURCE_DATE_EPOCH. Chainguard publica SLSA provenance em todos os artefatos.

## Anti-Patterns to Eliminate

1. **GitHub Actions pinned by tag** — Tags são mutáveis. O ataque tj-actions de 2025 provou que `@v4` pode ser reescrito para injetar código malicioso. → Pinar por SHA: `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11`. → Arquivos: todos os 19 `.github/workflows/*.yml`.

2. **Taskfile monolítico** — 2335 linhas em um arquivo dificulta onboarding e manutenção. Taskfile suporta `includes:` nativamente. → Split em `taskfiles/{k3d,helm,test,infra,db,dev}.yml`. → Arquivo: `Taskfile.yml`.

3. **Charts sem values.schema.json** — Valores errados são aceitos silenciosamente no lint e falham no deploy. → Criar schema por chart. → Arquivos: `charts/*/values.schema.json` (15 novos).

4. **Security context inconsistente** — Alguns charts têm `runAsNonRoot: true`, outros não. → Audit e padronizar em todos os 33 bootstrap charts + 15 published charts. → Arquivos: `infra/helm/*/` e `charts/*/`.

5. **Cosign key-based signing** — Requer gerenciamento de chaves e rotação. Cosign keyless é o padrão recomendado desde 2024. → Migrar para keyless com Fulcio + Rekor. → Arquivos: Argo Workflow templates, CI workflows.

## Sources

### Bootstrap & Architecture
- [Humanitec + Crossplane Architecture](https://humanitec.com/blog/crossplane-upbound-internal-developer-platforms-humanitec) — RMCD pattern, Score workload spec
- [Score Workload Specification (CNCF Sandbox)](https://humanitec.com/products/score) — Developer-facing workload abstraction
- [Platform Engineering Tools 2026](https://spacelift.io/blog/platform-engineering-tools) — Layered IaC survey
- [GitOps Architecture Patterns and Anti-Patterns](https://platformengineering.org/blog/gitops-architecture-patterns-and-anti-patterns) — App-of-Apps validation

### Local Development
- [k3d Developer Guide](https://dev.to/cloudx/why-you-should-use-k3d-for-local-development-a-developers-guide-2cp5) — Multi-cluster config patterns
- [Local Kubernetes Showdown 2025](https://sanj.dev/post/2025-12-11-ultimate-local-kubernetes-showdown-2025) — k3d vs kind vs minikube benchmarks
- [Tilt vs Skaffold vs DevSpace (vcluster)](https://www.vcluster.com/blog/skaffold-vs-tilt-vs-devspace) — Dev loop comparison
- [Tilt Alternatives 2026 (Northflank)](https://northflank.com/blog/tilt-alternatives) — Tilt status post-Docker acquisition

### Task Orchestration
- [Taskfile vs Just vs Make](https://mylinux.work/guides/taskfile-vs-just-vs-make/) — Feature comparison
- [Taskfile: Modern Alternative to Makefile](https://marmelab.com/blog/2026/03/12/taskfile-alternative-makefile.html) — Adoption trends

### GitOps
- [ArgoCD vs FluxCD 2026](https://dev.to/pockit_tools/github-actions-in-2026-the-complete-guide-to-monorepo-cicd-and-self-hosted-runners-1jop) — 60% ArgoCD adoption
- [ApplicationSet vs App of Apps](https://timesofcloud.com/argocd/applicationset-vs-app-of-apps/) — Pattern comparison
- [Flux vs ArgoCD (Spacelift)](https://spacelift.io/blog/flux-vs-argo-cd) — Graduated project comparison

### Helm & Charts
- [Bitnami Chart Architecture and Common Patterns](https://deepwiki.com/bitnami/charts/2-chart-architecture-and-common-patterns) — Library chart gold standard
- [bjw-s Common Library Architecture](https://bjw-s-labs.github.io/helm-charts/docs/common-library/) — Full resource generation pipeline
- [Helm OCI Registries](https://helm.sh/docs/topics/registries/) — OCI distribution specification
- [Goodbye index.yaml, Hello OCI](https://devopsie.com/2025-10-14/goodbye-index-yaml-hello-oci-a-hands-on-helm-guide-to-pushing-and-signing-your-charts.html) — OCI + cosign signing
- [Helm Chart Testing Best Practices](https://alexandre-vazquez.com/helm-chart-testing-best-practices/) — Testing pyramid for charts
- [Quality Gate for Helm Charts](https://medium.com/@michamarszaek/quality-gate-for-helm-charts-f260f5742198) — helm-unittest + kubeconform + Polaris

### CI/CD & Supply Chain
- [GitHub Actions 2026 Security Roadmap](https://github.blog/news-insights/product-news/whats-coming-to-our-github-actions-2026-security-roadmap/) — SHA pinning, OIDC, permissions
- [CI/CD Performance Optimization 2026](https://dasroot.net/posts/2026/04/ci-cd-performance-optimization-faster-builds-deployments/) — Caching, parallelism
- [Bit-for-bit Reproducible Builds with Dockerfile](https://medium.com/nttlabs/bit-for-bit-reproducible-builds-with-dockerfile-7cc2b9faed9f) — SOURCE_DATE_EPOCH pattern
- [Sigstore Cosign Overview](https://docs.sigstore.dev/cosign/signing/overview/) — Keyless signing reference
- [OpenSSF: Scaling Supply Chain Security with Sigstore](https://openssf.org/blog/2024/02/16/scaling-up-supply-chain-security-implementing-sigstore-for-seamless-container-image-signing/) — Enterprise adoption patterns
- [Supply Chain Security 2025: SBOMs, SLSA & Sigstore](https://faithforgelabs.com/blog_supplychain_security_2025.php) — SLSA provenance attestation

### IaC Testing
- [Pulumi Testing Docs](https://www.pulumi.com/docs/iac/guides/testing/) — 3-tier testing model
- [Pulumi Unit Testing](https://www.pulumi.com/docs/iac/guides/testing/unit/) — Mock-based resource testing
- [IaC Testing Strategies](https://autoshiftops.com/devops/iac/testing/2025/06/22/Infrastructure-as-Code-Testing-Strategies.html) — Survey of approaches

### Multi-Environment
- [Codefresh: Helm Values in Multi-Source ArgoCD](https://codefresh.io/blog/helm-values-argocd/) — Values layering reference architecture
- [Helmfile Best Practices Guide](https://helmfile.readthedocs.io/en/latest/writing-helmfile/) — Environment management
- [3 Patterns for Deploying Helm Charts with ArgoCD (Red Hat)](https://developers.redhat.com/articles/2023/05/25/3-patterns-deploying-helm-charts-argocd) — Anti-patterns to avoid
