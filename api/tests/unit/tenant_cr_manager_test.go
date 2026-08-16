// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/api/internal/routes/environments"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/k8s"
)

func TestKubeTenantCRManagerImplementsInterface(t *testing.T) {
	// Compile-time check: TenantCRManager implements routes.TenantCRManager.
	var _ domain.TenantCRManager = (*k8s.TenantCRManager)(nil)
}

func TestKubeTenantCRManagerImplementsEnvironmentManager(t *testing.T) {
	// Compile-time check: TenantCRManager implements routes.EnvironmentManager.
	var _ environments.EnvironmentManager = (*k8s.TenantCRManager)(nil)
}
