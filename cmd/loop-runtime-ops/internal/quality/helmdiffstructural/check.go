// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package helmdiffstructural fails CI only on STRUCTURALLY RISKY
// Helm-chart diffs, not on benign template changes. The naïve helm-diff
// gate fails every PR (any chart edit produces a diff — that IS the
// point of a PR). This gate parses the diff and exits non-zero ONLY
// when the change carries operator-explicit-approval risk:
//
//	STRUCT-1  Removal of a security-relevant resource Kind
//	          (NetworkPolicy, RoleBinding, ClusterRoleBinding,
//	           SecurityContextConstraints, PodSecurityPolicy)
//	STRUCT-2  Change of `namespace:` on any resource
//	STRUCT-3  Security-context downgrade:
//	            runAsNonRoot:  true  → false (or removed)
//	            readOnlyRootFilesystem: true → false (or removed)
//	            allowPrivilegeEscalation: false → true
//	            privileged: false → true (or added as true)
//
// Replaces infra/scripts/helm-diff-structural.sh (~196 LOC) per Phase 7
// of plan 2026-05-21-bash-to-go-migration-plan.
//
// Origin: audit gap M06 #21 / active-execution-plan §6.E.6b.
package helmdiffstructural

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"gopkg.in/yaml.v3"
)

// SensitiveKinds — resource kinds whose removal triggers STRUCT-1.
var SensitiveKinds = []string{
	"NetworkPolicy",
	"RoleBinding",
	"ClusterRoleBinding",
	"SecurityContextConstraints",
	"PodSecurityPolicy",
}

// kindLineRe matches `kind: Foo` at line start (avoiding comments).
func kindLineRe(kind string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^kind:[[:space:]]+` + regexp.QuoteMeta(kind) + `[[:space:]]*$`)
}

// Tuple is the (kind, name, namespace) tuple extracted from a rendered
// manifest. Used for STRUCT-2 detection (namespace moves).
type Tuple struct {
	Kind      string
	Name      string
	Namespace string
}

// Violation captures one STRUCT-* finding.
type Violation struct {
	Code    string // STRUCT-1 / STRUCT-2 / STRUCT-3a / STRUCT-3b / STRUCT-3c / STRUCT-3d
	Message string
}

// Config holds gate inputs.
type Config struct {
	// Chart is the chart name used in diagnostic messages.
	Chart string

	// BasePath / PRPath are paths to the two rendered YAML files.
	BasePath string
	PRPath   string
}

// Outcome captures the gate result.
type Outcome struct {
	Pass       bool
	Identical  bool
	Violations []Violation
	Messages   []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// extractTuples parses one rendered manifest file into a sorted slice of
// (Kind, Name, Namespace) tuples using a real multi-document YAML decoder —
// robust to quoting/indentation the former hardcoded prefix-offset parser
// could misread (Rule 9: don't reinvent a YAML parser).
func extractTuples(path string) ([]Tuple, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled
	if err != nil {
		return nil, err
	}
	var tuples []Tuple
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"metadata"`
		}
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if doc.Kind != "" && doc.Metadata.Name != "" {
			tuples = append(tuples, Tuple{Kind: doc.Kind, Name: doc.Metadata.Name, Namespace: doc.Metadata.Namespace})
		}
	}
	sort.Slice(tuples, func(i, j int) bool {
		if tuples[i].Kind != tuples[j].Kind {
			return tuples[i].Kind < tuples[j].Kind
		}
		if tuples[i].Name != tuples[j].Name {
			return tuples[i].Name < tuples[j].Name
		}
		return tuples[i].Namespace < tuples[j].Namespace
	})
	return tuples, nil
}

// countKind counts documents whose `kind: <kind>` appears at line start.
func countKind(content, kind string) int {
	re := kindLineRe(kind)
	return len(re.FindAllString(content, -1))
}

// findNamespaceMoves cross-references base vs PR tuples for namespace
// changes. Same (Kind, Name) with different Namespace → STRUCT-2.
func findNamespaceMoves(base, pr []Tuple) []Violation {
	prByKey := make(map[string]string, len(pr))
	for _, t := range pr {
		key := t.Kind + "|" + t.Name
		if _, exists := prByKey[key]; !exists {
			prByKey[key] = t.Namespace
		}
	}
	var out []Violation
	seen := make(map[string]bool)
	for _, t := range base {
		key := t.Kind + "|" + t.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		if prNS, ok := prByKey[key]; ok && prNS != t.Namespace {
			out = append(out, Violation{
				Code: "STRUCT-2",
				Message: fmt.Sprintf("%s/%s namespace changed from '%s' to '%s'. Namespace moves bypass RBAC and require explicit operator approval.",
					t.Kind, t.Name, t.Namespace, prNS),
			})
		}
	}
	return out
}

// diffLineRe matches a unified-diff added or removed line whose body
// matches the given pattern (sans leading +/-).
func diffLineRe(prefix, pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(prefix) + `[[:space:]]+` + pattern + `[[:space:]]*$`)
}

// findSecurityDowngrades parses a unified-diff string for STRUCT-3 patterns.
func findSecurityDowngrades(diff string) []Violation {
	var out []Violation
	rmRunAsNonRoot := diffLineRe("-", `runAsNonRoot:[[:space:]]+true`)
	addRunAsNonRoot := diffLineRe(`+`, `runAsNonRoot:[[:space:]]+true`)
	if rmRunAsNonRoot.MatchString(diff) && !addRunAsNonRoot.MatchString(diff) {
		out = append(out, Violation{
			Code:    "STRUCT-3a",
			Message: "runAsNonRoot:true was removed or set to false. Security downgrade requires explicit operator approval.",
		})
	}
	rmRORoot := diffLineRe("-", `readOnlyRootFilesystem:[[:space:]]+true`)
	addRORoot := diffLineRe(`+`, `readOnlyRootFilesystem:[[:space:]]+true`)
	if rmRORoot.MatchString(diff) && !addRORoot.MatchString(diff) {
		out = append(out, Violation{
			Code:    "STRUCT-3b",
			Message: "readOnlyRootFilesystem:true was removed or set to false. Security downgrade requires explicit operator approval.",
		})
	}
	addPrivEsc := diffLineRe(`+`, `allowPrivilegeEscalation:[[:space:]]+true`)
	if addPrivEsc.MatchString(diff) {
		out = append(out, Violation{
			Code:    "STRUCT-3c",
			Message: "allowPrivilegeEscalation:true was added or flipped from false. Security downgrade requires explicit operator approval.",
		})
	}
	addPrivileged := diffLineRe(`+`, `privileged:[[:space:]]+true`)
	if addPrivileged.MatchString(diff) {
		out = append(out, Violation{
			Code:    "STRUCT-3d",
			Message: "privileged:true was added or flipped from false. Security downgrade requires explicit operator approval.",
		})
	}
	return out
}

// computeDiff produces a minimal unified-diff between two files. The
// output is the substring that the STRUCT-3 detectors scan. We rely on
// the `diff -u` binary (exec.Command in diff.go) rather than reimplementing
// Myers' algorithm — Go's stdlib has no built-in diff.
//
// For Phase 7 we keep parity with bash by shelling out to `diff -u`.
// Future iteration may swap for a pure-Go diff library.
//
// It is a variable so tests can make the diff fail; Check must then refuse
// to report on a STRUCT-3 class it never evaluated.
var computeDiff = runDiff

// Check executes the gate.
func Check(cfg Config) (*Outcome, error) {
	if cfg.Chart == "" {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: "helmdiffstructural: Chart name required",
		}
	}
	if cfg.BasePath == "" || cfg.PRPath == "" {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: "helmdiffstructural: BasePath and PRPath required",
		}
	}
	baseRaw, err := os.ReadFile(cfg.BasePath) //nolint:gosec // operator-controlled
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("helmdiffstructural: base-rendered-yaml not found: %s", cfg.BasePath),
			Cause:   err,
		}
	}
	prRaw, err := os.ReadFile(cfg.PRPath) //nolint:gosec // operator-controlled
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("helmdiffstructural: pr-rendered-yaml not found: %s", cfg.PRPath),
			Cause:   err,
		}
	}

	out := &Outcome{}
	if string(baseRaw) == string(prRaw) {
		out.Pass = true
		out.Identical = true
		out.Messages = []string{fmt.Sprintf("[%s] no diff — safe.", cfg.Chart)}
		return out, nil
	}

	// STRUCT-1: sensitive kind removal.
	for _, kind := range SensitiveKinds {
		baseCount := countKind(string(baseRaw), kind)
		prCount := countKind(string(prRaw), kind)
		if baseCount > 0 && prCount < baseCount {
			out.Violations = append(out.Violations, Violation{
				Code: "STRUCT-1",
				Message: fmt.Sprintf("%s count dropped from %d to %d. Removing a security-relevant resource requires explicit operator approval.",
					kind, baseCount, prCount),
			})
		}
	}

	// STRUCT-2: namespace moves.
	baseTuples, err := extractTuples(cfg.BasePath)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("helmdiffstructural: extract base tuples: %v", err),
			Cause:   err,
		}
	}
	prTuples, err := extractTuples(cfg.PRPath)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("helmdiffstructural: extract PR tuples: %v", err),
			Cause:   err,
		}
	}
	out.Violations = append(out.Violations, findNamespaceMoves(baseTuples, prTuples)...)

	// STRUCT-3: security-context downgrades.
	diff, err := computeDiff(cfg.BasePath, cfg.PRPath)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("helmdiffstructural: cannot compute the unified diff, so STRUCT-3 (security-context downgrades) was not evaluated: %v", err),
			Cause:   err,
		}
	}
	out.Violations = append(out.Violations, findSecurityDowngrades(diff)...)

	for _, v := range out.Violations {
		out.Messages = append(out.Messages,
			fmt.Sprintf("::error::[%s] %s: %s", cfg.Chart, v.Code, v.Message))
	}

	if len(out.Violations) == 0 {
		out.Pass = true
		out.Messages = append(out.Messages,
			fmt.Sprintf("[%s] diff present but NO structural risks detected — informational only.", cfg.Chart))
		return out, nil
	}
	out.Pass = false
	out.Messages = append(out.Messages,
		"",
		fmt.Sprintf("[%s] FOUND %d structural risk(s). CI blocks per audit M06 #21 / §6.E.6b.",
			cfg.Chart, len(out.Violations)))
	return out, nil
}
