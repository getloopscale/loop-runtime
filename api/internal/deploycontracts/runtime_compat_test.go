// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deploycontracts

import "testing"

// simplify-deploy-loopzerokit-runtime-wiring T2.1 (F-dom-2): the build-plan
// language label is keyed on the services.json `runtime` ONLY (python|node).
// The legacy loopzero.yaml `framework` fallback (express→node, gin→go, etc.) was
// deleted with no backward compatibility (D1/D2). Anything that is not a
// supported runtime resolves to "unsupported".

func TestResolveLanguageStrategy_RuntimePython_ReturnsPython(t *testing.T) {
	if got := resolveLanguageStrategy("python"); got != "python" {
		t.Errorf(`expected "python", got %q`, got)
	}
}

func TestResolveLanguageStrategy_RuntimeNode_ReturnsNode(t *testing.T) {
	if got := resolveLanguageStrategy("node"); got != "node" {
		t.Errorf(`expected "node", got %q`, got)
	}
}

// Legacy framework strings, unknown values, and empty input all resolve to
// "unsupported" now — no Node default, no go/ruby/etc. mapping (D1/D4).
func TestResolveLanguageStrategy_NonRuntime_Unsupported(t *testing.T) {
	for _, v := range []string{"express", "nextjs", "fastapi", "gin", "ruby", "brainfuck", ""} {
		if got := resolveLanguageStrategy(v); got != "unsupported" {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want unsupported", v, got)
		}
	}
}
