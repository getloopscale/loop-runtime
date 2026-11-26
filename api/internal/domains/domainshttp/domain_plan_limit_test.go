// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// planOf answers the custom_domains_per_env limit of one plan and records
// what it was asked.
type planOf struct {
	plan      string
	limit     int
	err       error
	metric    string
	requested int
	asked     int
}

func (p *planOf) CheckValue(_ context.Context, _, metric string, requested int) (enforcement.LimitResult, error) {
	p.asked++
	p.metric, p.requested = metric, requested
	if p.err != nil {
		return enforcement.LimitResult{}, p.err
	}
	return enforcement.LimitResult{
		Allowed: p.limit > 0 && requested <= p.limit,
		Plan:    p.plan, Metric: metric, Current: requested, Limit: p.limit,
	}, nil
}

// envDomainStore counts the domains of the environment it is asked about and
// records whether a domain was created.
type envDomainStore struct {
	fakeDomainStore
	inEnvironment int
	filter        DomainListFilter
	created       int
}

func (s *envDomainStore) ListByTenant(_ context.Context, _ string, filter DomainListFilter) ([]CustomDomain, int, error) {
	s.filter = filter
	return nil, s.inEnvironment, nil
}

func (s *envDomainStore) Create(_ context.Context, _ *CustomDomain) error {
	s.created++
	return nil
}

func createDomain(t *testing.T, store DomainStore, limiter DomainPlanLimiter) *httptest.ResponseRecorder {
	t.Helper()
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	if limiter != nil {
		h.SetPlanLimiter(limiter)
	}
	body := `{"domain":"shop.example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Create(w, req)
	return w
}

func TestCreateRefusesADomainThePlanDoesNotInclude(t *testing.T) {
	store := &envDomainStore{}
	limiter := &planOf{plan: "starter", limit: 0}

	w := createDomain(t, store, limiter)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d body=%s", w.Code, w.Body.String())
	}
	if store.created != 0 {
		t.Fatalf("the refused domain was stored %d time(s)", store.created)
	}
	var answer struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		Current    int    `json:"current"`
		Max        int    `json:"max"`
		Plan       string `json:"plan"`
		UpgradeURL string `json:"upgrade_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	if answer.Code != "PLAN_LIMIT_EXCEEDED" || answer.Plan != "starter" || answer.Max != 0 || answer.Current != 0 {
		t.Fatalf("unexpected refusal: %s", w.Body.String())
	}
	if answer.UpgradeURL != "/billing" {
		t.Fatalf("want upgrade_url /billing, got %q", answer.UpgradeURL)
	}
	for _, part := range []string{"Starter", "0 custom domains per environment", "production", "Upgrade your plan"} {
		if !strings.Contains(answer.Message, part) {
			t.Fatalf("the message %q does not say %q", answer.Message, part)
		}
	}
}

func TestCreateCountsTheDomainsOfTheEnvironmentOnly(t *testing.T) {
	store := &envDomainStore{inEnvironment: 2}
	limiter := &planOf{plan: "pro", limit: 3}

	w := createDomain(t, store, limiter)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 for the third domain of a plan that allows 3, got %d body=%s", w.Code, w.Body.String())
	}
	if limiter.metric != "custom_domains_per_env" || limiter.requested != 3 {
		t.Fatalf("the plan was asked about %q with %d, want custom_domains_per_env with 3", limiter.metric, limiter.requested)
	}
	want := DomainListFilter{Project: "my-api", Environment: "production", Limit: 1}
	if store.filter != want {
		t.Fatalf("the count used filter %+v, want %+v", store.filter, want)
	}
}

func TestCreateRefusesTheDomainPastThePlanLimit(t *testing.T) {
	store := &envDomainStore{inEnvironment: 3}

	w := createDomain(t, store, &planOf{plan: "pro", limit: 3})

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402 for the fourth domain of a plan that allows 3, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Pro allows 3 custom domains per environment, and production has 3") {
		t.Fatalf("the refusal does not give the limit and the usage: %s", w.Body.String())
	}
	if store.created != 0 {
		t.Fatalf("the refused domain was stored %d time(s)", store.created)
	}
}

func TestCreateRefusesADomainWhenThePlanCannotBeRead(t *testing.T) {
	store := &envDomainStore{}

	w := createDomain(t, store, &planOf{err: errors.New("plan store down")})

	if w.Code < 500 {
		t.Fatalf("want a 5xx when the plan cannot be read, got %d body=%s", w.Code, w.Body.String())
	}
	if store.created != 0 {
		t.Fatalf("a domain was stored without a plan answer")
	}
}

func TestCreateAsksNoPlanWhereNoneIsEnforced(t *testing.T) {
	store := &envDomainStore{}

	w := createDomain(t, store, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 with no plan limiter, got %d body=%s", w.Code, w.Body.String())
	}
}

// The gate of the other routes answers with the address the platform is configured
// with (BILLING_UPGRADE_URL). This one answered "/billing" whatever was configured,
// which a person at the CLI cannot open.
func TestCreateRefusalNamesTheConfiguredUpgradeAddress(t *testing.T) {
	h := NewDomainHandler(&envDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.SetPlanLimiter(&planOf{plan: "starter", limit: 0})
	h.SetUpgradeURL("https://app.example.com/billing")
	body := `{"domain":"shop.example.com","project":"my-api","app":"web","environment":"production"}`
	req := routestest.WithAuth(httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body)), routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	var answer struct {
		Message    string `json:"message"`
		UpgradeURL string `json:"upgrade_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	if answer.UpgradeURL != "https://app.example.com/billing" {
		t.Errorf("upgrade_url = %q, want the configured address", answer.UpgradeURL)
	}
	if !strings.Contains(answer.Message, "Upgrade your plan at https://app.example.com/billing to continue.") {
		t.Errorf("the message %q does not name the upgrade address", answer.Message)
	}
}
