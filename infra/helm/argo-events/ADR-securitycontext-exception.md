# securityContext exception — `argo-events`

**Date:** 2026-08-12 · **Status:** ACCEPTED · **Item:** `B-045` (bullet 3)
**Review by:** 2026-11-09

## Why this chart does not declare the four fields

It **has no pod spec of its own**. `Chart.yaml` declares a dependency on the upstream `argo-events`
chart 2.4.4 (pinned, with Renovate raising patches), and this directory is a *wrapper*: it supplies
values, not container templates.

Hardening here is not adding `securityContext` to a template we own — it is overriding the
upstream's value tree, whose schema shifts between minor versions. Done blindly, a `values` key the
upstream renames stops applying **silently**, and the chart runs unhardened again with the gate
green. That failure mode is worse than the gap: it hides it.

## What this exception does NOT say

It does not say argo-events should run privileged. It says hardening it is a job of **overlaying
values against a versioned upstream schema**, which has to be verified against the real render — and
that doing it without that verification produces a false guarantee.

## Exit condition

When the upstream chart version is raised: read its `values.yaml`, map the `securityContext` fields
it exposes, apply them here, and confirm with `helm template` that they appear in the rendered
manifest. Once confirmed, this file is **deleted**, not updated.
