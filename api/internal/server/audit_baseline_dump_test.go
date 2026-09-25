// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/audit/audithttp"
	"github.com/getloopscale/loopzero/api/internal/billing/billinghttp"
	"github.com/getloopscale/loopzero/api/internal/domains/domainshttp"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
	"github.com/getloopscale/loopzero/api/internal/integrations/integrationshttp"
	"github.com/getloopscale/loopzero/api/internal/logs/logshttp"
	mwaudit "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loopzero/api/internal/release/argocdhttp"
	"github.com/getloopscale/loopzero/api/internal/release/promotehttp"
	"github.com/getloopscale/loopzero/api/internal/release/rollbackhttp"
	"github.com/getloopscale/loopzero/api/internal/repositories/repositorieshttp"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/scaling/scalinghttp"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"
	"github.com/getloopscale/loopzero/api/internal/teams/teamshttp"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// TestAuditBaselineDumpIsReproducible — B-059 Phase 1 T1.1 (NFR-002 regression floor).
//
// # PURPOSE
//
// Captures what `AuditLog` writes to `resourceID` today, per mutating route,
// so Phase 2 (delete body-snooper + rewire to context-declared id) can be
// diffed against a machine-readable baseline. The regression floor is: after
// migration, no route that was URL-sourced today may drop to source=none, and
// no route that was source=none today may drop out entirely (its slot must be
// declared or remain empty with the same explanation).
//
// # SCOPE OF THIS DUMP
//
// Bodyless synthetic requests, no query params — same shape as
// `TestEveryMutatingRouteRecordsItsDeclaredAction`. That means:
//
//   - URL-sourced routes (`extractResourceID(r.URL.Path)` returns non-empty)
//     are captured with `source=url | value=<id>`.
//   - Body-sourced routes (their id lives in the JSON body) show `source=none |
//     value=` — a legitimate baseline signal that says "this route relies on
//     body-snooping today, and after Phase 2 it must declare the id via the
//     `audit.SetResourceID` context surface".
//   - Query-sourced routes (POST /secrets uses `?project=`) also show
//     `source=none | value=` here because the test sends no query params.
//
// The plan's T1.1 originally cited "the route's canonical body (from
// `audit_actions_test.go` fixtures)"; measured 2026-09-04 that file tests
// `extractResourceID(path)` in isolation and holds no per-route body fixtures.
// Constructing bodies per route would require per-handler reading × ~24-32
// handlers. This dumper takes the parsimony-minimum shape that still gives a
// valid regression floor: post-migration, EVERY route that reports
// `source=none | value=` in this baseline must report `source=declared |
// value=<the declared id>` in the post-migration dump, or the migration
// regressed that route's audit path.
//
// # NOT A BLACK-BOX TEST OF THE MIDDLEWARE
//
// The single assertion here is "the baseline file was written with ≥ 17 rows"
// — enough to prove the dumper ran and the fixture reached the audited group.
// The baseline file itself is the artifact Phase 2 diffs against; Phase 2
// carries its own tests that assert the diff shape.
func TestAuditBaselineDumpIsReproducible(t *testing.T) {
	type row struct{ method, path, source, value string }
	var rows []row

	// A capturing writer that records the last resourceID seen. Reset per
	// request; the dump loop reads it after each ServeHTTP.
	var lastResourceID string
	writer := mwaudit.AuditWriter(func(_, _, _, _, resourceID, _ string, _ map[string]any) {
		lastResourceID = resourceID
	})

	cfg := Config{
		TokenValidator:            fixedTokenValidator{},
		MemberStore:               fixedMembership{},
		AuditWriter:               writer,
		BuildHandler:              &builds.BuildHandler{},
		BuildAttestationHandler:   &builds.BuildAttestationHandler{},
		BuildSBOMHandler:          &builds.BuildSBOMHandler{},
		ArgoCallbackHandler:       &argocdhttp.ArgoCallbackHandler{},
		LogsHandler:               &logshttp.LogsHandler{},
		LogStreamHandler:          &logshttp.LogStreamHandler{},
		RollbackHandler:           &rollbackhttp.RollbackHandler{},
		PromoteHandler:            &promotehttp.PromoteHandler{},
		StatusHandler:             &statushttp.StatusHandler{},
		ScaleHandler:              &scalinghttp.ScaleHandler{},
		BuildListHandler:          &builds.BuildListHandler{},
		BuildStatsHandler:         &builds.BuildStatsHandler{},
		EnvironmentHandler:        &environmentshttp.EnvironmentHandler{},
		TeamHandler:               &teamshttp.TeamHandler{},
		DomainHandler:             &domainshttp.DomainHandler{},
		RepositoryHandler:         &repositorieshttp.RepositoryHandler{},
		IntegrationsGitHubHandler: &integrationshttp.IntegrationsGitHubHandler{},
		AuditHandler:              &audithttp.AuditHandler{},
		BillingHandler:            &billinghttp.BillingHandler{},
		Guards:                    RouteGuards{RateLimiter: &ratelimit.RateLimiter{}, IPRateLimit: &ratelimit.IPRateLimiter{}, VerifyRateLimiter: &ratelimit.VerifyRateLimiter{}, PlanLimiter: &enforcement.PlanLimiter{}, CostEnforcer: &policy.CostEnforcer{}},
	}
	r := NewRouter(cfg)

	rotas := mutatingRoutes(t, r)
	if len(rotas) < 17 {
		t.Fatalf("baseline dumper walked %d mutating route(s); fixture must reach the audited group "+
			"(the same ≥17 floor that TestEveryMutatingRouteRecordsItsDeclaredAction enforces).", len(rotas))
	}

	auditar := mwaudit.AuditLog(slog.New(slog.NewTextHandler(io.Discard, nil)), writer)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	for _, rt := range rotas {
		lastResourceID = ""
		req := httptest.NewRequest(rt.method, concretePath(rt.pattern), nil)
		auditar.ServeHTTP(httptest.NewRecorder(), withFixedUser(req))
		src := "none"
		if lastResourceID != "" {
			src = "url"
		}
		rows = append(rows, row{
			method: rt.method, path: rt.pattern,
			source: src, value: lastResourceID,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].path == rows[j].path {
			return rows[i].method < rows[j].method
		}
		return rows[i].path < rows[j].path
	})

	// Locate the records dir. Test's cwd is the package dir (api/internal/server/),
	// so records/ is up four levels (api/ → repo root → .claude/records/).
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	baselineDir := filepath.Join(repoRoot, ".claude", "records", "alignment")
	if err := os.MkdirAll(baselineDir, 0o755); err != nil {
		t.Fatalf("create baseline dir: %v", err)
	}
	baselinePath := filepath.Join(baselineDir, "b059-baseline-resource-id-coverage.txt")

	var sb strings.Builder
	sb.WriteString("# B-059 Phase 1 T1.1 baseline — resource_id coverage per mutating route\n")
	sb.WriteString("#\n")
	sb.WriteString("# Captured with bodyless synthetic requests + no query params via\n")
	sb.WriteString("# TestAuditBaselineDumpIsReproducible. Rows where source=none rely on body/query\n")
	sb.WriteString("# snooping today and must be migrated to declare their id via\n")
	sb.WriteString("# audit.SetResourceID in Phase 1 T1.2. Rows where source=url are stable.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Format: <METHOD> <path> | source=<url|none> | value=<id-or-empty>\n")
	sb.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s %s | source=%s | value=%s\n",
			r.method, r.path, r.source, r.value)
	}

	if err := os.WriteFile(baselinePath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	// AC-T1.1.1: baseline file exists with ≥ 17 rows (matches chi.Walk fixture coverage).
	info, err := os.Stat(baselinePath)
	if err != nil {
		t.Fatalf("stat baseline: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("baseline written empty")
	}
	if len(rows) < 17 {
		t.Fatalf("baseline captured %d row(s); floor is 17 (same as the chi.Walk fixture)", len(rows))
	}
	t.Logf("baseline written: %s (%d rows)", baselinePath, len(rows))
}
