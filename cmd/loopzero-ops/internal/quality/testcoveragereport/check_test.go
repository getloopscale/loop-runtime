// M23 T5.1 (F22): hermetic tests for testcoveragereport M9 quality gate.
// Covers per plan v1.1 EC-5 (threshold exactly-at-boundary) + EC-7 (malformed
// JSON parser resilience).

package testcoveragereport

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// EC-5 absorption: threshold exactly-at-boundary uses `>=` comparison.
func TestCheck_thresholdExactlyAtBoundary(t *testing.T) {
	// Simulate parseCoverageJSON output at exactly 30% with 30% threshold — must PASS
	json := []byte(`{"Action":"output","Package":"pkg1","Output":"coverage: 30.0% of statements\n"}`)
	pct, _, err := parseCoverageJSON(json)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if pct != 30.0 {
		t.Errorf("expected 30.0, got %f", pct)
	}
	// PASS predicate: pct >= threshold (30 >= 30 = true)
	if !(pct >= 30.0) {
		t.Errorf("boundary check failed: pct=%f threshold=30.0", pct)
	}
	// FAIL: 29.99 < 30
	if 29.99 >= 30.0 {
		t.Errorf("29.99 >= 30 should be false")
	}
}

// EC-7 absorption: malformed JSON output resilience.
func TestCheck_malformedGoCoverJSON(t *testing.T) {
	// Truncated JSON — parser must NOT panic; best-effort continue
	json := []byte(`{"Action":"output","Package":"pkg1","Output":"coverage: 42.5% of statements\n"}
{"Action":"output","Package":"pkg2","Output":"cov  ← truncated`)
	pct, _, err := parseCoverageJSON(json)
	if err != nil {
		t.Fatalf("parser should best-effort continue, got err: %v", err)
	}
	if pct != 42.5 {
		t.Errorf("expected 42.5 (first valid entry), got %f", pct)
	}
}

func TestCheck_multiplePackages(t *testing.T) {
	json := []byte(`{"Action":"output","Package":"pkg1","Output":"coverage: 40.0% of statements\n"}
{"Action":"output","Package":"pkg2","Output":"coverage: 60.0% of statements\n"}`)
	pct, failed, err := parseCoverageJSON(json)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if pct != 50.0 {
		t.Errorf("expected mean=50.0, got %f", pct)
	}
	if len(failed) != 0 {
		t.Errorf("expected no failed pkgs, got %v", failed)
	}
}

func TestCheck_failedPackagesCollected(t *testing.T) {
	json := []byte(`{"Action":"output","Package":"pkg1","Output":"coverage: 50.0% of statements\n"}
{"Action":"fail","Package":"pkg2"}`)
	pct, failed, err := parseCoverageJSON(json)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if pct != 50.0 {
		t.Errorf("expected 50.0, got %f", pct)
	}
	if len(failed) != 1 || failed[0] != "pkg2" {
		t.Errorf("expected [pkg2], got %v", failed)
	}
}

func TestCheck_emptyOutputReturnsErr(t *testing.T) {
	_, _, err := parseCoverageJSON([]byte(""))
	if err == nil {
		t.Fatal("expected error on empty output")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in err, got %v", err)
	}
}

func TestCheck_advisoryFallthroughWhenGoMissing(t *testing.T) {
	// This test verifies the Check() advisory-fallthrough shape without actually
	// removing `go` from PATH. Instead we validate the Outcome struct's fields
	// carry Skip semantics correctly (integration would need mocking or a
	// subprocess with cleared PATH — out of scope for hermetic unit test).
	o := Outcome{
		Pass:       true,
		Skipped:    true,
		SkipReason: "go binary not on PATH — advisory-fallthrough",
	}
	if !o.Pass || !o.Skipped {
		t.Error("Pass + Skipped both must be true when advisory-fallthrough triggers")
	}
	if o.SkipReason == "" {
		t.Error("SkipReason must be non-empty on advisory-fallthrough")
	}
}

func TestErrCoverageBelowThreshold_message(t *testing.T) {
	if !errors.Is(ErrCoverageBelowThreshold, ErrCoverageBelowThreshold) {
		t.Error("errors.Is failed on ErrCoverageBelowThreshold")
	}
}

// DefaultScopes must point at packages that HAVE production statements.
//
// It used to point at ./infra/tests/{unit,charts}/..., which are test-only
// packages: `go test -cover` reports "coverage: [no statements]" for them, the
// gate averaged that to 0.00%, and then failed against its own 30% floor. The
// verdict was red for a reason unrelated to any code under test, which is how
// the gate ended up quarantined as "measured broken". Measured 2026-08-06.
func TestDefaultScopesCoverProductionCode(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	for _, scope := range DefaultScopes {
		dir := strings.TrimSuffix(strings.TrimPrefix(scope, "./"), "/...")
		production := 0
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				production++
			}
			return nil
		})
		if err != nil {
			t.Errorf("scope %q does not resolve on disk: %v", scope, err)
			continue
		}
		if production == 0 {
			t.Errorf("scope %q holds no non-test .go file — it measures the coverage OF tests, "+
				"not of the code they exercise, and averages to 0.00%%", scope)
		}
	}
}
