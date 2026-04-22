# Service Auth Secret Rotation

## Alert

**LoopZeroServiceAuthSecretExpiring** fires when `loopzero_service_auth_secret_age_seconds > 6912000` (80 days). The target is to rotate every 90 days.

## What This Secret Does

The service auth secret (`LOOPZERO_SERVICE_AUTH_SECRET`) authenticates service-to-service calls between the cloud service (dashboard backend) and the API (engine). Both services must share the same secret value. If they disagree, cloud-to-engine proxy calls fail with 401.

## Rotation Procedure (Zero-Downtime)

### 1. Generate a new secret

```bash
openssl rand -base64 48
```

Save the output. This is your `NEW_SECRET`.

### 2. Set the new secret in Pulumi

```bash
cd infra/pulumi
pulumi config set --secret serviceAuthSecret "<NEW_SECRET>" --stack <stack>
```

### 3. Set the creation timestamp

Record when the secret was created so the age metric resets:

```bash
pulumi config set serviceAuthSecretCreatedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --stack <stack>
```

### 4. Deploy

```bash
# Preview first
pulumi preview --stack <stack>

# Apply
pulumi up --stack <stack>
```

This updates the Kubernetes Secret `loopzero-api-secrets` and restarts both the API and cloud service pods (since the Secret content changed, the pod hash changes and Kubernetes rolls).

### 5. Verify

After rollout completes:

```bash
# Check API pods are running
kubectl get pods -n loopzero-system -l app=loopzero-api

# Check cloud service pods are running
kubectl get pods -n loopzero-system -l app=loopzero-cloud-service

# Verify the metric reset (should be close to 0)
curl -s http://<api-metrics>:9090/metrics | grep loopzero_service_auth_secret_age_seconds

# Test a cloud-to-engine call (e.g., via the dashboard)
# If using curl directly:
curl -H "Authorization: Bearer <NEW_SECRET>" https://api.<domain>/api/v1/health
```

### 6. Confirm alert cleared

The `LoopZeroServiceAuthSecretExpiring` alert should resolve within the next Prometheus scrape cycle (15s default) plus the `for: 1h` pending period reset.

## Rollback

If the new secret causes failures:

1. Revert to the previous secret in Pulumi config.
2. Run `pulumi up` to redeploy.
3. Investigate why the new secret did not propagate correctly.

## Automation (Future)

This process can be automated with External Secrets Operator and a secret rotation policy. Until then, the alert provides a 10-day window before the 90-day deadline.

## Schedule

| Action | Timeline |
|--------|----------|
| Alert fires | Day 80 |
| Rotate secret | Day 80-85 |
| Hard deadline | Day 90 |
