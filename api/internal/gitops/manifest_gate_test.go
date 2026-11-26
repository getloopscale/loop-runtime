// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitops

import (
	"strings"
	"testing"
)

// Roadmap item 01 (L1 Defect Remediation), Correction G:
// The previous gate used strings.Contains("kind: ClusterIssuer") which
// false-positived on `issuerRef.kind: ClusterIssuer` INSIDE Certificate CRs,
// blocking ALL new tenant deploys. The YAML-aware gate must:
//
//   1. Parse each document's top-level apiVersion+kind (not substring).
//   2. Allow Certificate with issuerRef.kind=ClusterIssuer (field, not doc).
//   3. Block ONLY top-level infra resources that must not live in tenant
//      gitops: Namespace, ClusterRole, ClusterRoleBinding, ClusterIssuer,
//      CustomResourceDefinition.
//   4. Be tolerant of empty/whitespace-only documents and YAML comments.

func TestManifestGate_AllowsCertificateWithClusterIssuerRef(t *testing.T) {
	// This is the real-world shape the renderer emits. Must pass.
	manifest := `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: tenant-web-tls
  namespace: tenant-foo
spec:
  secretName: tenant-web-tls
  dnsNames:
    - web.example.com
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
    group: cert-manager.io
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("Certificate with issuerRef.kind=ClusterIssuer must pass, got: %v", err)
	}
}

func TestManifestGate_BlocksTopLevelClusterIssuer(t *testing.T) {
	manifest := `apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    email: admin@example.com
`
	err := validateManifests(manifest)
	if err == nil {
		t.Fatal("top-level ClusterIssuer must be blocked")
	}
	if !strings.Contains(err.Error(), "ClusterIssuer") {
		t.Errorf("error should name the blocked kind, got: %v", err)
	}
}

func TestManifestGate_AllowsCommonTenantResources(t *testing.T) {
	// Deployment + Service + ConfigMap + IngressRoute + Certificate —
	// the five resources the renderer emits for every tenant app.
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: tenant-foo
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: tenant-foo
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
  namespace: tenant-foo
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: web
  namespace: tenant-foo
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web-tls
  namespace: tenant-foo
spec:
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt-prod
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("all-common-tenant-resources must pass, got: %v", err)
	}
}

func TestManifestGate_BlocksTopLevelNamespace(t *testing.T) {
	// Namespaces must be created by the controller, never by tenant gitops.
	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: tenant-foo
`
	if err := validateManifests(manifest); err == nil {
		t.Fatal("top-level Namespace must be blocked")
	}
}

func TestManifestGate_BlocksTopLevelClusterRole(t *testing.T) {
	manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: nosey
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["*"]
`
	if err := validateManifests(manifest); err == nil {
		t.Fatal("top-level ClusterRole must be blocked")
	}
}

func TestManifestGate_BlocksMixedWithForbidden(t *testing.T) {
	// Legit Deployment followed by a forbidden ClusterRoleBinding must fail.
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: tenant-foo
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: escalate
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: default
    namespace: tenant-foo
`
	if err := validateManifests(manifest); err == nil {
		t.Fatal("mixed manifest containing forbidden ClusterRoleBinding must be blocked")
	}
}

func TestManifestGate_TolerantOfEmptyDocuments(t *testing.T) {
	manifest := `
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: tenant-foo
---

---
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("empty/whitespace documents must be tolerated, got: %v", err)
	}
}

func TestManifestGate_EmptyInputIsNoop(t *testing.T) {
	if err := validateManifests(""); err != nil {
		t.Fatalf("empty input must pass (no-op), got: %v", err)
	}
}

func TestManifestGate_RejectsMalformedYAML(t *testing.T) {
	// Safety: if the gate can't parse it, it must fail closed.
	// A truly malformed doc should be rejected, not silently passed.
	manifest := `this is: : : not valid yaml
  nested: [unterminated
`
	if err := validateManifests(manifest); err == nil {
		t.Fatal("malformed YAML must be rejected (fail closed)")
	}
}

func TestManifestGate_BlocksCRD(t *testing.T) {
	manifest := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: evil.example.com
`
	err := validateManifests(manifest)
	if err == nil {
		t.Fatal("top-level CRD must be blocked")
	}
	if !strings.Contains(err.Error(), "CustomResourceDefinition") {
		t.Errorf("error should name blocked kind, got: %v", err)
	}
}

func TestManifestGate_BlocksClusterRoleBinding(t *testing.T) {
	manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: escalate
`
	if err := validateManifests(manifest); err == nil {
		t.Fatal("top-level ClusterRoleBinding must be blocked")
	}
}

func TestManifestGate_AllowsRole(t *testing.T) {
	manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: app-role
  namespace: tenant-foo
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("namespaced Role should be allowed, got: %v", err)
	}
}

func TestManifestGate_AllowsRoleBinding(t *testing.T) {
	manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: app-binding
  namespace: tenant-foo
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("namespaced RoleBinding should be allowed, got: %v", err)
	}
}

func TestManifestGate_WhitespaceOnly(t *testing.T) {
	if err := validateManifests("   \n\n   \t  "); err != nil {
		t.Fatalf("whitespace-only should pass, got: %v", err)
	}
}

func TestManifestGate_MultipleValidDocuments(t *testing.T) {
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: Secret
metadata:
  name: s1
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ss1
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("multiple valid docs should pass, got: %v", err)
	}
}

func TestManifestGate_AllowsIssuer(t *testing.T) {
	// Namespaced Issuer should NOT be blocked (only ClusterIssuer is blocked)
	manifest := `apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: my-issuer
  namespace: tenant-foo
`
	if err := validateManifests(manifest); err != nil {
		t.Fatalf("namespaced Issuer should be allowed, got: %v", err)
	}
}
