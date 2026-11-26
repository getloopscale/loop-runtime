// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cfzone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const (
	cfAPIBase = "https://api.cloudflare.com/client/v4"
	// callTimeout bounds one HTTP call; maxAttempts and retryBackoff shape the
	// retry of idempotent calls (500ms, then 1s).
	callTimeout  = 8 * time.Second
	maxAttempts  = 3
	retryBackoff = 500 * time.Millisecond
	// dnsRecordsPerPage is Cloudflare's largest page for DNS record listing.
	dnsRecordsPerPage = 100
)

// Client interacts with the Cloudflare Zone Management and DNS APIs.
// Used by the custom domain feature to create per-domain zones on the
// platform's CF account (model Vercel: user changes nameservers only).
type Client struct {
	accountID   string
	apiToken    string
	baseURL     string // cfAPIBase in production
	httpClient  *http.Client
	rateLimiter *rate.Limiter // CF API: 1200 req/5min = 4 req/s
}

// NewClient creates a CF Zone client. Returns nil if credentials are missing
// (acceptable in local development where CF is not used).
func NewClient(accountID, apiToken string) *Client {
	if accountID == "" || apiToken == "" {
		return nil
	}
	return &Client{
		accountID: accountID,
		apiToken:  apiToken,
		baseURL:   cfAPIBase,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: rate.NewLimiter(rate.Limit(4), 10), // 4 req/s sustained, burst 10
	}
}

// CreateZone creates a new DNS zone on the platform's CF account.
// The zone starts in "pending" status until the user changes their nameservers.
func (c *Client) CreateZone(ctx context.Context, domain string) (*ZoneInfo, error) {
	body := map[string]interface{}{
		"name":    domain,
		"account": map[string]string{"id": c.accountID},
		"type":    "full",
	}

	var resp cfResponse[ZoneInfo]
	if err := c.do(ctx, "POST", "/zones", body, &resp); err != nil {
		return nil, fmt.Errorf("cfzone: create zone %s: %w", domain, err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("cfzone: create zone %s: %s", domain, formatErrors(resp.Errors))
	}
	return &resp.Result, nil
}

// GetZoneStatus returns the current status of a zone.
func (c *Client) GetZoneStatus(ctx context.Context, zoneID string) (string, error) {
	var resp cfResponse[ZoneInfo]
	if err := c.do(ctx, "GET", "/zones/"+zoneID, nil, &resp); err != nil {
		return "", fmt.Errorf("cfzone: get zone %s: %w", zoneID, err)
	}
	if !resp.Success {
		return "", fmt.Errorf("cfzone: get zone %s: %s", zoneID, formatErrors(resp.Errors))
	}
	return resp.Result.Status, nil
}

// TriggerActivationCheck asks CF to immediately check if nameservers have been updated.
func (c *Client) TriggerActivationCheck(ctx context.Context, zoneID string) error {
	var resp cfResponse[json.RawMessage]
	if err := c.do(ctx, "PUT", "/zones/"+zoneID+"/activation_check", nil, &resp); err != nil {
		return fmt.Errorf("cfzone: activation check %s: %w", zoneID, err)
	}
	if !resp.Success {
		return fmt.Errorf("cfzone: activation check %s: %s", zoneID, formatErrors(resp.Errors))
	}
	return nil
}

// DeleteZone removes a DNS zone from the CF account.
func (c *Client) DeleteZone(ctx context.Context, zoneID string) error {
	var resp cfResponse[json.RawMessage]
	if err := c.do(ctx, "DELETE", "/zones/"+zoneID, nil, &resp); err != nil {
		return fmt.Errorf("cfzone: delete zone %s: %w", zoneID, err)
	}
	if !resp.Success {
		return fmt.Errorf("cfzone: delete zone %s: %s", zoneID, formatErrors(resp.Errors))
	}
	return nil
}

// ScanDNSRecords triggers CF's automatic DNS record scan for the zone.
// CF queries the zone's current authoritative nameservers to discover existing records.
// After scanning, use ListDNSRecords to see what was found.
func (c *Client) ScanDNSRecords(ctx context.Context, zoneID string) error {
	var resp cfResponse[ScanResult]
	if err := c.do(ctx, "POST", "/zones/"+zoneID+"/dns_records/scan", nil, &resp); err != nil {
		return fmt.Errorf("cfzone: scan records %s: %w", zoneID, err)
	}
	if !resp.Success {
		return fmt.Errorf("cfzone: scan records %s: %s", zoneID, formatErrors(resp.Errors))
	}
	return nil
}

// ListDNSRecords returns all DNS records in the zone, following
// result_info.total_pages: the apex-conflict check needs every record, and a
// single page stops at 100.
func (c *Client) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	var records []DNSRecord
	for page := 1; ; page++ {
		var resp cfListResponse[DNSRecord]
		path := fmt.Sprintf("/zones/%s/dns_records?per_page=%d&page=%d", zoneID, dnsRecordsPerPage, page)
		if err := c.do(ctx, "GET", path, nil, &resp); err != nil {
			return nil, fmt.Errorf("cfzone: list records %s page %d: %w", zoneID, page, err)
		}
		if !resp.Success {
			return nil, fmt.Errorf("cfzone: list records %s page %d: %s", zoneID, page, formatErrors(resp.Errors))
		}
		records = append(records, resp.Result...)
		if page >= resp.ResultInfo.TotalPages {
			return records, nil
		}
	}
}

// CreateDNSRecord adds a DNS record to the zone.
func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, record DNSRecord) (*DNSRecord, error) {
	var resp cfResponse[DNSRecord]
	if err := c.do(ctx, "POST", "/zones/"+zoneID+"/dns_records", record, &resp); err != nil {
		return nil, fmt.Errorf("cfzone: create record %s/%s: %w", zoneID, record.Name, err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("cfzone: create record %s/%s: %s", zoneID, record.Name, formatErrors(resp.Errors))
	}
	return &resp.Result, nil
}

// DeleteDNSRecord removes a DNS record from the zone.
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	var resp cfResponse[json.RawMessage]
	if err := c.do(ctx, "DELETE", "/zones/"+zoneID+"/dns_records/"+recordID, nil, &resp); err != nil {
		return fmt.Errorf("cfzone: delete record %s/%s: %w", zoneID, recordID, err)
	}
	if !resp.Success {
		return fmt.Errorf("cfzone: delete record %s/%s: %s", zoneID, recordID, formatErrors(resp.Errors))
	}
	return nil
}

// --- HTTP helpers ---

func (c *Client) do(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	// Rate limit: wait for token before making request
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("cf rate limit wait: %w", err)
		}
	}

	// MEET-044 P0: per-call timeout, so a CF outage cannot hang provisioning.
	// Only idempotent methods are retried: a POST that timed out may already
	// have created the zone or record, and sending it again would duplicate it
	// or fail with "already exists".
	attempts := 1
	if method != http.MethodPost {
		attempts = maxAttempts
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return lastErr
			case <-time.After(time.Duration(attempt) * retryBackoff):
			}
		}
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		lastErr = c.send(callCtx, method, path, body, result)
		cancel()
		if lastErr == nil || ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

func (c *Client) send(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("unmarshal response (status %d): %w", resp.StatusCode, err)
	}
	return nil
}

func formatErrors(errs []cfError) string {
	if len(errs) == 0 {
		return "unknown error"
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = fmt.Sprintf("[%d] %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%v", msgs)
}
