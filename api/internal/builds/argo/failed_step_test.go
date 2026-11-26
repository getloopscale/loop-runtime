// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"testing"

	forgemodel "github.com/getloopscale/argo-workflows-go/model"
)

// The node set of a build whose install step failed on every attempt. Argo
// ends this workflow Succeeded: the only leaf of the DAG, the SBOM task, has
// continueOn.failed, and that hides the failure of what it depends on.
func nodesOfAFailedInstall() map[string]forgemodel.NodeStatus {
	return map[string]forgemodel.NodeStatus{
		"wf":         {ID: "wf", DisplayName: "loopruntime-build-x", Type: "DAG", Phase: "Succeeded"},
		"clone":      {ID: "clone", DisplayName: "clone-repo", Type: "Retry", Phase: "Succeeded", StartedAt: "2026-11-25T22:00:00Z", Children: []string{"clone0"}},
		"clone0":     {ID: "clone0", DisplayName: "clone-repo(0)", Type: "Pod", Phase: "Succeeded", StartedAt: "2026-11-25T22:00:00Z"},
		"install":    {ID: "install", DisplayName: "install-deps", Type: "Retry", Phase: "Failed", Message: "Max duration limit exceeded", StartedAt: "2026-11-25T22:00:20Z", Children: []string{"install0", "install1"}},
		"install0":   {ID: "install0", DisplayName: "install-deps(0)", Type: "Pod", Phase: "Failed", Message: "main: Error (exit code 1)", StartedAt: "2026-11-25T22:00:20Z"},
		"install1":   {ID: "install1", DisplayName: "install-deps(1)", Type: "Pod", Phase: "Failed", Message: "main: Error (exit code 143)", StartedAt: "2026-11-25T22:00:50Z"},
		"dockerfile": {ID: "dockerfile", DisplayName: "generate-dockerfile", Type: "Skipped", Phase: "Omitted", Message: "omitted: depends condition not met"},
		"sbom":       {ID: "sbom", DisplayName: "sbom-app", Type: "Skipped", Phase: "Omitted"},
	}
}

func TestFailedRequiredStep_NamesTheStepThatFailedOnEveryAttempt(t *testing.T) {
	status := &forgemodel.WorkflowStatusDetail{Phase: "Succeeded", Nodes: nodesOfAFailedInstall()}

	step, message := failedRequiredStep(status)

	if step != "install-deps" {
		t.Errorf("step = %q, want install-deps", step)
	}
	// The attempts carry the exit code; the retry node only says it gave up.
	if message != "main: Error (exit code 143)" {
		t.Errorf("message = %q, want the last attempt's", message)
	}
}

func TestFailedRequiredStep_AnAttemptThatFailedBeforeARetrySucceededIsNotAFailure(t *testing.T) {
	nodes := nodesOfAFailedInstall()
	install := nodes["install"]
	install.Phase, install.Message = "Succeeded", ""
	nodes["install"] = install
	second := nodes["install1"]
	second.Phase, second.Message = "Succeeded", ""
	nodes["install1"] = second

	if step, _ := failedRequiredStep(&forgemodel.WorkflowStatusDetail{Phase: "Succeeded", Nodes: nodes}); step != "" {
		t.Errorf("step = %q, want none: the retry succeeded", step)
	}
}

// The SBOM is best effort: the build goes on without it.
func TestFailedRequiredStep_AFailedSBOMIsNotAFailure(t *testing.T) {
	nodes := map[string]forgemodel.NodeStatus{
		"sign": {ID: "sign", DisplayName: "sign-image-app", Type: "Pod", Phase: "Succeeded"},
		"sbom": {ID: "sbom", DisplayName: "sbom-app", Type: "Pod", Phase: "Failed", Message: "main: Error (exit code 1)"},
	}

	if step, _ := failedRequiredStep(&forgemodel.WorkflowStatusDetail{Phase: "Succeeded", Nodes: nodes}); step != "" {
		t.Errorf("step = %q, want none", step)
	}
}

func TestFailedRequiredStep_TheFirstFailureInTimeIsTheOneReported(t *testing.T) {
	nodes := map[string]forgemodel.NodeStatus{
		"scan":  {ID: "scan", DisplayName: "security-scan", Type: "Pod", Phase: "Failed", Message: "main: Error (exit code 2)", StartedAt: "2026-11-25T22:01:00Z"},
		"build": {ID: "build", DisplayName: "build-apps", Type: "Pod", Phase: "Error", Message: "pod deleted", StartedAt: "2026-11-25T22:00:30Z"},
	}

	step, message := failedRequiredStep(&forgemodel.WorkflowStatusDetail{Phase: "Succeeded", Nodes: nodes})

	if step != "build-apps" || message != "pod deleted" {
		t.Errorf("step = %q, message = %q, want build-apps and its message", step, message)
	}
}

func TestFailedRequiredStep_NothingFailed(t *testing.T) {
	if step, _ := failedRequiredStep(nil); step != "" {
		t.Errorf("step = %q for a nil status", step)
	}
}
