# Customer domains at the edge: Caddy with on-demand TLS

Status: accepted, 2026-11-26, by the maintainer.

## Context

A tenant app is served on a platform name, and a customer can attach a domain of their own
to it. In LoopScale's production the apps sit behind one CloudFront distribution with one
alias (`*.getloopscale.app`) and one ACM certificate. A customer's domain has no alias and
no certificate there, so it cannot be served. The `app-router` function already routes a
host to its cell, so what is missing is the TLS termination and the way in for the host.

The decision had three requirements:

- The customer creates a CNAME and HTTPS works with no action from us.
- The operating cost does not grow with each domain.
- The same mechanism runs on staging, which is an AWS emulator, and on a self-hosted
  installation, which may not be on AWS at all.

## Options

| Option | Benefit | Cost | Fit |
|---|---|---|---|
| A. Leave it | no work | customer domains do not work | fails the first requirement |
| B. Add each domain as an alias and a SAN on the current distribution | no new part | a certificate reissue and a distribution deploy per domain, 100 aliases per distribution by default, every customer on one certificate | a handful of domains only |
| C. CloudFront multi-tenant distributions, one tenant per domain | managed certificates, CDN and WAF kept, a default quota of 1,000,000 tenants per account | AWS only, the staging emulator has no such resource, the price per tenant was not found, and whether a function with a KeyValueStore runs on a tenant was not confirmed | good on AWS, poor for staging and self-hosted |
| D. Caddy with on-demand TLS behind a network load balancer, for customer domains only | one mechanism on staging, production and self-hosted, no lock-in | a fleet to operate (shared certificate storage, Let's Encrypt rate limits), and customer domains get no CloudFront CDN or WAF | matches the earlier decisions against lock-in |

The CloudFront figures come from the AWS documentation as read on 2026-11-26 (quotas,
alternate domain names and HTTPS, multi-tenant distributions). None of them was measured
by us, and nothing was created on AWS to test option C.

## Decision

1. Customer domains enter through a Caddy fleet that issues certificates on demand. Before
   it asks for a certificate, Caddy asks loopzero-api whether the host is an active custom
   domain (`GET /internal/edge/route?domain=<host>`), and it refuses names under the
   platform's own domain. The answer also names the cell, and Caddy proxies the request to
   that cell's load balancer.
2. Platform names stay where they are: `*.getloopscale.app` on CloudFront in production.
3. In production the fleet sits behind a network load balancer, and customers point their
   CNAME at it. It needs certificate storage shared by its instances.
4. Staging already runs this, with one Caddy on the host. On 2026-11-26
   `https://staging-app.theokit.dev/health` answered 200 with a Let's Encrypt certificate
   issued on demand, and an unregistered host was refused.

## Consequences

- A customer domain has no CDN and no WAF from CloudFront. If either becomes a product
  requirement for customer domains, option C is the one to revisit, starting with a spike
  that proves a function with a KeyValueStore on a multi-tenant distribution and reads the
  price per tenant.
- The fleet is ours to run: capacity, upgrades, the shared certificate store, and staying
  inside Let's Encrypt's rate limits.
- The loopback site that answers Caddy's question must name no host. `forward_auth` passes
  the viewer's Host along, and Caddy answers a Host no site names with an empty 200
  (getloopscale/loopzero#177).
- Production's fleet is not built yet. Its Terraform, the load balancer and the shared
  storage are work that follows this record.

## Related

- getloopscale/loopzero#173, #174: the provisioner and the staging edge.
- `infra/staging/edge/Caddyfile`: the configuration staging runs.
