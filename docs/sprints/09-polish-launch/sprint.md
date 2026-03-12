# Sprint 9 — Polish + Launch

> **Weeks:** 26-27
> **System Design Reference:** §23 (Sprint 9)

---

## Objective

The product is ready for the first external user. Every item in the launch checklist passes.

## Product Exit Gate

A non-team-member completes full onboarding (install → login → init → deploy → logs → secret pull → rollback) in < 10 minutes without help.

## CLI Commands Delivered

None — this sprint is polish and launch preparation.

## Tasks

### External validation

```
[ ] Non-team-member completes onboarding unassisted in < 10 minutes
[ ] loopzero.yaml generation tested against at least 3 real-world monorepos
[ ] CLI tested on macOS, Linux, and Windows (WSL2)
[ ] npm package published and installable via npm install -g loopzero-cli
```

### Documentation

```
[ ] docs.loopzero.example: quickstart guide
[ ] docs.loopzero.example: loopzero.yaml reference (mirrors section 26)
[ ] docs.loopzero.example: CLI reference (mirrors section 25)
[ ] docs.loopzero.example: FAQ (common errors and solutions)
```

### Infrastructure

```
[ ] *.loopzero.example DNS operational
[ ] TLS certificates (cert-manager + DNS-01) operational
[ ] Production Kubernetes cluster provisioned (managed, HA etcd)
[ ] PostgreSQL WAL archiving to object storage (PITR within 7 days)
[ ] Weekly backup restore test automated
```

### Business

```
[ ] Billing functional (user can sign up and pay)
[ ] Data retention and deletion policy documented (LGPD/GDPR minimum)
[ ] Terms of service and privacy policy published
```

### Final gate

```
[ ] All E2E + chaos tests pass
[ ] Load test passes (50 concurrent deploys)
[ ] Every row in section 12.1 failure matrix has a passing test
[ ] Supply chain signing verified end-to-end
[ ] All items in "Ready for first paying user" checklist below pass
```

## Ready for First Paying User — Product Exit Criteria

> Every item must be verified before the first external user deploys on LoopZero. Items may be added but never removed without CTO approval.

```
[ ] Onboarding works end-to-end: npm install → loopzero login → loopzero init → loopzero deploy → live URLs
[ ] A non-team-member completed onboarding in < 10 minutes without help
[ ] loopzero.yaml generation produces correct output for at least 3 real-world monorepos
[ ] Error messages follow the format in section 25.12 — no internal terms leak to users
[ ] CLI outputs use product terminology (deploy, team, revert) — never internal (release, tenant, rollback)
[ ] --json output matches section 0.3 contract for all commands
[ ] Exit codes match section 0.3 contract (0-5)
[ ] CI mode (CI=true) works correctly (no spinners, no color, no interactive prompts)
[ ] Partial failure behavior matches section 0.3 (per-app status, exit code 5)
[ ] *.loopzero.example DNS and TLS are operational for production traffic
[ ] Usage-based billing or fixed plan billing is functional (user can sign up and pay)
[ ] docs.loopzero.example exists with: quickstart, loopzero.yaml reference, CLI reference, and FAQ
[ ] Monitoring alerts (section 15.5) are routed to an on-call channel
[ ] Runbooks exist for every P1 failure mode (PostgreSQL down, Gitea down, ArgoCD degraded)
[ ] Data retention and deletion policy is documented (LGPD/GDPR minimum)
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| North Star (5-minute promise) | §1 |
| Target persona — Lucas | §1 |
| CLI Product Contract | §0.2 |
| CLI Output & Error Contract | §0.3 |
| User Experience & Onboarding | §24 |
| loopzero.yaml complete specification | §26 |
| CLI Reference (all commands) | §25 |
| Failure modes (full matrix) | §12.1 |
| Alerting rules | §15.5 |
| v1 Known Risks | Appendix F |
