// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package silentsurface

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oneRecordedOneSilent has the canonical shape (records in both arms) beside the
// shape the ratchet exists to catch.
const oneRecordedOneSilent = `package server
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	if cfg.GitHubWebhookHandler != nil {
		r.Post("/api/v1/webhooks/github", cfg.GitHubWebhookHandler.ServeHTTP)
		_ = cfg.Surfaces.Record(surfaces.GitHubWebhook, true, "")
	} else {
		_ = cfg.Surfaces.Record(surfaces.GitHubWebhook, false, "secret absent")
	}
	if cfg.AdminEvictor != nil {
		r.Post("/api/v1/admin/evict", cfg.AdminEvictor.Handle)
	}
	return r
}`

func TestAnalyze_CountsAGuardedSurfaceWithoutARecord(t *testing.T) {
	got, err := Analyze(oneRecordedOneSilent)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got.Silent) != 1 {
		t.Fatalf("silent = %d, want 1: %+v", len(got.Silent), got.Silent)
	}
	if got.Silent[0].Field != "AdminEvictor" {
		t.Errorf("field = %q, want AdminEvictor", got.Silent[0].Field)
	}
	if got.Guarded != 2 {
		t.Errorf("guarded = %d, want 2", got.Guarded)
	}
}

// The canonical form puts the report in the guard's CONDITION. A gate reading
// only the body would call it silent, and the answer would be to go back to the
// verbose form to please the gate.
func TestAnalyze_RecordedFormInTheConditionCounts(t *testing.T) {
	const viaHelper = `package server
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	if surfaces.Recorded(cfg.Surfaces, surfaces.Bootstrap, cfg.BootstrapStore != nil, "reason") {
		r.Post("/internal/bootstrap", h)
	}
	return r
}`
	got, err := Analyze(viaHelper)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Guarded != 1 {
		t.Fatalf("guarded = %d, want 1", got.Guarded)
	}
	if len(got.Silent) != 0 {
		t.Errorf("the Recorded form was counted as silent: %+v", got.Silent)
	}
}

// Not every `if cfg.X != nil` registers a surface: some mount middleware.
// Counting them would inflate the baseline with work that does not exist.
func TestAnalyze_GuardWithoutARouteDoesNotCount(t *testing.T) {
	const noRoute = `package server
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	if cfg.IPRateLimit != nil {
		r.Use(cfg.IPRateLimit.Middleware)
	}
	return r
}`
	got, err := Analyze(noRoute)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got.Silent) != 0 {
		t.Errorf("middleware was counted as a surface: %+v", got.Silent)
	}
}

// A call on something that merely starts with "r" is not a route registration:
// the router is identified by what it is, not by its first letter.
func TestAnalyze_ARepositoryGetIsNotARoute(t *testing.T) {
	const notARouter = `package server
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	if cfg.Repo != nil {
		repo.Get("key")
		registry.Delete("key")
		resp.Header().Get("X")
	}
	return r
}`
	got, err := Analyze(notARouter)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Guarded != 0 {
		t.Fatalf("guarded = %d, want 0: repo/registry/resp are not the router", got.Guarded)
	}
}

// A chi.Router parameter (a Group or Route closure) is a router, and a chain
// that starts at it (r.With(mw).Post) registers a route.
func TestAnalyze_RouterParameterAndWithChainCount(t *testing.T) {
	const grouped = `package server
func NewRouter(cfg Config) *chi.Mux {
	mux := chi.NewRouter()
	mux.Group(func(g chi.Router) {
		if cfg.Upload != nil {
			g.With(auth).Post("/upload", cfg.Upload.ServeHTTP)
		}
	})
	return mux
}`
	got, err := Analyze(grouped)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Guarded != 1 || len(got.Silent) != 1 {
		t.Fatalf("guarded=%d silent=%v, want the grouped route counted as one silent surface", got.Guarded, got.Silent)
	}
}

// The prescribed fix must not tell authors to discard the Record error.
func TestActionableFixDoesNotPrescribeDiscardingTheError(t *testing.T) {
	if strings.Contains(ActionableFix, "_ = cfg.Surfaces.Record") {
		t.Fatalf("the fix template swallows the Record error:\n%s", ActionableFix)
	}
	if !strings.Contains(ActionableFix, "surfaces.Recorded(") {
		t.Fatalf("the fix template must show the Recorded form:\n%s", ActionableFix)
	}
}

func TestEvaluate_RefusesAnIncreaseOverTheBaseline(t *testing.T) {
	out := Evaluate(Outcome{Guarded: 5, Silent: []Site{
		{Field: "A", Line: 1}, {Field: "B", Line: 2}, {Field: "C", Line: 3}, {Field: "D", Line: 4},
	}}, 3)
	if out.Pass {
		t.Fatal("an increase over the baseline was approved")
	}
	if !strings.Contains(out.Summary(), "4") || !strings.Contains(out.Summary(), "3") {
		t.Errorf("the summary does not confront measured with baseline: %s", out.Summary())
	}
}

// Real homing is never refused — that is why the ratchet is monotonic rather
// than an equality.
func TestEvaluate_ADecreasePasses(t *testing.T) {
	out := Evaluate(Outcome{Guarded: 5, Silent: []Site{{Field: "A", Line: 1}}}, 3)
	if !out.Pass {
		t.Fatalf("a decrease in debt was refused: %s", out.Summary())
	}
}

func TestEvaluate_EqualToTheBaselinePasses(t *testing.T) {
	out := Evaluate(Outcome{Guarded: 5, Silent: []Site{{Field: "A", Line: 1}}}, 1)
	if !out.Pass {
		t.Fatalf("equal to the baseline was refused: %s", out.Summary())
	}
}

// "Nothing to check" and "all clean" are different facts. A fixture with zero
// guards is legitimate for Analyze; the REAL router with none means it changed
// shape and the gate stopped recognising it.
func TestCheck_RouterWithNoGuardIsAFailureNotAPass(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.Dir(RouterPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := "package server\nfunc NewRouter(cfg Config) {}\n"
	if err := os.WriteFile(filepath.Join(root, RouterPath), []byte(src), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("a router with no guard was accepted as clean")
	}
}

func TestCheck_MissingRouterFails(t *testing.T) {
	if _, err := Check(Config{Root: t.TempDir()}); err == nil {
		t.Fatal("a missing router was accepted")
	}
}

func TestAnalyze_InvalidSourceFails(t *testing.T) {
	if _, err := Analyze("this is not Go"); err == nil {
		t.Fatal("invalid source was accepted")
	}
}
