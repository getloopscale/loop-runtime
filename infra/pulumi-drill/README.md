# loopzero-drill (infra/pulumi-drill)

> **LEGACY — retired at AM10.** This Pulumi project is the AS-IS L1 layer,
> live until the AM9 cutover and retired at AM10 by the CTO mandate of
> 2026-06-17 (ADR `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`).
> The target infrastructure is Terraform in `../loopzero-infra-modules` and
> `../loopzero-infra-live`. Do not add resources, providers or cells here; keep
> changes to what the running quarterly DR drill needs until the cutover.

The quarterly disaster-recovery drill stack, isolated from production (own
Cloudflare zone, DigitalOcean project, credentials and state bucket). Driven by
`task drill:*` (taskfiles/drill.yml).
