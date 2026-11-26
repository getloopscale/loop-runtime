# loopruntime-build-cwts

Argo `ClusterWorkflowTemplate`s for the LoopRuntime build pipeline (source clone + build cache
save/restore). Install-config-contract surface: this chart consumes a build secret **by
reference** (it owns none) and ships a fail-fast `values.schema.json` (root
`additionalProperties:false`).

## Install

```bash
helm install loopruntime-build-cwts oci://ghcr.io/getloopscale/charts/loopruntime-build-cwts \
  --namespace <build-namespace> \
  -f values-<env>.yaml
```

## Required secret (by reference)

The build CWTs read MinIO/S3 credentials from an operator-provided K8s Secret via
`envFrom: secretRef`. See [`SECRETS.md`](SECRETS.md) for the required keys and
[`examples/eso-minio.yaml`](examples/eso-minio.yaml) for an ESO recipe.

- Default referenced secret: `loopruntime-minio-credentials`.
- Override: `--set minio.existingSecret=<your-secret-name>` (existingSecret floor — any
  backend supplies it; no secret value ever lives in git/artifact).

## Schema fail-fast

`values.schema.json` rejects unknown top-level keys (`additionalProperties:false`): a
typo'd Helm value fails `helm install` instead of being silently ignored.
