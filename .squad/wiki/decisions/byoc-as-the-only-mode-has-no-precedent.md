---
type: decision-review
title: BYOC as the only mode has no precedent found
date: 2026-09-11
status: stable
disposition: recorded — the decision stands, the risk is named
reviews: the 2026-09-11 decision to drop self-host
---

# BYOC as the only mode has no precedent found

> **SUPERSEDED IN PART, 2026-09-11** — by
> [`two-doors-managed-and-byoc.md`](two-doors-managed-and-byoc.md). This record asked *"what is the
> second door?"* and the answer is the managed PaaS, which was already in the code and not in the
> documents. **The headline finding — BYOC as the only mode — no longer describes this product.**
>
> What is NOT superseded: the mechanism findings. WarpStream and Nuon argue against the assumed
> cross-account role, and the permission-surface measurements (Databricks ~45-70+ AWS actions,
> Redpanda's `PassRole`) stand. Those are about HOW the BYOC door works, and it is still unbuilt.

On 2026-09-11 self-host was dropped and **BYOC became the whole offer**. This records what
research into the market found afterwards, because four of its findings challenge that decision
and one of them challenges the shape of our design rather than the decision.

**The decision stands.** This is not a reversal; it is the evidence written down while reversing
is still cheap.

## How to read the grading

The research marked every claim **[READ]** (the page was fetched and read), **[SEARCH]** (only a
search summary), or **[COULD NOT VERIFY]**. That grading is preserved below and must not be
flattened — a claim that was only summarised is not a claim that was read.

## What it confirms

**BYOC is established, and specifically in our tier.** Not in data — in PaaS. Porter, Qovery and
Northflank all deploy a Kubernetes PaaS into the customer's cloud account with a vendor-hosted
control plane **[SEARCH]**. Baseten launched the same shape for inference **[SEARCH]**. ClickHouse
BYOC is GA on AWS **[READ]**; Fivetran Hybrid Deployment **[READ]**; Redpanda BYOC **[READ]**;
Confluent via WarpStream **[SEARCH]**.

An ecosystem exists solely to sell BYOC operations to vendors — Nuon, Omnistrate. That is evidence
the pattern is real **and** that operating it is hard enough to sustain a tools market.

**Our run and build genuinely stay in their account.** C1 put the build plane on the customer's
infrastructure and REQ-11 makes it a network-capture criterion. That half of the promise is ours to
keep and is written down.

## What it refutes

### 1. BYOC as the ONLY mode — no precedent found

In every vendor verified, BYOC is a **third** option, never the only one.

| Vendor | The other modes |
|---|---|
| Confluent | fully managed · self-managed · BYOC |
| MongoDB | Atlas · Enterprise Advanced self-managed |
| Temporal | Cloud · self-hosted |
| Porter | managed control plane · self-hosted control plane |

**No vendor was found whose only enterprise mode is BYOC with a vendor-hosted control plane.** That
does not make the decision wrong. It makes it unprecedented, which is a different and smaller claim,
and the honest one.

### 2. The largest vendor of customer-account compute is walking back from it

Databricks — whose compute has always run in the customer's account — went serverless GA in January
2026, and **classic workspace creation with a Databricks-managed VPC is planned for deprecation**
**[SEARCH]**. If customer-account compute were the destination, the vendor with the most experience
of it would not be reversing.

### 3. "A scoped cross-account role" does not survive a security review

Measured from the vendors' own permission documents:

- **Databricks [READ]** — ~45 actions for a customer-managed VPC, ~70+ otherwise. Includes
  `RunInstances`, `TerminateInstances`, `AuthorizeSecurityGroupIngress` and `PutRolePolicy`. The last
  two are escalation-relevant on their own.
- **Redpanda [READ]** — EKS lifecycle, IAM role and policy creation and deletion, OIDC provider
  setup, `PassRole`, VPC and peering, Route 53, S3, Secrets Manager deletion.

### 4. Two vendors argue publicly against OUR shape specifically

Not against BYOC — against the **assumed cross-account role**, which is what our D5 uses.

WarpStream **[READ]**:
> *"BYOC vendor often has the ability to manage a huge range of cloud infrastructure, including
> security policies and resources for your VPC, service accounts, subnetworks, IAM roles, firewall
> rules, and storage buckets. … But wouldn't it be better if external access wasn't required at
> all?"*

Nuon **[READ]**, on why least privilege does not hold in practice:
> *"handling the minimal set of permissions prohibits most day-to-day debugging"* — so *"the vendor
> and customer continually go back and forth to request new permissions."*

Both propose the same alternative: an agent **inside** the customer account, **egress-only, with no
inbound path**. Fivetran states the same posture flatly — *"Fivetran never initiates inbound
connections to your local environment"* **[READ]**.

**This is the finding with the most leverage**, because it is about the mechanism rather than the
strategy, and the mechanism is not yet built. Changing it later means changing a live trust
relationship in every customer account.

## What it does not refute

**The governance answer is better than assumed, and it has a template.** ClickHouse publishes a
**BYOC security playbook** giving the customer queries to run against their own CloudTrail,
CloudWatch and EKS audit logs — including one that identifies vendor personnel by filtering
`user.username like /clickhouse.com/` **[READ]**. That is the concrete artefact an enterprise buyer
asks for, and it exists as prior art we can answer with.

**Our `ExternalId` design is already correct.** AWS documents that the vendor must generate it, that
it is not a secret, and that the vendor must actively test assuming the role WITHOUT it **[READ]**.
D10 already specifies exactly this: a random UUID per customer generated by us, plus active
onboarding validation that refuses the role if assuming without the `ExternalId` succeeds.

## The vendor-side cost, measured

| | |
|---|---|
| Version skew | ClickHouse built a bespoke **Kubernetes Upgrader** to manage EKS versions per customer account **[READ]** |
| Incident response | Tensor9 reports **60 of 72 hours** of one incident spent understanding the customer's environment, not fixing the bug **[READ]** |
| Shipping | Tensor9: *"every feature had to ship three times"* **[READ]** |
| Product surface | ClickHouse publishes a list of features **not supported** under BYOC — SQL Console, ClickPipes, autoscaling, AWS KMS **[READ]** |
| Debug surface | Cluster API failures span five layers, in an account you cannot log into. Notably **ClickHouse did not use CAPI** — CloudFormation + Crossplane + a custom upgrader **[READ]** |

## What could not be verified

**No vendor was found that offered BYOC and withdrew it.** The research looked specifically. Absence
of evidence is weak here — a withdrawal reaches customers rather than a blog.

**Astronomer is the suspected case and is UNCONFIRMED.** The Astro Hybrid cluster-settings page
carries no deprecation notice and the Terraform provider still exposes `HYBRID` **[READ]**. **Do not
repeat "Astronomer killed Hybrid" as fact.** If true, it is the best-matched withdrawal precedent
that exists, and confirming it is worth a direct question to them.

**The CAIQ question IDs come from a third-party index**, not the CSA original — the official PDF
would not parse. Confirm before quoting one to a customer.

## The question this opens

Not *"bring self-host back"*. The question is **what the second door is** — and every vendor
verified has one.

The candidate that fits the evidence: **fully managed on our account for most customers, BYOC as the
sovereignty option**. That is the configuration Confluent, MongoDB, Temporal and Porter all have,
and it inverts which mode is the exception.

Deciding it is the owner's. This document exists so the decision is made against the evidence rather
than against the absence of it.

## Related

- [`../product/objectives.md`](../product/objectives.md) — OBJ-3, the objective this reviews
- [`../product/trd.md`](../product/trd.md) — REQ-11, the perimeter promise that survives regardless
- [`../product/alignment.md`](../product/alignment.md) — where the reviewer meets this before signing
