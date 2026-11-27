// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ports

import "testing"

func TestFindEnvironmentMatchesTheProjectAndNameTuple(t *testing.T) {
	entries := []EnvironmentEntry{{Name: "staging", Project: "web"}, {Name: "staging", Project: "api"}}

	got := FindEnvironment(entries, "api", "staging")

	if got == nil || got.Project != "api" {
		t.Fatalf("FindEnvironment = %+v, want the api/staging entry", got)
	}
}

// The m37 defect: matching by name alone found another project's env of the
// same name, so the new (project, env) tuple was never added.
func TestFindEnvironmentIgnoresTheSameNameInAnotherProject(t *testing.T) {
	entries := []EnvironmentEntry{{Name: "staging", Project: "web"}}

	if got := FindEnvironment(entries, "api", "staging"); got != nil {
		t.Fatalf("FindEnvironment = %+v, want nil", got)
	}
}

func TestFindEnvironmentMatchesALegacyEntryOnlyForTheEmptyProject(t *testing.T) {
	entries := []EnvironmentEntry{{Name: "production"}}

	if FindEnvironment(entries, "", "production") == nil {
		t.Fatal("a legacy (project-less) entry must match an empty project")
	}
	if FindEnvironment(entries, "web", "production") != nil {
		t.Fatal("a legacy entry must not match a named project")
	}
}
