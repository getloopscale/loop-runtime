// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cfzone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A create that failed on the wire may already exist at Cloudflare; sending
// it again would duplicate it, so a POST is attempted exactly once.
func TestCreateDNSRecordIsNotRetriedAfterAFailedCall(t *testing.T) {
	// Arrange: the server answers with a body the client cannot decode.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	// Act
	_, err := newTestClient(srv.URL, "acct", "tok").CreateDNSRecord(context.Background(), "zone-1", DNSRecord{Name: "a"})

	// Assert
	if err == nil {
		t.Fatal("CreateDNSRecord reported success on an undecodable response")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("POST sent %d times, want 1", got)
	}
}

func TestGetZoneStatusRetriesAFailedRead(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte("not json"))
			return
		}
		_ = json.NewEncoder(w).Encode(cfResponse[ZoneInfo]{Success: true, Result: ZoneInfo{Status: "active"}})
	}))
	defer srv.Close()

	status, err := newTestClient(srv.URL, "acct", "tok").GetZoneStatus(context.Background(), "zone-1")

	if err != nil || status != "active" {
		t.Fatalf("GetZoneStatus = %q, %v; want active after one retry", status, err)
	}
}

// The apex-conflict check needs every record; a zone with more than one page
// must be read to the end.
func TestListDNSRecordsFollowsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		resp := cfListResponse[DNSRecord]{Success: true, Result: []DNSRecord{{ID: "rec-" + page}}}
		resp.ResultInfo.Page = 1
		if page == "2" {
			resp.ResultInfo.Page = 2
		}
		resp.ResultInfo.TotalPages = 2
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	records, err := newTestClient(srv.URL, "acct", "tok").ListDNSRecords(context.Background(), "zone-1")

	if err != nil {
		t.Fatalf("ListDNSRecords: %v", err)
	}
	if len(records) != 2 || records[0].ID != "rec-1" || records[1].ID != "rec-2" {
		t.Errorf("records = %+v, want rec-1 and rec-2", records)
	}
}

func TestTriggerActivationCheckReportsAnAPILevelFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(cfResponse[json.RawMessage]{Success: false, Errors: []cfError{{Code: 1224, Message: "rate limited"}}})
	}))
	defer srv.Close()

	err := newTestClient(srv.URL, "acct", "tok").TriggerActivationCheck(context.Background(), "zone-1")

	if err == nil {
		t.Fatal("TriggerActivationCheck swallowed success=false")
	}
}
