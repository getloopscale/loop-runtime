// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// R2 of plan dev-public-cross-cluster-secrets (issue #111) — sponsor
// approved 2026-06-19. Extends the `openbao-init` chart's
// `write_auth_role` helper to render `bound_audiences=<csv>` when the
// role declares `boundAudiences` in its values block. Without
// bound_audiences, ESO's TokenRequest audience claim is not checked
// at OpenBao login → defence-in-depth gap (EC-5). Default-empty
// preserves backwards compat with existing roles that omit
// boundAudiences.
package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenbaoInitAuthRoleSupportsBoundAudiences locks R2 of the plan.
//
// Setup: render the chart with a single authRole declaring
// `boundAudiences: [aud-a, aud-b]`. The rendered ConfigMap MUST contain
// a `bound_audiences="aud-a,aud-b"` line in the `bao write
// auth/<mount>/role/<name>` block, AND a control role with NO
// boundAudiences MUST NOT emit the line (back-compat).
func TestOpenbaoInitAuthRoleSupportsBoundAudiences(t *testing.T) {
	chart := uniqueChart(t, "openbao-init")

	// Render the chart with a fixture authRole that carries
	// boundAudiences + a sibling control authRole that omits it.
	rendered := renderChart(t, chart, "t", map[string]string{
		"authMethods[0].path":                        "k8s-core",
		"authMethods[0].type":                        "kubernetes",
		"policies[0].name":                           "test-policy",
		"policies[0].ruleHcl":                        "path \"x\" { capabilities = [\"read\"] }",
		"authRoles[0].name":                          "role-with-audiences",
		"authRoles[0].mount":                         "k8s-core",
		"authRoles[0].boundServiceAccountNames":      "{sa-x}",
		"authRoles[0].boundServiceAccountNamespaces": "{ns-x}",
		"authRoles[0].boundAudiences":                "{aud-a,aud-b}",
		"authRoles[0].policies":                      "{test-policy}",
		"authRoles[0].ttl":                           "1h",
		"authRoles[1].name":                          "role-without-audiences",
		"authRoles[1].mount":                         "k8s-core",
		"authRoles[1].boundServiceAccountNames":      "{sa-y}",
		"authRoles[1].boundServiceAccountNamespaces": "{ns-y}",
		"authRoles[1].policies":                      "{test-policy}",
		"authRoles[1].ttl":                           "1h",
	})

	// T1. The role declaring boundAudiences MUST emit its CSV value
	// as a positional arg to write_auth_role. The helper consumes
	// arg7 ($bound_audiences) and, when non-empty, passes it as
	// `bound_audiences=...` to `bao write` (executed at Job run-time,
	// not rendered as a literal — so we assert on the input CSV the
	// chart wired into the call-site).
	if !strings.Contains(rendered, `"aud-a,aud-b"`) {
		t.Errorf("rendered output missing canonical CSV `\"aud-a,aud-b\"` at call-site — write_auth_role invocation does not pass boundAudiences as arg 7")
	}

	// T2. The helper has the conditional `bound_audiences="${7:-}"` +
	// guard `if [ -n "${bound_audiences}" ]` so roles that omit
	// boundAudiences produce a `bao write` WITHOUT the
	// `bound_audiences=` argument (back-compat with pre-R2 chart).
	if !strings.Contains(rendered, `bound_audiences="${7:-}"`) {
		t.Error("helper missing positional arg 7 capture `bound_audiences=\"${7:-}\"` — back-compat guard absent")
	}
	if !strings.Contains(rendered, `if [ -n "${bound_audiences}" ]`) {
		t.Error("helper missing conditional `if [ -n \"${bound_audiences}\" ]` — back-compat for roles without boundAudiences NOT preserved")
	}

	// T2b. The role-WITHOUT-audiences call-site MUST NOT contain a
	// stray literal `"aud-` CSV (defensive against accidental
	// hardcode that would override the helper's conditional).
	if strings.Count(rendered, `"aud-`) != 1 {
		t.Errorf("expected exactly 1 audience CSV literal in rendered output (role-with-audiences only); got %d", strings.Count(rendered, `"aud-`))
	}

	// T3. Traceability — the helper or value schema cites R2 / issue
	// #111 / dev-public-cross-cluster-secrets so future operator
	// understands why this defence-in-depth field exists.
	templateBody, terr := os.ReadFile(filepath.Join(chart, "templates", "configmap.yaml"))
	if terr != nil {
		t.Fatalf("read template: %v", terr)
	}
	valuesBody, verr := os.ReadFile(filepath.Join(chart, "values.yaml"))
	if verr != nil {
		t.Fatalf("read values: %v", verr)
	}
	combined := string(templateBody) + string(valuesBody)
	if !strings.Contains(combined, "issue #111") &&
		!strings.Contains(combined, "dev-public-cross-cluster-secrets") &&
		!strings.Contains(combined, "EC-5") {
		t.Error("R2 traceability comment missing in chart — cite `issue #111` / `dev-public-cross-cluster-secrets` / EC-5 in helper or values doc")
	}
}
