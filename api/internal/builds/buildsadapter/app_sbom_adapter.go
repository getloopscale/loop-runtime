// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/builds/buildshttp"
	"github.com/getloopscale/loopzero/api/internal/db/store"
)

var _ buildshttp.AppSBOMStore = (*AppSBOMStore)(nil)

// AppSBOMStore maps the per-app attestation rows onto the route's
// AppSBOMInfo, keeping the builds route decoupled from db types.
type AppSBOMStore struct {
	Inner store.AppAttestationStore
}

// GetBuildAppSBOMs returns the SBOM metadata of each app of a tenant's build.
func (a *AppSBOMStore) GetBuildAppSBOMs(ctx context.Context, tenantID, buildID string) ([]buildshttp.AppSBOMInfo, error) {
	infos, err := a.Inner.GetBuildAppAttestations(ctx, tenantID, buildID)
	if err != nil {
		return nil, err
	}
	out := make([]buildshttp.AppSBOMInfo, 0, len(infos))
	for _, in := range infos {
		out = append(out, buildshttp.AppSBOMInfo{
			AppName:            in.AppName,
			SBOMS3Key:          in.SBOMS3Key,
			SBOMFormat:         in.SBOMFormat,
			SBOMComponentCount: in.SBOMComponentCount,
			SBOMGeneratedAt:    in.SBOMGeneratedAt,
			Signed:             in.Signed,
		})
	}
	return out, nil
}
