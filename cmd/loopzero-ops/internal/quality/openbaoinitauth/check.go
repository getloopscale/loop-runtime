// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package openbaoinitauth enforces the per-cluster Kubernetes auth mounts and
// per-binary roles of infra/helm/openbao-init (T5.1-T5.9 of the openbao
// adoption plan).
//
// The mounts are read from the chart's own values (`authMethods`), so the gate
// follows the cells that exist instead of a hand-kept list that drifts — the
// list this gate used to carry named `k8s-cell-0` twice after the cell merge,
// and asserted four distinct mounts while its doc claimed five. The chart is
// rendered with `helm template`, and the render must show:
//
//   - every declared mount enabled, and each declared once;
//   - k8s-core and k8s-build present, and at least one k8s-cell-* mount;
//   - loopzero-api on k8s-core with TTL 1h, and the operators on k8s-core;
//   - argo-workflows on k8s-build;
//   - external-secrets-broker on every cell mount, and never on k8s-core;
//   - loopzero-api on no cell mount;
//   - no policy that no role carries.
//
// A machine without helm gets an advisory Warn.
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
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

const chartPath = "infra/helm/openbao-init"

// requiredMounts must be declared and rendered in every installation.
var requiredMounts = []string{"k8s-core", "k8s-build"}

const cellMountPrefix = "k8s-cell-"

// roleAssertion is one role that must — or must not — be written on a mount.
type roleAssertion struct {
	mount, role string
	present     bool
	label       string
}

// coreRoles are the roles bound on the fixed mounts.
var coreRoles = []roleAssertion{
	{"k8s-core", "loopzero-tenant-operator", true, "T5.6"},
	{"k8s-core", "loopzero-app-operator", true, "T5.7"},
	{"k8s-core", "loopzero-preview-operator", true, "T5.7"},
	{"k8s-core", "loopzero-platform-operator", true, "T5.7"},
	{"k8s-build", "argo-workflows", true, "T5.8"},
	{"k8s-core", "external-secrets-broker", false, "isolation"},
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Warn     bool
	Failures []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation, Skip when helm is not on
// PATH (the gate is advisory then and rendered nothing), and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case o.Warn:
		return verdict.Skip
	}
	return verdict.Pass
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("openbao-init auth-mounts contract violated")

// ErrChartMissing signals the chart is absent.
var ErrChartMissing = errors.New("openbao-init chart missing")

var loopzeroAPITTLRE = regexp.MustCompile(`write_auth_role "k8s-core" "loopzero-api"[\s\S]{0,400}?"1h"`)

// Check runs the assertions.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	chart := filepath.Join(root, chartPath)
	if _, err := os.Stat(chart); errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, ErrChartMissing
	}
	mounts, err := declaredMounts(filepath.Join(chart, "values.yaml"))
	if err != nil {
		return Outcome{}, err
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return Outcome{Warn: true}, nil
	}

	cmd := exec.Command("helm", "template", "openbao-init", chart)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template failed: %v\n%s", err, stderr.String())}, Violation: ErrViolations}, nil
	}
	if stdout.Len() == 0 {
		return Outcome{Failures: []string{"helm template returned empty output"}, Violation: ErrViolations}, nil
	}

	failures := checkRendered(stdout.String(), mounts)
	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// declaredMounts reads the k8s-* auth mount paths from the chart's authMethods,
// in declaration order, duplicates included (so they can be reported).
func declaredMounts(valuesPath string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(valuesPath))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", valuesPath, err)
	}
	var doc struct {
		AuthMethods []struct {
			Path string `yaml:"path"`
		} `yaml:"authMethods"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", valuesPath, err)
	}
	var mounts []string
	for _, m := range doc.AuthMethods {
		if strings.HasPrefix(m.Path, "k8s-") {
			mounts = append(mounts, m.Path)
		}
	}
	return mounts, nil
}

// checkRendered applies every assertion to the rendered chart.
func checkRendered(rendered string, mounts []string) []string {
	var failures []string
	count := map[string]int{}
	var cells []string
	for _, m := range mounts {
		count[m]++
		if count[m] == 1 && strings.HasPrefix(m, cellMountPrefix) {
			cells = append(cells, m)
		}
	}
	for _, m := range sortedKeys(count) {
		if count[m] > 1 {
			failures = append(failures, fmt.Sprintf("auth mount %s is declared %d times in authMethods — one cluster, one mount", m, count[m]))
		}
	}
	for _, m := range requiredMounts {
		if count[m] == 0 {
			failures = append(failures, fmt.Sprintf("auth mount %s is not declared in authMethods", m))
		}
	}
	if len(cells) == 0 {
		failures = append(failures, "no k8s-cell-* auth mount is declared — no workload cell can authenticate")
	}
	for _, m := range sortedKeys(count) {
		if !strings.Contains(rendered, fmt.Sprintf(`enable_auth "%s" "kubernetes"`, m)) {
			failures = append(failures, fmt.Sprintf("auth/%s enable_auth MISSING from the render", m))
		}
	}

	if !loopzeroAPITTLRE.MatchString(rendered) {
		if hasRole(rendered, "k8s-core", "loopzero-api") {
			failures = append(failures, "T5.5: loopzero-api TTL != 1h")
		} else {
			failures = append(failures, "T5.5: loopzero-api role on k8s-core MISSING")
		}
	}

	assertions := append([]roleAssertion{}, coreRoles...)
	for _, cell := range cells {
		assertions = append(assertions,
			roleAssertion{cell, "external-secrets-broker", true, "T5.9"},
			roleAssertion{cell, "loopzero-api", false, "isolation"})
	}
	for _, a := range assertions {
		switch got := hasRole(rendered, a.mount, a.role); {
		case a.present && !got:
			failures = append(failures, fmt.Sprintf("%s: %s role on %s MISSING", a.label, a.role, a.mount))
		case !a.present && got:
			failures = append(failures, fmt.Sprintf("%s: %s role LEAKED to %s", a.label, a.role, a.mount))
		}
	}

	return append(failures, orphanPolicyFailures(rendered)...)
}

func hasRole(rendered, mount, role string) bool {
	return strings.Contains(rendered, fmt.Sprintf(`write_auth_role "%s" "%s"`, mount, role))
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
