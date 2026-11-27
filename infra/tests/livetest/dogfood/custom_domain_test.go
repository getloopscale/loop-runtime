// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCustomDomainFromEnv_IsOffWithoutADomain(t *testing.T) {
	t.Setenv(CustomDomainEnv, "")
	if _, ok := CustomDomainFromEnv(); ok {
		t.Fatal("a custom domain was configured with no domain named")
	}
}

func TestCustomDomainFromEnv_ReadsWhereTheVerificationRecordIsWritten(t *testing.T) {
	t.Setenv(CustomDomainEnv, "app.customer.example")
	t.Setenv(VerifyZoneEnv, "platform.example")
	t.Setenv(VerifyRecordEnv, "_loopscale-verify.app.staging")
	t.Setenv(DNSTokenFileEnv, "/run/token")

	got, ok := CustomDomainFromEnv()

	want := CustomDomainSetup{Domain: "app.customer.example", Zone: "platform.example", Record: "_loopscale-verify.app.staging", TokenFile: "/run/token"}
	if !ok || got != want {
		t.Fatalf("CustomDomainFromEnv = %+v, %v; want %+v", got, ok, want)
	}
}

func TestSetTXTRecord_ReplacesTheRecordInTheZone(t *testing.T) {
	var method, path, authorization string
	var body struct {
		Overwrite bool `json:"overwrite"`
		Zone      []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			TTL     int    `json:"ttl"`
			Records []struct {
				Content string `json:"content"`
			} `json:"records"`
		} `json:"zone"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, authorization = r.Method, r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, `{"message":"Request accepted"}`)
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("dns-token-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dns := DNSZone{BaseURL: server.URL, Client: server.Client(), Zone: "platform.example", TokenFile: tokenFile}

	err := dns.SetTXTRecord(context.Background(), "_loopscale-verify.app.staging", "loopscale-verify=0a1b")

	if err != nil {
		t.Fatalf("SetTXTRecord: %v", err)
	}
	if method != http.MethodPut || path != "/api/dns/v1/zones/platform.example" {
		t.Errorf("sent %s %s", method, path)
	}
	if authorization != "Bearer dns-token-1" {
		t.Error("the request did not carry the token of the file, trimmed")
	}
	if !body.Overwrite || len(body.Zone) != 1 {
		t.Fatalf("body = %+v, want one record that overwrites", body)
	}
	rec := body.Zone[0]
	if rec.Name != "_loopscale-verify.app.staging" || rec.Type != "TXT" || rec.TTL != 60 || len(rec.Records) != 1 || rec.Records[0].Content != "loopscale-verify=0a1b" {
		t.Errorf("record = %+v", rec)
	}
}

func TestSetTXTRecord_NamesARefusalWithoutTheToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Unauthenticated"}`)
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("dns-token-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	dns := DNSZone{BaseURL: server.URL, Client: server.Client(), Zone: "platform.example", TokenFile: tokenFile}

	err := dns.SetTXTRecord(context.Background(), "_x", "v")

	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v, want the refusal's status", err)
	}
	if strings.Contains(err.Error(), "dns-token-1") {
		t.Error("the error carries the token")
	}
}

func TestEngine_DomainCallsCarryAnIdempotencyKeyAndDecodeTheRecords(t *testing.T) {
	stub, env := newEngineStub(t)
	stub.answers["POST /engine/api/v1/domains"] = stubAnswer{201, `{"domain":"app.customer.example","status":"pending_verification","cname_target":"cname.edge.example","verification":{"txt_name":"_loopscale-verify.app.customer.example","txt_value":"loopscale-verify=0a1b"}}`}
	stub.answers["GET /engine/api/v1/domains/app.customer.example"] = stubAnswer{200, `{"domain":"app.customer.example","status":"active","tls":{"status":"issued","expires_at":"2027-02-24T11:50:24Z"}}`}
	stub.answers["GET /engine/api/v1/domains"] = stubAnswer{200, `{"domains":[{"domain":"app.customer.example","status":"pending_verification","cname_target":"cname.edge.example","verification":{"txt_name":"_loopscale-verify.app.customer.example","txt_value":"loopscale-verify=0a1b"}}],"total":1}`}
	engine := NewEngine(env, "token-1")
	ctx := context.Background()

	created, err := engine.AddDomain(ctx, "app.customer.example", "staging-validate", "app", "production")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if created.Verification == nil || created.Verification.TXTValue != "loopscale-verify=0a1b" || created.CNAMETarget != "cname.edge.example" {
		t.Errorf("created = %+v", created)
	}
	if body := stub.bodies["POST /engine/api/v1/domains"]; body["app"] != "app" || body["project"] != "staging-validate" || body["environment"] != "production" {
		t.Errorf("POST body = %v", body)
	}

	read, err := engine.GetDomain(ctx, "app.customer.example")
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	wantExpiry := time.Date(2027, 2, 24, 11, 50, 24, 0, time.UTC)
	if read.TLS == nil || read.TLS.Status != "issued" || read.TLS.ExpiresAt == nil || !read.TLS.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("read.TLS = %+v", read.TLS)
	}

	listed, err := engine.ListDomains(ctx)
	if err != nil || len(listed) != 1 || listed[0].Verification == nil {
		t.Fatalf("ListDomains = %+v, %v", listed, err)
	}

	if _, err := engine.GetDomain(ctx, "gone.customer.example"); !IsNotFound(err) {
		t.Errorf("a missing domain answered %v, want a not-found error", err)
	}
}
