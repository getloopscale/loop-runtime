// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package tenant_test — T1.1 of plan 2026-06-09-m2-operator-canonical-fix.
//
// CRD-level admission validation that TenantEnvironmentEntry.Project is
// required and matches the RFC 1123 DNS-label pattern. Per ADR D5 this
// validation tightens AFTER Phase 5 (T5.1) migrates legacy CRs whose
// Project field was Optional under the previous schema (the J-23 #69
// backwards-compat path).
//
// Test source mapping:
//   - Plan TDD body lines 323-350 (RED 1 + RED 2)
//   - EC-2 absorption (Pattern catches non-RFC-1123 slugs at the boundary
//     instead of leaving Environment stuck Pending with a cryptic error
//     surfaced by pkg/naming.ValidateKubernetesName downstream).

package tenant_test

import (
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

// TestProjectFieldHasRFC1123PatternAndRequired (RED 2 / EC-2) — schema
// contract test. Asserts the generated CRD YAML at
// `../../config/crd/bases/loopzero.io_tenants.yaml` declares:
//  1. The TenantEnvironmentEntry.project field has the canonical RFC 1123
//     DNS-label Pattern (^[a-z0-9]([-a-z0-9]*[a-z0-9])?$).
//  2. The field has minLength: 1.
//  3. The TenantEnvironmentEntry required-list includes "project".
//
// This is a deterministic file-based test that bypasses envtest's API
// server (Pattern enforcement varies by envtest binary version). The CRD
// YAML IS the contract; any conformant K8s API server (dev-public, prod)
// enforces the Pattern.
func TestProjectFieldHasRFC1123PatternAndRequired(t *testing.T) {
	const crdPath = "../../config/crd/bases/loopzero.io_tenants.yaml"
	const expectedPattern = "pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	const expectedMinLength = "minLength: 1"

	bytes, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD YAML: %v", err)
	}
	content := string(bytes)

	// Locate the `project:` field block; the relevant Pattern + minLength
	// must appear within it (not, e.g., from the cluster/name fields).
	projectIdx := strings.Index(content, "                    project:")
	if projectIdx < 0 {
		t.Fatalf("CRD YAML missing 'project:' field declaration")
	}
	// Slice from project: forward to the next field at same indent (cluster
	// + name are alphabetically before project; the next field would be
	// after the project block ends).
	tail := content[projectIdx:]
	// Cap to a reasonable window so we don't accidentally match later fields.
	if len(tail) > 2500 {
		tail = tail[:2500]
	}

	if !strings.Contains(tail, expectedPattern) {
		t.Errorf("project field missing canonical RFC 1123 Pattern; expected %q in:\n%s",
			expectedPattern, tail)
	}
	if !strings.Contains(tail, expectedMinLength) {
		t.Errorf("project field missing %q in:\n%s", expectedMinLength, tail)
	}

	// Required-list check: the required: block under TenantEnvironmentEntry
	// must list cluster, name, AND project.
	requiredIdx := strings.Index(content, "                  required:\n                  - cluster\n                  - name\n                  - project\n")
	if requiredIdx < 0 {
		t.Errorf("TenantEnvironmentEntry required-list missing project — expected `required: [cluster, name, project]`")
	}
}

// makeTenantSpec returns a baseline TenantSpec that is valid EXCEPT for the
// caller-supplied Project field on the single Environments entry. The caller
// passes `project` to drive the test scenario:
//   - "" (empty) exercises the required-field gate.
//   - any non-RFC-1123 string exercises the Pattern gate.
//   - a valid slug ("sample") proves the happy path Create succeeds.
func makeTenantSpec(project string) loopzerov1.TenantSpec {
	return loopzerov1.TenantSpec{
		DisplayName: "T1.1 CRD validation tenant",
		Plan:        loopzerov1.PlanStarter,
		Environments: []loopzerov1.TenantEnvironmentEntry{
			{Name: "production", Cluster: "workload", Project: project},
		},
		GitOps: loopzerov1.GitOpsSpec{
			RepoName: "loopzero-tenant-t11",
			Branch:   "main",
		},
		Preview: loopzerov1.PreviewSpec{MaxEnvironments: 2, TTLHours: 24},
		Limits: loopzerov1.ResourceLimits{
			MaxCPU:              resource.MustParse("8"),
			MaxMemory:           resource.MustParse("16Gi"),
			MaxStorage:          resource.MustParse("50Gi"),
			MaxBuildConcurrency: 2,
		},
	}
}

var _ = Describe("TenantEnvironmentEntry.Project CRD validation", func() {
	Context("required-field gate (RED 1)", func() {
		It("rejects Tenant CR whose Environments entry has empty Project", func() {
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "t11-required-gate",
					Namespace: "default",
				},
				Spec: makeTenantSpec(""), // empty -> CRD must reject
			}
			err := k8sClient.Create(ctx, tenant)
			Expect(err).To(HaveOccurred(), "API server must reject empty Project")
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("project"),
				ContainSubstring("Project"),
			), "field error must mention Project — got: %v", err)
		})
	})

	// EC-2 Pattern gate is verified via the CRD YAML schema contract test
	// in TestProjectFieldHasRFC1123PatternAndRequired (below — NOT a
	// Ginkgo spec). Rationale: envtest's API server is reliable for
	// structural validation (Required + MinLength) but Pattern enforcement
	// has known limitations across envtest binary versions. The CRD YAML
	// is the contract; the LIVE dev-public cluster (T6.1) is the
	// integration boundary that proves Pattern is enforced end-to-end
	// against a real API server.
	Context("Pattern gate (RED 2 / EC-2) — deferred to schema contract test", func() {
		PIt("envtest skip — see TestProjectFieldHasRFC1123PatternAndRequired", func() {})
	})

	Context("happy path (sanity)", func() {
		It("accepts Tenant CR with valid RFC-1123 Project slug", func() {
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "t11-happy",
					Namespace: "default",
				},
				Spec: makeTenantSpec("sample"),
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed(),
				"valid slug 'sample' must be accepted as proof the gate is not over-restrictive")
		})
	})
})
