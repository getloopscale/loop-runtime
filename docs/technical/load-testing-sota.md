# Load Testing — State of the Art (2025-2026)

> Pesquisa de referencia para o LoopZero Platform.
> Data: 2026-05-04 | Autor: Principal Engineer (research agent)

---

## Sumario Executivo

O cenario de load testing evoluiu significativamente em 2024-2025. As principais tendencias sao:

1. **k6 consolidou-se como lider** — 29.9k GitHub stars, k6 Operator 1.0 GA (setembro 2025), integracao nativa com Grafana/Prometheus, e adocao massiva em equipes cloud-native.
2. **Shift-left e obrigatorio** — Load tests devem rodar em CI como gates automaticos, nao como atividade pre-release manual.
3. **Coordinated omission e um problema resolvido** — Ferramentas modernas (k6, wrk2, Vegeta, Artillery) oferecem open workload models que evitam o problema classico.
4. **Kubernetes-native e o padrao** — k6 Operator, Testkube e Gatling Enterprise oferecem execucao distribuida via CRDs.
5. **PaaS-specific testing exige harnesses customizados** — Nenhuma ferramenta off-the-shelf testa pipelines de deploy (build → sign → push → sync). Go harnesses customizados sao a norma na industria.

**Para o LoopZero:** A stack atual (k6 + Go harness) esta alinhada com SOTA. As lacunas sao: (a) soak testing nao esta ativo em cron, (b) k6 Operator nao esta instalado em producao, (c) falta integracao SSE/WebSocket nos scripts k6.

---

## Matriz de Comparacao de Ferramentas

| Criterio | k6 | Gatling | Locust | Artillery | Vegeta | wrk2 | Plow | hey |
|----------|:---:|:-------:|:------:|:---------:|:------:|:----:|:----:|:---:|
| **Linguagem de script** | JavaScript/TS | Scala/Java | Python | YAML+JS | CLI/Go lib | CLI (C) | CLI (Go) | CLI (Go) |
| **HTTP/1.1 + HTTP/2** | Sim | Sim | Sim | Sim | Sim | Sim | Sim | Sim |
| **gRPC** | Sim (built-in) | Sim (plugin) | Nao | Plugin | Nao | Nao | Nao | Nao |
| **WebSocket** | Sim (built-in) | Sim | Nao | Sim (built-in) | Nao | Nao | Nao | Nao |
| **SSE** | Sim (xk6-sse) | Nao | Nao | Nao | Nao | Nao | Nao | Nao |
| **Browser automation** | Sim (xk6-browser/Chromium) | Nao | Nao | Playwright plugin | Nao | Nao | Nao | Nao |
| **Execucao distribuida** | k6 Operator (K8s CRD) | Enterprise (Helm) | Built-in (master/worker) | Artillery Cloud (AWS) | Manual (pipe) | Manual | Nao | Nao |
| **Kubernetes-native** | Sim (Operator 1.0 GA) | Sim (Enterprise) | Nao | Nao | Nao | Nao | Nao | Nao |
| **Coordinated omission** | Sim (arrival-rate executor) | Sim (open model) | Parcial (open model disponivel) | Sim (arrival-rate phases) | Sim (constant-rate) | Sim (--rate flag) | Nao | Nao |
| **Prometheus export** | Built-in (remote write) | Enterprise | Plugin | Plugin | JSON → converter | Nao | Nao | Nao |
| **CI/CD integration** | Excelente (exit code + thresholds) | Boa | Boa | Boa (CLI) | Boa (exit code) | Basica | Basica | Basica |
| **Real-time metrics** | Sim (Grafana Cloud/Prometheus) | Sim (Enterprise) | Sim (Web UI) | Sim (Cloud) | JSON stream | Nao | Sim (Web UI) | Nao |
| **Cloud/SaaS** | Grafana Cloud k6 | Gatling Enterprise | Locust Cloud | Artillery Cloud | Nao | Nao | Nao | Nao |
| **Licenca** | AGPL-3.0 | Apache 2.0 | MIT | MPL-2.0 | MIT | Apache 2.0 | MIT | MIT |
| **GitHub Stars** | 29.9k | 6.6k | 27.5k | 8.5k | 23.7k | 4.2k | 4.1k | 18k |
| **Ultima release (2025+)** | Ativa (mensal) | Ativa | Ativa | Ativa | Ativa | Inativa (2022) | Moderada | Moderada |

### Legenda de uso

| Ferramenta | Melhor para |
|------------|-------------|
| **k6** | Cenarios complexos, CI gates, observabilidade, equipes JS/Go, Kubernetes |
| **Gatling** | Equipes JVM, reports HTML detalhados, throughput extremo |
| **Locust** | Equipes Python, prototipagem rapida, controle via Web UI |
| **Artillery** | Tests simples YAML-first, equipes Node.js |
| **Vegeta** | Constant-rate precision, benchmarks pontuais, Go libraries |
| **wrk2** | Single-endpoint saturation com latency correction |
| **Plow** | Benchmarks rapidos com visualizacao real-time |
| **hey** | Substituto moderno do ApacheBench, one-liners rapidos |

---

## Best Practices por Categoria

### 1. Coordinated Omission (CO)

**O problema:** Quando o load generator espera a resposta antes de enviar o proximo request, requests lentos "escondem" centenas de requests que deveriam ter sido enviados naquele intervalo. O resultado: latencias artificialmente baixas.

**Solucao SOTA:**
- Usar **open workload model** (arrival-rate scheduling) — k6 `constant-arrival-rate` ou `ramping-arrival-rate` executors
- Vegeta e wrk2 sao inerentemente livres de CO (constant-rate por design)
- HdrHistogram com correcao automatica para sistemas que ainda usam closed model
- **Regra:** Se voce mede p99 com closed model, voce esta medindo service time, nao response time

**Referencia:** Gil Tene, "How NOT to Measure Latency" — open workload e a unica forma correta de medir latencia sob carga.

### 2. Load Test as Code

**Principios:**
- Scripts versionados no mesmo repo que o codigo de producao
- Thresholds derivados de SLOs documentados (ex: `cell-budget.md`)
- Execucao automatica em CI — nao e "atividade de QA" separada
- Reproducibilidade: mesmos scripts, mesmos resultados (determinismo)
- Infrastructure as Code para o ambiente de teste (k6 Operator CRD, Job templates)

**Pattern:** k6 scripts como `*.js` no repo + thresholds como `export const options` + CI workflow que faz `k6 run --out prometheus` + threshold failure = pipeline failure.

### 3. Execucao Distribuida

**Quando necessaria:**
- SUT deve ser acessado de multiplos IPs (teste de load balancing)
- Single node nao gera carga suficiente (>300k RPS para k6)
- Teste deve simular multi-regiao

**Abordagens SOTA:**
- **k6 Operator** — CRD `TestRun` com `parallelism: N`, automatic execution segment split, pods synchronized via initializer/starter jobs
- **Testkube** — Wrapper sobre k6 Operator com dashboard de resultados, aggregation, e integracao CI
- **Gatling Enterprise** — Helm-based injectors com auto-scale
- **Locust** — master/worker pattern nativo (mas sem K8s integration built-in)

**Pattern LoopZero:** Para L4+ (500+ tenants), usar k6 Operator com `parallelism: 4-8` no build cluster (nao no workload cluster — evita autocompetir por recursos).

### 4. SLO-Driven Thresholds

**Principio:** Thresholds nao sao numeros arbitrarios — derivam diretamente dos SLOs publicados.

```javascript
// Exemplo: derivado de cell-budget.md
export const options = {
  thresholds: {
    http_req_failed:       ['rate<0.01'],      // SLO: 99% availability
    http_req_duration:     ['p(99)<600000'],   // SLO: deploy <10min budget
    'deploy_e2e_duration': ['p(95)<300000'],   // SLO: 95% deploys <5min
    'build_success_rate':  ['rate>0.99'],      // SLO: 99% build success
  },
};
```

**Best practice:** Thresholds devem ter 3 niveis:
1. **Warning** (80% do budget) — alert, nao falha
2. **Error** (100% do budget) — falha CI
3. **Critical** (150% do budget) — falha + notificacao imediata

### 5. Soak Testing

**Objetivo:** Detectar degradacao temporal — memory leaks, file descriptor exhaustion, connection pool starvation, GC pressure.

**Parametros SOTA:**
- Duracao: 4-12 horas (minimo 4h para detectar leaks sutis)
- Carga: 60-80% da capacidade nominal (nao e stress test)
- Monitoramento: memory RSS, goroutine count, FD count, GC pause, connection pool size
- Frequencia: semanal via CronJob ou post-release

**Metricas de deteccao:**
- Memory slope: `deriv(process_resident_memory_bytes[1h]) > 0` por >2h = leak
- FD slope: `deriv(process_open_fds[1h]) > 0` por >2h = exhaustion
- Goroutine accumulation: `go_goroutines` crescente sem plateau

**LoopZero gap:** Script `soak-l2-6h.js` existe no README mas nao esta implementado. CronJob `soak-l2-cronjob.yaml` referenciado mas nao encontrado.

### 6. Chaos + Load (Combined Testing)

**Principio:** Chaos testing sem carga e inutil — voce precisa saber como o sistema se comporta sob falha E carga simultaneamente.

**Patterns:**
- Injectar network delay (50-200ms) durante L2 load test
- Kill pod aleatoriamente durante soak test (pod disruption)
- Simular cell failover durante L3 (cell-2 drain enquanto L3 roda)
- Throttle build cluster disk I/O durante stress test

**Ferramentas:** Chaos Mesh (ja instalado no LoopZero) + k6 scripts rodando em paralelo.

### 7. PaaS-Specific Load Patterns

**Diferencial critico:** PaaS load testing NAO e so HTTP benchmarking. O workload real e:

```
User Action          → What Gets Tested
─────────────────────────────────────────
loopzero deploy          → Upload bundle → Lock → Argo Workflow → BuildKit →
                       cosign sign → push image → Gitea push → ArgoCD sync →
                       Pod running → SSE event
loopzero rollback        → Lock → state machine transition → Gitea revert →
                       ArgoCD sync → Pod running
loopzero logs            → SSE stream → cross-cluster query → real-time output
Concurrent deploys   → Lock contention → 1x202 + 1x409 behavior
```

**O Go harness (`real_deploy_harness.go`) e a ferramenta correta para isso.** k6 testa os endpoints HTTP; o Go harness testa o pipeline end-to-end.

**Pattern industria:** Vercel, Fly.io e Railway usam harnesses customizados internos (nao publicos) para testar seus pipelines de deploy. A abordagem do LoopZero (Go harness + k6) esta alinhada com o SOTA.

### 8. Kubernetes-Native Load Testing

**Opcoes:**

| Abordagem | Pros | Cons |
|-----------|------|------|
| **k6 Operator** | CRD nativo, sync entre pods, Grafana ecosystem | Requer K8s expertise |
| **Testkube** | Dashboard, result aggregation, multi-framework | Overhead (controller + API + dashboard) |
| **k6 como Job** | Simples, sem CRD extra | Sem distributed sync, sem parallelism auto |
| **CI runner externo** | Isolamento total do cluster | Latencia de rede, nao testa intra-cluster |

**Recomendacao LoopZero:** k6 Operator para L3+ (distribuido), k6 como Job para L1-L2 (single pod). Testkube e overengineering para o estagio atual.

### 9. Observabilidade de Resultados

**Pipeline de metricas:**
```
k6 script → Prometheus Remote Write → Prometheus → Grafana Dashboard
                                                         ↓
                                                   Alert Rules (SLO violation)
```

**Dashboards minimos:**
- **Real-time:** RPS, latency p50/p95/p99, error rate, VU count
- **Historical:** Trend de latencia por release, regressao detection
- **Soak:** Memory/FD/goroutine slopes com alertas

**k6 built-in (desde v0.42.0):** `--out experimental-prometheus-rw` — sem necessidade de build customizado com xk6.

### 10. Shift-Left Performance Testing

**Principio:** Load tests rodam no PR, nao so pre-release.

**Niveis de shift-left:**

| Quando | O que roda | Gate |
|--------|-----------|------|
| PR aberto | L1 smoke (10 VUs, 2 min) | Fail = block merge |
| Merge em develop | L2 sustained (50 VUs, 10 min) | Fail = revert |
| Release candidate | L3 full (100 VUs, 60 min) | Fail = block release |
| Weekly cron | Soak L2 (6h) | Fail = P1 ticket |
| Pre-prod (manual) | L4/L5 stress | Fail = capacity planning |

**LoopZero gap:** CI workflow `load-test.yml` existe mas nao roda em PRs. Apenas L1 smoke deveria rodar em PR (fast gate).

---

## Benchmark: Como PaaS Competitors Testam

### Vercel

- Load testing permitido **apenas para Enterprise plans** (requer coordenacao com engineering team)
- DDoS mitigation e WAF bloqueiam testes nao-autorizados (bypass rules necessarias)
- Recomendam k6 ou Artillery como ferramentas
- Concurrency limits: 30k (Pro), 100k (Enterprise)
- CI interno: GitHub Actions + Turborepo affected detection + chunk-tests.js para paralelizar
- Penetration testing regular via third-party assessors
- **Insight:** Vercel testa infraestrutura centralmente; clientes testam suas apps. Nao ha tooling publico para pipeline stress.

### Fly.io

- Firecracker microVMs — boot em centenas de ms, autoscale via Fly Proxy
- Prometheus/VictoriaMetrics builtin para metricas de app
- Comunidade reporta problemas em >1500 RPS em micro-1x (connection issues)
- **Insight real:** Sliplane (managed Docker hosting) usou Fly.io para builds e 10% falhavam por razoes nao-relacionadas ao codigo do cliente. Fly e otimizado para small stateless web apps, nao heavy build workloads.
- **Licao para LoopZero:** Build plane deve ser over-provisioned e isolado; nunca compartilhar recursos de build com workload.

### Railway

- Horizontal scaling com poucos data centers (4 regioes)
- Modelo usage-based como throttle natural
- Comunidade usa k6/Artillery para stress tests manuais
- Observability dashboard built-in por projeto
- **Insight:** Railway nao publica como testa internamente. Modelo simples (containers always-on) simplifica o problema.

### Cloudflare

- Escala massiva: "tens of millions of tests per day" internamente
- Problema real: **self-DDoS** — edge test agents ("flamingos") sobrecarregando control plane
- Solucao: desacoplar agents de control plane, leverage da propria rede como superpoder
- Worker performance examples no GitHub para benchmarking de providers
- Vitest integration para unit tests de Workers
- **Licao para LoopZero:** Em multi-cell, evitar que load tests de um cell saturem o control plane (mgmt cluster). Isolar o load generator.

---

## Recomendacao para o LoopZero

### Manter (ja alinhado com SOTA)

| Item | Status | Justificativa |
|------|--------|---------------|
| k6 como ferramenta HTTP SLO | Excelente | Lider de mercado, Grafana-native, K8s-ready |
| Go harness para pipeline E2E | Excelente | Unica forma correta de testar deploy pipelines |
| Thresholds derivados de SLOs | Bom | `cell-budget.md` como source of truth |
| Niveis L1-L5 | Bom | Progressao clara de smoke → stress |
| Prometheus remote write | Bom | Pipeline de observabilidade completa |
| Job template para in-cluster | Bom | Reproducivel e isolado |

### Adicionar (gaps identificados)

| Item | Prioridade | Esforco | Impacto |
|------|:----------:|:-------:|:-------:|
| **k6 Operator instalado no build cluster** | Alta | 2h | Distributed L4/L5 |
| **Soak test CronJob implementado** | Alta | 4h | Detecta memory leaks |
| **L1 smoke em CI de PR** | Alta | 2h | Shift-left gate |
| **SSE/WebSocket script k6** | Media | 4h | Cobertura de `loopzero logs` |
| **Chaos + Load combinado** | Media | 8h | Resiliencia validada |
| **Trend dashboard (regressao por release)** | Media | 4h | Historico de performance |
| **k6 arrival-rate para todos scripts** | Baixa | 2h | CO-safe em todos niveis |
| **Testkube evaluation** | Baixa | — | Desnecessario no estagio atual |

### Alterar (melhorias em existentes)

| Item | Mudanca |
|------|---------|
| `l1-10-tenants.js` | Migrar de `ramping-vus` para `constant-arrival-rate` (CO-safe) |
| `stress-break-point.js` | Adicionar metricas custom: goroutine count, PG connections, ArgoCD queue depth |
| `load-test.yml` workflow | Adicionar trigger em PRs (apenas L1, timeout 5min) |
| Go harness | Adicionar SSE listener para validar eventos de deploy em tempo real |
| Soak test | Implementar `soak-l2-6h.js` + alertas de slope (memory, FDs) |

### NAO Fazer (YAGNI)

| Item | Motivo |
|------|--------|
| Migrar para Gatling/Locust | k6 resolve tudo que precisamos, equipe usa JS/Go |
| Testkube full install | Overhead de operator+API+dashboard sem retorno no estagio atual |
| Grafana Cloud k6 | Self-hosted Prometheus ja funciona; cloud = custo sem beneficio |
| Multi-region load generation | Single-region (SFO3) e suficiente para validar; multi-region e YAGNI |
| Custom k6 binary (xk6) | Prometheus remote write ja e built-in desde v0.42.0 |

---

## Anti-Patterns a Evitar

| Anti-Pattern | Problema | Alternativa |
|--------------|----------|-------------|
| Load test so pre-release | Regressoes escapam por semanas | Shift-left: L1 em PR, L2 em merge |
| Closed model para medir latencia | Coordinated omission esconde p99 real | Usar `constant-arrival-rate` |
| Thresholds sem SLO | Numeros arbitrarios nao detectam regressao | Derivar de `cell-budget.md` |
| Soak test manual | Nunca e executado na pratica | CronJob semanal automatico |
| Load test no mesmo cluster que SUT | Auto-competicao por recursos | Load generator no build cluster |
| Ignorar metricas de infra durante load | Nao sabe se bottleneck e app ou infra | Correlacionar k6 + node_exporter + PG |
| `git log` como changelog de resultados | Ilegivel, nao-acionavel | Grafana dashboard com annotations |
| Testar so happy path sob carga | Erros sob carga sao diferentes de erros sem carga | Incluir cenarios de erro (401, 409, 429) |

---

## Fontes

### Ferramentas e Documentacao

- [k6 Official](https://k6.io/) — Load testing for engineering teams
- [k6 Operator GitHub](https://github.com/grafana/k6-operator) — Kubernetes operator for distributed k6 tests
- [k6 Operator 1.0 Announcement](https://grafana.com/blog/distributed-performance-testing-for-kubernetes-environments-grafana-k6-operator-1-0-is-here/) — Grafana Labs, Sep 2025
- [k6 Distributed Testing Guide](https://grafana.com/docs/k6/latest/testing-guides/running-distributed-tests/) — Official docs
- [Testkube k6 Integration](https://testkube.io/blog/load-testing-in-kubernetes-with-k6-and-testkube) — Comparison with k6 Operator
- [Testkube vs k6 Operator](https://testkube.io/blog/comparing-the-k6-operator-vs-testkube-for-load-testing)
- [Vegeta GitHub](https://github.com/tsenart/vegeta) — HTTP load testing tool and library
- [xk6-output-prometheus-remote](https://github.com/grafana/xk6-output-prometheus-remote) — (merged into k6 core v0.42.0+)

### Coordinated Omission

- [ScyllaDB: On Coordinated Omission](https://www.scylladb.com/2021/04/22/on-coordinated-omission/) — Explicacao detalhada com exemplos
- [Red Hat: Coordinated Omission](https://redhatperf.github.io/post/coordinated-omission/) — Solucoes praticas
- [Artillery: Open vs Closed Workload Models](https://www.artillery.io/blog/load-testing-workload-models)
- [Locust Cloud: Closed vs Open Workload Models](https://www.locust.cloud/blog/closed-vs-open-workload-models/)
- [k6 Community: Is k6 safe from CO?](https://community.k6.io/t/is-k6-safe-from-the-coordinated-omission-problem/1484)

### Comparacoes e Guias

- [Best Load Testing Tools 2026 — Vervali](https://www.vervali.com/blog/best-load-testing-tools-in-2026-definitive-guide-to-jmeter-gatling-k6-loadrunner-locust-blazemeter-neoload-artillery-and-more/)
- [Load Testing PoC: k6 vs Artillery vs Locust vs Gatling](https://medium.com/@dorangao/load-testing-poc-k6-vs-artillery-vs-locust-vs-gatling-node-js-express-target-f056094ffbef)
- [Best Load Testing Tools 2025 — DEV Community](https://dev.to/_d7eb1c1703182e3ce1782/best-load-testing-tools-for-developers-in-2025-k6-jmeter-locust-and-more-4513)
- [Go Performance Benchmarking Guide](https://goperf.dev/02-networking/bench-and-load/)
- [Soak Testing Complete Guide 2025 — Devzery](https://www.devzery.com/post/soak-testing-complete-guide-2025)

### PaaS Competitors

- [Vercel: How to Load Test](https://vercel.com/kb/guide/how-to-effectively-load-test-your-vercel-application)
- [Vercel: Load Testing Policy](https://vercel.com/kb/guide/what-s-vercel-s-policy-regarding-load-testing-deployments)
- [Vercel: CI/CD Infrastructure — DeepWiki](https://deepwiki.com/vercel/vercel/7.1-cicd-and-testing)
- [Cloudflare: Testing in Production](https://testkube.io/resources/how-cloudflare-tests-in-production-for-unbreakable-performance)
- [Cloudflare: Worker Performance Examples](https://github.com/cloudflare/worker-performance-examples)
- [Fly.io Community: Load Testing Results](https://community.fly.io/t/some-load-testing-results-and-a-few-questions/249)
- [Fly.io vs Render Comparison](https://northflank.com/blog/flyio-vs-render)

### Shift-Left e CI/CD

- [Shift-Left Performance Testing with k6](https://medium.com/@sumit.somanchd/shift-left-performance-testing-integrating-k6-in-ci-cd-pipelines-e56355abe861)
- [k6 Scaling on Kubernetes — NashTech](https://blog.nashtechglobal.com/scaling-performance-testing-with-kubernetes-and-k6-leveraging-containerization-for-distributed-load-generation/)
- [k6 on k3d/kind — Toucan Toco](https://www.toucantoco.com/en/tech-blog/tech-blog/load-testing-with-k6-and-k8s)
- [OneUptime: k6 Operator Configuration](https://oneuptime.com/blog/post/2026-02-09-k6-operator-distributed-load-testing/view)
