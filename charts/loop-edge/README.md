# loop-edge

The Caddy fleet that serves tenants' custom domains. The decision behind it is in
`docs/decisions/2026-11-26-customer-domains-at-the-edge.md`.

A customer points a CNAME at the fleet's load balancer. At the first request for the host,
Caddy asks loop-runtime-api whether the host is an active custom domain
(`GET /internal/edge/route?domain=<host>`). If it is, Caddy obtains a certificate for it and
proxies the request to the cell the answer names. Names under the platform's own domains are
refused before a certificate is asked for.

## What it needs

- The `loop-edge` image (`infra/images/loop-edge`): Caddy built with the Redis storage module.
  Stock Caddy refuses the configuration this chart writes.
- A Redis or Valkey every pod reaches. Certificates, ACME accounts and the locks that keep
  two pods from asking for the same certificate live there, so pods hold no state.
  When a private authority signs the store's certificate, `storage.caConfigMap` names it.
- The Secret with the bearer loop-runtime-api expects on the route lookup
  (`loop-runtime-edge-route-token` by default).
- A load balancer that passes TCP 80 and 443 through. The default Service annotations ask the
  AWS Load Balancer Controller for an internet-facing network load balancer. Port 80 has to be
  public: the ACME HTTP challenge arrives there.

## Required values

| Value | Meaning |
|---|---|
| `image.tag` | The image tag CI published. |
| `platformDomains` | The platform's domains. No name equal to or under them gets a certificate here. |
| `acme.email` | The contact of the ACME account (not needed with `acme.issuer: internal`). |
| `storage.host` | The Redis or Valkey host. |
| `cells` | Each cell and the host its load balancer answers for. |

`values.yaml` documents the rest.

## Limits

- The visitor's address is not passed on: the chart does not configure PROXY protocol, and
  what the cell sees depends on how the load balancer preserves the client address.
- `acme.issuer: internal` issues from Caddy's own authority, which no browser trusts. It is
  for an environment with no public address, where it still exercises the lookup, the shared
  store and the proxying.
- A changed Caddyfile rolls the pods. Caddy runs without its admin endpoint.
