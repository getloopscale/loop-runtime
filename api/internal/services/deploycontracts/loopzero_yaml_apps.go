// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

// ConvertLoopZeroYamlApps maps the raw `apps` block parsed from a project's
// loopzero.yaml into the canonical []AppSpec consumed by BuildOrchestrator.
//
// SINGLE SOURCE OF TRUTH for the conversion. Both webhook paths (push →
// autodeploy and pull_request → preview) MUST call this function instead
// of inlining the extraction logic.
//
// Why this exists: the conversion previously lived in two places —
// api/cmd/autodeploy_adapters.go (push path) and
// api/internal/services/preview_service.go appsFromLoopZeroYaml (preview path) —
// with a comment "mirror of autodeploy_adapters.go" admitting the
// duplication. When the canonical Dockerfile generator started requiring
// an explicit `start` command (2026-06-16), only the push path was
// updated. The preview path silently kept losing the field, so PR
// previews failed at generate-dockerfile time with "no declared start
// command" while push deploys succeeded against the same loopzero.yaml.
// Sponsor LIVE-caught 2026-06-18 against loopzerokit-example PR #1.
//
// CLAUDE.md anti-pattern: "Correct code in wrong place / mirror of X —
// single source of truth per behavior; grep duplicates before merge."
// The extraction is now centralized so any new field added flows to
// every caller automatically; the hermetic test pins the canonical
// shape to prevent silent drift from sneaking in via copy-paste.
//
// Input shape: loopzero.yaml `apps` is `map[<app-name>]map[<field>]interface{}`
// after yaml.Unmarshal into a generic map (the on-disk YAML carries
// untyped values that we extract one field at a time).
//
// Output shape: []AppSpec is the canonical struct consumed by
// BuildOrchestrator.SubmitBuild — Name + Type + Runtime + Path + Port +
// Build + Rendering + Start + Replicas + Autoscaling. Fields absent from
// the raw map fall back to AppSpec zero values (or documented defaults
// below).
//
// Defaults applied here (matching the prior push-path behavior):
//   - Type defaults to "server" when absent.
//   - Type=="frontend" with empty Rendering auto-infers "static".
//
// Defaults NOT applied here (caller responsibility):
//   - app.Name is always the map key — never extracted from the value.
//   - Schema validation (required fields, allowed values) lives upstream
//     in api/internal/source/validator_*.go for both the v1 loopzero.yaml
//     legacy path and the v2 services.json native path.
func ConvertLoopZeroYamlApps(rawApps map[string]map[string]interface{}) []AppSpec {
	out := make([]AppSpec, 0, len(rawApps))
	for name, cfg := range rawApps {
		app := AppSpec{Name: name}

		if v, ok := cfg["type"].(string); ok {
			app.Type = v
		} else {
			app.Type = "server"
		}
		if v, ok := cfg["runtime"].(string); ok {
			app.Runtime = v
		}
		if v, ok := cfg["path"].(string); ok {
			app.Path = v
		}
		if v, ok := cfg["port"].(int); ok {
			app.Port = v
		} else if v, ok := cfg["port"].(float64); ok {
			app.Port = int(v)
		}
		if v, ok := cfg["build"].(string); ok {
			app.Build = v
		}
		if v, ok := cfg["render"].(string); ok {
			app.Rendering = v
		}
		// Canonical 2026-06-16: required by canonical_dockerfile.go.
		if v, ok := cfg["start"].(string); ok {
			app.Start = v
		}
		if app.Type == "frontend" && app.Rendering == "" {
			app.Rendering = "static"
		}
		if v, ok := cfg["replicas"].(int); ok {
			app.Replicas = v
		} else if v, ok := cfg["replicas"].(float64); ok {
			app.Replicas = int(v)
		}
		if asRaw, ok := cfg["autoscaling"].(map[string]interface{}); ok {
			as := &AppAutoscalingSpec{}
			if v, ok := asRaw["min"].(int); ok {
				as.MinReplicas = v
			} else if v, ok := asRaw["min"].(float64); ok {
				as.MinReplicas = int(v)
			}
			if v, ok := asRaw["max"].(int); ok {
				as.MaxReplicas = v
			} else if v, ok := asRaw["max"].(float64); ok {
				as.MaxReplicas = int(v)
			}
			if v, ok := asRaw["targetCPU"].(int); ok {
				as.TargetCPU = v
			} else if v, ok := asRaw["targetCPU"].(float64); ok {
				as.TargetCPU = int(v)
			}
			app.Autoscaling = as
		}

		out = append(out, app)
	}
	return out
}
