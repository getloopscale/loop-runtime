// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"testing"

	"github.com/getloopscale/loopzero/pkg/naming"
)

// A spec.environments the seed cannot read must stop the seed. Proceeding
// re-applies the Tenant with the baseline only, which strips every
// environment another journey added — the bug the merge exists to prevent.
func TestParseEnvironments_RefusesAnUnreadableList(t *testing.T) {
	if _, err := parseEnvironments([]byte(`[{"name":`)); err == nil {
		t.Fatal("malformed spec.environments must be an error")
	}
}

// Project is required by the Tenant schema and is the namespace's middle
// segment; dropping it on the round trip made the re-applied entry invalid.
func TestParseEnvironments_KeepsTheProject(t *testing.T) {
	envs, err := parseEnvironments([]byte(`[{"name":"j-solo","cluster":"workload","project":"solo"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0]["project"] != "solo" {
		t.Fatalf("envs = %v, want the project kept", envs)
	}
}

func TestMergeEnvs_BaselineEntriesCarryTheSeededProject(t *testing.T) {
	for _, e := range mergeEnvs(nil) {
		if e["project"] != ProjectName {
			t.Fatalf("baseline entry %v has no project %q", e, ProjectName)
		}
	}
}

// The seed waits for the namespaces the operator creates, so it must name them
// with the operator's formula rather than a copy of an older one.
func TestExpectedNamespaces_UseTheOperatorNamingFormula(t *testing.T) {
	want := map[string]bool{}
	for _, env := range []string{"production", "staging"} {
		ns, err := naming.Namespace(TenantID, ProjectName, env)
		if err != nil {
			t.Fatal(err)
		}
		want["k3d-loopzero-cell-0/"+ns] = true
	}
	build, err := naming.BuildNamespace(TenantID)
	if err != nil {
		t.Fatal(err)
	}
	want["k3d-loopzero-build/"+build] = true

	got, err := ExpectedNamespacesByContext()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, e := range got {
		if !want[e.context+"/"+e.name] {
			t.Errorf("unexpected namespace %s/%s", e.context, e.name)
		}
	}
}
