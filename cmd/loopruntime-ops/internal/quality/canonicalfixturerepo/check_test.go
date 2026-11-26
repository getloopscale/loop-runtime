package canonicalfixturerepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o644)
}

func TestCheck_Canonical_PASS(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "infra/tests/e2e/a.sh"), "REPO=getloopscale/loopzerokit-example\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("expected clean: %v", err)
	}
}

func TestCheck_LegacyBare_FAIL(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "infra/tests/e2e/bad.sh"), "REPO=getloopscale/loopruntime-repotest\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected FAIL: %v", err)
	}
}

func TestCheck_LegacyInComment_PASS(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "infra/tests/e2e/audit.sh"), "# legacy loopruntime-repotest superseded by canonical loopzerokit-example\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("expected allowlisted PASS: %v", err)
	}
}

func TestCheck_MissingDirs_PASS(t *testing.T) {
	if _, err := violationOf(Check(Config{RepoRoot: t.TempDir()})); err != nil {
		t.Fatalf("expected missing dirs PASS: %v", err)
	}
}
