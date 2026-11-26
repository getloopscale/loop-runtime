// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package audit

import "strings"

// extractResourceID extracts the resource identifier from the URL path.
// For /api/v1/projects/my-app → "my-app"
// For /api/v1/teams/members/paulo → "paulo"
func extractResourceID(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// Find the segment after the resource type
	// /api/v1/projects/{name} → name at index 3
	// /api/v1/teams/members/{username} → username at index 4
	// /api/v1/secrets/{name} → name at index 3
	for i, p := range parts {
		if p == "projects" || p == "secrets" || p == "environments" || p == "builds" || p == "domains" {
			if i+1 < len(parts) && parts[i+1] != "" {
				return parts[i+1]
			}
		}
		if p == "members" {
			if i+1 < len(parts) && parts[i+1] != "" {
				return parts[i+1]
			}
		}
	}
	return ""
}
