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
	"net/http"
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loopzero/operators/internal/app"
	"github.com/getloopscale/loopzero/operators/internal/argocd"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/gitea"
	miniop "github.com/getloopscale/loopzero/operators/internal/minio"
	"github.com/getloopscale/loopzero/operators/internal/platform"
	"github.com/getloopscale/loopzero/operators/internal/preview"
	"github.com/getloopscale/loopzero/operators/internal/tenant"
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
	url := fmt.Sprintf("http://gitea-http.gitea.svc.cluster.local:3000/loopzero/%s.git", name)
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

// UnarchiveRepo is a no-op in the fake — repos are tracked by presence in the
// map, so "unarchive" is just "make sure it's present". Real production
// behavior (flipping the archived flag back to false) is exercised by
// integration tests that hit a real Gitea.
func (f *FakeGitProvisioner) UnarchiveRepo(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if _, ok := f.repos[name]; !ok {
		url := fmt.Sprintf("http://gitea-http.gitea.svc.cluster.local:3000/loopzero/%s.git", name)
		f.repos[name] = url
	}
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

// ListApplicationNames returns the names of every Application labeled with
// the given tenantID. #71 fake mirror of RealProvisioner behaviour.
func (f *FakeArgoCDProvisioner) ListApplicationNames(_ context.Context, tenantID string) ([]string, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	names := make([]string, 0)
	for name, app := range f.Applications {
		if app.TenantID == tenantID {
			names = append(names, name)
		}
	}
	return names, nil
}

// DeleteApplication removes a SINGLE Application by name. Idempotent.
// #71 fake mirror.
func (f *FakeArgoCDProvisioner) DeleteApplication(_ context.Context, appName string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	delete(f.Applications, appName)
	return nil
}

// Compile-time interface compliance check.
var _ miniop.CredentialProvisioner = (*FakeMinIOProvisioner)(nil)

// FakeMinIOProvisioner is an in-memory implementation of minio.CredentialProvisioner for tests.
// Tracks created credentials per tenant and can be configured to return errors.
type FakeMinIOProvisioner struct {
	mu          sync.Mutex
	Credentials map[string]struct{ AccessKey, SecretKey string } // tenantID -> credentials
	Errors      map[string]error                                 // tenantID -> error to return
	EnsureCalls int
	DeleteCalls int
}

// NewFakeMinIOProvisioner returns a fake MinIO CredentialProvisioner for testing.
func NewFakeMinIOProvisioner() *FakeMinIOProvisioner {
	return &FakeMinIOProvisioner{
		Credentials: make(map[string]struct{ AccessKey, SecretKey string }),
		Errors:      make(map[string]error),
	}
}

// Reset clears all state in the fake provisioner, restoring it to initial state.
// Call this between tests to ensure isolation.
func (f *FakeMinIOProvisioner) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Credentials = make(map[string]struct{ AccessKey, SecretKey string })
	f.Errors = make(map[string]error)
	f.EnsureCalls = 0
	f.DeleteCalls = 0
}

// ClearErrors clears all injected errors without resetting credentials or call counts.
// Use between test specs to prevent error leakage while preserving data state.
func (f *FakeMinIOProvisioner) ClearErrors() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Errors = make(map[string]error)
}

// SetError configures the fake to return the given error for a specific tenant.
func (f *FakeMinIOProvisioner) SetError(tenantID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Errors[tenantID] = err
}

// GetEnsureCalls returns the number of EnsureTenantCredentials calls (thread-safe).
func (f *FakeMinIOProvisioner) GetEnsureCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.EnsureCalls
}

// GetDeleteCalls returns the number of DeleteTenantCredentials calls (thread-safe).
func (f *FakeMinIOProvisioner) GetDeleteCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.DeleteCalls
}

// EnsureTenantCredentials creates or returns existing MinIO credentials for a tenant.
// If the tenant already has credentials, returns the access key with an empty secret
// (matching the real provisioner's idempotent behavior).
func (f *FakeMinIOProvisioner) EnsureTenantCredentials(_ context.Context, tenantID string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.EnsureCalls++
	if err, ok := f.Errors[tenantID]; ok {
		return "", "", err
	}
	if cred, ok := f.Credentials[tenantID]; ok {
		return cred.AccessKey, "", nil // existing user, empty secret
	}
	accessKey := fmt.Sprintf("loopzero-build-%s", tenantID)
	secretKey := fmt.Sprintf("fake-secret-%s", tenantID)
	f.Credentials[tenantID] = struct{ AccessKey, SecretKey string }{accessKey, secretKey}
	return accessKey, secretKey, nil
}

// DeleteTenantCredentials removes MinIO credentials for a tenant.
// Idempotent: returns nil if the tenant has no credentials.
func (f *FakeMinIOProvisioner) DeleteTenantCredentials(_ context.Context, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteCalls++
	if err, ok := f.Errors[tenantID]; ok {
		return err
	}
	delete(f.Credentials, tenantID)
	return nil
}

// SetupTenantController registers the TenantController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
// buildClient targets the build cluster; runtimeFactory resolves the runtime
// cluster of each tenant's cell (in envtest, the tracking factory).
//
// ADR-0023 D1: delegates to internal/tenant (canonical split-tree
// implementation) instead of internal/controller (deprecated monolith).
func SetupTenantController(mgr ctrl.Manager, gitProvisioner GitRepoProvisioner, argoCDProvisioner ArgoCDProvisioner, buildClient client.Client, runtimeFactory cellrouting.RuntimeClientFactory) error {
	return tenant.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, buildClient, runtimeFactory, tenant.TenantControllerConfig{})
}

// SetupClusterCredentialController registers the ClusterCredentialController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
//
// ADR-0023 D1: delegates to internal/platform.
func SetupClusterCredentialController(mgr ctrl.Manager) error {
	return platform.SetupClusterCredentialController(mgr, constants.DefaultArgoCDNamespace)
}

// Quality-Gate Phase 2: SetupEnvironmentController wrapper removed —
// envtest_suite.go calls SetupEnvironmentControllerWithFactory directly.

// SetupApplicationController registers the ApplicationController with the given manager.
// baseDomain is the base domain for hostname assignment (e.g., "dev.loopzero.example");
// runtimeFactory resolves the runtime cluster of each tenant's cell.
//
// ADR-0023 D1: delegates to internal/app.
func SetupApplicationController(mgr ctrl.Manager, baseDomain string, runtimeFactory cellrouting.RuntimeClientFactory) error {
	return app.SetupApplicationControllerWithFactory(mgr, baseDomain, runtimeFactory)
}

// SetupExposureController registers the ExposureController with the given manager.
// Enables AllowProbeURLOverride for test environments (SEC-005).
//
// ADR-0023 D1: delegates to internal/app.
func SetupExposureController(mgr ctrl.Manager, runtimeFactory cellrouting.RuntimeClientFactory) error {
	return app.SetupExposureController(mgr, app.ExposureControllerConfig{
		AllowProbeURLOverride: true,
		// #277: the TLS Secret lives in the workload's cell, so the cert check
		// resolves it through the same factory as every other runtime read.
		RuntimeFactory: runtimeFactory,
	})
}

// SetupGitRepositoryBindingController registers the GitRepositoryBindingController with the given manager.
// httpClient is used for Gitea API calls; pass nil to use http.DefaultClient.
//
// ADR-0023 D1: delegates to internal/platform.
func SetupGitRepositoryBindingController(mgr ctrl.Manager, httpClient *http.Client) error {
	return platform.SetupGitRepositoryBindingController(mgr, httpClient)
}

// SetupPlatformBootstrapController registers the PlatformBootstrapController with the given manager.
// httpClient is used for external API calls; pass nil to use http.DefaultClient.
//
// ADR-0023 D1: delegates to internal/platform.
func SetupPlatformBootstrapController(mgr ctrl.Manager, httpClient *http.Client) error {
	return platform.SetupPlatformBootstrapController(mgr, httpClient)
}

// SetupPreviewController registers the PreviewController with the given manager.
// runtimeFactory resolves the runtime cluster of each preview's cell.
//
// ADR-0023 D1: delegates to internal/preview.
func SetupPreviewController(mgr ctrl.Manager, runtimeFactory cellrouting.RuntimeClientFactory) error {
	return preview.SetupPreviewController(mgr, runtimeFactory)
}
