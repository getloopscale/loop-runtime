# Fulcio OIDC Trust Overrides — M07 Keyless 3.85

Helm values fragments + reference manifests para configurar Fulcio trust chain
em k3d local + production DOKS. Resultado final: cosign keyless signing GREEN
end-to-end (sign + attest + Rekor URL populated em DB).

## Histórico

Esses files foram criados iterativamente durante 9 Ralph iterations (2026-05-28)
investigando HTTP 400 "error processing identity token" do Fulcio em LIVE builds.

Iter 1-9 testaram (todos sem resolver):
- `cenario-c-issuer-mismatch.yaml` — added 2nd OIDCIssuer entry (iss already matched)
- `cenario-a-k3s-config.yaml` — K3s `anonymous-auth=true` + `jwks_uri=DNS`
- `cenario-a-anonymous-discovery.yaml` — ClusterRoleBinding `system:unauthenticated`
- `cenario-d-tls-ca-investigation.md` — investigation doc (Fulcio extraEnv blocker)

Research-1 (2026-05-28) via WebSearch + WebFetch achou fix oficial:
- **`cenario-a-cacert-trust.yaml`** — Fulcio config schema `OIDCIssuer.CACert`
  PEM field documented em pkg.go.dev/github.com/sigstore/fulcio/pkg/config.

## Apply flow (após cleanroom)

**Pré-requisitos** (Phase -1 do plan):

1. **K3s anonymous-auth=true**: `docker cp cenario-a-k3s-config.yaml k3d-loopzero-build-server-0:/etc/rancher/k3s/config.yaml && docker restart k3d-loopzero-build-server-0`
2. **CRB system:unauthenticated**: `kubectl apply -f cenario-a-anonymous-discovery.yaml`
3. **Sigstore stack**: `helm install scaffold sigstore/scaffold --version 0.6.108 --wait`

**Fix CACert** (Phase 0-2 do plan):

```bash
# Extract K3s server CA
docker exec k3d-loopzero-build-server-0 \
  cat /var/lib/rancher/k3s/server/tls/server-ca.crt > /tmp/k3s-server-ca.pem

# Render template with PEM inline (python — indented 10 spaces para YAML block scalar)
python3 <<'PY'
import re
with open('/tmp/k3s-server-ca.pem') as f:
    ca = f.read().rstrip()
ca_indented = '\n'.join('          ' + line for line in ca.splitlines())
with open('infra/pulumi-platform/values-fulcio-oidc-overrides/cenario-a-cacert-trust.yaml') as f:
    template = f.read()
rendered = re.sub(
    r'^<K3S_SERVER_CA_PEM_PLACEHOLDER>$',
    ca_indented,
    template,
    flags=re.MULTILINE,
)
with open('/tmp/cenario-a-cacert-trust-rendered.yaml', 'w') as f:
    f.write(rendered)
PY

# Apply (upsert idempotent)
helm --kubeconfig=$(k3d kubeconfig write loopzero-build) \
  upgrade --install fulcio sigstore/fulcio --version 2.9.1 \
  -n fulcio-system -f /tmp/cenario-a-cacert-trust-rendered.yaml
```

## Verify

```bash
KC_BUILD=$(k3d kubeconfig write loopzero-build)

# Config has CACert
kubectl --kubeconfig="$KC_BUILD" -n fulcio-system get cm fulcio-server-config \
  -o jsonpath='{.data.config\.json}' | jq '.OIDCIssuers'

# Fulcio Running
kubectl --kubeconfig="$KC_BUILD" -n fulcio-system get pods -l app.kubernetes.io/name=fulcio

# Manual probe (Phase 3)
# See plan T3.1 for full curl command — expected STATUS=201 + cert chain
```

## Why CACert resolves (root cause)

K3s API server uses self-signed CA (`/var/lib/rancher/k3s/server/tls/server-ca.crt`).
Fulcio pod (Alpine distroless image) has empty `/etc/ssl/certs/ca-certificates.crt`.
Fulcio go-oidc client uses default `crypto/tls.Config{RootCAs: nil}` which falls
back to system CA pool — but Alpine bundle in Fulcio image doesn't include K3s CA.

Without CACert config:
- Discovery endpoint `/.well-known/openid-configuration`: anon 200 OK (no TLS verify needed for response body)
- JWKS endpoint `/openid/v1/jwks`: anon 200 OK (same)
- BUT: when Fulcio TRIES to verify SA token signature, it must re-fetch JWKS via go-oidc TLS-strict client → TLS handshake fails silently → token verify fails → HTTP 400 "error processing identity token"

With CACert config: Fulcio go-oidc client uses provided PEM as trust root for K3s API server TLS verification → handshake OK → JWKS fetch OK → token verify OK → HTTP 201 + signing cert.

## References

- [Fulcio config package (pkg.go.dev)](https://pkg.go.dev/github.com/sigstore/fulcio/pkg/config)
- [Sigstore OIDC docs](https://docs.sigstore.dev/certificate_authority/oidc-in-fulcio/)
- [Fulcio OIDC docs (main branch)](https://github.com/sigstore/fulcio/blob/main/docs/oidc.md)
- [cosign issue #3160 — generic 400 symptom](https://github.com/sigstore/cosign/issues/3160)
- [Sigstore blog — Running Locally](https://blog.sigstore.dev/a-guide-to-running-sigstore-locally-f312dfac0682/)
- [Chainguard — Zero-friction K8s keyless](https://www.chainguard.dev/unchained/zero-friction-keyless-signing-with-kubernetes)

## Plans

- `git show 4f728eb7a:docs/plans/2026-05-28-M07-fulcio-cacert-keyless-LIVE-plan.md` v1.1 — current execution plan
- `git show 4f728eb7a:docs/plans/2026-05-28-M07-fulcio-oidc-trust-to-3-85-plan.md` v1.7 — predecessor (deferred, will be amended v1.8 cancelled by current plan)
- `git show 4f728eb7a:docs/plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md` v1.2 — derived plan (the M2 deferral is reverted in v1.3)
