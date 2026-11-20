// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"gopkg.in/yaml.v3"
)

// M14 gap #2 — the Tenant-CR-shape ValidatingAdmissionPolicy is CEL. These tests
// evaluate the ACTUAL CEL expressions extracted from the rendered VAP artifact
// against sample admission objects using cel-go — the same evaluator the API
// server runs. This couples the test to the real YAML: break the CEL in the
// template and these tests break (the wiring). Chosen over envtest because it
// tests the exact CEL hermetically, without depending on the apiserver
// ValidatingAdmissionPolicy feature-gate config (plan T1.1 / ADR D1).

// vapDoc is the minimal shape we read out of the rendered VAP.
type vapDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Validations []struct {
			Expression string `yaml:"expression"`
		} `yaml:"validations"`
		MatchConditions []struct {
			Name       string `yaml:"name"`
			Expression string `yaml:"expression"`
		} `yaml:"matchConditions"`
	} `yaml:"spec"`
}

// loadTenantVAP renders the admission-policies chart and returns the
// shape-validation and immutability CEL expressions of loopzero-tenant-cr-shape,
// plus its system-exclusion matchCondition. It reads the RENDER, not the
// template: the exclusion is produced by the loopzero-admission.systemActorExemptions
// helper, so a template read sees an include line and no expression at all.
func loadTenantVAP(t *testing.T) (shapeExpr, cellImmutableExpr, systemExcludeExpr string) {
	t.Helper()
	// The policy is off in the base values (the cells hold no Tenants) and on
	// in values-mgmt.yaml, where the Tenants live — render the install that
	// enforces it, so turning it off there fails these tests too.
	chart := publishedChart(t, "loopzero-admission-policies")
	rendered := renderChart(t, chart, "ap", nil, filepath.Join(chart, "values-mgmt.yaml"))
	for _, doc := range strings.Split(rendered, "\n---") {
		var v vapDoc
		if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
			continue
		}
		if v.Kind != "ValidatingAdmissionPolicy" || v.Metadata.Name != "loopzero-tenant-cr-shape" {
			continue
		}
		if len(v.Spec.Validations) >= 2 {
			shapeExpr = v.Spec.Validations[0].Expression
			cellImmutableExpr = v.Spec.Validations[1].Expression
		}
		for _, mc := range v.Spec.MatchConditions {
			if mc.Name == "exclude-system-actors" {
				systemExcludeExpr = mc.Expression
			}
		}
	}
	if shapeExpr == "" || cellImmutableExpr == "" || systemExcludeExpr == "" {
		t.Fatalf("could not extract all CEL exprs: shape=%q immutable=%q exclude=%q", shapeExpr, cellImmutableExpr, systemExcludeExpr)
	}
	return
}

// evalCEL compiles expr with object/oldObject/request vars and evaluates it.
func evalCEL(t *testing.T, expr string, vars map[string]any) bool {
	t.Helper()
	env, err := cel.NewEnv(
		cel.Variable("object", cel.DynType),
		cel.Variable("oldObject", cel.DynType),
		cel.Variable("request", cel.DynType),
	)
	if err != nil {
		t.Fatalf("cel env: %v", err)
	}
	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		t.Fatalf("cel compile %q: %v", expr, iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("cel program: %v", err)
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		t.Fatalf("cel eval %q: %v", expr, err)
	}
	return out == types.True
}

func env(name string) map[string]any { return map[string]any{"name": name} }

// TestTenantVAP_AcceptsValidTenant: a canonical CR with valid env names admits.
func TestTenantVAP_AcceptsValidTenant(t *testing.T) {
	shape, _, _ := loadTenantVAP(t)
	obj := map[string]any{"spec": map[string]any{"environments": []any{env("production"), env("staging")}}}
	if !evalCEL(t, shape, map[string]any{"object": obj}) {
		t.Error("valid tenant should be admitted (shape expr returned false)")
	}
}

// TestTenantVAP_RejectsInvalidProject: an env name with uppercase/underscore is
// denied.
func TestTenantVAP_RejectsInvalidProject(t *testing.T) {
	shape, _, _ := loadTenantVAP(t)
	obj := map[string]any{"spec": map[string]any{"environments": []any{env("Bad_Name")}}}
	if evalCEL(t, shape, map[string]any{"object": obj}) {
		t.Error("invalid env name should be denied (shape expr returned true)")
	}
}

// TestTenantVAP_HandlesMissingEnvironments (EC-1): a CR with no spec.environments
// is DENIED cleanly (expr returns false) rather than raising a CEL runtime error.
func TestTenantVAP_HandlesMissingEnvironments(t *testing.T) {
	shape, _, _ := loadTenantVAP(t)
	obj := map[string]any{"spec": map[string]any{}} // no environments key
	if evalCEL(t, shape, map[string]any{"object": obj}) {
		t.Error("missing environments should be denied cleanly, not admitted (EC-1 has()-guard)")
	}
}

// TestTenantVAP_RejectsCellIDMutation: changing cellID on UPDATE is denied.
func TestTenantVAP_RejectsCellIDMutation(t *testing.T) {
	_, immutable, _ := loadTenantVAP(t)
	vars := map[string]any{
		"request":   map[string]any{"operation": "UPDATE"},
		"oldObject": map[string]any{"spec": map[string]any{"cellID": "cell-0"}},
		"object":    map[string]any{"spec": map[string]any{"cellID": "cell-1"}},
	}
	if evalCEL(t, immutable, vars) {
		t.Error("cellID mutation should be denied (immutability expr returned true)")
	}
}

// TestTenantVAP_AllowsCellIDUnchanged: same cellID on UPDATE admits.
func TestTenantVAP_AllowsCellIDUnchanged(t *testing.T) {
	_, immutable, _ := loadTenantVAP(t)
	vars := map[string]any{
		"request":   map[string]any{"operation": "UPDATE"},
		"oldObject": map[string]any{"spec": map[string]any{"cellID": "cell-0"}},
		"object":    map[string]any{"spec": map[string]any{"cellID": "cell-0"}},
	}
	if !evalCEL(t, immutable, vars) {
		t.Error("unchanged cellID should be admitted (immutability expr returned false)")
	}
}

// TestTenantVAP_ExemptsSystemController (anti-B33): a kube-system SA is excluded
// by the matchCondition (expr returns false → policy not applied to it).
func TestTenantVAP_ExemptsSystemController(t *testing.T) {
	_, _, exclude := loadTenantVAP(t)
	vars := map[string]any{"request": map[string]any{"userInfo": map[string]any{
		"username": "system:serviceaccount:kube-system:namespace-controller",
		"groups":   []any{"system:authenticated"},
	}}}
	if evalCEL(t, exclude, vars) {
		t.Error("kube-system SA should be excluded from the policy (matchCondition returned true = policy applies)")
	}
}

// TestTenantVAP_AppliesToRegularUser: a normal user IS subject to the policy.
func TestTenantVAP_AppliesToRegularUser(t *testing.T) {
	_, _, exclude := loadTenantVAP(t)
	vars := map[string]any{"request": map[string]any{"userInfo": map[string]any{
		"username": "operator@getloopscale.com",
		"groups":   []any{"system:authenticated"},
	}}}
	if !evalCEL(t, exclude, vars) {
		t.Error("regular user should be subject to the policy (matchCondition returned false)")
	}
}

// TestTenantVAP_ExemptsLoopZeroAPIControlPlane (vap-pinpath-exemption T1): the
// loopzero-api pod's own ServiceAccount is the ONLY sanctioned cell-move writer
// (`loopzero admin tenants pin` → CellPinner.patchTenantCR runs as this SA via
// rest.InClusterConfig()). It MUST be excluded from the policy, exactly like
// kube-controller-manager — otherwise the immutability validation Denies the
// sanctioned pin and the DB/CR split-brain. exclude expr must return false
// (= policy skipped) for this SA.
func TestTenantVAP_ExemptsLoopZeroAPIControlPlane(t *testing.T) {
	_, _, exclude := loadTenantVAP(t)
	vars := map[string]any{"request": map[string]any{"userInfo": map[string]any{
		"username": "system:serviceaccount:loopzero-system:loopzero-api",
		"groups":   []any{"system:authenticated", "system:serviceaccounts"},
	}}}
	if evalCEL(t, exclude, vars) {
		t.Error("loopzero-api control-plane SA should be excluded so the sanctioned pin is admitted (matchCondition returned true = policy applies → pin Denied, split-brain)")
	}
}

// TestTenantVAP_DoesNotExemptOtherServiceAccounts (vap-pinpath-exemption T2):
// the exemption is scoped to the exact loopzero-api SA username — a look-alike SA
// in the same namespace stays subject to the immutability guarantee (specific
// equality, not a namespace prefix).
func TestTenantVAP_DoesNotExemptOtherServiceAccounts(t *testing.T) {
	_, _, exclude := loadTenantVAP(t)
	vars := map[string]any{"request": map[string]any{"userInfo": map[string]any{
		"username": "system:serviceaccount:loopzero-system:loopzero-operator",
		"groups":   []any{"system:authenticated", "system:serviceaccounts"},
	}}}
	if !evalCEL(t, exclude, vars) {
		t.Error("a non-loopzero-api SA must remain subject to the policy (matchCondition returned false = exempt)")
	}
}
