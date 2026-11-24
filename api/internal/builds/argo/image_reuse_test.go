// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/pkg/naming"

	forge "github.com/getloopscale/argo-workflows-go"
)

const reusedTestDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// imageStepNames are the per-app steps that build, sign, scan and attest an
// image. A reused image needs none of them.
func imageStepNames(app string) []string {
	return []string{
		naming.BuildImageStepName(app),
		naming.SignStepName(app),
		naming.SBOMStepName(app),
		naming.AttestStepName(app),
	}
}

func hasTemplate(wf *forge.Workflow, name string) bool {
	for _, tmpl := range wf.Templates {
		if tmpl.GetName() == name {
			return true
		}
	}
	return false
}

func TestWorkflowBuilder_ReusedImageAppGetsNoImageSteps(t *testing.T) {
	params := testInfraParams()
	params.CosignAttestEnabled = true
	spec := testSpec()
	spec.Apps = []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: "apps/api", Runtime: "node"},
		{Name: "web", Type: "server", Port: 3001, Path: "apps/web", Runtime: "node"},
	}
	spec.Plan = planFromApps(spec.Apps)
	spec.Plan.Apps[0].ReusedDigest = reusedTestDigest

	wf, err := NewWorkflowBuilder(params).Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, entrypointName)
	for _, name := range imageStepNames("api") {
		if findTaskOpt(dag, name) != nil {
			t.Errorf("task %q present for an app whose image is reused", name)
		}
		if hasTemplate(wf, name) {
			t.Errorf("template %q present for an app whose image is reused", name)
		}
	}
	for _, name := range imageStepNames("web") {
		if findTaskOpt(dag, name) == nil {
			t.Errorf("task %q missing for an app that has no image yet", name)
		}
	}
	if _, err := wf.ToDict(); err != nil {
		t.Fatalf("workflow does not serialize: %v", err)
	}
}

func TestWorkflowBuilder_AllImagesReusedRunsOnlyTheClone(t *testing.T) {
	spec := testSpec()
	spec.Plan.Apps[0].ReusedDigest = reusedTestDigest

	wf, err := NewWorkflowBuilder(testInfraParams()).Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, entrypointName)
	if len(dag.Tasks) != 1 || dag.Tasks[0].Name != "clone-repo" {
		names := make([]string, 0, len(dag.Tasks))
		for _, task := range dag.Tasks {
			names = append(names, task.Name)
		}
		t.Errorf("DAG tasks = %v, want only clone-repo when no app needs an image", names)
	}
	if wf.OnExit != "" {
		t.Errorf("OnExit = %q, want none: no language step ran, so no cache to save", wf.OnExit)
	}
	if _, err := wf.ToDict(); err != nil {
		t.Fatalf("workflow does not serialize: %v", err)
	}
}
