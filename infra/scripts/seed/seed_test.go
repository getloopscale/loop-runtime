// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"strings"
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

// Migration 000044 made tenants.account_id NOT NULL, and 000043/000042 did the
// same for subscriptions.account_id and moved its unique key from tenant_id to
// account_id. The E2E seed kept the old shape and failed on every migrated
// database: `null value in column "account_id" of relation "tenants"`.
func TestSeedSQL_GivesTheE2ETenantAndSubscriptionAnAccount(t *testing.T) {
	sql, err := seedSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"INSERT INTO accounts (id, display_name, plan, owner_id)\nVALUES ('" + AccountID + "'",
		"INSERT INTO tenants (id, display_name, slug, owner_id, cell_id, account_id)",
		"INSERT INTO subscriptions (id, tenant_id, account_id, plan, status)",
		"ON CONFLICT (account_id) DO UPDATE SET plan = 'enterprise'",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("seed SQL lacks %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "ON CONFLICT (tenant_id)") {
		t.Errorf("seed SQL still targets the subscriptions tenant_id key migration 000042 dropped:\n%s", sql)
	}
}

// The bootstrap user's single-tenant rule comes from the shared package, so the
// E2E seed cannot drift from the local and drill seeds again.
func TestSeedSQL_LeavesTheBootstrapUserInTheE2ETenantOnly(t *testing.T) {
	sql, err := seedSQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "DELETE FROM tenant_members WHERE user_id = 'loopzero-bootstrap' AND tenant_id <> '" + TenantID + "';"
	if !strings.Contains(sql, want) {
		t.Errorf("seed SQL lacks %q:\n%s", want, sql)
	}
	// The bootstrap membership must follow the tenant it references.
	if strings.Index(sql, want) < strings.Index(sql, "INSERT INTO tenants") {
		t.Errorf("bootstrap membership rule runs before the E2E tenant exists:\n%s", sql)
	}
}
