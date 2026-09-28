// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package securitycontext implements the INF-G08 gate: audit Helm bootstrap
// charts (infra/helm/*) for securityContext compliance — runAsNonRoot,
// readOnlyRootFilesystem, drop:[ALL], allowPrivilegeEscalation:false.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces scripts/audit-security-context.sh. Pure-Go (filepath walk + regexp) —
// replaces bash grep/find. No external CLI. (The "helm" in the chart dir path
// is a directory name, not a helm invocation.)
//
// A chart that is NON-CONFORMANT but legitimately needs privilege documents an
// ADR-of-exception at infra/helm/<chart>/ADR-securitycontext-exception.md.
package securitycontext

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultHelmDir holds bootstrap charts, relative to repo root.
const DefaultHelmDir = "infra/helm"

const adrExceptionFile = "ADR-securitycontext-exception.md"

var (
	runAsNonRootRe = regexp.MustCompile(`runAsNonRoot:[[:space:]]*true`)
	readOnlyFSRe   = regexp.MustCompile(`readOnlyRootFilesystem:[[:space:]]*true`)
	dropAllRe      = regexp.MustCompile(`drop:.*ALL|- ALL|-[[:space:]]*ALL`)
	noPrivEscRe    = regexp.MustCompile(`allowPrivilegeEscalation:[[:space:]]*false`)

	podKindRe        = regexp.MustCompile(`(?m)^kind:[[:space:]]*(Deployment|StatefulSet|DaemonSet|Job|CronJob|Pod)[[:space:]]*$`)
	chartDepsRe      = regexp.MustCompile(`(?m)^dependencies:`)
	valuesPodFieldRe = regexp.MustCompile(`(?m)^(image|replicas|resources|securityContext|persistence|podSecurityContext|containerSecurityContext|deployment):`)
)

// ChartResult is the per-chart audit row.
type ChartResult struct {
	Name           string
	PodBearing     bool
	RunAsNonRoot   bool
	ReadOnlyRootFS bool
	DropAll        bool
	NoPrivEsc      bool
	Status         string // "CONFORMANT" | "PARTIAL" | "NON-CONFORMANT" | "N/A"
	HasADR         bool
}

// Config drives the gate.
type Config struct {
	Root    string
	HelmDir string // default DefaultHelmDir (relative to Root)
	// Gate enforces the audit: a NON-CONFORMANT chart without an ADR exception
	// fails the run. Without it the run is a report and never fails.
	Gate bool
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool // true if no NON-CONFORMANT chart lacks an ADR exception
	Charts     []ChartResult
	Violations []string // chart names NON-CONFORMANT without ADR
	// Gated records that the run enforced the audit (Config.Gate).
	Gated bool
}

// Verdict is Fail when an enforced audit found a NON-CONFORMANT chart without
// an ADR exception, and Pass otherwise: a report run never fails.
func (o Outcome) Verdict() verdict.Verdict {
	if !o.Gated {
		return verdict.Pass
	}
	return verdict.FromPass(o.Pass)
}

// Check audits all charts under cfg.HelmDir.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	helmDir := cfg.HelmDir
	if helmDir == "" {
		helmDir = DefaultHelmDir
	}
	base := filepath.Join(root, helmDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var chartNames []string
	for _, e := range entries {
		if e.IsDir() {
			chartNames = append(chartNames, e.Name())
		}
	}
	sort.Strings(chartNames)

	out := &Outcome{Pass: true}
	for _, name := range chartNames {
		dir := filepath.Join(base, name)
		cr := ChartResult{Name: name}

		bearing, err := podBearing(dir)
		if err != nil {
			return nil, err
		}
		if !bearing {
			cr.Status = "N/A"
			out.Charts = append(out.Charts, cr)
			continue
		}
		cr.PodBearing = true
		for _, f := range []struct {
			field *bool
			re    *regexp.Regexp
		}{
			{&cr.RunAsNonRoot, runAsNonRootRe},
			{&cr.ReadOnlyRootFS, readOnlyFSRe},
			{&cr.DropAll, dropAllRe},
			{&cr.NoPrivEsc, noPrivEscRe},
		} {
			if *f.field, err = dirMatches(dir, f.re); err != nil {
				return nil, err
			}
		}
		cr.Status = classify(cr)
		cr.HasADR = fileExists(filepath.Join(dir, adrExceptionFile))

		if cr.Status == "NON-CONFORMANT" && !cr.HasADR {
			out.Violations = append(out.Violations, name)
		}
		out.Charts = append(out.Charts, cr)
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out, nil
}

func classify(cr ChartResult) string {
	yes := 0
	for _, v := range []bool{cr.RunAsNonRoot, cr.ReadOnlyRootFS, cr.DropAll, cr.NoPrivEsc} {
		if v {
			yes++
		}
	}
	switch yes {
	case 4:
		return "CONFORMANT"
	case 2, 3:
		return "PARTIAL"
	default:
		return "NON-CONFORMANT"
	}
}

// podBearing reports whether the chart deploys pod-bearing resources.
func podBearing(dir string) (bool, error) {
	matched, err := dirMatches(filepath.Join(dir, "templates"), podKindRe)
	if err != nil || matched {
		return matched, err
	}
	for _, f := range []struct {
		name string
		re   *regexp.Regexp
	}{{"Chart.yaml", chartDepsRe}, {"values.yaml", valuesPodFieldRe}} {
		data, err := os.ReadFile(filepath.Join(dir, f.name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("reading %s: %w", filepath.Join(dir, f.name), err)
		}
		if f.re.Match(data) {
			return true, nil
		}
	}
	return false, nil
}

// dirMatches reports whether any file under dir (recursive) matches re. A dir
// that does not exist matches nothing; any other walk or read error is returned,
// because a file the gate did not read may be the one that matches.
func dirMatches(dir string, re *regexp.Regexp) (bool, error) {
	matched := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == dir && os.IsNotExist(err) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		if re.Match(data) {
			matched = true
			return fs.SkipAll
		}
		return nil
	})
	return matched, err
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
