# Spike: HTTP-01 Challenge Through CF Tunnel for Custom Domains

> **Status:** OPEN
> **Blocker for:** Custom Domains Phase 2 (provisioning)
> **Meeting:** MEET-2026-04-05-033
> **Date:** 2026-04-05

---

## Hypothesis

Let's Encrypt HTTP-01 challenges will work for custom domains that CNAME to
`cname.{baseDomain}` through the Cloudflare Tunnel, reaching cert-manager's
solver pod via Traefik.

## Challenge Path (Expected)

```
Let's Encrypt ACME server
    → GET http://api.mycompany.com/.well-known/acme-challenge/{token}
    → DNS: api.mycompany.com CNAME cname.dev.loopzero.example
    → Cloudflare edge (proxied=true)
    → CF Tunnel (catch-all rule → Traefik)
    → cloudflared pod (workload cluster)
    → Traefik (IngressRoute or Ingress created by cert-manager)
    → cert-manager HTTP-01 solver pod
    → 200 OK with challenge response
    → Certificate issued
```

## Known Risks

### Risk 1: Cloudflare HTTP→HTTPS Redirect
**Problem:** Cloudflare's "Always Use HTTPS" setting (enabled by default for
proxied records) redirects `http://` to `https://`. HTTP-01 challenges use
plain HTTP (port 80). If redirected, the challenge fails.

**Mitigation:** Cloudflare has a Page Rule exception: ACME challenges at
`/.well-known/acme-challenge/*` are NOT redirected even with "Always Use HTTPS"
enabled. This is a Cloudflare-wide behavior for Let's Encrypt compatibility.

**Verify:** Test with `curl -v http://cname.dev.loopzero.example/.well-known/acme-challenge/test`
— should NOT get 301 redirect.

### Risk 2: Cloudflare Caching
**Problem:** Cloudflare may cache the challenge response, serving stale
tokens on retry.

**Mitigation:** ACME challenge paths are typically not cached. Verify with
Cache-Control headers.

### Risk 3: cert-manager Solver Ingress Type
**Problem:** cert-manager creates either an Ingress or IngressRoute (depending
on config) for the solver pod. Must match Traefik's routing expectations.

**Mitigation:** cert-manager's HTTP-01 solver uses standard K8s Ingress
resources. Traefik's Kubernetes Ingress provider must be enabled to process
these (it is by default with Helm chart). The solver Ingress has
`Host: api.mycompany.com` which Traefik will route correctly.

### Risk 4: Catch-all Timing
**Problem:** The Certificate CR is created before the custom domain CNAME is
verified to actually resolve. If the CNAME isn't working yet, the challenge
fails.

**Mitigation:** Custom domain provisioning only triggers AFTER DNS verification
succeeds (TXT record confirmed). By that point, the CNAME should also be
active.

## Validation Steps

### Step 1: Local Simulation (No LE)
Test that HTTP traffic for a non-wildcard hostname reaches Traefik through
the tunnel.

```bash
# After B1 is deployed (catch-all rule active):

# 1. Create a test IngressRoute for a fake custom domain
kubectl apply -f - <<EOF
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: spike-custom-domain-test
  namespace: platform-system
spec:
  entryPoints:
    - web
    - websecure
  routes:
    - match: Host(\`spike-test.dev.loopzero.example\`)
      kind: Rule
      services:
        - name: runtime-traefik
          port: 80
EOF

# 2. Test that the route is reachable
curl -v https://spike-test.dev.loopzero.example/
# Expected: some response (404 or default backend — proves routing works)

# 3. Clean up
kubectl delete ingressroute spike-custom-domain-test -n platform-system
```

### Step 2: CNAME Routing (Real DNS)
Verify a CNAME to `cname.{baseDomain}` reaches Traefik.

```bash
# Prerequisite: cname.dev.loopzero.example DNS record deployed (B1 task 2)

# 1. Set up a temporary CNAME (e.g., via Cloudflare dashboard or DNS provider)
#    spike-test.yourdomain.com CNAME cname.dev.loopzero.example

# 2. Create IngressRoute for the custom hostname
kubectl apply -f - <<EOF
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: spike-cname-test
  namespace: platform-system
spec:
  entryPoints:
    - web
  routes:
    - match: Host(\`spike-test.yourdomain.com\`)
      kind: Rule
      services:
        - name: runtime-traefik
          port: 80
EOF

# 3. Test HTTP (not HTTPS — simulates ACME challenge path)
curl -v http://spike-test.yourdomain.com/
# Expected: response from Traefik (any response = routing works)

# 4. Check for HTTP→HTTPS redirect
curl -v http://spike-test.yourdomain.com/.well-known/acme-challenge/test-token
# Expected: NO 301 redirect. Should hit Traefik directly.

# 5. Clean up
kubectl delete ingressroute spike-cname-test -n platform-system
```

### Step 3: Real Certificate (Let's Encrypt Staging)
Issue a real cert through the full path.

```bash
# 1. Ensure a Let's Encrypt STAGING ClusterIssuer exists
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: admin@loopzero.example
    privateKeySecretRef:
      name: letsencrypt-staging-key
    solvers:
      - http01:
          ingress:
            class: traefik
EOF

# 2. Create Certificate CR for the custom hostname
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: spike-custom-cert
  namespace: platform-system
spec:
  secretName: spike-custom-cert-tls
  issuerRef:
    name: letsencrypt-staging
    kind: ClusterIssuer
  dnsNames:
    - spike-test.yourdomain.com
EOF

# 3. Watch certificate status
kubectl get certificate spike-custom-cert -n platform-system -w
# Expected: READY=True within 2-5 minutes

# 4. Verify the challenge was served via HTTP
kubectl get challenges -n platform-system
# Expected: challenge should have been created and resolved

# 5. Check the issued certificate
kubectl get secret spike-custom-cert-tls -n platform-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep "Subject:"
# Expected: CN=spike-test.yourdomain.com (from LE staging)

# 6. Clean up
kubectl delete certificate spike-custom-cert -n platform-system
kubectl delete secret spike-custom-cert-tls -n platform-system
kubectl delete clusterissuer letsencrypt-staging
```

## Acceptance Criteria

- [ ] **AC1:** HTTP request to a non-wildcard hostname reaches Traefik through CF Tunnel catch-all rule
- [ ] **AC2:** CNAME to `cname.{baseDomain}` resolves and routes correctly to Traefik
- [ ] **AC3:** HTTP request to `/.well-known/acme-challenge/*` is NOT redirected to HTTPS by Cloudflare
- [ ] **AC4:** cert-manager HTTP-01 solver creates Ingress/IngressRoute that Traefik processes
- [ ] **AC5:** Let's Encrypt staging certificate is issued successfully for the custom hostname

## Fallback Plan

If HTTP-01 fails through CF Tunnel:

### Option A: Cloudflare for SaaS (SSL for SaaS)
- Cloudflare issues certs automatically for custom domains
- Requires Cloudflare Enterprise or Advanced Certificate Manager ($10/month)
- No cert-manager needed — Cloudflare handles everything at the edge
- Implementation: API calls CF API to add custom hostname

### Option B: DNS-01 with Cloudflare API Token (for domains on Cloudflare)
- Works only if user's domain DNS is on Cloudflare
- cert-manager creates TXT record via CF API
- Limited to Cloudflare users only

### Option C: Manual Certificate Upload
- User uploads their own TLS cert via `loopzero domain cert-upload`
- Most flexible but worst DX
- Last resort only

## Timeline

- Deploy B1 changes (catch-all + cname record): 1 day (Pulumi up)
- Run validation steps 1-3: 1-2 days
- Document results: same day
- **Total: 2-3 days after B1 deploy**
