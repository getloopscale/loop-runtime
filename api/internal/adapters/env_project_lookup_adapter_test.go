// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"testing"
)

// The environment routes use this lookup as the EC-1 cross-tenant gate: a
// project that does not belong to the caller's tenant must not be reachable.
// An unwired lookup cannot answer that question, so it must fail closed with an
// error instead of claiming every project exists.
func TestEnvProjectLookup_UnwiredLookupFailsClosed(t *testing.T) {
	for name, a := range map[string]*EnvProjectLookupAdapter{
		"nil adapter": nil,
		"nil store":   {},
	} {
		exists, err := a.ProjectExistsForTenant(context.Background(), "tn-1", "web")
		if err == nil || exists {
			t.Errorf("%s: got (exists=%v, err=%v), want (false, error)", name, exists, err)
		}
	}
}
