---
type: operational-runbook
title: Service-Auth Secret Rotation Runbook
---

# Service-Auth Secret Rotation Runbook

> T1.2 of plan `2026-05-29-dashboard-public-integration-canonicalization`.

## Context

`LOOPZERO_SERVICE_AUTH_SECRET` is the shared secret between `loopzero-cloud` and
`loopzero-api`. The cloud injects it as `X-Service-Auth` on every request it
proxies to the engine; the engine middleware (`api/internal/middleware/auth.go:97`)
validates it. Rotation must be **zero-downtime**: the two services must
accept BOTH the old and new value during the cutover window, otherwise
every dashboard request returns 401 `Invalid service authentication`.

The middleware already supports this via the `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS`
env var (`api/internal/middleware/auth.go:181`). This runbook documents the
procedure that exercises that capability.

## Trigger conditions

Rotate the secret when:

- A credential leak is suspected (logs disclosed, laptop compromised, etc.).
- 180 days have elapsed since `LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT` (set by
  `api/internal/config/config.go:432` via env var; track via Grafana panel
  `loopzero_service_auth_age_seconds`).
- During disaster recovery rehearsal (annual).

## Procedure (~3 minutes downtime risk window = 0)

```bash
# Set context
CONTEXT=${CONTEXT:-k3d-loopzero-mgmt}
NS=${NS:-loopzero-system}

# 1. Capture the current secret value (will become the PREVIOUS after rotation).
CURRENT=$(kubectl --context=$CONTEXT -n $NS \
  get secret loopzero-service-auth -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' \
  | base64 -d)
echo "Current secret length: ${#CURRENT} (must be >= 32)"

# 2. Generate a new value (base64-safe ASCII, 32+ chars).
NEW=$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)
echo "New secret length: ${#NEW}"

# 3. Apply BOTH keys (NEW as the active, CURRENT as the rotation window).
#    THIS is the no-downtime trick: both api and cloud accept either value
#    while the new one propagates and the workloads restart.
kubectl --context=$CONTEXT -n $NS create secret generic loopzero-service-auth \
  --from-literal=LOOPZERO_SERVICE_AUTH_SECRET="$NEW" \
  --from-literal=LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS="$CURRENT" \
  --from-literal=LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --dry-run=client -o yaml | kubectl --context=$CONTEXT apply -f -

# 4. Rollout-restart both deployments to pick up the new env values.
#    Order matters: restart loopzero-api FIRST so it accepts BOTH old and new
#    before cloud starts sending only the new value.
kubectl --context=$CONTEXT -n $NS rollout restart deploy/loopzero-api
kubectl --context=$CONTEXT -n $NS rollout status deploy/loopzero-api --timeout=180s

kubectl --context=$CONTEXT -n $NS rollout restart deploy/loopzero-cloud
kubectl --context=$CONTEXT -n $NS rollout status deploy/loopzero-cloud --timeout=180s

# 5. Smoke test: dashboard engine proxy should succeed with a fresh JWT.
#    See: infra/tests/e2e/dashboard-engine-proxy_test.sh
echo "Smoke test:"
bash infra/tests/e2e/dashboard-engine-proxy_test.sh
```

## Cleanup (after 24h soak — drop the PREVIOUS key)

After 24 hours the old credential cannot be in flight from any pod (worst-case
HTTP timeout is ~30s + connection keep-alive 60s + a few retries). Drop the
PREVIOUS field:

```bash
NEW=$(kubectl --context=$CONTEXT -n $NS \
  get secret loopzero-service-auth -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' \
  | base64 -d)

kubectl --context=$CONTEXT -n $NS create secret generic loopzero-service-auth \
  --from-literal=LOOPZERO_SERVICE_AUTH_SECRET="$NEW" \
  --from-literal=LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --dry-run=client -o yaml | kubectl --context=$CONTEXT apply -f -

kubectl --context=$CONTEXT -n $NS rollout restart deploy/loopzero-api deploy/loopzero-cloud
```

## Verification checklist

- [ ] `kubectl exec deploy/loopzero-cloud -- env | grep LOOPZERO_SERVICE_AUTH_SECRET` matches the new value.
- [ ] `kubectl exec deploy/loopzero-api -- env | grep LOOPZERO_SERVICE_AUTH_SECRET` matches the new value.
- [ ] `curl /engine/api/v1/projects` with a fresh JWT returns 200 (not 401).
- [ ] Grafana panel `loopzero_service_auth_age_seconds` reset to 0.
- [ ] `audit_log` row written by `api/internal/middleware/auth_failure_audit.go` shows zero `auth.service_auth_mismatch` events post-rotation.

## Rollback

If a smoke test fails post-rotation, the OLD value is still accepted (it's
in `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS`). Revert by promoting `_PREVIOUS` back
to the primary key and rolling out:

```bash
PREV=$(kubectl --context=$CONTEXT -n $NS \
  get secret loopzero-service-auth -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS}' \
  | base64 -d)

kubectl --context=$CONTEXT -n $NS create secret generic loopzero-service-auth \
  --from-literal=LOOPZERO_SERVICE_AUTH_SECRET="$PREV" \
  --dry-run=client -o yaml | kubectl --context=$CONTEXT apply -f -

kubectl --context=$CONTEXT -n $NS rollout restart deploy/loopzero-api deploy/loopzero-cloud
```

## Anti-Patterns (do NOT do these)

- **Single-value rotation without `_PREVIOUS`** — guaranteed 401 storm during
  the window where one pod has the new value and another has the old.
- **Restarting cloud before api** — cloud will send NEW requests with the new
  value; api still has only the old → 401. Order matters: api first.
- **Skipping the 24h soak** — long-lived connections (SSE, gRPC streams)
  hold the old value past pod restart in client libraries.
- **Using `kubectl set env` instead of patching the Secret** — set env keeps
  the literal in the Deployment manifest, bypassing the envFrom secretRef
  pattern and reintroducing the drift this plan removed.

## Cross-references

- Plan: `.claude/knowledge-base/plans/2026-05-29-dashboard-public-integration-canonicalization-plan.md`
- Middleware reading both keys: `api/internal/middleware/auth.go:175-200`
- Plan-time TTL config: `api/internal/config/config.go:432`
- Test fixture: `infra/tests/e2e/dashboard-engine-proxy_test.sh`
