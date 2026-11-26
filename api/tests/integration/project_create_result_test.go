// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// POST /api/v1/projects answers with the store's result as-is, and the store returned
// neither the row's created_at nor created_by: the response carried
// "created_at":"0001-01-01T00:00:00Z" while GET showed the stored time — measured on
// a cold cleanroom (#411). Both insert paths must return what they wrote.
func TestCreateProject_ReturnsTheStoredCreationTimeAndCreator(t *testing.T) {
	for _, tc := range []struct{ plan, path string }{
		{"starter", "cap-enforced insert"},
		{"enterprise", "unlimited insert"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			store, std := projectCapStore(t)
			owner, acct := "cr-"+tc.plan+"-uid", "acct-cr-"+tc.plan
			tn1, tn2 := "tn-cr-"+tc.plan+"-a", "tn-cr-"+tc.plan+"-b"
			projectCapSeed(t, std, owner, acct, tn1, tn2, tc.plan)
			t.Cleanup(func() { projectCapCleanup(t, std, owner) })
			before := time.Now().Add(-time.Minute)

			got, err := store.CreateProject(asTenant(tn1), ports.CreateProjectInput{TenantID: tn1, Name: "created", CreatedBy: owner})

			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if got.CreatedAt.Before(before) {
				t.Errorf("CreatedAt = %v, want the stored creation time", got.CreatedAt)
			}
			if got.CreatedBy != owner {
				t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, owner)
			}
		})
	}
}
