// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deploycontracts

// ConvertLoopRuntimeYamlApps maps the raw `apps` block parsed from a project's
// loopzero.yaml into the canonical []AppSpec consumed by BuildOrchestrator.
//
// SINGLE SOURCE OF TRUTH for the conversion. Both webhook paths (push →
// autodeploy and pull_request → preview) MUST call this function instead
// of inlining the extraction logic.
//
// Why this exists: the conversion previously lived in two places —
// api/cmd/autodeploy_adapters.go (push path) and
// api/internal/services/preview_service.go appsFromLoopRuntimeYaml (preview path) —
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
//   - Type defaults to "server" when absent (or not a string).
//
// The type is carried verbatim: whether it is allowed is decided once, by
// NormalizeApp (server|worker only). No rendering is inferred from it.
//
// Defaults NOT applied here (caller responsibility):
//   - app.Name is always the map key — never extracted from the value.
//   - Schema validation (required fields, allowed values) lives upstream
//     in api/internal/source/validator_*.go for both the v1 loopzero.yaml
//     legacy path and the v2 services.json native path.
func ConvertLoopRuntimeYamlApps(rawApps map[string]map[string]interface{}) []AppSpec {
	out := make([]AppSpec, 0, len(rawApps))
	for name, cfg := range rawApps {
		app := AppSpec{
			Name:      name,
			Type:      stringField(cfg, "type"),
			Runtime:   stringField(cfg, "runtime"),
			Path:      stringField(cfg, "path"),
			Port:      numberField(cfg, "port"),
			Build:     stringField(cfg, "build"),
			Rendering: stringField(cfg, "render"),
			// Canonical 2026-06-16: required by canonical_dockerfile.go.
			Start:    stringField(cfg, "start"),
			Replicas: numberField(cfg, "replicas"),
		}
		if app.Type == "" {
			app.Type = "server"
		}
		if asRaw, ok := cfg["autoscaling"].(map[string]interface{}); ok {
			app.Autoscaling = &AppAutoscalingSpec{
				MinReplicas: numberField(asRaw, "min"),
				MaxReplicas: numberField(asRaw, "max"),
				TargetCPU:   numberField(asRaw, "targetCPU"),
			}
		}
		out = append(out, app)
	}
	return out
}

// stringField returns cfg[key] when it is a string, "" otherwise.
func stringField(cfg map[string]interface{}, key string) string {
	v, _ := cfg[key].(string)
	return v
}

// numberField returns cfg[key] as an int. YAML decodes small integers as int
// and a JSON round-trip yields float64, so both are accepted; any other type
// yields 0 and is left for upstream schema validation to reject.
func numberField(cfg map[string]interface{}, key string) int {
	switch v := cfg[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
