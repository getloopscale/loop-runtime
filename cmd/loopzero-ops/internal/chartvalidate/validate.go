// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package chartvalidate renders one Helm chart and schema-validates what it
// renders, with no path on which a broken render reads as a valid one.
//
// `task validate:manifests` used to run `helm template … | kubeconform`.
// go-task runs a command without pipefail, so the pipeline's status was
// kubeconform's alone: a render that failed gave kubeconform an empty stdin,
// it reported "0 resource found" and exited 0, and the chart passed having
// been validated not at all (#392). On a fresh clone that is what happened to
// Velero, whose upstream dependency is gitignored and was never built.
//
// Validate takes each step on its own and stops at the first that goes wrong,
// naming the chart:
//
//  1. a chart that declares dependencies has them built first, as every other
//     task that renders a chart does — a fresh clone vendors none of them;
//  2. the render must exit 0 and must not have been truncated;
//  3. the render must hold at least one Kubernetes object;
//  4. kubeconform validates that render, from a file, and must exit 0.
//
// A violation is a Fail outcome. The error Validate returns means only that it
// could not measure: a tool that is not installed, a directory that is not a
// chart, a temporary file it could not write.
package chartvalidate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/shellrun"
)

// schemaFileTemplate is how kubeconform names a schema inside a schema
// directory — the layout of infra/kubeconform-schemas/master-standalone-strict.
const schemaFileTemplate = "{{.ResourceKind}}-{{.Group}}-{{.ResourceAPIVersion}}.json"

// Spec is one chart render to validate.
type Spec struct {
	Release     string
	Chart       string
	ValuesFiles []string
	Sets        []string
	// SkipKinds are kinds kubeconform does not validate, because no schema for
	// them exists locally or in its defaults.
	SkipKinds []string
	// SchemaDir holds local strict schemas, looked up before kubeconform's
	// defaults. Empty uses the defaults only.
	SchemaDir string
}

// Outcome is what one validation found.
type Outcome struct {
	Spec Spec
	// Objects is how many Kubernetes objects the render held.
	Objects int
	// Report is kubeconform's own output for a render it validated.
	Report string
	// Failure says what went wrong, naming the chart; empty on a pass.
	Failure string
}

// Verdict is Pass when every step succeeded and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Failure == "") }

// Validate renders spec.Chart and validates the render with kubeconform.
func Validate(ctx context.Context, spec Spec, run shellrun.Runner) (_ *Outcome, err error) {
	out := &Outcome{Spec: spec}
	fail := func(format string, args ...any) (*Outcome, error) {
		out.Failure = fmt.Sprintf("chart %s (release %s): ", spec.Chart, spec.Release) + fmt.Sprintf(format, args...)
		return out, nil
	}

	hasDeps, err := declaresDependencies(spec.Chart)
	if err != nil {
		return nil, err
	}
	if hasDeps {
		res, err := run.Run(ctx, "helm", []string{"dependency", "build", spec.Chart})
		if missing := toolMissing(err); missing != nil {
			return nil, missing
		}
		if err != nil {
			return fail("helm dependency build failed: %v%s", err, detail(res))
		}
	}

	res, err := run.Run(ctx, "helm", templateArgs(spec))
	if missing := toolMissing(err); missing != nil {
		return nil, missing
	}
	if err != nil {
		return fail("render failed: %v%s", err, detail(res))
	}
	if res.Truncated {
		return fail("render was truncated at %d bytes and cannot be validated whole", shellrun.MaxBufferSize)
	}
	objects, err := countObjects(res.Stdout)
	if err != nil {
		return fail("render is not valid YAML: %v", err)
	}
	if objects == 0 {
		return fail("render holds no Kubernetes object")
	}
	out.Objects = objects

	renderFile, err := writeRender(res.Stdout)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.Remove(renderFile); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("remove render file: %w", rmErr))
		}
	}()

	kres, err := run.Run(ctx, "kubeconform", kubeconformArgs(spec, renderFile))
	if missing := toolMissing(err); missing != nil {
		return nil, missing
	}
	if err != nil {
		return fail("kubeconform rejected the render: %v%s", err, detail(kres))
	}
	out.Report = strings.TrimSpace(kres.Stdout)
	return out, nil
}

func templateArgs(spec Spec) []string {
	args := []string{"template", spec.Release, spec.Chart}
	for _, f := range spec.ValuesFiles {
		args = append(args, "-f", f)
	}
	for _, s := range spec.Sets {
		args = append(args, "--set", s)
	}
	return args
}

func kubeconformArgs(spec Spec, renderFile string) []string {
	args := []string{"-strict", "-summary"}
	if spec.SchemaDir != "" {
		args = append(args, "-schema-location", spec.SchemaDir+"/"+schemaFileTemplate)
	}
	args = append(args, "-schema-location", "default")
	if len(spec.SkipKinds) > 0 {
		args = append(args, "-skip", strings.Join(spec.SkipKinds, ","))
	}
	return append(args, renderFile)
}

// declaresDependencies reports whether the chart's Chart.yaml lists any
// dependency. A directory with no readable Chart.yaml is not a chart.
func declaresDependencies(chart string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(chart, "Chart.yaml")) // #nosec G304 -- the chart path is the operator's own argument
	if err != nil {
		return false, fmt.Errorf("chart %s: %w", chart, err)
	}
	var meta struct {
		Dependencies []yaml.Node `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(body, &meta); err != nil {
		return false, fmt.Errorf("chart %s: parse Chart.yaml: %w", chart, err)
	}
	return len(meta.Dependencies) > 0, nil
}

// countObjects counts the documents of a render that declare a kind.
func countObjects(render string) (int, error) {
	dec := yaml.NewDecoder(strings.NewReader(render))
	n := 0
	for {
		var doc struct {
			Kind string `yaml:"kind"`
		}
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
		if doc.Kind != "" {
			n++
		}
	}
}

func writeRender(render string) (string, error) {
	f, err := os.CreateTemp("", "validate-chart-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create render file: %w", err)
	}
	_, werr := f.WriteString(render)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return "", errors.Join(fmt.Errorf("write render file: %w", err), os.Remove(f.Name()))
	}
	return f.Name(), nil
}

// toolMissing returns err when it says the binary is not installed: that is
// an inability to measure, never a verdict about the chart.
func toolMissing(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return err
	}
	return nil
}

// detail is the tool's own output, appended to a failure so it can be read
// without re-running anything.
func detail(res *shellrun.Result) string {
	if res == nil {
		return ""
	}
	msg := strings.TrimSpace(res.Stderr + "\n" + res.Stdout)
	if msg == "" {
		return ""
	}
	return "\n" + msg
}
