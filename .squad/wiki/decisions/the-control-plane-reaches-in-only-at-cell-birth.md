---
type: decision
title: The control plane reaches in only at cell birth; Day-2 is egress-only
date: 2026-09-11
status: stable
---

# The control plane reaches in only at cell birth; Day-2 is egress-only

**Decided 2026-09-11**, closing the last open question of `cycle-design` — D2's *"does the control
plane assume a role inward, or does an egress-only agent reach out?"*, which the market research
ranked as the finding with the most leverage precisely because the mechanism is not yet built.

## The decision

```
cell birth   CAPI/CAPA reaches in to create the cell    push · minimum scope · revoked after
Day-2        the cell reaches out                        pull · egress-only · no stored kubeconfig
```

No persistent inbound access, and no per-cell kubeconfig held for the life of the customer
relationship.

## Why this shape and not either extreme

**Everything persistent goes pull, because that is where the industry converged — unanimously.**

The three hyperscalers manage clusters outside their own cloud the same way. Azure Arc runs *"a
reverse proxy agent on the cluster that securely starts a session with the Arc service in an
outbound manner"*, with *"no inbound ports needing to be opened"*; GKE's Connect gateway is a
Google-hosted reverse proxy for clusters that can only egress; AWS SSM Agent is the same pattern.
The literature's own summary: *"all these solutions follow the same pattern — agents establish
secure outbound connections without requiring inbound firewall access."*

The BYOC vendors landed in the same place. Best practice is stated as *"egress-only, pull-based
connectivity — no inbound access, no SSH, no VPN, and no write permissions"*. **WarpStream needs
zero cross-account IAM**, and Confluent adopted that architecture after acquiring them.

**And the definition names where we were:** *"genuine zero-trust BYOC separates vendors who park a
data plane in your VPC but still require broad IAM roles from true egress-only architectures."*
CAPA creates the cell in the customer's account AND the control plane held a kubeconfig for it —
the middle position that reads as BYOC and is not zero-trust.

**Birth stays push, because the alternative contradicts the product.** Making the customer create
the cell breaks the vision's non-goal — *"not managed Kubernetes: the customer never operates a
cluster"* — and deletes `PIECE-9` and `REQ-14` with it. The word that matters in the zero-trust
definition is **persistent**: *"the vendor holds no persistent inbound access or stored
credentials."* Authority used once to provision and then revoked is not persistent access, and
this repository already recorded the seam — CAPI's authority at cell birth *"does not survive the
bootstrap."*

## What the two spikes found

**Spike 1 — `argocd-agent` maturity.** Active and cadenced: created 2023-10, pushed 2026-09-10,
589 stars, 63 open issues, releases every ~2 months (`v0.7.4` March → `v0.10.0` August 2026). Red
Hat ships it in OpenShift GitOps 1.19, which is the strongest signal available.

**It is still `v0.x` after three years, and that is the honest caveat.** A pre-1.0 API can break,
and this decision depends on it for Day-2. The mitigation is that the decision is about
*direction* — the mechanism can be `argocd-agent`, or an agent of our own on the same shape, and
the shape is what every vendor above converged on.

**Spike 2 — and it inverted the question.** `cross_cluster_token_review.go` was the reason to
believe we needed to reach in. Read: it *"validates the Bearer token from **ESO running in a cell
runtime cluster** against THAT cluster's K8s API via TokenReview."*

So the cell **already calls us** — ESO pulls secrets outbound — and the only reason we reach back
is to *verify who called*. That is not the control plane operating the cell; it is identity
validation, and it has a standard solution that needs no inbound path at all: a signed token.

**We already have that mechanism, built against the same threat.** `loopzero-contracts/serviceauth`
provides *"canonical payload construction, Ed25519 sign/verify, replay-window validation, and
nonce-store deduplication"*, and its own header states why: the previous design *"trusted identity
headers behind a single shared secret… a leak yielded full tenant escalation."* The `jwt` package
carries RSA public-key validation with `kid` lookup.

**So the pull model is not a new capability here. It is the capability we already built, applied
to a seam still using the older shape.**

## What this costs, stated

- **`argocd-agent` is pre-1.0.** Adopting it for Day-2 accepts an API that may break. Named above
  rather than discovered later.
- **Debugging changes shape.** Nuon's objection — least privilege *"prohibits most day-to-day
  debugging"* — is real, and the answer is that debugging goes through the agent rather than
  through a role. If the agent cannot express an operation, that operation stops being possible,
  and that is the trade.
- **Revocation has to be built.** *"Revoked after"* is a sentence today, not a mechanism. An
  authority that is granted and never revoked is the persistent access this decision exists to
  remove, so the revocation is part of the work and not a footnote to it.

## What is NOT decided here

Whether the Day-2 agent is `argocd-agent` or ours. This decision fixes the **direction** — no
persistent inbound, no stored per-cell kubeconfig — and leaves the implementation to the item that
does it.

## Related

- [`two-doors-managed-and-byoc.md`](two-doors-managed-and-byoc.md) — which door this governs
- [`byoc-as-the-only-mode-has-no-precedent.md`](byoc-as-the-only-mode-has-no-precedent.md) — where
  WarpStream and Nuon's objections to the assumed cross-account role were first recorded
- [`../design/trust.md`](../design/trust.md) — D2, the drawing this answers
