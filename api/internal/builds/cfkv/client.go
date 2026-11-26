// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package cfkv provides a client for the Cloudflare Workers KV API.
// Used by the static frontend delivery pipeline to write routing entries
// that the CF Worker reads to serve assets from DO Spaces.
package cfkv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// RoutingEntry is the value stored in KV for each static frontend hostname.
type RoutingEntry struct {
	BuildID  string `json:"buildId"`
	S3Prefix string `json:"s3Prefix"`
}

// Client writes routing entries to Cloudflare Workers KV.
type Client struct {
	accountID   string
	namespaceID string
	apiToken    string
	httpClient  *http.Client
}

// NewClient creates a KV client. Returns nil if credentials are not configured
// (acceptable in local development where CF Worker is not used).
func NewClient(accountID, namespaceID, apiToken string) *Client {
	if accountID == "" || namespaceID == "" || apiToken == "" {
		return nil
	}
	return &Client{
		accountID:   accountID,
		namespaceID: namespaceID,
		apiToken:    apiToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Put writes a routing entry to KV. The key is the flat subdomain
// (e.g., "tn-loopzero-e2e-project-web-production").
// The entry is JSON-serialized before writing.
func (c *Client) Put(ctx context.Context, subdomain string, entry interface{}) error {
	start := time.Now()
	body, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("cfkv: marshal entry: %w", err)
	}

	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.accountID, c.namespaceID, subdomain,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cfkv: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		metrics.CFKVOperationsTotal.WithLabelValues("put", "failure").Inc()
		metrics.CFKVOperationDuration.WithLabelValues("put").Observe(time.Since(start).Seconds())
		return fmt.Errorf("cfkv: put %s: %w", subdomain, err)
	}
	defer func() { _ = resp.Body.Close() }()

	metrics.CFKVOperationDuration.WithLabelValues("put").Observe(time.Since(start).Seconds())
	if resp.StatusCode != http.StatusOK {
		metrics.CFKVOperationsTotal.WithLabelValues("put", "failure").Inc()
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cfkv: put %s: status %d: %s", subdomain, resp.StatusCode, string(respBody))
	}
	metrics.CFKVOperationsTotal.WithLabelValues("put", "success").Inc()
	return nil
}

// Delete removes a routing entry from KV (used when deleting a static app).
func (c *Client) Delete(ctx context.Context, subdomain string) error {
	start := time.Now()
	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.accountID, c.namespaceID, subdomain,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("cfkv: create delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		metrics.CFKVOperationsTotal.WithLabelValues("delete", "failure").Inc()
		metrics.CFKVOperationDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
		return fmt.Errorf("cfkv: delete %s: %w", subdomain, err)
	}
	defer func() { _ = resp.Body.Close() }()

	metrics.CFKVOperationDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if resp.StatusCode != http.StatusOK {
		metrics.CFKVOperationsTotal.WithLabelValues("delete", "failure").Inc()
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cfkv: delete %s: status %d: %s", subdomain, resp.StatusCode, string(respBody))
	}
	metrics.CFKVOperationsTotal.WithLabelValues("delete", "success").Inc()
	return nil
}

// Get reads a routing entry from KV. Returns nil, nil if the key does not exist.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()
	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.accountID, c.namespaceID, key,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cfkv: create get request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		metrics.CFKVOperationsTotal.WithLabelValues("get", "failure").Inc()
		metrics.CFKVOperationDuration.WithLabelValues("get").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("cfkv: get %s: %w", key, err)
	}
	defer func() { _ = resp.Body.Close() }()

	metrics.CFKVOperationDuration.WithLabelValues("get").Observe(time.Since(start).Seconds())
	if resp.StatusCode == http.StatusNotFound {
		metrics.CFKVOperationsTotal.WithLabelValues("get", "success").Inc()
		return nil, nil // key does not exist
	}
	if resp.StatusCode != http.StatusOK {
		metrics.CFKVOperationsTotal.WithLabelValues("get", "failure").Inc()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cfkv: get %s: status %d: %s", key, resp.StatusCode, string(respBody))
	}
	metrics.CFKVOperationsTotal.WithLabelValues("get", "success").Inc()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cfkv: read body for %s: %w", key, err)
	}
	return body, nil
}

// ListKeys returns all keys in the KV namespace. Uses cursor-based pagination.
func (c *Client) ListKeys(ctx context.Context) ([]string, error) {
	var allKeys []string
	cursor := ""

	for {
		url := fmt.Sprintf(
			"https://api.cloudflare.com/client/v4/accounts/%s/storage/kv/namespaces/%s/keys?limit=1000",
			c.accountID, c.namespaceID,
		)
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("cfkv: create list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("cfkv: list keys: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("cfkv: list keys: status %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Result     []struct{ Name string } `json:"result"`
			ResultInfo struct{ Cursor string } `json:"result_info"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("cfkv: parse list response: %w", err)
		}

		for _, k := range result.Result {
			allKeys = append(allKeys, k.Name)
		}

		if result.ResultInfo.Cursor == "" {
			break
		}
		cursor = result.ResultInfo.Cursor
	}

	return allKeys, nil
}

// Subdomain builds the flat subdomain key from tenant/project/app/env.
// Format: {tenant}-{project}-{app}-{env} (max 63 chars, RFC 1123).
func Subdomain(tenantID, projectID, appName, environment string) string {
	subdomain := fmt.Sprintf("%s-%s-%s-%s", tenantID, projectID, appName, environment)
	if len(subdomain) > 63 {
		subdomain = subdomain[:63]
	}
	subdomain = strings.TrimRight(subdomain, "-")
	return subdomain
}
