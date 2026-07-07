// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package patchkratoscheckonly

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "scripts", "local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "patch-kratos-dev-public.sh"), []byte(body), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

const canonical = `#!/usr/bin/env bash
set -euo pipefail
DRY_RUN=0
SKIP_RESTART=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --skip-restart) SKIP_RESTART=1 ;;
    --check-only) DRY_RUN=1; SKIP_RESTART=1 ;;
  esac
done
if [ "$DRY_RUN" -eq 1 ]; then
  echo "(dry-run: no kubectl apply)"
else
  kubectl apply -f -
fi
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonical)
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_TargetMissingFails(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_NoCheckOnlyFlagFails(t *testing.T) {
	body := `#!/usr/bin/env bash
set -euo pipefail
DRY_RUN=0
case "$1" in
  --dry-run) DRY_RUN=1 ;;
esac
if [ "$DRY_RUN" -eq 1 ]; then :; fi
`
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing --check-only, got %v", err)
	}
}

func TestCheck_CheckOnlyMissesDryRunFails(t *testing.T) {
	body := `#!/usr/bin/env bash
set -euo pipefail
DRY_RUN=0; SKIP_RESTART=0
case "$1" in
  --check-only) SKIP_RESTART=1 ;;
esac
if [ "$DRY_RUN" -eq 1 ]; then :; fi
`
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when --check-only misses DRY_RUN=1, got %v", err)
	}
}

func TestCheck_CheckOnlyMissesSkipRestartFails(t *testing.T) {
	body := `#!/usr/bin/env bash
set -euo pipefail
DRY_RUN=0; SKIP_RESTART=0
case "$1" in
  --check-only) DRY_RUN=1 ;;
esac
if [ "$DRY_RUN" -eq 1 ]; then :; fi
`
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when --check-only misses SKIP_RESTART=1, got %v", err)
	}
}

func TestCheck_NoDryRunGuardFails(t *testing.T) {
	// --check-only sets both, but there is no DRY_RUN guard around apply.
	body := `#!/usr/bin/env bash
set -euo pipefail
DRY_RUN=0; SKIP_RESTART=0
case "$1" in
  --check-only) DRY_RUN=1; SKIP_RESTART=1 ;;
esac
kubectl apply -f -
`
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when no DRY_RUN guard exists, got %v", err)
	}
}

func TestCheck_RealRepoScriptPasses(t *testing.T) {
	// Sanity: the real repo patch-kratos-dev-public.sh must satisfy the invariants.
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, "infra", "scripts", "local", "patch-kratos-dev-public.sh")); statErr == nil {
			outcome, checkErr := Check(Config{RepoRoot: abs})
			if checkErr != nil {
				t.Fatalf("real script must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate patch-kratos-dev-public.sh within 8 parent dirs; skipping real-repo assertion")
}
