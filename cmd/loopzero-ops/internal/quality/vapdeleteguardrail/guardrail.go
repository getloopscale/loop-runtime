// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package vapdeleteguardrail implements the `vap-delete-guardrail` quality gate.
//
// Plan: .claude/knowledge-base/plans/d4-vap-delete-guardrail-plan.md (T1.3)
// ADR:  docs/decisions.md (D4)
//
// # WHAT IT GUARDS
//
// The mgmt cluster protects its boundary resources (Application, AppProject, Tenant,
// Environment, ClusterCredential) and the CRDs that define them from unauthorized
// DELETE. Deleting any of them triggers the ArgoCD cascade finalizer
// (`resources-finalizer.argocd.argoproj.io`, operators/internal/argocd/real_provisioner.go:205),
// which deletes the resources they manage. Under BYOC that cascade reaches a customer's
// own cloud account, so a regression here is contractual damage, not an internal incident.
//
// WHY THE CHECKS LOOK LIKE THIS
//
//   - EC-1: protecting only the *instances* leaves the exact vector of the reference
//     incident open — deleting the CRD garbage-collects every instance without any
//     `applications` rule being consulted. Hence the customresourcedefinitions rule.
//   - EC-2: the exemption CEL has the shape `!(username in [<list>])`. With an EMPTY list
//     that is `!(x in [])` — always true — so the policy runs for every identity and denies
//     EVERY delete, including the operators'. An empty list silently inverts the policy,
//     so the gate enforces a floor of two (tenant-operator + loopzero-api are the two with a
//     proven Delete caller).
//   - EC-3: the files are NOT valid YAML. Templates open with `{{- if .Values… }}` and the
//     helmfile is `.gotmpl`. Parsing them raw fails, so the gate renders with `helm template`
//     first and parses the OUTPUT — the same shape as crdsemanticdiff/esochartsanity.
//
// Advisory fallthrough when `helm` is absent from PATH follows the esooverlay precedent:
// a missing toolchain is a WARN, never a silent PASS.
package vapdeleteguardrail

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// chartPath is the one admission-policies chart: Helmfile installs it on core (with
	// mgmtValues) and on every cell, and ArgoCD consumes it as the published OCI chart.
	// There was a bootstrap copy under infra/helm, and this gate checked that both carried
	// the policy; the copy was deleted when the pair became one chart (docs/decisions.md,
	// "One chart per bootstrap/published pair"), and the parity check went with it.
	chartPath = "charts/loopzero-admission-policies"

	templateName = "vap-protect-platform-deletes.yaml"
	policyName   = "loopzero-protect-platform-deletes"
	toggleKey    = "protectPlatformDeletes"

	mgmtHelmfile = "infra/helmfile/core.helmfile.yaml.gotmpl"
	mgmtValues   = "charts/loopzero-admission-policies/values-mgmt.yaml"

	// minExemptIdentities is the EC-2 floor. Two is not arbitrary: exactly two
	// components have a proven production Delete caller against the guarded resources —
	// tenant-operator (real_provisioner.go:381,414,426 + tenant_deletion.go:86) and
	// loopzero-api (api/internal/k8s/tenant_evictor.go:248).
	minExemptIdentities = 2
)

// guardedResources are the boundary resources whose deletion triggers the cascade.
var guardedResources = []string{
	"applications",
	"appprojects",
	"tenants",
	"environments",
	"clustercredentials",
	"customresourcedefinitions", // EC-1
}

// clusterGCExemptions are copied verbatim from vap-protect-platform-networkpolicies.yaml:31-36.
// They exist because of B33: a VAP with failurePolicy=Fail on DELETE blocked the K8s
// namespace garbage collector and deadlocked 5 namespaces in Terminating forever.
var clusterGCExemptions = []string{
	"system:serviceaccount:kube-system:namespace-controller",
	"system:kube-controller-manager",
	"system:masters",
}

// Finding is one gate violation.
type Finding struct {
	Check   string
	Message string
}

// Report is the gate result.
type Report struct {
	Findings []Finding
	Warnings []string // advisory (e.g. helm absent) — never a silent pass
}

// OK reports whether the gate passed.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Renderer renders a chart to a YAML stream. Injected so the hermetic test does not
// require the helm binary (crdsemanticdiff:63 precedent).
type Renderer func(chartDir string, valuesFiles []string) (string, error)

// HelmRenderer shells out to `helm template`.
func HelmRenderer(chartDir string, valuesFiles []string) (string, error) {
	args := []string{"template", "gate-render", chartDir, "--namespace", "loopzero-system"}
	for _, v := range valuesFiles {
		args = append(args, "--values", v)
	}
	// The policy is default-OFF, so the gate must force it on to inspect the output.
	args = append(args, "--set", "vaps."+toggleKey+"=true")
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("helm template %s: %w: %s", chartDir, err, string(out))
	}
	return string(out), nil
}

// Check runs the gate against repoRoot.
func Check(repoRoot string, render Renderer) Report {
	var rep Report

	if render == nil {
		if _, err := exec.LookPath("helm"); err != nil {
			rep.Warnings = append(rep.Warnings,
				"helm not found on PATH — render-dependent checks skipped (advisory, not a pass)")
			// Source-level checks still run below.
		} else {
			render = HelmRenderer
		}
	}

	rep.checkTemplatePresent(repoRoot)
	rep.checkToggleDefaultOff(repoRoot)
	rep.checkInstalledOnMgmt(repoRoot)

	if render != nil {
		rendered, err := render(filepath.Join(repoRoot, chartPath), nil)
		if err != nil {
			rep.Findings = append(rep.Findings, Finding{
				Check:   "render",
				Message: fmt.Sprintf("chart failed to render: %v", err),
			})
			return rep
		}
		rep.checkRenderedPolicy(rendered)
		rep.checkExemptions(rendered)
		rep.checkNoObjectRefUnderDelete(rendered)
	}

	return rep
}

func (r *Report) fail(check, format string, a ...any) {
	r.Findings = append(r.Findings, Finding{Check: check, Message: fmt.Sprintf(format, a...)})
}

func (r *Report) checkTemplatePresent(root string) {
	p := filepath.Join(root, chartPath, "templates", templateName)
	if _, err := os.Stat(p); err != nil {
		r.fail("template-present",
			"%s missing — the mgmt cluster has no DELETE guardrail (plan T1.1)", p)
	}
}

// checkToggleDefaultOff mirrors the tenantNamespace convention of the chart values.yaml:
// a VAP that can self-inflict a DoS ships OFF and is enabled by overlay.
func (r *Report) checkToggleDefaultOff(root string) {
	p := filepath.Clean(filepath.Join(root, chartPath, "values.yaml"))
	b, err := os.ReadFile(p)
	if err != nil {
		r.fail("toggle-default-off", "%s unreadable: %v", p, err)
		return
	}
	re := regexp.MustCompile(`(?m)^\s*` + toggleKey + `:\s*(\S+)`)
	m := re.FindSubmatch(b)
	if m == nil {
		r.fail("toggle-default-off", "%s does not declare vaps.%s", p, toggleKey)
		return
	}
	if strings.TrimSpace(string(m[1])) != "false" {
		r.fail("toggle-default-off",
			"%s sets vaps.%s=%s; must default to false (enable via overlay after the Warn window)",
			p, toggleKey, m[1])
	}
}

// checkInstalledOnMgmt closes the finding that motivated this plan: the chart was only
// ever installed on the runtime cluster, while the guarded resources live on mgmt.
func (r *Report) checkInstalledOnMgmt(root string) {
	b, err := os.ReadFile(filepath.Join(root, mgmtHelmfile))
	if err != nil {
		r.fail("installed-on-mgmt", "%s unreadable: %v", mgmtHelmfile, err)
		return
	}
	if !strings.Contains(string(b), "admission-policies") {
		r.fail("installed-on-mgmt",
			"%s declares no admission-policies release — the policy would exist in the chart "+
				"but run on no cluster (plan T1.2)", mgmtHelmfile)
	}
	ov, err := os.ReadFile(filepath.Join(root, mgmtValues))
	if err != nil {
		r.fail("installed-on-mgmt", "%s missing — mgmt overlay must enable the policy", mgmtValues)
		return
	}
	if !regexp.MustCompile(`(?m)^\s*` + toggleKey + `:\s*true`).Match(ov) {
		r.fail("installed-on-mgmt", "%s does not set vaps.%s=true", mgmtValues, toggleKey)
	}
}

func (r *Report) checkRenderedPolicy(rendered string) {
	if !strings.Contains(rendered, policyName) {
		r.fail("policy-rendered", "rendered chart has no ValidatingAdmissionPolicy %q", policyName)
		return
	}
	if !strings.Contains(rendered, `"DELETE"`) && !strings.Contains(rendered, "- DELETE") {
		r.fail("policy-rendered", "policy %q declares no DELETE operation", policyName)
	}
	for _, res := range guardedResources {
		if !regexp.MustCompile(`(?m)^\s*-?\s*` + res + `\s*$|` + res).MatchString(rendered) {
			r.fail("guarded-resources",
				"resource %q not covered by the policy (EC-1 applies to customresourcedefinitions)", res)
		}
	}
}

// kubeSystemPrefixExemption is the negated prefix match the admission-policies helper
// (loopzero-admission.systemActorExemptions) renders. It exempts every kube-system service
// account — the namespace-controller included — so it satisfies the namespace-controller
// requirement without naming it. Only the NEGATED form counts: a positive startsWith
// would select the GC into the policy instead of keeping it out.
const kubeSystemPrefixExemption = "!(request.userInfo.username.startsWith('system:serviceaccount:kube-system:'))"

// exempted reports whether the rendered policy keeps identity ex out of its scope.
func exempted(rendered, ex string) bool {
	if strings.Contains(rendered, ex) {
		return true
	}
	return strings.HasPrefix(ex, "system:serviceaccount:kube-system:") &&
		strings.Contains(rendered, kubeSystemPrefixExemption)
}

// checkExemptions enforces EC-2 (floor of 2) and the B33 GC exemptions.
func (r *Report) checkExemptions(rendered string) {
	for _, ex := range clusterGCExemptions {
		if !exempted(rendered, ex) {
			r.fail("gc-exemption",
				"matchConditions missing %q — a VAP with failurePolicy=Fail on DELETE that does "+
					"not exempt the cluster GC deadlocks namespaces in Terminating (B33)", ex)
		}
	}

	count := len(regexp.MustCompile(`system:serviceaccount:loopzero-system:[a-z0-9-]+`).
		FindAllString(rendered, -1))
	if count < minExemptIdentities {
		r.fail("exempt-floor",
			"only %d platform identity exemption(s) found, need >= %d — an empty or near-empty "+
				"list makes the CEL `!(username in [...])` always true, so the policy denies EVERY "+
				"delete including the operators' (EC-2)", count, minExemptIdentities)
	}
}

// checkNoObjectRefUnderDelete enforces EC-7. On a DELETE admission request `object` is not
// populated the way it is on CREATE/UPDATE; an expression over `object` raises an
// evaluation error, and with failurePolicy=Fail that error becomes a blanket block.
func (r *Report) checkNoObjectRefUnderDelete(rendered string) {
	for _, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, "expression:") {
			continue
		}
		if regexp.MustCompile(`\bobject\.`).MatchString(line) &&
			!strings.Contains(line, "oldObject") {
			r.fail("delete-uses-oldobject",
				"expression references `object.` on a DELETE rule; use `oldObject` — on DELETE the "+
					"`object` is not populated and the CEL error becomes a blanket block under "+
					"failurePolicy=Fail (EC-7): %s", strings.TrimSpace(line))
		}
	}
}
