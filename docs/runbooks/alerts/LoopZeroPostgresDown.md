# LoopZeroPostgresDown

**Severity:** Critical
**Alert:** `pg_up == 0`
**Duration:** 1m

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- All API mutations blocked (advisory locks depend on PG)

## Triage (copy-paste commands)
```bash
# 1. Check PG connectivity from API pod
kubectl --context mgmt -n platform-system exec deploy/loopzero-api -- \
  pg_isready -h $PGHOST -p 5432 -U loopzero

# 2. Check API logs for connection errors
kubectl --context mgmt -n platform-system logs deploy/loopzero-api --tail=30 \
  | grep -i "postgres\|pgx\|connection"

# 3. Check PG pool metrics (if exporter is up)
kubectl --context mgmt -n monitoring port-forward svc/prometheus 9090:9090 &
curl -s 'http://localhost:9090/api/v1/query?query=pg_up' | jq .

# 4. For DO managed PG: check via doctl
doctl databases list
doctl databases connection <db-id> --format Host,Port,Database
```

## Resolution

### Managed PG (DigitalOcean production)
1. Check DO dashboard for maintenance windows or incidents: https://status.digitalocean.com
2. Verify firewall rules allow connections from DOKS nodes:
   ```bash
   doctl databases firewalls list <db-id>
   ```
3. If PG is up on DO but API cannot connect, check network policy:
   ```bash
   kubectl --context mgmt -n platform-system get networkpolicy
   ```

### Local PG (k3d dev)
1. Check PG pod status:
   ```bash
   kubectl --context mgmt -n platform-system get pods -l app.kubernetes.io/name=postgresql
   kubectl --context mgmt -n platform-system describe pod -l app.kubernetes.io/name=postgresql
   ```
2. Check PVC is bound:
   ```bash
   kubectl --context mgmt -n platform-system get pvc -l app.kubernetes.io/name=postgresql
   ```
3. Restart if needed:
   ```bash
   kubectl --context mgmt -n platform-system rollout restart statefulset/postgresql
   ```

## Escalation
- If not resolved in 5 min: escalate to SRE (PG down = full platform outage)
- If DO managed PG: open DO support ticket immediately

## Verification
```bash
# Verify PG is back
kubectl --context mgmt -n platform-system exec deploy/loopzero-api -- \
  pg_isready -h $PGHOST -p 5432 -U loopzero

# Verify API health endpoint
kubectl --context mgmt -n platform-system exec deploy/loopzero-api -- \
  curl -s http://localhost:8080/healthz | jq .

# Verify Prometheus metric recovered
curl -s 'http://localhost:9090/api/v1/query?query=pg_up' | jq '.data.result[0].value[1]'
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Check TCP keepalive settings (90s detect dead connections)
- [ ] Verify leader heartbeat timeout (10s) did not cause zombie hangs
