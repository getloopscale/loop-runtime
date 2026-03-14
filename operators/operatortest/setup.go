// Package operatortest exports operator setup functions for integration and E2E testing.
//
// This package provides a public facade over operators/internal/controller and
// operators/internal/gitea so that external test modules can start an in-process
// TenantController against envtest without importing internal packages.
package operatortest

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/controller"
	"github.com/loopzero/loopzero/operators/internal/gitea"
)

// GitRepoProvisioner is the interface for Git repository provisioning.
type GitRepoProvisioner = gitea.GitRepoProvisioner

// ArgoCDProvisioner is the interface for ArgoCD Application provisioning.
type ArgoCDProvisioner = argocd.Provisioner

// NewFakeGitProvisioner returns a fake GitRepoProvisioner for testing.
// EnsureRepo returns a predictable URL; ArchiveRepo is a no-op.
func NewFakeGitProvisioner() *gitea.FakeProvisioner {
	return gitea.NewFakeProvisioner()
}

// NewFakeArgoCDProvisioner returns a fake ArgoCD Provisioner for testing.
func NewFakeArgoCDProvisioner() *argocd.FakeProvisioner {
	return argocd.NewFakeProvisioner()
}

// SetupTenantController registers the TenantController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
func SetupTenantController(mgr ctrl.Manager, gitProvisioner GitRepoProvisioner, argoCDProvisioner ArgoCDProvisioner) error {
	return controller.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner)
}

// SetupClusterCredentialController registers the ClusterCredentialController with the given manager.
// Use this in E2E tests to start the operator in-process against envtest.
func SetupClusterCredentialController(mgr ctrl.Manager) error {
	return controller.SetupClusterCredentialController(mgr)
}
