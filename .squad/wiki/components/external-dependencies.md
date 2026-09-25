---
type: Component
title: "Third-party systems the platform depends on"
description: "Ten external surfaces, each reached from exactly one place in the code."
resource: "api/internal"
tags:
  - external
  - third-party
  - integration
sources:
  - id: xd-cf
    resource: "infra/cloudflare/worker.mjs#L1-L2"
    note: "Cloudflare (Workers, KV, Tunnel, DNS/TLS)"
  - id: xd-kv
    resource: "api/internal/cfkv/client.go#L63"
    note: "Cloudflare Workers KV"
  - id: xd-zone
    resource: "api/internal/domains/cfzone/client.go#L19"
    note: "Cloudflare Zone Management API"
  - id: xd-do
    resource: "api/internal/s3/uploader.go#L29"
    note: "DigitalOcean Spaces (S3-compatible)"
  - id: xd-gh
    resource: "api/internal/github/appclient.go#L32"
    note: "GitHub (App + REST API)"
  - id: xd-k8s
    resource: "api/internal/k8s/doc.go#L3-L6"
    note: "Kubernetes API server(s)"
  - id: xd-le
    resource: "infra/helm/cluster-issuer/values.yaml#L7-L10"
    note: "Let's Encrypt (ACME)"
  - id: xd-sig
    resource: "api/internal/buildplane/argo/cosign.go#L57-L67"
    note: "Sigstore (Fulcio + Rekor)"
  - id: xd-stripe
    resource: "api/internal/server/router.go#L608"
    note: "Stripe (billing)"
  - id: xd-helm
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L82-L127"
    note: "Upstream Helm chart repositories"
  - id: xd-worker
    resource: "infra/cloudflare/worker.mjs#L1-L2"
    note: "S3_ENDPOINT = \"https://loopzero-static-assets.sfo3.digitaloceanspaces.com\""
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The systems outside the cluster boundary. Every one of them is reached from a single package,
which is what makes the boundary auditable.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `ext.cloudflare` | `infra/cloudflare/worker.mjs` | external | external | `infra/cloudflare/worker.mjs:1-2` | Edge in front of tenant static content. infra/cloudflare/worker.mjs is a Worker that proxies from a DigitalOcean Spaces origin and stamps the security headers; infra/helm/cloudflared holds an outbound tunnel into the cells. |
| `ext.cloudflare.kv` | `api/internal/cfkv/client.go` | external | external | `api/internal/cfkv/client.go:63` | Routing entries for the static-site path; a CF Worker reads them to serve assets from DO Spaces. Three call sites in client.go build this same URL (lines 63, 118, 150) for put/get/delete. |
| `ext.cloudflare.zone` | `api/internal/domains/cfzone/client.go` | external | external | `api/internal/domains/cfzone/client.go:19` | Per-domain DNS zone creation for custom apex domains. The cfzone package doc records the flow: loopzero domain add -> CF API creates zone -> user changes nameservers at registrar -> CF activates -> CNAME apex flattened. |
| `ext.dospaces` | `api/internal/s3/uploader.go` | external | external | `api/internal/s3/uploader.go:29` | S3-compatible object storage for static assets. uploader.go:85 pins region us-east-1 because DO Spaces uses it for SigV4. |
| `ext.github` | `api/internal/github/appclient.go` | external | external | `api/internal/github/appclient.go:32` | GitHub App: JWT + installation-token auth, source tarball download, commit status, PR comments. Base URL is overridable via GITHUB_API_BASE_URL so GitHub Enterprise Server works. |
| `ext.k8sapi` | `api/internal/k8s` | external | external | `api/internal/k8s/doc.go:3-6` | Every cell's kube-apiserver. Reached from the api module (k8s, k8s/tenancy, k8s/workload, k8s/secretsync, cellmgr, placement, metering) and from all four operator binaries. A network hop into another cluster in the multi-cell case. |
| `ext.letsencrypt` | `infra/helm/cluster-issuer/values.yaml` | external | external | `infra/helm/cluster-issuer/values.yaml:7-10` | ACME certificate authority at https://acme-v02.api.letsencrypt.org/directory, consumed by cert-manager through the cluster-issuer chart when issuerKind=acme. |
| `ext.sigstore` | `api/internal/buildplane/argo/cosign.go` | external | external | `api/internal/buildplane/argo/cosign.go:57-67` | Supply chain signing. cosign.go documents two modes: key-based by default (a cosign-signing-key Secret mounts cosign.key + cosign.password), or keyless where cosign reads SIGSTORE_ID_TOKEN and Fulcio issues the certificate. sigstore-extras and policy-controller are separate Helm releases. |
| `ext.stripe` | `charts/loopzero-cloud` | external | external | `api/internal/server/router.go:608` | Billing provider, reached from two places. At runtime the control-plane API routes billing calls (api/internal/server/router.go); at deploy time charts/loopzero-cloud declares STRIPE_API_KEY and STRIPE_WEBHOOK_SECRET as required production secrets and its Deployment references the loopzero-stripe-webhook Secret, while charts/loopzero-api declares stripeSecretKey. The billing implementation itself was extracted to the sibling repo ../loopzero-cloud at M4. |
| `ext.helm_repositories` | `infra/helmfile/core.helmfile.yaml.gotmpl` | external | external | `infra/helmfile/core.helmfile.yaml.gotmpl:82-127` | Build/deploy-time dependency on 15 third-party chart repositories (jetstack, traefik, argoproj, gitea-charts, minio, prometheus-community, grafana, open-telemetry, ory, harbor, nfs-ganesha, metrics-server, bitnami, chaos-mesh, external-secrets, opencost, vmware-tanzu) plus sigstore and kedacore on the cells and openbao on the secrets cluster. |

# Relationships

| External system | Reached from | Relationship |
|---|---|---|
| Cloudflare KV | `api/internal/cfkv` | HTTP PUT of static routing entries, written **before** container activation |
| Cloudflare Zone API | `api/internal/domains/cfzone` | zone create/scan; v2 domains work by nameserver delegation |
| Cloudflare Worker | `infra/cloudflare/worker.mjs` | serves static content, reading from DO Spaces[^xd-worker] |
| DigitalOcean Spaces | `api/internal/s3` | S3 `PutObject` for source bundles and static assets |
| GitHub | `api/internal/github` | App API, base URL overridable |
| Kubernetes API | `api/internal/k8s`, all four operators | client-go / controller-runtime, per cell |
| Let's Encrypt | `infra/helm/cluster-issuer` | ACME over `acme-v02.api.letsencrypt.org` |
| Sigstore | the generated cosign workflow step | Fulcio/Rekor signing; the digest pins the manifests |
| Stripe | `charts/loopzero-cloud` secret contract only | the code that calls Stripe is in `../loopzero-cloud` |
| Upstream Helm repos | `infra/helmfile` | every pinned chart is pulled at sync time |

The clients are documented in
[`/components/external-integrations.md`](/components/external-integrations.md); the runtime
releases that hold the credentials are in
[`/components/platform-runtime-services.md`](/components/platform-runtime-services.md).

## Two of these are not in this repository

`ext.stripe` appears only as a **secret contract** in a chart: `STRIPE_API_KEY` and
`STRIPE_WEBHOOK_SECRET` declared required for production. The checkout session, the webhook
handler and the subscription lifecycle live in the extracted `../loopzero-cloud` repository.
Likewise the whole developer-facing CLI lives in `../loopzero-cli`. Looking for either here is a
dead end by design.

# Evidence

[^xd-cf]: `infra/cloudflare/worker.mjs:1-2`
[^xd-kv]: `api/internal/cfkv/client.go:63`
[^xd-zone]: `api/internal/domains/cfzone/client.go:19`
[^xd-do]: `api/internal/s3/uploader.go:29`
[^xd-gh]: `api/internal/github/appclient.go:32`
[^xd-k8s]: `api/internal/k8s/doc.go:3-6`
[^xd-le]: `infra/helm/cluster-issuer/values.yaml:7-10`
[^xd-sig]: `api/internal/buildplane/argo/cosign.go:57-67`
[^xd-stripe]: `api/internal/server/router.go:608`
[^xd-helm]: `infra/helmfile/core.helmfile.yaml.gotmpl:82-127`
[^xd-worker]: `infra/cloudflare/worker.mjs:1-2`
