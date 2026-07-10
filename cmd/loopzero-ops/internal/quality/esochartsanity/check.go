// Package esochartsanity enforces the iter 23 chart-sanity contract for
// infra/helm/loopzero-external-secrets (plan 2026-06-02-loopzero-api-vault-
// integration v1.8).
//
// Renders the chart across 5 canonical envs (vault-local + standalone-multi
// + dev + drill + prod) and asserts 8 invariants that span the entire
// rendered output:
//
//  1. renders ExternalSecret CRs matching inventory count
//     (`^  - targetSecret:` entries in values.yaml)
//  2. all metadata.name entries are unique (no duplicate target Secret names)
//  3. all CRs have refreshInterval set
//  4. all CRs reference the loopzero-vault-secrets ClusterSecretStore
//  5. chart renders successfully for standalone-multi + dev + drill + prod
//  6. vaultBinaryOverride: loopzero-tenant-operator CR data block has ≥ 3
//     api/secrets/* path entries (GITEA_TOKEN + MINIO_ACCESS_KEY + MINIO_SECRET_KEY)
//  7. envName empty fail-fast: rendering without envName → helm error
//  8. no unwanted `operators/secrets/gitea/token` path leak
//
// Advisory-fallthrough when helm binary is absent (esooverlay precedent).
//
// Migrated from infra/tests/check-loopzero-external-secrets-chart-sanity_test.sh
// (M-Migrate2 2026-07-10 M16 T3.5 Wave 2). Shape 2 (helm-template).
package esochartsanity

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	chartPath     = "infra/helm/loopzero-external-secrets"
	valuesRelPath = "values.yaml"
)

var canonicalEnvs = []string{"standalone-multi", "dev", "drill", "prod"}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Warn     bool // helm not on PATH
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("loopzero-external-secrets chart-sanity contract violated")

// ErrChartMissing signals the chart directory is absent.
var ErrChartMissing = errors.New("loopzero-external-secrets chart missing")

// Check runs the 8 assertions.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	chart := filepath.Join(root, chartPath)
	if _, err := os.Stat(chart); errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, ErrChartMissing
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return Outcome{Warn: true}, nil
	}

	// Inventory expected count from values.yaml `^  - targetSecret:` entries.
	valuesBytes, err := os.ReadFile(filepath.Join(chart, valuesRelPath))
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("read values.yaml: %v", err)}}, ErrViolations
	}
	expected := countMatches(valuesBytes, `(?m)^\s*-\s+targetSecret:`)
	if expected == 0 {
		return Outcome{Failures: []string{"inventory 0 targetSecret entries in values.yaml"}}, ErrViolations
	}

	renderVL, err := helmTemplate(chart, "vault-local")
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("render vault-local: %v", err)}}, ErrViolations
	}

	var failures []string

	// (1) ExternalSecret count matches inventory.
	esCount := countMatches(renderVL, `(?m)^kind: ExternalSecret`)
	if esCount != expected {
		failures = append(failures, fmt.Sprintf("ExternalSecret count %d != inventory %d", esCount, expected))
	}

	// (2) All metadata.name unique.
	names := regexp.MustCompile(`(?m)^  name:\s+(\S+)`).FindAllSubmatch(renderVL, -1)
	unique := map[string]struct{}{}
	for _, m := range names {
		unique[string(m[1])] = struct{}{}
	}
	if len(names) < expected || len(unique) != len(names) {
		failures = append(failures, fmt.Sprintf("target-name uniqueness: total=%d unique=%d", len(names), len(unique)))
	}

	// (3) All CRs have refreshInterval set.
	refresh := countMatches(renderVL, `(?m)^  refreshInterval:`)
	if refresh != expected {
		failures = append(failures, fmt.Sprintf("refreshInterval count %d != %d", refresh, expected))
	}

	// (4) All CRs reference loopzero-vault-secrets ClusterSecretStore.
	css := countMatches(renderVL, `name: loopzero-vault-secrets`)
	if css != expected {
		failures = append(failures, fmt.Sprintf("loopzero-vault-secrets ref count %d != %d", css, expected))
	}

	// (5) Chart renders for all canonical envs.
	for _, env := range canonicalEnvs {
		if _, err := helmTemplate(chart, env); err != nil {
			failures = append(failures, fmt.Sprintf("chart did NOT render for env=%s: %v", env, err))
		}
	}

	// (6) vaultBinaryOverride: ≥ 3 api/secrets/* entries under loopzero-tenant-operator.
	if got := countAPIOverridesInTenantOperator(renderVL, "vault-local"); got < 3 {
		failures = append(failures, fmt.Sprintf("vaultBinaryOverride api resolutions=%d expected ≥ 3", got))
	}

	// (7) envName empty fail-fast.
	if _, err := helmTemplate(chart, ""); err == nil {
		failures = append(failures, "rendered WITHOUT envName — expected helm error (fail-fast)")
	}

	// (8) No operators/secrets/gitea/token leak.
	leak := countMatches(renderVL, `key: loopzero/vault-local/operators/secrets/gitea/token`)
	if leak > 0 {
		failures = append(failures, fmt.Sprintf("operators/secrets/gitea/token path leak count=%d (vaultBinaryOverride not honored)", leak))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// helmTemplate runs `helm template loopzero-external-secrets <chart> [--set envName=X]`.
func helmTemplate(chartDir, envName string) ([]byte, error) {
	args := []string{"template", "loopzero-external-secrets", chartDir}
	if envName != "" {
		args = append(args, "--set", "envName="+envName)
	}
	cmd := exec.Command("helm", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %v\n%s", args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func countMatches(content []byte, pattern string) int {
	return len(regexp.MustCompile(pattern).FindAll(content, -1))
}

// countAPIOverridesInTenantOperator walks the CR containing name:
// loopzero-tenant-operator and counts `key: loopzero/<env>/api/secrets/*` lines
// until the next `^---` document separator.
func countAPIOverridesInTenantOperator(content []byte, env string) int {
	lines := strings.Split(string(content), "\n")
	inTO := false
	prefix := fmt.Sprintf("    key: loopzero/%s/api/secrets/", env)
	count := 0
	for _, line := range lines {
		if strings.Contains(line, "name: loopzero-tenant-operator") {
			inTO = true
			continue
		}
		if inTO && strings.HasPrefix(line, "---") {
			inTO = false
		}
		if inTO && strings.HasPrefix(strings.TrimLeft(line, " "), "key: loopzero/"+env+"/api/secrets/") ||
			(inTO && strings.HasPrefix(line, prefix)) {
			count++
		}
	}
	return count
}
