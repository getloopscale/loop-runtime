// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A published chart's values.schema.json is its configuration contract, and with
// `additionalProperties: false` at the top it is also the list of keys an installer is
// allowed to set. A key the schema admits and no template reads is a setting that
// validates and then does nothing.
//
// Measured before this test existed: the schemas of loop-runtime-api, loop-console,
// loop-console-dashboard and the four operators each admitted 9-12 such keys (autoscaling,
// ingress, env, envFrom, extraVolumes, extraVolumeMounts, podSecurityContext,
// containerSecurityContext, monitoring, rbac), copied from a template schema rather than
// derived from the chart. `ingress.enabled: true` passed validation and rendered nothing.
//
// A key is READ when a template of the chart references `.Values.<key>`, or — for a chart
// that depends on loop-runtime-common — a loop-runtime-common helper does. A key naming a dependency
// (Helm hands it to the subchart) is read by definition.
var valuesRefRe = regexp.MustCompile(`\.Values\.([A-Za-z0-9_]+)`)

func TestPublishedChartSchemas_AdmitOnlyKeysTheTemplatesRead(t *testing.T) {
	root := "../../../charts"
	common := readKeys(t, filepath.Join(root, "loop-runtime-common", "templates"))

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		chart := filepath.Join(root, e.Name())
		schemaBytes, err := os.ReadFile(filepath.Join(chart, "values.schema.json"))
		if err != nil {
			continue // no schema, no contract to check
		}
		var schema struct {
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(schemaBytes, &schema); err != nil {
			t.Fatalf("%s: parse schema: %v", e.Name(), err)
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			continue // an open schema promises nothing about the key set
		}
		checked++

		read := readKeys(t, filepath.Join(chart, "templates"))
		chartYAML, _ := os.ReadFile(filepath.Join(chart, "Chart.yaml"))
		if strings.Contains(string(chartYAML), "name: loop-runtime-common") {
			for k := range common {
				read[k] = true
			}
		}
		for _, dep := range regexp.MustCompile(`(?m)^\s*-\s*name:\s*(\S+)`).FindAllStringSubmatch(string(chartYAML), -1) {
			read[dep[1]] = true
		}

		var unread []string
		for key := range schema.Properties {
			if !read[key] {
				unread = append(unread, key)
			}
		}
		sort.Strings(unread)
		if len(unread) > 0 {
			t.Errorf("%s: values.schema.json admits %v, which no template reads — setting them "+
				"validates and does nothing. Remove them from the schema (and values.yaml), or "+
				"make a template read them.", e.Name(), unread)
		}
	}
	if checked == 0 {
		t.Fatal("no closed values.schema.json found under charts/ — a check over nothing proves nothing")
	}
}

func readKeys(t *testing.T, dir string) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range valuesRefRe.FindAllStringSubmatch(string(body), -1) {
			keys[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return keys
}
