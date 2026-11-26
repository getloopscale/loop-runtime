// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package orphanalerts implements the T5.10 gate: reject PrometheusRule alerts
// staged with an `expr: vector(0) > 1` placeholder (an alert whose underlying
// metric is not emitted — it can never fire, creating false confidence).
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check_orphan_alerts.sh. Pure-Go (file read + regexp) —
// replaces bash grep/sed. No external CLI. (The "helm" in the default rules dir
// path is a directory name, not a helm invocation.)
package orphanalerts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// DefaultRulesDir holds the PrometheusRule manifests, relative to repo root.
const DefaultRulesDir = "infra/helm/monitoring/manifests"

var (
	orphanExprRe = regexp.MustCompile(`expr:\s*vector\(0\)\s*>\s*1`)
	alertNameRe  = regexp.MustCompile(`alert:\s+([A-Za-z0-9_]+)`)
)

// Config drives the gate.
type Config struct {
	Root          string
	RulesDir      string   // default DefaultRulesDir (relative to Root)
	ExceptionList []string // alert names that are known/approved orphans
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Violations []string // "<file>:<line> — alert <name> ..."
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// ErrNothingScanned means the rules directory held no rule file: a scan over
// nothing cannot report the absence of orphans.
var ErrNothingScanned = errors.New("no PrometheusRule YAML found in the rules directory — the gate read nothing")

// Check scans PrometheusRule YAMLs for unallowlisted orphan-alert placeholders.
// A rules directory that cannot be read, a rule file that cannot be read, or a
// directory with no rule file is an error: none of them is a clean tree.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	rulesDir := cfg.RulesDir
	if rulesDir == "" {
		rulesDir = DefaultRulesDir
	}
	excepted := make(map[string]struct{}, len(cfg.ExceptionList))
	for _, e := range cfg.ExceptionList {
		excepted[e] = struct{}{}
	}

	out := &Outcome{Pass: true}
	dir := filepath.Join(root, rulesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading rules dir %s: %w", rulesDir, err)
	}
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, fmt.Errorf("reading rule file: %w", rerr)
		}
		scanned++
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !orphanExprRe.MatchString(line) {
				continue
			}
			name := alertNameAbove(lines, i)
			rel := filepath.ToSlash(filepath.Join(rulesDir, e.Name()))
			if name == "" {
				out.Violations = append(out.Violations,
					rel+":"+strconv.Itoa(i+1)+" — could not determine alert name for vector(0) > 1")
				continue
			}
			if _, ok := excepted[name]; !ok {
				out.Violations = append(out.Violations,
					rel+":"+strconv.Itoa(i+1)+" — alert "+name+" uses vector(0) > 1 (metric not emitted) without EXCEPTION_LIST entry")
			}
		}
	}
	if scanned == 0 {
		return nil, fmt.Errorf("%s: %w", rulesDir, ErrNothingScanned)
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out, nil
}

// alertNameAbove returns the nearest `alert: NAME` within the 5 lines up to and
// including line index i (mirrors the bash sed window start=lineno-5).
func alertNameAbove(lines []string, i int) string {
	start := i - 5
	if start < 0 {
		start = 0
	}
	name := ""
	for j := start; j <= i; j++ {
		if m := alertNameRe.FindStringSubmatch(lines[j]); m != nil {
			name = m[1] // last match in the window wins (matches `tail -1`)
		}
	}
	return name
}
