# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/) and this project adheres
to [Semantic Versioning](https://semver.org/).

> **History before 2026-09-11 is in git, not in this file.** The previous `CHANGELOG.md`
> held 4,802 lines and was removed in `4f728eb7a`. Recover it with
> `git show 4f728eb7a~1:CHANGELOG.md`, or read the release history with
> `git log --oneline`.

## [Unreleased]

### Added

- `loopzero-ops kubeconform-schemas --chart DIR` generates strict kubeconform schemas from the CRDs a chart's pinned dependency ships; the Velero schemas are committed and refreshed by `task helm:sync-kubeconform-schemas` (#391)

- `loopzero-ops validate-chart` renders one Helm chart and schema-validates the render, with no path on which a broken render reads as valid (#392)

- Quality gate `kubeconform-schemas-sync`, wired into `task quality:gates`, fails when a loopzero.io kubeconform schema is missing, stale or orphaned against its CRD (#389)

- `loopzero-ops kubeconform-schemas` (and `task helm:sync-kubeconform-schemas`) regenerates the loopzero.io kubeconform schemas from the CRDs, with a `--check` mode (#389)

- The `english-only` quality gate now also refuses Portuguese Go identifiers and Portuguese file or directory names in tracked paths; a Go identifier can keep its name only with the same inline exemption and reason (#380)

- `POST /internal/bootstrap` accepts `admin_user_id`, `admin_email` and `admin_display_name`, so a tenant can be provisioned for a login identity and not only a GitHub one (principle_violations#58, #383)

- `loopzero-ops quality english-only` gate, wired into `task quality:gates`: every git-tracked text file must be English, and a line keeps Portuguese only with an inline `english-only` exemption stating a reason of at least five words (#380)

- `loopzero-ops seed cluster-kubeconfigs` mints loopzero-api's build and cell-0 kubeconfigs with the scoped build identity, a 30-day token and each cluster's live CA (principle_violations#236)

- `loopzero-ops sql bootstrap-user` renders the seed that leaves the bootstrap user in exactly one tenant, shared by the local, drill and E2E seeds (principle_violations#236)

- `loopzero-ops dsn <field>` prints one part of a Postgres DSN read on stdin (user, password, host, port, database), accepting postgres:// and postgresql:// and failing instead of echoing an unparseable value (principle_violations#236)

- Operator charts accept `rbac.legacyClusterWideSecrets` (default `false`), a cutover escape hatch that restores cluster-wide Secret read for an operator image built before the scoped Secret cache (principle_violations#149, principle_violations#150)

- loopzero-ops quality contract-consumers reads infra/quality/contract-drops.txt: a JSON field removed on purpose is recorded per tag and file with its reason, stays listed on every run, and a recorded removal that no longer matches the diff fails the gate

- Environment status records `cellID`, the cell its namespace was provisioned in, so deletion targets that cell even after the tenant is re-pinned (principle_violations#59)

- Release inspection reports whether its audit trail was complete, unavailable or truncated, instead of showing an unreadable trail as empty (principle_violations#23)

- Quality gate `values-duplicate-keys` fails any Helm values file under charts/ or infra/helm/ that declares the same key twice.

- task dogfood:v2:journey JOURNEY=<label> runs one live dogfood journey (e.g. JOURNEY=j04).

- **api:** backlog B-119 — the build cache is keyed by name, so two tenants on the same
  stack share nothing
- **api:** backlog B-120 — a build with no lockfile warns and proceeds, so the platform
  ships non-deterministic images
- **api:** backlog B-121 — SLSA L3 attestation is off by default, and the code says what
  that costs

- Build provenance is attested to GitHub's native attestation registry, so `gh attestation
  verify` checks an image without anything installed. This is separate from the provenance
  buildx already embeds in the OCI manifest: that one is readable by whoever has the tooling,
  this one by whoever consumes the repository. Measured across 501 workflows in 18 large
  open-source projects, 3 of 18 attest provenance — a leading practice rather than a floor,
  taken because it is free on a public repository and this workflow already signs with cosign.

- `MAINTAINERS.md` and `RELEASES.md`, both declared by `.github/CODEOWNERS` and both absent.
- `docs/operations.md` gains a Troubleshooting section, written from failures this repository
  has actually produced: a root-level `go build` that covers almost nothing, the absent
  sibling module, an e2e suite that reports success having skipped all 132 specs, Ginkgo's
  own timeout, and the seed's legacy database URL.

- Continuous integration now runs on pull requests, not only after a merge. A contribution
  is checked before it reaches `develop` — previously the suite ran only on push to
  `develop`/`main`, so the first automated check on any change happened after it landed.
- A `public-checks` job that needs no repository secrets, so a pull request opened from a
  fork is gated like any other: it builds, vets and tests the six modules that do not
  depend on the private `loopzero-contracts` sibling, and runs the diff-scoped quality gates.
- An aggregated `CI gate` check. Branch protection can require one check instead of five,
  and it fails when an upstream job fails OR is skipped — a skipped required check
  otherwise reports as passing.

- `BACKLOG.md` — the repository's maintenance registry, created at the root of the governed
  scope (`single-repo`). Opened with 33 items, each carrying a measured evidence pointer from
  the 2026-09-11 system-cartography run (finding id in `cartography-output/cartography.db`
  plus the `file:line` it cites); since grown by DISCOVER sweeps, which register their own
  findings directly. Items are grouped by the quality pillar their
  Definition of Done moves — nomenclature, clarity, organisation, cohesion, domain
  separation — and six of them are blocked on the gate that must fail before they can be
  verified.
- `.squad/domain-routing.txt` — the project's domain routing table, derived from disk:
  7 domains (`loopzero`, `api`, `cmd`, `infra`, `operators`, `pkg`, `tools`), all resolving.

### Changed

- The canonical fixture project the dev environment seeds, its preview hostnames and the live journeys that exercise it are named `sample` instead of the Portuguese `teste`; an environment seeded before this keeps the old project until it is re-seeded (#379)

- The integration-test role fixture creates only the roles the platform uses; TestRolesExist asserts loopzero_api, loopzero_worker and loopzero_cloud exist and cannot bypass Row Level Security (#387)

- The tenant-isolation CI job builds its database from the whole migration chain with golang-migrate v4.19.1, the tool production runs, instead of a schema dump plus five hand-named migrations (#387)

- CI installs the pinned go-task and trufflehog the cmd/loopzero-ops suite requires, and build-and-test checks out full history (#370)

- `task quality:gates` skips the backlog-structure check and says why when `BACKLOG.md` or the installed kit is absent — both are gitignored, so a fresh clone has neither — instead of failing every clone; it runs in full where both exist (docs/decisions.md, 2026-09-26: a gate over the gitignored kit skips when the kit is absent)

- On a clone without the gitignored development kit (`.claude/`, `.squad/`), the four quality gates that check that kit (`skill-suites`, `skill-script-paths`, `records-tree-split`, `ecosystem-validators`) now skip and print why, instead of three of them exiting 5, so `task quality:gates` can pass on a fresh clone; with the kit installed they run in full (docs/decisions.md, 2026-09-26: a gate over the gitignored kit skips when the kit is absent)

- `coverage-merge` without `--output` and `helm-diff-structural` without `--base`/`--pr` report the missing flag as an invalid invocation (`invalid invocation: ...`); the exit status is unchanged (1) (principle_violations#256)

- The drill bootstrap no longer writes ClusterCredential status; the ClusterCredential controller is its only writer (principle_violations#234)

- The `PulumiOrphanStackDetected` alert's summary and description now say "parallel project" instead of the Portuguese "paralelas"; the alert name, labels and expression are unchanged (#380)

- The Taskfile task `contraprova:v2:hermetic` is now `counterproof:v2:hermetic`, and the counter-proof live-test package moved to `infra/tests/livetest/counterproof`; its verdicts read APPROVED / REJECTED / PARTIALLY_APPROVED (#380)

- The data-plane readiness poller behind the dashboard's project badge now runs in loopzero-api instead of loopzero-app-operator: configure it with `dataPlanePoller.{enabled,prometheusUrl,interval}` on the loopzero-api chart; the app-operator chart no longer accepts `dataPlanePoller` and no longer takes a database connection string (principle_violations#68)

- The `admin-loopzero-dev` dev seed provisions the admin tenant through the engine (`LOOPZERO_API_URL`, `LOOPZERO_BOOTSTRAP_SECRET`) instead of writing tables directly, and derives each seeded identity's username and tenant slug from its email (principle_violations#58)

- Comments, log and error messages, test names and chart descriptions across the engine, charts, Helmfile, Taskfiles and scripts are now English (#380)

- The database trigger that refuses a build, release or preview whose cell cannot be resolved now reports the error in English, and the descriptions of cells cell-0 and cell-1 are English; migration 000067 rewrites both on existing databases and leaves notes an operator has edited untouched (#380)

- `provision-api-tokens.sh` and the drill bootstrap provision the Gitea and ArgoCD tokens with `loopzero-ops seed gitea-token`, `argocd-token` and `argocd-gitea-creds`, and stop when a token cannot be minted instead of storing an empty one (principle_violations#236)

- The Sigstore volume alerts and the zero-ready operator alert no longer tell on-call to run scripts that do not exist; the Sigstore alerts point at a new `docs/runbooks.md` § Sigstore storage section on expanding the volume (principle_violations#240)

- `task dev-public:patch-kratos` is replaced by `task dev-public:sync-kratos`, which re-syncs the ory-kratos release and refuses to run without the OAuth and Resend credentials (principle_violations#67)

- `loopzero-ops seed broker` writes only the broker token; the broker store is installed by the cell helmfile on dev-public, addressing loopzero-api at a stable `host.k3d.internal` port instead of a container IP (principle_violations#67)

- `loopzero-ops seed vault-role-id-sync` writes the OpenBao role id into Secret `loopzero-openbao-approle` instead of the loopzero-api Deployment, and the stale role ids committed in the dev-public and Tilt values are gone (principle_violations#67)

- `helmfile sync` now renders every value the post-Cleanroom seeds used to patch into live objects: loopzero-api's LOKI_URL, the build cluster's Loki cross-cluster NodePort Service, the build Promtail push URL, and the cell secret-fetch broker (release `loopzero-broker`); the `patch-kratos`, `hydra-urls`, `loki-cross-cluster` and `vault-cross-cluster-addr` seeds are removed (principle_violations#67)

- `chart-default-tags` also checks the bootstrap values of charts Helmfile installs from `charts/`, and `helm-topology-index` now lists those releases, so both still cover what Helmfile installs after the pairs became one chart (design_pattern_findings#29)

- Helmfile now installs `charts/loopzero-api` with bootstrap values in place of the duplicate `infra/helm/loopzero-api`; the API keeps the same environment, and on an existing cluster the `loopzero-api` Deployment is recreated once because its selector changes (design_pattern_findings#29)

- Helmfile now installs `charts/loopzero-cloud` with bootstrap values in place of the duplicate `infra/helm/loopzero-cloud`; the dashboard keeps the same environment, and on an existing cluster the `loopzero-cloud` Deployment is recreated once because its selector changes (design_pattern_findings#29)

- Helmfile now installs the admission policies from the published `charts/loopzero-admission-policies` chart on the core cluster and on every cell; the duplicate bootstrap chart `infra/helm/admission-policies` is removed, and rendered policies are unchanged in every environment (design_pattern_findings#29)

- The platform operator chart refuses an empty `config.argocdNamespace` at render time (principle_violations#149, principle_violations#150)

- A ClusterCredential whose kubeconfig Secret lives outside `loopzero-system` is now refused by name when a tenant, app or preview operator resolves its cell, instead of being read on the operator's behalf (principle_violations#149, principle_violations#150)

- The per-tenant concurrent build limit is one setting, MAX_CONCURRENT_BUILDS_PER_TENANT (default 3): it drives build backpressure, the admission cap and the team's recorded limit; MAX_BUILDS_PER_TENANT is still accepted when it matches and refused at startup when it does not (principle_violations#49)

- loopzero-ops quality helmfile-context-parity reports a missing environment variable in English (principle_violations#159)

- `loopzero-ops quality spotcheck-dimension` rejects a non-integer `SPOTCHECK_SEED` as a usage error instead of silently picking a random sample (principle_violations#75)

- `loopzero-ops` quality gates and seeds now print their messages in English; Kubernetes errors from seeds name the cluster as `(context <name>)` (principle_violations#77)

- Every `loopzero-ops` subcommand uses one exit table: 0 success, 1 usage error or a failed check/seed, 5 the check could not reach a verdict; quality gates, seeds and chaos SLO checks no longer exit 2 (principle_violations#75)

- `loopzero-ops quality helm-drift --format json` adds a `render_errors` field, and an unknown `--format` is rejected as a usage error (principle_violations#51)

- The tenant and app operators refuse to start on a malformed LOOPZERO_TENANT_MAX_CONCURRENT or LOOPZERO_DATA_PLANE_POLLER_INTERVAL instead of silently using the default (principle_violations#91)

- The tenant operator connects to the MinIO admin API over TLS when MINIO_ENDPOINT starts with https:// (principle_violations#109)

- The API now refuses to start when a numeric or duration setting cannot be parsed (for example a timeout without a unit), naming the setting, instead of silently using the default (principle_violations#292)

- The build error for a loopzero.yaml that does not declare a runtime is now in English, like every other build message (principle_violations#11)

- Automatic placement of new accounts now respects the per-cell capacity an operator sets, not only the platform-wide limit (principle_violations#4)

- Build timeouts reported by the stuck-build reaper now name the phase budget that was exceeded, from the same values the build lifecycle uses (dp#9)

- Routes restricted to team owners or admins (repository connect/disconnect, domain create/delete, audit log, team membership) now all refuse other roles the same way: 403 with code INSUFFICIENT_ROLE

- Environment names are validated the same way when deploying and when creating an environment: lowercase letters, digits and hyphens, starting with a letter and not ending with a hyphen

- Every live stream (build, project, signals, logs, deploy timeline) now sends heartbeats, caps concurrent connections, and ends with an error event after repeated upstream failures instead of hanging silent

- POST and DELETE /api/v1/projects/{name}/connect are now rate limited like every other mutation

- `webhook-canonical-path` now fails when none of its scan directories exists, instead of passing over nothing (principle_violations#257)

- `loopzero-api-openbao-approle-envfrom` and `option-b-eso-deploy-doc` no longer require citations of private assistant memory in the chart and the runbook (principle_violations#270)

- `skill-paths`, `skill-script-paths`, `skill-suites` and `records-tree-split` now exit 5 (NOT VERIFIED) on a machine without the gitignored kit or records tree, instead of failing with a misleading count or passing over nothing (principle_violations#270)

- helm-drift reads its recorded chart-pair divergences from infra/quality/helm-drift-registry.yaml, and each entry names the docs/decisions.md row it rests on instead of an internal ticket id; the --json `adr` field now carries that decision

- The adapter-ports, alert-runbooks, billing-secrets, caddy-host-parity, chart-pairs, contract-consumers, dashboard-root-parity, dev-public-env, docs-contract-refs, ecosystem-validators, error-vocabulary, helm-topology-index, ldflag-symbols and lint-coverage quality gates now print their messages and descriptions in English

- loopzero-external-secrets-chart-sanity and loopzero-external-secrets-creation-policy now fail (exit 5) when helm is not installed instead of passing as advisory

- dangling-script-refs only treats versioned files as callers; skills in the gitignored .claude kit no longer affect its verdict

- The published charts now reject values that have no effect (for example `ingress`, `autoscaling`, `podSecurityContext`, unknown `config` keys on loopzero-cloud) instead of accepting and ignoring them; loopzero-cloud's example values no longer set inert keys.

- The published loopzero-api, loopzero-cloud, loopzero-dashboard and operator charts now merge a chart-local `imagePullSecrets` with `global.imagePullSecrets`.

- The published monitoring chart (0.3.0) now ships exactly the dashboards and alert rules the platform installs: six more dashboards, runbook links on its alerts, a corrected operator-error alert, and sixteen alerts on metrics nothing emits removed.

- Admission policies that guard deletes and updates now exempt every kube-system controller account and the same list of platform service accounts (configurable as `systemActorExemptions.platformServiceAccounts`); rendering fails if fewer than two are listed.

- The per-cell bootstrap tasks k3d:bootstrap-cell-0 and k3d:bootstrap-cell-1 are replaced by one task, k3d:bootstrap-cell CELL_CTX=<context>; cells and their bootstrap releases are declared once (CELL_CONTEXTS, CELL_BOOTSTRAP_RELEASES).

- **api:** the `surfaces` registry moved from `api/internal/server/surfaces` to `api/internal/surfaces`, and `deploycontracts` from `api/internal/services/deploycontracts` to `api/internal/deploycontracts`. Twelve `api/internal` subtrees depended on each other in one cycle through `metrics → server` and `buildplane → services`; both packages are leaves or contracts, so moving them out dissolves the cycle. A new architecture test, `api/internal/archtest`, fails the build if any two `api/internal` subtrees import each other again — the check the Go compiler cannot make.

- The five GitHub workflows are written in English. They carried 104 lines of Portuguese —
  comments, step names and error messages — in a repository about to be public, where the
  people who read a workflow to understand a failing check are exactly the people who do
  not speak it. Nothing about what they do changed; `actionlint` is clean and `zizmor`
  reports 0 high and 0 medium on the translated files.

- Every workflow declares `concurrency`, and the two semantics are deliberate. Checks group by
  ref and cancel superseded runs, because a push to an open pull request otherwise pays for a
  full run nobody will read. The publish workflow groups by IMAGE and does NOT cancel: it
  builds, signs and pushes, and interrupting that leaves a published tag with no signature or
  a signature with no image.

- Twenty-six tests over `.claude/hooks/delivery-gate.sh` are retired. `hooks.json` declares
  nine hooks and none of them is it — the mechanism was removed from the kit, and its
  replacement (`.git/hooks/pre-push`) has an interface nothing like it. What they asserted,
  and what their retirement costs, is recorded in `cmd/loopzero-ops/delivery_gate_history.go`.
- Two RED suites for registered-but-unimplemented items (B-001, B-048) skip by default and
  name the item and the measurement in the skip message. `LOOPZERO_RUN_B001_RED=1` and
  `LOOPZERO_RUN_B048_RED=1` arm them; both still fail when armed, so the skip hides nothing.

- The delivery-gate premise check asks which regime the repository is in before it
  asserts anything. Its three premises defend a specific trade — CI may run post-merge
  because a local hook blocks a bad push first — and that trade cannot hold for a public
  repository, where a contributor has no such hook and a fork's workflow receives no
  secrets. With `ci.yml` now triggering on `pull_request`, the guard is CI itself, which
  is verifiable by GitHub as a required check and covers every contributor. The local-hook
  premises remain in the file for a repository that still makes the post-merge trade.

- Every job runs on GitHub-hosted runners (`ubuntu-latest`). The third-party runner fleet
  is no longer the default anywhere, including in the reusable publish workflow, whose
  `runner` input still accepts a custom label for anyone who has a fleet of their own.
- Jobs that need the private `loopzero-contracts` module are skipped on pull requests from
  forks instead of failing there. A fork receives no secrets, so those jobs could never
  have succeeded; they now report as skipped and the aggregated gate accepts that by name.

- The four operator charts now pin their ServiceAccount name instead of deriving it from
  the Helm release name, so the identity the platform's delete-guard and OpenBao
  allowlists name is a property of the chart rather than of how it was installed.
  Installing under a release name other than the chart name previously produced an
  identity outside both lists, which surfaced as a permission failure during
  reconciliation rather than as a failed install. Rendered output is unchanged on every
  path this repository deploys (#B-047)
- The `helm-drift` gate now compares what each side of a declared chart pair **grants**, not
  only which resource types it installs: every `(apiGroup, resource, verb)` RBAC cell of both
  rendered ClusterRoles, with each differing cell named in the report. Measured on the
  `loopzero-api` pair, the two sides diverge on **174 cells of a 200-cell union** — a divergence
  no gate could see before, because both sides install exactly one `ClusterRole` and the
  comparison stopped at the resource type (B-001).
- Each of those 174 cells is declared individually in the gate's own registry, so the gate is
  green because the divergence is recorded rather than because nobody looked; withdrawing one
  declaration turns it red naming exactly that cell. Two of the declarations are recorded as
  **undecided**, not approved — the bootstrap chart's cluster-wide `secrets` `delete` and
  `pods/exec` `create` — and are printed by name on every run. The decision on them belongs to
  B-048; the reasoning is in `.squad/wiki/decisions/B-001-bootstrap-grant-disposition.md` (B-001).

### Changed

- `helm-drift`'s one-line summary in `loopzero-ops quality --list` now says what the gate actually
  compares. It read "drift between rendered Helm charts and the deployed state", which named a
  comparison the gate has never made — it compares `charts/` against `infra/helm/`, and since
  B-001 also compares their RBAC grants (B-001).
- `docs/` consolidated from 1,374 versioned markdown files across 17 subtrees into **six living
  documents** — `architecture.md`, `loopzerokit-loopzerocloud-seam.md`, `api-contracts.md`,
  `operations.md`, `runbooks.md`, `decisions.md` — plus `README.md` as the single index.
  Everything removed stays in git at `4f728eb7a`.
- `CLAUDE.md` repointed at the consolidated paths: the locked LoopZeroKit ↔ LoopZeroCloud rule now
  cites `docs/loopzerokit-loopzerocloud-seam.md`, and the paragraph describing `docs/` as a mix of
  living reference and audit trail now describes the six-document tree.
- Code-quality auditing is enabled for the `api` module — 97 of the workspace's 321 packages.
  It audited nothing at all before this: no language was enabled, so every plan scored in this
  repository was capped `INVALID` regardless of its own quality, and `/implement` was unreachable.
  Pre-existing dead-code findings are recorded as a baseline (14 rows) so they stay countable
  without failing changes that did not cause them, and Go mutation testing — which the toolchain
  defers unconditionally for every Go project, so the finding says nothing about this codebase —
  is exempted **until 2026-12-12**. On that date the exemption lapses, the finding returns at full
  severity and every plan drops back below the band `/implement` requires; renew the row in
  `.claude/rules/code-quality-allowlist.txt` or adopt a Go mutation runner (#B-034, #B-088)
- The four documents that declare when the Pulumi layer stops being authoritative now name the
  cutover milestone the ratified CTO mandate names, and cite the decision that defines it
  (`../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`) — `README.md`,
  `CLAUDE.md`, `docs/architecture.md` and `infra/pulumi/README.md`. In each of them the rule
  against adding to the Pulumi layer now stands on its own line, carrying no date, because it is
  unconditional and has been since the mandate; a rule sharing a line with a schedule reads as a
  rule that expires. `infra/pulumi/README.md` also stops presenting a Pulumi provider swap as the
  AWS path, and `CLAUDE.md` stops offering `CHANGELOG.md` as a place the milestone names survive
  — this file carries none of them. One further reference to the old milestone remains, in
  `operators/internal/argocd/real_provisioner.go`, where it freezes a cluster name rather than
  declaring a schedule; it is a separate defect in another domain and is filed as its own item
  (#B-018)
- A chart name that exists in **both** chart trees — `loopzero-api` and `loopzero-cloud` — no longer
  resolves silently to the bootstrap chart in render-time tests. `chartrender.ChartPath` now
  fails, naming both paths and the two resolvers that disambiguate. Previously the bootstrap
  chart won because it happened to sit first in the candidate list, so the published charts were
  unreachable through that function and a test could render the wrong chart while looking
  correct. Names present in exactly one tree are unaffected (#B-002)

### Removed

- `task k3d:heal` no longer re-applies the build registry config, loopzero-api image variables or the clone-template release, which the install already renders (principle_violations#243)

- The legacy Pulumi stack drops the Harbor-only resources and outputs — the container-images bucket, the `harbor_db` database, the Harbor managed Valkey cluster and the `harborRedis*` outputs; the next `pulumi up` plans their deletion and prod needs the cluster unprotected first (design_pattern_findings#30)

- The Harbor alert rules, the "Registry (Harbor)" dashboard and the Harbor panels of the home and alerts-overview dashboards are removed, as is the Harbor mirror PrometheusRule of loopzero-admission-policies (design_pattern_findings#30)

- The tenant-operator no longer provisions Harbor projects or robot accounts; the `harborUrl`, `harborAdminUsername`, `harborRegistryHost` and `harborAdminPassword` settings are removed from its chart and refused at install, and the per-plan registry storage quota that only Harbor enforced goes with them (design_pattern_findings#30)
- The tenant-operator chart no longer carries a `HARBOR_ADMIN_PASSWORD` entry in its Secret template; the value was already refused at install, and the dead branch would have come back live the day the schema loosened (design_pattern_findings#30)

- The build pipeline's Harbor dual-push mirror and the `harborRegistryUrl` setting of the loopzero-api chart are removed; images are pushed to `REGISTRY_URL` only, and a values file still setting the key is refused at install (design_pattern_findings#30)

- Harbor is no longer installed: the `harbor` and `harbor-redis` Helmfile releases, the `infra/helm/harbor` chart and the `harbor-init` image are gone, and every environment drops `profile.harbor`, `replicas.harbor`, `registries.harborPrefix` and the `harbor.loopzero.local` prefixes. Existing clusters must uninstall the releases by hand (docs/decisions.md, "Retire Harbor") (design_pattern_findings#30)

- The `AUDIT_ENFORCE_DOMAIN_ENUM` setting: it was never read on the audit-log write path, so setting it never rejected anything (principle_violations#300)

- `infra/scripts/local/regen_build_runtime_kubeconfigs.py`, replaced by `loopzero-ops seed cluster-kubeconfigs` (principle_violations#236)

- `infra/scripts/production/operator-cutover-execute.sh`, `infra/scripts/production/bootstrap-platform.sh` and `infra/scripts/security/rekor-archive-old-entries.sh`: none could complete — the cutover's pre-flight and rollback scripts were gone, the bootstrap script had no caller and restarted deployments that no longer exist, and the Rekor archiver exited 1 on every run (principle_violations#240)

- `task check` and `infra/scripts/local/check-sprint.sh`: every category it dispatched to had been deleted, so it reported WARN for all eight and exited 0 without checking anything; use `task doctor` (principle_violations#240)

- The secret-fetch broker is no longer installed on standalone-multi cells: its chart forbids a plaintext bearer outside dev-public, a guard the seed bypassed by always passing `envName=dev-public` (principle_violations#67)

- The `task helm:test:env` target is removed: after the per-environment loopzero-api stubs went with the bootstrap chart it selected no test (design_pattern_findings#29)

- The `helm-drift` and `helm-overlap-parity` quality gates and their registries are retired, since no chart exists in two trees any more; `chart-pairs` now refuses any chart present in both, including a template-identical copy under another name (design_pattern_findings#29)

- The loopzero-api chart no longer accepts config.maxBuildsPerTenant; set config.maxConcurrentBuildsPerTenant (principle_violations#49)

- The loopzero-preview-operator chart no longer accepts config.baseDomain and the loopzero-app-operator chart no longer accepts config.loopzeroReaperGracePeriodSeconds: nothing read them since the preview URL comes from the API and the unused reaper was deleted; remove them from your values, which now fail schema validation (principle_violations#74)

- PlatformBootstrap spec.timeouts and spec.argocd.tokenTTLSeconds, which the platform operator never applied (principle_violations#83)

- Operator metrics that were registered but never recorded: loopzero_operator_provisioner_duration_seconds, loopzero_operator_crd_count, loopzero_finalizer_blocked_seconds, loopzero_reconcile_queue_depth, loopzero_self_heal_total, loopzero_orphan_cr_total (principle_violations#72)

- POST /api/v1/tenants/bulk-delete, which could never succeed (it always answered 400)

- The unused secret-store client, cache and database/Kubernetes credential engines were removed from the shared library.

- The unused ArgoCD app-of-apps manifests under infra/argocd-bootstrap and task validate:argocd-sync-waves; the platform is installed and managed by Helmfile.

- Tasks that could only fail because their script was deleted: audit:action-pinning, dev-public:live-proof, validate:matrix:quick, parity:preflight, parity:k3d-virgin, parity:droplet, parity:doks, test:pre-release-rigor:scenario-3 and scenario-4, ops:build, and the 16 test:dogfood:* wrappers.

- landscape.yml, a vendored copy of the CNCF landscape data that no part of the repository read, is no longer shipped.

- `api/internal/cellmgr`, an unused per-cell Kubernetes client manager. It had no callers,
  and could not have worked if wired: it resolved clients from kubeconfig path settings that
  only the test-only router populates, while production uses the credential-based one. The
  mechanism that actually resolves per-cell clients is unchanged and unaffected. Also corrects
  the eleven comments and two inventory entries that pointed at it, nine of which described it
  as the live resolver (B-023).

- The dated audit trail under `docs/` — completed plans, baselines, meeting records,
  roadmap runs, per-iteration implementation logs and review reports. Two thirds of the
  tree. Still readable as history via `git show 4f728eb7a:docs/<path>`.
- The two competing indexes (`docs/README.md` curated in English and `index`
  generated in Portuguese, the latter also breaking the English-only rule for versioned
  artifacts), replaced by one index.
- The duplicate audit trails `docs/wiki/` (934 files) and `docs/knowledge-base/` (126),
  which overlapped in period with zero byte-identical files between them.
- `docs/operations/loopzero-capsim-mcp/` — a 26 MB Node checkout with its own `node_modules`
  living inside the documentation tree, and its now-dangling `.gitignore` entry.
- `docs/api-contracts.md`, which documented `memory:*` scopes belonging to `../loopzero-memory`
  rather than to this engine.
- `api/internal/health` — the outbound HTTP prober, unreachable since its caller, its port
  and its composition-root wiring were removed in March 2026 (`ad6de675f`). Nothing in the
  running API reached it. The `api/internal/adapters` doc comment that still required it is
  corrected, and a test now fails if that list ever again names a port declared nowhere or
  cites a file that is not on disk. Rationale:
  `.squad/wiki/decisions/B-022-health-prober-disposition.md` (B-022)

### Fixed

- The local dev loop starts the API again after a clean setup: it ran its own untracked copy of the database migrations with a client the host may not have, and it reloaded its own configuration forever; it now runs the same migration step as setup and settles (#403)

- The local dev loop can no longer deploy into the wrong cluster: it deployed into whichever cluster was current, which after a clean setup was the secrets cluster, and it now refuses to start anywhere but the core cluster (#402)

- A cold `task setup` installs the local login edge and mints the bootstrap token again: the identity service ports the local cluster reserves were never exposed, and the token step collided with one of them (#401)

- A cold `task setup` seeds the bootstrap user again: the seed wrote across tenants as the table owner, which row-level security now refuses, and it connects as the cross-tenant worker role instead (#396)

- A cold `task setup` connects the cells again: the cell credential bootstrap applied everything and then exited with an error on its closing log line, so the repair step reported both cells disconnected (#400)

- A cold `task setup` no longer stops after generating the build signing key: the build-secrets script lost track of its own directory once it had generated the key and could not reach its backup step (#399)

- A cold `task setup` no longer fails after provisioning API tokens: it created the MinIO buckets a third time by running the MinIO client inside the server, which the source-built server image does not carry; the chart already creates them (#394)

- A cold `task setup` completes with or without the optional secrets cluster: a missing one no longer fails the cross-cluster DNS step, and when it exists setup now initialises OpenBao and hands the API its credentials, which nothing did before (#398)

- The public CI job runs its diff-scoped quality gates again: it re-fetched the base branch without credentials and died before any gate ran, although its checkout already holds the full history (#370)

- A fresh install with the in-cluster database starts the API again: `task setup` creates the `loopzero_api` and `loopzero_worker` roles before the migrations and hands the API their connection URLs, instead of the table owner's, which the API refuses to serve requests over (#396)

- A cold `task setup` provisions the core cluster's database credentials again: two install steps still named the cluster by its pre-rename name, which the credentials script silently skipped, and it now refuses a cluster name it does not handle (#397)

- The rollback and concurrent-build integration tests run under the production role split (loopzero_worker fixtures, tenant-scoped loopzero_api request pool) and no longer fail or flake under fail-closed Row Level Security (#387)

- The module-selfcontained gate reports a module whose private dependency cannot be fetched without a credential as NOT VERIFIED and names it, instead of failing it; an unknown revision, a go.mod inconsistency or a compile error still fails (#370)

- The skill-suites quality gate reports a host with python3 but no pytest module as advisory, instead of failing on suites that never ran (#370)

- A cold start installs the object store again: MinIO withdrew every community binary and image, so the MinIO server and its `mc` client are now built from pinned upstream source commits (`loopzero-minio`, `loopzero-clone-runner`) and the chart, the build-cache steps and the lifecycle job use them instead of images that can no longer be pulled; the cache-save step no longer relies on bash-only syntax that fails in the new image. The upstream source is archived and unmaintained, so this is a bridge until the object store is replaced (#394)
- `task cleanroom:v2` now actually runs the cold-start chain: every spec used to skip unconditionally, so the task reported SUCCESS in milliseconds with no cluster destroyed or recreated; the destructive specs now run on explicit consent (`LOOPZERO_CLEANROOM_DESTROY=1`), which the task grants for the full-chain spec only (#393)
- `task validate:helmfile-templates` passes: Velero objects are validated against generated schemas instead of failing, the offline render no longer needs `LOOPZERO_DB_PASSWORD`, and the `dev` environment's missing loopzero-cloud values layer was added (#391)

- `task validate:manifests` now fails, naming the chart, when a chart fails to render or renders nothing; it used to pass over a broken render (Velero on every fresh clone), and it builds chart dependencies itself (#392)

- Offline manifest validation now checks loopzero.io objects against their current CRDs: six schemas (ClusterCredential, Environment, GitRepositoryBinding, PreviewEnvironment, Tenant, LoopZeroApplication) lacked fields and constraints added since (#389)

- `skill-script-paths` and `skill-suites` report a kit file they cannot read as "could not complete" (exit 5) instead of as a violation (exit 1) (principle_violations#256)

- `openbao-init-auth-mounts`, `loopzero-external-secrets-chart-sanity`, `loopzero-external-secrets-creation-policy` (chart missing), `render-schema-gen-sql --render` (pipeline could not run) and `route-auth-coverage` (no route enumerated) no longer print a `FAIL` line over a run that exits 5; the report now matches the exit status (principle_violations#256)

- A quality gate whose check crashes now exits 5 ("could not complete") and names the panic, instead of exiting 2, which is outside the `loopzero-ops` exit table (principle_violations#256)

- The API, the tenant operator, the loopzero-api chart and new PlatformBootstrap resources now default the in-cluster Gitea address to the loopzero-system namespace the platform installs Gitea into, instead of a `gitea` namespace that exists in no environment; existing PlatformBootstrap resources keep their stored address (#354)

- `task k3d:heal` re-bootstraps a missing or disconnected cell through `bootstrap-cell-cc.sh` instead of rewriting its kubeconfig by hand, which pointed cell-0 at the core cluster and gave cell-1 an identity that could not deploy (principle_violations#243)

- The drill environment renders its API, operator and build-template configuration from Helmfile values instead of patching the running objects after each sync, and its platform-operator overlay passes the chart schema again (principle_violations#234)

- The tenant and platform operators and the API are told where the platform installs Gitea (`loopzero-system`) in every environment, including prod, instead of a `gitea` namespace no environment uses (principle_violations#234)

- Connecting a GitHub repository now refuses a `loopzero.yaml` that declares no apps, the same rule every push deploy applies, so a repository can no longer be linked and then build nothing on each push; the missing-file message no longer points at the removed `loopzero init` command (principle_violations#141)

- The admin operations to evict tenants, pin a tenant to a cell and inspect a release now see every team's data again: eviction no longer matches nothing and deletes nothing in the database while tearing the cluster side down, the pin finds the tenant and counts the target cell correctly, and inspection finds releases of any team (#386)

- Background work that crosses tenants — creating an account through the platform bootstrap and recording deploy completion or failure events — no longer fails with a database permission error on installations whose database was built from the migrations (#385)

- The per-cell concurrent build limit counts every team's builds on the cell again (#386)

- Signing in no longer answers "No team associated with this account" for every user on the isolated database role, and members of several teams can pick one (#386)

- Push-to-deploy, pull-request previews and secrets delivered to running services work on the isolated database role (#384)

- Deploys finish, activate on sync and report their GitHub commit status, and every change is recorded in the audit log, when the platform runs on the isolated database role (#384)

- Every authenticated request now reads and writes its own team's data once the isolated database role is in use; before, lists came back empty and changes were refused without an error (#384)

- The helm-topology-index gate no longer lists a deleted chart as undeclared when only its downloaded dependencies (the gitignored charts/ folder) remain on disk, so the index is the same on a fresh clone and on a working machine (design_pattern_findings#30)

- Deploying to a team that was deleted and recreated under the same name no longer fails because its deployment repository was left archived (principle_violations#98)

- A deployed app's status URL and route health checks now use the same hostname the platform routes traffic to, instead of a shorter one no route matched; existing apps move to the routed hostname on their next reconcile (principle_violations#231)

- `POST /internal/bootstrap` provisions tenants again: it creates the owner's account and places the tenant in a cell, and refuses a tenant slug another identity owns with `409 TENANT_SLUG_TAKEN` instead of adding the caller to that tenant (principle_violations#58, #383)

- Plan limits, the per-account project cap, the cost cap and the plan shown by billing and scale now use the team's real subscription; they were reading every team as Starter (principle_violations#215, #382)

- `task k3d:heal` no longer rewrites loopzero-api's build credential with a 24-hour, cluster-admin token that expired and broke deploys; it uses `loopzero-ops seed cluster-kubeconfigs` and exits non-zero when that fails (principle_violations#236)

- Provisioning the Gitea or ArgoCD token no longer erases the other token, or the Cloudflare token, from the `loopzero-api-tokens` Secret; each writes only its own key (principle_violations#236)

- The drill JWT seed (`drill-mint-jwt.sh`) and the E2E database seed no longer fail with `null value in column "account_id"` on databases migrated past the account migrations (principle_violations#236)

- On the multi-cluster local topologies (dev-public, standalone-public, standalone-multi), cell log shippers now push to the build cluster's published Loki port, so workload logs from the cells reach Loki; they pushed to a Service the cells do not have or to a stale container IP (principle_violations#67)

- The published `loopzero-cloud` chart mounts a writable `/tmp`, so data exports no longer fail on its read-only root filesystem (design_pattern_findings#29)

- The platform operator's `loopzero-system` and ArgoCD Roles now bind the chart's ServiceAccount; under any release name other than the chart's they bound a ServiceAccount that does not exist (principle_violations#149, principle_violations#150)

- The app operator can now read ClusterCredentials, so a placed tenant's application reports Healthy and its TLS certificate is checked in the tenant's cell instead of the lookup being forbidden (principle_violations#149, principle_violations#150)

- loopzero-ops quality acyclic-graph no longer stops on files inside directories the Go toolchain never compiles (names starting with . or _), such as an installed editor kit

- loopzero-ops quality gates, the gate list and loopzero-ops leak exit 5 (could not complete) instead of 0 when their report could not be written, for example to a closed pipe; a failing command keeps its own status

- Rotating a secret the tenant operator reads through another component's path (the Gitea token, the MinIO keys) refreshes the operator's copy and restarts the operator again, instead of restarting only the API and leaving the operator on the old value (principle_violations#237)

- loopzero-ops quality billing-layers and routes-acyclic recognise the cost gate's new location and the shared plan-tier policy package instead of failing on them

- loopzero-ops quality error-vocabulary checks every user-facing error message again, including messages built with an error constructor before being written; 37 messages had left its scan, and error codes are no longer counted as messages

- Seeds that restart the Promtail DaemonSet, convert the Loki Service to NodePort or create their namespace no longer skip those steps silently when given a non-default Kubernetes client (principle_violations#60, principle_violations#88)

- `loopzero-ops seed` no longer looks for `dev-credentials.json` under a relative path when the home directory cannot be resolved; it asks for `LOOPZERO_DEV_CREDENTIALS` instead (principle_violations#62)

- A quality gate that cannot read its input now reports that it could not complete (exit 5) instead of reporting a violation (principle_violations#75)

- `loopzero-ops leak ports --json` adds English field names beside the existing ones, which are kept for compatibility (principle_violations#50)

- Seeds no longer report PASS after a rollout that did not settle, a namespace that could not be created, or a token or secret they failed to read; the Stripe API client now times out after 30s (principle_violations#65)

- The `tenant-fixture` seed refuses an unknown `LOOPZERO_PLAN_TIER` instead of silently seeding the starter plan (principle_violations#56)

- `loopzero-ops pulumi archive` fails closed: a stack whose resources could not be counted, or a directory it could not list, holds the archive instead of being reported as empty, and plaintext secret hits need an explicit `--accept-plaintext-hits` (principle_violations#52)

- The Stripe seeds and the webhook-endpoint seed refuse a placeholder or wrongly prefixed Stripe key instead of writing it into the cluster (principle_violations#54, principle_violations#55)

- Cancelled or timed-out reconciles no longer trip the ArgoCD circuit breaker and pause tenant deploy setup (principle_violations#106)

- A transient MinIO error no longer rotates a tenant's build storage credentials while builds are running (principle_violations#109)

- Connecting a project for a Pro or Team account no longer fails with a platform-degraded error: tenant provisioning now accepts the pro and team plans (principle_violations#85)

- A ClusterCredential no longer reports its ArgoCD cluster Secret out of sync after losing a routine write conflict (principle_violations#83 follow-up)

- The app operator now reports failed data-plane readiness writes and readiness samples whose environment has no stored target, instead of logging and continuing silently (principle_violations#68)

- The MisplacedTenantDetected alert can fire: the tenant operator now scans for Tenants outside loopzero-system and publishes loopzero_misplaced_tenant_total, and loopzero_tenant_cb_state reflects each circuit breaker's state (principle_violations#72)

- A PreviewEnvironment's status.url now shows the same public URL the dashboard and API report, instead of an operator-built host that did not route; PreviewEnvironment gains the optional spec.publicURL the API fills in (principle_violations#74)

- A ClusterCredential whose ArgoCD cluster Secret cannot be written now reports ArgoCDSecretSynced=False and is not Ready, instead of looking healthy while deployments to that cell cannot sync (principle_violations#100)

- Deleting a PlatformBootstrap now removes the loopzero-platform-tokens Secret it created, and a failed bootstrap records the failure reason in its BootstrapComplete condition (principle_violations#80, principle_violations#81)

- Ready/Reconciling/Stalled conditions are written in the same status update as the conditions they summarise, so they are no longer stale for a whole requeue interval after a conflict (principle_violations#61)

- LoopZeroApplication and ExposedRoute phases only move along their declared lifecycle transitions; a route that stops answering now reports Degraded instead of dropping back to Serving (principle_violations#64, principle_violations#66)

- Preview environments are provisioned in the tenant's cell (namespace and every isolation object together), matching where their deployment is synced; the preview-operator chart grants the reads this needs (principle_violations#76)

- Tenant PriorityClasses and Harbor pull secrets are written to the tenant's cell instead of always to cell-0, ending the endless pull-robot re-creation for tenants on other cells (principle_violations#78)

- Environment namespaces, RBAC, NetworkPolicies and quotas are created only in the tenant's own cell; an unresolvable cell now reports RuntimeClusterUnresolved and retries instead of silently writing to cell-0, and deletion no longer releases the finalizer when the namespace lookup fails (principle_violations#59)

- Operators pick up a rotated cell kubeconfig or ServiceAccount token without a restart (principle_violations#107)

- The tenant operator no longer deletes every live preview Application of a tenant when listing its PreviewEnvironments fails transiently; the step fails and retries instead (principle_violations#57)

- Source upload failures are no longer mislabelled as network or permission problems when an unrelated number or word in the error happened to match (principle_violations#301)

- Custom domain setup no longer risks duplicate DNS records after a slow Cloudflare response, and conflict detection now sees every record in large zones (principle_violations#293)

- Build and runtime log lines written within the same second are now shown in the order they were produced (principle_violations#295)

- Usage metering recovers on its own after a region credential is rotated, instead of failing until the controller restarts (principle_violations#291)

- The deploy timeline now reports a temporary server problem as an error instead of "deploy not found" (principle_violations#288)

- The deploy timeline now shows a release whose sync, mirror or rollback failed as failed or degraded instead of "in progress" forever (principle_violations#286)

- Image-signing latency metrics no longer report fabricated zero-second samples, so the signing latency alert reflects real measurements only (principle_violations#285)

- Pull request previews and GitHub app installations no longer fail silently when the account lookup or save fails; in-flight webhook processing stops cleanly on shutdown (principle_violations#280)

- Setting or deleting a secret for an account hosted on a region this API cannot reach now reports the sync as failed instead of silently writing it to the wrong cluster (principle_violations#46)

- A deploy with an app or account name that cannot form valid resource names now fails with an error instead of crashing the request (principle_violations#44)

- GitHub commit statuses and deployment-sync logins no longer fail when a description or password contains control characters (principle_violations#282)

- The commit status on GitHub now links the deployed app's real URL instead of a guessed "api" host (principle_violations#279)

- Deploying the same commit twice at once (for example a push and a pull request preview) no longer risks one deploy uploading or deleting the other's source (principle_violations#278)

- The metering freshness signal now keeps growing when usage could not be measured (database unreachable, every tenant failed), so billing gaps can be alerted on (principle_violations#290)

- Database connection pool metrics (acquire totals, saturation count, acquire wait) now report real values; they previously grew as a running sum of totals, making the related alerts meaningless (principle_violations#284)

- An expired credential for the deployment sync service is now refreshed whichever status check first sees it, instead of only after a sync attempt (principle_violations#276)

- The build timeline and build logs now group every build step into its real phase (clone, install, build, sign, deliver) instead of showing most steps as "other" (principle_violations#289)

- In projects with several services, each service's release now records that service's own image digest instead of possibly another service's (principle_violations#302)

- Source uploads and signed download links now work for object names containing spaces or non-ASCII characters (principle_violations#8)

- Services deployed without progressive delivery now get the same graceful-shutdown drain as canary deploys, so in-flight connections are no longer cut during a rollout (principle_violations#41)

- Deleting a project now fails when the platform cannot reach the deployment repository, instead of reporting success while the service keeps running (principle_violations#2)

- Evicting a tenant no longer deletes resources of other tenants whose ID starts with the same characters, and an eviction that could not delete every resource now fails instead of reporting success (principle_violations#45)

- Only one API replica runs the background loops at a time, and a replica that loses the lease stops them (principle_violations#202)

- A scaling configuration is refused for an environment name that can never become a valid environment (leading or trailing hyphen) (principle_violations#201)

- A duplicate-name conflict is reported the same way on every deploy path (principle_violations#193)

- Evicting a team either completes or changes nothing; a failure part-way no longer leaves a half-deleted team (principle_violations#196)

- Moving a team to another cell cannot race a concurrent move or a new-team placement past the cell's capacity (principle_violations#195)

- Build cleanup keeps each team's own recent builds; two teams with the same project and environment names no longer evict each other's history (principle_violations#194)

- The API refuses to start when replay protection, quota enforcement or team controls are configured but unreachable, instead of running with them silently off (principle_violations#199)

- A failed GitHub push or installation event is reported as a failed delivery instead of being silently acknowledged (principle_violations#197, principle_violations#198)

- Build status and build listings report a database failure instead of showing a build with no apps (principle_violations#208)

- Preview environments rebuilt from an earlier build keep the app's runtime, path, build and start commands, instead of failing to sync (principle_violations#183)

- Compute usage is metered for every environment of a team again; namespaces were being skipped, under-counting usage (principle_violations#181)

- A secret stored encrypted is never returned or synced as its ciphertext when the encryption backend is unavailable; the request fails with a backend-unavailable error instead (principle_violations#180)

- The per-Account project limit is now enforced across all of an account's teams when a deploy creates a new project; previously each team could create up to the limit (principle_violations#179)

- The per-environment last-deploy summary no longer fails for an environment whose active release has no recorded commit (principle_violations#187)

- Creating a preview on an installation without a build orchestrator returns "preview service not available" instead of failing with an internal error (principle_violations#185)

- Deleting a project no longer cancels archived releases, and now also cancels rolled-back ones, matching the release lifecycle (principle_violations#190)

- Deploy webhooks (deploy.failed, deploy.succeeded) now fire whichever process runs the background loops, and the controller runs the same audit-log retention, build cleanup and custom-domain loops as the API (principle_violations#182)

- Pinning a team to a cell that is full now answers 409 "cell at capacity" instead of an internal error (principle_violations#26)

- Opening or re-pushing a pull request now records its preview under the same lock as the preview build, so concurrent webhooks for one pull request can no longer race (principle_violations#33)

- Environment provisioning and secret re-sync no longer treat unrelated errors as "already exists" or "namespace terminating" based on their wording; closing a pull request no longer hides a preview lookup failure (principle_violations#35)

- A failed deploy sync caused by a network error (such as a DNS lookup failure) now fails the release immediately instead of leaving it pending until it times out (principle_violations#35)

- A misconfigured environment provisioner, secret re-sync or scaling resolver now fails loudly at startup or on use instead of silently doing nothing (principle_violations#36)

- Every build success and failure path now records its outcome the same way, so no path can skip the failure event or leave a build half-finalized (dp#11)

- Custom domain status changes all follow the domain lifecycle rules, and concurrent health checks can no longer overwrite each other's status (principle_violations#18)

- Deploy activation identifies a synced application by its labels only, so an application whose name cannot be parsed is reported instead of attributed to the wrong team (principle_violations#15)

- Refused cell pins (unknown cell, cell at capacity) now return a client error instead of an internal error (principle_violations#26)

- Pinning a team to a cell is now all-or-nothing: a failure applying the pin reverts it and reports an actionable error instead of claiming success (principle_violations#25)

- Every user-facing app URL (deploy result and rollback) is now composed the same way, including the public domain (principle_violations#19)

- Rolling back to a release that was removed mid-rollback now fails with a clear "release not found" error instead of crashing (principle_violations#22)

- Deleting a project now prunes and syncs the project's own deploy application instead of a mis-named one (principle_violations#14)

- Bulk secret uploads and scale requests are refused with 503 when the account's plan cannot be read, instead of being checked against an unknown limit.

- The API no longer rejects every write with 503 MAINTENANCE_MODE when installed with the chart's default LOOPZERO_MAINTENANCE_MODE="false"; "false", "0", "off" and "no" now mean maintenance is off

- GET /api/v1/billing shows the plan that limits are actually enforced against (a canceled subscription no longer shows as its old plan), and answers 503 PLATFORM_DEGRADED instead of 500 when the plan cannot be read

- POST /api/v1/environments answers 409 ENV_ALREADY_EXISTS when the environment already exists in the project (it answered 201), and no longer reports unrelated failures as conflicts

- POST /api/v1/admin/tenants/evict returns the job id the eviction actually ran under, and no longer echoes internal errors when tenant matching fails

- GET /api/v1/builds/{id}/logs answers 503 LOGS_UNAVAILABLE when logs cannot be read right now, instead of an empty list that looked like the logs were gone

- `loopzero domain dns` works: GET /api/v1/domains/{domain}/dns-records is now served (it answered 404), and a server-side failure answers 500 instead of "domain not found"

- API request logs now include the trace_id and span_id of the request, so a log line can be joined to its trace

- Rate-limited responses (429) now carry Retry-After as whole seconds computed from the actual limit window, instead of values like "45.3s" or a fixed 60

- Requests sent with an Idempotency-Key are refused with 413 when too large instead of being processed truncated, a failed (5xx) attempt is no longer replayed to retries, and a key reused on a different endpoint is rejected

- POST /api/v1/builds answers 400 (not 500) for an invalid app name or path, and environment lookups that match several projects answer 400

- POST /api/v1/builds validates app names and paths before creating a new environment, so a rejected deploy no longer provisions one

- Scaling a project (POST /api/v1/projects/{name}/scale) is now recorded in the audit log as project.scaled instead of project.created

- GET /api/v1/projects/{name}/signals/stream now delivers status signals, and only those of the environment requested with ?env=

- Cross-origin PATCH requests (changing a team member's role) now pass the CORS preflight

- POST /api/v1/domains and POST /api/v1/projects/{project}/previews no longer count each request twice against the rate limit

- POST /api/v1/secrets/bulk now enforces the plan's per-environment secret limit and answers 402 PLAN_LIMIT_EXCEEDED when an upload would exceed it

- Creating, updating and deleting secrets now records SECRET_CREATED / SECRET_UPDATED / SECRET_DELETED entries in the team audit log

- POST /api/v1/secrets/bulk now serializes with deploys per environment, so a deploy never captures a half-applied bulk upload; a busy environment fails only its own rows

- POST /api/v1/source/upload now actually rejects a multipart body whose first field is not services_json (400 MULTIPART_FIELD_ORDER), validating services.json before reading the bundle

- `no-migrate-force` now fails on a file it cannot read or a line too long to scan, instead of skipping it (principle_violations#257)

- `silent-surface` no longer counts calls on repositories or responses as route registrations (principle_violations#273)

- `transit-coverage` now parses the whole secret-store package, so a raw encrypt/decrypt call hidden after a brace in a string, or moved into another store file, is caught (principle_violations#272)

- The openbao-init chart no longer declares the `k8s-cell-0` auth mount twice, and `openbao-init-auth-mounts` reads the mounts from the chart and refuses duplicates (principle_violations#268)

- `sqlc-tx-prop` now checks the database stores, where a pool-bound query inside a transaction can actually occur; it previously scanned code that can never import the generated queries and passed vacuously (principle_violations#267)

- `worker-csp` now fails when the worker sets no Content-Security-Policy or no script policy at all (principle_violations#266)

- `scheduled-image` now reports an image expression it cannot resolve instead of treating it as a third-party image (principle_violations#265)

- `publish-hygiene` no longer drops the findings of a file that contains a very long line (principle_violations#264)

- Ten quality gates (including `orphan-alerts`, `mock-budget`, `no-shell-wrap`, `security-context`, `prod-image-digests`, `sqlc-sync`) now fail when they cannot read a file or scanned nothing, instead of passing (principle_violations#263)

- `ory-kratos-public-urls` now fails when an environment does not render, instead of reporting SKIP (principle_violations#262)

- `test-deletion`, `no-new-bash` and `quarantine-protect` now fail when the base ref or diff range cannot be read, instead of passing over a comparison they never made (principle_violations#261)

- Four quality-gate tests that checked the real repository had been skipping on every run; they now run and fail when the repository root cannot be found (principle_violations#249)

- The `no-legacy-sa-token-secret`, `no-pulumi-inline-secrets` and `no-aes-remaining` gates now fail on a file they cannot read instead of passing with a warning (principle_violations#248)

- The `trivy-scan` quality gate now fails when Trivy is missing or cannot complete a scan, instead of reporting SKIP and exiting 0 over an unscanned tree (principle_violations#247)

- Resource-name composers (namespaces, repositories, application and secret names) name the missing field when an input is empty, e.g. "namespace: tenant slug must not be empty", instead of a generic "argument must not be empty".

- advisory-lock-scope, correlation-header and crd-semantic-diff now fail when they find nothing to scan (a missing source tree, or a --crd filter that matches no CRD) instead of reporting PASS, and print how many files they scanned

- dev-public-oidc-redirect-syntax now reports the 10 assertions it evaluates; it advertised 9

- coverage-baseline no longer drops coverage for files whose profile path contains a drive letter

- ldflag-symbols now fails when a -X target has the right name but is not a string variable, and cli-exit-codes no longer skips a CLI checkout located under a __tests__ directory

- helm-overlap-parity orders pre-release chart versions per semver (rc.10 after rc.2)

- The cosign and service-auth deadline ratchets no longer count a marker date as a day older in the evening in negative UTC offsets

- k6-load-rigor validates the L5 manifest YAML in-process and no longer passes when python3 is missing or fails when PyYAML is missing

- helm-overlap-parity no longer passes on a truncated render when helm prints warnings or a rendered document fails to parse

- lint-coverage no longer records a golangci-lint run that crashed as a module with zero findings

- Six quality gates (acyclic-graph, alert-runbooks, billing-layers, chart-pairs, drill-outputs-consumers, cost-series-producers) now fail when a file in scope cannot be read or parsed instead of silently skipping it

- ecosystem-validators, clusterimagepolicy-mode and docs-contract-refs no longer change their verdict depending on whether the gitignored .claude kit is installed on the machine

- divergence-matrix reads the local-vs-prod matrix from docs/operations.md and runs in quality:gates instead of failing on every clone

- billing-layers no longer fails on every fresh clone: open debts are read from the versioned registry instead of the unversioned BACKLOG.md

- contract-consumers now scans the dashboard and CLI sibling checkouts for readers of a dropped JSON field, reports PARTIAL when one is absent, and fails when git grep errors instead of passing

- decisions-deletion now guards docs/decisions.md and docs/runbooks.md and fails when git cannot resolve the base or head ref instead of reporting a clean result

- coverage-diff now fails when the touched-lines file is missing or unreadable instead of reporting an empty-diff PASS

- helm-diff-structural now fails when it cannot compute the diff instead of silently skipping the security-context downgrade checks

- Secret backups written by the build-cluster and runtime-cluster secret generators can now be restored with `kubectl apply`; they used to keep server fields that made the apply fail once the Secret had changed.

- Drill cells' credentials now carry the `loopzero.io/cell` label the multi-cell runbook selects on.

- `LOOPZERO_HELMFILE_ENV=dev-public task setup` (and standalone-public) now installs the build and cell clusters; those environments were unknown to the build and cell helmfiles, which stopped the setup with exit 3.

- A fresh local setup now installs the cell's image admission policy in the environment's configured mode (warn locally) instead of silently switching it to enforce.

- The published loopzero-cloud chart's ServiceMonitor now targets the port the Service actually exposes (`http`) when enabled.

- Setting `serviceAccount.automountServiceAccountToken: false` on the published operator and API charts now takes effect; it was silently forced to true.

- The published admission-policies chart no longer lets the tenant-namespace policy block the platform's own controllers, which could leave a Tenant impossible to delete; it also gains the image-digest warn-rate alert the bootstrap chart already had.

- KEDA and the KEDA HTTP add-on on workload cells now run with their configured resource limits, non-root user and interceptor replica bounds; dev and prod cells get two KEDA replicas with disruption budgets. The values had been nested under a key the installed charts never read.

- Gitea HA (dev/prod) now actually applies its Redis global lock, LFS server and console logging, and Tempo now keeps traces for 7 days with its resource limits and hardened container security context — both values files declared their main key twice and Helm silently dropped the first block.

- Secret rotation now refreshes exactly the ExternalSecrets that read the rotated path, including a key another binary reads through an override: rotating the Gitea token refreshes and restarts both the API and the tenant operator, which read the same path.

- The drill cost watchdog and teardown validator now fail when DigitalOcean cannot be queried, instead of reporting that no drill resources are left, and the watchdog runs the validator again.

- The drill bootstrap creates both cells again and, when clearing a stale leader lock, no longer terminates database sessions holding deploy locks.

- The local cluster heal now repairs cell-0 and cell-1 under their current ids (it previously targeted the wrong cell and an id the bootstrap refuses), and the infrastructure check reports each cell once.

- The e2e seed no longer strips environments added by earlier journeys when their list cannot be read, keeps each environment's project, and waits for the namespaces the platform actually creates.

- The Harbor post-install job no longer treats a truncated or non-200 health response as a health report.

- The registry mirror tool now fails when it cannot list a repository's tags, instead of reporting success for a partial mirror.

- Secret-store responses that fail to read are reported as errors instead of being parsed as empty.

- The circuit breaker lets a single trial request through while recovering, instead of letting every waiting request hit a dependency that may still be down.

- Build submission failures now return 500 with their own error code instead of an unmapped status.

- Validation errors for an invalid app name or path, an environment name shared by several projects, and scale-to-zero where it is unavailable now return 400, and an environment that does not become ready in time returns 504, instead of 500.

- A project's status now reflects every release state (including rolling back, rolled back and superseded) instead of falling back to a generic state.

- Legacy "growth" subscriptions get Pro limits instead of being denied everything, and an unrecognised plan is reported rather than downgraded to Starter.

- Plan limits (projects, builds per month, permanent environments, seats) now refuse the operation when usage or the plan cannot be read, instead of silently allowing it or treating the account as Starter.

- The production migration fallback no longer records a failed or partial migration as applied: it stops at the first failing file, leaves the schema marked dirty, and refuses to run over a dirty schema.

- The services-acyclic quality gate governs the shared deploycontracts package at its new home, api/internal/deploycontracts, and refuses to run when its own allow-list permits an import cycle or names an undeclared context, instead of passing the cycle it exists to forbid.

- The DR drill declares each cell once (cell-0, cell-1); it declared cell-0 twice after the cell renumbering.

- .github/branch-protection-config.yml requires the CI gate check that ci.yml actually reports instead of 13 checks from deleted workflows.

- task pprof:operators takes OPERATOR=tenant|app|preview|platform and api:hotfix no longer restarts a deployment that no longer exists.

- task audit:security-context, task dev-public:patch-kratos and task validate:matrix run their current implementations instead of deleted scripts.

- task validate:helmfile-templates renders the current core, build and cell helmfiles instead of helmfiles that no longer exist, and shows render errors.

- task infra:install:monitoring-crds and task test:smoke now cover every cell cluster; cell-1 was skipped.

- task drill no longer aborts at Phase 5 (cell bootstrap) and leaves the paid drill stack running; it bootstraps cells 0 and 1.

- Three more tests that had stopped matching the repository. Two read documents the
  consolidation moved, and now read history through a shared helper that tries disk first
  and fails loudly when neither answers. The third asserted two operator alerts that were
  retired on purpose in September; it asserts their absence now, so a closed-window watcher
  cannot drift back without someone re-opening the reasoning that retired it.

- A second chart test read a rule file deleted from the worktree. Like the guard beside it,
  it reads git history when the document is not on disk — the assertion is about what the
  rule SAID, and history holds that exactly.

- Seven citations in versioned files that pointed at documents which no longer exist. Four
  were in production operator comments and three in the test that exists to prove such
  citations resolve — it checked the working tree, and the 2026-09-11 consolidation had
  taken two of its three subjects. It reads git history now, which is where a consolidated
  document actually goes. The guard that caught the operator comments ships instead of
  staying untracked.

- `cluster-rename-guard` reports on the rename again, in a second instead of six minutes.
  Its baseline was deleted with 1,380 other files and is restored to `infra/quality/`,
  where it is versioned; its pinned commit is translated rather than re-captured, because
  re-capturing would compare the new state with itself; the gate now tells a `git grep`
  that answers "not found" from a `git grep` that could not run, which is what made it
  read 554 MB of `.git` per absent occurrence; and it honours the `amendments` field the
  baseline has carried since August, which no code had ever read.

- The allowlist sunset contract is enforced again. `check-allowlist-sunsets` was a
  contract test for a Python script that exists in no clone and no installed kit, so it
  reported a missing program instead of a lapsed deadline — and two code-review exemptions
  passed their 2026-09-09 sunset unnoticed. The check runs in Go now, reads the allowlist
  directly, and warns on the last day instead of blocking it. The two expired entries are
  removed, and the work they deferred is tracked at #368.

- The `clusterimagepolicy-mode` gate can be satisfied again. It demanded a recorded
  sign-off before admission control may reject unsigned images, and searched three
  directories that do not exist — so no action could pass it, while the remedy it printed
  named the first of the three. It reads `docs/decisions.md` first now, and the deny-flip
  decision is recorded there with its scope: enforce in clusters we own, warn in the
  chart a self-hoster installs.

- `helm-topology-index --write` no longer replaces `docs/architecture.md`. It built the
  whole document — header, table, footer — and wrote it over whatever was there, which
  suited the dedicated file it used to own and destroyed the living document the
  2026-09-11 consolidation made its default. It splices between its markers now, and
  refuses an existing document that has none rather than overwriting it.

- The Helm topology index is back in `docs/architecture.md`, alongside the package usage
  matrix — the same consolidation deleted both sets of markers, and both gates had been
  failing against a section that no longer existed. Its generated block is in English now
  too. It indexes 43 charts and lists three under `infra/helm/` that no helmfile
  mentions: `k6-operator`, `sigstore-extras` and `loopzero-external-secrets`.

- The CLI exit-code contract is checked against the CLI again. Both subjects were declared
  absent — one because the tree moved to a sibling repository at M2, the other because
  that sibling was not checked out — and the sibling has since been cloned, so the gate
  now scans its 81 source files instead of recording why it could not.

- Three places where the secret-redaction pass replaced something that was not a secret.
  `tools/gen-service-auth-ed25519` stopped compiling because the replacement landed on a
  struct field access inside the test that proves keypair generation is random, and two
  `task` targets lost a Grafana credential without gaining a way to obtain one. The
  remaining nine occurrences are in comments and are tracked in #367.

- The package usage matrix is back in `docs/architecture.md`. The 2026-09-11
  consolidation deleted the document that carried its markers, so the gate had been
  failing against a section that no longer existed anywhere. The generated block is also
  in English now — it had been writing Portuguese prose into a versioned document on
  every run (#366 tracks two columns of it that count directories removed at M2/M4).

- Five test skips that reported an inability to measure as a pass. Four guards over
  tracked files now fail instead of skipping — an absent CLAUDE.md, an absent tracked
  manifest, and a ClusterRole finder returning zero all mean the harness is broken, not
  that there is nothing to check. The SHA-pin verification behind every pinned action
  now fails under CI on a shallow clone, where a silent skip reported an unverified
  supply chain as a clean one.
- Four known-debt skips named backlog ids that do not travel with a clone. They now cite
  GitHub issues (#361, #362, #363, #364), and their identifiers describe what they arm
  rather than which ticket filed them.

- The pre-push quality gates ask the convention check about what the push introduces,
  not about the last forty commits. Grading commits already on the remote made every
  push fail on violations no amend could reach, and the only remedy the arithmetic
  allowed — more commits — was the thing being refused. The standalone audit keeps the
  forty-commit window, where grading history is the question being asked.

- The records-tree gate named the root that `rules/records-location.md` retired on
  2026-09-09, so it began failing the moment the last file left the old path — reporting
  that it could not find the tree it governs, which is exactly what it should do when
  pointed at the right one.
- Two more citations survived the six-document consolidation only as names: the OpenBao
  transit cutover procedure, which the `loopzero-api` chart had stopped citing entirely, and
  the runbook needle that checked for it.

- The local development edge no longer hardcodes one machine's home directory. The dashboard
  root in `infra/local/Caddyfile` reads `{$LOOPZERO_DASHBOARD_DIST:../loopzero-cloud/dashboard/dist}`,
  so the file works from any checkout instead of exactly one, and the runbook naming that path
  was corrected in the same change.
- Four tests named documentation paths that the six-document consolidation retired. Three were
  repointed at where the content actually lives; the fourth is retired, because its subject left
  with the dated audit trail and there is nothing to repoint at.
- Thirteen test helpers located the repository root by looking for a file this repository
  deleted, and reported the search rather than the cause.
- The Tiltfile named the pre-rename organisation and a four-minor-versions-old release of the
  contracts module that `api/go.mod` and `pkg/go.mod` already require.

- The shell-script allowlist is versioned again, at `infra/quality/shell-scripts-allowlist.md`.
  It lived under `.claude/`, which is an installed dependency and is not tracked, so six
  quality gates read a file that existed on one machine and in no clone — four of them
  failed for every contributor and every CI run. The 768-line record was recovered from the
  commit before the one that untracked the tooling tree; the content is unchanged.
- A missing allowlist now means *no exemptions declared* rather than a fatal read error, in
  the two gates whose question is "may this be exempt?". A gate whose default exempts
  nothing cannot be weakened by losing its file; a file that is present and unreadable is a
  different claim and stays fatal.
- Thirteen test helpers located the repository root by looking for `ROADMAP.md`, a file this
  repository deleted. They use `go.work` — which defines the nine-module workspace and
  exists by construction.
- Seven documentation citations pointed at `pkg/quality/`, relocated at M9. Four were in
  sqlc-generated files and were fixed in the `.sql` sources so regeneration preserves them.
- A guard against stale `pkg/quality/` citations reported itself, because a guard for a token
  must spell the token. Its sibling exempted itself by hardcoded filename and that exemption
  did not grow when the second guard was written; both now key on the class.

- Domain routing was FATAL for every repository in this project. `rules/domain-routing.txt`
  carried no domain rows at all — only comments plus a stray `grep` command that had landed
  in the file — so `route_domain.py` exited `UNROUTED` for all 7 repos and no backlog item
  could have been assigned to anyone. The table is now derived and verified.

- A deploy stuck in release activation no longer creates a duplicate release each time the
  platform fails to read the release it already has. The read failure was being mistaken for
  "this build has no release yet", which is the one condition that legitimately creates one;
  a database blip during activation could therefore leave up to ten releases behind for a
  single deploy, with nothing recorded to explain them. The failure is now reported and the
  attempt retried on the next pass, and a deploy whose read keeps failing still ends as a
  clear failure on the same schedule as before (#B-069)
- A failed Cloudflare activation check on a custom domain is now reported instead of being
  discarded silently. Domain activation was unaffected either way — the check only speeds up
  detection, and the next pass re-reads the zone regardless — but a rate-limited or failing
  Cloudflare account produced no trace at any log level, leaving slow activations with no
  explanation (#B-069)

### Security

- A team can no longer list the repositories of a GitHub installation that belongs to another team (#386)

- The admission policies no longer admit images from a Harbor host: `registries.harborPrefix` and the Harbor image globs are removed, and the registries block now refuses unknown keys (design_pattern_findings#30)

- Harbor ran in every environment, prod included, with the vendor's default admin password, an in-cluster database and Redis, and an Ingress, because its values were nested under the wrong key; it is removed, and the admin credential must be treated as exposed wherever Harbor was reachable (design_pattern_findings#30, #381)

- The bootstrap `loopzero-api` no longer holds cluster-wide `secrets` delete or `pods/exec`: deleting Secrets is granted only in its own namespace, and patching or deleting ArgoCD Applications sits behind a value that is off for ArgoCD installs (design_pattern_findings#29)

- The platform operator no longer holds cluster-wide Secret access: it reads and updates Secrets only in `loopzero-system` and writes ArgoCD's Secrets only in `config.argocdNamespace`, each through a namespaced Role, and caches Secrets in those two namespaces alone (principle_violations#149, principle_violations#150)

- The preview operator no longer holds cluster-wide Secret read: it reads cell kubeconfig Secrets only in `loopzero-system`, through a namespaced Role, and caches Secrets in that namespace alone (principle_violations#149, principle_violations#150)

- The app operator no longer holds cluster-wide Secret read: it reads cell kubeconfig Secrets only in `loopzero-system`, through a namespaced Role, and caches Secrets in that namespace alone (principle_violations#149, principle_violations#150)

- The tenant operator no longer holds cluster-wide read/write on Secrets or ConfigMaps: it reads Secrets only in `loopzero-system`, through a namespaced Role, and caches Secrets in that namespace alone (principle_violations#149, principle_violations#150)

- The `github-webhook-fixture` seed no longer puts the webhook HMAC secret on the `gh` command line or in its error and status messages; the request body is sent on stdin (principle_violations#53)

- The API refuses to start in production when reserved-key protection for service environment variables is set to warn-only (principle_violations#300)

- Error messages from source uploads, build submission and the build timeline now hide the same internal infrastructure terms, including ones previously shown on some paths (principle_violations#10)

- The Git hosting token used for source pushes is no longer visible in process listings (principle_violations#294)

- A configured certificate authority for the deployment sync service that cannot be loaded now stops startup instead of silently disabling TLS verification (principle_violations#275)

- Image signing no longer falls back to a shared signing identity when the environment name is not recognised; the build fails instead (principle_violations#303)

- Python builds now stop when a dependency has a known vulnerability, or when the dependency scan cannot run, instead of passing silently (principle_violations#9)

- The auth-failure audit throttle no longer trusts X-Forwarded-For from clients that are not a trusted proxy, so a client cannot bypass the 100/min limit by rewriting the header

- Connecting a repository can no longer take over a GitHub App installation that belongs to another team

- GET /api/v1/admin/releases/{id}/inspect no longer accepts the proxy service secret; it requires the admin service secret like every other admin endpoint

- GET /api/v1/builds/{id}/attestation no longer returns another team's build signing data; builds you do not own answer 404, and a server-side failure answers 500 instead of 'build not found'

- cluster-role-no-secrets-star no longer treats a chart-wide `if .Values.rbac.create` wrapper as a cutover guard; it now reports the cluster-wide secrets grants in the operator charts and fails when a ClusterRole template cannot be read

- The bootstrap loopzero-api role no longer grants every verb (including deletecollection) on the platform's own resources; it grants the same explicit verbs as the published chart.

- The tenant eviction tool no longer places the database password in a pod spec, rejects tenant patterns that are not a tenant-id prefix, and fails when the database cleanup fails.

- The cosign cutover emergency override is refused when its audit record cannot be written, and the override reason can no longer alter the audit SQL.

- Credentials the legacy Pulumi stack reads from LOOPZERO_* environment variables are now stored encrypted in stack state, like values set with pulumi config set --secret.

- Workflow permissions are declared per job rather than repository-wide. `packages: write`
  and `id-token: write` were granted to every job in the release workflow, including one
  that only reads the repository.
- The release workflow names the four secrets it passes to the workflow it calls, instead
  of `secrets: inherit`. A secret added later is no longer forwarded by omission.
- `persist-credentials: false` on all nine checkouts: the credential no longer stays in
  `.git/config` on the runner where later third-party steps can read it.
- A branch name is no longer interpolated into a shell command in the quality-gate step.
  On a fork pull request the branch name is chosen by the contributor.

### Known regressions

- **The consolidation broke the 82 inbound `docs/*.md` references that still resolved.** Of the
  154 distinct `docs/*.md` paths cited from code, configuration and ADR bodies, 72 were already
  dangling before the cut; all 154 dangle now. No content was lost — every target is at
  `git show 4f728eb7a:<path>` — but a reader following a source comment no longer lands on a
  file. Repointing them means editing hundreds of comments across the tree, which is a separate
  change.
- Links *between* the six consolidated documents all resolve; the regression is inbound only.

