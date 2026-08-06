---
type: plan
title: "Edge Case Review — 2026-05-28-M07-fulcio-oidc-trust-to-3-85"
---

# Edge Case Review — 2026-05-28-M07-fulcio-oidc-trust-to-3-85

**Data:** 2026-05-28
**Plano:** [docs/plans/2026-05-28-M07-fulcio-oidc-trust-to-3-85-plan.md](./2026-05-28-M07-fulcio-oidc-trust-to-3-85-plan.md)
**Tasks analisadas:** 9 (T0.1, T0.2, T0.3, T1.1, T2.1, T2.2, T3.1, T4.1, T4.2)
**Edge cases encontrados:** 14 (MUST FIX: 7, SHOULD TEST: 4, DOCUMENT: 3)

## MUST FIX

### EC-1: Plano assume `builds.attestation_url` column que NÃO EXISTE

- **Task afetada:** T3.1 (Phase 3 acceptance criteria) + T4.1 (audit doc evidence)
- **Familia:** Format / Integration (DB schema mismatch)
- **Cenário:** Plan acceptance criteria diz:
  > `SELECT id, status, attestation_url FROM builds WHERE project_id='m07-keyless-app' ORDER BY created_at DESC LIMIT 1` — espera `attestation_url` non-null
  
  Mas LIVE schema `\d builds` mostra **ZERO** colunas attest/rekor/sign. A coluna real é `releases.image_attestation_rekor_url` (verificado: 3 rows com signature populated em release_app_attestations + releases tem `image_attestation_rekor_url`, `image_signed_at`, `image_signature`).
- **Impacto:** Operator roda query do plano → erro `column "attestation_url" does not exist` → operator pode "interpretar como green" se ignorar erro OU dar plan como done quando attestation nunca validada. Mascarar 3.85 sem proof.
- **Fix sugerido:** Substituir em T3.1 acceptance criteria + Deep Dives Sequence step 4 + T4.1 evidence:
  ```sql
  SELECT id, image_signed_at, image_attestation_rekor_url
  FROM releases
  WHERE id IN (SELECT release_id FROM release_app_attestations WHERE app_name='m07-keyless-app')
  ORDER BY created_at DESC LIMIT 1;
  ```
  Acceptance: `image_attestation_rekor_url IS NOT NULL` (regex match Rekor entry URL `^http://rekor-server\.sigstore-system\.svc\.cluster\.local:80/api/v[12]/log/entries/.+$`).

### EC-2: Concurrent diagnose script runs colidem em pod names

- **Task afetada:** T0.1 (script design)
- **Familia:** Concurrency / Resource
- **Cenário:** Dois operators rodam `task fulcio:diagnose` simultaneamente. Ambos tentam `kubectl run sa-token-probe ...` → segundo retorna `AlreadyExists`. Script crash sem cleanup adequado.
- **Impacto:** Pod orfão + Fulcio com `--log-level=debug` permanente (rollback do trap falha).
- **Fix sugerido:** Sufixo random em todos os pod names + lock file:
  ```bash
  PROBE_ID="$(date +%s)-$$"
  flock -n /tmp/fulcio-diagnose.lock || { echo "Already running"; exit 5; }
  # use: sa-token-probe-${PROBE_ID}, net-probe-${PROBE_ID}
  ```

### EC-3: Empty SA token crash em probe 1 (audience não autorizado)

- **Task afetada:** T0.1 (probe_sa_token_iss function)
- **Familia:** Input / Permission
- **Cenário:** Se kube-apiserver tem `--api-audiences` configurado mas `sigstore` não está na lista, ProjectedVolume monta arquivo vazio. Script chama `base64 -d` em string vazia → output vazio → jq retorna null em `iss` → verdict UNKNOWN.
- **Impacto:** Operator interpreta como cenário ambíguo + escalation desnecessária.
- **Fix sugerido:** Validar token non-empty antes de decode + emitir hint específico:
  ```bash
  [[ -s "$TOKEN_FILE" ]] || { echo "ERROR: SA token empty — check kube-apiserver --api-audiences includes 'sigstore'"; exit 6; }
  ```

### EC-4: Script falha quando chamado fora do repo root

- **Task afetada:** T0.1 (script bootstrap)
- **Familia:** Format / Path resolution
- **Cenário:** Operator roda `cd /tmp && bash ~/loopzero/infra/scripts/local/fulcio-oidc-diagnose.sh`. Script tenta `source lib/fulcio-probes.sh` com path relativo → file not found.
- **Impacto:** Script broken em qualquer cwd que não seja repo root; users no terminal/IDE não-padrão veem error críptico.
- **Fix sugerido:** Resolver path absoluto via BASH_SOURCE no header:
  ```bash
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  source "$SCRIPT_DIR/lib/fulcio-probes.sh"
  ```

### EC-5: sigstore-scaffolding chart 0.7.16 pode não expor `fulcio.server.config` schema

- **Task afetada:** T2.1 (Cenário C+D values override)
- **Familia:** Integration (Helm chart contract)
- **Cenário:** Plano já tem NOTE inline mas SEM fallback. Se chart 0.7.16 não tem `.fulcio.server.config.OIDCIssuers` key, override silently dropped — `helm template` renderiza sem error, mas Fulcio pod arranca com config default (sem fix). Operator faz `pulumi up`, vê tudo green, mas Phase 3 LIVE volta a falhar com mesmo HTTP 400.
- **Impacto:** False sense of success; loop back Phase 1 sem entender por quê (operator pode acusar diagnose script de bug).
- **Fix sugerido:** Adicionar vitest assertion que faz `helm template` REAL contra chart 0.7.16 e grep pelo issuer override no rendered ConfigMap. Se template não contém issuer → test fail → force fallback path (extraEnv ou raw configMap mount).
  ```typescript
  it("rendered ConfigMap contains override issuer (chart contract check)", () => {
    const rendered = renderHelmTemplate(release);
    expect(rendered).toContain('"IssuerURL": "https://kubernetes.default.svc"');
  });
  ```

### EC-6: Phase 1 verdict=GREEN cenário não-tratado em Phase 2

- **Task afetada:** T2.1 (Phase 2 entry)
- **Familia:** State / Path
- **Cenário:** Phase 1 diagnose pode retornar `verdict=GREEN` (sem fix needed — talvez Fulcio se auto-recuperou OR loopzero-api rebuild já tinha um fix lateral). Plan T2.1 assume sempre aplicar fix → operator confuso, aplica fix supérfluo, introduz drift.
- **Impacto:** Trabalho desperdiçado + risk de Pulumi up causar drift desnecessário.
- **Fix sugerido:** Adicionar guard no início de Phase 2:
  > Se Phase 1 verdict=GREEN → skip T2.1+T2.2 directly to Phase 3 (LIVE retry validate). Se LIVE GREEN também → pular para Phase 4 audit.

### EC-7: Mock kubectl não cobre `kubectl wait` subcommand

- **Task afetada:** T0.2 (mock-kubectl.sh) + T0.1 (script real)
- **Familia:** Integration (test coverage)
- **Cenário:** Script real precisa `kubectl wait --for=condition=Available deploy/fulcio-server` após patch (rollout aguarda). Plano lista 6 subcommands no mock (run/exec/get/patch/logs/delete) — sem `wait`. Hermetic test passa, prod travel ou falha silenciosamente.
- **Impacto:** Test green falsamente; prod regression escapa via CI.
- **Fix sugerido:** Adicionar `wait` ao mock + enumerar TODOS subcommands usados no script real via grep pre-test:
  ```bash
  # T0.2 setup: validate mock coverage
  USED=$(grep -oE 'kubectl [a-z]+' infra/scripts/local/fulcio-oidc-diagnose.sh | sort -u | awk '{print $2}')
  for sub in $USED; do grep -q "case .* $sub" mock-kubectl.sh || fail "$sub uncovered"; done
  ```

### EC-8: Phase 3 não diferencia pre-sign failure vs sign failure

- **Task afetada:** T3.1
- **Familia:** State / Diagnose
- **Cenário:** Build pode falhar em step ANTERIOR ao sign (build-image OOM, install-deps npm error, registry down). Plan T3.1 acceptance criteria assume "Workflow phase = Succeeded" mas falha em pre-sign step também = phase=Failed. Operator interpreta como "Fulcio fix não funcionou" e itera infinitamente.
- **Impacto:** Time wasted em wrong diagnose loop; operator pode reverter fix correto.
- **Fix sugerido:** Adicionar guard em T3.1 step 3:
  > Antes de classificar failure como "Fulcio gap", verificar workflow node `build-image-*` = Succeeded. Se não → failure é pre-sign (separate issue), abrir issue, NÃO loop back Phase 1.

## SHOULD TEST

### EC-9: Cleanup trap não roda em SIGKILL

- **Task afetada:** T0.1
- **Teste sugerido:** `test_diagnose_cleanup_on_sigterm` — fixture: spawn script, send SIGTERM mid-probe-2, assertar Fulcio --log-level removed + pods deleted no proximo `kubectl get`. bash trap EXIT roda em SIGTERM mas NÃO em SIGKILL — documentar limitação ou adicionar reconcile-on-startup que detecta orfã state.

### EC-10: Probe 2 jwks_uri externo (cloud LB URL) falha por timeout

- **Task afetada:** T0.1
- **Teste sugerido:** `test_probe2_external_jwks_uri` — fixture: discovery retorna `jwks_uri=https://prod-cluster.example.com/openid/v1/jwks`; script timeout 5s sem retry → verdict B errado (na verdade DNS+LB lento, não NetworkPolicy). Adicionar timeout maior + retry 2x antes de declarar B.

### EC-11: Fulcio CrashLoop pós-rollout (T2.2 sem timeout cap)

- **Task afetada:** T2.2
- **Teste sugerido:** N/A (operator manual) — adicionar acceptance criteria explícita em T2.2:
  > Operator DEVE confirmar `kubectl rollout status deploy/fulcio-server -n sigstore-system --timeout=5m` exit 0. Se CrashLoop, restaurar values anterior via `pulumi up --target` revert + abrir issue antes Phase 3.

### EC-12: cosign verify --certificate-oidc-issuer exato

- **Task afetada:** T3.1 step 5
- **Teste sugerido:** Script T3.1 acceptance step 5 specify issuer EXATO extraído de Phase 1 verdict.json: `cosign verify --certificate-oidc-issuer "$(jq -r .sa_token_iss /tmp/fulcio-diagnose-out/verdict.json)" <image>`. Se hardcoded errado, verify false-fail.

## DOCUMENT

### EC-13: Plan derivado v1.2 honesty — apenas Step 5+6 fechados

- **Task afetada:** T4.2
- **Risco aceito:** Per `.claude/rules/pre-release-governance.md` § Plan Amendment Policy: amendment NÃO pode silenciosamente marcar steps não-fechados. Plan derivado v1.1 lista 8 HARD STOPs; este plano fecha apenas Step 5 (Fulcio config) + Step 6 (Re-audit 3.85). Os outros 6 (3.9 cap → prod DNS) ficam HARD STOP. Documentar explicitamente em T4.2 amendment note: "v1.2 só fecha Step 5+6; demais 6 HARD STOPs deferred ao plano future 3.9 cap".

### EC-14: Aggregate SUMMARY baseline inconsistência (3.62 vs 3.65)

- **Task afetada:** T4.1
- **Risco aceito:** Audit 2026-05-26 = 3.62 canonical baseline; 2026-05-27-keyless-LIVE SUMMARY já bumped pra 3.65 com M07 3.80. Plan T4.1 calc faz "3.65 → 3.654" — preserva continuity. NÃO recalibrar baseline 3.62 (isso seria scope creep + violation Rule 11 confidence cap). Documentar em SUMMARY.md prelúdio: "rolling delta from 2026-05-27-keyless-LIVE 3.65; canonical baseline anchor remains 2026-05-26 3.62 per ADR D5 plan derivado".

### EC-15: CA ConfigMap mount staleness em K3s CA rotation (Cenário D apenas)

- **Task afetada:** T2.1 (Cenário D path)
- **Risco aceito:** Per CLAUDE.md "Live vs snapshot credentials" pattern + memory `full_green_validation_2026_05_25` (H13 CA rotation lesson): K3s pode rotacionar `kube-root-ca.crt`. ConfigMap mount via kubelet sync atualiza arquivo, mas Fulcio process cacheia trust store em-memória. Mitigation: pod restart on cm update. Documentar em sigstore.ts inline comment Cenário D: "// Trust store snapshot — Fulcio pod restart required on K3s CA rotation; Helm chart annotation `checksum/configmap-ca` triggers restart on ConfigMap change". Para now, accept risk (manual restart pelo operator se K3s upgrade detected).

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Não | Phase 2 wire é explicit via Pulumi up + Phase 3 LIVE proof |
| Correct code in wrong place | Não | Override em sigstore.ts (canonical) |
| Project name vs project ID | Não | N/A (não toca em builds via API) |
| ArgoCD notifiers not services | Não | N/A |
| Single ArgoCD App per tenant | Não | N/A (sigstore stack separado) |
| Live vs snapshot credentials | **Sim** | EC-15 — CA ConfigMap mount Cenário D |
| Bootstrap circular dependency | Não | Fulcio fetches via SA token, não bootstrap circular |
| Validation procedure proves nothing | **Sim parcial** | T2.2 step 4 valida CM existence mas não que Fulcio efetivamente usa — coberto por T3.1 LIVE proof, OK |
| CF scan imports conflicting apex | Não | N/A |

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 4 | 3 (EC-2, EC-3, EC-4) | 2 (EC-9, EC-10) | 0 |
| T0.2 | 1 | 1 (EC-7) | 0 | 0 |
| T0.3 | 0 | 0 | 0 | 0 |
| T1.1 | 0 | 0 | 0 | 0 |
| T2.1 | 2 | 2 (EC-5, EC-6) | 0 | 1 (EC-15) |
| T2.2 | 1 | 0 | 1 (EC-11) | 0 |
| T3.1 | 3 | 2 (EC-1, EC-8) | 1 (EC-12) | 0 |
| T4.1 | 1 | 0 | 0 | 1 (EC-14) |
| T4.2 | 1 | 0 | 0 | 1 (EC-13) |
| **Total** | **13** | **8** | **4** | **3** |

**Veredicto:** **PLANO PRECISA DE AJUSTE** — antes de iniciar implementação, absorver 8 MUST FIX (especialmente EC-1 que invalida proof de 3.85, e EC-5 que pode esconder "fix" inválido em chart contract).

### Priorização de absorção

1. **EC-1** (DB schema mismatch) — bloqueia acceptance criteria de 3.85; MUST corrigir antes de Phase 3
2. **EC-5** (Helm chart contract) — sem isso, fix pode ser silently dropped + 3 outros MUST FIX (EC-6, EC-8) mitigam outros caminhos de "false GREEN"
3. **EC-2, EC-3, EC-4** (script robustness) — produzem script confiável em mãos múltiplas (operator + drill replicas)
4. **EC-7** (mock coverage) — garante hermetic test pega regressions reais

Recomendação: bump plan v1.0 → v1.1 absorvendo os 8 MUST FIX antes de iniciar Phase 0 implementation.
