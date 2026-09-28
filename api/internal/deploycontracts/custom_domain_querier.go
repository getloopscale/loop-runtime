// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// CustomDomainQuerier queries active custom domains for manifest rendering.
// MEET-2026-04-06-034 Block 0: both deploy and domain provisioner must include custom domains.
type CustomDomainQuerier interface {
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]ports.CustomDomain, error)
}
