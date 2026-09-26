// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scheduledimage refuses an autonomously executed manifest whose image has
// no way to exist.
//
// # The defect it closes
//
// Four CronWorkflows ran `registry.digitalocean.com/loopzero-system/loopzero-ops:v0.1.0`
// while the registry answered 404, no Dockerfile built loopzero-ops, the build task the
// values file cited did not exist, and the publication matrix did not list it. No
// imagePullSecret resolves an image nobody builds, so every run produced a red pod —
// worse than no automation, because it trains whoever watches the cluster to ignore
// red.
//
// # The criterion
//
// The gate judges OUR image used by an autonomously executed resource: CronWorkflow,
// CronJob and WorkflowTemplate (event-triggered). What they share is that the
// failure surfaces far from whoever caused it. A third-party image is not ours to
// publish and is not judged.
//
// The check is hermetic: our images are compared against this tree's publication
// matrix, never against the registry — probing a registry needs network and
// credentials at push time, and a 401 does not tell absent from unauthorised.
//
// # Limits
//
// The chart is not rendered. `{{ .Values.x.y }}` (optionally `| default "lit"`),
// `$var := .Values.x` and
// `$var := printf "..." .Values.a .Values.b` are resolved textually, which covers
// the forms this tree uses. Any other expression (a conditional, `include`, `tpl`)
// stays unresolved, and an image still holding a template expression is reported as
// UNRESOLVED — never assumed to be a third-party image. The gate measures what the
// chart declares, not what an environment enables.
package scheduledimage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

// ErrNoPublicationPath means an autonomous manifest uses one of our images that
// nothing in this tree builds, or an image the gate could not resolve.
var ErrNoPublicationPath = errors.New("an autonomously executed resource uses an image of ours with no publication path")

// ErrNothingScanned means the scan reached no autonomously executed manifest.
var ErrNothingScanned = errors.New("no autonomously executed manifest was scanned — finding nothing is not finding nothing wrong")

// ErrMatrixUnreadable means the publication matrix could not be read. Absence is a
// failure: without the matrix there is no way to know what is published.
var ErrMatrixUnreadable = errors.New("publication matrix unreadable")

// matrixRelPath is where the published image list lives.
const matrixRelPath = ".github/workflows/release-artifacts.yml"

// ourRegistries marks an image as ours to publish. A closed list on purpose: a new
// prefix is added consciously, where a heuristic ("anything named loopzero") would
// claim a third party's `loopzero-` image.
var ourRegistries = []string{
	"registry.digitalocean.com/loopzero-system/",
	"ghcr.io/getloopscale/",
	"ghcr.io/getloopscale/",
}

var (
	scheduledKindRE = regexp.MustCompile(`(?m)^kind:\s*(CronWorkflow|CronJob|WorkflowTemplate)\s*$`)
	imageLineRE     = regexp.MustCompile(`(?m)^\s*-?\s*image:\s*(.+)$`)
	// `{{ .Values.a.b }}`, optionally piped through `default "literal"`.
	valuesRefRE = regexp.MustCompile(`\{\{-?\s*\.Values\.([A-Za-z0-9_.]+)\s*(?:\|\s*default\s+"([^"]*)"\s*)?-?\}\}`)
	// The form the real tree uses: the image is assembled in a template variable
	// and used afterwards.
	varAssignRE   = regexp.MustCompile(`\{\{-?\s*(\$[A-Za-z0-9_]+)\s*:=\s*(.+?)\s*-?\}\}`)
	varUseRE      = regexp.MustCompile(`\{\{-?\s*(\$[A-Za-z0-9_]+)(?:\s*\|[^}]*)?\s*-?\}\}`)
	printfRE      = regexp.MustCompile(`^printf\s+"([^"]*)"\s+(.+)$`)
	bareValueRE   = regexp.MustCompile(`\.Values\.([A-Za-z0-9_.]+)`)
	matrixImageRE = regexp.MustCompile(`"image"\s*:\s*"([^"]+)"`)
)

// Exemption is an image knowingly without a publication path. Without a reason AND
// a sunset an entry declares nothing, it only silences.
type Exemption struct {
	Image  string
	Reason string
	Sunset string // YYYY-MM-DD
}

// DefaultExemptions is empty on purpose.
var DefaultExemptions = []Exemption{}

// Config drives the check.
type Config struct {
	RepoRoot string
	// Exemptions defaults to DefaultExemptions when nil.
	Exemptions []Exemption
}

func (c Config) exemptions() []Exemption {
	if c.Exemptions == nil {
		return DefaultExemptions
	}
	return c.Exemptions
}

// Finding is one image with no publication path, or one that could not be resolved.
type Finding struct {
	Chart string
	File  string
	Image string
	Why   string
}

// Outcome reports the scan.
type Outcome struct {
	Scanned  int
	Exempt   int
	Findings []Finding
	Reason   string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// Check scans every autonomously executed manifest under infra/helm and charts.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	published, err := publishedImages(filepath.Join(root, matrixRelPath))
	if err != nil {
		return out, err
	}

	exempt := map[string]bool{}
	for _, e := range cfg.exemptions() {
		if strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.Sunset) == "" {
			out.Violation = fmt.Errorf("%w: the exemption for %q has no reason or no sunset — without both it declares nothing, it only silences",
				ErrNoPublicationPath, e.Image)
			return out, nil
		}
		exempt[e.Image] = true
	}

	manifests, err := scheduledManifests(root)
	if err != nil {
		return out, err
	}
	out.Scanned = len(manifests)
	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}

	for _, m := range manifests {
		chartDir := filepath.Dir(filepath.Dir(m))
		vals, err := loadValues(filepath.Join(chartDir, "values.yaml"))
		if err != nil {
			return out, err
		}
		images, err := imagesIn(m, vals)
		if err != nil {
			return out, err
		}
		rel, _ := filepath.Rel(root, m)
		for _, img := range images {
			if f, ok := judge(img, published, exempt, &out); ok {
				f.Chart, f.File = filepath.Base(chartDir), rel
				out.Findings = append(out.Findings, f)
			}
		}
	}

	sort.Slice(out.Findings, func(i, j int) bool { return out.Findings[i].File < out.Findings[j].File })
	if len(out.Findings) > 0 {
		out.Violation = fmt.Errorf("%w (%d): %s", ErrNoPublicationPath, len(out.Findings), out.Findings[0].Why)
		return out, nil
	}
	out.Reason = fmt.Sprintf("%d autonomously executed manifest(s) scanned; %d image(s) exempt with reason and sunset", out.Scanned, out.Exempt)
	return out, nil
}

// judge decides one image. It returns a finding for an unresolved image or for one
// of ours that the matrix does not publish, and counts an exempt one.
func judge(img string, published, exempt map[string]bool, out *Outcome) (Finding, bool) {
	if strings.Contains(img, "{{") {
		if exempt[img] {
			out.Exempt++
			return Finding{}, false
		}
		return Finding{Image: img, Why: fmt.Sprintf("%q still holds a template expression this gate cannot resolve, "+
			"so it cannot tell whose image it is. Resolve it through .Values (directly or via a $var / printf), "+
			"or exempt it with a reason and a sunset", img)}, true
	}
	if !isOurs(img) {
		return Finding{}, false // a third-party image is not ours to publish
	}
	name := imageName(img)
	if published[name] {
		return Finding{}, false
	}
	if exempt[name] {
		out.Exempt++
		return Finding{}, false
	}
	return Finding{Image: img, Why: fmt.Sprintf("%q is under one of our registries and is not in %s — nothing in this "+
		"tree builds it, so no imagePullSecret resolves it. Publish it, or point the manifest at an image that exists",
		name, matrixRelPath)}, true
}

// publishedImages reads the image names the release matrix publishes.
func publishedImages(path string) (map[string]bool, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrMatrixUnreadable, path, err)
	}
	out := map[string]bool{}
	for _, m := range matrixImageRE.FindAllStringSubmatch(string(body), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %s declares no image", ErrMatrixUnreadable, path)
	}
	return out, nil
}

// scheduledManifests finds every chart template declaring an autonomously executed
// resource. A missing subtree (infra/helm or charts) holds nothing; any other walk
// or read error is returned.
func scheduledManifests(root string) ([]string, error) {
	var out []string
	for _, base := range []string{"infra/helm", "charts"} {
		dir := filepath.Join(root, base)
		err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				if p == dir && os.IsNotExist(err) {
					return filepath.SkipDir
				}
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".yaml") && !strings.HasSuffix(p, ".yml") {
				return nil
			}
			if !strings.Contains(p, string(os.PathSeparator)+"templates"+string(os.PathSeparator)) {
				return nil
			}
			// #nosec G122 -- p comes from the Walk over a constant-derived root and is
			// only read; the worst case of a symlink is reading a YAML that is not a chart.
			body, err := os.ReadFile(filepath.Clean(p))
			if err != nil {
				return err
			}
			if scheduledKindRE.Match(body) {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", base, err)
		}
	}
	sort.Strings(out)
	return out, nil
}

// imagesIn extracts every image from a manifest, resolved against the values.
func imagesIn(path string, vals map[string]any) ([]string, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	vars := templateVars(string(body), vals)
	var out []string
	for _, m := range imageLineRE.FindAllStringSubmatch(string(body), -1) {
		raw := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		raw = resolveVars(raw, vars)
		out = append(out, resolve(raw, vals))
	}
	return out, nil
}

// templateVars resolves `{{- $name := ... }}` assignments in the same file: a
// direct `.Values.x`, and `printf "fmt" .Values.a .Values.b`. Any other form stays
// unresolved, which keeps the original text visible instead of producing a
// plausible but false image the gate would approve.
func templateVars(body string, vals map[string]any) map[string]string {
	out := map[string]string{}
	for _, m := range varAssignRE.FindAllStringSubmatch(body, -1) {
		name, expr := m[1], strings.TrimSpace(m[2])
		if p := printfRE.FindStringSubmatch(expr); p != nil {
			format := p[1]
			var args []any
			for _, a := range bareValueRE.FindAllStringSubmatch(p[2], -1) {
				v, ok := lookup(vals, strings.Split(a[1], "."))
				if !ok {
					args = nil
					break
				}
				args = append(args, v)
			}
			if args != nil && strings.Count(format, "%") == len(args) {
				out[name] = fmt.Sprintf(format, args...)
			}
			continue
		}
		if a := bareValueRE.FindStringSubmatch(expr); a != nil && expr == "."+"Values."+a[1] {
			if v, ok := lookup(vals, strings.Split(a[1], ".")); ok {
				out[name] = fmt.Sprintf("%v", v)
			}
		}
	}
	return out
}

// resolveVars replaces `{{ $name }}` and `{{ $name | quote }}` with the value, or
// leaves the reference when the variable is unknown.
func resolveVars(raw string, vars map[string]string) string {
	return varUseRE.ReplaceAllStringFunc(raw, func(ref string) string {
		m := varUseRE.FindStringSubmatch(ref)
		if m == nil {
			return ref
		}
		if v, ok := vars[m[1]]; ok {
			return v
		}
		return ref
	})
}

// resolve replaces `{{ .Values.a.b }}` with the value — or with the literal of a
// `| default "..."` when the value is unset — leaving unresolved forms visible so
// the operator sees what the gate saw.
func resolve(raw string, vals map[string]any) string {
	return valuesRefRE.ReplaceAllStringFunc(raw, func(ref string) string {
		m := valuesRefRE.FindStringSubmatch(ref)
		if m == nil {
			return ref
		}
		if v, ok := lookup(vals, strings.Split(m[1], ".")); ok && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
		if m[2] != "" {
			return m[2]
		}
		return ref
	})
}

func lookup(vals map[string]any, path []string) (any, bool) {
	var cur any = vals
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// loadValues reads the chart's values.yaml. A chart without one has no values; a
// values file that cannot be read or parsed is an error — resolving images against
// it silently as empty would leave them unresolved for the wrong reason.
func loadValues(path string) (map[string]any, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return out, nil
}

// isOurs reports whether we are the ones who must publish this image.
func isOurs(img string) bool {
	for _, p := range ourRegistries {
		if strings.HasPrefix(img, p) {
			return true
		}
	}
	return false
}

// imageName strips registry and tag, leaving the name the matrix declares.
func imageName(img string) string {
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		img = img[:i]
	}
	if i := strings.LastIndex(img, "/"); i >= 0 {
		img = img[i+1:]
	}
	return img
}
