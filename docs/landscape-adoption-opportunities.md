# CNCF Landscape — Oportunidades de Adoção para o LoopZero

**Data:** 2026-03-30
**Fonte:** [CNCF Landscape — Projects & Products](https://landscape.cncf.io/?group=projects-and-products)
**Objetivo:** Identificar projetos e serviços do ecossistema CNCF que podem ser aplicados no LoopZero para fechar lacunas existentes ou substituir componentes atuais por alternativas mais maduras.

---

## Metodologia

Cada recomendação foi avaliada com base em:

1. **Lacuna real no LoopZero** — existe uma necessidade concreta hoje ou planejada nos sprints
2. **Maturidade do projeto** — graduated/incubating tem preferência sobre sandbox
3. **Fit arquitetural** — compatível com a stack atual (Go, K8s, multi-cluster, GitOps)
4. **Custo operacional** — overhead de operação vs benefício
5. **Alternativa existente** — se já temos algo que funciona, precisa justificar a troca

### Classificação de Prioridade

| Prioridade | Significado |
|-----------|-------------|
| 🔴 **Crítica** | Lacuna de segurança ou blocker de produção. Deve ser resolvida antes do launch |
| 🟠 **Alta** | Melhoria significativa em resiliência, operação ou DX. Próximos sprints |
| 🟡 **Média** | Otimização ou substituição que traz valor mas não bloqueia |
| 🟢 **Baixa** | Nice-to-have para futuro. Monitorar maturidade |

---

## Resumo Executivo

| # | Projeto CNCF | Substitui/Complementa | Prioridade | Sprint Alvo |
|---|-------------|----------------------|-----------|-------------|
| 1 | **Kyverno** (graduated) | Nenhum (gap de policy) | 🔴 Crítica | 6 (Security) |
| 2 | **Cosign + in-toto** (graduated) | Nenhum (gap de supply chain) | 🔴 Crítica | 6 (Security) |
| 3 | **Falco** (graduated) | Nenhum (gap de runtime security) | 🟠 Alta | 7 (Observability) |
| 4 | **CloudNativePG** (sandbox) | bitnami/postgresql-ha Helm chart | 🟠 Alta | Hardening |
| 5 | **Chaos Mesh** (incubating) | Nenhum (gap de chaos testing) | 🟠 Alta | 8 (Chaos) |
| 6 | **KEDA** (graduated) | Nenhum (gap de autoscaling) | 🟡 Média | 9 (Polish) |
| 7 | **CloudEvents** (graduated) | SSE events custom format | 🟡 Média | 7 (Observability) |
| 8 | **OpenCost** (incubating) | Nenhum (gap de cost visibility) | 🟡 Média | 9 (Polish) |
| 9 | **Harbor** (graduated) | DO Container Registry | 🟡 Média | Futuro |
| 10 | **OpenFeature** (incubating) | Nenhum (gap de feature flags) | 🟢 Baixa | Futuro |
| 11 | **Velero** | Nenhum (gap de backup/DR) | 🟠 Alta | Hardening |
| 12 | **Cilium** (graduated) | NetworkPolicy básica | 🟡 Média | Futuro |
| 13 | **Argo Rollouts** (parte do Argo graduated) | Deploy direto (replace all) | 🟡 Média | 4 (Rollback) |
| 14 | **NATS** (incubating) | pg NOTIFY/LISTEN para SSE | 🟢 Baixa | Futuro |
| 15 | **Buildpacks** (incubating) | Detecção automática sem Dockerfile | 🟡 Média | 9 (Polish) |

---

## Análise Detalhada

### 1. Kyverno — Policy Engine 🔴

**CNCF Status:** Graduated
**Categoria:** Security & Compliance
**Site:** https://kyverno.io
**Repo:** https://github.com/kyverno/kyverno

**Lacuna que fecha:**
O LoopZero não tem admission control. Pods podem rodar como root, sem resource limits, com hostNetwork, ou com imagens sem assinatura. O plano de workload protection (`docs/plans/workload-protection.md`) já lista Kyverno como Phase 2, mas deveria ser Phase 1.

**O que Kyverno faz pelo LoopZero:**

| Policy | Impacto |
|--------|---------|
| `disallow-privileged-containers` | Impede pods privilegiados no workload cluster |
| `require-run-as-nonroot` | Força SecurityContext em todos os pods de tenant |
| `require-resource-limits` | Garante que todo pod tem limits de CPU/mem |
| `verify-image-signatures` | Valida assinatura cosign antes de admitir imagem |
| `disallow-latest-tag` | Bloqueia imagens com `:latest` |
| `restrict-hostpath` | Impede montagem de volumes do host |
| `restrict-nodeport` | Impede serviços NodePort (only ClusterIP + Ingress) |

**Fit arquitetural:**
- Instala como Helm chart no workload cluster
- Policies são CRDs (ClusterPolicy) — GitOps-friendly via ArgoCD
- Integra com cosign para verificação de assinatura
- Pode gerar reports de compliance (PolicyReport CRD)
- Sem sidecars, sem service mesh dependency

**Alternativa considerada:** OPA/Gatekeeper (também graduated). Kyverno vence por:
- Policies em YAML nativo (não Rego — curva de aprendizado menor)
- Mutating + validating + generating em um só recurso
- Verificação de imagem nativa (sem webhook externo)
- Mais adotado em cenários PaaS multi-tenant

**Custo operacional:** Baixo. 1 Deployment + webhook. ~100MB RAM.

**Sprint alvo:** 6 (Security + Supply Chain)

---

### 2. Cosign + in-toto — Supply Chain Security 🔴

**CNCF Status:** in-toto (Graduated), cosign faz parte do ecossistema Sigstore
**Categoria:** Security & Compliance
**Site:** https://www.sigstore.dev (cosign), https://in-toto.io
**Repos:** https://github.com/sigstore/cosign, https://github.com/in-toto/in-toto

**Lacuna que fecha:**
O LoopZero builda e pusha imagens sem assinatura. Qualquer imagem no registry pode ser deployada, incluindo imagens tampereadas. O CLAUDE.md já menciona "cosign + syft — Sprint 6 — not yet implemented".

**O que cosign + in-toto fazem pelo LoopZero:**

```
Build Pipeline (Argo Workflows)
  → BuildKit produz imagem
  → cosign sign (assina imagem com chave ou keyless/Fulcio)
  → syft (gera SBOM em formato SPDX/CycloneDX)
  → in-toto (atesta que o build seguiu o pipeline esperado)
  → push to registry (imagem + assinatura + SBOM + attestation)

Deploy Pipeline (ArgoCD + Kyverno)
  → Kyverno verifica assinatura cosign antes de admitir
  → Rejeita imagens não assinadas ou com SBOM ausente
```

**Integração com Argo Workflows:**
- Adicionar step `cosign-sign` após `build-push` no ClusterWorkflowTemplate
- Chave de assinatura armazenada como K8s Secret (ou keyless via Fulcio para zero key management)
- Attestation via `cosign attest` com predicado in-toto

**Alternativa considerada:** Notary Project (CNCF Incubating). Cosign vence por:
- Adoção massiva (padrão de facto para container signing)
- Integração nativa com Kyverno
- Keyless signing via Fulcio (zero key management)
- CLI simples (`cosign sign`, `cosign verify`)

**Custo operacional:** Mínimo. Apenas CLI tools no pipeline. Sem daemon.

**Sprint alvo:** 6 (Security + Supply Chain)

---

### 3. Falco — Runtime Security Monitoring 🟠

**CNCF Status:** Graduated
**Categoria:** Security & Compliance
**Site:** https://falco.org
**Repo:** https://github.com/falcosecurity/falco

**Lacuna que fecha:**
O LoopZero não tem visibilidade de runtime security. Se um container de tenant executa `curl` para exfiltrar dados, faz privilege escalation, ou acessa o metadata service da cloud, ninguém sabe.

**O que Falco faz pelo LoopZero:**

| Detecção | Exemplo |
|----------|---------|
| Shell spawned in container | `kubectl exec` ou exploit RCE em app de tenant |
| Sensitive file access | Leitura de `/etc/shadow`, `/proc/1/environ` |
| Network anomaly | Conexão para IP de crypto mining |
| Privilege escalation | `setuid`, `ptrace`, capability abuse |
| Drift detection | Binário executado que não existia na imagem original |

**Fit arquitetural:**
- DaemonSet no workload cluster (eBPF driver, sem kernel module)
- Output como JSON para stdout → Promtail → Loki (já temos)
- Ou: Falcosidekick → Prometheus metrics → alertas existentes
- Rules customizáveis por namespace (tenant isolation)
- Integra com Grafana dashboards (já temos)

**Por que é importante para PaaS multi-tenant:**
Em um PaaS, o LoopZero aceita código de terceiros e roda em containers. Falco é a última linha de defesa — detecta comportamento malicioso que passou pelo Kyverno admission.

**Custo operacional:** Médio. DaemonSet em cada node do workload cluster. ~200MB RAM/node.

**Sprint alvo:** 7 (Observability)

---

### 4. CloudNativePG — Postgres Operator 🟠

**CNCF Status:** Sandbox
**Categoria:** Database
**Site:** https://cloudnative-pg.io
**Repo:** https://github.com/cloudnative-pg/cloudnative-pg

**Lacuna que fecha:**
O LoopZero usa o Helm chart `bitnami/postgresql-ha` com Patroni para HA. Funciona, mas tem limitações operacionais: backup manual, sem point-in-time recovery (PITR), sem connection pooling integrado, failover lento comparado com CNPG.

**O que CloudNativePG faz melhor:**

| Feature | bitnami/postgresql-ha | CloudNativePG |
|---------|----------------------|---------------|
| Failover | Patroni (10-30s) | Nativo K8s (<10s) |
| Backup | Manual (pg_dump) | Contínuo WAL archiving para S3/MinIO |
| PITR | ❌ Não suportado | ✅ Point-in-time recovery |
| Connection pooling | PgBouncer externo | PgBouncer integrado |
| Monitoring | Prometheus exporter manual | Métricas nativas |
| Rolling updates | Arriscado | Zero-downtime rolling |
| Declarative | Helm values | CRD (Cluster) — GitOps native |
| Fencing | Manual | Automático em split-brain |

**Fit arquitetural:**
- CRD `Cluster` gerenciado pelo operator — GitOps via ArgoCD
- Backup contínuo para MinIO (já temos) ou DO Spaces
- PITR alinhado com o spec §3 (source of truth hierarchy)
- PgBouncer integrado elimina overhead de pool externo
- O LoopZero já usa `pgx` (connection pool) — CNPG PgBouncer é transparente

**Nota:** No ambiente de produção em DigitalOcean, o LoopZero usa PostgreSQL gerenciado (DO Managed Database). CNPG seria mais relevante para:
- Self-hosted (on-prem ou bare metal)
- Ambientes onde managed DB não está disponível
- Controle total sobre backup/PITR

**Risco:** Sandbox CNCF (não graduated). Mas: mantido pela EDB, adoção significativa, releases frequentes.

**Custo operacional:** Menor que bitnami/postgresql-ha. O operator gerencia tudo.

**Sprint alvo:** Avaliar no próximo hardening sprint. Não é urgente com DO Managed DB.

---

### 5. Chaos Mesh — Chaos Engineering 🟠

**CNCF Status:** Incubating
**Categoria:** Chaos Engineering
**Site:** https://chaos-mesh.org
**Repo:** https://github.com/chaos-mesh/chaos-mesh

**Lacuna que fecha:**
Sprint 8 (Hardening + Chaos) exige "50 concurrent deploys, all chaos passes". O LoopZero tem skeleton de chaos tests em `infra/tests/chaos/` mas marcados como `Pending` (false green). Não há ferramenta de fault injection.

**O que Chaos Mesh faz pelo LoopZero:**

| Teste | CRD Chaos Mesh | Cenário LoopZero |
|-------|----------------|--------------|
| Pod kill | PodChaos (pod-kill) | Build pod morre mid-build → pipeline retry correto? |
| Network delay | NetworkChaos (delay) | ArgoCD sync com 5s latency → timeout handling? |
| Network partition | NetworkChaos (partition) | Workload cluster isolado → API degrada gracefully? |
| IO fault | IOChaos (errno) | PostgreSQL I/O error → advisory lock release? |
| DNS failure | DNSChaos | Gitea DNS unreachable → circuit breaker ativa? |
| Time skew | TimeChaos | Clock drift no build cluster → TTL/timeout correto? |
| Stress | StressChaos (cpu/mem) | Node com 90% CPU → pod eviction handling? |

**Fit arquitetural:**
- Instala via Helm no management cluster
- Chaos experiments são CRDs — programáticos via Ginkgo tests
- Pode rodar em schedule (CronChaos) para validação contínua
- Dashboard web opcional para exploração manual
- Integra com Grafana para correlacionar caos com métricas

**Integração com testes existentes:**
```go
// infra/tests/chaos/build_resilience_test.go
It("should recover when build pod is killed mid-build", func() {
    // Arrange: deploy Chaos Mesh PodChaos targeting argo namespace
    // Act: trigger loopzero deploy
    // Assert: build retries and succeeds within timeout
})
```

**Alternativa considerada:** Litmus (CNCF Incubating). Chaos Mesh vence por:
- CRDs mais simples e intuitivos
- Melhor integração com Go/Ginkgo tests
- Dashboard mais leve
- Maior adoção em cenários K8s-native

**Custo operacional:** Médio. Controller + DaemonSet. ~200MB RAM.

**Sprint alvo:** 8 (Hardening + Chaos)

---

### 6. KEDA — Event-Driven Autoscaling 🟡

**CNCF Status:** Graduated
**Categoria:** Scheduling & Orchestration
**Site:** https://keda.sh
**Repo:** https://github.com/kedacore/keda

**Lacuna que fecha:**
Apps de tenant no LoopZero usam HPA estático (se configurado). Não há scale-to-zero, não há autoscaling baseado em métricas custom (queue depth, HTTP rate, cron). Para um PaaS, autoscaling inteligente é diferencial de DX.

**O que KEDA faz pelo LoopZero:**

| Scaler | Cenário |
|--------|---------|
| `prometheus` | Scale baseado em request rate (Prometheus metrics) |
| `cron` | Scale por horário (reduz custos em horários ociosos) |
| `cpu/memory` | HPA padrão, mas com scale-to-zero |
| `postgresql` | Scale baseado em queue depth (jobs pendentes) |

**Scale-to-zero é o killer feature:**
- Apps de tenant sem tráfego → 0 pods → custo zero
- Primeiro request → KEDA escala para 1 pod (cold start ~3-5s)
- Modelo Vercel/Render: pay-per-use sem pods idle

**Fit arquitetural:**
- Instala via Helm no workload cluster
- `ScaledObject` CRD por app — gerado pelo LoopZero na fase de manifests
- Integra com Prometheus (já temos) para métricas custom
- Não conflita com HPA existente (substitui)

**Impacto no `loopzero.yaml`:**
```yaml
# loopzero.yaml v3 (futuro)
scaling:
  min: 0        # scale-to-zero habilitado
  max: 10
  metric: http   # ou cpu, cron, custom
```

**Custo operacional:** Baixo. 1 Deployment + metrics server. ~100MB RAM.

**Sprint alvo:** 9 (Polish) ou feature sprint dedicado

---

### 7. CloudEvents — Formato Padrão de Eventos 🟡

**CNCF Status:** Graduated
**Categoria:** Streaming & Messaging
**Site:** https://cloudevents.io
**Repo:** https://github.com/cloudevents/spec

**Lacuna que fecha:**
O LoopZero usa SSE com formato custom para eventos de build/deploy. O spec é interno e não segue um padrão da indústria. Isso dificulta integração com ferramentas externas (webhooks, Slack, CI/CD).

**O que CloudEvents padroniza:**

Formato atual (custom):
```json
{"type": "build.status", "data": {"buildId": "...", "state": "RUNNING"}}
```

Formato CloudEvents:
```json
{
  "specversion": "1.0",
  "type": "dev.loopzero.build.status.changed",
  "source": "/tenants/acme/projects/api/environments/production",
  "id": "bld_abc123",
  "time": "2026-03-30T10:00:00Z",
  "datacontenttype": "application/json",
  "data": {
    "buildId": "bld_abc123",
    "state": "RUNNING",
    "previousState": "QUEUED"
  }
}
```

**Benefícios:**
- Interoperabilidade com qualquer sistema que suporte CloudEvents (Knative, Argo Events, Dapr)
- Webhooks padronizados para integrações de CI/CD
- SDK disponível em Go e TypeScript (ambas linguagens do LoopZero)
- Spec estável (graduated) — não vai mudar

**Fit arquitetural:**
- Não é componente para instalar — é spec + SDK
- Apenas refatorar o formato dos SSE events e webhooks (ArgoCD callback)
- Go SDK: `github.com/cloudevents/sdk-go`
- TS SDK: `cloudevents` npm package

**Custo operacional:** Zero. Apenas formatação de dados.

**Sprint alvo:** 7 (Observability) — alinhar com refactor de eventos

---

### 8. OpenCost — Visibilidade de Custos 🟡

**CNCF Status:** Incubating
**Categoria:** Continuous Optimization
**Site:** https://www.opencost.io
**Repo:** https://github.com/opencost/opencost

**Lacuna que fecha:**
O LoopZero não tem visibilidade de custo por tenant. Em um PaaS multi-tenant, saber quanto cada tenant consome (CPU, memória, storage, network) é essencial para pricing e capacity planning.

**O que OpenCost faz pelo LoopZero:**

| Métrica | Granularidade |
|---------|--------------|
| CPU cost | Per namespace (= per tenant+env) |
| Memory cost | Per namespace |
| Storage cost | Per PVC |
| Network cost | Per pod |
| Idle cost | Recursos alocados mas não usados |

**Fit arquitetural:**
- Instala como sidecar do Prometheus (já temos)
- Scraping automático de métricas K8s + cloud pricing API
- Namespace = tenant boundary → custo por tenant automático
- Dashboard Grafana (plugin oficial)
- API para expor custos no CLI (`loopzero cost` futuro)

**Impacto futuro:**
```bash
$ loopzero cost --project my-api --environment production
  CPU:     $12.40/mo  (0.5 vCPU avg)
  Memory:  $8.20/mo   (256Mi avg)
  Storage: $2.00/mo   (1Gi PVC)
  Total:   $22.60/mo
```

**Custo operacional:** Mínimo. 1 Deployment alongside Prometheus. ~100MB RAM.

**Sprint alvo:** 9 (Polish) — nice-to-have para launch

---

### 9. Harbor — Container Registry 🟡

**CNCF Status:** Graduated
**Categoria:** Container Registry
**Site:** https://goharbor.io
**Repo:** https://github.com/goharbor/harbor

**Lacuna que fecha:**
O LoopZero usa DO Container Registry (managed). Funciona, mas:
- Sem vulnerability scanning integrado
- Sem replication policies (multi-region)
- Sem garbage collection granular
- Sem image signing verification nativa
- Vendor lock-in

**O que Harbor faz melhor:**

| Feature | DO Registry | Harbor |
|---------|-------------|--------|
| Vulnerability scan | ❌ | ✅ (Trivy integrado) |
| Image signing | ❌ | ✅ (cosign + Notary) |
| Replication | ❌ | ✅ (multi-registry sync) |
| Garbage collection | Basic | ✅ (por tag, por repo, por idade) |
| RBAC per project | ❌ | ✅ (tenant isolation nativa) |
| Audit logs | ❌ | ✅ |
| Webhooks | ❌ | ✅ (push, scan, sign events) |
| Quota per project | ❌ | ✅ (storage quota per tenant) |

**Fit arquitetural:**
- Instala via Helm no management cluster
- Cada tenant = Harbor project (RBAC isolado)
- Integra com cosign para signing verification
- Trivy scan automático em cada push
- Replication para DR (multi-registry)

**Trade-off:** Harbor é pesado (~1GB RAM, PostgreSQL próprio, Redis). Em produção DO com managed registry, pode não justificar. Mais relevante para self-hosted ou on-prem.

**Custo operacional:** Alto. Múltiplos componentes (core, jobservice, registry, trivy, redis, db).

**Sprint alvo:** Futuro — avaliar quando DO Registry limitar

---

### 10. OpenFeature — Feature Flags 🟢

**CNCF Status:** Incubating
**Categoria:** Feature Flagging
**Site:** https://openfeature.dev
**Repo:** https://github.com/open-feature/spec

**Lacuna que fecha:**
O LoopZero não tem feature flags. Para rollout gradual de features do próprio LoopZero (não dos apps de tenant), OpenFeature padronizaria a abordagem.

**Relevância para o LoopZero:**

| Cenário | Uso |
|---------|-----|
| Rollout de static delivery | Flag `static-delivery-enabled` por tenant |
| Preview environments beta | Flag `preview-envs-enabled` por tenant |
| Novo build engine (Buildpacks) | Flag `buildpacks-enabled` por project |
| Rate limiting tiers | Flag `rate-limit-tier` por tenant |

**Fit arquitetural:**
- Go SDK (`github.com/open-feature/go-sdk`) no API
- Provider: flagd (lightweight, K8s-native) ou Flipt (self-hosted)
- Flags como CRDs (FeatureFlag) — GitOps via ArgoCD

**Custo operacional:** Baixo. flagd é um sidecar leve.

**Sprint alvo:** Futuro — útil quando houver features para rollout gradual

---

### 11. Velero — Backup & Disaster Recovery 🟠

**CNCF Status:** Não é CNCF (VMware/Broadcom), mas padrão de facto
**Categoria:** Cloud Native Storage (adjacent)
**Site:** https://velero.io
**Repo:** https://github.com/vmware-tanzu/velero

**Lacuna que fecha:**
O LoopZero não tem estratégia de backup documentada. Se o management cluster corromper, não há recovery path além de full recreate via Pulumi (validado, mas perde dados de state).

**O que Velero faz pelo LoopZero:**

| Backup | Target | Frequência |
|--------|--------|-----------|
| PostgreSQL (via CSI snapshot) | DO Spaces | Diário |
| Gitea PVCs | DO Spaces | Diário |
| CRDs (Tenant, Environment, etc.) | DO Spaces | A cada mudança |
| Namespaces completos | DO Spaces | Semanal |
| MinIO buckets | DO Spaces (cross-copy) | Diário |

**Fit arquitetural:**
- Instala via Helm no management cluster
- Backup storage: DO Spaces (S3-compatible, já temos)
- CSI snapshots para PVCs (PostgreSQL, Gitea)
- Schedule CRDs para backup automático
- `velero restore` para DR

**Alternativa:** Longhorn (CNCF Incubating) inclui backup, mas é storage completo. Velero é mais leve para backup-only.

**Custo operacional:** Baixo. 1 Deployment + BackupStorageLocation. ~100MB RAM.

**Sprint alvo:** Hardening sprint — antes de launch

---

### 12. Cilium — Advanced Networking 🟡

**CNCF Status:** Graduated
**Categoria:** Cloud Native Network
**Site:** https://cilium.io
**Repo:** https://github.com/cilium/cilium

**Lacuna que fecha:**
O LoopZero usa NetworkPolicy básica (deny-all + allowlist por namespace). Funciona, mas:
- Sem network observability (quem fala com quem)
- Sem DNS-aware policies (allow `*.github.com` mas block `evil.com`)
- Sem encryption em trânsito (mTLS without service mesh)
- Sem bandwidth limiting per tenant

**O que Cilium adiciona:**

| Feature | NetworkPolicy | Cilium |
|---------|--------------|--------|
| L3/L4 filtering | ✅ | ✅ |
| L7 filtering (HTTP) | ❌ | ✅ (allow GET /api, block DELETE) |
| DNS-aware policies | ❌ | ✅ (`toFQDNs`) |
| Transparent encryption | ❌ | ✅ (WireGuard ou IPsec) |
| Network observability | ❌ | ✅ (Hubble UI + Grafana) |
| Bandwidth limiting | ❌ | ✅ (per-pod annotation) |
| Identity-based | IP-based | ✅ (pod identity) |

**Trade-off:** Cilium substitui o CNI inteiro. Em k3d (Flannel) e DOKS (Cilium por padrão!), a migração é diferente:
- **DOKS:** Já usa Cilium! Apenas habilitar features avançadas
- **k3d (local):** Requer trocar CNI de Flannel para Cilium

**Custo operacional:** Já incluso no DOKS. Para local, substituição de CNI é invasiva.

**Sprint alvo:** Futuro — investigar features avançadas no DOKS existente

---

### 13. Argo Rollouts — Progressive Delivery 🟡

**CNCF Status:** Parte do Argo (Graduated)
**Categoria:** CI/CD (sub-projeto)
**Site:** https://argoproj.github.io/rollouts
**Repo:** https://github.com/argoproj/argo-rollouts

**Lacuna que fecha:**
O LoopZero faz deploy "replace all" — mata pods antigos, sobe novos. Não há canary, blue-green, ou análise automática. Um deploy ruim derruba 100% dos usuários.

**O que Argo Rollouts faz pelo LoopZero:**

| Estratégia | Comportamento |
|-----------|--------------|
| **Canary** | 10% → análise → 50% → análise → 100% |
| **Blue-Green** | Nova versão sobe em paralelo, switch instantâneo |
| **Analysis** | Prometheus query durante rollout (error rate < 1%?) |
| **Auto-rollback** | Se analysis falha → reverte automaticamente |

**Fit arquitetural:**
- Instala via Helm no workload cluster
- `Rollout` CRD substitui `Deployment` nos manifests gerados
- Integra com Traefik para traffic splitting (canary)
- Integra com Prometheus para analysis (já temos)
- ArgoCD suporta Rollout CRD nativamente

**Impacto no `loopzero.yaml`:**
```yaml
# loopzero.yaml v3 (futuro)
deploy:
  strategy: canary    # ou blue-green, rolling (default)
  canary:
    steps:
      - setWeight: 20
      - pause: {duration: 60s}
      - setWeight: 50
      - pause: {duration: 60s}
```

**Trade-off:** Adiciona complexidade ao manifest generation. Para MVP, "replace all" com rollback rápido (3s) pode ser suficiente.

**Custo operacional:** Baixo. 1 Deployment no workload cluster. ~100MB RAM.

**Sprint alvo:** Sprint 4 (Rollback + Promote) — ou post-launch

---

### 14. NATS — Messaging Layer 🟢

**CNCF Status:** Incubating
**Categoria:** Streaming & Messaging
**Site:** https://nats.io
**Repo:** https://github.com/nats-io/nats-server

**Lacuna que fecha:**
O LoopZero usa PostgreSQL NOTIFY/LISTEN para SSE events. Funciona, mas:
- Mensagens perdidas se ninguém está escutando (no durability)
- Não escala para múltiplas instâncias da API (cada uma tem seu listener)
- Sem replay de eventos históricos
- Acoplado ao PostgreSQL

**O que NATS faz melhor:**

| Feature | pg NOTIFY/LISTEN | NATS JetStream |
|---------|-----------------|----------------|
| Durability | ❌ Fire-and-forget | ✅ Persistido em stream |
| Multi-consumer | ❌ 1 listener per conn | ✅ Consumer groups |
| Replay | ❌ | ✅ Replay from offset |
| Scaling | ❌ 1 API instance | ✅ N API instances |
| Cross-cluster | ❌ | ✅ Multi-cluster leafnodes |

**Fit arquitetural:**
- Instala via Helm no management cluster
- Go SDK nativo (`github.com/nats-io/nats.go`)
- Subjects: `loopzero.builds.{tenant}.{project}.{env}` → SSE per-build
- JetStream para durability (sobrevive restart)
- Leafnodes para cross-cluster (build events → mgmt cluster)

**Trade-off:** pg NOTIFY/LISTEN funciona para o volume atual. NATS adiciona mais um componente para operar. Justifica quando:
- Múltiplas instâncias da API (HA)
- Volume de eventos > 100/s
- Cross-cluster events (build cluster → API)

**Custo operacional:** Médio. NATS cluster (3 nodes para HA). ~100MB RAM/node.

**Sprint alvo:** Futuro — quando API precisar escalar para múltiplas instâncias

---

### 15. Buildpacks — Zero-Dockerfile Builds 🟡

**CNCF Status:** Incubating
**Categoria:** Application Definition & Image Build
**Site:** https://buildpacks.io
**Repo:** https://github.com/buildpacks

**Lacuna que fecha:**
O LoopZero exige Dockerfile (ou gera um via framework detection). Buildpacks eliminam essa necessidade — detectam linguagem/framework e buildam automaticamente, sem Dockerfile.

**O que Buildpacks faz pelo LoopZero:**

| Cenário | Hoje (BuildKit) | Com Buildpacks |
|---------|----------------|----------------|
| Node.js app sem Dockerfile | Gera Dockerfile via detection | Auto-detect, zero config |
| Go app sem Dockerfile | Gera Dockerfile via detection | Auto-detect, zero config |
| Python app sem Dockerfile | Gera Dockerfile via detection | Auto-detect, zero config |
| Multi-stage optimization | Manual no Dockerfile | Automático (pack layers) |
| Security patching | Rebuild imagem | Rebase (base image swap sem rebuild) |

**Killer feature: `pack rebase`**
Atualiza a base image (security patches) sem rebuildar a app. Instantâneo. Isso é transformador para um PaaS — patching em massa de todos os tenants em segundos.

**Fit arquitetural:**
- Integra como alternativa ao BuildKit no Argo Workflows pipeline
- `pack` CLI ou lifecycle como container no workflow
- Paketo Buildpacks (mantidos pela VMware/Broadcom, adoção massiva)
- Coexiste com BuildKit — apps com Dockerfile usam BuildKit, apps sem Dockerfile usam Buildpacks

**Impacto no `loopzero.yaml`:**
```yaml
# loopzero.yaml v3 (futuro)
build:
  engine: auto       # auto-detect: Dockerfile → BuildKit, sem Dockerfile → Buildpacks
  # ou forçar:
  engine: buildpacks  # ignora Dockerfile, usa Buildpacks
  engine: dockerfile   # exige Dockerfile
```

**Trade-off:** Buildpacks são mais lentos que BuildKit para builds custom. Mas para apps "convencionais" (Node/Go/Python/Java), são mais simples e seguros.

**Custo operacional:** Zero adicional. Apenas imagem `pack` no workflow.

**Sprint alvo:** 9 (Polish) — como engine alternativo

---

## Projetos Avaliados e Descartados

| Projeto | CNCF Status | Por que descartado |
|---------|-------------|-------------------|
| **Istio/Linkerd** (Service Mesh) | Graduated | Overhead excessivo para PaaS. Cilium + NetworkPolicy resolvem. mTLS via Cilium WireGuard |
| **Crossplane** | Graduated | O LoopZero já tem Pulumi para IaC. Crossplane é complementar mas não necessário |
| **Dapr** | Graduated | Framework runtime — útil para apps de tenant, mas não para o LoopZero em si |
| **Knative** | Graduated | Serverless — KEDA cobre scale-to-zero sem a complexidade de Knative |
| **Flux** | Graduated | ArgoCD já é o GitOps engine. Migrar não justifica |
| **Fluentd** | Graduated | Promtail + Loki já resolvem. Fluentd é mais pesado |
| **Jaeger** | Graduated | Grafana Tempo já cobre tracing com custo operacional menor |
| **OPA/Gatekeeper** | Graduated | Kyverno é mais simples para o caso de uso (YAML policies vs Rego) |
| **Thanos** | Incubating | Prometheus single-instance é suficiente por agora. Thanos quando precisar de HA/long-term |
| **Cortex** | Incubating | Mesmo caso do Thanos — overengineering para o volume atual |
| **Strimzi** (Kafka) | Incubating | Kafka é overkill. pg NOTIFY ou NATS resolve o volume de eventos |
| **KubeVirt** | Incubating | VMs não são relevantes para PaaS container-native |
| **Backstage** | Incubating | Portal — complementar mas não core. Após launch |

---

## Roadmap de Adoção Sugerido

### Fase 1 — Security Baseline (Sprint 6) 🔴

```
1. Kyverno         → admission policies no workload cluster
2. cosign + syft   → image signing no build pipeline
3. in-toto         → build attestation
```

**Resultado:** Nenhuma imagem não assinada roda. Pods forçados a non-root + resource limits.

### Fase 2 — Resiliência (Sprints 7-8) 🟠

```
4. Falco           → runtime security monitoring
5. Chaos Mesh      → fault injection testing
6. Velero          → backup + disaster recovery
```

**Resultado:** Visibilidade de runtime, resiliência validada, DR testado.

### Fase 3 — DX & Otimização (Sprint 9+) 🟡

```
7. KEDA            → scale-to-zero para apps de tenant
8. CloudEvents     → formato padrão de eventos
9. Argo Rollouts   → progressive delivery (canary/blue-green)
10. Buildpacks     → zero-Dockerfile builds
11. OpenCost       → custo por tenant
```

**Resultado:** DX nível Vercel completo. Scale-to-zero, canary deploys, cost visibility.

### Fase 4 — Scale (Futuro) 🟢

```
12. Cilium (avançado) → L7 policies, network observability
13. Harbor            → self-hosted registry com scan
14. NATS              → messaging para multi-instance API
15. OpenFeature       → feature flags para rollout gradual
```

**Resultado:** Plataforma pronta para escalar além de early adopters.

---

## Referências

| Projeto | Documentação | Helm Chart |
|---------|-------------|------------|
| Kyverno | https://kyverno.io/docs | `kyverno/kyverno` |
| cosign | https://docs.sigstore.dev/cosign | CLI tool (no Helm) |
| Falco | https://falco.org/docs | `falcosecurity/falco` |
| CloudNativePG | https://cloudnative-pg.io/docs | `cloudnative-pg/cloudnative-pg` |
| Chaos Mesh | https://chaos-mesh.org/docs | `chaos-mesh/chaos-mesh` |
| KEDA | https://keda.sh/docs | `kedacore/keda` |
| CloudEvents | https://cloudevents.io | SDK only |
| OpenCost | https://www.opencost.io/docs | `opencost/opencost` |
| Harbor | https://goharbor.io/docs | `harbor/harbor` |
| Velero | https://velero.io/docs | `vmware-tanzu/velero` |
| Cilium | https://docs.cilium.io | `cilium/cilium` |
| Argo Rollouts | https://argoproj.github.io/rollouts | `argo/argo-rollouts` |
| NATS | https://docs.nats.io | `nats-io/nats-server` |
| Buildpacks | https://buildpacks.io/docs | CLI tool (no Helm) |
| OpenFeature | https://openfeature.dev/docs | `flagd` sidecar |
