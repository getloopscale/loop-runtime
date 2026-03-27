// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package operatortest exports operator setup functions for integration and E2E testing.
//
// This package provides a public facade over operators/internal/controller and
// operators/internal/gitea so that external test modules can start an in-process
// TenantController against envtest without importing internal packages.
//
// Fake provisioners live here (test-only package), NOT in production packages.
package operatortest

import (
	"context"
	"fmt"
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/controller"
	"github.com/loopzero/loopzero/operators/internal/gitea"
)

// GitRepoProvisioner is the interface for Git repository provisioning.
type GitRepoProvisioner = gitea.GitRepoProvisioner

// ArgoCDProvisioner is the interface for ArgoCD Application provisioning.
type ArgoCDProvisioner = argocd.Provisioner

// FakeGitProvisioner is a test double for GitRepoProvisioner.
// Tracks created repos in memory and can be configured to return errors.
type FakeGitProvisioner struct {
	mu    sync.Mutex
	repos map[string]string // name → URL
	Err   error             // if set, all calls return this error
	Calls int               // number of EnsureRepo calls
}

// NewFakeGitProvisioner returns a fake GitRepoProvisioner for testing.
func NewFakeGitProvisioner() *FakeGitProvisioner {
	return &FakeGitProvisioner{
		repos: make(map[string]string),
	}
}

// SetErr configures the fake to return the given error on all calls.
func (f *FakeGitProvisioner) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Err = err
}

// Reset clears all state in the fake provisioner, restoring it to initial state.
// Call this between tests to ensure isolation.
func (f *FakeGitProvisioner) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos = make(map[string]string)
	f.Err = nil
	f.Calls = 0
}

// GetCalls returns the number of EnsureRepo calls (thread-safe).
func (f *FakeGitProvisioner) GetCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls
}

func (f *FakeGitProvisioner) EnsureRepo(_ context.Context, name string, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Err != nil {
		return "", f.Err
	}
	if url, ok := f.repos[name]; ok {
		return url, nil
	}
	url := fmt.Sprintf("http://gitea.loopzero-system.svc:3000/loopzero/%s.git", name)
	f.repos[name] = url
	return url, nil
}

func (f *FakeGitProvisioner) RepoExists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return false, f.Err
	}
	_, ok := f.repos[name]
	return ok, nil
}

func (f *FakeGitProvisioner) ArchiveRepo(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	delete(f.repos, name)
	return nil
}

// FakeArgoCDProvisioner is a test double for argocd.Provisioner.
// Tracks created applications and app projects in memory.
type FakeArgoCDProvisioner struct {
	Mu           sync.Mutex
	Applications map[string]argocd.ApplicationSpec
	AppProjects  map[string]argocd.AppProjectSpec
	EnsureErr    error
	DeleteErr    error
	EnsureCalls  int
	DeleteCalls  int
	ProjectCalls int // number of EnsureAppProject calls
}

// NewFakeArgoCDProvisioner returns a fake ArgoCD Provisioner for testing.
func NewFakeArgoCDProvisioner() *FakeArgoCDProvisioner {
	return &FakeArgoCDProvisioner{
		Applications: make(map[string]argocd.ApplicationSpec),
		AppProjects:  make(map[string]argocd.AppProjectSpec),
	}
}

// Reset clears all state in the fake provisioner, restoring it to initial state.
// Call this between tests to ensure isolation.
func (f *FakeArgoCDProvisioner) Reset() {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Applications = make(map[string]argocd.ApplicationSpec)
	f.AppProjects = make(map[string]argocd.AppProjectSpec)
	f.EnsureErr = nil
	f.DeleteErr = nil
	f.EnsureCalls = 0
	f.DeleteCalls = 0
	f.ProjectCalls = 0
}

func (f *FakeArgoCDProvisioner) EnsureAppProject(_ context.Context, spec argocd.AppProjectSpec) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.ProjectCalls++
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	f.AppProjects[spec.Name] = spec
	return nil
}

func (f *FakeArgoCDProvisioner) EnsureApplication(_ context.Context, spec argocd.ApplicationSpec) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.EnsureCalls++
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	f.Applications[spec.Name] = spec
	return nil
}

func (f *FakeArgoCDProvisioner) DeleteApplications(_ context.Context, tenantID string) error {
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

func (f *FakeArgoCDProvisioner) DeleteAppProject(_ context.Context, tenantID string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	delete(f.AppProjects, tenantID)
	return nil
}

// SetupTenantController registers the TenantController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
// buildClient and runtimeClient target the build and runtime clusters respectively.
// In envtest, pass mgr.GetClient() for both (single API server).
func SetupTenantController(mgr ctrl.Manager, gitProvisioner GitRepoProvisioner, argoCDProvisioner ArgoCDProvisioner, buildClient client.Client, runtimeClient client.Client) error {
	return controller.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, buildClient, runtimeClient, controller.TenantControllerConfig{})
}

// SetupClusterCredentialController registers the ClusterCredentialController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
func SetupClusterCredentialController(mgr ctrl.Manager) error {
	return controller.SetupClusterCredentialController(mgr, constants.DefaultArgoCDNamespace)
}
