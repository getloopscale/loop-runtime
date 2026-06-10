# Final Phase Evidence — Plan M07-to-3.85 v1.1 Ralph iter 2

**Data:** 2026-05-27T23:00Z (Ralph iter 2 completion)

## Gates GREEN

| Gate | Status | Evidence |
|------|--------|----------|
| `task doctor` | **41 PASS / 0 FAIL** | identical baseline 2026-05-25 |
| Sigstore stack LIVE | **3/3 + 3/3 Running** | sigstore-system (Fulcio + Rekor + Redis) + trillian-system (logserver + logsigner + mysql) |
| VAP enforce | **Deny,Audit** | loopzero-require-image-digest-binding warn→deny per `helm upgrade --set vaps.requireImageDigestEnforce=true` |
| Cosign attest LIVE (keyed) | **build-18e1e3afd2a4ef54 SUCCEEDED 447s** | Attest pod log "Attestation complete; Rekor entry: unknown" — keyed mode esperado vazio |
| Chaos drill cosign-fulcio-kill LIVE | **PASS** | Fulcio killed → rescheduled 58s → endpoint LIVE |
| `/dogfood` Ginkgo journey suite | **112 PASS / 0 FAIL / 20 SKIP** | 66min em fresh Sigstore-active infra (this evidence) |

## Cleanroom validation

`task doctor` 41/0 confirma infra stable. Sigstore stack survived dogfood suite (175min uptime).

Ginkgo journey suite com Sigstore + VAP enforce ATIVO em paralelo passou TODOS os 112 specs sem regression.

## Scope analysis

**Cap deste cycle: 3.75 / 4.0** (não 3.85 ceiling original):
- LIVE cosign keyless attest mode NÃO exercise-able (loopzero-forge `ProjectedVolumeModel` upstream stub)
- Workaround rejeitado per CTO mandate "SEM WORKAROUNDS, NIVEL FAANG"
- Operator chose ship 7/9 M2 phases LIVE em keyed mode + Sigstore stack RUNNING (validates infra deployment + attest chain mechanism + all complementary phases)

## Path para 3.85 (post-cycle)

1. **loopzero-forge upstream PR** — add ServiceAccountToken source + Audience field para `model.ProjectedVolumeModel`
2. **workflow_builder.go** — adicionar pod-level projected volume com audience "sigstore"
3. **cosign_attest.go** — VolumeMount já documentado mas requer workflow-level injection
4. **LIVE keyless build retry** — re-run Phase 9 com SIGSTORE_ID_TOKEN mountable + assert Rekor URL non-empty
5. **Re-audit terceiro** — registrar 3.75 → 3.85

## Path para 3.9 cap (out of scope deste plan)

- Prod DNS allocation (`fulcio.loopzero.example` + `rekor.loopzero.example` + `ct.loopzero.example`)
- Prod cluster activation com cross-cluster OIDC URL
- Plano separado
