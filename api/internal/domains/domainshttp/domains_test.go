// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domainshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// --- Fake DomainStore ---

type fakeDomainStore struct {
	domain       *CustomDomain
	domains      []CustomDomain
	total        int
	count        int
	createErr    error
	deleteErr    error
	transErr     error
	updateVerErr error
	getErr       error
}

func (f *fakeDomainStore) Create(_ context.Context, _ *CustomDomain) error {
	return f.createErr
}

func (f *fakeDomainStore) GetByDomainAndTenant(_ context.Context, _, _ string) (*CustomDomain, error) {
	return f.domain, f.getErr
}

func (f *fakeDomainStore) ListByTenant(_ context.Context, _ string, _ DomainListFilter) ([]CustomDomain, int, error) {
	return f.domains, f.total, nil
}

func (f *fakeDomainStore) CountByTenant(_ context.Context, _ string) (int, error) {
	return f.count, nil
}

func (f *fakeDomainStore) UpdateStatus(_ context.Context, _, _ string) error { return nil }

func (f *fakeDomainStore) TransitionStatus(_ context.Context, _, _, _ string) error {
	return f.transErr
}

func (f *fakeDomainStore) UpdateVerified(_ context.Context, _ string, _ time.Time) error {
	return f.updateVerErr
}

func (f *fakeDomainStore) UpdateTLS(_ context.Context, _, _ string, _ *time.Time) error { return nil }
func (f *fakeDomainStore) UpdateFailure(_ context.Context, _, _, _ string) error        { return nil }

func (f *fakeDomainStore) Delete(_ context.Context, _ string) error {
	return f.deleteErr
}

func (f *fakeDomainStore) CreateWithCFZone(_ context.Context, _ *CustomDomain) error { return nil }
func (f *fakeDomainStore) UpdateCFZoneStatus(_ context.Context, _, _ string) error   { return nil }
func (f *fakeDomainStore) FindPendingNS(_ context.Context) ([]CustomDomain, error)   { return nil, nil }
func (f *fakeDomainStore) FindNSActive(_ context.Context) ([]CustomDomain, error)    { return nil, nil }

// --- Fake DomainVerifier ---

type fakeDomainVerifier struct {
	verified bool
	err      error
}

func (f *fakeDomainVerifier) VerifyTXT(_ context.Context, _, _ string) (bool, error) {
	return f.verified, f.err
}

// --- Fake DomainValidator ---

type fakeDomainValidator struct {
	err error
}

func (f *fakeDomainValidator) Validate(_ string) error {
	return f.err
}

// --- Tests ---

func TestDomains_Create_HappyPath_V1(t *testing.T) {
	store := &fakeDomainStore{}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("want ok=true")
	}
	if resp.Domain != "example.com" {
		t.Fatalf("want domain example.com, got %s", resp.Domain)
	}
	if resp.Status != "pending_verification" {
		t.Fatalf("want status pending_verification, got %s", resp.Status)
	}
}

func TestDomains_Create_MissingFields(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	body := `{"domain":"example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Create_InvalidDomainFormat(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{
		err: errors.New("invalid domain format"),
	}, "apps.dev.getloopscale.app")

	body := `{"domain":"not a domain","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Create_AlreadyExists(t *testing.T) {
	store := &fakeDomainStore{
		createErr: ports.ErrDomainExists,
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Create_QuotaExceeded(t *testing.T) {
	store := &fakeDomainStore{count: 25}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_List_HappyPath(t *testing.T) {
	store := &fakeDomainStore{
		domains: []CustomDomain{
			{ID: "dom-1", Domain: "example.com", Status: "active"},
		},
		total: 1,
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("want total 1, got %d", resp.Total)
	}
}

func TestDomains_Get_Found(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			ID:       "dom-1",
			Domain:   "example.com",
			Status:   "active",
			TenantID: "tn-test-123",
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Get_NotFound(t *testing.T) {
	store := &fakeDomainStore{domain: nil}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/nonexistent.com", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "nonexistent.com")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Verify_TXTMatch(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:            "example.com",
			Status:            "pending_verification",
			VerificationToken: "REDACTED_SECRET_055",
		},
	}
	verifier := &fakeDomainVerifier{verified: true}
	h := NewDomainHandler(store, verifier, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainVerifyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "verified" {
		t.Fatalf("want status verified, got %s", resp.Status)
	}
}

func TestDomains_Verify_TXTNotFound(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:            "example.com",
			Status:            "pending_verification",
			VerificationToken: "REDACTED_SECRET_055",
		},
	}
	verifier := &fakeDomainVerifier{verified: false}
	h := NewDomainHandler(store, verifier, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainVerifyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "pending_verification" {
		t.Fatalf("want status pending_verification, got %s", resp.Status)
	}
}

func TestDomains_Verify_DomainNotFound(t *testing.T) {
	store := &fakeDomainStore{domain: nil}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/nonexistent.com/verify", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "nonexistent.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Verify_AlreadyVerified(t *testing.T) {
	now := time.Now()
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:     "example.com",
			Status:     "verified",
			VerifiedAt: &now,
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Delete_HappyPath(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:   "example.com",
			Status:   "active",
			TenantID: "tn-test-123",
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/domains/example.com", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestDomains_Delete_AuthorizationAndStateMatrix consolidates 5 near-clone
// tests for DomainHandler.Delete authorization + state-validation paths.
// Phase 3 helper: each case previously had a ~14-line scaffold differing
// only in (store-setup, auth context, expected HTTP status).
func TestDomains_Delete_AuthorizationAndStateMatrix(t *testing.T) {
	cases := []struct {
		name      string
		store     *fakeDomainStore
		setupAuth func(req *http.Request) *http.Request // nil for unauthenticated
		wantCode  int
	}{
		{
			name:  "NotFound",
			store: &fakeDomainStore{domain: nil},
			setupAuth: func(req *http.Request) *http.Request {
				return routestest.WithAuth(req, routestest.DefaultUserContext())
			},
			wantCode: http.StatusNotFound,
		},
		{
			name: "ConcurrentModification",
			store: &fakeDomainStore{
				domain:   &CustomDomain{Domain: "example.com", Status: "active"},
				transErr: errors.New("concurrent modification"),
			},
			setupAuth: func(req *http.Request) *http.Request {
				return routestest.WithAuth(req, routestest.DefaultUserContext())
			},
			wantCode: http.StatusConflict,
		},
		{
			name:  "InvalidState",
			store: &fakeDomainStore{domain: &CustomDomain{Domain: "example.com", Status: "removing"}},
			setupAuth: func(req *http.Request) *http.Request {
				return routestest.WithAuth(req, routestest.DefaultUserContext())
			},
			wantCode: http.StatusConflict,
		},
		{
			name:      "Unauthenticated",
			store:     &fakeDomainStore{},
			setupAuth: nil,
			wantCode:  http.StatusUnauthorized,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewDomainHandler(c.store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/domains/example.com", nil)
			if c.setupAuth != nil {
				req = c.setupAuth(req)
			}
			req = routestest.WithChiParam(req, "domain", "example.com")
			w := httptest.NewRecorder()
			h.Delete(w, req)
			if w.Code != c.wantCode {
				t.Fatalf("want %d, got %d body=%s", c.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

// Original TestDomains_Delete_Unauthenticated consolidated into
// TestDomains_Delete_AuthorizationAndStateMatrix/Unauthenticated (Phase 3 v28).

func TestDomains_List_Unauthenticated(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Get_Unauthenticated(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com", nil)
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Create_Unauthenticated(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Create_InvalidJSON(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString("not-json"))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Verify_Unauthenticated(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_Get_CFManagedDomain(t *testing.T) {
	zoneID := "zone-123"
	store := &fakeDomainStore{
		domain: &CustomDomain{
			ID:            "dom-1",
			Domain:        "example.com",
			Status:        "active",
			CFZoneID:      &zoneID,
			CFNameservers: []string{"ns1.cloudflare.com", "ns2.cloudflare.com"},
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.CFManaged {
		t.Fatal("want cf_managed=true")
	}
	if len(resp.Nameservers) != 2 {
		t.Fatalf("want 2 nameservers, got %d", len(resp.Nameservers))
	}
}

func TestDomains_Get_PendingVerificationShowsToken(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			ID:                "dom-1",
			Domain:            "example.com",
			Status:            "pending_verification",
			VerificationToken: "REDACTED_SECRET_055",
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext()) // owner
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Verification == nil {
		t.Fatal("want verification info for pending_verification domain")
	}
	if resp.Verification.TXTValue != "REDACTED_SECRET_055" {
		t.Fatalf("want token REDACTED_SECRET_055, got %s", resp.Verification.TXTValue)
	}
}

func TestDomains_List_WithPagination(t *testing.T) {
	store := &fakeDomainStore{
		domains: []CustomDomain{},
		total:   0,
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains?limit=10&offset=5&project=my-api", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Limit != 10 {
		t.Fatalf("want limit=10, got %d", resp.Limit)
	}
	if resp.Offset != 5 {
		t.Fatalf("want offset=5, got %d", resp.Offset)
	}
}

func TestDomains_Verify_DNSLookupError(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:            "example.com",
			Status:            "pending_verification",
			VerificationToken: "REDACTED_SECRET_055",
		},
	}
	verifier := &fakeDomainVerifier{err: errors.New("dns lookup failed")}
	h := NewDomainHandler(store, verifier, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainVerifyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.VerificationError == "" {
		t.Fatal("want verification error when DNS lookup fails")
	}
}

func TestDomains_Verify_DeprecatedWhenCFZone(t *testing.T) {
	store := &fakeDomainStore{}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.cfZone = &fakeCFZoneManager{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.Verify(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("want 410 (deprecated), got %d body=%s", w.Code, w.Body.String())
	}
}

// --- Fake CFZoneManager ---

type fakeCFZoneManager struct {
	zoneResult *CFZoneResult
	zoneErr    error
	records    []CFDNSRecord
	recordErr  error
	created    *CFDNSRecord
	createErr  error
	// deleteZoneErr fails DeleteZone; deletedZones records every attempt.
	deleteZoneErr error
	deletedZones  []string
}

func (f *fakeCFZoneManager) CreateZone(_ context.Context, _ string) (*CFZoneResult, error) {
	return f.zoneResult, f.zoneErr
}

func (f *fakeCFZoneManager) ScanDNSRecords(_ context.Context, _ string) error { return nil }

func (f *fakeCFZoneManager) ListDNSRecords(_ context.Context, _ string) ([]CFDNSRecord, error) {
	return f.records, f.recordErr
}

func (f *fakeCFZoneManager) CreateDNSRecord(_ context.Context, _ string, _ CFDNSRecord) (*CFDNSRecord, error) {
	if f.created != nil {
		return f.created, f.createErr
	}
	return &CFDNSRecord{ID: "rec-1"}, f.createErr
}

func (f *fakeCFZoneManager) DeleteDNSRecord(_ context.Context, _, _ string) error { return nil }
func (f *fakeCFZoneManager) DeleteZone(_ context.Context, zoneID string) error {
	f.deletedZones = append(f.deletedZones, zoneID)
	return f.deleteZoneErr
}

func TestDomains_DNS_ListRecords_NotCFManaged(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain: "example.com",
			Status: "active",
			// No CFZoneID — not CF managed
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.cfZone = &fakeCFZoneManager{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com/dns-records", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.ListDNSRecords(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_DNS_ListRecords_CFManaged(t *testing.T) {
	zoneID := "zone-123"
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:   "example.com",
			Status:   "active",
			CFZoneID: &zoneID,
		},
	}
	cfZone := &fakeCFZoneManager{
		records: []CFDNSRecord{
			{ID: "r1", Type: "A", Name: "example.com", Content: "1.2.3.4"},
			{ID: "r2", Type: "MX", Name: "example.com", Content: "mx.example.com"},
		},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.cfZone = cfZone

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com/dns-records", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.ListDNSRecords(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDomains_DNS_ListRecords_Unauthenticated(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com/dns-records", nil)
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.ListDNSRecords(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// A store failure is not "domain not found": the CLI would tell the user
// their domain does not exist while the database is down.
func TestDomains_DNS_ListRecords_StoreFailureIs500(t *testing.T) {
	h := NewDomainHandler(&fakeDomainStore{getErr: errors.New("connection refused")}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.cfZone = &fakeCFZoneManager{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/example.com/dns-records", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "domain", "example.com")
	w := httptest.NewRecorder()

	h.ListDNSRecords(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}

// When the routing record cannot be created the request undoes the zone it
// created. If that undo fails the zone is orphaned — nothing points at it —
// so the failure must be counted, not discarded.
func TestDomains_Create_V2_CFZone_CountsAZoneItCouldNotRollBack(t *testing.T) {
	cfZone := &fakeCFZoneManager{
		zoneResult:    &CFZoneResult{ZoneID: "zone-orphan", Nameservers: []string{"ns1"}},
		createErr:     errors.New("record rejected"),
		deleteZoneErr: errors.New("provider timeout"),
	}
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.cfZone = cfZone
	failures := metrics.DomainZoneCleanupFailuresTotal.WithLabelValues("delete_zone")
	before := testutil.ToFloat64(failures)

	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d body=%s", w.Code, w.Body.String())
	}
	if len(cfZone.deletedZones) != 1 || cfZone.deletedZones[0] != "zone-orphan" {
		t.Fatalf("rollback attempted on %v, want [zone-orphan]", cfZone.deletedZones)
	}
	if got := testutil.ToFloat64(failures) - before; got != 1 {
		t.Errorf("zone cleanup failures counted %v, want 1", got)
	}
}

func TestDomains_Create_V2_CFZone_HappyPath(t *testing.T) {
	store := &fakeDomainStore{}
	cfZone := &fakeCFZoneManager{
		zoneResult: &CFZoneResult{
			ZoneID:      "zone-abc",
			Nameservers: []string{"ns1.cloudflare.com", "ns2.cloudflare.com"},
		},
		records: []CFDNSRecord{},
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.getloopscale.app")
	h.cfZone = cfZone

	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp domainCreateV2Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("want ok=true")
	}
	if resp.Status != "pending_ns" {
		t.Fatalf("want status pending_ns, got %s", resp.Status)
	}
	if len(resp.Nameservers) != 2 {
		t.Fatalf("want 2 nameservers, got %d", len(resp.Nameservers))
	}
}

// The dashboard lists the domains and shows, under a pending one, the two DNS records the
// customer has to create. The list answered neither, so the screen had nothing to show and a
// customer could not finish adding a domain from it.
func TestDomains_List_CarriesTheRecordsACustomerHasToCreate(t *testing.T) {
	store := &fakeDomainStore{
		domains: []CustomDomain{
			{ID: "dom-1", Domain: "app.acme.com", Status: "pending_verification", VerificationToken: "loopscale-verify=0a1b"},
			{ID: "dom-2", Domain: "www.acme.com", Status: "active", VerificationToken: "loopscale-verify=2c3d"},
		},
		total: 2,
	}
	h := NewDomainHandler(store, &fakeDomainVerifier{}, &fakeDomainValidator{}, "cname.apps.example")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	var resp struct {
		Domains []struct {
			Domain       string `json:"domain"`
			CNAMETarget  string `json:"cname_target"`
			Verification *struct {
				TXTName  string `json:"txt_name"`
				TXTValue string `json:"txt_value"`
			} `json:"verification"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(resp.Domains) != 2 {
		t.Fatalf("want 2 domains, got %d", len(resp.Domains))
	}

	pending, active := resp.Domains[0], resp.Domains[1]
	if pending.CNAMETarget != "cname.apps.example" || active.CNAMETarget != "cname.apps.example" {
		t.Errorf("cname_target = %q and %q, want the handler's target on both", pending.CNAMETarget, active.CNAMETarget)
	}
	if pending.Verification == nil {
		t.Fatalf("the pending domain carries no verification record: %s", w.Body.String())
	}
	if pending.Verification.TXTName != "_loopscale-verify.app.acme.com" || pending.Verification.TXTValue != "loopscale-verify=0a1b" {
		t.Errorf("verification = %+v", *pending.Verification)
	}
	if active.Verification != nil {
		t.Errorf("an active domain still answers its verification record: %+v", *active.Verification)
	}
}
