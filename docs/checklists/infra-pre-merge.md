# Infra Pre-Merge Checklist

Validate before merging any PR that touches `infra/`.

## Pulumi Code (`infra/pulumi/`)

- [ ] `npx tsc --noEmit` passes
- [ ] `npm test` passes (32 unit tests)
- [ ] `pulumi preview` shows only expected changes (posted as PR comment)
- [ ] Zero destructive deletes unless intentional
- [ ] New deployments have `skipAwait: "true"` if image may not exist yet
- [ ] Container ports match readiness/liveness probe ports
- [ ] All platform deployments have PSA-restricted securityContext:
  - Pod: `runAsNonRoot`, `seccompProfile: RuntimeDefault`
  - Container: `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`
- [ ] Helm chart dependencies resolve (`helm dependency update`)
- [ ] No `Date.now()` or timestamp-based triggers (causes non-idempotent diffs)

## Scripts (`infra/scripts/`)

- [ ] `bash -n script.sh` passes (syntax valid)
- [ ] Zero `|| true` masking unexpected errors (only on `kill` of PIDs)
- [ ] All cloud resource filters use stack-scoped prefix (`startswith(prefix)`)
- [ ] Idempotent: second run is no-op on healthy system

## Workflows (`.github/workflows/infra-*.yml`)

- [ ] YAML valid (`python3 -c "import yaml; yaml.safe_load(open('file.yml'))"`)
- [ ] Zero `pulumi cancel` or `pulumi state delete` in deploy workflow
- [ ] Zero `PULUMI_K8S_DELETE_UNREACHABLE` in deploy workflow
- [ ] `concurrency.cancel-in-progress: false` for deploy workflows
- [ ] Environment protection configured for target stacks
