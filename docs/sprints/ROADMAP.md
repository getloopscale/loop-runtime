# Roadmap — LoopZero PaaS

> Ultima atualizacao: 2026-03-27

## Visao

LoopZero sera um PaaS CLI-first para Kubernetes com qualidade DX equivalente ao Vercel para backends. O desenvolvedor executa `loopzero login`, `loopzero init`, `loopzero deploy` e em menos de 5 minutos cada servico esta live com TLS, preview environments em cada PR, e rollback com um comando. Sem Kubernetes. Sem YAML de infraestrutura. Monorepos nativos.

---

## Status Atual

| Area | Status | Observacao |
|------|--------|------------|
| `loopzero login` → `loopzero init` → `loopzero deploy` → URL live | Funcional em DO | 193s, exit code 0 |
| Monorepo (API + Web static + Worker) | Funcional | CF Worker para static |
| Infra destroy + recreate via GitHub Actions | Testado | Pipeline completa |
| Observabilidade (Loki, Prometheus, Grafana) | Funcional | Dashboards operacionais |
| `loopzero logs`, `loopzero rollback`, `loopzero secret` | Funcional em DO | Validados end-to-end |
| Seguranca de rede (Cloudflare Tunnel, WAF, headers) | Funcional | DDoS + security headers |
| Operator controllers (7/7 wired em main.go) | Code-complete | Falta validacao DO de 5 controllers |
| `loopzero status` | Handler + CLI prontos | Rota nao registrada em `server.go` |
| Workload security (PodSecurityAdmission, NetworkPolicy) | Ausente | Pods rodam como root |
| Preview environments (`loopzero preview`) | Nao iniciado | State machine + test skeleton existem |
| `loopzero promote`, `loopzero cancel` | Nao iniciado | State machines definidas em `pkg/statemachine/` |
| Image signing (cosign + syft) | Nao iniciado | Supply chain verification |
| Chaos testing (50 deploys concorrentes) | Nao iniciado | Skeleton em `infra/tests/chaos/` |

---

## Fases

### Fase 1 — Platform Integrity (atual)

**Objetivo:** Garantir que a plataforma funcione corretamente para multiplos tenants com isolamento de seguranca adequado.

**Estimativa:** 2-3 semanas

**O que desbloqueia:** Multi-tenancy segura. Sem isso, cada pod comprometido tem acesso total ao cluster, e controllers nao validados podem falhar silenciosamente em producao.

**Escopo:**
- Wiring de `loopzero status` (30min — 1 linha em `server.go`)
- Validacao dos 5 controllers ja wired no DigitalOcean (Environment, Application, Exposure, GitRepositoryBinding, PlatformBootstrap)
- Workload security: PodSecurityAdmission `restricted`, NetworkPolicy enforcement, non-root containers, resource limits obrigatorios

### Fase 2 — Developer Experience

**Objetivo:** Entregar as features que tornam LoopZero comparavel ao Vercel em DX.

**Estimativa:** 5-7 semanas

**O que desbloqueia:** Preview environments por PR (a feature mais pedida), promotion staging→production com safety checks, cancelamento de builds em progresso, e suporte a Dockerfiles customizados.

**Escopo:**
- Preview environments (`loopzero preview`) — PR → URL efemera → auto-cleanup no merge
- `loopzero promote` — staging → production com validacao de health
- `loopzero cancel` — cancelar builds em andamento
- Custom Dockerfiles — `build: dockerfile` no `loopzero.yaml`

### Fase 3 — Production Hardening

**Objetivo:** Garantir que o sistema sobrevive a falhas reais e escala para uso em producao.

**Estimativa:** 3-5 semanas

**O que desbloqueia:** Confianca para rodar cargas de producao. Supply chain segura (imagens assinadas), resiliencia comprovada via chaos testing.

**Escopo:**
- Image signing com cosign + syft (SBOM)
- Chaos testing: 50 deploys concorrentes, pod kill, network partition, DB failover durante deploy
- Hardening de edge cases encontrados no chaos

### Fase 4 — Launch

**Objetivo:** Um desenvolvedor externo ao time consegue fazer onboarding em menos de 10 minutos.

**Estimativa:** 2-3 semanas

**O que desbloqueia:** Lancamento publico. CLI polido, docs completos, error messages com next-action, zero edges asperos.

**Escopo:**
- Polish de CLI (mensagens, spinners, sugestoes de acao)
- Documentacao de onboarding
- `loopzero doctor` para diagnostico de ambiente
- Validacao com usuario externo

---

## Sequencia Visual

```
                    Fase 1                    Fase 2                     Fase 3           Fase 4
              Platform Integrity         Developer Experience       Production Hard.      Launch
             ┌─────────────────┐        ┌─────────────────────┐    ┌──────────────┐   ┌─────────┐
             │ H6.1 status     │───┐    │ Sprint 5: Preview   │    │ Sprint 6:    │   │Sprint 9:│
             │ (30min)         │   │    │ Environments        │    │ Supply Chain │   │ Polish  │
             └─────────────────┘   │    │ (20-30h)            │    │ (15-20h)     │   │ (10-15h)│
             ┌─────────────────┐   ├──▶ └─────────────────────┘──▶ └──────────────┘──▶└─────────┘
             │ H7: Workload    │   │    ┌─────────────────────┐    ┌──────────────┐
             │ Security (6-8h) │───┤    │ Sprint 4: Promote   │    │ Sprint 8:    │
             └─────────────────┘   │    │ + Cancel (15-20h)   │    │ Chaos (10-15h│
             ┌─────────────────┐   │    └─────────────────────┘    └──────────────┘
             │ Sprint 11: Wire │───┘
             │ Controllers on  │
             │ DO (8-12h)      │
             └─────────────────┘

Dependencias:
  H6.1 ──────────────────────────▶ Nenhuma (independente)
  H7 ───────────────────────────▶ Nenhuma (independente)
  Sprint 11 ────────────────────▶ Nenhuma (independente, mas valida Fase 1)
  Sprint 5 (Preview) ──────────▶ Sprint 11 (controllers validados)
  Sprint 4 (Promote/Cancel) ───▶ Sprint 11 (controllers validados)
  Sprint 6 (Supply Chain) ─────▶ Sprint 5 (preview cleanup precisa de image policy)
  Sprint 8 (Chaos) ────────────▶ Sprint 6 (chaos testa supply chain tambem)
  Sprint 9 (Launch) ───────────▶ Tudo anterior
```

---

## Marcos

| Marco | Data Alvo | Criterio de Sucesso |
|-------|-----------|---------------------|
| **M1: Platform Secure** | 2026-04-10 | `loopzero status` funcional, pods non-root com PSA restricted, 5 controllers validados em DO |
| **M2: Preview MVP** | 2026-04-28 | PR aberta → `loopzero preview` → URL efemera funcional → merge limpa recursos |
| **M3: Full CLI** | 2026-05-12 | `loopzero promote` + `loopzero cancel` funcionais em DO |
| **M4: Supply Chain** | 2026-05-26 | Imagens assinadas com cosign, SBOM via syft, policy enforcement |
| **M5: Battle-Tested** | 2026-06-09 | 50 deploys concorrentes passam, chaos tests passam, zero data loss |
| **M6: Launch Ready** | 2026-06-23 | Dev externo faz onboarding em < 10min sem ajuda |
