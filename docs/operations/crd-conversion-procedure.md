# CRD Conversion Procedure (loopzero.io CRDs)

> **Status: DORMANT.** M14 gap #4 installed the Hub/Convertible *pattern*
> (marker + round-trip harness) but the conversion webhook itself is inactive
> because there is only one CRD version today. This document is the runbook for
> when the FIRST backward-incompatible schema change forces a v2.

## Why the pattern ships dormant

Per plan ADR D5: the `/convert` webhook + the CRD `spec.conversion` patch are
100% coupled to a spoke version that does not exist yet. Installing the webhook
without a v2 would be dead config. What ships now:

- `operators/api/v1/tenant_conversion.go` — `func (*Tenant) Hub() {}` marks v1 as
  the storage/hub version.
- `roundTripThroughHub` — the harness a future spoke is validated against.
- Tests proving the marker + harness work (`TestTenantConversion_*`).

Reference implementation studied: KServe
`.claude/knowledge-base/references/kserve/pkg/apis/serving/v1beta1/inference_service_conversion.go:19`
(Hub marker) + `pkg/apis/serving/v1alpha1/llm_inference_service_conversion.go:45`
(spoke ConvertTo/ConvertFrom) + `config/crd/minimal/llmisvc/llmisvc_conversion_webhook_patch.yaml`
(CRD patch).

## Activation runbook (when v2 lands)

1. **Create the v2 API package** (`operators/api/v2/tenant_types.go`) with the
   new schema. Keep v1 as the hub (do NOT move the `Hub()` marker unless v2
   becomes the storage version).
2. **Implement the spoke** on the non-hub version: `ConvertTo(conversion.Hub)` +
   `ConvertFrom(conversion.Hub)` mapping every field. Follow the kserve spoke
   shape (field-by-field, no data loss).
3. **Extend the harness** — `roundTripThroughHub` gains real ConvertTo→ConvertFrom
   assertions per the kserve `PreservesCriticality` / `NoCriticality` cases.
4. **Register both versions** in the scheme (`SchemeBuilder.Register`).
5. **Patch the CRD manifest** with `spec.conversion.strategy: Webhook` +
   `webhook.clientConfig.service` pointing at `/convert` + the
   `cert-manager.io/inject-ca-from` annotation (mirror the kserve patch).
6. **Wire the webhook server** in the operator's manager setup
   (`ctrl.NewWebhookManagedBy(mgr).For(&v1.Tenant{}).Complete()`), matching the
   existing webhook registration path.
7. **Validate** with envtest: apply a v1 CR, read it as v2, assert conversion;
   and vice-versa.

Until step 1 happens, the marker + harness are the whole of gap #4.
