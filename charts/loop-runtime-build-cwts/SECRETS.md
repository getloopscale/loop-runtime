# loop-runtime-build-cwts — Secrets manifest (install-config-contract)

The `loop-runtime-build-cwts` chart owns NO secret. Its build steps (Argo `ClusterWorkflowTemplate`s
`loop-runtime-clone` / cache-save / cache-restore) consume an **operator-provided** K8s Secret
**by reference** (`envFrom: secretRef`) — the existingSecret floor (anti-lock-in): the chart's
contract is "a Secret with these keys must exist", not "the chart creates the secret". Any
backend (ESO / CSI / sops / `kubectl create secret`) may supply it.

**Referenced Secret:** `loop-runtime-minio-credentials`

Override the name with `--set minio.existingSecret=<name>` (default `loop-runtime-minio-credentials`).

## Required keys

The referenced Secret MUST provide these keys (consumed via `envFrom` by the clone runner —
`mc alias set "$MINIO_ENDPOINT" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY"`, `cwt-clone.yaml:45`):

| Key | Plane | Description |
|---|---|---|
| `AWS_ACCESS_KEY_ID` | secret | MinIO/S3 access key id the build runner authenticates with |
| `AWS_SECRET_ACCESS_KEY` | secret | MinIO/S3 secret access key |
| `MINIO_ENDPOINT` | config (same secret) | MinIO/S3 endpoint URL the runner targets (delivered in the same referenced secret via envFrom) |

## How to provide it

- **ESO (recommended):** see [`examples/eso-minio.yaml`](examples/eso-minio.yaml) — a `ClusterSecretStore` + `ExternalSecret` materializing `loop-runtime-minio-credentials` from your backend (OpenBao / AWS SM / GCP SM).
- **Direct:** `kubectl create secret generic loop-runtime-minio-credentials --from-literal=AWS_ACCESS_KEY_ID=... --from-literal=AWS_SECRET_ACCESS_KEY=... --from-literal=MINIO_ENDPOINT=...`
- **Never** commit secret values to git/artifact — the secret lives in the target cluster's native store (anti-lock-in ADR).
