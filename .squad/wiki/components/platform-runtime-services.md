---
type: Component
title: "Platform runtime services"
description: "Ingress, certificates, secret sync, admission policy, observability, backup and identity — the nineteen releases that are neither datastore nor orchestrator."
resource: "infra/helm"
tags:
  - traefik
  - cert-manager
  - eso
  - observability
  - ory
sources:
  - id: pr-traefik
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L121"
    note: "Traefik (ingress / edge)"
  - id: pr-cm
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L145-L150"
    note: "cert-manager"
  - id: pr-cf
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L247-L249"
    note: "cloudflared (tunnel)"
  - id: pr-ci
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L257-L259"
    note: "ClusterIssuer (ACME / self-signed)"
  - id: pr-eso
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L701-L704"
    note: "External Secrets Operator"
  - id: pr-teso
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L701-L704"
    note: "loopzero-external-secrets (ExternalSecret set)"
  - id: pr-pol
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L179-L185"
    note: "sigstore policy-controller"
  - id: pr-adm
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L192-L194"
    note: "admission-policies (ValidatingAdmissionPolicies)"
  - id: pr-static
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L277-L279"
    note: "static-assets"
  - id: pr-obs
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L369"
    note: "Prometheus + Grafana + Tempo + OTel collector"
  - id: pr-vel
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L332"
    note: "Velero (cluster backup)"
  - id: pr-kratos
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L465-L470"
    note: "Ory Kratos (identity)"
  - id: pr-le
    resource: "infra/helm/cluster-issuer/values.yaml#L7-L10"
    note: "acme.server: https://acme-v02.api.letsencrypt.org/directory"
  - id: pr-tunnel
    resource: "infra/helm/cloudflared/values.yaml#L4-L11"
    note: "outbound tunnel authenticated with the cloudflared-tunnel-token Secret"
  - id: pr-bao
    resource: "infra/helm/loopzero-external-secrets/values.yaml#L34-L75"
    note: "the ESO -> loopzero-api broker -> OpenBao transit path named in the chart header"
  - id: pr-pt
    resource: "infra/helm/loopzero-external-secrets/templates/externalsecrets.yaml#L46"
    note: "HEAD commit claims the ExternalSecret comments were translated; line 46 is still Portuguese"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Everything the platform runs that holds no durable state of its own and orchestrates nothing:
ingress and TLS, secret delivery, admission control, telemetry, backup, and the identity stack.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `infra.traefik` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:121` | Helmfile release 'traefik', chart traefik/traefik 28.3.0, installed on BOTH the core cluster and every workload cell. The k3d cell definitions map host 8090/8443 onto it, so this is where user HTTP traffic enters. |
| `infra.cert_manager` | `infra/helmfile/core.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:145-150` | Helmfile release 'cert-manager', chart jetstack/cert-manager v1.14.5 on core and cell; v1.15.3 on the secrets cluster.  [merged from duplicate infra.certmanager: Declared in core.helmfile (145), cell.helmfile (99) and secrets.helmfile (72).] |
| `infra.cloudflared` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:247-249` | Helmfile release 'cloudflared', in-repo chart. Holds an outbound tunnel to the Cloudflare edge using a token from the cloudflared-tunnel-token Secret. |
| `infra.cluster_issuer` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:257-259` | Helmfile release 'cluster-issuer', in-repo chart. Emits either a self-signed issuer or an ACME issuer pointed at the configured directory URL. |
| `infra.external_secrets` | `infra/helmfile/core.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:701-704` | Helmfile release 'external-secrets', chart external-secrets/external-secrets 0.14.3, installed on core and on every cell.  [merged from duplicate infra.eso: Reconciles the ExternalSecret CRs api/internal/k8s/secretsync writes on the runtime cluster.] |
| `infra.loopzero_external_secrets` | `infra/helmfile/core.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:701-704` | In-repo chart infra/helm/loopzero-external-secrets declaring the ExternalSecret objects that materialise the platform's K8s Secrets (loopzero-api-tokens, loopzero-api-minio, loopzero-api-internal, ...). |
| `infra.policy_controller` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:179-185` | Helmfile release 'policy-controller', chart sigstore/policy-controller 0.8.1. Admission-time enforcement that workload images carry a valid cosign signature. |
| `infra.admission_policies` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:192-194` | Helmfile release 'admission-policies', in-repo chart ../helm/admission-policies, installed on the cells; a second release 'admission-policies-mgmt' installs it on core. |
| `infra.static_assets` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:277-279` | Helmfile release 'static-assets', in-repo chart on the workload cells; the static-site serving path that the Cloudflare Worker fronts. |
| `infra.grafana` | `infra/helmfile/core.helmfile.yaml.gotmpl` | runtime | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:380-383` | Helmfile release 'grafana', chart grafana/grafana 8.5.2. |
| `infra.otel_collector` | `infra/helmfile/core.helmfile.yaml.gotmpl` | stream | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:411-414` | Helmfile release 'otel-collector', chart open-telemetry/opentelemetry-collector 0.96.0. The telemetry pipeline that fans spans and metrics out to Tempo and Prometheus. |
| `infra.promtail` | `infra/helmfile/build.helmfile.yaml.gotmpl` | stream | l2-platform | `infra/helmfile/build.helmfile.yaml.gotmpl:164-167` | Helmfile release 'promtail', chart grafana/promtail 6.16.6, declared on both the build cluster and the workload cells; ships into Loki. |
| `infra.observability` | `infra/helmfile/core.helmfile.yaml.gotmpl` | service | infrastructure | `infra/helmfile/core.helmfile.yaml.gotmpl:369` | Observability stack in the monitoring namespace: prometheus 25.27.0, grafana 8.5.2, tempo 1.14.0, otel-collector 0.96.0, blackbox-exporter 9.2.0, opencost. |
| `infra.velero` | `infra/helmfile/core.helmfile.yaml.gotmpl` | batch_job | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:332` | Helmfile release 'velero', in-repo wrapper chart ../helm/velero with a values-schedules.yaml. Its backup storage location bucket/region/s3Url are set from environment values. |
| `infra.smoke_test` | `infra/helmfile/core.helmfile.yaml.gotmpl` | batch_job | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:722-741` | Helmfile release 'smoke-test', in-repo chart ../helm/smoke-test. Skipped in drill, dev-public and standalone. |
| `infra.openbao_snapshot` | `infra/helmfile/secrets.helmfile.yaml.gotmpl` | batch_job | l2-platform | `infra/helmfile/secrets.helmfile.yaml.gotmpl:383-386` | Helmfile release 'openbao-snapshot-agent', gated on `snapshotAgentEnabled` — a DIFFERENT flag from the three releases gated on `openbaoEnabled`, and also default false. |
| `infra.ory_hydra` | `infra/helmfile/core.helmfile.yaml.gotmpl` | service | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:480-485` | Helmfile release 'ory-hydra', chart ory/hydra 0.61.0, namespace loopzero-auth. |
| `infra.ory_kratos` | `infra/helmfile/core.helmfile.yaml.gotmpl` | service | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:465-470` | Helmfile release 'ory-kratos', chart ory/kratos 0.61.0, namespace loopzero-auth. |
| `ext.ory` | `infra/helmfile/core.helmfile.yaml.gotmpl` | external | infrastructure | `infra/helmfile/core.helmfile.yaml.gotmpl:465` | Two helmfile releases in the loopzero-auth namespace: ory-kratos (line 465, needs postgresql in the standalone profile) and ory-hydra (line 480, needs loopzero-auth/ory-kratos). Third-party, deployed here but NOT built here. Grepped api/internal for hydra/kratos: the only non-generated hit is api/internal/config/config.go — the api module does not embed an Ory client. |

# Relationships

- **Traefik** is the ingress on every runtime cell, on API group `traefik.io/v1alpha1`. The
  manifest renderer emits a `TraefikService` beside each Rollout for canary weighting.
- **cert-manager + cluster-issuer** obtain certificates from Let's Encrypt over
  ACME.[^pr-le] For Cloudflare-managed domains the Certificate CR is **skipped** — CF handles
  TLS, and creating one anyway is a documented mistake.
- **cloudflared** opens an outbound tunnel authenticated by the `cloudflared-tunnel-token`
  Secret;[^pr-tunnel] **static-assets** serves tenant static content through the Cloudflare
  Worker, which reads from DigitalOcean Spaces.
- **ESO + loopzero-external-secrets** are the secret delivery path: the chart declares
  `ExternalSecret` objects, ESO reconciles them, and the broker path reaches OpenBao
  transit.[^pr-bao] The API's own write into a workload namespace is best-effort and its failure
  is logged rather than returned — trap one of
  [`/flows/developer-first-app-live.md`](/flows/developer-first-app-live.md).
- **policy-controller + admission-policies** enforce ValidatingAdmissionPolicies on each cell.
  Two CEL traps are recorded in the repository: `object` is not populated on `DELETE` (use
  `oldObject` or `request.*`), and a negated `matchCondition` over an **empty** exemption list is
  always true, which denies everything including the operators.
- **Velero** backs up to MinIO in standalone and to a Spaces bucket in the cloud profiles.
- **Ory Kratos + Hydra** are the identity stack; Hydra `needs` Kratos. Kratos reads its config
  **only at process start** — there is no SIGHUP, so a ConfigMap change requires
  `kubectl rollout restart statefulset ory-kratos-courier`, and the courier is the StatefulSet
  rather than the Deployment.
- The observability releases feed the datastores in
  [`/components/platform-datastores.md`](/components/platform-datastores.md); the API exposes its
  own metrics through [`/components/metering-and-billing.md`](/components/metering-and-billing.md).

## Where the code and the documents disagree

`loopzero-external-secrets/templates/externalsecrets.yaml:46` is still in Portuguese, against the
repository's own English-only rule — and the **last commit to touch that file** is `6c22c4ecf`,
titled *"chore(helm): translate the ExternalSecret template comments to English"*.[^pr-pt] The
commit message asserts the fix that the file shows was not completed, which is strictly worse
than an untranslated comment: a reader checking the history concludes the work is done.

It is one instance of a wider measurement — **331 tracked files carry Portuguese**, including Go
error values and five package doc comments that make `go doc` render Portuguese. The full
finding is in [`/operations/contradictions.md`](/operations/contradictions.md).

# Evidence

[^pr-traefik]: `infra/helmfile/cell.helmfile.yaml.gotmpl:121`
[^pr-cm]: `infra/helmfile/core.helmfile.yaml.gotmpl:145-150`
[^pr-cf]: `infra/helmfile/cell.helmfile.yaml.gotmpl:247-249`
[^pr-ci]: `infra/helmfile/cell.helmfile.yaml.gotmpl:257-259`
[^pr-eso]: `infra/helmfile/core.helmfile.yaml.gotmpl:701-704`
[^pr-teso]: `infra/helmfile/core.helmfile.yaml.gotmpl:701-704`
[^pr-pol]: `infra/helmfile/cell.helmfile.yaml.gotmpl:179-185`
[^pr-adm]: `infra/helmfile/cell.helmfile.yaml.gotmpl:192-194`
[^pr-static]: `infra/helmfile/cell.helmfile.yaml.gotmpl:277-279`
[^pr-obs]: `infra/helmfile/core.helmfile.yaml.gotmpl:369`
[^pr-vel]: `infra/helmfile/core.helmfile.yaml.gotmpl:332`
[^pr-kratos]: `infra/helmfile/core.helmfile.yaml.gotmpl:465-470`
[^pr-le]: `infra/helm/cluster-issuer/values.yaml:7-10`
[^pr-tunnel]: `infra/helm/cloudflared/values.yaml:4-11`
[^pr-bao]: `infra/helm/loopzero-external-secrets/values.yaml:34-75`
[^pr-pt]: `infra/helm/loopzero-external-secrets/templates/externalsecrets.yaml:46`
