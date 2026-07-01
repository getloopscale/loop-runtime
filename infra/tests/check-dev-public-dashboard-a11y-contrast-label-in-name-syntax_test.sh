#!/usr/bin/env bash
# Hermetic structural + source-presence test for Red Team loopzero/loopzero#163
# (MAJOR a11y): (1) contrast AA insuficiente na cor primária —
# `#995ffe` sobre bg `#1f1a2a` = 4.37 e `#ffffff` sobre bg `#995ffe`
# = 3.87 (WCAG 1.4.3 requer >=4.5); (2) label-content-name-mismatch
# (WCAG 2.5.3) em 2 elementos com texto visível cujo aria-label não
# contém o texto visível — command palette button + FAB onboarding
# panel.
#
# Fix multi-layer:
#  - dashboard/src/index.css: override --primary token para tonalidade
#    mais escura (passa AA vs branco) + override utilitário .text-primary
#    para usar --primary-glow (passa AA em bg escuro para foreground text).
#  - dashboard/src/components/layout/top-bar.tsx: aria-label do command
#    palette button começa com "Search" (o texto visível).
#  - dashboard/src/components/onboarding/onboarding-panel.tsx: aria-label
#    do FAB começa com "{completed}/{total}" (o badge visível).
#
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-dashboard-a11y-contrast-label-in-name_test.sh"
INDEX_CSS="../loopzero-cloud/dashboard/src/index.css"
TOPBAR="../loopzero-cloud/dashboard/src/components/layout/top-bar.tsx"
FAB="../loopzero-cloud/dashboard/src/components/onboarding/onboarding-panel.tsx"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
PASS=0
FAIL=0

assert() {
  local label="$1"; shift
  if "$@"; then
    PASS=$((PASS+1))
    echo "OK $label"
  else
    FAIL=$((FAIL+1))
    echo "FAIL $label"
  fi
}

# --- Section A: LIVE bash script exists + is well-formed ---
assert "A1 LIVE script exists"                    test -f "$SCRIPT"
assert "A2 bash -n syntax clean"                  bash -n "$SCRIPT"
assert "A3 strict mode set -euo pipefail"         grep -Fq 'set -euo pipefail' "$SCRIPT"
assert "A4 issue #163 cited in header"            grep -Fq '#163' "$SCRIPT"
assert "A5 slug cited"                            grep -Fq 'dashboard-a11y-contrast-label-in-name' "$SCRIPT"

# --- Section B: scenarios + preflight defined ---
assert "B1 scenario_a defined"                    grep -Eq '^scenario_a\(\)' "$SCRIPT"
assert "B2 scenario_b defined"                    grep -Eq '^scenario_b\(\)' "$SCRIPT"
assert "B3 scenario_c defined"                    grep -Eq '^scenario_c\(\)' "$SCRIPT"
assert "B4 preflight() defined"                   grep -Eq '^preflight\(\)' "$SCRIPT"

# --- Section C: flags wired ---
assert "C1 --skip-live flag"                      grep -Fq -- '--skip-live' "$SCRIPT"
assert "C2 --scenario= flag"                      grep -Fq -- '--scenario=' "$SCRIPT"
assert "C3 --json flag"                           grep -Fq -- '--json' "$SCRIPT"
assert "C4 --help flag"                           grep -Fq -- '--help' "$SCRIPT"

# --- Section D: OPERATOR HARD STOP + mandate + INQUEBRÁVEL feedback ---
assert "D1 OPERATOR HARD STOP header"             grep -Fq 'OPERATOR HARD STOP' "$SCRIPT"
assert "D2 cleanroom mandate cited"               grep -Fq 'cleanroom-up-live-test-mandate' "$SCRIPT"
assert "D3 feedback_issue_fix cited"              grep -Fq 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$SCRIPT"

# --- Section E: LIVE probes cite the a11y assertions ---
assert "E1 command palette Search literal probe"  grep -Fq 'Search' "$SCRIPT"
assert "E2 FAB visible-text literal probe"        grep -Fq 'Get your app live' "$SCRIPT"
assert "E3 primary token darker probe"            grep -Fq -- '--primary' "$SCRIPT"

# --- Section F: source-side fix landed (contrast override) ---
assert "F1 index.css exists"                      test -f "$INDEX_CSS"
assert "F2 --primary override in @theme"          grep -Eq '(--primary:[[:space:]]*oklch\(0\.(5[0-5]|4[5-9]))' "$INDEX_CSS"
assert "F3 comment cites #163"                    grep -Fq '#163' "$INDEX_CSS"

# --- Section G: aria-label mirror visible text (command palette + FAB) ---
assert "G1 top-bar aria-label starts with Search" grep -Fq 'Search — open command palette' "$TOPBAR"
assert "G2 FAB aria-label starts with X/Y"        grep -Fq '${completed}/${total}' "$FAB"

# --- Section H: exit code + governance + allowlist ---
assert "H1 exit code reflects FAIL"               grep -Fq 'FAIL_COUNT > 0 ? 1 : 0' "$SCRIPT"

# H2 + H3 use direct grep (bash '!' doesn't parse in the assert helper).
if grep -Fq 'Co-Authored-By' "$SCRIPT"; then
  FAIL=$((FAIL+1)); echo "FAIL H2 no Co-Authored-By trailer"
else
  PASS=$((PASS+1)); echo "OK H2 no Co-Authored-By trailer"
fi
if grep -Fq -- '--no-verify' "$SCRIPT"; then
  FAIL=$((FAIL+1)); echo "FAIL H3 no --no-verify bypass"
else
  PASS=$((PASS+1)); echo "OK H3 no --no-verify bypass"
fi

assert "H4 LIVE script in allowlist"              grep -Fq "$SCRIPT" "$ALLOWLIST"
assert "H5 hermetic guard in allowlist"           grep -Fq "check-dev-public-dashboard-a11y-contrast-label-in-name-syntax_test.sh" "$ALLOWLIST"

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
