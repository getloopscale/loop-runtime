# Runbook: LoopZeroCertRenewalFailed

**Severity:** Warning
**Component:** cert-manager + loopzero-tenant-controller (ExposureController)
**SLO impact:** TLS for tenant ingress, custom domains v2

## Alert expression

```
certmanager_certificate_expiration_timestamp_seconds - time() < 7 * 86400
  unless certmanager_certificate_ready_status{condition="Ready"} == 1
```

Triggered when a certificate is < 7 days from expiry AND not in Ready state.

## What this means

cert-manager has not renewed a certificate near expiry. Imminent TLS failure for tenant ingress.

## Triage (first 5 min)

1. Identify failing cert:
   ```
   kubectl get certificates -A -o jsonpath='{range .items[?(@.status.conditions[0].status!="True")]}{.metadata.namespace}/{.metadata.name}{"\t"}{.spec.dnsNames}{"\n"}{end}'
   ```
2. Read latest CertificateRequest:
   ```
   kubectl get certificaterequests -A -l 'cert-manager.io/certificate-name'=<cert>
   kubectl describe certificaterequest <latest>
   ```
3. Check Order + Challenge:
   ```
   kubectl get orders,challenges -n <namespace>
   ```

## Diagnose

| Symptom | Likely cause |
|---------|--------------|
| HTTP01 challenge failing | Ingress not reachable from internet (CF Tunnel down or DNS misconfig) |
| DNS01 challenge failing | DNS provider creds expired |
| Rate limit (LE) | Issued > 50 certs/week from same domain — back off |
| No Order created | cert-manager pod issues — check pods |
| `CFManaged: true` and CR exists | **Misconfiguration** — CF-managed domains should NOT have cert-manager Certificate (per CLAUDE.md known patterns) |

## Mitigate

**HTTP01 unreachable:**
1. Verify CF Tunnel: `kubectl -n loopzero-system logs deploy/cloudflared`
2. Verify DNS: `dig <domain> @1.1.1.1`
3. Re-check ingress reachability

**Rate-limited:**
- Use staging issuer until limit resets (1 week)
- Revoke unused certs first

**CFManaged misconfiguration (known pattern):**
- Set `CFManaged=true` on the ExposedRoute; controller will skip Certificate CR creation
- Remove existing Certificate object if it was erroneously created

## Verify recovery

- Certificate Status.Conditions[Ready]=True
- Renewal log shows success
- TLS endpoint returns valid cert: `curl --resolve <host>:443:<ip> https://<host>/`

## Escalation

> 24h before expiry unresolved: page on-call edge/networking engineer. Customer-facing impact.

## Related

- Source: `operators/internal/controller/exposure_controller.go`
- CLAUDE.md: "Custom domains = CF Zone per domain" pattern
- CFZone client: `api/internal/cfzone/`
