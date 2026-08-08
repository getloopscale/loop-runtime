---
type: plan
title: "Edge Case Review — 2026-05-25-live-ca-fetch-clustercredential"
status: deprecated
---

# Edge Case Review — 2026-05-25-live-ca-fetch-clustercredential

**Data:** 2026-05-25
**Plano:** [docs/plans/2026-05-25-live-ca-fetch-clustercredential-plan.md](./2026-05-25-live-ca-fetch-clustercredential-plan.md)
**Tasks analisadas:** 7 (T0.1, T0.2, T1.1, T2.1, T3.1, T4.1, T5.1, T6.1)
**Edge cases encontrados:** 7 (MUST FIX: 2, SHOULD TEST: 3, DOCUMENT: 2)

> Análise complementar à seção "Edge Case Review Notes" inline no plano (EC-1 a EC-12). Esta passagem independente focou em **fronteiras de I/O e procedimentos** — não revisitei os EC já cobertos inline. Achou 2 MUST FIX que invalidam suposições do plano.

---

## MUST FIX

### EC-13: Recursão x509 — fetcher usa caData stale do kubeconfig para fetch da CA fresh

- **Task afetada:** T1.1 (`kubeRootCAFetcher.FetchClusterCA`) + T2.1 (wire)
- **Família:** I/O + Bootstrap circular dependency
- **Cenário:** o fetcher recebe `restConfig` derivado de `clientcmd.RESTConfigFromKubeConfig(kubeconfigData)`. Esse `restConfig.CAData` é o **mesmo snapshot stale** que Op (B) tenta consertar. Quando k3s rotaciona a server CA do cluster alvo:
  1. Fetcher chama `kubernetes.NewForConfig(restConfig)` → client com CAData antigo
  2. Client chama `GET /api/v1/namespaces/default/configmaps/kube-root-ca.crt` via TLS
  3. TLS handshake **falha** porque server cert é assinado pela CA nova; client valida contra CA velha → `x509: certificate signed by unknown authority`
  4. Fetcher retorna error → D2 fallback ativa → caData = snapshot stale (mesmo state de antes)
  5. Op (B) é **no-op nesse cenário exato** que mais importa
- **Impacto:** o cenário primário que justifica Op (B) (CA rotation) é o cenário em que Op (B) silenciosamente não funciona. O fallback mascara a falha — counter incrementa `result="fallback"`, ArgoCD continua com CA stale, x509 errors persistem. SRE pensa que Op (B) está ativo (counter > 0) mas é só fallback.
- **Fix sugerido:** no fetcher, clonar `restConfig` setando `restConfig.TLSClientConfig.Insecure = true` + zerar `CAData`/`CertData` SOMENTE para esta chamada GET. ConfigMap content (`kube-root-ca.crt`) é público (RBAC `system:public-info-viewer` permite leitura anônima na maioria dos clusters) e o seu valor é a CA propriamente dita — não há ganho de segurança em verificar TLS na chamada que busca a própria CA. O resultado retornado é a CA pública correta, que ArgoCD então usa para verificação subsequente.
  ```go
  cfg := rest.CopyConfig(restConfig)
  cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}  // bootstrap-only
  cs, err := kubernetes.NewForConfig(cfg)
  ```
  Atualizar ADR D1 explicando o trade-off "insecure-fetch para descobrir CA, secure-thereafter".

---

### EC-14: Phase 4 step 7 simulation de CA rotation não rotaciona CA

- **Task afetada:** T4.1 step 7
- **Família:** Procedimento de validação incorreto
- **Cenário:** o plano instrui `docker restart k3d-loopzero-workload-server-0` como simulação de rotação de CA. **k3s NÃO regenera server CA em restart** — a CA vive em `/var/lib/rancher/k3s/server/tls/server-ca.{crt,key}` que é persistido em docker volume e preservado entre restarts. K3s só regenera CA quando:
  - Esses arquivos são deletados antes do start (k3s recria no boot)
  - Cluster é recriado do zero (`k3d cluster delete` + `create`)
- **Impacto:** step 7 retorna PEM idêntico ao snapshot → diff = 0 sem provar nada. Operator marca evidência como [MEASURED] passando, mas a CA nunca rotacionou — falso GREEN. Op (B) pode ter regressão silenciosa que nenhum teste pega.
- **Fix sugerido:** trocar o procedimento step 7 por:
  ```bash
  # Force CA regeneration on k3s server
  docker exec k3d-loopzero-workload-server-0 sh -c \
    'rm -f /var/lib/rancher/k3s/server/tls/server-ca.crt /var/lib/rancher/k3s/server/tls/server-ca.key'
  docker restart k3d-loopzero-workload-server-0
  # k3s recreates CA on boot; wait for API server ready
  ```
  E adicionar pre/post diff de CA via `kubectl --context k3d-loopzero-workload get cm kube-root-ca.crt -n default -o jsonpath='{.data.ca\.crt}' | sha256sum` antes/depois — assertion: hashes DIFERENTES.

---

## SHOULD TEST

### EC-15: `restConfig == nil` (DI bug em test setup)

- **Task afetada:** T1.1
- **Teste sugerido:** `TestKubeRootCAFetcher_NilRestConfig` — `FetchClusterCA(ctx, nil)` retorna erro (não panic). Cobre regressão se algum teste injetar fake malformado. 1-liner: `if restConfig == nil { return nil, errors.New("...") }` no início.

### EC-16: PEM com BOM (UTF-8 byte order mark prepended)

- **Task afetada:** T1.1
- **Teste sugerido:** `TestKubeRootCAFetcher_PEMWithBOM` — algumas pipelines de geração de cert produzem `﻿-----BEGIN...`. Atual check `strings.HasPrefix(pem, "-----BEGIN")` retorna false → fallback. Fix de uma linha: `pem = strings.TrimPrefix(pem, "﻿")` antes do check. Worth testing porque silent fallback mascara o problema.

### EC-17: envtest control-plane cert chain drift entre versões

- **Task afetada:** T3.1 spec 1 ("live CA fetched from kube-root-ca.crt overrides kubeconfig snapshot caData")
- **Teste sugerido:** `TestPlatform_OpB_AssertsLiveCASourceNotMatchesSnapshot` — verifica que a CA fetched DIFERE da CA snapshot (não que iguala um valor fixo). Asserção robusta contra envtest version bumps. Linha: `g.Expect(decoded).NotTo(Equal(string(restConfig.CAData)))`.

---

## DOCUMENT

### EC-18: ArgoCD app-controller informer resync latency (5min)

- **Risco aceito:** quando Op (B) atualiza Secret com nova CA, app-controller pode demorar até 5min para re-observar a mudança via informer resync (controlled by `--app-resync` flag, default 180s na maioria dos deployments, 300s em alguns Helm charts). Durante essa janela, app-controller usa CA antiga em cache → x509 ainda manifesta. Plano hoje não menciona essa janela.
- **Por que é OK:** janela máxima 5min vs cenário atual de "permanente até H19 manual". Melhoria substancial mesmo com latência. Pode-se reduzir com `kubectl rollout restart statefulset/argocd-application-controller` no fim do reconcile (como H19 fazia), mas isso é restart agressivo demais para reconcile periódico — aceito como degradado consciente.

### EC-19: 7-day soak gate sem automation (operator discipline)

- **Risco aceito:** Phase 5 depende do operator esperar 7 dias e validar zero CA-related incidents antes de deletar H19/H19a. Sem cron job ou checklist automatizado, esquecimento humano é provável. Plano hoje só menciona "verify 7-day soak completion (operator)" sem mecanismo.
- **Por que é OK:** soak prematuro pior caso = H19/H19a deletados sem proof + bug regride após release → operator re-adiciona via revert. Aceitável porque Phase 5 é low-risk (delete dead code) — vale o risco menor do operator empurrar a janela versus criar infra de cron job só pra isso. **Mitigação leve:** adicionar T5.0 pre-flight check no Phase 5 que faz `grep -c "result=\"fallback\"" Prometheus_export_dump` over last 7 days — deve ser 0.

---

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Não (Phase 1 + Phase 2 cobertas) | — |
| Correct code in wrong place | Não | — |
| Project name vs ID | N/A (operator-side) | — |
| ArgoCD notifiers not services | N/A | — |
| Single ArgoCD App per tenant | N/A | — |
| CF scan imports conflicting apex | N/A | — |
| **Bootstrap circular dependency (CA-fetches-CA)** | **SIM (NEW)** | T1.1 + T2.1 (EC-13) |
| **Validation procedure proves nothing** | **SIM (NEW)** | T4.1 step 7 (EC-14) |

> 2 padrões NOVOS detectados que merecem entrar em CLAUDE.md Known Systemic Patterns após este plano shipar.

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 (arch docs) | 0 | 0 | 0 | 0 |
| T0.2 (governance) | 0 | 0 | 0 | 0 |
| T1.1 (caFetcher) | 4 | 1 (EC-13) | 2 (EC-15, EC-16) | 0 |
| T2.1 (wire) | 1 | 1 (EC-13 mesma raiz) | 0 | 1 (EC-18) |
| T3.1 (envtest) | 1 | 0 | 1 (EC-17) | 0 |
| T4.1 (LIVE) | 1 | 1 (EC-14) | 0 | 0 |
| T5.1 (heal cleanup) | 1 | 0 | 0 | 1 (EC-19) |
| T6.1 (docs) | 0 | 0 | 0 | 0 |

**Veredicto: PLANO PRECISA DE AJUSTE**

EC-13 (recursão x509) é crítico — invalida silenciosamente o cenário primário que motiva Op (B). EC-14 (simulação incorreta de CA rotation) é crítico porque transforma evidence LIVE em falso GREEN. Ambos precisam ser incorporados ao plano ANTES de T1.1 implementation começar.

### Ações requeridas antes de Phase 1

1. **Editar T1.1**:
   - Atualizar Deep Dive de `kubeRootCAFetcher.FetchClusterCA` adicionando o clone com `Insecure: true` (EC-13 fix).
   - Adicionar `TestKubeRootCAFetcher_NilRestConfig` (EC-15) à lista RED.
   - Adicionar `TestKubeRootCAFetcher_PEMWithBOM` (EC-16) à lista RED.
   - Atualizar ADR D1 com nota sobre "insecure-fetch bootstrap".

2. **Editar T3.1 spec 1**:
   - Mudar asserção de "iguala envtest CA" para "DIFERE da snapshot CA" (EC-17).

3. **Editar T4.1 step 7**:
   - Substituir `docker restart` por procedimento que deleta `server-ca.{crt,key}` ANTES do restart (EC-14).
   - Adicionar pre/post hash verification para provar que CA realmente rotacionou.

4. **Editar T2.1**:
   - Adicionar nota em Deep Dive sobre EC-18 (5min informer latency aceita).

5. **Adicionar T5.0**:
   - Pre-flight check para Phase 5 que valida `result="fallback"` counter == 0 over last 7 days (EC-19 mitigation).

6. **Atualizar Pre-Approval Checklist**:
   - Adicionar linha "EC-13 + EC-14 incorporados no plano antes de T1.1".
