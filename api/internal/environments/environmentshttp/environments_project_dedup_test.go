// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED test for issue #142: `loopzero env list --project <p>` returns each
// environment TWICE — `production, preview, production, preview`.
//
// The duplicate is manufactured by the `?project=` filter itself. Red Team
// #48 added it to close a cross-project probe, and admitted two shapes:
//
//	e.Project == projectFilter   // the project-scoped entry
//	e.Project == ""              // the legacy shared entry (ADR D2)
//
// That second clause was correct WHEN IT WAS WRITTEN. Before the #124
// backfill, the legacy `production` row was the ONLY row a tenant had, and
// dropping it would have hidden a live environment. After the backfill the
// two shapes COEXIST for the same env name, so the predicate that used to
// guarantee visibility now guarantees a duplicate.
//
// The fix cannot be "delete the legacy clause": a tenant whose backfill has
// not run still carries ONLY the empty-Project row and would get an empty
// list — a working environment made invisible, strictly worse than a
// duplicate. Both properties must hold at once, so both are asserted here.
package environmentshttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// listEnvNamesFiltered drives List with a `?project=` filter and returns the
// env names in wire order, so a duplicate surfaces as a repeated element
// instead of being silently collapsed by a set.
func listEnvNamesFiltered(t *testing.T, envs []EnvironmentEntry, projectFilter string) []string {
	t.Helper()

	h := NewEnvironmentHandler(&fakeEnvMgr{envs: envs})

	url := "/api/v1/environments"
	if projectFilter != "" {
		url += "?project=" + projectFilter
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("List returned %d, want 200 — body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Environments []EnvironmentEntry `json:"environments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v — body: %s", err, w.Body.String())
	}

	names := make([]string, 0, len(body.Environments))
	for _, e := range body.Environments {
		names = append(names, e.Name)
	}
	return names
}

// TestEnvListProjectFilter_NoDuplicateWhenBackfilled is the #142 RED.
//
// The tenant is mid-migration: `production` exists BOTH as the legacy shared
// row (Project "") and as the backfilled project-scoped row (Project "web").
// A caller filtering by `web` must see each environment exactly once.
func TestEnvListProjectFilter_NoDuplicateWhenBackfilled(t *testing.T) {
	envs := []EnvironmentEntry{
		{Name: "production", Status: "active", Project: ""},    // legacy, pre-#124
		{Name: "preview", Status: "active", Project: ""},       // legacy, pre-#124
		{Name: "production", Status: "active", Project: "web"}, // backfilled by #124
		{Name: "preview", Status: "active", Project: "web"},    // backfilled by #124
	}

	got := listEnvNamesFiltered(t, envs, "web")

	seen := map[string]int{}
	for _, n := range got {
		seen[n]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("environment %q appears %d times in `env list --project web` (issue #142).\n"+
				"    got order: %v\n"+
				"    The `?project=` filter admits BOTH the project-scoped entry and the legacy\n"+
				"    empty-Project entry. Post-#124 backfill the two coexist for the same name, so\n"+
				"    the clause that used to guarantee visibility now guarantees a duplicate.\n"+
				"    The legacy row must be a FALLBACK for a name, not an addition to it.",
				name, count, got)
		}
	}

	if len(got) != 2 {
		t.Errorf("got %d environments %v, want exactly 2 (production, preview)", len(got), got)
	}
}

// TestEnvListProjectFilter_LegacyOnlyTenantStillSeesEnvs is the guard that
// stops the fix from becoming a regression of ADR D2.
//
// A tenant whose backfill has not run carries ONLY empty-Project rows.
// Deleting the legacy clause to kill the duplicate would return an EMPTY list
// here. This test is the reason the fix must be a per-name fallback rather
// than a stricter filter.
func TestEnvListProjectFilter_LegacyOnlyTenantStillSeesEnvs(t *testing.T) {
	envs := []EnvironmentEntry{
		{Name: "production", Status: "active", Project: ""},
		{Name: "preview", Status: "active", Project: ""},
	}

	got := listEnvNamesFiltered(t, envs, "web")

	if len(got) != 2 {
		t.Errorf("legacy-only tenant got %d environments %v, want 2 — ADR D2 says the shared\n"+
			"    empty-Project entry stays visible to every project until the #124 backfill runs.\n"+
			"    Dropping it to fix the duplicate would hide a live environment.", len(got), got)
	}
}

// TestEnvListProjectFilter_ExcludesOtherProjects proves the dedup does not
// widen the filter: entries belonging to a DIFFERENT project stay out. That
// is the cross-project property Red Team #48 bought, and a dedup that leaks
// it would trade one bug for a worse one.
func TestEnvListProjectFilter_ExcludesOtherProjects(t *testing.T) {
	envs := []EnvironmentEntry{
		{Name: "production", Status: "active", Project: "web"},
		{Name: "staging", Status: "active", Project: "api"},
	}

	got := listEnvNamesFiltered(t, envs, "web")

	for _, n := range got {
		if n == "staging" {
			t.Errorf("env list --project web leaked %q, which belongs to project \"api\" — the\n"+
				"    dedup must not widen the filter Red Team #48 installed. got: %v", n, got)
		}
	}
	if len(got) != 1 {
		t.Errorf("got %d environments %v, want exactly 1 (production)", len(got), got)
	}
}

// TestEnvListProjectFilter_PrefersProjectScopedRow asserts WHICH of the two
// rows survives when both exist. They are not interchangeable: the
// project-scoped row is the one whose 3-segment namespace
// `loopzero-<tenant>-<project>-<env>` routes post-M2. Returning the legacy row
// would dedup the list correctly and still hand the caller the wrong entry.
func TestEnvListProjectFilter_PrefersProjectScopedRow(t *testing.T) {
	envs := []EnvironmentEntry{
		{Name: "production", Status: "legacy-row", Project: ""},
		{Name: "production", Status: "scoped-row", Project: "web"},
	}

	h := NewEnvironmentHandler(&fakeEnvMgr{envs: envs})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments?project=web", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	var body struct {
		Environments []EnvironmentEntry `json:"environments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Environments) != 1 {
		t.Fatalf("got %d environments, want 1: %+v", len(body.Environments), body.Environments)
	}
	if got := body.Environments[0]; got.Project != "web" || got.Status != "scoped-row" {
		t.Errorf("survivor is %+v, want the project-scoped row (Project=\"web\").\n"+
			"    The two rows are NOT interchangeable — only the project-scoped one carries the\n"+
			"    3-segment namespace that routes post-M2. Deduping to the legacy row would return\n"+
			"    a correct-looking list pointing at the wrong environment.", got)
	}
}
