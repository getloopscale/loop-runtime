---
type: audit
title: M20 Pre-Migration Baseline (2026-07-10)
---

# M20 Pre-Migration Baseline (2026-07-10)

**entries_count:** 118

Command: `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md`
Output: `118`

**18 target files verified present:**
- 17 `check-*_test.sh` in `infra/tests/`
- 1 `helm/ory-kratos-public-urls_test.sh` in `infra/tests/helm/`

**Delta expected post-M20:** -18 (allowlist 118 → 100)

**Cross-references:**
- Plan: `knowledge-base/plans/m20-hermetic-infra-tests-remainder-plan.md` v1.0
- Blueprint: [`knowledge-base/discoveries/blueprints/m20-hermetic-infra-tests-remainder-blueprint.md`](../discoveries/blueprints/m20-hermetic-infra-tests-remainder-blueprint.md) v1.0
