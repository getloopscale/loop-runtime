# Scenario D — TLS CA mount investigation (M07 plan 2026-05-28 T2.1)

## Why this is NOT a simple values override

Investigation 2026-05-28 (Ralph iter 3): `sigstore/fulcio` chart **v2.9.1**
does NOT expose `extraVolumes`/`extraVolumeMounts`/`extraEnv` keys.

`helm show values sigstore/fulcio --version 2.9.1` returns only:
- `config.contents` (top-level — for OIDCIssuers/MetaIssuers)
- `server.{replicaCount,name,svcPort,...}`
- `server.secret` (single signing key Secret)
- Standard pod spec basics

**No customization hook for trust store mount.**

## Reality check before applying fix

K8s pods get `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`
mounted **automatically** when they have a ServiceAccount (default
behavior, NOT chart-controlled). Fulcio pod has SA → CA is at
`/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`.

The question is: does Fulcio v1.8.6 **trust** that CA when calling
`kubernetes.default.svc.cluster.local/openid/v1/jwks`?

Default Go HTTP client uses system CA pool (`/etc/ssl/certs/`).
Alpine-based images (Fulcio uses distroless/alpine) typically have
empty `/etc/ssl/certs/`. So Fulcio CALL to K8s API would fail TLS
handshake even with CA available at SA mount path.

## Fix paths (operator decision required)

### Path 1 — chart supports `extraVolumes` in NEWER version
- Check `helm show values sigstore/fulcio --version 2.10+` for new keys
- If newer chart exposes `extraVolumes`, upgrade chart AND add overrides

### Path 2 — Helm postRenderer (kustomize patch)
- Write kustomization that patches Deployment with `volumes` + `volumeMounts`
  + `SSL_CERT_FILE` env var pointing to mounted CA
- Apply via `helm upgrade ... --post-renderer ./kustomize-patch.sh`

### Path 3 — Custom Fulcio image with CA in /etc/ssl/certs/
- Build derived image: `FROM ghcr.io/sigstore/fulcio:v1.8.6` + `COPY ca.crt /etc/ssl/certs/`
- Override `server.image.repository` to point at custom image
- Operator overhead: re-build on chart upgrade

### Path 4 — Use cluster public CA for K3s API (infra change)
- Replace K3s self-signed cert with cert issued by recognized CA
- Out of scope for this plan; infra-level change

## Recommendation

**Phase 1 diagnose verdict will tell us if D is actually the blocker.**
The fulcio_log_excerpt from probe 3 will say something like:
- `x509: certificate signed by unknown authority` → confirmed D
- Other patterns → a different scenario

**Only if verdict=D, operator chooses Path 1-4.** Path 2 (postRenderer)
recommended for KISS local k3d — keeps chart pinned, isolated patch.

DO NOT pre-apply the Scenario D fix without verdict — it's complex and
chart-specific. Scenario C (issuer mismatch, 45% probability) has clean
values override (see `scenario-c-issuer-mismatch.yaml`).
