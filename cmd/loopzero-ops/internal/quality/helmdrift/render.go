// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Renderer turns a chart directory into the manifest stream it installs, under
// the values `sets` (each a `--set` expression; nil renders at chart defaults).
//
// It is an interface so the gate can be tested without `helm` on the machine —
// a gate that required the toolchain it audits in order to run its own tests
// would teach the reader that a red suite is environmental.
//
// # Why values are on THIS method rather than on a second interface
//
// The RBAC comparison (B-001) renders the published chart once per entry of
// rbacValuesMatrix, and the original signature had no room for that. A separate
// `ValuesRenderer` would have left the existing doubles untouched, at the cost of
// a type assertion at the call site and a fallback that renders at defaults — so
// a caller could report "both flag states agree" about a renderer that ignored
// them. Widening the one method means every implementation is handed the values
// and none can silently drop them; the two doubles in the suite take a parameter
// they ignore, which is visible in their source rather than hidden in a branch.
type Renderer interface {
	Render(chartDir string, sets []string) (string, error)
}

// helmRenderer is the production implementation: `helm template`, at chart
// defaults when `sets` is empty and under the given `--set` expressions
// otherwise.
//
// The release name is fixed so two runs of the same chart produce byte-identical
// output; it is never compared across charts, because the two chart families use
// deliberately different naming conventions (see renderedKinds).
type helmRenderer struct{}

// helmTemplateArgs builds the invocation. Split out from Render so how the gate
// calls helm can be asserted without helm being installed — the values are the
// one part of the invocation that the comparison's correctness depends on.
func helmTemplateArgs(chartDir string, sets []string) []string {
	args := []string{"template", "drift-probe", chartDir}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	return args
}

func (helmRenderer) Render(chartDir string, sets []string) (string, error) {
	args := helmTemplateArgs(chartDir, sets)
	out, err := exec.Command("helm", args...).Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errorsAs(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		// The --set list is named so the failing invocation can be reproduced
		// from the message alone.
		return "", fmt.Errorf("helm %s: %w%s", strings.Join(args, " "), err, prefixed(stderr))
	}
	return string(out), nil
}

// errorsAs is a tiny indirection so the import list stays honest about what this
// file needs; behaviour is errors.As.
func errorsAs(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError) //nolint:errorlint // direct type is what we want here
	if ok {
		*target = ee
	}
	return ok
}

func prefixed(s string) string {
	if s == "" {
		return ""
	}
	return " — " + s
}

// renderedKinds returns the set of resource KINDS a chart installs.
//
// # Why kinds, and not kind+name
//
// Measured 2026-08-16: comparing by kind+name gives ZERO resources in common for
// `loopzero-api` and `loopzero-cloud`, because the published charts prefix names with the
// release (`t-loopzero-api`) while the bootstrap charts hardcode them (`loopzero-api`).
// Those are deliberately different conventions, not drift — a gate built on the
// assumption that names match would report 100% drift on two of three pairs.
//
// The cost of kind granularity is declared, not hidden: two DIFFERENT ConfigMaps
// both count as "ConfigMap present". This gate answers "do the two sides install
// the same TYPES of resource?", not "the same resources". Going finer would mean
// normalising names across two intentional conventions — inventing a mapping —
// and an invented mapping inside a gate is the very defect this one exists to
// catch.
//
// # Why `helm.sh/hook: test` is excluded
//
// A resource annotated as a test hook is not installed by `helm install`; only
// `helm test` runs it. Counting it reports an asymmetry with no effect on what is
// actually running — and measured against the real charts, it is exactly the
// difference that showed up (`Pod` on the published side of both pairs).
func renderedKinds(r Renderer, chartDir string) (map[string]bool, error) {
	// nil values: the kind comparison renders at chart DEFAULTS, unchanged. Its
	// eleven approvals were measured under them, and moving its baseline while
	// adding a second comparison would make both findings unreadable.
	raw, err := r.Render(chartDir, nil)
	if err != nil {
		// A render that FAILED and a chart that produces NOTHING are different
		// facts. Collapsing the first into the second would make the gate report
		// total drift as though it had measured it.
		return nil, err
	}

	kinds := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(raw))
	for {
		var doc map[string]any
		derr := dec.Decode(&doc)
		if derr != nil {
			if derr.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("parsing rendered manifest of %s: %w", chartDir, derr)
		}
		// `helm template` emits stray `---` separators when a conditional block
		// renders nothing, so a nil document in the stream is normal.
		if doc == nil {
			continue
		}
		kind, _ := doc["kind"].(string)
		if kind == "" {
			continue
		}
		if isTestHook(doc) {
			continue
		}
		kinds[kind] = true
	}
	return kinds, nil
}

// isTestHook reports whether a rendered document is a Helm test hook.
func isTestHook(doc map[string]any) bool {
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		return false
	}
	ann, _ := meta["annotations"].(map[string]any)
	if ann == nil {
		return false
	}
	hook, _ := ann["helm.sh/hook"].(string)
	return strings.Contains(hook, "test")
}

// diffKinds returns what only the published chart installs and what only the
// bootstrap chart installs, each ordered so two reports diff cleanly.
func diffKinds(published, bootstrap map[string]bool) (onlyPublished, onlyBootstrap []string) {
	for k := range published {
		if !bootstrap[k] {
			onlyPublished = append(onlyPublished, k)
		}
	}
	for k := range bootstrap {
		if !published[k] {
			onlyBootstrap = append(onlyBootstrap, k)
		}
	}
	sort.Strings(onlyPublished)
	sort.Strings(onlyBootstrap)
	return onlyPublished, onlyBootstrap
}
