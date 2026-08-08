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
| `admission-policies` | mgmt, runtime | próprio | — | `sempre` |
| `argo-ci-namespace` | build | próprio | — | `sempre` |
| `argo-events` | build | próprio | — | `sempre` |
| `argo-events-bus` | build | próprio | — | `sempre` |
| `argo-rollouts` | runtime | próprio | — | `sempre` |
| `argo-workflows` | build | upstream | 1.0.5 | `sempre` |
| `argocd` | mgmt | upstream | 7.3.11 | `sempre` |
| `blackbox-exporter` | mgmt | upstream | 9.2.0 | `sempre` |
| `cert-manager` | mgmt, runtime | upstream | — | `sempre` |
| `chaos-mesh` | mgmt | próprio | — | `{{ .Values \| get "installChaosMesh" false }}` |
| `cloudflared` | runtime | próprio | — | `sempre` |
| `cluster-issuer` | runtime, vault | próprio | — | `sempre` |
| `external-secrets` | mgmt, runtime | upstream | 0.14.3 | `sempre` |
| `gitea` | mgmt | upstream | 10.6.0 | `sempre` |
| `harbor` | mgmt | upstream | 1.16.2 | `{{ ne .Environment.Name "drill" }}` |
| `k6-operator` | — | não declarado | — | `sempre` |
| `keda` | runtime | upstream | 2.16.1 | `sempre` |
| `keda-http-addon` | runtime | upstream | 0.8.0 | `{{ ne .Environment.Name "drill" }}` |
| `loki` | build | upstream | 6.24.0 | `sempre` |
| `minio` | mgmt | upstream | 5.4.0 | `sempre` |
| `monitoring` | mgmt, runtime | upstream | 3.12.1 | `{{ .Values \| get "installMetricsServer" false }}` |
| `network-policies` | runtime | próprio | — | `sempre` |
| `nfs-server-provisioner` | mgmt | upstream | 1.8.0 | `sempre` |
| `openbao-cert` | vault | próprio | — | `{{ .Values \| get "openbaoEnabled" false }}` |
| `openbao-init` | vault | próprio | — | `{{ .Values \| get "openbaoEnabled" false }}` |
| `openbao-snapshot-agent` | vault | próprio | — | `{{ .Values \| get "snapshotAgentEnabled" false }}` |
| `opencost` | mgmt | próprio | — | `sempre` |
| `ory-hydra` | mgmt | upstream | 0.61.0 | `sempre` |
| `ory-kratos` | mgmt | upstream | 0.61.0 | `sempre` |
| `otel` | mgmt | upstream | 0.96.0 | `sempre` |
| `policy-controller` | runtime | upstream | 0.8.1 | `sempre` |
| `postgresql` | mgmt | upstream | 18.6.2 | `{{ .Values.inClusterPostgres \| default false }}` |
| `promtail` | build, runtime | upstream | 6.16.6 | `sempre` |
| `redis` | mgmt | upstream | 19.6.4 | `{{ ne .Environment.Name "drill" }}` |
| `sigstore-extras` | — | não declarado | — | `sempre` |
| `smoke-test` | mgmt | próprio | — | `{{ and (ne .Environment.Name "drill") (ne .Environment.Name "dev-public") (ne (.Values \| get "clusterRole" "dev") "standalone") }}` |
| `static-assets` | runtime | próprio | — | `{{ ne .Environment.Name "drill" }}` |
| `tempo` | mgmt | upstream | 1.14.0 | `sempre` |
| `loopzero-api` | mgmt | próprio | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `loopzero-ci-workflows` | build | próprio | — | `sempre` |
| `loopzero-cloud` | mgmt | próprio | — | `{{ and (ne .Environment.Name "drill") (ne (.Values \| get "clusterRole" "dev") "standalone") }}` |
| `loopzero-external-secrets` | — | não declarado | — | `sempre` |
| `traefik` | mgmt, runtime | upstream | 28.3.0 | `sempre` |
| `velero` | mgmt | próprio | — | `sempre` |
<!-- END GENERATED TABLE -->

A data de geração fica fora da tabela por desenho: um gate de frescor responde
"o conteúdo divergiu da fonte?", não "isto foi gerado hoje?".
