// Hermetic tests for the lokivaultretention gate.
//
// Migrated from bash test infra/tests/check-loki-vault-retention_test.sh
// (M-Migrate2 2026-07-10 M16 T3.1 Wave 2).
package lokivaultretention

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writefile %s: %v", path, err)
	}
}

const canonicalLokiValues = `
retention_period: 168h
retention_stream:
  - selector: '{namespace="secrets-system"}'
    period: 2160h
`

const canonicalCronJob = `apiVersion: batch/v1
kind: CronJob
metadata:
  name: loki-vault-audit-archive
spec:
  schedule: "0 4 1 * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          securityContext:
            runAsNonRoot: true
          containers:
            - name: archive
              securityContext:
                capabilities:
                  drop: ["ALL"]
              env:
                - name: AWS_ACCESS_KEY_ID
                  valueFrom:
                    secretKeyRef:
                      name: loki-archive-s3-creds
                      key: access_key
                - name: AWS_SECRET_ACCESS_KEY
                  valueFrom:
                    secretKeyRef:
                      name: loki-archive-s3-creds
                      key: secret_key
              command:
                - sh
                - -c
                - |
                  logcli query 'namespace=secrets-system' | \
                    mc cp - loopzero-audit-logs-prod/audit/$(date +%Y%m).log
`

func TestCheck_HappyPath_PASS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, lokiValuesRel), canonicalLokiValues)
	writeFile(t, filepath.Join(root, archiveCronRel), canonicalCronJob)

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil err, got %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %v", outcome.Failures)
	}
}

func TestCheck_MissingLokiValues_FAIL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, archiveCronRel), canonicalCronJob)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing Loki values, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "loki values.yaml MISSING") {
		t.Fatalf("expected MISSING failure, got %v", outcome.Failures)
	}
}

func TestCheck_MissingCronJob_FAIL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, lokiValuesRel), canonicalLokiValues)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing CronJob, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "cronjob-loki-vault-audit-archive.yaml MISSING") {
		t.Fatalf("expected MISSING failure, got %v", outcome.Failures)
	}
}

func TestCheck_LokiValuesMissingRetentionStream_FAIL(t *testing.T) {
	root := t.TempDir()
	// values.yaml missing retention_stream declaration.
	brokenValues := `retention_period: 168h
namespace_selector: 'app=loki'
`
	writeFile(t, filepath.Join(root, lokiValuesRel), brokenValues)
	writeFile(t, filepath.Join(root, archiveCronRel), canonicalCronJob)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	// Expect ≥ 3 T8.2 failures (retention_stream + selector + period 2160h all missing).
	if len(outcome.Failures) < 3 {
		t.Fatalf("expected ≥ 3 T8.2 failures, got %v", outcome.Failures)
	}
}

func TestCheck_CronJobMissingHardening_FAIL(t *testing.T) {
	root := t.TempDir()
	// CronJob missing runAsNonRoot + capabilities drop hardening.
	brokenCron := `apiVersion: batch/v1
kind: CronJob
spec:
  schedule: "0 4 1 * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: archive
              env:
                - name: AWS_ACCESS_KEY_ID
                - name: AWS_SECRET_ACCESS_KEY
              command:
                - sh
                - -c
                - "logcli query namespace=secrets-system | mc cp - loopzero-audit-logs-prod/x"
              volumeMounts: []
      name: loki-archive-s3-creds
`
	writeFile(t, filepath.Join(root, lokiValuesRel), canonicalLokiValues)
	writeFile(t, filepath.Join(root, archiveCronRel), brokenCron)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "runAsNonRoot") {
		t.Fatalf("expected runAsNonRoot failure, got %v", outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "ROADMAP.md")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	if _, err := os.Stat(filepath.Join(root, "ROADMAP.md")); err != nil {
		t.Skipf("could not locate ROADMAP.md (skipping)")
	}
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("real repo violation: %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures against real repo, got %v", outcome.Failures)
	}
}

func hasSubstring(failures []string, substr string) bool {
	for _, f := range failures {
		for i := 0; i+len(substr) <= len(f); i++ {
			if f[i:i+len(substr)] == substr {
				return true
			}
		}
	}
	return false
}
