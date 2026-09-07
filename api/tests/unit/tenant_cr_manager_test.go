// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routes/environments"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/k8s/tenancy"
)

func TestKubeTenantCRManagerImplementsInterface(t *testing.T) {
	// Compile-time check: TenantCRManager implements routes.TenantCRManager.
	var _ ports.TenantCRManager = (*tenancy.TenantCRManager)(nil)
}

func TestKubeTenantCRManagerImplementsEnvironmentManager(t *testing.T) {
	// Compile-time check: TenantCRManager implements routes.EnvironmentManager.
	var _ environments.EnvironmentManager = (*tenancy.TenantCRManager)(nil)
}
