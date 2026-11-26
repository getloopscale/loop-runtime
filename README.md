# LoopRuntime

The managed runtime for AI agents and the apps around them.

You describe your services in a `loopzero.yaml` file and push to Git. LoopRuntime builds each
service, signs the image, writes the Kubernetes manifests to a GitOps repository and rolls the
release out. API servers, background workers and static frontends from the same monorepo deploy
together.

This repository is the engine: the HTTP API, the Kubernetes operators, the Helm charts and the
infrastructure that runs them.

## How a deploy works

```
git push
  -> the API validates loopzero.yaml and locks the environment
  -> Argo Workflows builds each app with rootless BuildKit and signs it with cosign
  -> the API writes the manifests to Gitea
  -> Argo CD syncs the manifests and the app starts
  -> the client receives the URL over server-sent events
```

A `loopzero.yaml` at the repository root lists the apps a push deploys. Each app needs a
`runtime` (`node` or `python`) and the `start` command that runs it; the platform writes
the Dockerfile:

```yaml
project: my-saas

apps:
  api:
    path: api
    runtime: python
    start: python -m app
    port: 8000

  web:
    path: web
    runtime: node
    start: node server.js
    port: 3000
```

## What is in this repository

| Path | Contents |
|---|---|
| `api/` | The HTTP API in Go: projects, environments, builds, releases, secrets, domains, previews, billing and metering. The OpenAPI description is in `api/openapi/`. |
| `operators/` | Four Kubernetes operators built with controller-runtime: tenant, app, preview and platform. |
| `pkg/` | Go libraries shared by the API and the operators: typed errors, logging, naming, conditions, state machines, circuit breaker. |
| `cmd/loop-runtime-ops/` | The operations and quality-gate binary used by the task runner. |
| `charts/` | Twelve Helm charts for the API, the operators, the CRDs, the dashboard, monitoring and admission policies. |
| `infra/` | Helmfile releases, bootstrap Helm charts, k3d cluster definitions, Tilt setup and the remaining Pulumi projects. |
| `policies/`, `rules/` | Admission policies and the rules the quality gates check. |

The operators manage eight custom resources in the `runtime.getloopscale.com` API group: `Tenant`,
`Environment`, `LoopRuntimeApplication`, `PreviewEnvironment`, `ExposedRoute`,
`GitRepositoryBinding`, `ClusterCredential` and `PlatformBootstrap`.

## Stack

| Layer | Technology |
|---|---|
| API | Go 1.26, chi, pgx, PostgreSQL |
| Operators | Go, kubebuilder, controller-runtime |
| Identity | Ory Kratos and Ory Hydra |
| Build | Argo Workflows, rootless BuildKit, cosign |
| GitOps | Argo CD and Gitea |
| Progressive delivery | Argo Rollouts with Traefik weighted routing |
| Autoscaling | KEDA with the HTTP add-on |
| Secrets | OpenBao and External Secrets Operator |
| Observability | Prometheus, Grafana, Loki, Tempo, OpenTelemetry |

## Development

You need Go 1.26, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev),
[Task](https://taskfile.dev), [Helm](https://helm.sh), [Helmfile](https://helmfile.readthedocs.io)
and kubectl.

The repository is a Go workspace with nine modules and no `go.mod` at the root. One of the
modules is the sibling repository `loop-runtime-contracts`, which `go.work` expects at
`../loop-runtime-contracts`. Clone it next to this one, or the workspace does not build.

### Setup

```bash
task setup    # one-shot: creates the local k3d clusters and installs the platform
tilt up       # dev loop: reload of the API and the operators on file save
task doctor   # checks prerequisites and the health of the environment
```

`task setup` provisions the platform once. The dev loop is `tilt up`. The full workflow is in
[docs/operations.md](docs/operations.md).

Tests run per module, or through the task runner:

```bash
task test                           # unit tests, lint and manifest validation

cd api               && go test ./...
cd pkg               && go test ./...
cd operators         && go test ./...
cd cmd/loop-runtime-ops  && go test ./...
```

## Branches

Work happens on `workspace`. `develop` receives it through a pull request, and `main` receives
releases from `develop`.

## License

See [LICENSE](LICENSE).
