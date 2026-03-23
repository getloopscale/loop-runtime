// Package naming re-exports naming functions from pkg/naming for use
// within operators. All naming logic is centralized in pkg/naming;
// this package exists only for backward compatibility with existing imports.
package naming

import (
	pkgnaming "github.com/loopzero/loopzero/pkg/naming"
)

// MaxKubernetesNameLength re-exports the constant from pkg/naming.
const MaxKubernetesNameLength = pkgnaming.MaxKubernetesNameLength

// ValidateKubernetesName delegates to pkg/naming.
var ValidateKubernetesName = pkgnaming.ValidateKubernetesName

// TenantNamespace delegates to pkg/naming.
var TenantNamespace = pkgnaming.TenantNamespace

// EnvironmentNamespace delegates to pkg/naming.
var EnvironmentNamespace = pkgnaming.EnvironmentNamespace

// BuildNamespace delegates to pkg/naming.
var BuildNamespace = pkgnaming.BuildNamespace

// PreviewNamespace delegates to pkg/naming.
var PreviewNamespace = pkgnaming.PreviewNamespace

// RepoName delegates to pkg/naming.
var RepoName = pkgnaming.RepoName

// GitOpsRepoName delegates to pkg/naming.
var GitOpsRepoName = pkgnaming.GitOpsRepoName

// ArgoAppName delegates to pkg/naming.
var ArgoAppName = pkgnaming.ArgoAppName

// Hostname delegates to pkg/naming.
var Hostname = pkgnaming.Hostname

// CertSecretName delegates to pkg/naming.
var CertSecretName = pkgnaming.CertSecretName

// Labels delegates to pkg/naming.
var Labels = pkgnaming.Labels

// Sanitize delegates to pkg/naming.
var Sanitize = pkgnaming.Sanitize
