// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// What the custom domain step of the app lifecycle journey needs from the environment.
//
// The domain is a name in a zone outside the platform, the way a customer's is. Its CNAME
// points at the edge, and the name of its verification record is a CNAME to a record in the
// platform's zone, which the journey can write: the value changes with every run, and the
// customer's zone stays as it is.
const (
	// CustomDomainEnv names the domain the journey attaches. Unset, the step is skipped.
	CustomDomainEnv = "LOOP_RUNTIME_DOGFOOD_CUSTOM_DOMAIN"
	// VerifyZoneEnv names the zone that holds the record the verification name points to.
	VerifyZoneEnv = "LOOP_RUNTIME_DOGFOOD_VERIFY_ZONE"
	// VerifyRecordEnv names that record, relative to the zone.
	VerifyRecordEnv = "LOOP_RUNTIME_DOGFOOD_VERIFY_RECORD"
	// DNSTokenFileEnv names the file with the token of the zone's DNS API.
	DNSTokenFileEnv = "LOOP_RUNTIME_DOGFOOD_DNS_TOKEN_FILE" // #nosec G101 -- the name of a variable, not a credential

	// HostingerAPI is the DNS API of the provider that hosts the platform's zone.
	HostingerAPI = "https://developers.hostinger.com"
	// verificationRecordTTL is the TTL of the record the journey writes. A resolver keeps
	// the value of the run before for that long.
	verificationRecordTTL = 60
)

// CustomDomainSetup is the custom domain the journey attaches and where its verification
// record is written.
type CustomDomainSetup struct {
	Domain    string
	Zone      string
	Record    string
	TokenFile string
}

// CustomDomainFromEnv reads the setup. ok is false when no domain is named.
func CustomDomainFromEnv() (setup CustomDomainSetup, ok bool) {
	setup = CustomDomainSetup{
		Domain:    os.Getenv(CustomDomainEnv),
		Zone:      os.Getenv(VerifyZoneEnv),
		Record:    os.Getenv(VerifyRecordEnv),
		TokenFile: os.Getenv(DNSTokenFileEnv),
	}
	return setup, setup.Domain != ""
}

// DNSZone writes records of one zone through the provider's API.
type DNSZone struct {
	BaseURL   string
	Client    *http.Client
	Zone      string
	TokenFile string
}

// SetTXTRecord makes value the only TXT record at name, which is relative to the zone.
// An error names the request and the answer, never the token.
func (z DNSZone) SetTXTRecord(ctx context.Context, name, value string) error {
	token, err := os.ReadFile(z.TokenFile)
	if err != nil {
		return fmt.Errorf("read the DNS token: %w", err)
	}
	type record struct {
		Content string `json:"content"`
	}
	type entry struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		TTL     int      `json:"ttl"`
		Records []record `json:"records"`
	}
	raw, err := json.Marshal(map[string]any{
		"overwrite": true,
		"zone":      []entry{{Name: name, Type: "TXT", TTL: verificationRecordTTL, Records: []record{{Content: value}}}},
	})
	if err != nil {
		return fmt.Errorf("encode the TXT record: %w", err)
	}
	target := strings.TrimRight(z.BaseURL, "/") + "/api/dns/v1/zones/" + url.PathEscape(z.Zone)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build the DNS request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := z.Client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT the TXT record %s in %s: %w", name, z.Zone, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("PUT the TXT record %s in %s answered HTTP %d: %s", name, z.Zone, resp.StatusCode, truncate(string(answer), 300))
	}
	return nil
}

// TXTAnswers reports whether a lookup of name answers value. A name with no record is
// (false, nil); a CNAME on the way is followed by the resolver.
func TXTAnswers(ctx context.Context, name, value string) (bool, error) {
	records, err := net.DefaultResolver.LookupTXT(ctx, name)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, nil
		}
		return false, err
	}
	for _, record := range records {
		if record == value {
			return true, nil
		}
	}
	return false, nil
}

// DomainVerification is the TXT record that proves control of a domain.
type DomainVerification struct {
	TXTName  string `json:"txt_name"`
	TXTValue string `json:"txt_value"`
}

// DomainTLS is what the API recorded about a domain's certificate.
type DomainTLS struct {
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// DomainRecord is a custom domain as the API answers it.
type DomainRecord struct {
	Domain            string              `json:"domain"`
	Status            string              `json:"status"`
	CNAMETarget       string              `json:"cname_target"`
	Verification      *DomainVerification `json:"verification"`
	VerificationError string              `json:"verification_error"`
	TLS               *DomainTLS          `json:"tls"`
}

// AddDomain attaches domain to an app of a project's environment.
func (e *Engine) AddDomain(ctx context.Context, domain, project, app, environment string) (DomainRecord, error) {
	var out DomainRecord
	body := map[string]string{"domain": domain, "project": project, "app": app, "environment": environment}
	err := e.json(ctx, http.MethodPost, "/api/v1/domains", idempotencyKey(), body, &out)
	return out, err
}

// VerifyDomain asks the API to look the verification record up now. The API allows one
// call every 30 seconds for a domain.
func (e *Engine) VerifyDomain(ctx context.Context, domain string) (DomainRecord, error) {
	var out DomainRecord
	err := e.json(ctx, http.MethodPost, "/api/v1/domains/"+url.PathEscape(domain)+"/verify", idempotencyKey(), nil, &out)
	return out, err
}

// GetDomain reads one domain. IsNotFound tells a domain that does not exist.
func (e *Engine) GetDomain(ctx context.Context, domain string) (DomainRecord, error) {
	var out DomainRecord
	err := e.json(ctx, http.MethodGet, "/api/v1/domains/"+url.PathEscape(domain), nil, nil, &out)
	return out, err
}

// ListDomains reads the tenant's domains, as the dashboard does.
func (e *Engine) ListDomains(ctx context.Context) ([]DomainRecord, error) {
	var out struct {
		Domains []DomainRecord `json:"domains"`
	}
	err := e.json(ctx, http.MethodGet, "/api/v1/domains", nil, nil, &out)
	return out.Domains, err
}

// DeleteDomain starts the removal of a domain.
func (e *Engine) DeleteDomain(ctx context.Context, domain string) error {
	return e.json(ctx, http.MethodDelete, "/api/v1/domains/"+url.PathEscape(domain), idempotencyKey(), nil, nil)
}

// IsNotFound reports whether err is the API's answer for something that does not exist.
func IsNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "answered HTTP 404")
}
