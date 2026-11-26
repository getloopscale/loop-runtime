// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Cleanroom catch 2026-06-19 (cleanroom-mandate-run-2026-06-19 session):
// the Slice 4 ISP remediation commit 52ccf8a6 (2026-06-18) tightened the
// per-operator chart schemas (additionalProperties:false at /config) and
// renamed config.insecureSkipTls → config.operatorInsecureSkipTls, but the
// dev-public helmfile overlay files were not updated in the same commit.
// `helmfile sync` then FAILed hard against `loopruntime-platform-operator` AND
// `loopruntime-tenant-operator` with messages like:
//
//	values don't meet the specifications of the schema(s):
//	loopruntime-platform-operator:
//	- at '/config': additional properties 'baseDomain' not allowed
//
//	loopruntime-tenant-operator:
//	- at '/config': additional properties 'insecureSkipTls' not allowed
//
// Same canonical stale-helper-post-architecture-refactor class as the
// 5 findings of issue #91 (memory `feedback_stale_helper_post_architecture_refactor_2026_06_18`).
// The Slice 4 commit already shipped an EC-3 transitive Go-deps guard
// catching the binary↔chart drift, but the env helmfile overlay (this
// directory) was not in its scope. This test closes that gap.
//
// Hermetic invariant: for every helmfile env overlay under
// infra/helmfile/values/<env>/<chart>.yaml.gotmpl, every key passed under
// `/config:` MUST be a documented property of the matching chart's
// values.schema.json (since additionalProperties:false at /config is now
// enforced uniformly per Slice 4).
package charts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// helmfileEnvsRoot resolves infra/helmfile/values relative to this test
// file (which lives at infra/tests/charts/).
func helmfileEnvsRoot(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "infra", "helmfile", "values")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("helmfile values dir not found at %s: %v", p, err)
	}
	return p
}

// extractGoTmplConfigKeys parses an env overlay (.yaml.gotmpl) and returns
// the top-level keys nested under `config:`. Gotmpl `{{ ... }}` directives
// are tolerated because we only key-extract; values are ignored.
//
// We intentionally avoid a full YAML/gotmpl parse — those files mix Go
// template directives with YAML which most stdlib parsers reject. Instead
// we use a structural line-shape detector matching the canonical
// indentation style enforced by every overlay in this repo (2-space
// indent under top-level `config:`).
func extractGoTmplConfigKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(b), "\n")
	var keys []string
	inConfig := false
	for _, raw := range lines {
		trim := strings.TrimRight(raw, " \t")
		// Top-level `config:` opens the section. Stop when we hit
		// another top-level key (zero indent + key: pattern).
		if strings.HasPrefix(trim, "#") || trim == "" {
			continue
		}
		if !strings.HasPrefix(trim, " ") && !strings.HasPrefix(trim, "\t") {
			if strings.HasPrefix(trim, "config:") {
				inConfig = true
				continue
			}
			inConfig = false
			continue
		}
		if !inConfig {
			continue
		}
		// Inside config: capture 2-space-indented direct children only.
		// Skip deeper nesting (4+ spaces) — schema check applies to the
		// top-level config props only.
		if !strings.HasPrefix(raw, "  ") || strings.HasPrefix(raw, "   ") {
			if !strings.HasPrefix(raw, "  ") {
				// 0-space line inside config: section already handled
				continue
			}
			// 3+ space line = nested under a config prop — skip.
			continue
		}
		body := strings.TrimSpace(trim)
		// body like `key: value` or `key:` (block scalar). Ignore comments,
		// list items, and gotmpl directives.
		if strings.HasPrefix(body, "-") || strings.HasPrefix(body, "#") || strings.HasPrefix(body, "{{") {
			continue
		}
		colonIdx := strings.IndexByte(body, ':')
		if colonIdx <= 0 {
			continue
		}
		k := strings.TrimSpace(body[:colonIdx])
		// Defensive: reject empty + keys that look like nested YAML
		// (no spaces in a valid YAML key).
		if k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// schemaConfigProps loads charts/<chart>/values.schema.json and returns
// the set of properties declared under /properties/config/properties.
func schemaConfigProps(t *testing.T, chartName string) (map[string]bool, bool) {
	t.Helper()
	schemaPath := filepath.Join(repoRoot(t), "charts", chartName, "values.schema.json")
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		// Not all overlays correspond to a published chart with a schema
		// (e.g. ory-hydra, prometheus). Skip those.
		return nil, false
	}
	var doc struct {
		Properties struct {
			Config struct {
				Properties           map[string]any `json:"properties"`
				AdditionalProperties any            `json:"additionalProperties"`
			} `json:"config"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", schemaPath, err)
	}
	// Only enforce drift on schemas that lock additional properties OFF
	// (Slice 4 hardening). Schemas without that lock are advisory.
	addProp := doc.Properties.Config.AdditionalProperties
	locked := false
	if b, ok := addProp.(bool); ok && !b {
		locked = true
	}
	if !locked {
		return nil, false
	}
	props := make(map[string]bool, len(doc.Properties.Config.Properties))
	for k := range doc.Properties.Config.Properties {
		props[k] = true
	}
	return props, true
}

// TestHelmfileOverlayConfigKeysHonorSchema is the canonical regression
// gate for the cleanroom-mandate-run-2026-06-19 Slice 4 drift class. For
// every helmfile env overlay under infra/helmfile/values/<env>/, every
// key under top-level `config:` MUST exist in the matching chart's
// values.schema.json /properties/config/properties (when the schema has
// additionalProperties:false at that path — i.e., the Slice 4 hardening).
//
// Schemas without the hardening lock are SKIPPED with INFO — only the
// hardened-root charts are gated. New hardened-root chart joins the gate
// automatically.
func TestHelmfileOverlayConfigKeysHonorSchema(t *testing.T) {
	envsRoot := helmfileEnvsRoot(t)
	entries, err := os.ReadDir(envsRoot)
	if err != nil {
		t.Fatalf("list helmfile values envs: %v", err)
	}

	type drift struct {
		env, chart string
		extras     []string
		schema     []string
	}
	var drifts []drift
	var scanned, gated, skipped int

	for _, env := range entries {
		if !env.IsDir() {
			continue
		}
		envDir := filepath.Join(envsRoot, env.Name())
		files, err := os.ReadDir(envDir)
		if err != nil {
			t.Fatalf("list env %s: %v", env.Name(), err)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			if !strings.HasSuffix(name, ".yaml.gotmpl") && !strings.HasSuffix(name, ".yaml") {
				continue
			}
			// Chart name = file basename minus extension.
			chart := strings.TrimSuffix(strings.TrimSuffix(name, ".gotmpl"), ".yaml")
			scanned++

			schemaProps, hardened := schemaConfigProps(t, chart)
			if !hardened {
				skipped++
				continue
			}
			gated++

			overlayPath := filepath.Join(envDir, name)
			overlayKeys := extractGoTmplConfigKeys(t, overlayPath)
			var extras []string
			for _, k := range overlayKeys {
				if !schemaProps[k] {
					extras = append(extras, k)
				}
			}
			if len(extras) > 0 {
				schemaList := make([]string, 0, len(schemaProps))
				for k := range schemaProps {
					schemaList = append(schemaList, k)
				}
				sort.Strings(schemaList)
				drifts = append(drifts, drift{
					env:    env.Name(),
					chart:  chart,
					extras: extras,
					schema: schemaList,
				})
			}
		}
	}

	t.Logf("scanned=%d overlays, gated=%d (schema additionalProperties:false), skipped=%d (advisory)",
		scanned, gated, skipped)

	if len(drifts) > 0 {
		for _, d := range drifts {
			t.Errorf(
				"DRIFT env=%s chart=%s overlay-extras=%v\n  schema accepts: %v\n  hint: drop the extra keys OR add them to charts/%s/values.schema.json (Slice 4 hardening)",
				d.env, d.chart, d.extras, d.schema, d.chart,
			)
		}
		t.Fatalf("%d helmfile overlay/schema drift(s) found — Cleanroom catch 2026-06-19 regression class (stale-helper-post-architecture-refactor)",
			len(drifts))
	}
}
