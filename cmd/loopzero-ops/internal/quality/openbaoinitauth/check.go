// Package openbaoinitauth enforces T5.1-T5.9 of plan
// 2026-06-02-openbao-adoption v1.3: per-cluster K8s auth mounts + per-binary
// roles in infra/helm/openbao-init.
//
// Renders the chart via `helm template` and asserts:
//
//   - T5.1-T5.4: 5 auth/k8s-* mounts enabled (k8s-mgmt, k8s-build,
//     k8s-cell-default, k8s-cell-1, k8s-cell-2).
//   - T5.5: loopzero-api role on k8s-mgmt with TTL 1h.
//   - T5.6: loopzero-tenant-operator role on k8s-mgmt.
//   - T5.7: loopzero-app/preview/platform-operator roles on k8s-mgmt.
//   - T5.8: argo-workflows role on k8s-build.
//   - T5.9: external-secrets-broker role on each k8s-cell-* mount.
//   - Isolation: loopzero-api NOT on any k8s-cell-* mount.
//   - Isolation: external-secrets-broker NOT on k8s-mgmt.
//
// Advisory-fallthrough when helm binary is absent.
//
// Migrated from infra/tests/check-openbao-init-auth-mounts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.7 Wave 2).
package openbaoinitauth

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

const chartPath = "infra/helm/openbao-init"

var (
	authMounts = []string{
		"k8s-mgmt", "k8s-build", "k8s-cell-default", "k8s-cell-1", "k8s-cell-2",
	}
	mgmtBinaries = []string{
		"loopzero-api",
		"loopzero-tenant-operator",
		"loopzero-app-operator",
		"loopzero-preview-operator",
		"loopzero-platform-operator",
	}
	cellMounts = []string{"k8s-cell-default", "k8s-cell-1", "k8s-cell-2"}
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Warn     bool
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("openbao-init auth-mounts contract violated")

// ErrChartMissing signals the chart is absent.
var ErrChartMissing = errors.New("openbao-init chart missing")

// Check runs the assertions.
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

	cmd := exec.Command("helm", "template", "openbao-init", chart)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template failed: %v\n%s", err, stderr.String())}}, ErrViolations
	}
	rendered := stdout.Bytes()

	if len(rendered) == 0 {
		return Outcome{Failures: []string{"helm template returned empty output"}}, ErrViolations
	}

	var failures []string

	// T5.1-T5.4: 5 auth mounts enabled.
	for _, mount := range authMounts {
		re := regexp.MustCompile(fmt.Sprintf(`enable_auth "%s" "kubernetes"`, regexp.QuoteMeta(mount)))
		if !re.Match(rendered) {
			failures = append(failures, fmt.Sprintf("auth/%s enable_auth MISSING", mount))
		}
	}

	// T5.5: loopzero-api role on k8s-mgmt with TTL 1h.
	loopzeroAPIRE := regexp.MustCompile(`write_auth_role "k8s-mgmt" "loopzero-api"[\s\S]{0,400}?"1h"`)
	if !loopzeroAPIRE.Match(rendered) {
		if !regexp.MustCompile(`write_auth_role "k8s-mgmt" "loopzero-api"`).Match(rendered) {
			failures = append(failures, "T5.5: loopzero-api role on k8s-mgmt MISSING")
		} else {
			failures = append(failures, "T5.5: loopzero-api TTL != 1h")
		}
	}

	// T5.6-T5.7: mgmt-side binaries.
	for _, binary := range mgmtBinaries {
		if binary == "loopzero-api" {
			continue // already asserted with TTL
		}
		re := regexp.MustCompile(fmt.Sprintf(`write_auth_role "k8s-mgmt" "%s"`, regexp.QuoteMeta(binary)))
		if !re.Match(rendered) {
			failures = append(failures, fmt.Sprintf("T5.6/T5.7: %s role on k8s-mgmt MISSING", binary))
		}
	}

	// T5.8: argo-workflows on k8s-build.
	if !regexp.MustCompile(`write_auth_role "k8s-build" "argo-workflows"`).Match(rendered) {
		failures = append(failures, "T5.8: argo-workflows role on k8s-build MISSING")
	}

	// T5.9: external-secrets-broker on each cell mount.
	for _, cell := range cellMounts {
		re := regexp.MustCompile(fmt.Sprintf(`write_auth_role "%s" "external-secrets-broker"`, regexp.QuoteMeta(cell)))
		if !re.Match(rendered) {
			failures = append(failures, fmt.Sprintf("T5.9: external-secrets-broker role on %s MISSING", cell))
		}
	}

	// Isolation: loopzero-api NOT on any cell mount.
	for _, cell := range cellMounts {
		re := regexp.MustCompile(fmt.Sprintf(`write_auth_role "%s" "loopzero-api"`, regexp.QuoteMeta(cell)))
		if re.Match(rendered) {
			failures = append(failures, fmt.Sprintf("Isolation: loopzero-api role LEAKED to %s (isolation broken)", cell))
		}
	}

	// Isolation: external-secrets-broker NOT on k8s-mgmt.
	if regexp.MustCompile(`write_auth_role "k8s-mgmt" "external-secrets-broker"`).Match(rendered) {
		failures = append(failures, "Isolation: external-secrets-broker role LEAKED to k8s-mgmt (isolation broken)")
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}
