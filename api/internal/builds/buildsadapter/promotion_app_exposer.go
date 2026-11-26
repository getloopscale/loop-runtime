// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/builds"
	"github.com/getloopscale/loop-runtime/api/internal/release"
)

// PromotionAppExposer exposes a promoted app through the port the deploy path
// uses for the apps of a build, so both end with the same application resource.
type PromotionAppExposer struct {
	Ensurer builds.ApplicationCREnsurer
}

func (a *PromotionAppExposer) ExposeApp(ctx context.Context, in release.AppExposure) error {
	return a.Ensurer.EnsureLoopZeroApplication(ctx, builds.ApplicationCRInput{
		TenantID:    in.TenantID,
		Project:     in.Project,
		Environment: in.Environment,
		AppName:     in.AppName,
		ServiceName: in.AppName,
		ServicePort: in.ServicePort,
	})
}

var _ release.PromotionAppExposer = (*PromotionAppExposer)(nil)
