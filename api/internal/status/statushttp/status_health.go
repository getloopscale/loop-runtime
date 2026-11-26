// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statushttp

import (
	"context"
	"log/slog"
	"time"
)

// StatusMetricsSource reads the live numbers of one environment of a project.
// A nil value with a nil error means no series exists yet.
type StatusMetricsSource interface {
	// ReadyReplicas is the number of ready pods over the named apps of the
	// environment, or over every app there when apps is empty.
	ReadyReplicas(ctx context.Context, tenantID, project, environment string, apps []string) (*int, error)
	// ServerErrorPercent is the share of requests answered 5xx over window,
	// from 0 to 100. Without traffic there is no share.
	ServerErrorPercent(ctx context.Context, tenantID, project, environment string, window time.Duration) (*float64, error)
}

// StatusAppScale is the scale one app of an environment is configured to run.
type StatusAppScale struct {
	App string
	// Replicas is the count the app's workload is rendered with.
	Replicas int
	// Min and Max are the autoscaling bounds, or both equal to Replicas when
	// the scale is fixed by a saved configuration. Both are zero for a fixed
	// scale that came from the build.
	Min, Max int
	// TargetCPU and TargetMemory are the utilization targets of an autoscaled
	// app, in percent. Zero when the app has no such target.
	TargetCPU, TargetMemory int
	// Saved is true when the scale comes from the saved scale configuration
	// and false when it is what the build rendered.
	Saved bool
}

// StatusScaleReader reports the scale of each app the build of an
// environment's active release declares, with the saved scale configuration
// applied over it.
type StatusScaleReader interface {
	DesiredScale(ctx context.Context, tenantID, projectID, environment, buildID string) ([]StatusAppScale, error)
}

// SetMetricsSource wires the source of replicas_ready and traffic_5xx_pct_1h.
// Without one, both fields are omitted.
func (h *StatusHandler) SetMetricsSource(m StatusMetricsSource) { h.metrics = m }

// SetScaleReader wires the source of replicas_desired and of the scaling
// block. Without one, replicas_desired is omitted and the scaling block is
// read from the last build.
func (h *StatusHandler) SetScaleReader(s StatusScaleReader) { h.scale = s }

// healthQueryTimeout bounds the reads behind the health fields, so a slow
// Prometheus delays a status answer by at most this long.
const healthQueryTimeout = 3 * time.Second

// serverErrorWindow is the window of traffic_5xx_pct_1h.
const serverErrorWindow = time.Hour

// deployStatusOf translates a release status to the build status vocabulary
// the console reads in deploy_status. The handler only sees ACTIVE releases,
// which are deploys that succeeded; any other status has no translation.
func deployStatusOf(releaseStatus string) *string {
	if releaseStatus != "ACTIVE" {
		return nil
	}
	succeeded := "succeeded"
	return &succeeded
}

// fillHealth sets the health fields of an environment that has an active
// release, and reports the scale of its apps (nil when it could not be read).
// Each field is left nil when its source is not wired, has no value or fails:
// a failure is logged and never fails the status answer.
func (h *StatusHandler) fillHealth(ctx context.Context, resp *statusResponse, tenantID, project, env string, release *StatusRelease) []StatusAppScale {
	resp.DeployStatus = deployStatusOf(release.Status)
	became := release.DeployedAt
	if became.IsZero() {
		became = release.CreatedAt
	}
	deployedAt := became.UTC().Format(time.RFC3339)
	resp.LastDeployAt = &deployedAt

	ctx, cancel := context.WithTimeout(ctx, healthQueryTimeout)
	defer cancel()
	log := slog.With("tenant_id", tenantID, "project", project, "environment", env)

	var apps []StatusAppScale
	if h.scale != nil {
		read, err := h.scale.DesiredScale(ctx, tenantID, project, env, release.BuildID)
		if err != nil {
			log.WarnContext(ctx, "status: desired scale not read; replicas_desired omitted", "error", err)
		} else if len(read) > 0 {
			apps = read
			desired := 0
			for _, app := range apps {
				desired += app.Replicas
			}
			resp.ReplicasDesired = &desired
		}
	}

	if h.metrics != nil {
		// The apps of the active release: one a deploy or a rollback removed
		// keeps its last sample in Prometheus for minutes and must not count.
		released := make([]string, 0, len(apps))
		for _, app := range apps {
			released = append(released, app.App)
		}
		ready, err := h.metrics.ReadyReplicas(ctx, tenantID, project, env, released)
		if err != nil {
			log.WarnContext(ctx, "status: ready replicas not read; replicas_ready omitted", "error", err)
		} else {
			resp.ReplicasReady = ready
		}
		pct, err := h.metrics.ServerErrorPercent(ctx, tenantID, project, env, serverErrorWindow)
		if err != nil {
			log.WarnContext(ctx, "status: 5xx share not read; traffic_5xx_pct_1h omitted", "error", err)
		} else {
			resp.Traffic5xxPct1h = pct
		}
	}
	return apps
}

// scalingOfApps is the scaling block for the apps of the active release: the
// first app with a saved scale configuration, or else the first app that
// autoscales. It is nil when no app has either, as for a build without
// autoscaling.
func scalingOfApps(apps []StatusAppScale) *scalingStatusResponse {
	pick := -1
	for i, app := range apps {
		if app.Saved {
			pick = i
			break
		}
		if pick < 0 && app.Max > 0 {
			pick = i
		}
	}
	if pick < 0 {
		return nil
	}
	app := apps[pick]
	return &scalingStatusResponse{
		Replicas:     app.Replicas,
		Min:          app.Min,
		Max:          app.Max,
		TargetCPU:    app.TargetCPU,
		TargetMemory: app.TargetMemory,
		ScaledToZero: app.Min == 0 && app.Max > 0,
	}
}
