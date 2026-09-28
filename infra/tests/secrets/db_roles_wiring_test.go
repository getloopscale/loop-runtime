// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The request and worker roles must exist BEFORE the migrations run: 000062 and
// 000068 grant to them only if they are already there (#396). And every consumer
// of the database on the local path must connect as them, or loopzero-api refuses to
// boot on a pool that can bypass RLS.

func setupSteps(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../Taskfile.yml")
	if err != nil {
		t.Fatalf("read Taskfile.yml: %v", err)
	}
	var doc struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"` // strings and {task: …} maps, mixed
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse Taskfile.yml: %v", err)
	}
	var steps []string
	for _, c := range doc.Tasks["setup"].Cmds {
		if m, ok := c.(map[string]any); ok {
			if task, ok := m["task"].(string); ok {
				steps = append(steps, task)
			}
		}
	}
	if len(steps) == 0 {
		t.Fatal("task setup declares no task steps")
	}
	return steps
}

func indexOf(steps []string, name string) int {
	for i, s := range steps {
		if s == name {
			return i
		}
	}
	return -1
}

func TestSetupProvisionsTheRolesBeforeTheMigrations(t *testing.T) {
	steps := setupSteps(t)
	roles, migrate := indexOf(steps, "db:roles"), indexOf(steps, "db:migrate")
	if roles < 0 {
		t.Fatalf("task setup never runs db:roles; steps: %v", steps)
	}
	if migrate < 0 || roles > migrate {
		t.Errorf("db:roles (step %d) must run before db:migrate (step %d): the migrations grant to the roles only if they exist", roles, migrate)
	}
}

func TestTheTiltAPIConnectsThroughTheRoleSplit(t *testing.T) {
	raw, err := os.ReadFile("../../tilt/loopzero-api-values.yaml")
	if err != nil {
		t.Fatalf("read the Tilt values: %v", err)
	}
	var values struct {
		Secrets struct {
			DatabaseURL       string `yaml:"databaseUrl"`
			WorkerDatabaseURL string `yaml:"loopzeroWorkerDatabaseUrl"`
		} `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("parse the Tilt values: %v", err)
	}
	if !strings.HasPrefix(values.Secrets.DatabaseURL, "postgres://loopzero_api:") {
		t.Errorf("Tilt's DATABASE_URL does not connect as loopzero_api: %q", values.Secrets.DatabaseURL)
	}
	if !strings.HasPrefix(values.Secrets.WorkerDatabaseURL, "postgres://loopzero_worker:") {
		t.Errorf("Tilt's LOOPZERO_WORKER_DATABASE_URL does not connect as loopzero_worker: %q", values.Secrets.WorkerDatabaseURL)
	}
}
