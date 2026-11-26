// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// The request pool is the only one that serves a caller, so it is the only one
// that scopes its connections to the caller's tenant; without that, every
// store it backs reads nothing under fail-closed Row Level Security.
func TestTheRequestPoolScopesConnectionsToTheCallersTenant(t *testing.T) {
	if !requestPoolConfig(config.Config{}).TenantScoped {
		t.Fatal("the request pool must scope its connections to the caller's tenant")
	}
}

// The background pool crosses tenants through its role's named policies; a
// tenant scope would add nothing and has no caller to come from.
func TestTheBackgroundPoolIsNotTenantScoped(t *testing.T) {
	if backgroundPoolConfig(config.Config{}).TenantScoped {
		t.Fatal("the background pool must not scope its connections to a tenant")
	}
}
