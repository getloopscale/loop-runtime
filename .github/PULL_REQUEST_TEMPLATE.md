<!--
Plan 2026-05-21-comprehensive-quality-gates v1.1 T5.1 + D12.
Checked by the `template-compliance` gate, which needs the PR body as input:
  loopzero-ops quality template-compliance --pr-body "$(gh pr view N --json body -q .body)"
It is NOT wired into `task quality:gates` (a repo scan has no PR body) and the
workflow this line used to name, .github/workflows/template-compliance.yml, does
not exist — GitHub Actions stopped being the pipeline on 2026-05-23.
For bots (dependabot/renovate) + PRs labeled `dependencies`/`automated`:
template gate SKIPS (EC-5 fix).
-->

## Summary

<!-- 1-3 sentences: what + why -->

## Quality Gates Checklist

- [ ] Tests added for new code (or N/A justified below)
- [ ] Regression test added if fixing bug (or N/A)
- [ ] Coverage diff PASS (will be verified by CI)
- [ ] No tests deleted (or `test-deletion-approved` label + justification below)
- [ ] CHANGELOG.md entry added under `[Unreleased]` (or N/A)

### N/A justifications (if any)

<!-- Explain why any checkbox above is N/A. Reviewer validates. -->

## Test plan

<!-- Bulleted markdown of how to verify this PR.
     For bug fixes: include "Bug #N: <description>" + link to regression test. -->

## Related

<!-- Link to issue, related PRs, design docs. -->

---

<details>
<summary>Quality Gates reference (auto-enforced)</summary>

- **Coverage diff** (T2.1): new code must be ≥80% covered
- **Baseline regression** (T2.2): no package may drop below baseline-3pp
  (use `coverage-baseline-defer` label for legitimate refactor splits)
- **Regression test** (T5.2): `fix #N` / `Bug #N` requires regression test
- **Test deletion** (T5.3): requires `test-deletion-approved` label OR justification
- **Runtime SLO** (T6.1): no test >60s, no suite >10min
- **Flaky detection** (T6.2): auto-quarantine after 3 marks/7d
- **Test pyramid** (T6.3): unit ≥70%, integration ≤25%, E2E ≤5%
  (use `pyramid-defer` label for sprint deliverables)
- **Mock budget** (T6.4): max 5 mocks per test file
- **Skip-link** (T6.5): every `t.Skip()` requires issue URL

</details>
