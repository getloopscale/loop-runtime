// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import "strings"

// deriveAction maps HTTP method + path to a human-readable audit action.
func deriveAction(method, path string) string {
	segment := extractSegment(path)
	switch method {
	case "POST":
		switch segment {
		case "projects":
			return "project.created"
		case "builds":
			if strings.Contains(path, "/cancel") {
				return "build.cancelled"
			}
			return "build.submitted"
		case "environments":
			return "environment.created"
		case "members":
			return "member.added"
		case "secrets":
			return "secret.set"
		case "rollback":
			return "rollback.executed"
		case "checkout":
			return "billing.checkout_started"
		}
	case "DELETE":
		switch segment {
		case "projects":
			return "project.deleted"
		case "environments":
			return "environment.deleted"
		case "members":
			return "member.removed"
		case "secrets":
			return "secret.deleted"
		}
	case "PATCH":
		if segment == "members" {
			return "member.role_changed"
		}
	case "PUT":
		return segment + ".updated"
	}
	return method + "." + segment
}

// extractResourceType returns the primary resource type from a URL path.
func extractResourceType(path string) string {
	segment := extractSegment(path)
	switch segment {
	case "projects":
		return "project"
	case "builds":
		return "build"
	case "environments":
		return "environment"
	case "members":
		return "member"
	case "secrets":
		return "secret"
	case "rollback":
		return "release"
	case "checkout", "billing", "portal":
		return "billing"
	default:
		return segment
	}
}

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
		if p == "projects" || p == "secrets" || p == "environments" || p == "builds" {
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

// extractSegment returns the last meaningful URL segment for action/resource mapping.
func extractSegment(path string) string {
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/"), "/")
	// Walk backwards to find the first non-ID segment
	// /api/v1/projects/my-app → "projects"
	// /api/v1/builds/123/cancel → "cancel"
	// /api/v1/teams/members → "members"
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		// Skip likely ID segments (UUIDs, hex IDs, names with hyphens that look like IDs)
		if p == "" || p == "api" || p == "v1" || p == "internal" {
			continue
		}
		// Return the first meaningful segment from the end
		return p
	}
	return "unknown"
}
