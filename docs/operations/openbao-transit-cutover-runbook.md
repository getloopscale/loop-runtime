---
type: operational-runbook
title: OpenBao Transit Cutover Runbook
---

# OpenBao Transit Cutover Runbook

> **Plan:** `2026-06-10-j22-openbao-transit-eso-sync` v1.2 § Phase 6 T6.1
> Final Phase Integration Validation. This runbook closes the 4 operator
> HARD STOPs that the engineering side (commits `3a393d4f..8ad2524f`)
> cannot fire from a Claude session.
>
> **Status:** Engineering-side 18/18 tasks closed. Operator action required
> below before `<promise>IMPLEMENTATION_COMPLETE</promise>` is honored.
>
> **Per `feedback_post_feature_live_regression_workflow_2026_06_09`:**
> Sponsor walkthrough (step 4) is the only path to canonical closure —
> hermetic + LIVE bash alone are necessary-but-NOT-sufficient.

## Pre-flight (5 minutes — sponsor + Claude together)

```bash
# Verify the engineering chain landed in commit history
git log --oneline | grep "openbao-transit" | head -20

# Re-run hermetic dogfood to lock the baseline before LIVE
task test:dogfood:secrets-openbao --hermetic-only
# Expected: verdict=PASS pass=3 fail=0

# Capture pre-cutover steady-state baseline (per .claude/rules/steady-state-baseline.md)
infra/scripts/local/baseline-capture.sh \
  --output docs/operations/baselines/2026-06-10-pre-cutover-openbao-transit.json \
  --layers all
```

If any step above fails → STOP. Surface to Claude before proceeding.

---

## Step 1 — Provision OpenBao server + AppRole (T6.1.1)

> **Pre-requisite OpenBao server.** The transit chain requires a reachable
> OpenBao server at the URL declared in `OPENBAO_VAULT_ADDR`. Local k3d
> dev-public typically targets a helmfile-deployed instance in the
> `vault-system` namespace.
>
> **Major delegation update (iter 23, cleanroom-audit 2026-06-10):** the
> canonical `task k3d:vault:bootstrap` + `helmfile sync --selector
> name=openbao-init` pipeline ALREADY provisions ~80% of what J-22
> Transit needs out-of-the-box (transit engine enabled, master keys
> created, `loopzero-api-transit-encrypt` policy written). This runbook
> previously duplicated those steps; the duplication is now removed.
> Only the AppRole-specific configuration remains — per J-22 plan T2.1
> ADR which chose AppRole over K8s auth.

### 1.1 — Bootstrap OpenBao server + base provisioning (delegated)

Run the canonical pipeline — installs OpenBao chart + does Shamir init
+ provisions transit engine + loopzero-api-transit-encrypt policy + K8s
auth mounts (all idempotent):

```bash
task k3d:vault:bootstrap        # creates loopzero-vault cluster + OpenBao
                                 # chart (PRODLIKE.2 — HA Raft + Shamir 5/3)

# Then follow the operator-only Shamir ceremony:
# docs/operations/openbao-init-runbook.md
#   bao operator init -key-shares=5 -key-threshold=3 -format=json
#   bao operator unseal <shard1> <shard2> <shard3>
#   kubectl create secret generic openbao-init-token --from-literal=BAO_TOKEN=<root>

helmfile sync --selector name=openbao-init   # T5.1-T5.9 provisioning
                                              # Enables: transit engine,
                                              # kv-v2, database engine,
                                              # kubernetes engine, K8s
                                              # auth mounts (k8s-mgmt /
                                              # k8s-build / k8s-cell-*),
                                              # loopzero-api-* policies
```

What openbao-init does NOT provide:
- AppRole authentication method (uses K8s auth instead per the upstream
  openbao-adoption plan ADR)
- Per-tenant transit key auto-creation (J-22 EC-06 wires this client-side
  via `pkg/vault/transit_client.go`)

These remaining ~20% are step 1.2 through 1.4 below.

### 1.2 — Enable AppRole auth method + write the J-22 transit policy

```bash
# Port-forward OpenBao for local CLI access (loopzero-vault cluster, not mgmt)
kubectl --context k3d-loopzero-vault -n vault-system port-forward svc/openbao 8200:8200 &
export VAULT_ADDR=https://localhost:8200
export VAULT_TOKEN=$(kubectl --context k3d-loopzero-vault -n vault-system get secret openbao-init-token -o jsonpath='{.data.BAO_TOKEN}' | base64 -d)
export VAULT_CACERT=/tmp/openbao-ca.pem
kubectl --context k3d-loopzero-vault -n vault-system get secret openbao-tls -o jsonpath='{.data.ca\.crt}' | base64 -d > $VAULT_CACERT

# Enable AppRole auth method (idempotent — bao returns "already enabled" OK)
bao auth enable approle 2>&1 | grep -v "already enabled" || true

# Write the J-22 AppRole transit policy. NOTE: this is a NARROWER variant
# of the existing `loopzero-api-transit-encrypt` policy provisioned by
# openbao-init. J-22 plan T2.1 + EC-06 self-heal requires `create` on
# transit/keys/loopzero-tenants-* — openbao-init's existing policy uses `+`
# wildcard which is too broad for the per-tenant key path convention.
# We add the dedicated AppRole-bound policy alongside the K8s-auth one.
#
# CANONICAL key-name format: `loopzero-tenants-<tenantID>` (hyphen-joined
# single segment). LIVE-caught 2026-06-10 J-22 walkthrough: an earlier
# draft of this runbook used slash-separated `loopzero-tenants/<id>` paths
# which OpenBao transit rejects with 404 "unsupported path" because the
# engine parses the slash as endpoint delimiter and `loopzero-tenants` as
# the key name. The hyphen convention is implemented in
# pkg/vault/transit_client.go via the tenantKeyName() helper; this
# policy MUST match.
bao policy write loopzero-api-approle - <<'EOF'
path "transit/encrypt/loopzero-tenants-*" {
  capabilities = ["update"]
}
path "transit/decrypt/loopzero-tenants-*" {
  capabilities = ["update"]
}
path "transit/keys/loopzero-tenants-*" {
  # NOTE: "update" is required IN ADDITION to "create" because the
  # `bao write` CLI (and the Go client's POST to /v1/transit/keys/<name>)
  # uses HTTP PUT, which OpenBao policy-enforces as "update" semantics
  # even when the key does not yet exist. LIVE-caught 2026-06-10 J-22
  # walkthrough: with only ["create", "read"], the AppRole token's
  # `bao token capabilities` reported "create, read" but the actual PUT
  # returned 403 "permission denied" because PUT requires "update".
  capabilities = ["create", "read", "update"]
}
EOF
```

### 1.3 — Create the AppRole + capture role_id

```bash
# Create the role bound to the new policy. 24h secret_id TTL is canonical
# for dev-public; prod uses 1h + response-wrapping auto-rotation (separate
# runbook for prod cutover ships when J-22 promotes).
bao write auth/approle/role/loopzero-api \
  token_policies="loopzero-api-approle" \
  token_ttl=1h \
  token_max_ttl=4h \
  secret_id_ttl=24h \
  secret_id_num_uses=0

# Capture both credential halves.
ROLE_ID=$(bao read -field=role_id auth/approle/role/loopzero-api/role-id)
SECRET_ID=$(bao write -force -field=secret_id auth/approle/role/loopzero-api/secret-id)

echo "role_id:   $ROLE_ID"
echo "secret_id: $SECRET_ID"
```

**Save both values for step 2.** The secret_id is short-lived (24h TTL) — record the mint timestamp; if step 2 takes >24h to start, re-mint via the same `bao write` command.

### 1.4 — Capture the TLS SPKI pin (defense vs rogue CA)

```bash
# Extract the leaf cert's SubjectPublicKeyInfo SHA-256
SPKI_PIN=$(echo | openssl s_client -connect localhost:8200 -servername openbao 2>/dev/null | \
  openssl x509 -pubkey -noout | \
  openssl pkey -pubin -outform DER | \
  openssl dgst -sha256 -hex | awk '{print $2}')

echo "spki_pin: $SPKI_PIN"
```

Save this value too. The LoopZero TransitClient refuses to talk to any server presenting a different SPKI — defense-in-depth against rogue CA mis-issuance (`pkg/vault/transit_client.go` `ErrTLSPinMismatch`).

---

## Open Question — Cross-cluster reachability (iter 23 cleanroom-audit catch)

**Found during productive-idle iter 23 (2026-06-10):** `task k3d:vault:bootstrap` deploys OpenBao to k3d cluster `loopzero-vault` (NOT mgmt). But the loopzero-api chart values default `OPENBAO_VAULT_ADDR=https://openbao.vault-system.svc.cluster.local:8200` — that's in-cluster service DNS resolvable only from WITHIN the same cluster.

loopzero-api runs in `k3d-loopzero-core`. Reaching openbao in `k3d-loopzero-vault` requires one of:

| Option | Implementation | Trade-off |
|---|---|---|
| (a) Shared Docker network DNS | Override `OPENBAO_VAULT_ADDR` to `https://k3d-loopzero-vault-server-0:NODEPORT/` | Works for k3d only; not portable to prod (DOKS); SPKI pin breaks because hostname doesn't match cert SAN |
| (b) NodePort + host networking | Expose openbao Service as NodePort, point loopzero-api at `k3d-loopzero-vault-serverlb:NODEPORT` | k3d-specific; same SPKI issue |
| (c) Deploy OpenBao in mgmt cluster's vault-system ns | Override `task k3d:vault:bootstrap --kube-context k3d-loopzero-core`. Or create a parallel helmfile release in mgmt's helmfile chain. | Simplest reachability; loses isolation between control plane and secret storage; deviates from canonical 5-cluster topology |
| (d) Add a Service-of-services bridge | Create a Service in mgmt with `type: ExternalName` pointing at the k3d-loopzero-vault container hostname | Requires manual port-forward maintenance OR cluster-spanning network policy work |

**Sponsor decision needed before LIVE step 1:**

Recommended for dev-public iter 1 LIVE: **option (c)** — single-cluster reachability is the lowest-risk path. The 5-cluster isolation pattern is canonical for prod but adds operational complexity that LIVE test 1 doesn't need to validate. Promote to multi-cluster (option a or d) when J-22 promotes from dev to staging.

**If sponsor picks option (c):** a turn-key task shortcut shipped iter 24 collapses step 1.1 (~5min) into one command:

```bash
task k3d:vault:bootstrap-in-mgmt

# Then the operator Shamir init ceremony (unchanged from canonical):
# bao operator init -key-shares=5 -key-threshold=3 -format=json
# bao operator unseal <shard1> <shard2> <shard3>
# kubectl create secret generic openbao-init-token --from-literal=BAO_TOKEN=<root>

helmfile sync --selector name=openbao-init   # T5.1-T5.9 provisioning
                                              # NOTE: under option (c) the
                                              # default kube-context already
                                              # points at mgmt, so this works
                                              # without --kube-context override
```

The shortcut is contract-locked via `infra/tests/check-vault-bootstrap-in-mgmt-task_test.sh` (18 hermetic assertions). When the upstream `task k3d:vault:bootstrap` evolves, the test fails so the shortcut catches up.

Until sponsor decides, steps 2-4 below assume reachability is handled — they take `OPENBAO_VAULT_ADDR` as an opaque URL.

---

## Step 2 — Wire credentials into loopzero-api Deployment (T6.1.2)

> **Canonical pattern per `feedback_secret_canonical_no_hardcode`:**
> non-sensitive flags inline via `env:` map in helmfile values; sensitive
> credentials via K8s Secret + `envFrom: secretRef` with `optional: true`.
> NEVER `kubectl set env` with literal secret values — drift between
> cluster + chart, audit-log silent.

### 2.1 — Create the K8s Secret holding the AppRole secret_id

```bash
kubectl --context k3d-loopzero-core -n loopzero-system create secret generic loopzero-openbao-approle \
  --from-literal=OPENBAO_VAULT_SECRET_ID="$SECRET_ID" \
  --dry-run=client -o yaml | kubectl --context k3d-loopzero-core apply -f -
```

Verify:

```bash
kubectl --context k3d-loopzero-core -n loopzero-system get secret loopzero-openbao-approle -o jsonpath='{.metadata.resourceVersion}'
```

Record the resourceVersion in the baseline so the next baseline-capture can prove
the secret was rotated.

### 2.2 — Add the 4 non-sensitive env vars + envFrom block to helmfile values

The chart deployment template already has:

```yaml
{{- range $k, $v := .Values.env }}
- name: {{ $k | quote }}
  value: {{ $v | quote }}
{{- end }}
```

So 4 literals are wired via `env:` in `infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl`:

```yaml
env:
  # ... existing keys ...
  OPENBAO_TRANSIT_ENABLED: "true"
  OPENBAO_VAULT_ADDR: "https://openbao.vault-system.svc.cluster.local:8200"
  OPENBAO_VAULT_ROLE_ID: "<role_id from step 1.4>"
  OPENBAO_SPKI_PIN_SHA256: "<spki_pin from step 1.5>"
```

The 5th env var (`OPENBAO_VAULT_SECRET_ID`) loads via the chart's existing
envFrom block — see step 2.3 for the one-line chart edit.

### 2.3 — Chart envFrom: secretRef for the AppRole secret_id

Edit `infra/helm/loopzero-api/templates/deployment.yaml` envFrom block (around line 243):

```yaml
          envFrom:
            - configMapRef:
                name: {{ .Values.wiringConfigMap | quote }}
                optional: true
            - secretRef:
                name: {{ .Values.dbCredentialsSecret | quote }}
                optional: true
            - secretRef:
                name: loopzero-service-auth
                optional: true
            # ... existing entries ...
            # T2.5 + T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2:
            # AppRole secret_id is the only OpenBao credential that MUST come
            # from a K8s Secret (per feedback_secret_canonical_no_hardcode).
            # Bootstrap: kubectl create secret loopzero-openbao-approle with key
            # OPENBAO_VAULT_SECRET_ID; mint command in runbook step 1.4.
            # optional=true so dev/test envs without OpenBao still boot.
            - secretRef:
                name: loopzero-openbao-approle
                optional: true
```

Apply via:

```bash
helmfile -e dev-public apply -l name=loopzero-api
```

### 2.3.bis — ESO ClusterSecretStore enable (iter 25 productive-idle shortcut)

Iter 25 shipped a pre-authored helmfile env overlay so step 2.3 of ESO chart configuration is one line + apply (no chart-values surgery under live-cluster pressure):

1. Edit `infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl`:
   - Set `openbaoTransit.enabled: true` (currently `false` — safe default)
   - Paste the role_id captured in step 1.3 into `roleId: ""` (currently empty placeholder)
2. Create the Secret in the `external-secrets` namespace too (step 2.1 also created it in `loopzero-system` for the loopzero-api envFrom path; ESO reads ClusterSecretStore secretRefs from its own controller ns):

```bash
kubectl --context k3d-loopzero-core -n external-secrets create secret generic loopzero-openbao-approle \
  --from-literal=OPENBAO_VAULT_SECRET_ID="$SECRET_ID" \
  --dry-run=client -o yaml | kubectl --context k3d-loopzero-core apply -f -
```

3. Apply the chart:

```bash
helmfile -e dev-public apply -l name=loopzero-external-secrets
```

4. Verify the ClusterSecretStore is Ready (ESO controller polls every 5min by default; first reconcile is immediate):

```bash
kubectl --context k3d-loopzero-core get clustersecretstore openbao-transit -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
# Expected: "True"
```

If `False` → kubectl describe the CSS, check ESO controller logs for "AppRole login failed" (role_id mismatch OR secret_id rotated OR network policy blocking).

The overlay contract is locked by `infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh` (18 hermetic assertions). If the overlay schema ever drifts (e.g., chart renames `openbaoTransit:` top-level key, or secretRef shape changes), the test catches it.

### 2.4 — Restart loopzero-api + verify boot log

```bash
kubectl --context k3d-loopzero-core -n loopzero-system rollout restart deploy/loopzero-api
kubectl --context k3d-loopzero-core -n loopzero-system rollout status deploy/loopzero-api --timeout=120s
```

Expected boot log (per `api/internal/config/config.go` EC-05 absorption):

```text
crypto_mode=transit openbao_addr=https://openbao.vault-system.svc.cluster.local:8200 aes_key_ignored=false spki_pinned=true
openbao transit client constructed addr=https://openbao.vault-system.svc.cluster.local:8200 spki_pinned=true rate_limit_per_sec=10 lru_max_entries=10000
secret store wired with openbao transit dispatch legacy_aes_key_present=false
```

```bash
# Capture the boot log slice for the post-fix baseline
kubectl --context k3d-loopzero-core -n loopzero-system logs deploy/loopzero-api --since=2m | grep -E "crypto_mode|openbao transit|secret store wired"
```

If `crypto_mode=transit` is NOT in the log → STOP. The 4 OpenBao env vars
were not picked up. Common causes:
- Helmfile values typo (check `helmfile -e dev-public template -l name=loopzero-api | grep OPENBAO`)
- Secret `loopzero-openbao-approle` not in the right namespace (must be `loopzero-system`)
- Chart was not re-applied (re-run `helmfile apply` and verify the Deployment's
  envFrom array has the new entry via `kubectl get deploy loopzero-api -o yaml | grep -A2 loopzero-openbao`)

---

## Step 3 — LIVE 6-scenario zero-leak test (T6.1.3)

```bash
task test:dogfood:secrets-openbao --live-only
```

Expected: `verdict=PASS pass=6 fail=0` in ~10s. The 6 scenarios per
`infra/tests/e2e/dev-public-secrets-zero-leak_test.sh`:

| # | Layer | Probe | Pass when… |
|---|---|---|---|
| A | PG `encrypted_value` | `SELECT encrypted_value FROM secrets WHERE name=<sentinel>` | starts with `vault:v1:` |
| B | Workload K8s Secret | `kubectl get secret -n tn-<id> <name> -o jsonpath='{.data.<key>}' | base64 -d` | plaintext (via ESO) |
| C | Pod env via printenv | `kubectl exec <pod> -- printenv <key>` | plaintext (via envFrom) |
| D | kubectl logs | `kubectl logs <loopzero-api-pod> --since=5m | grep <sentinel>` | sentinel NEVER appears |
| E | kubectl describe pods | `kubectl describe pod <tenant-pod> | grep <sentinel>` | sentinel NEVER appears |
| F | Tenant-namespace scope | `kubectl get secret -A -o name | grep <sentinel>` | ONLY in tenant ns, NOT in loopzero-system |

Any FAIL → STOP. Diagnose the leak BEFORE step 4. The most common failure
mode is scenario D: loopzero-api logs the plaintext via a slog.Info or an error
unwrap. Fix via `pkg/vault/redaction.go` slog.Handler wrapper extension.

---

## Step 4 — Sponsor LIVE browser walkthrough (T6.1.4)

> **HARD STOP — sponsor verbatim "Funcionou!" required.** Hermetic GREEN +
> LIVE GREEN are necessary-but-NOT-sufficient per the per-feature LIVE
> workflow mandate. The 4 LIVE-only bugs caught during the M2 sponsor
> walkthrough (`feedback_post_feature_live_regression_workflow_2026_06_09`)
> are why this step is non-negotiable.

### Architectural finding — ESO controller absent in mgmt (iter 31 LIVE pre-flight)

**Discovered 2026-06-10 during the first LIVE attempt:** `kubectl get pods -n external-secrets` returns no resources in k3d-loopzero-core. ESO controller is deployed in workload + runtime-2 clusters only (`infra/helmfile/runtime.helmfile.yaml.gotmpl` line 108-110), NOT in mgmt. The full T4.1 6/6 scenarios (B/C/F that depend on ESO sync) cannot succeed under Option (c) — they require either:

- (B-extend) ESO controller ALSO deployed in mgmt — add release to `infra/helmfile/core.helmfile.yaml.gotmpl`
- (C-extend) Cross-cluster ExternalSecret targeting — design pattern where mgmt ESO reads from mgmt OpenBao + writes K8s Secrets to workload cluster (requires multi-cluster secret push)

Either path requires ~1h+ of additional architectural work. For dev-public iter 1 LIVE, the pragmatic scope is the **write-path subset** below.

### 4.0a — Option A LIVE: write-path subset (iter 31 productive-idle)

Run only scenarios A + D + E that validate the canonical "no plaintext at rest" mandate WITHOUT requiring ESO. Total ~10s execution. **This is the canonical proof of the J-22 security mandate.**

```bash
# Pre-requisite: scenarios A+D+E require:
#   1. OpenBao in mgmt cluster (task k3d:vault:bootstrap-in-mgmt — runbook step 1.1)
#   2. AppRole configured (runbook steps 1.2-1.4)
#   3. K8s Secret loopzero-openbao-approle in loopzero-system ns (runbook step 2.1)
#   4. OPENBAO_TRANSIT_ENABLED=true + 4 env vars on loopzero-api (runbook step 2.2)
#   5. loopzero-api restarted + boot log shows crypto_mode=transit (runbook step 2.4)
#   6. PG port-forward localhost:15432 (per LOOPZERO_PG_CONN default)
#   7. Sponsor JWT at ~/.loopzero/local-dogfood-token.txt

bash infra/tests/e2e/dev-public-secrets-write-path-subset_test.sh
```

Expected output when canonical:

```text
[probe] POST /api/v1/secrets/bulk with sentinel=zeroleak-<uuid>
[scenario A] psql query: encrypted_value prefix
  PASS: PG encrypted_value starts with vault:v1:
[scenario D] kubectl logs grep sentinel in loopzero-api (last 5min)
  PASS: 0 sentinel matches in loopzero-api logs
[scenario E] kubectl describe pods across all namespaces for value leak
  PASS: 0 value matches in describe pods output

=== Verdict ===
verdict=PASS pass=3 fail=0 mode=write-path-subset
scenarios: A=PASS  D=PASS  E=PASS
deferred:  B C F (require ESO in mgmt cluster — see runbook § Open Question)
```

Scenario A is THE canonical zero-leak proof: PG `encrypted_value` column starts with `vault:v1:` (NOT plaintext bytes nor legacy AES nonce+ciphertext). Demonstrates the entire iter 18 wire-up chain: `loopzero deploy` → `routes/secrets.go Bulk` → `h.transitClient.Encrypt` → `store.Set` → PG row.

After Option A passes, the sponsor walkthrough (step 4.3) becomes the human-eyes verification: confirm the dashboard accepted the secret + the PG inspection terminal shows the `vault:v1:` ciphertext.

The contract is locked by `infra/tests/check-write-path-subset-syntax_test.sh` (27 hermetic assertions). When future work closes the ESO-in-mgmt gap, the subset script can be deprecated in favor of the full 6/6 T4.1 LIVE script — until then this is the canonical dev-public iter 1 LIVE proof.

### 4.0 — Rehearsal smoke test (iter 26 productive-idle shortcut)

Before resetting DB + committing to the full walkthrough, run the LIVE preflight rehearsal to confirm the wire chain is ready. ~5s total, 5 kubectl probes, NO probe-secret POSTed, NO encrypt path exercised, NO PG row written:

```bash
task test:dogfood:secrets-openbao:rehearsal
```

Expected output when wire chain is READY:

```text
[rehearsal] LIVE preflight probes:
  PASS: R1 ESO controller pod Running
  PASS: R2 ClusterSecretStore openbao-transit Ready=True
  PASS: R3 Secret loopzero-openbao-approle in external-secrets ns
  PASS: R4 Secret loopzero-openbao-approle in loopzero-system ns
  PASS: R5 loopzero-api OPENBAO_TRANSIT_ENABLED=true env present
[rehearsal] verdict=PASS pass=5 fail=0
```

If ANY probe FAILs, do NOT proceed to step 4.1. Each probe maps to a specific runbook step that did not complete:

| Failed probe | Cause | Fix |
|---|---|---|
| R1 | ESO chart not synced | `helmfile -e dev-public sync -l name=external-secrets` |
| R2 | ESO can't authenticate to OpenBao via AppRole | Re-check role_id (step 1.3), secret_id rotation (step 1.4), TLS pin (step 1.5), or OpenBao reachability (Open Question § cross-cluster) |
| R3 | Secret loopzero-openbao-approle missing in external-secrets ns | Re-run step 2.3.bis kubectl create secret |
| R4 | Secret loopzero-openbao-approle missing in loopzero-system ns | Re-run step 2.1 kubectl create secret |
| R5 | loopzero-api Deployment env var unflipped | Edit `infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl` to add `OPENBAO_TRANSIT_ENABLED: "true"` + `helmfile apply -l name=loopzero-api` |

Only when rehearsal verdict=PASS proceed to step 4.1.

### 4.1 — Reset DB for clean E2E

```bash
# reset-for-e2e.sh was deleted in M15 Wave A (2026-07-10) per sponsor mandate.
# Canonical replacement is a full Cleanroom TOTAL (task clean:all && task setup)
# followed by seed-post-cleanroom-all.sh, per rules/cleanroom-up-live-test-mandate.md.
task clean:all && task setup && bash infra/scripts/local/seed-post-cleanroom-all.sh --verify
```

### 4.2 — Seed canonical user

```bash
infra/scripts/local/seed-admin-loopzero-dev.sh
```

### 4.3 — Browser walkthrough script

Sponsor opens dev-public dashboard at `https://app-dev.loopzero.example`, logs in as `admin@loopzero.example`, then:

1. Navigate to `/projects/<test-project>/secrets`.
2. Click **Add secret** → enter `LIVE_PROBE_<timestamp>` + value `liveprobe-<uuid>` → environments `production` → **Save**.
3. Inspect PG (terminal, sponsor + Claude side-by-side):
   ```bash
   psql -h localhost -p 15432 -U loopzero -d loopzero -c \
     "SELECT name, substring(encode(encrypted_value, 'escape') for 30) as preview FROM secrets WHERE name LIKE 'LIVE_PROBE_%'"
   ```
   Expected: `preview` column starts with `vault:v1:` (NOT random bytes — that would indicate AES legacy path still wired).
4. Trigger a redeploy of the test app (so workload pod picks up the new ESO-synced env). Verify pod env:
   ```bash
   POD=$(kubectl --context k3d-loopzero-workload -n tn-<id> get pod -o name | head -1)
   kubectl --context k3d-loopzero-workload -n tn-<id> exec $POD -- printenv LIVE_PROBE_<timestamp>
   ```
   Expected: prints `liveprobe-<uuid>` exactly (plaintext via ESO + envFrom).
5. Sponsor verbatim: *"Funcionou!"* OR surface what didn't work.

### 4.4 — Capture post-cutover baseline

```bash
infra/scripts/local/baseline-capture.sh \
  --output docs/operations/baselines/2026-06-10-post-j22-openbao-transit.json \
  --layers all

# Diff pre/post (per .claude/rules/steady-state-baseline.md)
infra/scripts/local/baseline-diff.sh \
  docs/operations/baselines/2026-06-10-pre-cutover-openbao-transit.json \
  docs/operations/baselines/2026-06-10-post-j22-openbao-transit.json
```

Expected verdict: `OK` with deltas limited to (a) loopzero-api Deployment podSpec
(envFrom new entry), (b) new Secret `loopzero-openbao-approle` resourceVersion,
(c) secrets table column `encrypted_value` shape changed (vault:v1: prefix
on new rows). Any unrelated delta → investigate before declaring done.

---

## Rollback path (any step fails)

```bash
# Step 1 — revert chart envFrom (one-line removal)
git restore infra/helm/loopzero-api/templates/deployment.yaml
helmfile -e dev-public apply -l name=loopzero-api

# Step 2 — flip env back to aes-local
kubectl --context k3d-loopzero-core -n loopzero-system set env deploy/loopzero-api OPENBAO_TRANSIT_ENABLED=false
kubectl --context k3d-loopzero-core -n loopzero-system rollout restart deploy/loopzero-api

# Step 3 — verify boot log shows crypto_mode=aes-local
kubectl --context k3d-loopzero-core -n loopzero-system logs deploy/loopzero-api --since=2m | grep crypto_mode

# Step 4 — secrets written during the failed cutover stay in PG with vault:v1: prefix.
# The dual-read path in secret_wrapper.decryptFromStore still detects the prefix +
# decrypts via OpenBao IF OpenBao is reachable. If you fully tear down OpenBao,
# those rows become unreadable — DO NOT delete OpenBao before re-encrypting via
# upstream openbao plan's Day 60 cron OR a manual one-shot script.
```

---

## What this runbook does NOT do

- **Production cutover.** Dev-public AppRole TTL is 24h; prod requires 1h
  + auto-rotation via OpenBao's response-wrapping mint flow. Separate
  runbook ships when J-22 promotes to prod.
- **Multi-cluster.** Per ADR D4 of the plan: vault-system namespace is
  core-cluster-only. Workload + build clusters reach it via cross-cluster
  ClusterCredentials, NOT direct connection.
- **AES-to-transit migration of existing rows.** Pre-T2.5 rows in the
  secrets table carry the AES envelope. The dual-read path in
  `decryptFromStore` reads them transparently (prefix detection + AES
  fallback). A one-shot re-encrypt cron ships in the upstream
  `2026-06-02-openbao-adoption` plan Phase 10 (T10.3 — Day 60).

---

## Audit trail

After step 4.4 sponsor verbatim "Funcionou!":

1. Commit the post-cutover baseline JSON to `docs/operations/baselines/`.
2. Update `CHANGELOG.md` `[Unreleased] § Security` bullet for J-22 OpenBao
   Transit with the cutover timestamp + sponsor verbatim quote.
3. Emit `<promise>IMPLEMENTATION_COMPLETE</promise>` for ralph-loop.
4. Update memory `feedback_post_feature_live_regression_workflow_2026_06_09`
   counter: another LIVE-validated feature shipped.

---

## Related

- **Plan:** `.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md`
- **Edge-case review:** `.claude/knowledge-base/reviews/2026-06-10-j22-openbao-transit-eso-sync-edge-cases-2026-06-10.md`
- **Engineering closure baseline:** `docs/operations/baselines/2026-06-10-post-engineering-j22-openbao-transit.md`
- **Steady-state baseline rule:** `.claude/rules/steady-state-baseline.md`
- **Per-feature LIVE workflow mandate:** memory `feedback_post_feature_live_regression_workflow_2026_06_09`
- **Sponsor scope-extension pattern:** memory `feedback_sponsor_scope_extension_pattern`
- **Anti-hardcode gate:** memory `feedback_no_hardcode_gate_2026_06_09`
- **Secret canonical pattern:** memory `feedback_secret_canonical_no_hardcode`
- **Sibling cutover runbook (M07 keyless):** [`docs/operations/m07-cutover-sequence.md`](./m07-cutover-sequence.md)
