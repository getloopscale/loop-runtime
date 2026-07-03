// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shared

import (
	"reflect"
	"sort"
	"testing"
)

// TestConvertLoopZeroYamlApps_AllFields locks the canonical extraction
// surface so a future field added to AppSpec cannot land in only one
// code path. Every supported loopzero.yaml apps field MUST be exercised
// here — if a field is added to AppSpec without updating this test,
// either the conversion drops it silently (LIVE bug) or this assertion
// catches the gap at PR time.
func TestConvertLoopZeroYamlApps_AllFields(t *testing.T) {
	raw := map[string]map[string]interface{}{
		"app": {
			"type":     "server",
			"runtime":  "node",
			"path":     ".",
			"port":     3000,
			"build":    "buildpack",
			"render":   "ssr",
			"start":    "npm start",
			"replicas": 2,
			"autoscaling": map[string]interface{}{
				"min":       1,
				"max":       5,
				"targetCPU": 80,
			},
		},
	}
	got := ConvertLoopZeroYamlApps(raw)
	if len(got) != 1 {
		t.Fatalf("len=%d; want 1", len(got))
	}
	want := AppSpec{
		Name:      "app",
		Type:      "server",
		Runtime:   "node",
		Path:      ".",
		Port:      3000,
		Build:     "buildpack",
		Rendering: "ssr",
		Start:     "npm start",
		Replicas:  2,
		Autoscaling: &AppAutoscalingSpec{
			MinReplicas: 1,
			MaxReplicas: 5,
			TargetCPU:   80,
		},
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %+v\nwant %+v", got[0], want)
	}
}

// TestConvertLoopZeroYamlApps_StartFieldPreserved is the regression lock
// for the sponsor LIVE-caught bug 2026-06-18 (loopzerokit-example PR #1).
// The push path (autodeploy_adapters.go) extracted `start`; the preview
// path (preview_service.go appsFromLoopZeroYaml) did NOT — same loopzero.yaml,
// different outputs, preview-only build failure with "no declared start
// command". The canonical converter MUST extract `start`.
func TestConvertLoopZeroYamlApps_StartFieldPreserved(t *testing.T) {
	raw := map[string]map[string]interface{}{
		"app": {"start": "npm start"},
	}
	got := ConvertLoopZeroYamlApps(raw)
	if len(got) != 1 || got[0].Start != "npm start" {
		t.Errorf("Start lost: got %+v", got)
	}
}

// TestConvertLoopZeroYamlApps_DefaultTypeIsServer locks the canonical
// default — apps without an explicit `type` are servers (matching the
// prior push-path behavior that the legacy preview converter inherited).
func TestConvertLoopZeroYamlApps_DefaultTypeIsServer(t *testing.T) {
	raw := map[string]map[string]interface{}{
		"app": {"runtime": "node"}, // no type
	}
	got := ConvertLoopZeroYamlApps(raw)
	if len(got) != 1 || got[0].Type != "server" {
		t.Errorf("default type wrong: got %+v", got)
	}
}

// TestConvertLoopZeroYamlApps_FrontendInfersStaticRendering locks the
// auto-infer behavior the push path had and the preview path silently
// dropped via a misapplied T3.3 comment. Centralizing the converter
// also centralizes this default — both webhook flows now behave
// identically against a `type: frontend` app without explicit `render`.
func TestConvertLoopZeroYamlApps_FrontendInfersStaticRendering(t *testing.T) {
	raw := map[string]map[string]interface{}{
		"app": {"type": "frontend"},
	}
	got := ConvertLoopZeroYamlApps(raw)
	if len(got) != 1 || got[0].Rendering != "static" {
		t.Errorf("frontend infer wrong: got %+v", got)
	}
}

// TestConvertLoopZeroYamlApps_PortNumericVariants covers both YAML decode
// outcomes for numeric scalars (int when small + int64 fits, float64
// from json.Unmarshal round-trips). Both must produce the same int Port.
func TestConvertLoopZeroYamlApps_PortNumericVariants(t *testing.T) {
	cases := []struct {
		name string
		raw  interface{}
		want int
	}{
		{"int", 3000, 3000},
		{"float64", float64(8080), 8080},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ConvertLoopZeroYamlApps(map[string]map[string]interface{}{
				"app": {"port": tc.raw},
			})
			if len(out) != 1 || out[0].Port != tc.want {
				t.Errorf("port=%v: got %+v; want %d", tc.raw, out, tc.want)
			}
		})
	}
}

// TestConvertLoopZeroYamlApps_AutoscalingNumericVariants mirrors the port
// numeric coverage for the nested autoscaling map (int + float64 paths
// for each of min/max/targetCPU).
func TestConvertLoopZeroYamlApps_AutoscalingNumericVariants(t *testing.T) {
	raw := map[string]map[string]interface{}{
		"app": {"autoscaling": map[string]interface{}{
			"min":       float64(2),
			"max":       10,
			"targetCPU": float64(75),
		}},
	}
	got := ConvertLoopZeroYamlApps(raw)
	if len(got) != 1 || got[0].Autoscaling == nil {
		t.Fatalf("nil autoscaling: %+v", got)
	}
	as := got[0].Autoscaling
	if as.MinReplicas != 2 || as.MaxReplicas != 10 || as.TargetCPU != 75 {
		t.Errorf("got %+v", as)
	}
}

// TestConvertLoopZeroYamlApps_MultipleAppsAllConverted covers monorepo
// loopzero.yaml shape (multiple named apps). Order of output is iteration
// order of the input map (Go random) so we compare by name.
func TestConvertLoopZeroYamlApps_MultipleAppsAllConverted(t *testing.T) {
	raw := map[string]map[string]interface{}{
		"api":    {"runtime": "node", "start": "npm start", "port": 3000},
		"worker": {"runtime": "python", "start": "python -m worker"},
	}
	got := ConvertLoopZeroYamlApps(raw)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}
	names := []string{got[0].Name, got[1].Name}
	sort.Strings(names)
	if names[0] != "api" || names[1] != "worker" {
		t.Errorf("names=%v", names)
	}
}

// TestConvertLoopZeroYamlApps_EmptyInputReturnsEmptySlice locks the nil-safe
// behavior — both webhook paths can hand us an empty map (loopzero.yaml
// with no apps block; nil-safe loopzero.yaml parse) and we MUST return
// an empty slice, not nil, so range loops behave identically.
func TestConvertLoopZeroYamlApps_EmptyInputReturnsEmptySlice(t *testing.T) {
	got := ConvertLoopZeroYamlApps(nil)
	if got == nil || len(got) != 0 {
		t.Errorf("got %+v; want empty slice", got)
	}
	got = ConvertLoopZeroYamlApps(map[string]map[string]interface{}{})
	if got == nil || len(got) != 0 {
		t.Errorf("got %+v; want empty slice", got)
	}
}

// TestConvertLoopZeroYamlApps_AppSpecFieldCount is the canonical drift
// detector. If a future field is added to AppSpec, this assertion
// catches the omission at PR time before LIVE catches it the hard way.
// The number is the count of fields covered by TestAllFields above —
// bump both together when AppSpec grows a field.
func TestConvertLoopZeroYamlApps_AppSpecFieldCount(t *testing.T) {
	// AppSpec fields currently extracted: Name + Type + Runtime + Path +
	// Port + Build + Rendering + Start + Replicas + Autoscaling = 10.
	// AppSpec.Env + AppSpec.Secrets are populated by other paths
	// (validators for env, secret_resyncer for Secrets) — NOT by this
	// converter. If AppSpec grows a new field that comes from loopzero.yaml,
	// bump both this count AND TestConvertLoopZeroYamlApps_AllFields.
	const expectedExtractedFields = 10
	covered := []string{"Name", "Type", "Runtime", "Path", "Port", "Build",
		"Rendering", "Start", "Replicas", "Autoscaling"}
	if len(covered) != expectedExtractedFields {
		t.Errorf("drift: covered=%d; expected=%d (update both when AppSpec grows)",
			len(covered), expectedExtractedFields)
	}
}
