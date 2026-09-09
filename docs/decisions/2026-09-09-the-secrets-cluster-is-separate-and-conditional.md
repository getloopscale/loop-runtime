# The secrets cluster is separate, and it is conditional

- **Status:** PROPOSED — supersedes § Q2 of a RATIFIED ADR, so ratification is sponsor-direct and not implied by this file existing
- **Date:** 2026-09-09
- **Tier:** T3 (cluster topology, credential blast radius)
- **Supersedes:** `docs/wiki/decisions/2026-06-02-openbao-adoption.md` § Q2 — *"Topology: `vault-system` namespace in mgmt cluster"*
- **Source:** issue #357; measurement during the cluster-management extraction design (`docs/wiki/grills/cluster-management-extraction-grill.md`)

## Context

Three artifacts disagreed about where OpenBao runs, and the disagreement was load-bearing.

**§ Q2 of the adoption ADR (RATIFIED 2026-06-02)** decided a dedicated `vault-system` namespace **in
the mgmt cluster**, explicitly *"not dedicated DOKS cluster"*, and priced the alternative:

> *"Dedicated DOKS cluster (~$60-100/mo additional) **deferred** until SOC 2 physical-separation
> requirement OR multi-tenant Enterprise tier triggers it."*

**The tree runs a separate cluster anyway** — `infra/k3d/secrets-cluster.yaml`,
`infra/helmfile/secrets.helmfile.yaml.gotmpl` with `kubeContext: k3d-loopzero-secrets` and a header that
reads *"ISOLATED from mgmt"*, reached by `task k3d:secrets:create` + `k3d:secrets:bootstrap`.

**The helmfile names its own origin as a decision that has no document.** It cites *"VK.4 of
sponsor-direct decision 2026-06-02 (vault cluster isolation T3)"*. Measured: three artifacts cite
VK.4 (this helmfile, `docs/operations/vault-cluster-local-runbook.md:11`, and § D8 of the CAPI ADR)
and **none decides it**. No amendment from 2026-06-02 or 2026-06-03 touches Q2 — the only `Q2` in
those records is `Q2-F1`, about AWS bootstrap auth and region.

**And a second topology decision leans on the first.** § D8 of
`docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` gives Cluster API its own cluster and
justifies it as *"precedente interno direto, não analogia"*, pointing at the separate OpenBao cluster
and at a rule file — `.claude/rules/cleanroom-up-live-test-mandate.md` — that **resolves nowhere in
the workspace**, because `.claude/` is not in this repository.

So a topology the ADR record rejects was holding up a topology decision that is still `PROPOSED`,
through a citation nobody can read.

## Decision

**1 · The secrets cluster is separate. Recorded here, with the reason that actually holds.**

The reason is blast radius, and it is already stated well in § D8 — this ADR gives it a home in the
decision tree instead of leaving it as a supporting note inside an ADR about something else:

> The credential that creates and destroys infrastructure in customer accounts is in the same class as
> the vault unseal key. The mgmt cluster runs ~50 releases — the whole product. Adding fleet authority
> to that makes one mgmt compromise a compromise of every customer at once.

**2 · The composition is conditional on the secret backend, and always was.**

Measured 2026-09-09 against `secrets.helmfile.yaml.gotmpl`:

| Fact | Value |
|---|---|
| OpenBao releases behind an `installed:` guard | 4 — `openbao-cert`, `openbao`, `openbao-init`, `openbao-snapshot-agent` |
| The guard | `{{ .Values \| get "openbaoEnabled" false }}` — **default off** |
| Environment files that set it | **1 of 8** — only `vault-local.yaml` |
| Callers | only `taskfiles/k3d.yml`, always `-e vault-local --kube-context k3d-loopzero-secrets` |
| Production target | the header names *"DOKS `loopzero-secrets-prod` **in future**"* |

This is not a change introduced here — the gate exists in the helmfile. What is new is that the
documents stop describing the fifth cluster as unconditional.

The contract that makes it conditional was already published: `charts/loopzero-api/SECRETS.md` and
`charts/loopzero-cloud/SECRETS.md` state that secrets are set *"per installation, in your target's native
store (AWS Secrets Manager / GCP Secret Manager)"*, and AM6.5's anti-lock-in ADR makes ESO
**recommended, not required**. **OpenBao is this platform's operational choice, not a product
requirement** — a self-hoster bringing their own secret store runs a **4-cluster** topology.

**3 · The Q2 deferral trigger has since fired, independently.**

Q2 deferred isolation until *"SOC 2 physical-separation requirement OR multi-tenant Enterprise
tier"*. There is now a concrete self-host Enterprise customer. So the isolation is defensible **today
by Q2's own criterion** — but it was implemented before the trigger, for the blast-radius reason
above. Both justifications are recorded so nobody has to reconstruct which one applied when.

**4 · § D8's citation is repointed at this file.**

`docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` § D8 no longer cites the absent rule
file. Its argument is unchanged; only its source becomes readable. That ADR is itself `PROPOSED`, so
this touches nothing ratified.

## Consequences

**Enables**

- A reader asking *"why does OpenBao get its own cluster?"* gets the answer from the decision tree
  instead of the opposite answer.
- § D8 of the CAPI ADR rests on a document in this repository, so the Cluster API cluster decision
  can be ratified without inheriting a dangling reference.
- The self-host topology question shrinks: four clusters when the customer brings their own secret
  store, five when they adopt OpenBao.

**Constrains**

- `compose/` must express the conditionality **declaratively** when the composition moves to
  `loopzero-cluster-management` — not as an `if` discovered at install time. Same rule that repository
  already applies to the BYOC cell composition.
- The prohibition on installing OpenBao into core stands for **our** topology. What this ADR does not
  reconstruct is the *"forbidden in every tier"* wording, whose only source was the absent rule file.

**Does not change**

- The topology on disk. Nothing is created, moved or deleted by this ADR.
- The cost figure. The ~$60-100/mo in Q2 is quoted, not re-measured; if it moved, the number belongs
  in a follow-up, not in an unverified edit here.

## Alternatives rejected

| Option | Why rejected |
|---|---|
| Amend § Q2 in place | It is RATIFIED. Editing a ratified decision to match the present destroys the evidence of what was decided then — the same reason `docs/README.md` marks dated records as audit trail. Supersede, don't rewrite. |
| Leave § D8 citing the absent file | It is the foundation of a `PROPOSED` T3 decision. Ratifying on an unreadable source means the next reader cannot check the claim. |
| Restore `cleanroom-up-live-test-mandate.md` into the repo | `.claude/` is deliberately not versioned here (it is an installed plugin with its own repository). Copying one rule out of it would create a second original that drifts. |
| Treat the fifth cluster as unconditional and drop the gate | The gate is what makes a 4-cluster self-host installation possible, and it is the shipped behaviour. Removing it to match the prose would be changing the product to match a document. |

## What this does not decide

- **Whether OpenBao remains the right choice after the AWS re-platform.** The adoption ADR accepted
  AWS KMS as an irreducible cross-cloud dependency; after the AM-series that dependency stops being
  cross-cloud, and the operational burden Q2's successor listed under *Constrains* (upgrades,
  snapshots, audit-log retention, Shamir custody, quarterly break-glass drill) is exactly what a
  managed store removes. That is a separate ADR with its own measurement, not a paragraph here.
- **Whether `loopzero-secrets-prod` gets created.** The helmfile still calls it future work.
