// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package crdsemanticdiff performs semantic-level diffing between
// operator-source CRDs (`operators/config/crd/bases/runtime.getloopscale.com_*.yaml`,
// kubebuilder-generated) and Helm chart templates
// (`charts/loopruntime-crds/templates/*.yaml`, post-render). Detects `.spec`
// drift (group, scope, versions[].schema, etc.) that the existing
// nominal crd-sync check misses.
//
// Replaces infra/scripts/crd-semantic-diff.sh (~114 LOC, yq + helm
// shell-out) per Phase 7 of plan
// 2026-05-21-bash-to-go-migration-plan.
//
// Origin: charts-sota-implementation-plan T1.4 / gate CH-G06.
package crdsemanticdiff

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/helmrender"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/scanresult"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"gopkg.in/yaml.v3"
)

// DefaultSourceDir — kubebuilder-generated CRDs.
const DefaultSourceDir = "operators/config/crd/bases"

// DefaultChartPath — Helm chart directory to render.
const DefaultChartPath = "charts/loopruntime-crds"

// SourcePrefix — bash basename prefix (runtime.getloopscale.com_<short>.yaml).
const SourcePrefix = "runtime.getloopscale.com_"

// Drift captures one CRD's diff result.
type Drift struct {
	ShortName  string // e.g., "tenants"
	CRDName    string // e.g., "tenants.runtime.getloopscale.com"
	Reason     string // human-friendly classification
	SourceSpec string // normalized source `.spec`
	ChartSpec  string // normalized chart `.spec`
}

// Config holds gate inputs.
type Config struct {
	// Root is the repo root. Default: ".".
	Root string

	// SourceDir overrides DefaultSourceDir.
	SourceDir string

	// ChartPath overrides DefaultChartPath.
	ChartPath string

	// CRDFilter limits the audit to a single short-name (e.g. "tenants").
	CRDFilter string

	// HelmRenderOverride bypasses `helm template` for testing: when
	// non-empty, the value is used as the rendered chart YAML.
	HelmRenderOverride string

	// Verbose appends an inline diff snippet to each drift entry.
	Verbose bool
}

// Outcome captures gate result.
type Outcome struct {
	Pass     bool
	Scanned  int
	Drifts   []Drift
	Messages []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// rawCRD is the minimal shape of a CustomResourceDefinition YAML doc.
type rawCRD struct {
	Kind     string         `yaml:"kind"`
	Metadata rawCRDMeta     `yaml:"metadata"`
	Spec     map[string]any `yaml:"spec"`
}

type rawCRDMeta struct {
	Name        string         `yaml:"name"`
	Annotations map[string]any `yaml:"annotations,omitempty"`
}

// parseSourceCRD loads ONE `runtime.getloopscale.com_<X>.yaml` source file.
func parseSourceCRD(path string) (*rawCRD, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled
	if err != nil {
		return nil, err
	}
	var crd rawCRD
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return nil, fmt.Errorf("yaml unmarshal %s: %w", path, err)
	}
	return &crd, nil
}

// parseHelmRender extracts ALL CustomResourceDefinition documents from
// a multi-document YAML stream (output of `helm template`).
func parseHelmRender(raw string) (map[string]*rawCRD, error) {
	out := make(map[string]*rawCRD)
	dec := yaml.NewDecoder(strings.NewReader(raw))
	for {
		var crd rawCRD
		if err := dec.Decode(&crd); err != nil {
			// Only io.EOF ends the stream. Matching "EOF" in the message would
			// also swallow "unexpected EOF", a document cut in half.
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, fmt.Errorf("yaml decode: %w", err)
		}
		if crd.Kind != "CustomResourceDefinition" {
			continue
		}
		if crd.Metadata.Name == "" {
			continue
		}
		out[crd.Metadata.Name] = &crd
	}
}

// normalizeSpec strips controller-gen build-tool noise that doesn't
// affect API semantic. Mirrors bash `yq eval 'del(.. | select(has(
// "controller-gen.kubebuilder.io/version")))'`.
func normalizeSpec(spec map[string]any) string {
	walkAndStrip(spec)
	out, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Sprintf("MARSHAL_ERROR: %v", err)
	}
	return string(out)
}

// walkAndStrip recursively removes any map keyed by
// `controller-gen.kubebuilder.io/version`.
func walkAndStrip(node any) {
	switch v := node.(type) {
	case map[string]any:
		// If this map ITSELF has the noise key, clear all keys (bash's
		// `select(has(...))` matches the map; `del(...)` removes IT).
		if _, ok := v["controller-gen.kubebuilder.io/version"]; ok {
			for k := range v {
				delete(v, k)
			}
			return
		}
		for _, child := range v {
			walkAndStrip(child)
		}
	case []any:
		for _, child := range v {
			walkAndStrip(child)
		}
	}
}

// renderChart renders the chart through quality/helmrender. When the override
// is set, returns it verbatim (test hook — keeps unit tests hermetic without helm).
func renderChart(chartPath, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return helmrender.Helm{}.Render(chartPath, nil)
}

// listSourceFiles returns sorted `runtime.getloopscale.com_*.yaml` files in dir.
func listSourceFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasPrefix(n, SourcePrefix) {
			continue
		}
		if !strings.HasSuffix(n, ".yaml") {
			continue
		}
		files = append(files, filepath.Join(dir, n))
	}
	sort.Strings(files)
	return files, nil
}

// shortNameFromPath turns "operators/.../runtime.getloopscale.com_tenants.yaml" → "tenants".
func shortNameFromPath(p string) string {
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, ".yaml")
	return strings.TrimPrefix(base, SourcePrefix)
}

// resolvePath joins p under root unless absolute.
func resolvePath(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, p)
}

// Check executes the gate.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	sourceDir := cfg.SourceDir
	if sourceDir == "" {
		sourceDir = DefaultSourceDir
	}
	chartPath := cfg.ChartPath
	if chartPath == "" {
		chartPath = DefaultChartPath
	}
	resolvedSource := resolvePath(root, sourceDir)
	resolvedChart := resolvePath(root, chartPath)

	out := &Outcome{}

	sourceFiles, err := listSourceFiles(resolvedSource)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: CRD source dir %s not found", scanresult.ErrNothingScanned, resolvedSource)
		}
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("crdsemanticdiff: read source: %v", err),
			Cause:   err,
		}
	}

	chartYAML, err := renderChart(resolvedChart, cfg.HelmRenderOverride)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("crdsemanticdiff: helm template: %v", err),
			Cause:   err,
		}
	}
	chartCRDs, err := parseHelmRender(chartYAML)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("crdsemanticdiff: parse chart yaml: %v", err),
			Cause:   err,
		}
	}

	out.Messages = append(out.Messages,
		"CRD semantic diff (operators/config/crd/bases ↔ charts/loopruntime-crds/templates)",
		"===========================================================================")

	for _, srcPath := range sourceFiles {
		short := shortNameFromPath(srcPath)
		if cfg.CRDFilter != "" && short != cfg.CRDFilter {
			continue
		}
		out.Scanned++

		srcCRD, err := parseSourceCRD(srcPath)
		if err != nil {
			return nil, &loopruntimeerrors.LoopRuntimeError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("crdsemanticdiff: parse %s: %v", srcPath, err),
				Cause:   err,
			}
		}
		crdName := srcCRD.Metadata.Name
		chartCRD, ok := chartCRDs[crdName]
		if !ok {
			out.Messages = append(out.Messages,
				fmt.Sprintf("::error::Chart template missing CRD %s", crdName))
			out.Drifts = append(out.Drifts, Drift{
				ShortName: short,
				CRDName:   crdName,
				Reason:    "chart template missing",
			})
			continue
		}
		srcSpec := normalizeSpec(srcCRD.Spec)
		chartSpec := normalizeSpec(chartCRD.Spec)
		if srcSpec == chartSpec {
			out.Messages = append(out.Messages, fmt.Sprintf("  ✓ %s (%s)", short, crdName))
			continue
		}
		out.Messages = append(out.Messages,
			fmt.Sprintf("  ✗ %s (%s) — semantic drift detected", short, crdName),
			fmt.Sprintf("    Reproduce: loopruntime-ops quality crd-semantic-diff --crd %s", short))
		d := Drift{
			ShortName:  short,
			CRDName:    crdName,
			Reason:     "spec mismatch",
			SourceSpec: srcSpec,
			ChartSpec:  chartSpec,
		}
		out.Drifts = append(out.Drifts, d)
		if cfg.Verbose {
			out.Messages = append(out.Messages,
				"    --- source",
				indent(srcSpec, "    "),
				"    --- chart",
				indent(chartSpec, "    "))
		}
	}

	if err := (scanresult.Result{Scanned: out.Scanned}).Err(); err != nil {
		return nil, fmt.Errorf("%w: no source CRD in %s matched (filter %q)", err, resolvedSource, cfg.CRDFilter)
	}
	out.Messages = append(out.Messages, "", fmt.Sprintf("Scanned: %d CRDs", out.Scanned))
	if len(out.Drifts) == 0 {
		out.Pass = true
		out.Messages = append(out.Messages,
			fmt.Sprintf("✓ All %d CRDs are in semantic sync.", out.Scanned))
		return out, nil
	}
	out.Pass = false
	out.Messages = append(out.Messages,
		fmt.Sprintf("::error::Semantic drift in %d CRD(s). Run with --verbose for diff details.", len(out.Drifts)),
		"Fix: regenerate chart from kubebuilder via 'task helm:sync-crds'")
	return out, nil
}

// indent prefixes every line of s with prefix.
func indent(s, prefix string) string {
	var sb strings.Builder
	for _, line := range strings.Split(s, "\n") {
		sb.WriteString(prefix)
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}
