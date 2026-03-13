// Package operatortest exports operator setup functions for integration and E2E testing.
//
// This package provides a public facade over operators/internal/controller and
// operators/internal/gitea so that external test modules can start an in-process
// TenantController against envtest without importing internal packages.
package operatortest

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/loopzero/loopzero/operators/internal/controller"
	"github.com/loopzero/loopzero/operators/internal/gitea"
)

// GitRepoProvisioner is the interface for Git repository provisioning.
type GitRepoProvisioner = gitea.GitRepoProvisioner

// NewFakeGitProvisioner returns a fake GitRepoProvisioner for testing.
// EnsureRepo returns a predictable URL; ArchiveRepo is a no-op.
func NewFakeGitProvisioner() *gitea.FakeProvisioner {
	return gitea.NewFakeProvisioner()
}

// SetupTenantController registers the TenantController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
func SetupTenantController(mgr ctrl.Manager, gitProvisioner GitRepoProvisioner) error {
	return controller.SetupTenantController(mgr, gitProvisioner)
}
