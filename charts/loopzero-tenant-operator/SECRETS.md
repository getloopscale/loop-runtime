# loopzero-tenant-operator — Secret manifest (install-config-contract)

The `loopzero-tenant-operator` chart consumes credentials **by reference** (`--set secrets.existingSecret=<name>`), never inline.
Populate via ESO (`examples/eso-*.yaml`) / CSI / sops / kubectl — per-install in your target's native store.

> kubeconfigs are NOT here — they are platform-mounted (`loopzero-cluster-kubeconfigs` volume, EC-1).

Set-equal to the rendered Secret keys (hermetic Go guard). `openssl rand -hex 32` where noted.

| Key | How to obtain | Description |
|---|---|---|
| `DIGITALOCEAN_TOKEN` | from DO | tenant-operator credential |
| `GITEA_TOKEN` | gitea API token | tenant-operator credential |
| `MINIO_ACCESS_KEY` | access key | tenant-operator credential |
| `MINIO_SECRET_KEY` | openssl rand -hex 20 | tenant-operator credential |
| `REGISTRY_DOCKER_CONFIG` | docker config json | tenant-operator credential |
| `SPACES_ACCESS_KEY_ID` | DO Spaces | tenant-operator credential |
| `SPACES_SECRET_ACCESS_KEY` | DO Spaces secret | tenant-operator credential |
