// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"log/slog"
	"sort"
)

// Plan simplify-deploy-loopzerokit-only v2.0 — T3.1.
//
// Helpers that derive what the upload handler needs (project name + sorted
// app names) directly from the native *ServicesManifest, so routes/source.go
// no longer depends on the deprecated ValidateServicesJSON *LoopRuntimeYAML bridge.
// The bridge previously flattened every service to Type:"server" (C7 mislabel);
// the native manifest preserves the declared type — these helpers never touch
// per-service type, so nothing is flattened.

// ProjectNameFromManifest returns the project name for a validated manifest.
//
//   - v2: the schema requires + DNS-1123-validates the `project` field.
//   - v1: `project` is absent; fall back to the legacy "services-bundle"
//     (ADR D10 — preserves Gitea repo lineage loopruntime-source-{tenant}-services-bundle)
//     and emit a structured deprecation warning.
func ProjectNameFromManifest(m *ServicesManifest) string {
	if m == nil {
		return servicesJSONV1LegacyProject
	}
	if m.Project != "" {
		return m.Project
	}
	if m.Version == 1 {
		slog.Warn("services.json v1 missing project — applying legacy fallback (deprecated)",
			"code", "SERVICES_JSON_V1_DEPRECATED",
			"fallback_project", servicesJSONV1LegacyProject,
			"sunset", "loopzerokit 0.6.0",
			"action", "Upgrade emit to services.json v2 and set `project` from loopzero.config.ts name")
	}
	return servicesJSONV1LegacyProject
}

// AppNamesFromManifest returns the service names sorted deterministically.
func AppNamesFromManifest(m *ServicesManifest) []string {
	if m == nil {
		return nil
	}
	names := make([]string, 0, len(m.Services))
	for _, svc := range m.Services {
		names = append(names, svc.Name)
	}
	sort.Strings(names)
	return names
}
