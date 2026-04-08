# Custom Domains Go-Live Plan

> Feature code is 100% complete and merged. This plan covers deployment,
> migration, configuration, and end-to-end validation with real domain `aquim.ai`.
> Estimated effort: 1 day.

---

## Pre-Conditions

- [x] Code merged to develop (PR #25 + blocker fixes)
- [x] All components wired (API, CLI, dashboard, provisioner, health checker)
- [x] Migrations 000024 + 000025 exist
- [x] State machine complete (including `degraded` state)
- [ ] Pipeline deployed to DigitalOcean
- [ ] Migrations applied to production DB
- [ ] Domain `aquim.ai` DNS accessible

---

## Task 1: Deploy to DigitalOcean

Push to develop triggers CI → Build → Deploy pipeline.

**DoD:**
- [ ] Pipeline: CI passes (lint, tests, manifests)
- [ ] Pipeline: Build creates new loopzero-api + loopzero-dashboard images
- [ ] Pipeline: Pulumi deploys (migrations run via init container)
- [ ] `custom_domains` table exists in DB
- [ ] Domain provisioner running (check API logs: "domain provisioner started")
- [ ] Domain health checker running (check API logs: "domain health checker started")

---

## Task 2: Verify CNAME Target Exists

The CF Tunnel must have `cname.dev.loopzero.example` DNS record pointing to the tunnel.

**Check:**
```bash
dig cname.dev.loopzero.example +short
# Should return: {tunnel-id}.cfargotunnel.com
```

**DoD:**
- [ ] `cname.dev.loopzero.example` resolves to CF Tunnel
- [ ] CF Tunnel catch-all routes to Traefik (not 404)

---

## Task 3: Create a Project for Testing

Need an active project + environment to attach a domain to.

```bash
LOOPZERO_URL=https://api.dev.loopzero.example
loopzero project create aquim-test
loopzero deploy  # or just create project — domain needs project+app+env
```

**DoD:**
- [ ] Project `aquim-test` exists
- [ ] Environment `production` active

---

## Task 4: Add Custom Domain via CLI

```bash
loopzero domain add aquim.ai --project aquim-test --app web --env production
```

Expected output:
```
Custom domain added: aquim.ai

  Status: pending_verification

  To verify ownership, add this DNS record:
    Type:  TXT
    Name:  _loopzero-verify.aquim.ai
    Value: loopzero-verify=<token>

  After adding the record, run:
    loopzero domain verify aquim.ai
```

**DoD:**
- [ ] `loopzero domain add` returns pending_verification status
- [ ] TXT record name and value displayed
- [ ] Domain visible in `loopzero domain list`
- [ ] Domain visible in dashboard at cloud.dev.loopzero.example/domains

---

## Task 5: Configure DNS for aquim.ai

Two DNS records needed at the domain registrar:

**Record 1: TXT (verification)**
```
Type:  TXT
Name:  _loopzero-verify.aquim.ai
Value: loopzero-verify=<token-from-step-4>
```

**Record 2: CNAME (traffic routing)**
```
Type:  CNAME
Name:  aquim.ai (or @ or subdomain)
Value: cname.dev.loopzero.example
```

**Note:** Some registrars don't allow CNAME on root domain (@). In that case:
- Use a subdomain: `www.aquim.ai` or `app.aquim.ai`
- Or use registrar's CNAME flattening / ALIAS feature if available

**DoD:**
- [ ] TXT record added and propagated (`dig TXT _loopzero-verify.aquim.ai`)
- [ ] CNAME record added and propagated (`dig CNAME aquim.ai` or subdomain)

---

## Task 6: Verify Domain

```bash
loopzero domain verify aquim.ai
```

Expected: status changes to `verified`, then provisioner picks it up.

**DoD:**
- [ ] `loopzero domain verify` returns "verified" or "Verification successful"
- [ ] Status transitions: pending_verification → verified
- [ ] Domain provisioner log: "claimed verified domain" (within 30s)

---

## Task 7: Wait for Provisioning

The domain provisioner (30s interval) will:
1. Claim the verified domain
2. Read the active release for the app
3. Render IngressRoute + Certificate manifests with the custom domain
4. Push to gitops (Gitea)
5. ArgoCD syncs → IngressRoute + Certificate created on runtime cluster
6. cert-manager issues Let's Encrypt TLS certificate
7. Domain transitions to `active`

```bash
# Monitor status
loopzero domain status aquim.ai
# Or watch logs
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt -n platform-system \
  logs deployment/loopzero-api --tail=20 -f | grep -i "domain"
```

**DoD:**
- [ ] Status: provisioning → active
- [ ] IngressRoute exists on runtime cluster for aquim.ai
- [ ] Certificate CR exists and status is Ready=True
- [ ] TLS status: issued
- [ ] `loopzero domain status aquim.ai` shows active + TLS issued

---

## Task 8: Test Domain Access

```bash
# Test HTTPS
curl -s -o /dev/null -w "%{http_code}" https://aquim.ai
# Or the subdomain if using subdomain
curl -s -o /dev/null -w "%{http_code}" https://www.aquim.ai

# Test TLS certificate
echo | openssl s_client -connect aquim.ai:443 -servername aquim.ai 2>/dev/null | \
  openssl x509 -noout -subject -dates
```

**DoD:**
- [ ] HTTPS request to aquim.ai returns response from the app
- [ ] TLS certificate is valid (issued by Let's Encrypt)
- [ ] Certificate subject matches aquim.ai
- [ ] No certificate errors in browser

---

## Task 9: Test Dashboard

Open `https://cloud.dev.loopzero.example/domains` in browser.

**DoD:**
- [ ] Domain `aquim.ai` shows in the list
- [ ] Status badge shows "Active" (green)
- [ ] TLS info shows "Issued" with expiry date
- [ ] No console errors

---

## Task 10: Test Domain Removal Flow

```bash
loopzero domain remove aquim.ai --confirm aquim.ai
```

**DoD:**
- [ ] Status transitions to `removing`
- [ ] Provisioner removes IngressRoute + Certificate from gitops
- [ ] ArgoCD syncs → resources removed from runtime cluster
- [ ] Domain row deleted from DB
- [ ] `loopzero domain list` shows empty (or domain gone)
- [ ] Traffic to aquim.ai no longer routes to the app

---

## Task 11: Re-add Domain (Idempotency Test)

```bash
loopzero domain add aquim.ai --project aquim-test --app web --env production
loopzero domain verify aquim.ai
# Wait for provisioning
loopzero domain status aquim.ai
```

**DoD:**
- [ ] Domain can be re-added after removal
- [ ] Full cycle works again: add → verify → provision → active
- [ ] TLS certificate re-issued

---

## Execution Order

```
Task 1: Deploy (pipeline)           — 25 min (wait for pipeline)
Task 2: Verify CNAME target         — 2 min
Task 3: Create test project         — 2 min
Task 4: Add domain via CLI          — 2 min
Task 5: Configure DNS (manual)      — 5 min + propagation (up to 5 min)
Task 6: Verify domain               — 1 min
Task 7: Wait for provisioning       — 2-5 min (30s poll + gitops + cert)
Task 8: Test HTTPS access           — 2 min
Task 9: Test dashboard              — 2 min
Task 10: Test removal               — 5 min
Task 11: Re-add (idempotency)       — 5 min

Total: ~1 hour (including DNS propagation and pipeline wait)
```

---

## Troubleshooting

| Problem | Cause | Fix |
|---------|-------|-----|
| Migration fails | DB constraint conflict | Check if table already exists from previous attempt |
| Provisioner not starting | Not leader pod | Check `loopzero-api` logs for "leader lock" |
| CNAME not resolving | DNS propagation | Wait 5-10 min, check with `dig` |
| TXT verification fails | DNS not propagated | Wait, check with `dig TXT _loopzero-verify.aquim.ai` |
| Certificate not issued | cert-manager rate limit | Check Certificate CR status, cert-manager logs |
| IngressRoute not created | GitOps push failed | Check provisioner logs for gitops errors |
| HTTPS timeout | CF Tunnel not routing | Verify `cname.dev.loopzero.example` resolves, check tunnel config |
