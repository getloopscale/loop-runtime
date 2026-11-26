// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Harbor was retired on 2026-09-25 (docs/decisions.md, "Retire Harbor"): the api no
// longer mirrors images to it and the tenant-operator no longer provisions projects
// or robot accounts in it. An installation whose values still set one of the keys
// that switched those on expects a behaviour that no longer exists, so the install
// must fail and name the key — never render and quietly drop it.
//
// The chart schema is the boundary that says so: both charts refuse an unknown key
// under `config` and `secrets` (additionalProperties:false), and the binaries read
// only what the charts deliver (TestLoopRuntimeOperatorsEnvCompleteness), so no environment
// variable is left for a retired value to arrive through.
func TestAnInstallStillSettingAHarborKeyIsRefused(t *testing.T) {
	cases := []struct {
		chart, key string
	}{
		{"loopruntime-api", "config.harborRegistryUrl"},
		{"loopruntime-tenant-operator", "config.harborUrl"},
		{"loopruntime-tenant-operator", "config.harborAdminUsername"},
		{"loopruntime-tenant-operator", "config.harborRegistryHost"},
		{"loopruntime-tenant-operator", "secrets.harborAdminPassword"},
	}
	for _, tc := range cases {
		t.Run(tc.chart+"/"+tc.key, func(t *testing.T) {
			_, err := renderChartE(t, publishedChart(t, tc.chart), "retired-harbor",
				map[string]string{tc.key: "harbor.example.com"})

			leaf := tc.key[strings.LastIndex(tc.key, ".")+1:]
			if err == nil || !strings.Contains(err.Error(), leaf) {
				t.Fatalf("%s rendered with %s set (err = %v); the schema must refuse the retired key by name", tc.chart, tc.key, err)
			}
		})
	}
}

// The schema makes a Harbor branch in a template unreachable, and an unreachable
// branch is not harmless: it is what comes back live the day somebody loosens the
// schema to accept one more key, delivering a credential nothing consumes. So the
// retired keys must be gone from the templates too, not only refused at the door.
func TestNoTemplateStillRendersAHarborValue(t *testing.T) {
	for _, chart := range []string{"loopruntime-api", "loopruntime-tenant-operator"} {
		t.Run(chart, func(t *testing.T) {
			templates := filepath.Join(publishedChart(t, chart), "templates")
			err := filepath.WalkDir(templates, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(strings.ToLower(string(body)), "harbor") {
					t.Errorf("%s still names Harbor; a branch the schema cannot reach is dead code that returns live when the schema loosens", path)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", templates, err)
			}
		})
	}
}
