---
type: reference
title: "Topologia Helm — o que roda em qual cluster"
---

# Topologia Helm

> **Documento gerado. Não edite à mão** — a próxima execução do gate sobrescreve.
> Fonte: `infra/helmfile/*.helmfile.yaml.gotmpl`. Regenerar:
> `loopzero-ops quality helm-topology-index --write`

Responde "em qual cluster este chart roda?" sem abrir os helmfiles. Cada linha vem da
declaração da release, não de convenção de pasta.

**Como ler as colunas**

- **Natureza** — vem da *forma de declaração*: `chart: ../helm/X` é chart nosso;
  chart remoto com `values:` local é configuração de upstream. `não declarado`
  é diretório que nenhum helmfile menciona — dívida ou engano, nunca omitido daqui.
- **Condicional** — o predicado `installed:` **literal**, não avaliado. `sempre`
  significa incondicional. Um predicado aqui quer dizer que a release existe em alguns
  ambientes e não em outros: `postgresql` roda no mgmt em standalone e não em prod.

**Escopo:** só `infra/helm/` (bootstrap via Helmfile L2). Os charts publicados em
`charts/`, consumidos por ArgoCD (L3), não estão aqui.

<!-- BEGIN GENERATED TABLE -->
| Chart | Cluster(s) | Natureza | Versão | Condicional |
|---|---|---|---|---|
| `admission-policies` | cell, core | próprio | — | `sempre` |
| `argo-ci-namespace` | build | próprio | — | `sempre` |
| `argo-events` | build | próprio | — | `sempre` |
| `argo-events-bus` | build | próprio | — | `sempre` |
| `argo-rollouts` | cell | próprio | — | `sempre` |
| `argo-workflows` | build | upstream | 1.0.5 | `sempre` |
| `argocd` | core | upstream | 7.3.11 | `sempre` |
| `blackbox-exporter` | core | upstream | 9.2.0 | `sempre` |
| `cert-manager` | cell, core | upstream | — | `sempre` |
| `chaos-mesh` | core | próprio | — | `{{ .Values \| get "installChaosMesh" false }}` |
| `cloudflared` | cell | próprio | — | `sempre` |
| `cluster-issuer` | cell, secrets | próprio | — | `sempre` |
| `external-secrets` | cell, core | upstream | 0.14.3 | `sempre` |
| `gitea` | core | upstream | 10.6.0 | `sempre` |
| `harbor` | core | upstream | 1.16.2 | `{{ ne .Environment.Name "drill" }}` |
| `k6-operator` | — | não declarado | — | `sempre` |
| `keda` | cell | upstream | 2.16.1 | `sempre` |
| `keda-http-addon` | cell | upstream | 0.8.0 | `{{ ne .Environment.Name "drill" }}` |
| `loki` | build | upstream | 6.24.0 | `sempre` |
| `minio` | core | upstream | 5.4.0 | `sempre` |
| `monitoring` | cell, core | upstream | 3.12.1 | `{{ .Values \| get "installMetricsServer" false }}` |
| `network-policies` | cell | próprio | — | `sempre` |
| `nfs-server-provisioner` | core | upstream | 1.8.0 | `sempre` |
| `openbao-cert` | secrets | próprio | — | `{{ .Values \| get "openbaoEnabled" false }}` |
| `openbao-init` | secrets | próprio | — | `{{ .Values \| get "openbaoEnabled" false }}` |
| `openbao-snapshot-agent` | secrets | próprio | — | `{{ .Values \| get "snapshotAgentEnabled" false }}` |
| `opencost` | core | próprio | — | `sempre` |
| `ory-hydra` | core | upstream | 0.61.0 | `sempre` |
| `ory-kratos` | core | upstream | 0.61.0 | `sempre` |
| `otel` | core | upstream | 0.96.0 | `sempre` |
| `policy-controller` | cell | upstream | 0.8.1 | `sempre` |
| `postgresql` | core | upstream | 18.6.2 | `{{ .Values.inClusterPostgres \| default false }}` |
| `promtail` | build, cell | upstream | 6.16.6 | `sempre` |
| `redis` | core | upstream | 19.6.4 | `{{ ne .Environment.Name "drill" }}` |
| `sigstore-extras` | — | não declarado | — | `sempre` |
| `smoke-test` | core | próprio | — | `{{ and (ne .Environment.Name "drill") (ne .Environment.Name "dev-public") (ne (.Values \| get "clusterRole" "dev") "standalone") }}` |
| `static-assets` | cell | próprio | — | `{{ ne .Environment.Name "drill" }}` |
| `tempo` | core | upstream | 1.14.0 | `sempre` |
| `loopzero-api` | core | próprio | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `loopzero-ci-workflows` | build | próprio | — | `sempre` |
| `loopzero-cloud` | core | próprio | — | `{{ and (ne .Environment.Name "drill") (ne (.Values \| get "clusterRole" "dev") "standalone") }}` |
| `loopzero-external-secrets` | — | não declarado | — | `sempre` |
| `traefik` | cell, core | upstream | 28.3.0 | `sempre` |
| `velero` | core | próprio | — | `sempre` |
<!-- END GENERATED TABLE -->

A data de geração fica fora da tabela por desenho: um gate de frescor responde
"o conteúdo divergiu da fonte?", não "isto foi gerado hoje?".
