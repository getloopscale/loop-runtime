// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import "testing"

func TestValidateManifests_RejectsCertificate(t *testing.T) {
	manifests := `apiVersion: v1
kind: Service
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: tls-cd-example-com
`
	err := validateManifests(manifests)
	if err == nil {
		t.Fatal("expected error for Certificate CR in manifests")
	}
	if err.Error() == "" {
		t.Fatal("error message should not be empty")
	}
}

func TestValidateManifests_AcceptsValidManifests(t *testing.T) {
	manifests := `apiVersion: v1
kind: Service
metadata:
  name: api
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: api
`
	if err := validateManifests(manifests); err != nil {
		t.Fatalf("expected no error for valid manifests, got: %v", err)
	}
}

func TestValidateManifests_RejectsClusterIssuer(t *testing.T) {
	manifests := `apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
`
	if err := validateManifests(manifests); err == nil {
		t.Fatal("expected error for ClusterIssuer in manifests")
	}
}

func TestValidateManifests_AcceptsEmpty(t *testing.T) {
	if err := validateManifests(""); err != nil {
		t.Fatalf("expected no error for empty manifests, got: %v", err)
	}
}
