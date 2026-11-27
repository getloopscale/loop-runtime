// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secretshttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// capLimiter allows up to cap secrets per environment and records what it
// was asked, so the test can assert the per-environment arithmetic.
type capLimiter struct {
	cap    int
	asked  []int
	metric string
}

func (c *capLimiter) CheckValue(_ context.Context, _, metric string, requested int) (enforcement.LimitResult, error) {
	c.asked = append(c.asked, requested)
	c.metric = metric
	return enforcement.LimitResult{Allowed: requested <= c.cap, Plan: "starter", Limit: c.cap, Current: requested}, nil
}

// unreadableLimiter cannot read the plan, which is what the limiter reports
// when its store is down.
type unreadableLimiter struct{}

func (unreadableLimiter) CheckValue(context.Context, string, string, int) (enforcement.LimitResult, error) {
	return enforcement.LimitResult{}, loopruntimeerrors.ErrPlatformDegraded
}

// envStore holds existing secrets per environment.
type envStore struct {
	byEnv map[string][]ports.SecretMetadata
	sets  int
}

func (s *envStore) ListNames(_ context.Context, _, _, env string) ([]ports.SecretMetadata, error) {
	return s.byEnv[env], nil
}
func (s *envStore) Set(context.Context, string, string, string, string, string, []byte) (bool, error) {
	s.sets++
	return true, nil
}
func (s *envStore) Delete(context.Context, string, string, string, string, string) (bool, error) {
	return true, nil
}

func TestSecretBulk_PlanGate_CountsAnUpdateOfAnExistingKeyAsNoAddition(t *testing.T) {
	store := &envStore{byEnv: map[string][]ports.SecretMetadata{
		"production": {{Name: "A"}, {Name: "B"}},
	}}
	limiter := &capLimiter{cap: 2}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetPlanLimiter(limiter)
	w := httptest.NewRecorder()

	h.Bulk(w, bulkRequest(t, []map[string]any{{"name": "A", "value": "new", "envs": []string{"production"}}}))

	if w.Code != http.StatusOK {
		t.Fatalf("rewriting an existing key at the cap must pass: got %d body=%s", w.Code, w.Body.String())
	}
	if limiter.metric != perEnvironmentLimitMetric || len(limiter.asked) != 1 || limiter.asked[0] != 2 {
		t.Fatalf("want one %q check for 2 secrets, got metric=%q asked=%v", perEnvironmentLimitMetric, limiter.metric, limiter.asked)
	}
}

func TestSecretBulk_PlanGate_RefusesTheEnvironmentThatWouldExceedItsCap(t *testing.T) {
	store := &envStore{byEnv: map[string][]ports.SecretMetadata{
		"staging": {{Name: "A"}},
	}}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetPlanLimiter(&capLimiter{cap: 1})
	w := httptest.NewRecorder()

	h.Bulk(w, bulkRequest(t, []map[string]any{
		{"name": "NEW_KEY", "value": "v", "envs": []string{"production", "staging"}},
	}))

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("staging holds 1 of 1: adding one must be refused, got %d body=%s", w.Code, w.Body.String())
	}
	if store.sets != 0 {
		t.Fatalf("a refused batch must write nothing, wrote %d", store.sets)
	}
}

func TestSecretBulk_PlanGate_RefusesWhenThePlanCannotBeRead(t *testing.T) {
	store := &envStore{byEnv: map[string][]ports.SecretMetadata{}}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetPlanLimiter(unreadableLimiter{})
	w := httptest.NewRecorder()

	h.Bulk(w, bulkRequest(t, []map[string]any{{"name": "NEW_KEY", "value": "v", "envs": []string{"production"}}}))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreadable plan must refuse, never allow: got %d body=%s", w.Code, w.Body.String())
	}
	if store.sets != 0 {
		t.Fatalf("a refused batch must write nothing, wrote %d", store.sets)
	}
}
