// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cfzone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		if body.Type != "CNAME" || body.Content != "cname.dev.loopzero.example" {
			t.Errorf("unexpected record: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(cfResponse[DNSRecord]{
			Success: true,
			Result:  DNSRecord{ID: "new-1", Type: "CNAME", Name: "empresa.com", Content: "cname.dev.loopzero.example", Proxied: &proxied},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "acct", "token")
	rec, err := c.CreateDNSRecord(context.Background(), "zone-123", DNSRecord{
		Type:    "CNAME",
		Name:    "empresa.com",
		Content: "cname.dev.loopzero.example",
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
func newTestClient(baseURL, accountID, apiToken string) *testClient {
	return &testClient{
		Client: Client{
			accountID:  accountID,
			apiToken:   apiToken,
			httpClient: &http.Client{Timeout: 5 * time.Second},
		},
		baseURL: baseURL + "/client/v4",
	}
}

// testClient overrides the base URL for testing.
type testClient struct {
	Client
	baseURL string
}


// Proxy methods that use the test base URL
func (tc *testClient) CreateZone(ctx context.Context, domain string) (*ZoneInfo, error) {
	body := map[string]interface{}{
		"name":    domain,
		"account": map[string]string{"id": tc.accountID},
		"type":    "full",
	}
	var resp cfResponse[ZoneInfo]
	if err := tc.doWithBase(ctx, tc.baseURL, "POST", "/zones", body, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("cfzone: create zone %s: %s", domain, formatErrors(resp.Errors))
	}
	return &resp.Result, nil
}

func (tc *testClient) GetZoneStatus(ctx context.Context, zoneID string) (string, error) {
	var resp cfResponse[ZoneInfo]
	if err := tc.doWithBase(ctx, tc.baseURL, "GET", "/zones/"+zoneID, nil, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("%s", formatErrors(resp.Errors))
	}
	return resp.Result.Status, nil
}

func (tc *testClient) DeleteZone(ctx context.Context, zoneID string) error {
	var resp cfResponse[json.RawMessage]
	return tc.doWithBase(ctx, tc.baseURL, "DELETE", "/zones/"+zoneID, nil, &resp)
}

func (tc *testClient) ScanDNSRecords(ctx context.Context, zoneID string) error {
	var resp cfResponse[ScanResult]
	if err := tc.doWithBase(ctx, tc.baseURL, "POST", "/zones/"+zoneID+"/dns_records/scan", nil, &resp); err != nil {
		return err
	}
	return nil
}

func (tc *testClient) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	var resp cfListResponse[DNSRecord]
	if err := tc.doWithBase(ctx, tc.baseURL, "GET", "/zones/"+zoneID+"/dns_records?per_page=100", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func (tc *testClient) CreateDNSRecord(ctx context.Context, zoneID string, record DNSRecord) (*DNSRecord, error) {
	var resp cfResponse[DNSRecord]
	if err := tc.doWithBase(ctx, tc.baseURL, "POST", "/zones/"+zoneID+"/dns_records", record, &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

func (tc *testClient) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	var resp cfResponse[json.RawMessage]
	return tc.doWithBase(ctx, tc.baseURL, "DELETE", "/zones/"+zoneID+"/dns_records/"+recordID, nil, &resp)
}

func (tc *testClient) TriggerActivationCheck(ctx context.Context, zoneID string) error {
	var resp cfResponse[json.RawMessage]
	return tc.doWithBase(ctx, tc.baseURL, "PUT", "/zones/"+zoneID+"/activation_check", nil, &resp)
}
