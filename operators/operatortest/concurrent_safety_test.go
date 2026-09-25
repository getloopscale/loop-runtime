// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// M-08: Concurrent reconciliation safety tests.
//
// MaxConcurrentReconciles=5 is the production setting. These tests verify that
// fake provisioners (which mirror the real ones' sync.Mutex patterns) are safe
// under concurrent access. Run with `go test -race` for full verification.

package operatortest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/operators/internal/argocd"
)

func TestFakeGitProvisioner_ConcurrentAccess(t *testing.T) {
	prov := NewFakeGitProvisioner()
	ctx := context.Background()

	const goroutines = 10
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				repoName := fmt.Sprintf("repo-%d-%d", id, j)
				_, _ = prov.EnsureRepo(ctx, repoName, "org")
				_ = prov.ArchiveRepo(ctx, repoName)
			}
		}(i)
	}
	wg.Wait()

	// Verify no panic and calls were counted
	if prov.GetCalls() != goroutines*opsPerGoroutine {
		t.Errorf("expected %d EnsureRepo calls, got %d", goroutines*opsPerGoroutine, prov.GetCalls())
	}
}

func TestFakeArgoCDProvisioner_ConcurrentAccess(t *testing.T) {
	prov := NewFakeArgoCDProvisioner()
	ctx := context.Background()

	const goroutines = 10
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				name := fmt.Sprintf("app-%d-%d", id, j)
				_ = prov.EnsureApplication(ctx, argocd.ApplicationSpec{
					Name:     name,
					TenantID: fmt.Sprintf("tenant-%d", id),
				})
				_ = prov.EnsureAppProject(ctx, argocd.AppProjectSpec{
					Name: name,
				})
				_ = prov.DeleteApplications(ctx, fmt.Sprintf("tenant-%d", id))
				_ = prov.DeleteAppProject(ctx, fmt.Sprintf("tenant-%d", id))
			}
		}(i)
	}
	wg.Wait()

	// Verify no panic — concurrent deletes + creates should not race
	prov.Mu.Lock()
	totalEnsure := prov.EnsureCalls
	totalDelete := prov.DeleteCalls
	prov.Mu.Unlock()

	if totalEnsure != goroutines*opsPerGoroutine {
		t.Errorf("expected %d EnsureApplication calls, got %d", goroutines*opsPerGoroutine, totalEnsure)
	}
	if totalDelete != goroutines*opsPerGoroutine {
		t.Errorf("expected %d DeleteApplications calls, got %d", goroutines*opsPerGoroutine, totalDelete)
	}
}

func TestFakeMinIOProvisioner_ConcurrentAccess(t *testing.T) {
	prov := NewFakeMinIOProvisioner()
	ctx := context.Background()

	const goroutines = 10
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				tenantID := fmt.Sprintf("tenant-%d-%d", id, j)
				_, _, _ = prov.EnsureTenantCredentials(ctx, tenantID)
				_ = prov.DeleteTenantCredentials(ctx, tenantID)
			}
		}(i)
	}
	wg.Wait()

	// Verify no panic and calls counted (use thread-safe getters)
	totalEnsure := prov.GetEnsureCalls()
	totalDelete := prov.GetDeleteCalls()

	if totalEnsure != goroutines*opsPerGoroutine {
		t.Errorf("expected %d EnsureTenantCredentials calls, got %d", goroutines*opsPerGoroutine, totalEnsure)
	}
	if totalDelete != goroutines*opsPerGoroutine {
		t.Errorf("expected %d DeleteTenantCredentials calls, got %d", goroutines*opsPerGoroutine, totalDelete)
	}
}
