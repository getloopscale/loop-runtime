// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildshttp

import "context"

// ReleaseStatusLookup reads the status of the latest release each build has in
// its own environment. A build with no release there is absent from the result.
type ReleaseStatusLookup interface {
	LatestReleaseStatuses(ctx context.Context, tenantID string, buildIDs []string) (map[string]string, error)
}

// DeployStatusOf turns a release status into the `deploy_status` of the build
// that release came from. The build status says how the build ended; this says
// how its deploy did, and the two differ for an image that was built and then
// failed to roll out.
//
// It answers "" for no release and for a release status it does not know, and
// the field is then left out: an outcome is reported or absent, never guessed.
func DeployStatusOf(releaseStatus string) string {
	switch releaseStatus {
	case "READY", "PENDING":
		return "in_progress"
	case "ACTIVE", "DRIFT_ALERT":
		return "succeeded"
	case "SYNC_FAILED", "GIT_MIRROR_FAILED":
		return "failed"
	case "SUPERSEDED":
		return "superseded"
	case "ROLLING_BACK":
		return "rolling_back"
	case "ROLLED_BACK":
		return "rolled_back"
	case "ROLLBACK_FAILED":
		return "rollback_failed"
	case "CANCELLED":
		return "cancelled"
	case "ARCHIVED":
		return "archived"
	}
	return ""
}

// WithReleaseStatuses makes the list carry `deploy_status` for each build.
func (h *BuildListHandler) WithReleaseStatuses(l ReleaseStatusLookup) *BuildListHandler {
	h.releases = l
	return h
}

// WithReleaseStatuses makes GET /builds/{id} carry `deploy_status`.
func (h *BuildHandler) WithReleaseStatuses(l ReleaseStatusLookup) *BuildHandler {
	h.releases = l
	return h
}
