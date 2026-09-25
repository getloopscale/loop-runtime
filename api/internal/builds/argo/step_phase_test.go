// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	forge "github.com/getloopscale/loopzero-forge"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/events"
)

// Every task the builder puts in the build pipeline must land in a real UI
// phase: the SSE timeline, the step metric and the Loki labels all group by
// events.PhaseGroupForStep, and a step it does not know is shown as "other".
func TestEveryBuildPipelineTaskHasAPhase(t *testing.T) {
	specs := map[string][]deploycontracts.AppSpec{
		"node":   {{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
		"python": {{Name: "worker", Type: "worker", Runtime: "python"}},
		"multi-language": {
			{Name: "checkout", Type: "server", Port: 3000, Runtime: "node", Path: "web"},
			{Name: "billing", Type: "server", Port: 8000, Runtime: "python", Path: "svc"},
		},
	}
	infra := testInfraParams()
	infra.CosignAttestEnabled = true
	infra.HarborRegistryURL = "harbor.example.com"
	for name, apps := range specs {
		t.Run(name, func(t *testing.T) {
			spec := testSpec()
			spec.Apps = apps
			spec.Plan = planFromApps(apps)
			wf, err := NewWorkflowBuilder(infra).Build(spec)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			for _, task := range pipelineTasks(t, wf) {
				if got := events.PhaseGroupForStep(task); got == "other" {
					t.Errorf("task %q has no phase — the timeline shows it as other", task)
				}
			}
		})
	}
}

func pipelineTasks(t *testing.T, wf *forge.Workflow) []string {
	t.Helper()
	for _, tmpl := range wf.Templates {
		if dag, ok := tmpl.(*forge.DAG); ok && dag.Name == entrypointName {
			names := make([]string, 0, len(dag.Tasks))
			for _, task := range dag.Tasks {
				names = append(names, task.Name)
			}
			return names
		}
	}
	t.Fatal("no build-pipeline DAG in the workflow")
	return nil
}
