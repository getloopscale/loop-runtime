// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentshttp

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/k8s/tenancy"
)

// The handler's EnvironmentManager port is satisfied in production by the
// Tenant CR manager. The assertion lives on the consumer's side: the kernel
// package that implements the port does not import the feature that declares it.
func TestTenantCRManager_ImplementsEnvironmentManager(t *testing.T) {
	var _ EnvironmentManager = (*tenancy.TenantCRManager)(nil)
}
