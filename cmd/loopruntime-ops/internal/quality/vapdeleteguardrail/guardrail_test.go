// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vapdeleteguardrail

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// stubRender returns a canned render so the hermetic tests never need the helm binary
// (crdsemanticdiff HelmRenderOverride precedent).
func stubRender(body string) Renderer {
	return func(string, []string) (string, error) { return body, nil }
}

const goodRender = `
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: loopruntime-protect-platform-deletes
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: ["argoproj.io"]
        operations: ["DELETE"]
        resources: ["applications", "appprojects"]
      - apiGroups: ["runtime.getloopscale.com"]
        operations: ["DELETE"]
        resources: ["tenants", "environments", "clustercredentials"]
      - apiGroups: ["apiextensions.k8s.io"]
        operations: ["DELETE"]
        resources: ["customresourcedefinitions"]
  matchConditions:
    - name: skip-cluster-internal-gc
      expression: >-
        request.userInfo.username != 'system:serviceaccount:kube-system:namespace-controller'
        && request.userInfo.username != 'system:kube-controller-manager'
        && !('system:masters' in request.userInfo.groups)
    - name: skip-platform-identities
      expression: >-
        !(request.userInfo.username in [
          'system:serviceaccount:loopruntime-system:loopruntime-tenant-operator',
          'system:serviceaccount:loopruntime-system:loopruntime-api'
        ])
  validations:
    - expression: 'false'
`

// --- EC-1: the CRD itself must be guarded -----------------------------------------
//
// This is the vector of the reference incident: deleting the CRD garbage-collects every
// instance, so a policy that guards only instances leaves the actual attack path open.

func TestGuardrail_FailsWhenCRDRuleMissing(t *testing.T) {
	noCRD := strings.ReplaceAll(goodRender, "customresourcedefinitions", "somethingelse")
	rep := Report{}
	rep.checkRenderedPolicy(noCRD)
	if rep.OK() {
		t.Fatal("expected failure when customresourcedefinitions is not guarded (EC-1)")
	}
}

// --- EC-2: an empty exemption list silently inverts the policy ---------------------

func TestGuardrail_FailsWhenExemptListShorterThanTwo(t *testing.T) {
	oneIdentity := strings.Replace(goodRender,
		"'system:serviceaccount:loopruntime-system:loopruntime-api'\n", "", 1)
	rep := Report{}
	rep.checkExemptions(oneIdentity)
	if rep.OK() {
		t.Fatal("expected failure with fewer than 2 platform exemptions (EC-2): " +
			"`!(x in [])` is always true, so the policy would deny every delete")
	}
}

func TestGuardrail_PassesWithTwoExemptions(t *testing.T) {
	rep := Report{}
	rep.checkExemptions(goodRender)
	if !rep.OK() {
		t.Fatalf("expected pass with 2 exemptions + GC exemptions, got %+v", rep.Findings)
	}
}

// --- B33: the cluster GC must stay exempt -----------------------------------------

func TestGuardrail_FailsWhenGCExemptionMissing(t *testing.T) {
	noGC := strings.ReplaceAll(goodRender,
		"system:serviceaccount:kube-system:namespace-controller", "system:someone-else")
	rep := Report{}
	rep.checkExemptions(noGC)
	if rep.OK() {
		t.Fatal("expected failure when the namespace-controller is not exempt (B33)")
	}
}

// kubeSystemPrefixRender is the shape the admission-policies helper
// (loopruntime-admission.systemActorExemptions) renders: every kube-system service account
// is exempt by prefix, which covers the namespace-controller without naming it.
const kubeSystemPrefixRender = `
  matchConditions:
    - name: exclude-system-actors
      expression: >-
        !(request.userInfo.username.startsWith('system:serviceaccount:kube-system:'))
        && request.userInfo.username != 'system:kube-controller-manager'
        && !(request.userInfo.username in [
          'system:serviceaccount:loopruntime-system:loopruntime-api',
          'system:serviceaccount:loopruntime-system:loopruntime-tenant-operator'
        ])
        && !('system:masters' in request.userInfo.groups)
`

func TestGuardrail_AcceptsKubeSystemPrefixAsNamespaceControllerExemption(t *testing.T) {
	rep := Report{}
	rep.checkExemptions(kubeSystemPrefixRender)
	if !rep.OK() {
		t.Fatalf("a kube-system prefix exemption covers the namespace-controller (B33), got %+v", rep.Findings)
	}
}

func TestGuardrail_FailsWhenKubeSystemPrefixIsNotAnExemption(t *testing.T) {
	// The prefix appearing somewhere else (a message, an unrelated rule) is not an
	// exemption: only the negated startsWith form keeps the GC out of the policy.
	notExempt := strings.Replace(kubeSystemPrefixRender,
		"!(request.userInfo.username.startsWith('system:serviceaccount:kube-system:'))",
		"request.userInfo.username.startsWith('system:serviceaccount:kube-system:')", 1)
	rep := Report{}
	rep.checkExemptions(notExempt)
	if rep.OK() {
		t.Fatal("expected failure: a positive startsWith does not exempt the namespace-controller (B33)")
	}
}

// --- EC-7: on DELETE the CEL must read oldObject, not object -----------------------

func TestGuardrail_FailsWhenExpressionUsesObjectOnDelete(t *testing.T) {
	bad := goodRender + "\n    - expression: 'object.metadata.name != \"x\"'\n"
	rep := Report{}
	rep.checkNoObjectRefUnderDelete(bad)
	if rep.OK() {
		t.Fatal("expected failure when an expression references object. on a DELETE rule (EC-7)")
	}
}

func TestGuardrail_AllowsOldObjectOnDelete(t *testing.T) {
	ok := goodRender + "\n    - expression: 'oldObject.metadata.name != \"x\"'\n"
	rep := Report{}
	rep.checkNoObjectRefUnderDelete(ok)
	if !rep.OK() {
		t.Fatalf("oldObject must be accepted on DELETE, got %+v", rep.Findings)
	}
}

// --- EC-3: the gate must parse RENDERED output, never the raw templated file -------
//
// The template's first line is `{{- if .Values… }}` and the helmfile is .gotmpl; a raw
// YAML parse fails on both. This asserts the gate goes through a Renderer.

func TestGuardrail_RendersViaHelmNotRawYAML(t *testing.T) {
	root := qtestutil.Root(t)
	called := false
	render := func(dir string, vals []string) (string, error) {
		called = true
		return goodRender, nil
	}
	_ = Check(root, render)
	if !called {
		t.Fatal("gate did not go through the Renderer — parsing the raw template would " +
			"fail on the leading `{{- if }}` (EC-3)")
	}
}

// --- Repo-state checks: these are the RED tests for T1.1/T1.2/T1.4 -----------------

func TestGuardrail_TemplateExistsInChart(t *testing.T) {
	root := qtestutil.Root(t)
	rep := Report{}
	rep.checkTemplatePresent(root)
	if !rep.OK() {
		t.Fatalf("T1.1 not implemented: %+v", rep.Findings)
	}
}

func TestGuardrail_ToggleDefaultsOff(t *testing.T) {
	root := qtestutil.Root(t)
	rep := Report{}
	rep.checkToggleDefaultOff(root)
	if !rep.OK() {
		t.Fatalf("toggle must exist and default to false in the chart: %+v", rep.Findings)
	}
}

func TestGuardrail_InstalledOnMgmt(t *testing.T) {
	root := qtestutil.Root(t)
	rep := Report{}
	rep.checkInstalledOnMgmt(root)
	if !rep.OK() {
		t.Fatalf("T1.2 not implemented — the policy would run on no cluster: %+v", rep.Findings)
	}
}

// End-to-end against the real repo with a stubbed renderer.
func TestGuardrail_FullCheckAgainstRepo(t *testing.T) {
	root := qtestutil.Root(t)
	rep := Check(root, stubRender(goodRender))
	if !rep.OK() {
		t.Fatalf("gate failing against repo state: %+v", rep.Findings)
	}
}
