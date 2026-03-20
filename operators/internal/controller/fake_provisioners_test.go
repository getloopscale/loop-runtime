package controller_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/loopzero/loopzero/operators/internal/argocd"
)

// fakeGiteaProvisioner is a test double for gitea.GitRepoProvisioner.
// Lives in _test.go only — never in production packages.
// All field access must go through thread-safe methods because the
// reconciler goroutine calls EnsureRepo/RepoExists/ArchiveRepo concurrently.
type fakeGiteaProvisioner struct {
	mu    sync.Mutex
	repos map[string]string // name → URL
	err   error             // if set, all calls return this error
	calls int               // number of EnsureRepo calls
}

func newFakeGiteaProvisioner() *fakeGiteaProvisioner {
	return &fakeGiteaProvisioner{
		repos: make(map[string]string),
	}
}

func (f *fakeGiteaProvisioner) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeGiteaProvisioner) GetCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeGiteaProvisioner) EnsureRepo(_ context.Context, name string, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.err != nil {
		return "", f.err
	}

	if url, ok := f.repos[name]; ok {
		return url, nil
	}

	url := fmt.Sprintf("http://gitea.loopzero-system.svc:3000/loopzero/%s.git", name)
	f.repos[name] = url
	return url, nil
}

func (f *fakeGiteaProvisioner) RepoExists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return false, f.err
	}

	_, ok := f.repos[name]
	return ok, nil
}

func (f *fakeGiteaProvisioner) ArchiveRepo(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	delete(f.repos, name)
	return nil
}

// fakeArgoCDProvisioner is a test double for argocd.Provisioner.
// Lives in _test.go only — never in production packages.
type fakeArgoCDProvisioner struct {
	Mu           sync.Mutex
	Applications map[string]argocd.ApplicationSpec
	AppProjects  map[string]argocd.AppProjectSpec
	EnsureErr    error
	DeleteErr    error
	EnsureCalls  int
	DeleteCalls  int
	ProjectCalls int
}

func newFakeArgoCDProvisioner() *fakeArgoCDProvisioner {
	return &fakeArgoCDProvisioner{
		Applications: make(map[string]argocd.ApplicationSpec),
		AppProjects:  make(map[string]argocd.AppProjectSpec),
	}
}

func (f *fakeArgoCDProvisioner) EnsureAppProject(_ context.Context, spec argocd.AppProjectSpec) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.ProjectCalls++
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	f.AppProjects[spec.Name] = spec
	return nil
}

func (f *fakeArgoCDProvisioner) EnsureApplication(_ context.Context, spec argocd.ApplicationSpec) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.EnsureCalls++
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	f.Applications[spec.Name] = spec
	return nil
}

func (f *fakeArgoCDProvisioner) DeleteApplications(_ context.Context, tenantID string) error {
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

func (f *fakeArgoCDProvisioner) DeleteAppProject(_ context.Context, tenantID string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	delete(f.AppProjects, tenantID)
	return nil
}
