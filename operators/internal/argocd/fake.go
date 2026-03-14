package argocd

import (
	"context"
	"sync"
)

// FakeProvisioner is a test double for ArgoCD Provisioner.
// It tracks created applications and can be configured to return errors.
type FakeProvisioner struct {
	Mu           sync.Mutex
	Applications map[string]ApplicationSpec
	EnsureErr    error
	DeleteErr    error
	EnsureCalls  int
	DeleteCalls  int
}

// NewFakeProvisioner creates a new FakeProvisioner with empty application state.
func NewFakeProvisioner() *FakeProvisioner {
	return &FakeProvisioner{
		Applications: make(map[string]ApplicationSpec),
	}
}

// EnsureApplication stores the application spec in the fake's internal map.
// Returns EnsureErr if configured.
func (f *FakeProvisioner) EnsureApplication(_ context.Context, spec ApplicationSpec) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.EnsureCalls++
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	f.Applications[spec.Name] = spec
	return nil
}

// DeleteApplications removes all applications matching the given tenantID.
// Returns DeleteErr if configured.
func (f *FakeProvisioner) DeleteApplications(_ context.Context, tenantID string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.DeleteCalls++
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	for name, app := range f.Applications {
		if app.TenantID == tenantID {
			delete(f.Applications, name)
		}
	}
	return nil
}
