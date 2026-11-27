// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
)

// ---------------------------------------------------------------------------
// DAG task factories
// ---------------------------------------------------------------------------

// cwtTask creates a DAG task that references a ClusterWorkflowTemplate.
func cwtTask(taskName, cwtName, templateName string, deps []string, params map[string]string) *forge.Task {
	return &forge.Task{
		Name: taskName,
		TemplateRef: &forge.TemplateRef{
			Name:         cwtName,
			Template:     templateName,
			ClusterScope: true,
		},
		Dependencies: deps,
		Arguments:    paramsToForge(params),
	}
}

// inlineTask creates a DAG task that references an inline template by name.
func inlineTask(taskName, templateName string, deps []string) *forge.Task {
	return &forge.Task{
		Name:         taskName,
		Template:     templateName,
		Dependencies: deps,
	}
}

// ---------------------------------------------------------------------------
// Forge type converters
// ---------------------------------------------------------------------------

// paramsToForge converts a string map to forge Parameters for task arguments.
// Keys are sorted for deterministic output (stable YAML across builds).
func paramsToForge(params map[string]string) []forge.Parameter {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]forge.Parameter, 0, len(params))
	for _, k := range keys {
		val := params[k]
		result = append(result, forge.Parameter{Name: k, Value: &val})
	}
	return result
}

// withSuffix appends a language suffix to a container template name.
// For single-language projects (suffix=""), the name is unchanged.
// For mixed-language (suffix="-node"), name becomes e.g. "install-deps-node".
func withSuffix(c *forge.Container, suffix string) *forge.Container {
	if suffix != "" {
		c.Name = c.Name + suffix
	}
	return c
}

// appsJSON serializes apps to JSON for passing as a workflow parameter.
// Uses json.Marshal for correct escaping of special characters.
// CODE-009 fix: log error instead of swallowing silently.
func appsJSON(apps []deploycontracts.AppSpec) string {
	if len(apps) == 0 {
		return "[]"
	}
	data, err := json.Marshal(apps)
	if err != nil {
		slog.Error("failed to marshal apps to JSON — falling back to empty array",
			"error", err, "app_count", len(apps))
		return "[]"
	}
	return string(data)
}

// suffixForStrategy returns the language suffix for a given strategy name
// by looking it up in the pre-built language groups. Used to connect per-app
// delivery tasks to their language group's pipeline step.
func suffixForStrategy(groups []languageGroup, strategyName string) string {
	for _, g := range groups {
		if g.Strategy.Name() == strategyName {
			return g.Suffix
		}
	}
	return ""
}
