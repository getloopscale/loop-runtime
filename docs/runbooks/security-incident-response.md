# Security Incident Response Playbook

**Owner:** Platform Team
**Last Updated:** 2026-04-22
**Review Cadence:** Quarterly

---

## Severity Classification

| Level | Description | Response Time | Examples |
|-------|------------|---------------|---------|
| **P0** | Active breach, data exfiltration | Immediately | Unauthorized access to PG, leaked admin key in use |
| **P1** | Credential leak, unexploited | < 1 hour | Secret committed to git, token in logs |
| **P2** | Vulnerability discovered | < 24 hours | CVE in dependency, misconfiguration |
| **P3** | Policy violation | < 1 week | Missing MFA, expired token, audit gap |

---

## Scenario 1: Credential Leak

**Trigger:** Secret, API key, token, or password found in logs, git history, or public repo.

### Containment (< 15 min)
```bash
# 1. Identify which credential leaked
# 2. Rotate immediately — dual-key pattern for zero-downtime

# Service auth secret:
pulumi config set --secret LOOPZERO_SERVICE_AUTH_SECRET "$(openssl rand -hex 32)" --stack prod
pulumi config set --secret LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS "$OLD_SECRET" --stack prod
pulumi up --stack prod --yes

# Admin API key:
pulumi config set --secret LOOPZERO_ADMIN_API_KEY "$(openssl rand -hex 32)" --stack prod
pulumi up --stack prod --yes

# Gitea token: trigger PlatformBootstrap re-reconcile
kubectl annotate platformbootstrap platform-bootstrap platform.loopzero.io/force-retry=true --overwrite

# GitHub App private key: regenerate in GitHub App settings → download new PEM
```

### Investigation
```bash
# Check audit log for unauthorized usage
curl -H "Authorization: Bearer $TOKEN" https://api.$DOMAIN/api/v1/audit?since=2h

# Check access logs
kubectl logs -n platform-system deployment/loopzero-api --since=2h | grep "X-Service-Auth\|X-Admin-Key"
```

### Notification
- [ ] Notify all team members via Slack/PagerDuty
- [ ] If customer data potentially accessed: GDPR 72h notification clock starts
- [ ] Document in incident timeline

---

## Scenario 2: Data Breach

**Trigger:** Evidence of unauthorized data access or exfiltration.

### Containment (< 5 min)
```bash
# 1. Block external access if needed
# Disable CF Tunnel (nuclear option — blocks ALL traffic):
kubectl scale deployment cloudflared -n platform-system --replicas=0

# 2. Revoke all active sessions
kubectl rollout restart deployment loopzero-api -n platform-system

# 3. Rotate PG credentials
pulumi config set --secret DATABASE_URL "new-connection-string" --stack prod
pulumi up --stack prod --yes
```

### Investigation
```bash
# Audit trail
psql $DATABASE_URL -c "SELECT * FROM audit_log WHERE created_at > now() - interval '24 hours' ORDER BY created_at DESC LIMIT 100"

# PG connection history (if available)
psql $DATABASE_URL -c "SELECT * FROM pg_stat_activity WHERE state = 'active'"
```

### GDPR Notification (if EU users affected)
- [ ] Notify DPA within 72 hours
- [ ] Notify affected users without undue delay
- [ ] Document: what data, how many users, containment measures, remediation

---

## Scenario 3: Tenant Isolation Breach

**Trigger:** Tenant A accesses Tenant B's data, namespace, or resources.

### Containment (< 10 min)
```bash
# 1. Identify affected tenants
kubectl get namespaces -l loopzero.io/tenant=$AFFECTED_TENANT

# 2. Verify NetworkPolicy is in place
kubectl get networkpolicy -n $NAMESPACE

# 3. If breach confirmed — isolate the breaching tenant
kubectl cordon nodes running breaching tenant pods
kubectl scale deployment --all -n $BREACHING_NAMESPACE --replicas=0
```

### Investigation
```bash
# Check RBAC — was the Role too permissive?
kubectl get role,rolebinding -n $NAMESPACE -o yaml

# Check if tenant has extra permissions
kubectl auth can-i --list --as=system:serviceaccount:$NAMESPACE:loopzero-app -n $OTHER_NAMESPACE
```

---

## Scenario 4: Supply Chain Compromise

**Trigger:** Malicious image detected in registry, compromised dependency, or unsigned image running.

### Containment (< 15 min)
```bash
# 1. Identify compromised image
kubectl get pods --all-namespaces -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.spec.containers[*].image}{"\n"}{end}' | grep $SUSPECT_IMAGE

# 2. Block the image via registry
# Harbor: quarantine the repository
# DOCR: delete the tag

# 3. Roll back affected deployments
kubectl rollout undo deployment/$APP -n $NAMESPACE

# 4. Rebuild from clean source
# Trigger new build from verified commit
```

### Investigation
```bash
# Check cosign signatures
cosign verify --certificate-identity-regexp=".*" --certificate-oidc-issuer-regexp=".*" $IMAGE

# Check SBOM
cosign download sbom $IMAGE | grype
```

---

## Scenario 5: Unauthorized Access

**Trigger:** Unknown user, session, or API call detected in audit logs.

### Containment (< 10 min)
```bash
# 1. Identify the account
curl -H "Authorization: Bearer $ADMIN_TOKEN" https://api.$DOMAIN/api/v1/audit?user_id=$SUSPECT_USER

# 2. Disable the account (via Kratos admin API)
curl -X PUT "http://kratos-admin:4434/admin/identities/$IDENTITY_ID" \
  -H "Content-Type: application/json" \
  -d '{"state": "inactive"}'

# 3. Invalidate all sessions
curl -X DELETE "http://kratos-admin:4434/admin/identities/$IDENTITY_ID/sessions"
```

---

## Scenario 6: Platform Infrastructure Compromise

**Trigger:** Unauthorized access to K8s cluster, Pulumi state, or cloud account.

### Containment
```bash
# 1. Rotate all kubeconfigs
doctl kubernetes cluster kubeconfig save $CLUSTER_NAME

# 2. Rotate Pulumi passphrase (if state encrypted)
# 3. Rotate DO API token in GitHub Secrets
# 4. Review IAM permissions — remove any unauthorized

# 5. Check for persistent backdoors
kubectl get clusterrolebindings | grep -v system:
kubectl get serviceaccounts --all-namespaces | grep -v default | grep -v system
```

---

## Emergency Contacts

| Role | Contact | Escalation |
|------|---------|-----------|
| Platform Lead | Paulo | First responder for all P0/P1 |
| On-call Engineer | (define rotation) | First responder for P2/P3 |

## Post-Incident Template

```markdown
## Incident Report — INC-YYYY-MM-DD-NNN

**Severity:** P0/P1/P2/P3
**Duration:** Start → End (total minutes)
**Impact:** N users/tenants affected
**Root Cause:** [description]

### Timeline
- HH:MM — Alert fired / issue reported
- HH:MM — Containment action taken
- HH:MM — Root cause identified
- HH:MM — Fix deployed
- HH:MM — Verified resolved

### What went well
- ...

### What went wrong
- ...

### Action items
- [ ] ...
```
