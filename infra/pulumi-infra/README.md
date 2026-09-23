# loopzero-infra (infra/pulumi-infra)

> **LEGACY — retired at AM10.** This Pulumi project is the AS-IS L1 layer,
> live until the AM9 cutover and retired at AM10 by the CTO mandate of
> 2026-06-17 (ADR `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`).
> The target infrastructure is Terraform in `../loopzero-infra-modules` and
> `../loopzero-infra-live`. Do not add resources, providers or cells here; keep
> changes to what the running stack needs until the cutover.

Cloud primitives for the split layout: VPC, DOKS clusters (declared as a
topology list, `src/topology/`), managed PostgreSQL, Spaces buckets, CDN and
container registry. Stateful resources live here and change rarely.

## Project name collision

`infra/pulumi` and `infra/pulumi-infra` both declare `name: loopzero-infra` in
`Pulumi.yaml`. The name keys the state backend and the config namespace
(`loopzero-infra:*`), so it is not renamed in place: renaming would orphan every
existing stack. Always select the project by directory
(`pulumi --cwd infra/pulumi-infra ...`), never by a bare stack name.
