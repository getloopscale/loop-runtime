// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// gateSpec is one entry in the canonical gate registry: the stable gate name
// dispatched by runQuality's switch plus a one-line human summary.
//
// The registry is the single enumerable source of truth for the gate NAME set.
// The dispatch switch in quality_subcmd.go remains the execution surface; a
// build-time parity test (quality_registry_test.go) asserts the two agree so
// the `--list` oracle can never lie about which gates exist.
type gateSpec struct {
	Name    string
	Summary string
	// Run is the gate's entry point (flag parse -> Check -> print -> exit code).
	// json:"-" keeps gateSpec marshalable (a func field otherwise errors
	// json.Marshal). M9 Step 7: merging Name+Summary+Run into one entry makes
	// this literal the single source of truth per gate — no separate dispatch
	// switch to drift against (ADR 2026-07-03-m9-step7-gate-registration-mechanism).
	Run func([]string) int `json:"-"`
}

// gateRegistry is the SINGLE SOURCE OF TRUTH for every `loopzero-ops quality <name>`
// gate: Name + Summary + Run in one entry. runQuality dispatches by map-lookup
// over this slice (no switch); --list renders Name+Summary from it. The gate
// funcs live in quality_gates_*.go (thematic decomposition). Ordering here is
// not load-bearing (--list sorts); alphabetical for reviewability.
// A gate is added by ONE line here + its func in a theme file — nothing else.
var gateRegistry = []gateSpec{
	{"acyclic-graph", "Enforces the decomposition DAG (loopzero-contracts ◄── engine/cloud/cli); no import cycle.", runAcyclicGraph},
	{"advisory-lock-scope", "Forbids session-scoped pg_advisory_lock; only pg_advisory_xact_lock is allowed.", runAdvisoryLockScope},
	{"agpl-isolation", "Verifies AGPL-licensed code stays isolated from proprietary import paths.", runAGPLIsolation},
	{"alert-runbooks", "Every Prometheus alert has a linked runbook annotation.", runAlertRunbooks},
	{"anonymous-auth", "Forbids anonymous/unauthenticated access on protected API surfaces.", runAnonymousAuth},
	{"lint-coverage", "Todo modulo Go e medido (lint + gofmt), com catraca por modulo contra regressao.", runLintCoverage},
	{"scheduled-image", "Execucao autonoma (agendada ou por evento) nao pode usar imagem nossa sem caminho de publicacao nesta arvore.", runScheduledImage},
	{"billing-layers", "As camadas de cobranca precisam ser enumeraveis e com dono; divida fechada sem cobertura reprova.", runBillingLayers},
	{"billing-secrets", "Segredos de cobranca nao podem ter opcionalidade literal; a decisao e por valor.", runBillingSecrets},
	{"base-image-digest", "Base images are pinned by digest, not floating tags.", runBaseImageDigest},
	{"baseline-compare", "Compares a captured steady-state baseline against the current state.", runBaselineCompare},
	{"baseline-update", "Updates the committed steady-state baseline artifact.", runBaselineUpdate},
	{"caddyfile-csp", "Operator-local edge Caddyfile invariants (no CSP on app-dev, preserved headers, rate-limit).", runCaddyfileCSP},
	{"vap-delete-guardrail", "Boundary resources (Application/Tenant/Environment/ClusterCredential) AND their CRDs are protected from unauthorized DELETE (D4).", runVAPDeleteGuardrail},
	{"argo-app-identity", "The ArgoCD Application name is derived only from naming.ArgoAppName (dropping the project made two projects of a tenant collide on one Application).", runArgoAppIdentity},
	{"gomod-auth-org", "The org build files authenticate to matches the org go.mod requires (a drift makes git fetch unauthenticated and GitHub answer 'not found').", runGoModAuthOrg},
	{"helm-topology-index", "Index of which chart runs on which cluster, derived from the helmfiles (never a hand-written second copy).", runHelmTopologyIndex},
	{"worker-csp", "Cloudflare static worker CSP must not permit inline/eval script (#235).", runWorkerCSP},
	{"tenant-csp-passthrough", "Static-CSP Traefik middleware must not be attached to tenant routes (#59).", runTenantCSPPassthrough},
	{"ecosystem-validators", "Roda `check_xrefs.py` e `test_e2e_smoke.py`, que o protocolo de mudanca de golden rule exige verdes e que nenhum gate rodava (B-129). Reporta FAIL e WARN SEPARADOS: somar faria o aviso apagar a falha.", runEcosystemValidators},
	{"publish-hygiene", "Nenhum arquivo versionado VIVO carrega caminho de workstation. Registro datado e isento por desenho (docs/README.md proibe reescreve-lo). A metade de IP do gate irmao do loopzero-rag NAO foi transportada: contra esta arvore ela deu 104 achados quase todos falsos (strings de versao, CIDRs da Cloudflare, o alvo vivo declarado). B-130.", runPublishHygiene},
	{"test-target-coverage", "Todo PACOTE com teste e rodado por um alvo do Taskfile, ou tem isencao NOMEADA com razao e sunset. Granularidade de pacote, nao de modulo: um `./unit/...` marcava o modulo inteiro coberto enquanto 41 arquivos em tres subarvores nao rodavam (B-114). `go build` nao conta (B-087/B-088).", runTestTargetCoverage},
	{"env-prefix-parity", "As duas declaracoes de reservedEnvKeyPrefixes (API e dockerfile-lint) casam prefixo do MESMO jeito (exato, sensivel a caixa) e so divergem de conjunto no que tem isencao datada.", runEnvPrefixParity},
	{"skill-suites", "Roda as suites pytest de .claude/skills/*/tests/, que nenhuma task rodava. Advisory quando pytest ausente; FALHA quando a descoberta nao acha suite nenhuma, porque nao achar nada nao e o mesmo que nao achar nada errado (B-124).", runSkillSuites},
	{"skill-script-paths", "Toda invocacao de script num SKILL.md resolve nos DOIS layouts do ecossistema (.claude/ no plugin, raiz no standalone). Caminho nu quebra em um dos dois, e em qual depende de como o ecossistema foi instalado (B-120).", runSkillScriptPaths},
	{"docs-contract-refs", "Toda referencia .claude/rules/*.md no CLAUDE.md e todo ADR-NNNN citado em comentario Go resolve (ou tem excecao com prazo).", runDocsContractRefs},
	{"chart-default-tags", "loopzero-owned charts must not default their image tag to a mutable ref (#232).", runChartDefaultTags},
	{"caddyfile-dev-server-deny", "Defense-in-depth: app-dev block MUST 404 dev-server-only paths (/src/*, /.env, /@vite/*, /node_modules/*, /vite.config.*). Regression protection for loopzero-cloud#141.", runCaddyfileDevServerDeny},
	{"dev-public-billing-portal-syntax", "Hermetic contract for infra/tests/e2e/live/dev-public-billing-portal_test.sh — 8 assertions (bash -n / strict-mode / log+assert / billing endpoint / exit-code contract / allowlisted). M20 T2.1 Wave A gate 1/18.", runDevPublicBillingPortalSyntax},
	{"dev-public-oidc-redirect-syntax", "Hermetic contract for infra/tests/e2e/live/dev-public-oidc-redirect_test.sh — 9 assertions (bash -n / strict-mode / log+assert+test_provider / OIDC provider ref / Kratos browser flow path / exit-code / allowlisted). M22 T2.1 Wave A gate 2/17.", runDevPublicOIDCRedirectSyntax},
	{"ory-kratos-public-urls", "Hermetic helm-template golden test for Ory Kratos serve.public.base_url + CORS + cookie.domain + allowed_return_urls + dashboardRoute-mapped ui_url across standalone-public + dev envs. Advisory-fallthrough when helmfile absent. M22 empirical follow-up (the ONLY M22 target allowlisted — delivers allowlist -1).", runOryKratosPublicURLs},
	{"test-coverage-report", "Hermetic coverage gate for infra/tests/{unit,charts}/... — runs `go test -cover -json` against configured scope + threshold (default 30%). Advisory-fallthrough when `go` binary missing. M23 T5.1 closes F22 (coverage_pct not measured).", runTestCoverageReport},
	{"cli-exit-codes", "Enforces the permanent CLI exit-code contract (0/1/2/3/4/5 only).", runCLIExitCodes},
	{"clusterimagepolicy-mode", "Validates the sigstore ClusterImagePolicy enforcement mode.", runClusterImagePolicyMode},
	{"cluster-role-no-secrets-star", "Forbids unconditional cluster-wide secrets/configmaps RBAC grants.", runClusterRoleNoSecretsStar},
	{"contract-consumers", "Verifies cross-repo contract consumers stay in sync with the contract.", runContractConsumers},
	{"corrections-txt-closed", "Asserts CORRECTIONS.txt items are all closed.", runCorrectionsTxtClosed},
	{"correlation-header", "Correlation-ID header is forwarded across the request path.", runCorrelationHeader},
	{"cosign-cutover", "Enforces the cosign keyed→keyless cutover deadline ratchet.", runCosignCutover},
	{"coverage-diff", "Diffs test coverage against a baseline to catch regressions.", runCoverageDiff},
	{"coverage-merge", "Merges per-package coverage profiles into one report.", runCoverageMerge},
	{"crd-semantic-diff", "Semantic diff of CRD schemas to catch breaking spec changes.", runCRDSemanticDiff},
	{"dangling-data-refs", "Detects data/file references that no longer resolve after a change.", runDanglingDataRefs},
	{"dangling-script-refs", "Detects shell-script references that no longer resolve after a change.", runDanglingScriptRefs},
	{"dashboard-promtool", "Validates Grafana dashboard + Prometheus rules via promtool.", runDashboardPromtool},
	{"decisions-deletion", "Guards deletion of decision/ADR records.", runDecisionsDeletion},
	{"divergence-matrix", "Checks the local-vs-prod divergence matrix is current.", runDivergenceMatrix},
	{"dockerfile-no-docker-hub", "Forbids unqualified docker.io/Docker Hub base images.", runDockerfileNoDockerHub},
	{"drill-creds-isolation", "Ensures DR-drill credentials are isolated from production.", runDrillCredsIsolation},
	{"drill-outputs-consumers", "Verifies drill Pulumi-output consumers use the canonical schema.", runDrillOutputsConsumers},
	{"git-diff-to-lines", "Utility: converts a git diff into a changed-line set for other gates.", runGitDiffToLines},
	{"helm-diff-structural", "Structural pre-sync Helm diff to catch drift before apply.", runHelmDiffStructural},
	{"helmfile-context-parity", "A task que invoca um helmfile aponta para o cluster que o helmfile declara (B-044).", runHelmfileContextParity},
	{"domain-sink-purity", "Ratchet: the services DAG sink must not grow its persistence surface (B-049).", runDomainSinkPurity},
	{"routes-acyclic", "Deny-by-default fence over api/internal/routes subpackages: registered families only, no sibling edges (B-041).", runRoutesAcyclic},
	{"ldflag-symbols", "Refuses a `-X` ldflag injection whose import path or symbol does not resolve on disk — the linker accepts it silently and the binary keeps its default (B-053).", runLdflagSymbols},
	{"silent-surface", "Ratchet: refuses a NEW router surface guarded by `if cfg.X != nil` that does not report whether it was registered (B-104). Baseline may fall, never rise.", runSilentSurface},
	{"status-writer-contract", "Refuses a bare Status().Update anywhere in operators production code, including the local-alias and SubResource spellings (ADR 2026-08-12-status-writer-contract, B-085).", runStatusWriterContract},
	{"helm-drift", "Detects drift between rendered Helm charts and the deployed state.", runHelmDrift},
	{"helm-overlap-parity", "Keeps the dual Helm chart systems (infra/helm vs charts) in parity.", runHelmOverlapParity},
	{"migration-test-coverage", "Every DB migration has corresponding test coverage.", runMigrationTestCoverage},
	{"mock-budget", "Caps the number of mocks per test (SRP smell budget).", runMockBudget},
	{"no-aes-remaining", "Calendar-gated: fails on remaining AES code paths post-transit migration.", runNoAESRemaining},
	{"no-env-vault-secrets", "Forbids secrets-as-env-var regression after the ESO/OpenBao migration.", runNoEnvVaultSecrets},
	{"no-legacy-sa-token-secret", "Forbids legacy kubernetes.io/service-account-token Secrets.", runNoLegacySATokenSecret},
	{"no-migrate-force", "Forbids forced/destructive DB migration operations.", runNoMigrateForce},
	{"no-new-bash", "Blocks new .sh files not in the shell-scripts allowlist.", runNoNewBash},
	{"no-pulumi-inline-secrets", "Forbids inline secret literals in Pulumi code.", runNoPulumiInlineSecrets},
	{"no-shell-wrap", "Forbids thin shell wrappers around pure-logic that belongs in Go.", runNoShellWrap},
	{"webhook-canonical-path", "Blocks the /engine/api/v1/webhooks/github 301 trap; enforces the canonical /api/v1/webhooks/github (M-Migrate2 2026-07-07 replaces infra/tests/check-webhook-canonical-path_test.sh).", runWebhookCanonicalPath},
	{"oidc-cred-admin-loopzero", "Rejects fabricated paulo@loopzero.test/LoopZeroDev2026! credentials; canonical is admin@loopzero.example/loopzero-dev-password (M-Migrate2 2026-07-07 replaces infra/tests/check-oidc-cred-admin-loopzero_test.sh).", runOIDCCredAdminLoopzero},
	{"canonical-fixture-repo", "Rejects legacy loopzero-repotest fixture in LIVE test/seed scripts; canonical is getloopscale/loopzerokit-example (M-Migrate2 2026-07-07 replaces infra/tests/check-canonical-fixture-repo_test.sh).", runCanonicalFixtureRepo},
	{"install-git-hooks", "Validates the M8 1.5 pre-push hook + installer canonical shape + idempotency (M-Migrate2 2026-07-07 replaces infra/tests/check-install-git-hooks_test.sh).", runInstallGitHooks},
	{"caddyfile-no-docker-ip", "Blocks hardcoded 172.18.x.x Docker network IPs in infra/local/Caddyfile + enforces canonical https://127.0.0.1:8443 upstream in both wildcard blocks (M-Migrate2 2026-07-07 replaces infra/tests/check-caddyfile-no-docker-network-ip_test.sh — closes Red Team loopzero-cloud#127 guard).", runCaddyfileNoDockerIP},
	{"vault-bootstrap-unseal-loop", "Regression guard for the 2026-06-15 canonical fix of vault-bootstrap-dev.sh Steps 2+3 (rc-independent bao-status parse, capitalized 'True' compare, no lowercase 'true' fallback). M-Migrate2 2026-07-07 replaces infra/tests/check-vault-bootstrap-unseal-loop_test.sh.", runVaultBootstrapUnsealLoop},
	{"option-b-eso-deploy-doc", "Locks docs/operations/option-b-mgmt-helmfile-eso-deploy.md contract (4 sub-paths named, B1 recommended, B4 rejected, canonical commands + rollback + decision matrix + cross-references present, ESO version drift check against runtime helmfile). M-Migrate2 2026-07-07 replaces infra/tests/check-option-b-eso-deploy-doc_test.sh.", runOptionBEsoDeployDoc},
	{"openbao-bootstrap-in-core-task", "Proibe instalar o plano de secrets no cluster mgmt: reprova a task bootstrap-in-core e qualquer helmfile de secrets apontado para contexto mgmt, em todo taskfiles/*.yml (B-047, ADR 2026-08-12-openbao-in-core-rule-vs-task).", runOpenBaoBootstrapInCoreTask},
	{"dev-public-loopzero-external-secrets-overlay", "Locks post-T1.2 workload-identity contract for infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl (enabled=false safe default, K8s-auth shape, no legacy AppRole roleId/secretRef, canonical mgmt DNS, EC-1/EC-2 traceability). M-Migrate2 2026-07-07 replaces infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh.", runDevPublicLoopZeroExternalSecretsOverlay},
	{"check-allowlist-sunsets", "Hermetic contract test for .claude/scripts/check-allowlist-sunsets.py (T5.2.a): 5 fixture cases + missing-file + LIVE rules/code-review-allowlist.txt check. Exits 0 for future/today/missing/live-clean; catches past-sunset (exit 1) and malformed/bad-date (exit 2). Requires python3. M-Migrate2 2026-07-07 replaces infra/tests/check-allowlist-sunsets_test.sh.", runCheckAllowlistSunsets},
	{"cluster-rename-guard", "Proves the cluster rename (B-001: mgmt->core, vault->secrets) touched only cluster identifiers. Asserts every line a pre-rename baseline classified GENERIC still exists byte-for-byte — `vault` carries 74 such lines naming OpenBao, `mgmt` carries zero. Refuses to run when the baseline cannot be proven an ancestor of HEAD, because a baseline captured after the first edit compares the new state with itself and passes forever. T0.1 of plans/cluster-rename-core-secrets-plan.md.", runClusterRenameGuard},
	{"route-auth-coverage", "Enforces R2-F8 (HIGH) — every api/internal/server/router.go route MUST be inside middleware.Auth group OR registerProtectedRoutes OR explicit public-allowlist. Locates group ranges via brace-counter, enumerates r.{Get,Post,Put,Patch,Delete,Method}( calls. M-Migrate2 2026-07-07 replaces infra/tests/check-route-auth-coverage_test.sh.", runRouteAuthCoverage},
	{"k6-load-rigor", "k6 load-harness rigor: dropped_iterations Counter threshold in `base` (EC-2), L5 distributed manifest shape (TestRun kind + pinned image + secretKeyRef + no inline secret, EC-1 YAML validity), k6:distributed:l5 task wired, README per-level runner doc. M-Migrate2 2026-07-07 replaces infra/tests/check-k6-load-rigor_test.sh.", runK6LoadRigor},
	{"skill-paths", "T0.1 EC D5: every infra/scripts/ path AND every `task X` invocation cited in any .claude/skills/*/SKILL.md MUST resolve on disk. Ports the bash awk task-name extractor + flattened/namespaced include set walk into Go. Skips task-automation skill (generic third-party). Enforces dogfood no-GH_TOKEN/.env + to-plan no-`task teardown` anti-patterns. M-Migrate2 2026-07-07 replaces infra/tests/skills/skill-paths_test.sh.", runSkillPaths},
	{"dev-public-env", "Locks dev-public.yaml canonical shape: publicAuthUrl/publicCloudUrl/uiBaseUrl/dashboardOrigin point to *-dev.loopzero.example, cookieDomain=loopzero.example, CORS + allowedReturnUrls cover both dev hosts with trailing-slash variants, no localhost or legacy non-dev URLs leak, D5 OAuth reuse (no _DEV env var override), Stripe webhook per-env split documented. M-Migrate2 2026-07-07 replaces infra/tests/helm/dev-public-env_test.sh.", runDevPublicEnv},
	{"ory-kratos-lifespan", "Locks canonical self-service flow lifespans in infra/helm/ory-kratos/values.yaml.gotmpl: login=10m + registration=10m + recovery=15m (defense pair with edge rate_limit for Red Team #89 email-bomb closure). Section-scoped stricter than bash grep -A N (sibling lifespan can NOT leak). M-Migrate2 2026-07-07 replaces infra/tests/helm/ory-kratos-lifespan_test.sh.", runOryKratosLifespan},
	{"shellcheck-advisory", "Advisory shell-lint gate — runs shellcheck across infra/scripts/local + infra/tests/check-*-syntax_test + infra/tests/e2e/live/dev-public-*. Tolerates SC1091/SC2155/SC2086/SC2046. Advisory-only fallthrough when shellcheck not installed (WARN + exit 0). M-Migrate2 2026-07-07 replaces infra/scripts/local/shellcheck-advisory.sh.", runShellcheckAdvisory},
	{"loopzero-api-openbao-approle-envfrom", "T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync: loopzero-api chart unconditionally wires AppRole secret_id via envFrom.secretRef to K8s Secret loopzero-openbao-approle (optional=true). Anti-hardcode gate — NO inline value: for OPENBAO_VAULT_SECRET_ID. Renders chart via helm template subprocess (Anti-Pattern #8 legitimate); advisory fallthrough when helm absent. M-Migrate2 2026-07-07 replaces infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh.", runLoopZeroApiOpenbaoApproleEnvfrom},
	{"render-schema-gen-sql", "T0.2 sqlc schema render pipeline: ephemeral PG16 + golang-migrate replay + pg_dump inside container + post-process (drop ALTER OWNER TO, banners, SET, restrict, orphan --, collapse blanks, trim trailing WS) + sqlc compile pre-flight with throwaway probe. Default check-only mode validates toolchain + migrations dir. Pass --render for destructive pipeline. M-Migrate2 2026-07-07 replaces infra/scripts/ci/render-schema-gen-sql.sh.", runRenderSchemaGenSQL},
	{"credentials-sot", "T1.1 SoT drift audit — every Secret created by canonical generator scripts (generate-secrets.sh, generate-build-secrets.sh, generate-runtime-secrets.sh, provision-api-tokens.sh) MUST be enumerated in docs/operations/credentials-required.md. Also asserts 4 mandatory drill env vars + Chart-hardcoded credentials section + Rotation playbook section. M-Migrate2 2026-07-07 replaces infra/tests/docs/credentials-sot_test.sh.", runCredentialsSot},
	{"pulumi-required-secrets", "T1.5 — every Pulumi.<env>.yaml enumerates required secret keys (as `secure:` entry OR comment). Prod=6 keys, staging=4, drill=3 (DO token via env var per D7' sponsor override). Cross-checks prod keys also appear backticked in docs/operations/credentials-required.md. M-Migrate2 2026-07-07 replaces infra/tests/docs/pulumi-required-secrets_test.sh.", runPulumiRequiredSecrets},
	{"sprint-syntax", "Hermetic contract test for infra/scripts/local/check-sprint.sh + its homed helper check_sprint.py: script exists + bash -n clean + helper compiles (python3 -m py_compile) + no python3 heredoc (T3.2 de-costume gate) + JSON report shape (pass count + spaced-detail preservation + [id,status,detail] tuple layout) + allowlist entry present. Requires bash + python3 on PATH. M-Migrate2 2026-07-10 M16 T2.1 Wave 1 replaces infra/tests/check-sprint-syntax_test.sh.", runSprintSyntax},
	{"archive-script-repo-root", "T1.2 hermetic guard of plan m1-commit-canonical-30-fixes: no script in archive/canonical-tests-v1/scripts/local/ may resolve REPO_ROOT with 3-dirs-up ($(dirname \"$0\")/../../..) — canonical post-quarantine 289cf9d6 is 4-dirs-up. Pure filepath.Walk + regex; missing scan dir treated as no-content. M-Migrate2 2026-07-10 M16 T2.2 Wave 1 replaces infra/tests/check-archive-script-repo-root_test.sh.", runArchiveScriptRepoRoot},
	{"no-cloud-dev-default", "T1.1 hermetic guard of plan m1-commit-canonical-30-fixes: no test/seed script in archive/canonical-tests-v1/scripts/local/, infra/tests/e2e/, or infra/scripts/local/ may declare a default env-var value pointing at cloud-dev.loopzero.example (Caddy 301 redirect trap post commit ee77b8da 2026-06-22). EC-1 absorption: /api/v1/webhooks/github handler is single exception (Caddyfile 142-156). M-Migrate2 2026-07-10 M16 T2.3 Wave 1 replaces infra/tests/check-no-cloud-dev-default_test.sh.", runNoCloudDevDefault},
	{"loki-vault-retention", "T8.2 Loki per-NS retention (retention_period 168h default + retention_stream secrets-system selector + period 2160h 90-day forensic) + T8.5 archive CronJob (kind + schedule 04:00 UTC monthly + concurrencyPolicy Forbid + runAsNonRoot + capabilities drop ALL + logcli/mc invocation + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY secretKeyRef + loki-archive-s3-creds Secret + secrets-system NS target). 14 assertions. M-Migrate2 2026-07-10 M16 T3.1 Wave 2 replaces infra/tests/check-loki-vault-retention_test.sh.", runLokiVaultRetention},
	{"loki-retention-schema", "Trio-compat regression for T8.2 Loki retention_stream schema: infra/helm/loki/values.yaml parses cleanly + deploymentMode SingleBinary (cleanroom-compatible) + exactly 1 retention_stream entry + selector secrets-system + period 2160h + priority int (Loki startup schema) + default retention_period 168h. 7 assertions. Uses gopkg.in/yaml.v3 (already-vendored, per plan D2 parsimony rung 4). M-Migrate2 2026-07-10 M16 T3.2 Wave 2 replaces infra/tests/check-loki-retention-schema_test.sh.", runLokiRetentionSchema},
	{"vault-prom-alerts", "T8.4 hermetic contract for PrometheusRule vault-audit-alerts: YAML parses + all 4 P1 alerts declared (VaultAuthFailureRateHigh + VaultPolicyDenialRateHigh + VaultSealedOrDown + VaultAuditLogStalled) + all severity=critical + all runbook_url annotations + all team=platform-security + all domain=vault + VaultAuditLogStalled carries invariant \"audit-log-must-flow\" (high-confidence adversary signal). M-Migrate2 2026-07-10 M16 T3.3 Wave 2 replaces infra/tests/check-vault-prom-alerts_test.sh.", runVaultPromAlerts},
	{"loopzero-external-secrets-chart-sanity", "iter 23 chart-sanity contract for infra/helm/loopzero-external-secrets: renders 5 canonical envs (vault-local/standalone-multi/dev/drill/prod) + inventory-based ExternalSecret count parity + metadata.name uniqueness + refreshInterval + loopzero-secrets-secrets ClusterSecretStore refs + vaultBinaryOverride api/secrets/* resolution (≥3) + envName empty fail-fast + no operators/gitea path leak. 8 assertions. Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.5 Wave 2 replaces infra/tests/check-loopzero-external-secrets-chart-sanity_test.sh.", runLoopZeroExternalSecretsChartSanity},
	{"loopzero-external-secrets-creation-policy", "iter 20 P0 regression: infra/helm/loopzero-external-secrets renders ExternalSecret CRs with creationPolicy: Orphan (NEVER Owner — Owner+blockOwnerDeletion cascade-deletes K8s Secrets on chart uninstall). 4 assertions: ≥1 ES CR + zero Owner + deletionPolicy Retain ≥ Orphan + Orphan==ES CR count. Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.6 Wave 2 replaces infra/tests/check-loopzero-external-secrets-creation-policy_test.sh.", runLoopZeroExternalSecretsCreationPolicy},
	{"openbao-init-auth-mounts", "T5.1-T5.9 openbao-init auth mounts + per-binary roles: 5 mounts (k8s-core/k8s-build/k8s-cell-0/k8s-cell-0/k8s-cell-1) + loopzero-api TTL 1h on k8s-core + loopzero-tenant/app/preview/platform-operator roles on k8s-core + argo-workflows on k8s-build + external-secrets-broker on each cell + isolation invariants (loopzero-api NOT on cells, ESO-broker NOT on core). Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.7 Wave 2 replaces infra/tests/check-openbao-init-auth-mounts_test.sh (Wave 2 COMPLETE).", runOpenbaoInitAuthMounts},
	{"orphan-alerts", "Detects Prometheus alerts with no owning dashboard/runbook.", runOrphanAlerts},
	{"pkg-usage-matrix", "Verifies the pkg/ cross-component usage matrix is current.", runPkgUsageMatrix},
	{"postgresql-pdb-config", "Validates the PostgreSQL PodDisruptionBudget configuration.", runPostgresqlPDBConfig},
	{"prod-image-digests", "Production manifests reference images by digest.", runProdImageDigests},
	{"prod-image-tags", "Forbids mutable :latest-style image tags in production manifests.", runProdImageTags},
	{"quarantine-protect", "Protects the canonical-tests-v1 quarantine from accidental edits.", runQuarantineProtect},
	{"regression-matrix", "Verifies the regression-test matrix covers required scenarios.", runRegressionMatrix},
	{"regression-test", "Asserts a regression test exists for tracked fixed bugs.", runRegressionTest},
	{"runtime-slo", "Validates runtime SLO definitions and thresholds.", runRuntimeSLO},
	{"script-count", "Tracks the total shell-script count against the reduction target.", runScriptCount},
	{"scripts-audit", "Audits shell scripts against the allowlist ledger.", runScriptsAudit},
	{"security-context", "Enforces pod securityContext (non-root, limits, probes).", runSecurityContext},
	{"service-auth-rotation", "Enforces the Ed25519 service-auth key rotation cadence.", runServiceAuthRotation},
	{"shell-hygiene", "Lints shell scripts for strict-mode and hygiene rules.", runShellHygiene},
	{"proxy-exposure", "Every edge reverse_proxy declares what it lets through.", runProxyExposure},
	{"skip-link", "Every t.Skip/it.skip carries an issue URL (or lives in *_skip_test.go).", runSkipLink},
	{"spotcheck-dimension", "Spot-checks a maturity-audit dimension against evidence.", runSpotcheckDimension},
	{"sqlc-sync", "Committed sqlc generated/ is in sync with the schema codegen.", runSqlcSync},
	{"sqlc-tx-prop", "Enforces pool-bound vs tx-bound query propagation inside withEnvironmentLock.", runSqlcTxProp},
	{"db-generated-boundary", "MA-7 Gate A: no services/routes/server imports internal/db/generated.", runDbGeneratedBoundary},
	{"ma7-status-locality", "MA-7 Gates B+C: sqlc status funcs + SafeTransition only in db/store/.", runMa7StatusLocality},
	{"services-acyclic", "M9 Step 6: services bounded-context imports form the frozen acyclic DAG.", runServicesAcyclic},
	{"stack-coverage", "Verifies the tool-inventory stack-coverage claims via grep.", runStackCoverage},
	{"taskfile-interface", "Validates the Taskfile public-interface contract.", runTaskfileInterface},
	{"template-compliance", "Checks documents comply with their canonical templates.", runTemplateCompliance},
	{"tenant-predicate", "Enforces the multi-tenant isolation predicate (security veto-backed).", runTenantPredicate},
	{"test-deletion", "Guards deletion of test files against coverage loss.", runTestDeletion},
	{"test-pyramid", "Validates the unit/integration/e2e test-pyramid balance.", runTestPyramid},
	{"test-targets", "Verifies Taskfile test targets exist and are wired.", runTestTargets},
	{"transit-coverage", "Enforces the OpenBao transit dispatcher invariant (encrypt/decrypt-for-store).", runTransitCoverage},
	{"trivy-scan", "Runs Trivy CVE scanning on filesystem/images.", runTrivyScan},
	{"trufflehog-scan", "Refuses a new unadjudicated secret candidate, a live secret, or a scan that could not run.", runTrufflehogScan},
	{"unhomed-logic", "Forbids un-homed operational logic that belongs in a pkg/quality gate.", runUnhomedLogic},
}

// gateRegistrySet returns the registry names as a set for membership checks.
// gateDispatch builds the name->gate lookup used by runQuality. It PANICS on a
// duplicate Name: a dup would otherwise silently collapse (map overwrite) while
// --list stayed byte-identical (both entries carry a non-nil Run) — the exact
// silent-un-wire failure mode M9 Step 7 must make impossible. A nil Run also
// panics (a registered gate with no entry point is a wiring bug). Tests
// (TestRegistry_CountNilAndNoDup) assert both conditions so the panic is never
// reached in a shipped binary; it is the fail-fast backstop.
func gateDispatch() map[string]gateSpec {
	m := make(map[string]gateSpec, len(gateRegistry))
	for _, g := range gateRegistry {
		if _, dup := m[g.Name]; dup {
			panic(fmt.Sprintf("quality: duplicate gate name %q in gateRegistry", g.Name))
		}
		if g.Run == nil {
			panic(fmt.Sprintf("quality: gate %q has nil Run (registered but not wired)", g.Name))
		}
		m[g.Name] = g
	}
	return m
}

func gateRegistrySet() map[string]bool {
	out := make(map[string]bool, len(gateRegistry))
	for _, g := range gateRegistry {
		out[g.Name] = true
	}
	return out
}

// taskWiredWord captures the gate name in a `loopzero-ops quality <gate>` invocation.
// The gate name must start alphanumeric ([a-z0-9]) so the tool's own meta-flags
// (`--list`, `--names`, `--json`, `--help`) are NOT counted as gates. Applied
// ONLY to command strings extracted from `cmds:` blocks (see collectCmdStrings),
// so prose, `desc:` block scalars, and comments cannot leak a false gate.
// Compiled once at package scope.
var taskWiredWord = regexp.MustCompile(`loopzero-ops quality ([a-z0-9][a-z0-9-]*)`)

// taskGateTasks returns, per gate name, the SORTED set of top-level task names
// in the given taskfile whose `cmds:` invoke that gate. Detection is STRUCTURAL:
// the taskfile is YAML-parsed and only command strings under `cmds:` sequences
// are scanned — a `desc:` mention or a `#` comment can never be mistaken for an
// invocation. A read or parse failure returns a typed error naming the path —
// never a silent empty map (which would mislabel every gate un-wired).
//
// Per-task attribution (MEET-2026-07-02-001 MA-1; hardened per the
// verification-architect sign-off, MEET-2026-07-02-002-adjacent) replaces the
// former boolean whole-file UNION. The union masked a silent un-wire: a gate
// dropped from `quality:gates` but still referenced by `quality:diff` (or a
// stray `echo`) kept `wired=true`, so the baseline stayed byte-identical and the
// un-wire went undetected. Attributing each gate to the EXACT task set makes any
// drop / move (gates→diff) / removal change the baseline bytes, and lets the
// positive-control test assert the oracle fires (which the boolean union could
// not express). It is also honest: it shows whether a gate is enforced by the
// full-scan hard set (`quality:gates`), the changed-files set (`quality:diff`),
// both, or neither.
//
// Known limitation (no occurrences in the live taskfile): a gate name embedded
// inside a shell string within a cmd (e.g. `echo "loopzero-ops quality X"`) attributes
// X to that task, and a gate invoked only via `preconditions:` (not `cmds:`) is
// not counted. Both are acceptable for the repo's convention (all real
// invocations are direct `- <path>loopzero-ops quality <gate>` cmds items).
func taskGateTasks(taskfilePath string) (map[string][]string, error) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		return nil, fmt.Errorf("read taskfile %s: %w", taskfilePath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse taskfile %s: %w", taskfilePath, err)
	}
	acc := map[string]map[string]bool{} // gate -> set of task names
	for taskName, cmds := range collectTaskCmds(&doc) {
		for _, cmd := range cmds {
			for _, m := range taskWiredWord.FindAllStringSubmatch(cmd, -1) {
				if acc[m[1]] == nil {
					acc[m[1]] = map[string]bool{}
				}
				acc[m[1]][taskName] = true
			}
		}
	}
	out := make(map[string][]string, len(acc))
	for gate, tasks := range acc {
		list := make([]string, 0, len(tasks))
		for name := range tasks {
			list = append(list, name)
		}
		sort.Strings(list)
		out[gate] = list
	}
	return out, nil
}

// collectTaskCmds maps each top-level task name (under `tasks:`) to the command
// strings reachable under ITS OWN `cmds:` sequence. Task definitions do not nest
// task definitions, so a task body's cmds are unambiguously that task's. This is
// the per-task counterpart to the whole-file collectCmdStrings, and is what makes
// the wired attribution task-scoped instead of a file-wide union.
func collectTaskCmds(n *yaml.Node) map[string][]string {
	out := map[string][]string{}
	root := n
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "tasks" {
			continue
		}
		tasksMap := root.Content[i+1]
		if tasksMap.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(tasksMap.Content); j += 2 {
			taskName := tasksMap.Content[j].Value
			// collectCmdStrings on a single task body returns exactly that
			// task's cmds (a task body carries at most one `cmds:`).
			out[taskName] = collectCmdStrings(tasksMap.Content[j+1])
		}
	}
	return out
}

// collectCmdStrings walks a parsed taskfile and returns every command string
// reachable under a `cmds:` sequence. A cmds item is either a scalar (the
// command) or a mapping with a `cmd:` key (Task's map form); `task:`/`defer:`
// items carry no shell command and are skipped. Nodes outside `cmds:` (desc,
// vars, comments) are never scanned.
func collectCmdStrings(n *yaml.Node) []string {
	var out []string
	var walk func(node *yaml.Node)
	walk = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				if key.Value == "cmds" && val.Kind == yaml.SequenceNode {
					for _, item := range val.Content {
						switch item.Kind {
						case yaml.ScalarNode:
							out = append(out, item.Value)
						case yaml.MappingNode:
							for j := 0; j+1 < len(item.Content); j += 2 {
								if item.Content[j].Value == "cmd" {
									out = append(out, item.Content[j+1].Value)
								}
							}
						}
					}
				}
				walk(val)
			}
			return
		}
		for _, c := range node.Content {
			walk(c)
		}
	}
	walk(n)
	return out
}

// runQualityList is the production entry point for `loopzero-ops quality --list`.
func runQualityList(args []string) int {
	return runQualityListTo(os.Stdout, args)
}

// runQualityListTo renders the gate inventory to w. Modes:
//
//	default  -> NAME\tTASKS\tSUMMARY  (one gate per line; TASKS = comma-joined
//	            sorted task names invoking the gate, or "-" if none)
//	--names  -> NAME                  (names only; feeds the per-gate liveness loop)
//	--json   -> [{"name","tasks":[...],"summary"}, ...]
//
// TASKS replaces the former boolean WIRED bit (per-task attribution — see
// taskGateTasks). Output is always sorted by gate name and deterministic, so it
// can be captured as a byte-stable baseline (the MA-1 oracle). Any drop / move /
// removal of a gate invocation changes a TASKS cell, turning the baseline diff
// (and CI) red.
func runQualityListTo(w io.Writer, args []string) int {
	fs := flag.NewFlagSet("quality --list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	namesOnly := fs.Bool("names", false, "print gate names only, one per line")
	asJSON := fs.Bool("json", false, "print the gate inventory as JSON")
	taskfile := fs.String("taskfile", "taskfiles/quality.yml", "taskfile scanned for per-gate task attribution")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	gateTasks, err := taskGateTasks(*taskfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quality --list: %v\n", err)
		return 1
	}

	specs := make([]gateSpec, len(gateRegistry))
	copy(specs, gateRegistry)
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })

	switch {
	case *asJSON:
		type row struct {
			Name    string   `json:"name"`
			Tasks   []string `json:"tasks"`
			Summary string   `json:"summary"`
		}
		rows := make([]row, 0, len(specs))
		for _, g := range specs {
			tasks := gateTasks[g.Name]
			if tasks == nil {
				tasks = []string{} // render [] not null
			}
			rows = append(rows, row{g.Name, tasks, g.Summary})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fmt.Fprintf(os.Stderr, "quality --list: encode json: %v\n", err)
			return 1
		}
	case *namesOnly:
		for _, g := range specs {
			fmt.Fprintln(w, g.Name)
		}
	default:
		for _, g := range specs {
			tasksCol := "-"
			if ts := gateTasks[g.Name]; len(ts) > 0 {
				tasksCol = strings.Join(ts, ",")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", g.Name, tasksCol, g.Summary)
		}
	}
	return 0
}
