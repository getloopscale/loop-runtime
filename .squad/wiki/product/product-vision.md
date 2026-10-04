# Product vision: loopzero

Agreed in the brainstorm session of 2026-10-04
(`.squad/records/brainstorms/2026-10-04-session.md`). Phase 1 of 4; not yet signed.

## Who it is for

A small company, or a team of up to about ten people, with nobody whose job is platform work. They
build AI agents with LoopZeroKit, often as a monorepo of several agents and the services around them,
and they need those agents in production with TLS, a preview per pull request and a rollback they
trust.

The same team may run on our cloud or in its own cloud account (BYOC). BYOC serves the same user,
with one more requirement: their agents and data stay on infrastructure they own and are isolated
from every other customer.

Not decided yet: when the solo developer on the free plan and the paying team of ten want different
things, which one wins.

## The problem

Today this team picks between two bad paths.

A general PaaS (Heroku, Render, Fly, Vercel) fits one app. Each agent and service of a monorepo
becomes separate configuration, a preview of the whole set per pull request is hard or expensive,
and the bill grows without warning.

Building their own platform (Kubernetes, Terraform, Helm, CI, certificates) takes weeks and turns
someone on the team into the part-time infrastructure person. A team that also needs the agents to
run in its own account, isolated, has to build that isolation itself.

The cost is product time spent on infrastructure, deploys and rollbacks treated as risky events,
and fear of the monthly bill.

Evidence class: inferred. No user research exists in the repository and no observed case was given
in the session.

## What it is

LoopZero is where a small company puts the agents it built with LoopZeroKit into production, with no
platform team. You run `loopzero deploy` in your project, and every agent and service goes live over
HTTPS, isolated from other customers, with a preview environment per pull request and a rollback in
one command. It runs on our cloud, where you pay for what runs and set your own spending ceiling, or
in your own cloud account (BYOC), with the same experience and the compute billed to you by your
cloud provider. You never see Kubernetes.

The CLI is the primary interface; the dashboard follows it.

## What it is NOT

- Not a Kubernetes platform. The user never writes a manifest, a chart or a cluster definition, in
  our cloud or in theirs.
- Not an agent framework. LoopZeroKit is the framework and LoopZero is where it runs; the seam between them
  is `.loopzero/services.json`, and neither imports the other.
- Not built for companies that already have a platform team. They can assemble this from parts, and
  serving their requirements first would pull the product away from the team that cannot.
