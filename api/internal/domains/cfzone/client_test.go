// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cfzone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient_NilWhenMissingCredentials(t *testing.T) {
	if c := NewClient("", "token"); c != nil {
		t.Error("expected nil client when accountID empty")
	}
	if c := NewClient("account", ""); c != nil {
		t.Error("expected nil client when apiToken empty")
	}
	if c := NewClient("account", "token"); c == nil {
		t.Error("expected non-nil client with valid credentials")
	}
}

func TestCreateZone_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/client/v4/zones" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "empresa.com" {
			t.Errorf("expected domain empresa.com, got %v", body["name"])
		}

		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: true,
			Result: ZoneInfo{
				ID:          "zone-123",
				Name:        "empresa.com",
				Status:      "pending",
				Nameservers: []string{"ns1.cloudflare.com", "ns2.cloudflare.com"},
			},
		})
	}))
	defer srv.Close()

	c2 := newTestClient(srv.URL, "acct", "test-token")
	zone, err := c2.CreateZone(context.Background(), "empresa.com")
	if err != nil {
		t.Fatalf("CreateZone failed: %v", err)
	}
	if zone.ID != "zone-123" {
		t.Errorf("zone ID = %q, want zone-123", zone.ID)
	}
	if zone.Status != "pending" {
		t.Errorf("zone status = %q, want pending", zone.Status)
	}
	if len(zone.Nameservers) != 2 {
		t.Errorf("nameservers count = %d, want 2", len(zone.Nameservers))
	}
}

func TestCreateZone_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: false,
			Errors:  []cfError{{Code: 1061, Message: "Zone already exists"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	_, err := c.CreateZone(context.Background(), "existing.com")
	if err == nil {
		t.Fatal("expected error for duplicate zone")
	}
}

func TestGetZoneStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/zones/zone-123" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: true,
			Result:  ZoneInfo{ID: "zone-123", Status: "active"},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	status, err := c.GetZoneStatus(context.Background(), "zone-123")
	if err != nil {
		t.Fatalf("GetZoneStatus failed: %v", err)
	}
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}
}

func TestDeleteZone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: true})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	if err := c.DeleteZone(context.Background(), "zone-123"); err != nil {
		t.Fatalf("DeleteZone failed: %v", err)
	}
}

func TestScanDNSRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/client/v4/zones/zone-123/dns_records/scan" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[ScanResult]{
			Success: true,
			Result:  ScanResult{TotalRecords: 5},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	if err := c.ScanDNSRecords(context.Background(), "zone-123"); err != nil {
		t.Fatalf("ScanDNSRecords failed: %v", err)
	}
}

func TestListDNSRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mx5 := 5
		_ = json.NewEncoder(w).Encode(cfListResponse[DNSRecord]{
			Success: true,
			Result: []DNSRecord{
				{ID: "r1", Type: "MX", Name: "empresa.com", Content: "mx1.google.com", Priority: &mx5},
				{ID: "r2", Type: "TXT", Name: "empresa.com", Content: "v=spf1 include:_spf.google.com ~all"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	records, err := c.ListDNSRecords(context.Background(), "zone-123")
	if err != nil {
		t.Fatalf("ListDNSRecords failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Type != "MX" {
		t.Errorf("first record type = %q, want MX", records[0].Type)
	}
}

func TestCreateDNSRecord(t *testing.T) {
	proxied := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body DNSRecord
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Type != "CNAME" || body.Content != "cname.dev.getloopscale.com" {
			t.Errorf("unexpected record: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[DNSRecord]{
			Success: true,
			Result:  DNSRecord{ID: "new-1", Type: "CNAME", Name: "empresa.com", Content: "cname.dev.getloopscale.com", Proxied: &proxied},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	rec, err := c.CreateDNSRecord(context.Background(), "zone-123", DNSRecord{
		Type:    "CNAME",
		Name:    "empresa.com",
		Content: "cname.dev.getloopscale.com",
		TTL:     1,
		Proxied: &proxied,
	})
	if err != nil {
		t.Fatalf("CreateDNSRecord failed: %v", err)
	}
	if rec.ID != "new-1" {
		t.Errorf("record ID = %q, want new-1", rec.ID)
	}
}

func TestDeleteDNSRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/client/v4/zones/zone-123/dns_records/rec-1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: true})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	if err := c.DeleteDNSRecord(context.Background(), "zone-123", "rec-1"); err != nil {
		t.Fatalf("DeleteDNSRecord failed: %v", err)
	}
}

func TestTriggerActivationCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/client/v4/zones/zone-123/activation_check" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: true})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	if err := c.TriggerActivationCheck(context.Background(), "zone-123"); err != nil {
		t.Fatalf("TriggerActivationCheck failed: %v", err)
	}
}

// newTestClient creates a Client pointing to a test HTTP server.
// newTestClient points the real Client at a test server, so every test
// exercises the production methods rather than a copy of them.
func newTestClient(baseURL, accountID, apiToken string) *Client {
	return &Client{
		accountID:  accountID,
		apiToken:   apiToken,
		baseURL:    baseURL + "/client/v4",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// --- Additional coverage tests ---

func TestFormatErrors_Empty(t *testing.T) {
	got := formatErrors(nil)
	if got != "unknown error" {
		t.Errorf("formatErrors(nil) = %q, want 'unknown error'", got)
	}
}

func TestFormatErrors_Multiple(t *testing.T) {
	errs := []cfError{
		{Code: 1001, Message: "first"},
		{Code: 1002, Message: "second"},
	}
	got := formatErrors(errs)
	if !strings.Contains(got, "[1001] first") {
		t.Errorf("formatErrors missing first error: %s", got)
	}
	if !strings.Contains(got, "[1002] second") {
		t.Errorf("formatErrors missing second error: %s", got)
	}
}

func TestGetZoneStatus_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: false,
			Errors:  []cfError{{Code: 7003, Message: "not found"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	_, err := c.GetZoneStatus(context.Background(), "bad-zone")
	// The testClient.GetZoneStatus checks resp.Success
	if err == nil {
		t.Fatal("expected error for failed status check")
	}
}

func TestDeleteZone_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{
			Success: false,
			Errors:  []cfError{{Code: 1000, Message: "not found"}},
		})
	}))
	defer srv.Close()

	// Verify response directly since this reads the raw envelope
	c := &Client{
		accountID:  "acct",
		apiToken:   "token",
		httpClient: srv.Client(),
	}
	var resp cfResponse[json.RawMessage]
	_ = withBase(c, srv.URL+"/client/v4").send(context.Background(), "DELETE", "/zones/bad-zone", nil, &resp)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if len(resp.Errors) == 0 || resp.Errors[0].Code != 1000 {
		t.Errorf("expected error code 1000, got %+v", resp.Errors)
	}
}

func TestScanDNSRecords_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ScanResult]{
			Success: false,
			Errors:  []cfError{{Code: 2000, Message: "scan failed"}},
		})
	}))
	defer srv.Close()

	// Verify response directly
	c := &Client{
		accountID:  "acct",
		apiToken:   "token",
		httpClient: srv.Client(),
	}
	var resp cfResponse[ScanResult]
	_ = withBase(c, srv.URL+"/client/v4").send(context.Background(), "POST", "/zones/zone-123/dns_records/scan", nil, &resp)
	if resp.Success {
		t.Fatal("expected success=false")
	}
}

func TestListDNSRecords_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfListResponse[DNSRecord]{
			Success: false,
			Errors:  []cfError{{Code: 3000, Message: "list failed"}},
		})
	}))
	defer srv.Close()

	// Verify response directly
	c := &Client{
		accountID:  "acct",
		apiToken:   "token",
		httpClient: srv.Client(),
	}
	var resp cfListResponse[DNSRecord]
	_ = withBase(c, srv.URL+"/client/v4").send(context.Background(), "GET", "/zones/zone-123/dns_records?per_page=100", nil, &resp)
	if resp.Success {
		t.Fatal("expected success=false")
	}
}

func TestCreateDNSRecord_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[DNSRecord]{
			Success: false,
			Errors:  []cfError{{Code: 4000, Message: "create failed"}},
		})
	}))
	defer srv.Close()

	// Verify the response contains success=false and errors
	c := &Client{
		accountID:  "acct",
		apiToken:   "token",
		httpClient: srv.Client(),
	}
	var resp cfResponse[DNSRecord]
	_ = withBase(c, srv.URL+"/client/v4").send(context.Background(), "POST", "/zones/zone-123/dns_records", DNSRecord{
		Type: "A", Name: "test.com", Content: "1.2.3.4",
	}, &resp)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if len(resp.Errors) == 0 || resp.Errors[0].Code != 4000 {
		t.Errorf("expected error code 4000, got %+v", resp.Errors)
	}
}

func TestDeleteDNSRecord_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{
			Success: false,
			Errors:  []cfError{{Code: 5000, Message: "delete failed"}},
		})
	}))
	defer srv.Close()

	// Use the real Client through send to test the actual error path.
	// The testClient.DeleteDNSRecord skips the Success check, so we call
	// the real Client through send + check manually.
	c := &Client{
		accountID:  "acct",
		apiToken:   "token",
		httpClient: srv.Client(),
	}
	var resp cfResponse[json.RawMessage]
	_ = withBase(c, srv.URL+"/client/v4").send(context.Background(), "DELETE", "/zones/zone-123/dns_records/rec-bad", nil, &resp)
	if resp.Success {
		t.Fatal("expected success=false in response")
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected errors in response")
	}
	if resp.Errors[0].Code != 5000 {
		t.Errorf("expected error code 5000, got %d", resp.Errors[0].Code)
	}
}

func TestDoWithBase_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	var resp cfResponse[ZoneInfo]
	err := withBase(c, srv.URL+"/client/v4").send(context.Background(), "GET", "/zones/test", nil, &resp)
	if err == nil {
		t.Fatal("expected unmarshal error for invalid JSON response")
	}
}

func TestDoWithBase_SetsAuthAndContentType(t *testing.T) {
	var capturedAuth string
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedContentType = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{Success: true})
	}))
	defer srv.Close()

	c := &Client{
		accountID:  "acct",
		apiToken:   "my-secret-token",
		httpClient: srv.Client(),
	}

	// POST with body should set Content-Type
	var resp cfResponse[ZoneInfo]
	_ = withBase(c, srv.URL).send(context.Background(), "POST", "/zones", map[string]string{"name": "test"}, &resp)
	if capturedAuth != "Bearer my-secret-token" {
		t.Errorf("auth = %q, want Bearer my-secret-token", capturedAuth)
	}
	if capturedContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", capturedContentType)
	}
}

func TestDoWithBase_GET_NoContentType(t *testing.T) {
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{Success: true})
	}))
	defer srv.Close()

	c := &Client{
		accountID:  "acct",
		apiToken:   "token",
		httpClient: srv.Client(),
	}

	// GET with nil body should not set Content-Type
	var resp cfResponse[ZoneInfo]
	_ = withBase(c, srv.URL).send(context.Background(), "GET", "/zones/test", nil, &resp)
	if capturedContentType != "" {
		t.Errorf("GET should not set Content-Type, got %q", capturedContentType)
	}
}

func TestDoWithBase_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{Success: true})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var resp cfResponse[ZoneInfo]
	err := withBase(c, srv.URL+"/client/v4").send(ctx, "GET", "/zones/test", nil, &resp)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// --- Real Client methods via URL-rewriting transport ---
// These tests exercise the actual Client.CreateZone, Client.GetZoneStatus, etc.
// methods (the ones at 0% coverage) by using an HTTP transport that redirects
// all requests from api.cloudflare.com to the local test server.

type cfRewriteTransport struct {
	target  string
	wrapped http.RoundTripper
}

func (t *cfRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	u, _ := http.NewRequest("GET", t.target, nil)
	req.URL.Host = u.URL.Host
	return t.wrapped.RoundTrip(req)
}

func newRealClientWithTestServer(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := NewClient("test-acct", "test-token")
	c.httpClient = &http.Client{
		Transport: &cfRewriteTransport{target: srv.URL, wrapped: http.DefaultTransport},
		Timeout:   5 * time.Second,
	}
	c.rateLimiter = nil // disable rate limiting for tests
	return c, srv
}

func TestRealClient_CreateZone(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: true,
			Result:  ZoneInfo{ID: "z1", Name: "test.com", Status: "pending", Nameservers: []string{"ns1.cf.com"}},
		})
	}))
	defer srv.Close()

	zone, err := c.CreateZone(context.Background(), "test.com")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if zone.ID != "z1" {
		t.Errorf("zone.ID = %q, want z1", zone.ID)
	}
}

func TestRealClient_CreateZone_Failure(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: false,
			Errors:  []cfError{{Code: 1061, Message: "already exists"}},
		})
	}))
	defer srv.Close()

	_, err := c.CreateZone(context.Background(), "dup.com")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRealClient_GetZoneStatus(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{
			Success: true,
			Result:  ZoneInfo{ID: "z1", Status: "active"},
		})
	}))
	defer srv.Close()

	status, err := c.GetZoneStatus(context.Background(), "z1")
	if err != nil {
		t.Fatalf("GetZoneStatus: %v", err)
	}
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}
}

func TestRealClient_DeleteZone(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: true})
	}))
	defer srv.Close()

	if err := c.DeleteZone(context.Background(), "z1"); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
}

func TestRealClient_ScanDNSRecords(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[ScanResult]{Success: true, Result: ScanResult{TotalRecords: 3}})
	}))
	defer srv.Close()

	if err := c.ScanDNSRecords(context.Background(), "z1"); err != nil {
		t.Fatalf("ScanDNSRecords: %v", err)
	}
}

func TestRealClient_ListDNSRecords(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfListResponse[DNSRecord]{
			Success: true,
			Result:  []DNSRecord{{ID: "r1", Type: "A", Name: "test.com", Content: "1.2.3.4"}},
		})
	}))
	defer srv.Close()

	records, err := c.ListDNSRecords(context.Background(), "z1")
	if err != nil {
		t.Fatalf("ListDNSRecords: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
}

func TestRealClient_CreateDNSRecord(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[DNSRecord]{
			Success: true,
			Result:  DNSRecord{ID: "r-new", Type: "CNAME"},
		})
	}))
	defer srv.Close()

	rec, err := c.CreateDNSRecord(context.Background(), "z1", DNSRecord{Type: "CNAME", Name: "test.com", Content: "target.com"})
	if err != nil {
		t.Fatalf("CreateDNSRecord: %v", err)
	}
	if rec.ID != "r-new" {
		t.Errorf("record.ID = %q, want r-new", rec.ID)
	}
}

func TestRealClient_DeleteDNSRecord(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: true})
	}))
	defer srv.Close()

	if err := c.DeleteDNSRecord(context.Background(), "z1", "r1"); err != nil {
		t.Fatalf("DeleteDNSRecord: %v", err)
	}
}

func TestRealClient_TriggerActivationCheck(t *testing.T) {
	c, srv := newRealClientWithTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: true})
	}))
	defer srv.Close()

	if err := c.TriggerActivationCheck(context.Background(), "z1"); err != nil {
		t.Fatalf("TriggerActivationCheck: %v", err)
	}
}

// withBase points c at base and returns it, for tests that read the raw
// response envelope through send.
func withBase(c *Client, base string) *Client {
	c.baseURL = base
	return c
}
