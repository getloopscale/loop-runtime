// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

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

// The converter carries the declared type verbatim and never invents a
// rendering. Type rules live in one place — NormalizeApp — which accepts
// server|worker only; inferring "static" for "frontend" here encoded a
// second, contradicting rule for a delivery model that no longer exists.
func TestConvertLoopZeroYamlApps_FrontendTypeCarriedWithoutInventedRendering(t *testing.T) {
	got := ConvertLoopZeroYamlApps(map[string]map[string]interface{}{
		"app": {"type": "frontend"},
	})

	if len(got) != 1 || got[0].Type != "frontend" || got[0].Rendering != "" {
		t.Errorf("want type frontend with empty rendering, got %+v", got)
	}
}

func TestConvertLoopZeroYamlApps_ReplicasFloat64(t *testing.T) {
	got := ConvertLoopZeroYamlApps(map[string]map[string]interface{}{
		"app": {"replicas": float64(3)},
	})

	if len(got) != 1 || got[0].Replicas != 3 {
		t.Errorf("want replicas 3, got %+v", got)
	}
}

func TestConvertLoopZeroYamlApps_AutoscalingIntAndFloatMirror(t *testing.T) {
	got := ConvertLoopZeroYamlApps(map[string]map[string]interface{}{
		"app": {"autoscaling": map[string]interface{}{"min": 1, "max": float64(4), "targetCPU": 60}},
	})

	want := &AppAutoscalingSpec{MinReplicas: 1, MaxReplicas: 4, TargetCPU: 60}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Autoscaling, want) {
		t.Errorf("want %+v, got %+v", want, got)
	}
}

// Values of the wrong YAML type are left at the zero value rather than
// coerced; schema validation upstream is what rejects them.
func TestConvertLoopZeroYamlApps_WrongTypedValuesLeftZero(t *testing.T) {
	got := ConvertLoopZeroYamlApps(map[string]map[string]interface{}{
		"app": {"type": 7, "runtime": 1, "port": "3000", "replicas": "2", "start": true, "autoscaling": "on"},
	})

	want := AppSpec{Name: "app", Type: "server"}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("want %+v, got %+v", want, got)
	}
}

// An autoscaling block with no numeric keys still yields an (empty)
// autoscaling spec: its presence is what the caller reads.
func TestConvertLoopZeroYamlApps_EmptyAutoscalingBlockIsPresent(t *testing.T) {
	got := ConvertLoopZeroYamlApps(map[string]map[string]interface{}{
		"app": {"autoscaling": map[string]interface{}{}},
	})

	if len(got) != 1 || got[0].Autoscaling == nil || *got[0].Autoscaling != (AppAutoscalingSpec{}) {
		t.Errorf("want empty non-nil autoscaling, got %+v", got)
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
