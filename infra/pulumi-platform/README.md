# loopzero-platform (infra/pulumi-platform)

> **LEGACY — retired at AM10.** This Pulumi project is the AS-IS L1 layer,
> live until the AM9 cutover and retired at AM10 by the CTO mandate of
> 2026-06-17 (ADR `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`).
> The target infrastructure is Terraform in `../loopzero-infra-modules` and
> `../loopzero-infra-live`. Do not add resources, providers or cells here; keep
> changes to what the running stack needs until the cutover.

Platform services installed on top of `loopzero-infra` outputs (read through a
StackReference): the `services/*.ts` components wired from `src/index.ts`.
