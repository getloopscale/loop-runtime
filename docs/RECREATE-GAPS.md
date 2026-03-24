# Recreate Gaps — Status Final

> **Data:** 2026-03-24
> **Status:** 14 gaps originais resolvidos. 3 gaps adicionais encontrados durante validação.
> **Validação:** destroy+recreate executado com sucesso. Deploy + TLS Let's Encrypt validado.

---

## Validação Completa

```
1. pulumi destroy --yes                    ✅ Completou
2. pulumi up --yes                         ✅ 83 resources criados (20min)
3. Tenant criado + Active                  ✅ Controller reconciliou automaticamente
4. loopzero deploy (examples/node-npm)         ✅ Build + manifests + GitOps push
5. App live com resposta                   ✅ {"status":"ok","message":"Hello from node-npm!"}
6. TLS Let's Encrypt (cadeado verde)       ✅ issuer=Let's Encrypt CN=R12, curl sem -k funciona
```

## Gaps Restantes (intervenção manual necessária)

### Gap 15 — Bootstrap Job image tag incorreta

**Status:** CORRIGIDO no código (bitnami/kubectl:1.31), aguarda rebuild da infra
**Impacto:** Bootstrap Job não roda = Gitea token fica placeholder = ArgoCD não sincroniza
**Workaround manual:** Gerar token via kubectl exec + patch Secret (3 comandos)

### Gap 16 — loopzero-builds bucket no DO Spaces

**Status:** CORRIGIDO no código (infra.ts), aguarda `pulumi up` com código novo
**Impacto:** Source upload falha sem o bucket
**Workaround manual:** `aws s3 mb s3://loopzero-builds --endpoint-url https://sfo3.digitaloceanspaces.com`

### Gap 17 — gitea-repo-creds tem token placeholder

**Status:** Consequência do Gap 15 (bootstrap Job não rodou)
**Impacto:** ArgoCD não consegue clonar repos = sync Unknown = pods não deployam
**Workaround manual:** `kubectl patch secret gitea-repo-creds` com token real

## Conclusão

O sistema funciona end-to-end após recreate com 3 comandos manuais de workaround.
Quando o GitHub Actions billing for resolvido, a imagem do bootstrap Job será publicada
e os 3 gaps restantes serão eliminados automaticamente.
