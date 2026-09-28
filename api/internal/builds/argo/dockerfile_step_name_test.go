// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// The Dockerfile step name was a real VARIATION POINT: the
// bring-your-own-Dockerfile path emitted `dockerfile-lint` while the language
// strategies emitted `generate-dockerfile`. That path was removed in T2.2 (see
// `user_dockerfile_strategy_test.go`, which asserts the step is ABSENT), and the
// variation collapsed without anything recording it.
//
// This test is the record. While it passes, the name does not vary and the
// constant is legitimate. If someone reintroduces a strategy that emits another
// name, it fails here — and the message says so: the variation point must COME
// BACK, not be worked around with a literal in the orchestrator.
//
// The previous version kept the variation by building a THROWAWAY container
// only to read its name (B-068). A throwaway construction tells nobody whether
// there is still variation; this test does.
func TestEveryStrategyEmitsTheSameDockerfileStepName(t *testing.T) {
	if len(runtimeRegistry) == 0 {
		t.Fatal("empty registry: a test that walks no strategy passes vacuously")
	}

	probe := []deploycontracts.AppSpec{{Name: "probe", Path: "."}}

	// The fallback is included on purpose: it emits a step too, and a divergence
	// there would produce the same defect in the DAG.
	strategies := map[string]LanguageStrategy{"<unsupported>": &unsupportedStrategy{runtime: "none"}}
	for runtime, d := range runtimeRegistry {
		strategies[runtime] = d.Strategy
	}
	if len(strategies) < 3 {
		t.Fatalf("only %d strateg(ies) walked; the real tree had 3 on 2026-08-18 "+
			"(node, python and the fallback) — the sweep shrank", len(strategies))
	}

	for name, s := range strategies {
		c := s.DockerfileStep(probe, 1)
		if c == nil {
			t.Errorf("strategy %s emitted no Dockerfile step", name)
			continue
		}
		if c.Name != dockerfileStepTemplate {
			t.Errorf("strategy %s emits step %q, and the constant says %q.\n\n"+
				"The name VARIES between strategies again. Restore the variation point — "+
				"the orchestrator must ask the strategy which step it emits — instead "+
				"of hard-coding a literal in the DAG.", name, c.Name, dockerfileStepTemplate)
		}
	}
}
