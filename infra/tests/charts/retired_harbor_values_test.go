// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
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
// only what the charts deliver (TestLoopZeroOperatorsEnvCompleteness), so no environment
// variable is left for a retired value to arrive through.
func TestAnInstallStillSettingAHarborKeyIsRefused(t *testing.T) {
	cases := []struct {
		chart, key string
	}{
		{"loopzero-api", "config.harborRegistryUrl"},
		{"loopzero-tenant-operator", "config.harborUrl"},
		{"loopzero-tenant-operator", "config.harborAdminUsername"},
		{"loopzero-tenant-operator", "config.harborRegistryHost"},
		{"loopzero-tenant-operator", "secrets.harborAdminPassword"},
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
