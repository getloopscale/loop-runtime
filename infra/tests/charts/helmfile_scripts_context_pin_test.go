// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Directory-wide regression-lock for issue #117 (and the #115 class it
// generalizes): a helmfile script that invokes `kubectl` without making
// its target cluster explicit silently writes to whatever kubecontext
// happens to be active. On a clean machine it passes; on a dev box
// carrying residue (e.g. `k3d-loopzero-secrets` left over from a vault
// bootstrap) it provisions into the WRONG CLUSTER and `task setup`
// dies mid-chain — or worse, succeeds against the wrong target.
//
// #115 fixed exactly one script (generate-secrets.sh). #117 observed
// that the same shape was latent across the whole directory and asked
// for "an audit + sweep ... not a per-script fix". This guard IS that
// sweep, expressed as an executable contract: it fails for ANY script
// in infra/helmfile/scripts/** that touches kubectl without pinning.
//
// FOUR pinning mechanisms are accepted. They are genuinely equivalent
// for the failure mode at hand — a guard that accepted only one would
// produce false positives and push authors toward pointless churn:
//
//	(1) Canonical wrapper — `KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-<ctx>}"`
//	    plus a `kubectl()` function override injecting `--context`.
//	    Safe BY CONSTRUCTION: every bare call downstream inherits it.
//	    Preferred for scripts with a fixed, known target cluster.
//
//	(2) Explicit `--context` on EVERY kubectl invocation. Safe, but
//	    fragile — one forgotten call reopens the hole.
//
//	(3) `export KUBECONFIG=<explicit>` before the first kubectl call,
//	    where the path comes from an argument. The script pins itself
//	    to a caller-supplied file (scoped-kubeconfig.sh does this).
//
//	(4) Caller-pinned contract made EXPLICIT AND FAIL-FAST via
//	    `: "${KUBECONFIG:?...}"`. Some scripts legitimately target a
//	    cluster chosen by the caller (generate-runtime-secrets.sh is
//	    invoked as `KUBECONFIG=$(k3d kubeconfig write loopzero-cell-0)
//	    bash ...`). Inheriting is fine; inheriting SILENTLY is not.
//	    The `:?` form aborts with a message when the caller forgot.
//
// SCOPE DISCIPLINE — read before extending:
//
//	This guard asserts that a target cluster is UNAMBIGUOUS and that
//	absence fails loudly. It deliberately does NOT assert WHICH
//	cluster is correct per script — that is a semantic question the
//	guard cannot answer without duplicating the deployment topology.
//	It also does not police the `command kubectl` escape hatch, which
//	is a deliberate raw bypass used by mechanism (1) itself.
//
// Cross-references:
//   - Issues: getloopscale/loopzero#117 (this sweep), #115 (the single-script precursor)
//   - Sibling single-script guard: generate_secrets_context_pin_test.go
//   - Canonical reference implementation: infra/helmfile/scripts/generate-secrets.sh
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const helmfileScriptsRelPath = "infra/helmfile/scripts"

// scriptsExemptFromContextPin lists paths that legitimately invoke
// kubectl without pinning. Each entry needs a reason — an unexplained
// exemption is how a guard rots into decoration.
var scriptsExemptFromContextPin = map[string]string{
	// Test scaffolding: these shim kubectl via PATH (mock-kubectl.sh)
	// and never reach a real cluster. Pinning a context here would
	// assert against a mock that ignores it.
	"test_helpers/test-generate-secrets.sh":         "hermetic test harness — kubectl is shimmed via mock-kubectl.sh on PATH",
	"test_helpers/test-generate-runtime-secrets.sh": "hermetic test harness — kubectl is shimmed via mock-kubectl.sh on PATH",
	"test_helpers/test-create-wiring-configmap.sh":  "hermetic test harness — kubectl is shimmed via mock-kubectl.sh on PATH",
	"test_helpers/test-idempotency.sh":              "hermetic test harness — kubectl is shimmed via mock-kubectl.sh on PATH",
	"test_helpers/mock-kubectl.sh":                  "IS the kubectl mock",
	"test_helpers/run-all.sh":                       "hermetic test harness runner",
}

var (
	// Mechanism (1a): KUBECTL_CONTEXT default assignment. The context
	// value is intentionally unconstrained — each script owns its target.
	reCtxDefault = regexp.MustCompile(`KUBECTL_CONTEXT="\$\{KUBECTL_CONTEXT:-[^"}]+\}"`)

	// Mechanism (1b): the kubectl() function override.
	reCtxWrapper = regexp.MustCompile(`kubectl\(\)\s*\{\s*command\s+kubectl\s+--context="\$KUBECTL_CONTEXT"\s+"\$@";?\s*\}`)

	// Mechanism (3): self-pin via explicit KUBECONFIG export.
	reKubeconfigExport = regexp.MustCompile(`(?m)^\s*export\s+KUBECONFIG=`)

	// Mechanism (4): caller-pinned contract, fail-fast when unset.
	// Matches `: "${KUBECONFIG:?...}"` — the idiomatic bash abort.
	reKubeconfigRequired = regexp.MustCompile(`:\s*"\$\{KUBECONFIG:\?`)

	// A kubectl invocation. Word-boundary-guarded so `kubectl()` (the
	// wrapper definition) and words like `mock-kubectl` do not match.
	reKubectlCall = regexp.MustCompile(`(^|[^a-zA-Z0-9_./-])kubectl\s`)
)

// stripShellComments removes full-line and trailing `#` comments so a
// kubectl mentioned in prose does not count as an invocation. Naive
// about `#` inside string literals; acceptable because a false
// "unpinned call" only ever makes the guard stricter, never laxer.
func stripShellComments(body string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// contextPinFinding describes one unpinned script.
type contextPinFinding struct {
	relPath       string
	unpinnedCalls int
}

// auditScriptContextPin returns a finding when the script invokes
// kubectl without any of the four accepted pinning mechanisms.
// A nil return means the script is safe.
func auditScriptContextPin(relPath, body string) *contextPinFinding {
	code := stripShellComments(body)

	calls := reKubectlCall.FindAllString(code, -1)
	if len(calls) == 0 {
		return nil // does not touch kubectl at all
	}

	// (1) canonical wrapper — safe by construction.
	if reCtxDefault.MatchString(code) && reCtxWrapper.MatchString(code) {
		return nil
	}
	// (3) self-pin via explicit KUBECONFIG export.
	if reKubeconfigExport.MatchString(code) {
		return nil
	}
	// (4) caller-pinned contract, asserted fail-fast.
	if reKubeconfigRequired.MatchString(code) {
		return nil
	}

	// (2) every invocation carries --context.
	unpinned := 0
	for _, line := range strings.Split(code, "\n") {
		if !reKubectlCall.MatchString(line) {
			continue
		}
		if !strings.Contains(line, "--context") {
			unpinned++
		}
	}
	if unpinned == 0 {
		return nil
	}
	return &contextPinFinding{relPath: relPath, unpinnedCalls: unpinned}
}

// collectHelmfileScripts walks infra/helmfile/scripts/** returning
// repo-relative-to-scripts-dir paths mapped to their bodies.
func collectHelmfileScripts(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	base := filepath.Join(root, helmfileScriptsRelPath)

	scripts := map[string]string{}
	walkErr := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".sh") {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		scripts[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", base, walkErr)
	}
	if len(scripts) == 0 {
		t.Fatalf("no .sh files found under %s — guard would pass vacuously", base)
	}
	return scripts
}

// TestHelmfileScriptsPinKubectlContext is the #117 sweep guard.
func TestHelmfileScriptsPinKubectlContext(t *testing.T) {
	scripts := collectHelmfileScripts(t)

	var findings []*contextPinFinding
	for rel, body := range scripts {
		if reason, exempt := scriptsExemptFromContextPin[rel]; exempt {
			t.Logf("exempt: %s (%s)", rel, reason)
			continue
		}
		if f := auditScriptContextPin(rel, body); f != nil {
			findings = append(findings, f)
		}
	}

	for _, f := range findings {
		t.Errorf("%s: %d kubectl invocation(s) with no pinned target cluster.\n"+
			"    The script inherits whatever kubecontext is active — issue #117 (generalizing #115).\n"+
			"    Fix with ONE of:\n"+
			"      (1) KUBECTL_CONTEXT=\"${KUBECTL_CONTEXT:-<ctx>}\" + kubectl() { command kubectl --context=\"$KUBECTL_CONTEXT\" \"$@\"; }\n"+
			"      (2) --context on every invocation\n"+
			"      (3) export KUBECONFIG=<explicit path> before the first call\n"+
			"      (4) : \"${KUBECONFIG:?...}\" when the caller is contractually responsible for pinning",
			f.relPath, f.unpinnedCalls)
	}
}

// TestHelmfileScriptsPinKubectlContext_NegativeControl proves the guard
// actually detects the regression, by feeding it the pre-#115 shape of
// the canonical script (wrapper + default stripped). A guard that stays
// green against a known-broken input is theatre.
func TestHelmfileScriptsPinKubectlContext_NegativeControl(t *testing.T) {
	scripts := collectHelmfileScripts(t)

	body, ok := scripts["generate-secrets.sh"]
	if !ok {
		t.Fatalf("negative control needs generate-secrets.sh — the canonical #115 reference implementation")
	}
	if auditScriptContextPin("generate-secrets.sh", body) != nil {
		t.Fatalf("setup failure: generate-secrets.sh should be PINNED today (it is the #115 fix)")
	}

	// Strip both halves of mechanism (1) → the pre-#115 shape.
	broken := reCtxWrapper.ReplaceAllString(body, "# wrapper removed for negative control")
	broken = reCtxDefault.ReplaceAllString(broken, "# default removed for negative control")
	if broken == body {
		t.Fatalf("negative-control setup failed: canonical wrapper/default not found in generate-secrets.sh")
	}

	f := auditScriptContextPin("generate-secrets.sh", broken)
	if f == nil {
		t.Fatalf("negative control did NOT fire: removing the canonical pin left the guard green — the guard cannot detect the #115/#117 regression it exists to catch")
	}
	if f.unpinnedCalls == 0 {
		t.Errorf("negative control fired but reported 0 unpinned calls — the count is meaningless to whoever reads the failure")
	}
}

// TestHelmfileScriptsContextPinExemptionsResolve keeps the exemption
// list honest: an entry for a file that no longer exists is a stale
// hole that would silently exempt a future file of the same name.
func TestHelmfileScriptsContextPinExemptionsResolve(t *testing.T) {
	scripts := collectHelmfileScripts(t)
	for rel, reason := range scriptsExemptFromContextPin {
		if _, ok := scripts[rel]; !ok {
			t.Errorf("exemption for %q (%s) does not resolve to a script on disk — remove the stale entry", rel, reason)
		}
	}
}
