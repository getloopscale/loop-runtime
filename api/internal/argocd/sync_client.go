// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argocd provides real ArgoCD integration for deployment sync operations.
package argocd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SyncClientConfig holds configuration for connecting to ArgoCD.
type SyncClientConfig struct {
	BaseURL    string       // e.g., "https://argocd-server.argocd.svc:443"
	Token      string       // ArgoCD API token
	Insecure   bool         // Skip TLS verification (for local dev with self-signed certs)
	HTTPClient *http.Client // Optional: shared HTTP client with connection pooling. When nil, a default client is created.
}

// SyncClient implements services.SyncClient using the ArgoCD REST API.
type SyncClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewSyncClient creates a SyncClient for real ArgoCD operations.
// When cfg.HTTPClient is provided, it is used directly (connection pooling
// is shared with other components). Otherwise a default client is created.
// If cfg.Insecure is true and no custom client is provided, TLS verification
// is disabled on the default transport.
func NewSyncClient(cfg SyncClientConfig) *SyncClient {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
		if cfg.Insecure {
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		}
	}

	return &SyncClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		client:  client,
	}
}

// TriggerSync initiates a sync operation on an ArgoCD Application.
func (c *SyncClient) TriggerSync(ctx context.Context, appName string) error {
	url := fmt.Sprintf("%s/api/v1/applications/%s/sync", c.baseURL, appName)

	body := strings.NewReader(`{"prune": true}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("argocd sync: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("argocd sync: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return fmt.Errorf("argocd sync: application %q not found", appName)
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("argocd sync: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// WaitForSync polls ArgoCD until the application is synced and the specified
// deployedApps are healthy, or until the timeout expires.
// If deployedApps is empty, checks global Application health (legacy behavior).
// Polls every 10 seconds per spec §10.1.
func (c *SyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 10 * time.Second

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("argocd wait: sync timeout after %s for %q", timeout, appName)
		}

		synced, healthy, err := c.getAppStatus(ctx, appName, deployedApps)
		if err != nil {
			return fmt.Errorf("argocd wait: %w", err)
		}

		if synced && healthy {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("argocd wait: context cancelled: %w", ctx.Err())
		case <-time.After(pollInterval):
			// continue polling
		}
	}
}

// resourceStatus represents an individual resource's health in the ArgoCD Application.
type resourceStatus struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Health *struct {
		Status string `json:"status"`
	} `json:"health,omitempty"`
}

// getAppStatus returns the sync and health status of an ArgoCD Application.
// When deployedApps is non-empty, health is checked per-resource for only the
// Deployments matching those app names — so one degraded app doesn't block others.
func (c *SyncClient) getAppStatus(ctx context.Context, appName string, deployedApps []string) (synced bool, healthy bool, err error) {
	url := fmt.Sprintf("%s/api/v1/applications/%s", c.baseURL, appName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return false, false, fmt.Errorf("get app status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return false, false, fmt.Errorf("application %q not found", appName)
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return false, false, fmt.Errorf("get app status %d: %s", resp.StatusCode, string(respBody))
	}

	var app struct {
		Status struct {
			Sync struct {
				Status string `json:"status"`
			} `json:"sync"`
			Health struct {
				Status string `json:"status"`
			} `json:"health"`
			Resources []resourceStatus `json:"resources"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return false, false, fmt.Errorf("decode app status: %w", err)
	}

	isSynced := app.Status.Sync.Status == "Synced"

	// If no specific apps to check, use global Application health (legacy).
	if len(deployedApps) == 0 {
		return isSynced, app.Status.Health.Status == "Healthy", nil
	}

	// Per-resource health: check only Deployments matching the deployed app names.
	// This ensures one degraded app doesn't block deploys of other apps in the
	// same ArgoCD Application (which manages all apps in a tenant/environment).
	appSet := make(map[string]bool, len(deployedApps))
	for _, name := range deployedApps {
		appSet[name] = true
	}

	matchedCount := 0
	for _, res := range app.Status.Resources {
		if res.Kind != "Deployment" {
			continue
		}
		if !appSet[res.Name] {
			continue
		}
		matchedCount++
		if res.Health == nil || res.Health.Status != "Healthy" {
			return isSynced, false, nil
		}
	}

	// If we found all Deployments and they're all healthy, consider it healthy.
	// If no matching Deployments found yet (still rolling out), not healthy.
	return isSynced, matchedCount >= len(deployedApps), nil
}
