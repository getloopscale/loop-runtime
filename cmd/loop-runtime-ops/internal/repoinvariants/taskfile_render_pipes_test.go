// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Regression-lock: no Taskfile command pipes a Helm render into kubeconform.
//
// `task validate:manifests` validated nine charts as
// `helm template … | kubeconform --strict --summary`. go-task runs a command
// without `pipefail`, so the pipeline's status was kubeconform's alone: a
// render that failed handed kubeconform an empty stdin, it validated
// "0 resource found" and exited 0, and the task passed having validated
// nothing for that chart (#392). On a fresh clone it already did so for
// Velero, whose chart dependency is not vendored.
//
// Those renders go through `loop-runtime-ops validate-chart`, which builds the chart's
// dependencies, fails on a render error or an empty render and names the
// chart. This test keeps the pipe from coming back under any task.
package repoinvariants

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
	"gopkg.in/yaml.v3"
)

// reRenderPipedToKubeconform matches a helm or helmfile `template` render
// whose output is piped, directly or further down the pipeline, into
// kubeconform.
var reRenderPipedToKubeconform = regexp.MustCompile(`\bhelm(?:file)?\b[^\n|]*\btemplate\b[^\n]*\|[^\n]*\bkubeconform\b`)

func TestNoTaskfileCommandPipesARenderIntoKubeconform(t *testing.T) {
	repoRoot := qtestutil.MustRepoRoot()
	for _, tf := range taskfilesToScan {
		body, err := os.ReadFile(filepath.Join(repoRoot, tf))
		if err != nil {
			t.Fatalf("read %s: %v", tf, err)
		}
		var doc scriptRefTaskfile
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("parse %s: %v", tf, err)
		}
		for name, task := range doc.Tasks {
			for _, c := range task.Cmds {
				cmd := commandText(c)
				if reRenderPipedToKubeconform.MatchString(cmd) {
					t.Errorf("%s task %q pipes a render into kubeconform:\n    %s\n"+
						"    Without pipefail a render that fails validates \"0 resource found\" and passes.\n"+
						"    Validate the chart with `loop-runtime-ops validate-chart` instead (#392).",
						tf, name, cmd)
				}
			}
		}
	}
}
