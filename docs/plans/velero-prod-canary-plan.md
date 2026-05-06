# Plan: Velero Production Canary & Validation

> **Version 1.1** (edge case review incorporated, 2026-05-06) — Fechar os 7 gaps acionáveis identificados após o `velero-backup-dr-plan.md` v1.1 CONVERGED (2026-05-06). O plano-mãe entregou os artefatos (Helm chart, schedules, runbook, drill, CI workflow) e validou-os em k3d standalone via MinIO. Este plano move a validação para o ambiente que importa: DO Spaces, helmfile sync completo, cron observado, restore REAL de tenant com finalizers, CI workflow ativo, alerting end-to-end. Resultado esperado: maturity gain real medido (+0.1-0.2), elevando o estado de "implementado" para "production-ready".
>
> **v1.1 changes:** Incorporados 3 MUST FIX items do edge-case review (EC-1: idempotent revert em T0.1; EC-3: defensive YAML snapshot em T2.3 + T3.2; EC-4: promtool test rules substitui metric mock inviável em T4.2). 6 SHOULD TEST items adicionados aos TDD blocks (EC-2, EC-5, EC-6, EC-7, EC-8, EC-9). 2 DOCUMENT items adicionados como notas (EC-10 stack pre-selection; EC-11 signatures async).

## Context

### O que existe hoje (post-velero-backup-dr v1.1)

- **Helm wrapper chart** `infra/helm/velero/` com 5 schedules + plugins AWS + PSA restricted. Validado por `helm template` + `kubeconform --strict`.
- **Pulumi L1** define o bucket `loopzero-{project}-{region}-velero-backups` (versioned, protect=isProd).
- **Helmfile L2** integra Velero no Wave -1 com `needs:[cert-manager]` e MinIO (em standalone).
- **Runbook** `docs/runbooks/disaster-recovery-restore.md` (287 linhas, 10 steps, RTO < 30min).
- **DR drill script** `infra/scripts/velero-dr-drill.sh` — LIVE 9/9 phases pass em 4-5s contra k3d MinIO, idempotent.
- **CI workflow** `.github/workflows/velero-backup-check.yml` — YAML válido, cron diário 08:00 UTC.
- **PrometheusRule** `infra/helm/monitoring/manifests/prometheusrule-velero.yaml` — 4 alertas; ORPHAN `LoopZeroGiteaBackupStale` removido.

### O que NÃO foi testado / NÃO está 100%

| # | Gap | Severidade | Impacto |
|---|---|---|---|
| G1 | Schedules cron nunca dispararam | 🟡 médio | LASTBACKUP vazio em todas as 5 schedules. Typo numa expressão cron só aparece quando o horário chega. |
| G2 | DO Spaces backend nunca testado LIVE | 🟠 alto | Toda validação foi MinIO local. Provider=aws + s3ForcePathStyle deveria funcionar com DO Spaces, mas há quirks (publicUrl, x-amz-content-sha256) que o chart mitiga inconsistentemente entre versões. |
| G3 | helmfile sync LIVE com prod env | 🟠 alto | Apenas `helm install` direto foi testado. `helmfile sync -e prod` pode revelar dependências não-mapeadas (cert-manager wait, Wave -1 ordering interactions). |
| G4 | Restore de Tenant REAL (não-drill) | 🟠 alto | Drill usa `drill-test` Tenant sintético com spec mínimo. Nunca restaurei `e2e-tenant` ou `tn-e2etest` que têm finalizers ativos + namespaces filhos + ArgoCD Apps + ESO Secrets dependentes. |
| G5 | CI workflow nunca rodou | 🟡 médio | `velero-backup-check.yml` requer secret `MGMT_KUBECONFIG` no GitHub repo — não configurado. Workflow YAML válido mas nunca executado mesmo em dry-run. |
| G6 | Prometheus alerting end-to-end | 🟡 médio | PrometheusRule está em `manifests/`, scrape job em `values-prometheus.yaml`, mas nunca verifiquei que Prometheus carrega + scrape funciona + alert dispara. |
| G7 | `upgradeCRDs: false` workaround | 🟢 baixo | Bypass do chart's hook funciona porque o template já cria os CRDs. Se Velero upstream mudar isso em v1.15+, regressão silenciosa. |
| G8 | EC-6 (DO Spaces global API key) | 🟢 aceito | Mesma key que MinIO + Loki acessa backups. Risk-accept formal. |
| G9 | EC-7 (Secrets em plaintext em S3) | 🟢 aceito | DO Spaces encripta at-rest. Bucket private ACL. Risk-accept formal. |

### Evidência

- Plano-mãe `docs/plans/velero-backup-dr-plan.md` v1.1 §Final Status — explicitamente lista esses gaps como "deferred to canary" (commit `71f6b4b5`).
- `kubectl --context k3d-loopzero-mgmt -n velero-system get schedules` — 5 schedules `Enabled` mas `LASTBACKUP` vazio em todas (G1 evidência observada).
- `cli/src/config/constants.ts:53` API_REQUEST_TIMEOUT_MS — referência arquitetural sem efeito direto no plano.
- Maturity assessment `docs/maturity-assessment.md` (line referenced by CLAUDE.md:11) — mantém DR validation como gate para 4.2.

### Padrões existentes que este plano segue

- **Canary stages multi-fase** — mesmo padrão de `docs/runbooks/multi-cell-canary.md` (Stage 1 smoke → Stage 2 6h soak → Stage 3 ADR-compliant probes).
- **Prova de Vida + Prova de Morte** — invariantes do dogfood já usados em e2e-ginkgo-rewrite-plan v1.2.
- **Runbook drift validation** — comparar com `infra/scripts/velero-dr-drill.sh` que já existe; novo restore script será sibling.

## Objective

**Done =** os 7 gaps acionáveis fechados com evidência LIVE registrada, e os 2 gaps `aceito` formalizados em ADR de risk-accept assinado. Velero está empiricamente provado em DO Spaces, com cron observado, alerting funcional, CI workflow ativo, e restore real de tenant com finalizers + namespaces filhos validado.

Metas específicas e mensuráveis:

1. Os 5 schedules têm `LASTBACKUP` populado e em phase `Completed`
2. `velero backup-location get` retorna `Available` em DO Spaces stack (não só MinIO)
3. `helmfile sync -e standalone-multi` completa Wave -1 com Velero healthy (sem `helm install` manual)
4. Restore de tenant real com finalizer ativo + namespaces filhos + ArgoCD Apps + ESO Secrets concluído + verificado via HTTP probe
5. Workflow `velero-backup-check.yml` rodou ≥1 vez em GitHub Actions (success ou negative-test que abre issue)
6. Alert `LoopZeroVeleroBackupStale` dispara quando schedule pausado por >12h (test em ambiente isolado, depois revertido)
7. ADR `0024-velero-upgrade-crds-workaround.md` documenta o bypass + condição de revisão upstream
8. ADR `0025-velero-risk-accept.md` formaliza EC-6 e EC-7

## ADRs

### D1 — Canary stages, não Big-Bang

**Decision:** Validar em 4 stages sequenciais (cron-observe → helmfile sync local → DO Spaces dev → restore-real), não tudo em prod direto.

**Rationale:** O plano-mãe entregou v1.1 com 100% de implementação mas 0% de prod validation. Pular stages introduz risco de descobrir bugs no DO Spaces apenas em prod. O padrão multi-cell-canary já validou que stages-faseados são o caminho confiável (vs cell-2 ATIVADO direct, que teria retrabalho).

**Consequences:** Plano demora ~3-5 dias calendário (vs 1 dia se fizéssemos prod direto). Cada stage tem evidência LIVE separada — mais auditável. Permite parar/recuar por stage sem rollback grande.

### D2 — Restore real usa tenant existente, não cria novo

**Decision:** Para a Phase 3 (restore real), usar um tenant JÁ existente no cluster (`e2e-tenant` ou `tn-e2etest`) em vez de criar um tenant novo.

**Rationale:** Tenants reais já têm finalizers ativos + namespaces filhos + ArgoCD Apps em estado real. Criar tenant novo apenas para teste reproduz o problema do drill sintético — espec mínimo não exercita os edge cases (cleanup-finalizers, namespace teardown order, ArgoCD App deletion sync). Reusar tenant existente força confronto com a realidade.

**Consequences:** O tenant escolhido fica indisponível durante o ciclo backup → delete → restore (~5 min). Coordenar com qualquer carga ativa nele. Em prod isso seria inaceitável; em dev/k3d é aceitável.

### D3 — DO Spaces validation em dev stack, não prod

**Decision:** Phase 2 (DO Spaces backend) usa o stack `dev` (não `prod`) para primeira validação.

**Rationale:** Dev stack tem o mesmo provider DO Spaces + chart + IAM que prod, mas sem tenant traffic real. Quirks específicos do DO Spaces (`publicUrl`, `x-amz-content-sha256`, `s3ForcePathStyle`) aparecem em dev igual a prod. Prod fica para depois do canary do plano cell-2 (ADR-0021 D6 reference).

**Consequences:** Dev pode ter bucket separado de prod (já atende via Pulumi `protect: isProd`). Validation em dev não substitui prod canary — é pré-condição. Sprint 9 / cell-2 stage 4 fará prod final.

### D4 — Alert dispatch test usa schedule pause + manual time-skip

**Decision:** Para testar `LoopZeroVeleroBackupStale`, pausar um schedule e ajustar artificialmente o `velero_backup_last_successful_timestamp` (via metric mock ou waiting 12h+). NÃO simular falha real do MinIO/DO Spaces.

**Rationale:** Quebrar o backend (MinIO/DO Spaces) para forçar staleness afeta outros consumidores (Loki, builds, artifacts). Pausar schedule + esperar tempo real OU injetar metric mock isola o test ao alert dispatch path.

**Consequences:** Test não exercita o path "backup falhou por erro de S3". Para isso, Phase 4 (Prometheus alerting) inclui um test secundário com `velero backup create` apontando para BSL inválido — backup falha, `LoopZeroVeleroBackupFailing` deve disparar.

### D5 — CI workflow ativado via GitHub repo secret + 1 manual dispatch

**Decision:** Configurar `MGMT_KUBECONFIG` como GitHub repo secret + executar `workflow_dispatch` para verificar primeiro run end-to-end. NÃO esperar pelo cron diário.

**Rationale:** Cron 08:00 UTC daria 1 chance/dia de detectar bugs do workflow YAML. `workflow_dispatch` permite N tries imediatos. O secret `MGMT_KUBECONFIG` é o mesmo que outras workflows já usam (precedente).

**Consequences:** Requer permissão `secrets:write` no repo. Em forks o workflow não roda (já tem `if: github.repository == 'loopzero/loopzero'` no YAML).

### D6 — Workaround `upgradeCRDs: false` documented in ADR + tracking issue

**Decision:** Manter o workaround atual (`upgradeCRDs: false`) e documentar via ADR-0024 com data de revisão (3 meses). Abrir issue upstream em `vmware-tanzu/helm-charts` para tornar visível.

**Rationale:** Mudar agora introduziria risco (Bitnami pruned image continua quebrado). Mas a entrada `kubectl: image: rancher/kubectl:v1.32.0` no values.yaml não foi suficiente porque o hook ainda usa o `velero` image internally. ADR + tracking issue é o nível certo de documentação para "we know about it, we'll revisit".

**Consequences:** Velero CRDs em produção atualizam apenas via re-apply do template (helm upgrade), não via hook imperativo. Para v1.15+ Velero upstream pode mudar o pattern; ADR cita data de revisão para forçar check.

## Dependency Graph

```
Phase 0 (Cron observe — fast, low-risk) ──▶ Phase 1 (helmfile sync local) ──▶ Phase 2 (DO Spaces dev)
                                                       │
                                                       ▼
                                              Phase 3 (Restore real tenant)
                                                       │
                                                       ▼
                                              Phase 4 (Prometheus alerting + CI workflow)
                                                       │
                                                       ▼
                                              Phase 5 (Risk-accept ADRs + workaround tracking)
```

- Phase 0 é independente (apenas observação) — pode rodar em paralelo a outros workflows.
- Phase 1 valida helmfile sync, requer drift remediation se houver — pré-condição para Phase 2 e 3.
- Phase 2 (DO Spaces) e Phase 3 (restore real) são paralelizáveis após Phase 1 (acessam clusters diferentes).
- Phase 4 (alerting + CI) depende das anteriores estarem stable.
- Phase 5 (ADRs) finaliza o ciclo, fecha o ciclo de governança.

---

## Phase 0: Cron Observation

**Objective:** Provar que as 5 schedules cron disparam corretamente, populam `LASTBACKUP`, e produzem backups `Completed` no MinIO local. Validar antes de gastar tempo em DO Spaces.

### T0.1 — Force-trigger 1 schedule + observar 1 ciclo natural

#### Objective
Provar que pelo menos 1 schedule não tem typo cron + grava no bucket + cleanup da TTL funciona.

#### Evidence
- `kubectl --context k3d-loopzero-mgmt -n velero-system get schedules` retorna `LASTBACKUP=` vazio para todas as 5
- Cron `0 */6 * * *` significa próximo run às 18:00/00:00/06:00/12:00 UTC — pode ser 5h+ de espera
- ADR D4 do plano-mãe diz "TTL=720h auto-prune"; nunca observado executar

#### Files to edit
```
infra/helm/velero/values-schedules.yaml — modificação temporária para 1 schedule (revertida ao final)
```

#### Deep file dependency analysis
- **`values-schedules.yaml`** — alterar `loopzero-crds-backup.schedule: "0 */6 * * *"` para `"*/5 * * * *"` (every 5min) por 30 minutos, observar 5+ runs, depois reverter. Downstream: `helm upgrade velero` aplica a mudança. Sem efeito em produção (só local).

#### Deep Dives
- **Por que `loopzero-crds-backup`:** menor escopo (apenas CRDs loopzero.io/v1, ~10 resources), backup completa em <30s. `loopzero-full-backup` semanal seria muito demorado pra observação.
- **Reverso garantido:** após observação, `git checkout values-schedules.yaml` + `helm upgrade` retorna ao state v1.1.
- **Invariante:** observed `Completed` count >= 5 em 30 minutos.

#### Tasks
1. **(EC-1 fix)** Sentry pre-condition: detectar se script foi interrompido em run anterior. Verificar
   se `git diff --quiet infra/helm/velero/values-schedules.yaml` é falso E o conteúdo contém
   `*/5 * * * *` — se sim, executar revert primeiro (`git checkout infra/helm/velero/values-schedules.yaml`
   + `helm upgrade`) ANTES de iniciar nova observação. Garante reentrância sem state pollution.
2. **(EC-1 fix)** Trap EXIT no início do script: `trap 'cleanup_revert' EXIT INT TERM`. A função
   `cleanup_revert` faz `cp /tmp/velero-schedules-original.yaml ...` + `helm upgrade` se a flag
   `MODIFIED=1` foi setada. Captura Ctrl+C, OOM-kill, ou exit normal.
3. Backup de `values-schedules.yaml` para `/tmp/velero-schedules-original.yaml` (com timestamp para
   evitar overwrite acidental se o sentry da step 1 já restaurou)
4. Setar `MODIFIED=1` (consumido pelo trap)
5. Editar `loopzero-crds-backup.schedule` para `*/5 * * * *`
6. `helm upgrade velero infra/helm/velero --reuse-values --values infra/helm/velero/values-schedules.yaml`
7. Esperar 30 minutos, observar `velero backup get -l velero.io/schedule-name=loopzero-crds-backup`
8. Confirmar `Completed` count >= 5 + `velero backup describe <one>` mostra resources backed up
9. Reverter explicitamente (não confiar só no trap): `cp /tmp/velero-schedules-original.yaml...`
   + `helm upgrade` + setar `MODIFIED=0`
10. Verificar que apenas 1 schedule mudou + outras intactas

#### TDD
```
RED:     test_schedule_cron_observed() — após 30min com schedule */5, count(backups Completed) >= 5
RED:     test_revert_clean() — após reverter, schedule volta para "0 */6 * * *"
RED:     test_no_drift_other_schedules() — outras 4 schedules permanecem com cron original
RED:     test_idempotent_after_crash() — (EC-1) simular Ctrl+C entre steps 6 e 7 do run anterior;
         próxima invocação detecta values-schedules.yaml modificado E faz revert antes de re-iniciar
RED:     test_trap_revert_on_exit() — (EC-1) processo killed via SIGTERM durante observação restaura
         o values-schedules.yaml para o original via trap EXIT
GREEN:   Bash automation script `infra/scripts/velero-cron-observe.sh` (NEW) que faz steps 1-10 idempotente
REFACTOR: Nenhum esperado
VERIFY:  bash infra/scripts/velero-cron-observe.sh && echo "EXIT=$?"
```

#### Acceptance Criteria
- [ ] 5+ backups `Completed` observed em 30 minutos para `loopzero-crds-backup`
- [ ] `velero backup describe <name>` mostra `Phase: Completed` + items > 0
- [ ] Reverter restaura cron `0 */6 * * *` corretamente
- [ ] Outras 4 schedules não tocadas (cron unchanged)
- [ ] Script `infra/scripts/velero-cron-observe.sh` é idempotente (rerun 2x produz mesmo resultado)
- [ ] **(EC-1)** Sentry pre-condition detecta state pollution de run anterior interrompido
- [ ] **(EC-1)** Trap EXIT/INT/TERM restaura values-schedules.yaml mesmo em crash mid-observation

#### DoD (Definition of Done)
- [ ] Script criado e testado
- [ ] LIVE evidence: 5+ backups in 30min
- [ ] State revertido para v1.1
- [ ] `git status` clean (nenhuma mudança no values-schedules.yaml após revert)

### T0.2 — Verificar TTL auto-prune funciona

#### Objective
Confirmar que o `ttl: "720h"` do chart é honrado pelo Velero — backups antigos devem ser removidos automaticamente.

#### Evidence
- ADR D3 do plano-mãe define retenção `30 dias / 14 dias / 7 dias` por scope
- Velero documentation: `Schedule.spec.template.ttl` propaga para Backup, mas auto-prune requer `gc-frequency` no Velero server
- Nunca observado em ambiente LoopZero

#### Files to edit
```
infra/scripts/velero-cron-observe.sh — adicionar fase TTL test (artificial low TTL)
```

#### Deep Dives
- **Mecanismo:** Velero server roda GC controller que verifica `Backup.metadata.creationTimestamp + ttl < now()` e deleta. Frequência DEFAULT é **1h** (não 1min).
- **EC-2 fix — GC frequency override:** test default-config nunca veria a poda em 4min. Override explícito `velero.gcFrequency=1m` (ou helm `--set velero.configuration.garbageCollectionFrequency=1m`) em modo test, depois revert.
- **Test design:** criar 1 backup on-demand com `--ttl=2m`, esperar `gcFrequency` (1min override) × 5 = 5min margem, verificar que backup desapareceu (`velero backup get` não lista mais).

#### Tasks
1. **(EC-2 fix)** Override GC frequency: `helm upgrade velero ... --set velero.configuration.garbageCollectionFrequency=1m` antes do test
2. Adicionar Phase TTL no script: `velero backup create ttl-test-$(date +%s) --include-resources tenants.loopzero.io --ttl=2m`
3. Esperar 5 min (2min ttl + 3min margem para 3 ciclos de GC)
4. `velero backup get | grep ttl-test` — deve estar vazio (auto-pruned)
5. Reverter GC frequency para default (1h)
6. Reportar TTL auto-prune funcionou ou falhou

#### TDD
```
RED:     test_ttl_auto_prune() — backup com ttl=2m desaparece em <5min com gcFrequency=1m
RED:     test_gc_frequency_overridden() — (EC-2) helm get values mostra garbageCollectionFrequency=1m durante test
RED:     test_gc_frequency_reverted() — (EC-2) após test, gcFrequency volta a default (1h)
GREEN:   Adicionar phase ao script velero-cron-observe.sh
REFACTOR: Nenhum esperado
VERIFY:  bash infra/scripts/velero-cron-observe.sh --include-ttl-test
```

#### Acceptance Criteria
- [ ] Backup com `--ttl=2m` é deletado pelo Velero GC em <5min
- [ ] `velero backup get` sem o nome do backup
- [ ] Bucket também não tem mais o objeto (verify via `mc ls minio/velero-backups/backups/`)

#### DoD
- [ ] Script extensão funcional
- [ ] LIVE evidence: TTL auto-prune observed

---

## Phase 1: helmfile sync LIVE (standalone-multi)

**Objective:** Validar que `helmfile sync -e standalone-multi` instala Velero corretamente como parte do bootstrap completo, não apenas via `helm install` direto. Captura quirks de Wave ordering + dependências `needs:`.

### T1.1 — helmfile sync standalone-multi com Velero ativo

#### Objective
Provar que o helmfile sync flow completo respeita ordering (cert-manager → MinIO → velero) e que credentials geradas pelo `generate-secrets.sh` funcionam.

#### Evidence
- `infra/helmfile/mgmt.helmfile.yaml.gotmpl` foi editado em T1.2 do plano-mãe mas nunca executado
- Apenas `helm install velero` direto foi testado — não exercita o ordering do helmfile
- `helmfile sync -e standalone lint` passou (validate green) mas lint != execução

#### Files to edit
```
infra/scripts/velero-helmfile-validate.sh (NEW) — orquestrador do test
```

#### Deep file dependency analysis
- **`velero-helmfile-validate.sh`** (NEW) — script que: (a) `helmfile destroy -l name=velero -e standalone-multi`, (b) `helmfile sync -e standalone-multi -l name=velero`, (c) verify pod 1/1, BSL Available, 5 schedules. Downstream: nenhum (script standalone).

#### Deep Dives
- **`helmfile destroy -l name=velero`:** remove apenas o release velero, preservando outros (cert-manager, MinIO).
- **`helmfile sync -l name=velero`:** instala apenas velero, mas o `needs:` do helmfile valida que cert-manager + MinIO estão healthy ANTES de prosseguir.
- **Pre-condition:** `bash infra/helmfile/scripts/generate-secrets.sh standalone-multi mgmt` rodou e criou `velero-system/velero-s3-credentials`.

#### Tasks
1. Criar `infra/scripts/velero-helmfile-validate.sh`
2. **(EC-6 fix)** Phase 0 pré-condition: assert (a) cert-manager pod Ready
   (`kubectl -n cert-manager wait --for=condition=Ready pod -l app=cert-manager --timeout=10s`);
   (b) MinIO Ready (mesma forma); (c) bucket `velero-backups` existe
   (`kubectl -n loopzero-system exec deploy/minio -- mc ls local/velero-backups`); (d) Secret
   `velero-system/velero-s3-credentials` existe. Falhar fast com mensagem específica para cada
   pré-condição ausente — evita gastar minutos para descobrir falta de bucket.
3. Phase: `helmfile destroy -l name=velero -e standalone-multi` (idempotent, --no-hooks)
4. Phase: `bash infra/helmfile/scripts/generate-secrets.sh standalone-multi mgmt` (recria Secret se faltou)
5. Phase: `helmfile sync -l name=velero -e standalone-multi`
6. Phase: poll for `kubectl -n velero-system get bsl default -o jsonpath='{.status.phase}' == "Available"` (timeout 2min)
7. Phase: assert pod 1/1, 5 schedules `Enabled`
8. Phase: `velero backup create helmfile-validate-$(date +%s) --include-resources tenants.loopzero.io --wait` — confirma fluxo end-to-end
9. JSON report on stdout

#### TDD
```
RED:     test_helmfile_destroy_velero_only() — outras releases não tocadas
RED:     test_helmfile_sync_velero_brings_bsl_available() — BSL Available em <2min após sync
RED:     test_post_sync_5_schedules_enabled() — todas as 5 schedules em phase Enabled
RED:     test_post_sync_backup_works() — `velero backup create --wait` retorna Completed
RED:     test_precondition_certmanager_required() — (EC-6) script aborts fast se cert-manager não Ready
RED:     test_precondition_minio_bucket_required() — (EC-6) script aborts fast se bucket velero-backups não existe
RED:     test_precondition_secret_required() — (EC-6) script aborts fast se velero-s3-credentials Secret faltando
GREEN:   Implementar script
REFACTOR: Nenhum esperado
VERIFY:  bash infra/scripts/velero-helmfile-validate.sh
```

#### Acceptance Criteria
- [ ] Script roda em <5min total
- [ ] Pod Velero 1/1 Running pós-sync
- [ ] BSL `default` phase=Available
- [ ] 5 schedules `Enabled`
- [ ] On-demand backup `Completed`
- [ ] Outras releases (cert-manager, MinIO) intactas (helm list compara antes/depois)
- [ ] Idempotent (rerun 2x produz mesmo resultado)

#### DoD
- [ ] Script funcional
- [ ] LIVE evidence: helmfile sync completa Velero healthy
- [ ] go vet ./infra/tests/... silent

---

## Phase 2: DO Spaces backend validation (dev stack)

**Objective:** Validar que Velero contra DO Spaces (não MinIO) funciona end-to-end. Captura quirks específicos do provider DO.

### T2.1 — Provisionar bucket DO Spaces velero-backups via Pulumi

#### Objective
`pulumi up --stack dev` cria o bucket `loopzero-infra-sfo3-velero-backups` (T0.1 do plano-mãe).

#### Evidence
- Plano-mãe T0.1 entregou o código TypeScript (validado por vitest 9/9), mas `pulumi up` nunca rodou em dev stack
- Bucket criação envolve DO API key + correct billing account — pode falhar em scenarios de quota / IAM

#### Files to edit
Nenhum (apenas execução).

#### Deep Dives
- **Pulumi command:** `cd infra/pulumi && npx pulumi up --stack dev` — cria todos os 6 buckets (incluindo o novo velero-backups) idempotente
- **Side effects:** outros buckets (artifacts, builds, logs, static-assets, container-images) já existem; pulumi reconhece e deixa intactos
- **DO Spaces specifics:** versioning enabled requires DO Spaces tier que suporta (todos suportam atualmente)
- **(EC-10 — DOCUMENT) Stack pre-selection:** operador deve `pulumi stack select dev` antes de `pulumi up --stack dev`. Se stack não existe localmente, recovery é `pulumi stack init dev` + import state do bucket. Não é blocker; documentar como pré-condição explícita no script.

#### Tasks
1. **(EC-10)** Pré-condition: `pulumi stack ls 2>/dev/null | grep -q dev` — falha clara se stack `dev` não está inicializado localmente. Sugestão de fix na mensagem de erro: `pulumi stack init dev` + `pulumi stack select dev`.
2. `cd infra/pulumi && npx pulumi preview --stack dev` — confirmar que apenas bucket novo está nas changes
3. `npx pulumi up --stack dev --yes`
4. Verificar bucket existe: `aws s3 --endpoint-url https://sfo3.digitaloceanspaces.com ls s3://loopzero-infra-sfo3-velero-backups/`
5. Capturar `pulumi stack output veleroBackupsBucketName`

#### TDD
```
RED:     test_pulumi_preview_only_velero_changes() — preview output não toca outros buckets
RED:     test_bucket_exists_post_apply() — `aws s3 ls` retorna o bucket
RED:     test_bucket_versioning_enabled() — `aws s3api get-bucket-versioning` retorna Enabled
GREEN:   `npx pulumi up`
REFACTOR: Nenhum
VERIFY:  evidência via pulumi stack output + aws s3
```

#### Acceptance Criteria
- [ ] `pulumi preview` mostra apenas 1 recurso novo (`SpacesBucket: loopzero-infra-sfo3-velero-backups`)
- [ ] `pulumi up` succeeds em <2min
- [ ] Bucket verificado via aws s3 cli
- [ ] Versioning enabled
- [ ] `pulumi stack output veleroBackupsBucketName` retorna o nome correto

#### DoD
- [ ] Bucket existe em DO Spaces
- [ ] Pulumi state armazenado corretamente

### T2.2 — Velero contra DO Spaces

#### Objective
`velero backup-location get` retorna `Available` quando o BSL aponta para DO Spaces (não MinIO).

#### Evidence
- ADR D2 plano-mãe: "Usar DigitalOcean Spaces como BackupStorageLocation"
- Toda LIVE validation foi MinIO local
- DO Spaces tem quirks documentados: `s3ForcePathStyle: true` é obrigatório, `publicUrl` vazio para evitar invalid URL errors em URLs pre-signed, `x-amz-content-sha256` exige Velero >= 1.14 com plugin AWS >= 1.10.1 (tudo já configurado)

#### Files to edit
```
(nenhum — apenas execução)
```

#### Deep Dives
- **Pre-condition:** `MGMT_KUBECONFIG` apontando para mgmt cluster do dev stack (DOKS)
- **Pre-condition:** `LOOPZERO_SPACES_KEY/SECRET` exportados ou Secret `velero-s3-credentials` já criado pelo `generate-secrets.sh dev mgmt`
- **Validation flow:** helmfile sync já roda generate-secrets antes do helm install. Apenas precisamos esperar BSL controller validar.

#### Tasks
1. **(EC-7 fix)** Context guard: `current=$(kubectl config current-context); echo "$current" | grep -E "(dev|loopzero-mgmt-dev|loopzero-mgmt-cloud)" || { echo "ERROR: not on dev cluster (current=$current)"; exit 1; }`. Previne validação acidental contra k3d local que destruiria tenant local em vez de cloud dev.
2. Garantir `kubectl` aponta para mgmt cluster dev: `kubectl config use-context loopzero-mgmt-dev` (ou equivalente)
3. `bash infra/helmfile/scripts/generate-secrets.sh dev mgmt` — cria velero-s3-credentials com LOOPZERO_SPACES_KEY/SECRET
4. `helmfile -e dev -l name=velero sync`
5. Aguardar `kubectl -n velero-system get bsl default -o jsonpath='{.status.phase}'` retornar `Available` (timeout 5min)
6. `velero backup create do-spaces-test-$(date +%s) --include-resources tenants.loopzero.io --wait`
7. Verify backup landed: `aws s3 --endpoint-url https://sfo3.digitaloceanspaces.com ls s3://loopzero-infra-sfo3-velero-backups/backups/`

#### TDD
```
RED:     test_velero_dev_bsl_available() — BSL phase=Available em <5min após sync dev
RED:     test_velero_dev_backup_lands_in_do_spaces() — aws s3 ls mostra o objeto criado
RED:     test_velero_dev_backup_describe_completed() — `velero backup describe` retorna Phase: Completed
RED:     test_kubectl_context_guard() — (EC-7) script falha com mensagem clara se context não match dev
GREEN:   helmfile sync + velero backup
REFACTOR: Nenhum
VERIFY:  evidência via velero backup get + aws s3 ls
```

#### Acceptance Criteria
- [ ] BSL `default` phase=Available no dev cluster
- [ ] Backup `do-spaces-test-*` Completed
- [ ] Objeto presente em DO Spaces bucket
- [ ] `velero backup describe` mostra > 0 items
- [ ] Tempo total <10min

#### DoD
- [ ] Velero opera contra DO Spaces
- [ ] LIVE evidence registrada em `docs/runbooks/velero-do-spaces-validation.md` (NEW)

### T2.3 — Restore from DO Spaces

#### Objective
Restore de um backup criado em DO Spaces volta para um cluster (mesmo cluster mgmt do dev).

#### Evidence
- Plano-mãe drill apenas testou MinIO; nunca exercitou descarga de backup do DO Spaces
- Network egress + multipart download podem falhar em redes restritas

#### Tasks
1. Identificar backup criado em T2.2 (e.g. `do-spaces-test-1778086138`)
2. Pegar 1 tenant teste no cluster dev (não-crítico): exemplo `e2e-tenant`
3. **(EC-3 fix — DEFENSIVE BACKUP)** Snapshot YAML do tenant ANTES de qualquer operação destrutiva:
   `kubectl get tenant e2e-tenant -n loopzero-system -o yaml > /tmp/defensive-tenant-snapshot-$(date +%s).yaml`.
   Esse arquivo é o rollback de última instância: se Velero restore falhar (DO Spaces incident, BSL
   Unavailable, plugin bug), recuperamos o spec via `kubectl apply -f /tmp/defensive-...yaml`. Não
   recupera Secrets/Apps mas pelo menos o Tenant CR retorna.
4. Backup snapshot via Velero: `velero backup create do-spaces-restore-test --include-resources tenants.loopzero.io --wait`
5. Patch tenant finalizers + delete (Phase 3 detalha mais)
6. `velero restore create restore-test --from-backup do-spaces-restore-test --wait`
7. Verify tenant retornou: `kubectl get tenant e2e-tenant -n loopzero-system`
8. **(EC-3 fallback)** Se Step 7 falhar após retry, executar `kubectl apply -f /tmp/defensive-tenant-snapshot-*.yaml` como rollback. Documentar a falha de Velero como achado P0 do plano (block subsequent phases até root cause).

#### TDD
```
RED:     test_restore_from_do_spaces_completes() — restore phase=Completed
RED:     test_restored_tenant_has_correct_spec() — yaml comparison antes/depois
RED:     test_defensive_snapshot_created() — (EC-3) /tmp/defensive-tenant-snapshot-*.yaml existe e é parseable
RED:     test_defensive_fallback_works() — (EC-3) simular Velero restore failure; kubectl apply do snapshot recupera o Tenant CR
GREEN:   Execute steps
REFACTOR: Nenhum
VERIFY:  velero restore describe + kubectl get tenant + ls /tmp/defensive-tenant-snapshot-*.yaml
```

#### Acceptance Criteria
- [ ] Restore completa em <2min
- [ ] Tenant restaurado spec idêntico ao backup
- [ ] Annotations preservadas

#### DoD
- [ ] Restore from DO Spaces empiricamente provado
- [ ] Runbook atualizado com timing real (RTO observado)

---

## Phase 3: Restore REAL de tenant com finalizers

**Objective:** Provar que o restore loop funciona contra um Tenant CR com finalizers ativos, namespaces filhos, ArgoCD Apps, e ESO Secrets dependentes — exatamente o cenário que o drill sintético NÃO exercita.

### T3.1 — Backup pre-condição: tenant ativo selecionado

#### Objective
Identificar e capturar backup de um tenant real com todas as dependências cross-cutting.

#### Evidence
- `kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenant e2e-tenant` mostra finalizer `loopzero.io/tenant-cleanup`
- Plano-mãe drill usa `drill-test` com finalizer-bypass — nunca exercitou cleanup real

#### Files to edit
```
infra/scripts/velero-real-restore-test.sh (NEW) — orquestrador
```

#### Deep file dependency analysis
- **Script** (NEW) — encapsula sequence: select tenant → snapshot dependencies (namespace count, ArgoCD App count, Secret count) → backup → simulate disaster → restore → verify all dependencies restored. Downstream: nenhum.

#### Deep Dives
- **Tenant escolhido:** `e2e-tenant` (presente em k3d local) ou `tn-e2etest` (presente em dev cloud)
- **Dependencies a capturar pre-disaster:**
  - 2 namespaces filhos (`loopzero-e2e-tenant-production`, `loopzero-e2e-tenant-staging`)
  - 1+ Application em `argocd` com nome `e2e-tenant-{env}`
  - N Secrets em `loopzero-system` com label `loopzero.io/tenant=e2e-tenant`
  - Status conditions: GitRepoReady, ArgoCDAppReady, QuotaEnforced, Ready
- **Backup includes:** CRDs (Tenant), namespaces (loopzero-e2e-tenant-*), argocd namespace, loopzero-system Secrets

#### Tasks
1. Criar script com Phase 1: snapshot pré-disaster
2. `kubectl get tenant e2e-tenant -o yaml > /tmp/pre-tenant.yaml`
3. `kubectl get ns -l loopzero.io/tenant=e2e-tenant -o name | wc -l > /tmp/pre-ns-count`
4. `kubectl -n argocd get applications -l loopzero.io/tenant=e2e-tenant -o name | wc -l > /tmp/pre-app-count`
5. `kubectl -n loopzero-system get secrets -l loopzero.io/tenant=e2e-tenant -o name | wc -l > /tmp/pre-secret-count`
6. `velero backup create real-restore-test-$(date +%s) --include-resources tenants.loopzero.io,namespaces,applications.argoproj.io,secrets --selector loopzero.io/tenant=e2e-tenant --wait`

#### TDD
```
RED:     test_pre_disaster_snapshot_captures_all_deps() — 4 files in /tmp populated
RED:     test_backup_includes_all_resource_types() — `velero backup describe` shows tenants, namespaces, applications, secrets
GREEN:   Implementar
REFACTOR: Nenhum
VERIFY:  bash infra/scripts/velero-real-restore-test.sh --phase=snapshot
```

#### Acceptance Criteria
- [ ] Snapshot files criados
- [ ] Backup `Completed` com items > 0 para cada resource type
- [ ] Backup inclui Tenant + 2 namespaces + 1+ Application + N Secrets

#### DoD
- [ ] Phase 1 do script funcional

### T3.2 — Simulate disaster: delete tenant + finalizer cleanup

#### Objective
Forçar o cleanup do tenant via patching finalizers (mimic do que o drill faz, mas em tenant real).

#### Evidence
- Tenant CR tem finalizer `loopzero.io/tenant-cleanup` que bloqueia delete síncrono
- Simulate disaster precisa NÃO esperar pelo controller (que poderia introduzir delays multi-min)

#### Tasks
1. **(EC-3 fix)** Defensive snapshot ANTES de qualquer destrutiva — esta phase reusa o snapshot
   de T3.1 step 2 (`/tmp/pre-tenant.yaml`). Se T3.1 não rodou ou snapshot está vazio, abortar fast
   com erro claro. Adicional: snapshot fresco do estado atual antes do patch:
   `kubectl get tenant e2e-tenant -n loopzero-system -o yaml > /tmp/defensive-pre-delete-$(date +%s).yaml`.
2. `kubectl patch tenant e2e-tenant -n loopzero-system --type=merge -p '{"metadata":{"finalizers":[]}}'`
3. `kubectl delete tenant e2e-tenant -n loopzero-system --grace-period=0 --force`
4. Verify tenant gone: `kubectl get tenant e2e-tenant -n loopzero-system` retorna NotFound
5. Capturar logs do operator: `kubectl logs -n loopzero-system -l app=tenant-operator --tail=20 > /tmp/post-disaster-operator.log`
6. **(EC-3 fallback)** Se Phase T3.3 (restore) falhar 2 retries, recovery automático:
   `kubectl apply -f /tmp/defensive-pre-delete-*.yaml` revive Tenant CR (sem Apps/Secrets,
   mas evita a perda permanente do tenant em dev cluster).

#### TDD
```
RED:     test_tenant_finalizer_patched() — finalizers field empty after patch
RED:     test_tenant_deleted() — kubectl get returns NotFound
RED:     test_defensive_snapshot_pre_delete() — (EC-3) /tmp/defensive-pre-delete-*.yaml existe + parseable + spec.displayName matches
RED:     test_t3_1_snapshot_required() — (EC-3) phase aborta fast se /tmp/pre-tenant.yaml não existir
GREEN:   Implement
REFACTOR: Nenhum
VERIFY:  Phase 2 do script
```

#### Acceptance Criteria
- [ ] Tenant CR removed
- [ ] Operator não panica (logs sem traceback)
- [ ] **(EC-3)** /tmp/defensive-pre-delete-*.yaml criado E parseable como Tenant CR válido
- [ ] **(EC-3)** Fallback recovery testado simulando Velero restore failure

#### DoD
- [ ] Phase 2 funcional
- [ ] **(EC-3)** Defensive snapshot path exercitado em pelo menos 1 dry-run

### T3.3 — Restore + verify all dependencies returned

#### Objective
`velero restore create` traz Tenant CR + Namespaces + Applications + Secrets. Verify counts match pre-disaster.

#### Evidence
- Drill sintético verifica APENAS o Tenant CR; aqui verificamos todo o grafo de dependencies

#### Tasks
1. `velero restore create real-restore-$(date +%s) --from-backup real-restore-test-* --wait`
2. Aguardar 60s para controllers reconcilarem
3. **(EC-5 fix)** ClusterCredential post-restore refresh: rodar `task k3d:heal` (que codifica H12-H15
   inclusive cell-1 ClusterCredential bootstrap + cell-2 token refresh). ClusterCredentials no
   backup têm tokens que podem estar inválidos pós-restore (SA UID muda quando recreated, expira
   independente do exp claim). Sem refresh, cells ficam disconnected silently.
4. Verify ClusterCredentials connected: `kubectl get clustercredential -A -o jsonpath='{.items[*].status.connected}'`
   retorna `true true true` (3 cells: default, cell-1, cell-2).
5. Verify: `diff /tmp/pre-tenant.yaml <(kubectl get tenant e2e-tenant -o yaml)` (modulo resourceVersion + status)
6. Verify counts: ns/app/secret counts match pre-disaster
7. Verify HTTP probe: deploy de smoke fixture against tenant funciona

#### TDD
```
RED:     test_restore_completes() — Phase=Completed
RED:     test_tenant_restored_spec() — spec matches pre-disaster (modulo timestamps)
RED:     test_namespace_count_matches() — N namespaces back
RED:     test_application_count_matches() — N apps back
RED:     test_secret_count_matches() — N secrets back
RED:     test_smoke_deploy_works() — `loopzero deploy` against restored tenant succeeds (post-restore)
RED:     test_clustercredential_tokens_valid_post_restore() — (EC-5) status.connected=true para todos cells após task k3d:heal
RED:     test_argocd_apps_resync() — (EC-5 + Padrão Single ArgoCD App) `argocd app sync --hard-refresh` aplicado para evitar repo-server cache (5min)
GREEN:   Execute
REFACTOR: Nenhum
VERIFY:  Phase 3 do script + smoke test integration
```

#### Acceptance Criteria
- [ ] Restore Phase=Completed
- [ ] All 4 counts (tenant + 2 ns + N app + N secret) match pre-disaster
- [ ] HTTP smoke probe ok against restored tenant
- [ ] No data loss (compared to pre-tenant.yaml)

#### DoD
- [ ] Real restore prova que finalizers + namespaces + ArgoCD + ESO funcionam após restore
- [ ] LIVE evidence em `docs/runbooks/disaster-recovery-restore.md` (atualizar com timing real)

---

## Phase 4: Prometheus alerting + CI workflow

**Objective:** Provar que o alert chain (scrape → rule eval → fire) funciona end-to-end E que o CI workflow GitHub Actions executa contra cluster real.

### T4.1 — Prometheus scrape velero metrics

#### Objective
Verify que `velero_backup_*` metrics aparecem em Prometheus + estão sendo scrapeadas.

#### Evidence
- T3.2 do plano-mãe entregou `extraScrapeConfigs` em `values-prometheus.yaml`
- Helm template green, mas nunca verificado em runtime

#### Files to edit
```
(nenhum — apenas execução)
```

#### Tasks
1. `kubectl --context k3d-loopzero-mgmt port-forward -n monitoring svc/prometheus-server 9090:80 &`
2. `curl -s 'http://localhost:9090/api/v1/targets' | jq '.data.activeTargets[] | select(.labels.job=="velero")'`
3. Confirmar `health: "up"` no target velero
4. `curl -s 'http://localhost:9090/api/v1/query?query=velero_backup_success_total' | jq '.data.result'`
5. Confirmar pelo menos 1 series retornada (de backups das phases anteriores)

#### TDD
```
RED:     test_velero_target_up() — Prometheus reports velero job up
RED:     test_velero_metrics_scraped() — velero_backup_* metrics return non-empty result
GREEN:   helmfile sync monitoring + verify
REFACTOR: Nenhum
VERIFY:  curl + jq
```

#### Acceptance Criteria
- [ ] Prometheus target `velero` health=up
- [ ] `velero_backup_success_total` series present
- [ ] `velero_backup_last_successful_timestamp` series present

#### DoD
- [ ] Scrape end-to-end empiricamente provado

### T4.2 — Alert dispatch test (EC-4 fix: promtool test rules + pause schedule)

#### Objective
Validar que a EXPR de `LoopZeroVeleroBackupStale` dispara corretamente para entradas antigas E que o ciclo end-to-end (pause schedule → alert fire → unpause → alert clear) funciona em ambiente isolado.

#### Evidence
- ADR D4 deste plano (revisado pós-edge-case): pause schedule (real time wait OU promtool test) — metric mock via recording rule é INVIÁVEL (recording rules computam expressões agregadas, não substituem séries existentes)
- Alert expr: `time() - velero_backup_last_successful_timestamp{schedule=~"loopzero-.*"} > 43200` (12h)

#### Files to edit
```
infra/helm/monitoring/manifests/prometheusrule-velero.test.yaml (NEW) — promtool test fixtures
```

#### Deep Dives
- **(EC-4 fix) Promtool test rules é a abordagem canônica:** o arquivo `*.test.yaml` define séries literais com timestamps no passado, depois assert que o alert `LoopZeroVeleroBackupStale` está firing aos 12h+. Roda offline em <1s, isolado de cluster real. Cobre 80% do test sem impactar prod.
- **Ciclo real (pause schedule):** complementa o promtool test exercitando o path completo (Velero metric → Prometheus scrape → AlertManager dispatch). Requer 12h em cluster com schedule pausado.

#### Tasks
1. **(EC-4 fix)** Criar `infra/helm/monitoring/manifests/prometheusrule-velero.test.yaml` com:
   - `rule_files: [prometheusrule-velero.yaml]`
   - `tests:` com 2 cases:
     - `interval: 1m`, série `velero_backup_last_successful_timestamp{schedule="loopzero-secrets-backup"}` valor antigo (e.g. `0`), `eval_time: 13h`, `exp_alerts: LoopZeroVeleroBackupStale firing`
     - `interval: 1m`, série com valor recente, `eval_time: 1m`, `exp_alerts: []` (silenciado)
2. **(EC-4 fix)** Rodar `promtool test rules infra/helm/monitoring/manifests/prometheusrule-velero.test.yaml` — assert exit 0 + 2/2 cases pass
3. Ciclo real (não-bloqueante para DoD se promtool passa): pause `velero-loopzero-secrets-backup` em ambiente isolado, esperar 12h, verify alert fired via Alertmanager API, unpause + verify cleared
4. Adicionar `promtool test rules` ao `task validate:manifests` para regression-prevention

#### TDD
```
RED:     test_alert_fires_on_stale_backup_via_promtool() — (EC-4) `promtool test rules` retorna exit 0 + 2 cases pass
RED:     test_alert_silences_when_fresh_via_promtool() — (EC-4) test case com timestamp recente NÃO dispara
RED:     test_promtool_test_in_validate_manifests() — Taskfile.yml validate:manifests inclui o promtool step
RED:     test_alert_fires_in_real_cluster() — (opcional, async 12h+) pause + wait + Alertmanager API check
GREEN:   Criar prometheusrule-velero.test.yaml + add Taskfile entry
REFACTOR: Nenhum
VERIFY:  promtool test rules infra/helm/monitoring/manifests/prometheusrule-velero.test.yaml
```

#### Acceptance Criteria
- [ ] `prometheusrule-velero.test.yaml` criado com 2+ test cases
- [ ] `promtool test rules` exit 0 (CI-validavel offline)
- [ ] `task validate:manifests` inclui o promtool test step
- [ ] **(opcional)** Alert real fired + cleared em ambiente isolado dentro de 24h
- [ ] Alert annotations include runbook URL (já validado por kubeconform)

#### DoD
- [ ] Promtool test passa (offline, em CI)
- [ ] Test fixture committed
- [ ] Real-cluster cycle documented mesmo se executado async (deadline +7 dias)

### T4.3 — CI workflow execution

#### Objective
Configurar `MGMT_KUBECONFIG` GitHub secret + executar workflow_dispatch.

#### Evidence
- `.github/workflows/velero-backup-check.yml` válido YAML mas nunca executado

#### Files to edit
```
(nenhum no repo — apenas configuração GitHub)
```

#### Tasks
1. **(EC-8 fix)** Pre-validate workflow YAML via `actionlint .github/workflows/velero-backup-check.yml`
   antes de qualquer push/dispatch. Captura erros estruturais (action versão inválida, expression
   syntax, shell quoting) que `python yaml.safe_load` não pega. Adicionar ao `task validate:manifests`
   como step extra (ou `task lint:workflows`) para regression.
2. Generate kubeconfig limited-scope para Velero: ServiceAccount em `velero-system` + RoleBinding read-only para Velero CRDs
3. Encode em base64
4. Adicionar como GitHub repo secret `MGMT_KUBECONFIG` (via UI ou `gh secret set`)
5. `gh workflow run velero-backup-check.yml`
6. `gh run watch` — confirma workflow completa
7. Se backup freshness OK: workflow exit 0 (no issue created)
8. Se backup stale: workflow exit 1 + issue criada

#### TDD
```
RED:     test_workflow_exit_0_when_fresh() — happy path workflow run
RED:     test_workflow_creates_issue_on_stale() — negative test: pause schedule, run workflow, verify issue created
RED:     test_actionlint_workflow_yaml() — (EC-8) actionlint exit 0 contra .github/workflows/velero-backup-check.yml
GREEN:   Configure secret + dispatch
REFACTOR: Nenhum
VERIFY:  gh run view <run-id> + actionlint .github/workflows/velero-backup-check.yml
```

#### Acceptance Criteria
- [ ] `MGMT_KUBECONFIG` secret presente
- [ ] Workflow_dispatch executa em <5min
- [ ] Happy path: exit 0, no issue
- [ ] Negative path: exit 1, issue criada com label `backup-freshness`

#### DoD
- [ ] CI workflow ativo e testado em ambos paths

---

## Phase 5: Risk-accept ADRs + workaround tracking

**Objective:** Formalizar EC-6 e EC-7 como risk-accepts ADR + criar tracking issue para `upgradeCRDs` workaround.

### T5.1 — ADR-0024 velero upgradeCRDs workaround

#### Objective
Documentar o workaround `upgradeCRDs: false` formalmente com data de revisão.

#### Files to edit
```
docs/meetings/adr/ADR-0024-velero-upgrade-crds-workaround.md (NEW)
```

#### Deep Dives
- **Trigger:** Bitnami pruned `bitnami/kubectl` from Docker Hub in 2025
- **Impact:** chart's pre-install hook ImagePullBackOff
- **Mitigation:** disable hook (CRDs created via template)
- **Review condition:** Velero v1.15+ chart release OR Bitnami restores image
- **Review date:** 2026-08-06 (3 meses)

#### Tasks
1. Criar ADR seguindo template em `docs/meetings/adr/`
2. Cross-reference plano-mãe v1.1 §Lessons captured #1
3. Adicionar tracking issue Github (`type: tech-debt`, `area: infra/helm`)

#### TDD
N/A (documento)

#### Acceptance Criteria
- [ ] ADR written, signed, dated
- [ ] Cross-references intact

### T5.2 — ADR-0025 velero risk-accept (EC-6, EC-7)

#### Objective
Formalizar os 2 risks aceitos do plano-mãe.

#### Files to edit
```
docs/meetings/adr/ADR-0025-velero-risk-accept.md (NEW)
```

#### Deep Dives
- **EC-6 (DO Spaces global API key):** mesma key acessa todos os 6 buckets. Mitigation futura: migrar para STS/IAM granular quando DO suportar.
- **EC-7 (Secrets em plaintext em S3):** Velero backups dos Secrets do `loopzero-system` são encripted at-rest pelo provider mas readable com a key. Mitigation futura: client-side encryption antes do upload.
- **(EC-11 — DOCUMENT) Signature async:** Sponsor (Platform Architect) + Challenger (Security Engineer) podem demorar dias para responder. Plano NÃO bloqueia outras phases por isso — Phase 5 é última, ADR pode ser commit com placeholder `[ ] Sponsor: __pending__ (deadline 2026-05-13)` e referenced explicitly como debt. Plan completion = ADR file existe + tracking; full ratification é continuous follow-up.

#### Tasks
1. Criar ADR
2. Sponsor signature: Platform Architect (deadline 7 dias após commit)
3. Challenger signature: Security Engineer (deadline 7 dias após commit)
4. **(EC-11)** Permitir merge com signatures `__pending__` se >= 1 das 2 já assinou OU se ADR está aguardando há <7 dias. Track via comment-watcher GitHub action ou planilha simples de pending.

#### Acceptance Criteria
- [ ] ADR written
- [ ] 2 signatures (ou 1 + tracker para a outra com deadline 7d)
- [ ] Filed under `docs/meetings/adr/`
- [ ] **(EC-11)** Async signature workflow não bloqueia Plan DoD — `__pending__` placeholder aceito com deadline tracker

### T5.3 — Tracking issue upstream upgradeCRDs

#### Objective
Open issue em `vmware-tanzu/helm-charts` para visibility upstream.

#### Tasks
1. **(EC-9 fix)** Search-before-create idempotente: `count=$(gh issue list -R vmware-tanzu/helm-charts -S "bitnami/kubectl" --json number,title --state all | jq 'length')`. Se `$count > 0`, listar issues encontradas e usar a mais recente como cross-reference (não criar duplicata).
2. Se `$count == 0`: `gh issue create -R vmware-tanzu/helm-charts --title "bitnami/kubectl image tag 1.32 missing — chart upgrade-CRDs hook breaks" --body "Linking from: docs/meetings/adr/ADR-0024-velero-upgrade-crds-workaround.md ..."`.
3. Cross-reference no ADR-0024 (URL fixa ou link de issue achada)

#### TDD
```
RED:     test_gh_issue_search_first() — (EC-9) gh issue list executa antes de create; se já existe, usa link da existente
RED:     test_no_duplicate_issue() — (EC-9) rerun do task NÃO cria segunda issue
GREEN:   Implement
REFACTOR: Nenhum
VERIFY:  gh issue list -R vmware-tanzu/helm-charts -S bitnami/kubectl
```

#### Acceptance Criteria
- [ ] Upstream issue created or referenced (if pre-existing)
- [ ] Link em ADR-0024
- [ ] **(EC-9)** Rerun de T5.3 não cria issue duplicada

---

## Edge Case Mitigation Summary

(Resultado do `/edge-case-plan velero-prod-canary` — 2026-05-06; v1.1 incorporated)

| EC | Tipo | Task afetada | Status | Solução incorporada |
|----|------|---------------|--------|---------------------|
| EC-1 | MUST FIX | T0.1 | ✅ incorporated | Sentry pre-condition + trap EXIT/INT/TERM no script velero-cron-observe.sh |
| EC-2 | SHOULD TEST | T0.2 | ✅ incorporated | Override `velero.configuration.garbageCollectionFrequency=1m` durante test |
| EC-3 | MUST FIX | T2.3, T3.2 | ✅ incorporated | Defensive YAML snapshots em `/tmp/defensive-*.yaml` antes de qualquer destrutiva + fallback `kubectl apply` |
| EC-4 | MUST FIX | T4.2 | ✅ incorporated | Substituído metric mock por `promtool test rules` + arquivo `prometheusrule-velero.test.yaml` |
| EC-5 | SHOULD TEST | T3.3 | ✅ incorporated | Phase explícita `task k3d:heal` pós-restore + assertion ClusterCredential connected=true |
| EC-6 | SHOULD TEST | T1.1 | ✅ incorporated | Phase 0 pré-condition checks (cert-manager Ready, MinIO Ready, bucket existe, Secret existe) |
| EC-7 | SHOULD TEST | T2.2 | ✅ incorporated | Context guard regex no início do script |
| EC-8 | SHOULD TEST | T4.3 | ✅ incorporated | `actionlint` pre-validate em CI workflow YAML |
| EC-9 | SHOULD TEST | T5.3 | ✅ incorporated | `gh issue list` antes de create (idempotent) |
| EC-10 | DOCUMENT | T2.1 | ✅ incorporated | Pré-condição `pulumi stack ls | grep dev` com erro claro |
| EC-11 | DOCUMENT | T5.2 | ✅ incorporated | Signature async permitida com placeholder + 7d deadline tracker |

**11/11 edge cases incorporated.** Plano v1.1 está pronto para implementação.

---

## Coverage Matrix

| # | Gap | Severidade | Phase / Task | Resolution |
|---|---|---|---|---|
| G1 | Schedules cron nunca dispararam | 🟡 médio | Phase 0 / T0.1, T0.2 | Bump 1 schedule to */5min, observe 5+ Completed, revert |
| G2 | DO Spaces backend nunca testado LIVE | 🟠 alto | Phase 2 / T2.1, T2.2, T2.3 | Pulumi up dev + helmfile sync + backup + restore |
| G3 | helmfile sync LIVE com prod env | 🟠 alto | Phase 1 / T1.1 | helmfile sync standalone-multi com Velero healthy |
| G4 | Restore de Tenant REAL (não-drill) | 🟠 alto | Phase 3 / T3.1, T3.2, T3.3 | Backup → patch finalizer → delete → restore → verify deps |
| G5 | CI workflow nunca rodou | 🟡 médio | Phase 4 / T4.3 | Configure secret + workflow_dispatch + verify both paths |
| G6 | Prometheus alerting end-to-end | 🟡 médio | Phase 4 / T4.1, T4.2 | Scrape verify + alert dispatch test (pause + revert) |
| G7 | upgradeCRDs workaround | 🟢 baixo | Phase 5 / T5.1, T5.3 | ADR-0024 + upstream tracking issue |
| G8 | EC-6 DO Spaces global API key | 🟢 aceito | Phase 5 / T5.2 | ADR-0025 risk-accept signed |
| G9 | EC-7 Secrets plaintext S3 | 🟢 aceito | Phase 5 / T5.2 | ADR-0025 risk-accept signed |

**Coverage: 9/9 gaps covered (100%)**

## Global Definition of Done

- [ ] Todas as phases (0-5) completadas
- [ ] Todos os tests passing (helm template + kubeconform + bash -n + helmfile lint)
- [ ] Zero regressões (validate:manifests EXIT=0)
- [ ] 5 schedules têm `LASTBACKUP` populado por pelo menos 1 ciclo natural
- [ ] DO Spaces BSL Available no dev stack (não só MinIO)
- [ ] `helmfile sync` end-to-end Velero healthy
- [ ] Restore real de tenant com finalizers concluído + smoke test ok
- [ ] CI workflow executou ≥1x em GitHub Actions
- [ ] Alert `LoopZeroVeleroBackupStale` disparou + clearou (test cycle)
- [ ] ADR-0024 + ADR-0025 escritos + signed
- [ ] Tracking issue upstream upgradeCRDs criado
- [ ] **`/cross-validation velero-prod-canary` PASSED** — zero CRITICO, zero GRAVE
- [ ] **`/dogfood full` PASSED** (ver Dogfood Gate abaixo)

---

## Dogfood Gate (MANDATORY -- NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Apply all velero artifacts (post velero-backup-dr v1.1 + this plan)
helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e standalone-multi sync -l name=velero
bash infra/scripts/velero-cron-observe.sh --quick   # 30min observation
bash infra/scripts/velero-real-restore-test.sh      # real tenant restore

# 3. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] All 5 schedules have at least 1 `Completed` backup observed
- [ ] DO Spaces BSL Available in dev stack
- [ ] Real tenant restore with finalizers + namespaces + ArgoCD Apps + Secrets verified
- [ ] CI workflow executed in GitHub Actions (success or expected negative)
- [ ] `LoopZeroVeleroBackupStale` alert dispatch + clear cycle observed
- [ ] No regressions in previously passing flows
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.

---

## Governance Note

This plan is a **T2 change** (validation phases against production-like infra, ADR creation, GitHub repo configuration). Per pre-release governance rules, requires meeting approval before Phase 2+ (DO Spaces) since cloud resource changes touch production infra. Submit via `/meeting` with scope: validation + governance.
