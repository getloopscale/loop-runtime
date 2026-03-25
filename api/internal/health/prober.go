// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package health provides real HTTP health probing for deployed applications.
package health

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// HTTPProber checks if a deployed app is accessible via HTTP GET.
// It retries up to 3 times with exponential backoff (2s, 4s, 8s).
// Status codes 200-399 are considered healthy.
//
// When IngressURL is set, requests are sent to the Ingress controller's
// address with the original hostname as Host header. This allows health
// probing through Traefik without DNS resolution — standard practice for
// testing Ingress controllers in any environment (local, CI, production).
type HTTPProber struct {
	Client     *http.Client
	IngressURL string        // Optional: e.g., "http://localhost:8090". When set, probe through Ingress.
	Backoffs   []time.Duration // Optional: retry backoffs. Default: 10 retries over ~120s for real deploys.
}

// Probe sends HTTP GET to url+path and returns nil if the response is 200-399.
// If IngressURL is configured, the request is sent to IngressURL+path with
// the Host header set to the hostname from the original URL.
func (p *HTTPProber) Probe(ctx context.Context, appURL string, path string) error {
	endpoint, host, err := p.resolveEndpoint(appURL, path)
	if err != nil {
		return fmt.Errorf("health probe: %w", err)
	}

	backoffs := p.Backoffs
	if len(backoffs) == 0 {
		// Default: ~120s total — gives pods time to pull image and start.
		// Spec §10.1: activation timeout up to 150s.
		backoffs = []time.Duration{0, 5 * time.Second, 10 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second}
	}
	var lastErr error

	for attempt, wait := range backoffs {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("health probe cancelled: %w", ctx.Err())
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("health probe: invalid request: %w", err)
		}

		// Set Host header when probing through Ingress controller
		if host != "" {
			req.Host = host
		}

		resp, err := p.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("health probe attempt %d: %w", attempt+1, err)
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return nil
		}
		lastErr = fmt.Errorf("health probe attempt %d: status %d", attempt+1, resp.StatusCode)
	}

	return fmt.Errorf("health probe failed after %d attempts: %w", len(backoffs), lastErr)
}

// resolveEndpoint determines the actual endpoint to probe and the Host header.
// When IngressURL is set, returns the Ingress address with the original hostname
// as the Host header. Otherwise, returns the original URL directly.
func (p *HTTPProber) resolveEndpoint(appURL string, path string) (endpoint string, host string, err error) {
	if p.IngressURL == "" {
		// Direct probe — no Ingress override
		return appURL + path, "", nil
	}

	// Parse the app URL to extract the hostname
	parsed, err := url.Parse(appURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid app URL %q: %w", appURL, err)
	}

	// Probe the Ingress controller with the path
	endpoint = p.IngressURL + path
	host = parsed.Host // e.g., "myapp.acme.loopzero.local"
	return endpoint, host, nil
}
