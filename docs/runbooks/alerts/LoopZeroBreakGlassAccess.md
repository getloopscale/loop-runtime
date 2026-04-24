# LoopZeroBreakGlassAccess

**Severity:** Critical
**Alert:** `loopzero_break_glass_sessions_active > 0`
**Duration:** 0m (fires immediately)

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Emergency direct cluster access is in use, bypassing normal RBAC
- Requires post-incident review within 24 hours

## Triage (copy-paste commands)
```bash
# 1. Identify active break-glass sessions
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_break_glass_sessions_active' | jq .

# 2. Check audit log for break-glass events
kubectl --context mgmt -n loopzero-system logs deploy/loopzero-api --tail=100 \
  | grep -i "break.glass\|emergency.access\|elevated"

# 3. Check K8s audit logs for direct cluster access
kubectl --context mgmt -n kube-system logs -l component=kube-apiserver --tail=50 \
  | grep -i "break-glass\|cluster-admin"

# 4. List ClusterRoleBindings for elevated access
kubectl --context mgmt get clusterrolebindings | grep -i "break-glass\|emergency\|admin"

# 5. Check who is connected
kubectl --context mgmt auth whoami
kubectl --context mgmt get events --sort-by=.lastTimestamp | tail -20
```

## Resolution
1. **Verify the session is authorized:**
   - Confirm with the on-call engineer that the break-glass access is for a legitimate emergency
   - Document the reason in the incident channel immediately

2. **Monitor the session:**
   ```bash
   # Watch for any destructive operations
   kubectl --context mgmt get events -w --sort-by=.lastTimestamp
   ```

3. **Revoke access when no longer needed:**
   ```bash
   # Remove break-glass ClusterRoleBinding
   kubectl --context mgmt delete clusterrolebinding break-glass-<user>
   # Verify removal
   kubectl --context mgmt get clusterrolebindings | grep -i break-glass
   ```

4. **Rotate credentials if unauthorized:**
   ```bash
   # If unauthorized access detected, rotate immediately:
   # 1. Revoke the break-glass binding
   kubectl --context mgmt delete clusterrolebinding break-glass-<user>
   # 2. Rotate service account tokens
   kubectl --context mgmt -n loopzero-system delete secret <sa-token>
   # 3. Refresh kubeconfig
   doctl kubernetes cluster kubeconfig save <cluster-id>
   ```

## Escalation
- If unauthorized: escalate IMMEDIATELY to security lead
- If authorized: schedule post-incident review within 24 hours
- See [Security Incident Response](../security-incident-response.md) for credential leak procedures

## Verification
```bash
# Verify break-glass session ended
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_break_glass_sessions_active' \
  | jq '.data.result[0].value[1]'
# Should be "0"

# Verify no elevated ClusterRoleBindings remain
kubectl --context mgmt get clusterrolebindings | grep -i "break-glass\|emergency"
# Should be empty
```

## Post-Incident (mandatory within 24h)
- [ ] Document reason for break-glass access
- [ ] Document all operations performed during the session
- [ ] Audit trail review: verify no unauthorized changes
- [ ] Root cause analysis: why was normal access insufficient?
- [ ] Update RBAC if the required access should be permanent
