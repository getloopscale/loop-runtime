// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"log/slog"
	"strings"
)

// deriveAction maps HTTP method + path to a human-readable audit action.
// CHG-2026-04-23-AUDIT-17: domain branches added to the POST/DELETE switches
// so audit entries follow the repo-wide resource.verb_past convention
// (previously fell through to "DELETE.domains" / "POST.domains").
// CHG-2026-04-23-AUDIT-18: source.uploaded branch + fail-loud slog.Warn
// on unknown fallthroughs so dashboards can filter action LIKE 'unknown.%'.
// CHG-2026-04-23-AUDIT-19: promote / connect / disconnect branches.
// These sub-resource verbs were silently recorded as "project.created"
// / "project.deleted" (the parent collection's CRUD action), corrupting
// audit forensics. Each is now a typed action on its true resource
// (release / repository).
func deriveAction(method, path string) string {
	segment := extractSegment(path)
	switch method {
	case "POST":
		switch segment {
		case "projects":
			return "project.created"
		case "builds":
			return "build.submitted"
		case "cancel":
			if strings.Contains(path, "/builds/") {
				return "build.cancelled"
			}
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
		case "domains":
			return "domain.added"
		case "verify":
			if strings.Contains(path, "/domains/") {
				return "domain.verified"
			}
		case "upload":
			if strings.Contains(path, "/source/") {
				return "source.uploaded"
			}
		case "promote":
			if strings.Contains(path, "/projects/") {
				return "release.promoted"
			}
		case "connect":
			if strings.Contains(path, "/projects/") {
				return "repository.connected"
			}
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
		case "domains":
			return "domain.removed"
		case "connect":
			if strings.Contains(path, "/projects/") {
				return "repository.disconnected"
			}
		}
	case "PATCH":
		if segment == "members" {
			return "member.role_changed"
		}
	case "PUT":
		return segment + ".updated"
	}
	// AUDIT-18: fail-loud. If we reached here, a new endpoint was added
	// without a branch — log it so operators notice and filter in dashboards.
	slog.Warn("audit: unmapped action (add a branch in deriveAction)",
		"method", method, "path", path, "segment", segment)
	return "unknown." + method + "." + segment
}

// extractResourceType returns the primary resource type from a URL path.
// CHG-2026-04-23-AUDIT-17: domain added.
// CHG-2026-04-23-AUDIT-18: source added (for /source/upload audit rows).
// CHG-2026-04-23-AUDIT-19: promote → release, connect → repository.
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
	case "rollback", "promote":
		return "release"
	case "domains", "verify":
		return "domain"
	case "checkout", "billing", "portal":
		return "billing"
	case "source", "upload":
		return "source"
	case "connect":
		return "repository"
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

// knownCollections is the set of URL path segments that represent resource
// collections. extractSegment uses this to distinguish collection names from
// resource identifiers when walking a URL path backwards.
var knownCollections = map[string]bool{
	"projects":     true,
	"builds":       true,
	"environments": true,
	"members":      true,
	"secrets":      true,
	"rollback":     true,
	"checkout":     true,
	"billing":      true,
	"portal":       true,
	"teams":        true,
	"domains":      true,
	"cancel":       true,
	"stream":       true,
	"callback":     true,
	"status":       true,
	"audit":        true,
	"verify":       true,
	"source":       true,
	"upload":       true,
	"promote":      true,
	"connect":      true,
}

// extractSegment returns the last meaningful URL segment for action/resource mapping.
// It walks backwards through the path, skipping segments that look like resource IDs,
// and returns the first known collection or action segment it finds.
//
//	/api/v1/projects/my-app → "projects"
//	/api/v1/builds/123/cancel → "cancel"
//	/api/v1/teams/members → "members"
func extractSegment(path string) string {
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/"), "/")
	// Walk backwards to find the first known collection/action segment
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p == "" || p == "api" || p == "v1" || p == "internal" {
			continue
		}
		if knownCollections[p] {
			return p
		}
	}
	// Fallback: return the last non-infrastructure segment
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p == "" || p == "api" || p == "v1" || p == "internal" {
			continue
		}
		return p
	}
	return "unknown"
}
