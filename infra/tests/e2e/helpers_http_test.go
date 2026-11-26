// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package e2e provides shared test helpers for all E2E test suites.
package e2e

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// resolveTraefikPort returns the Traefik HTTPS port for a tenant based on cell assignment.
// cell-1 uses port 8444, everything else uses 8443.
func resolveTraefikPort(tenant string) string {
	// Query the tenant's cell from the API or default.
	url := fmt.Sprintf("%s/api/v1/admin/tenants/%s", apiURL(), tenant)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return e2eTraefikPort
	}
	req.Header.Set("Authorization", "Bearer "+resolveToken())

	resp, err := client.Do(req)
	if err != nil {
		return e2eTraefikPort
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return e2eTraefikPort
	}

	if cellID, ok := result["cell_id"].(string); ok && cellID == "cell-1" {
		return e2eTraefikPortCell2
	}
	return e2eTraefikPort
}

// waitHTTP200WithHost polls an HTTPS endpoint via Traefik with a specific Host header.
// Uses --connect-to equivalent to route through localhost Traefik.
func waitHTTP200WithHost(host, path string, timeout time.Duration) {
	port := resolveTraefikPort(e2eTenantID)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	targetURL := fmt.Sprintf("https://%s%s", host, path)

	// Plan 2026-05-19-drill-skills-rigor-restoration: drill validation against
	// DOKS resolves host via public DNS (CF wildcard A record → runtime LB).
	// Local k3d still dials localhost:8443 via Traefik port-forward.
	dialTarget := "127.0.0.1:" + port
	if os.Getenv("DRILL_KUBECONFIG_DIR") != "" {
		// Drill mode: let real DNS resolve to runtime cluster LB.
		dialTarget = ""
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // E2E test against local k3d or drill DOKS
			ServerName:         host,
		},
	}
	if dialTarget != "" {
		transport.DialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("tcp", dialTarget, 5*time.Second)
		}
	}
	client := &http.Client{
		Transport: transport,
		// V9 dogfood 2026-05-30 fix L9: per-request timeout 10s → 30s.
		// Under env-isolation (commit e99e721f) each journey has its own
		// ArgoCD app + TraefikService; the routing table takes longer to
		// populate on first request after newly-created Service. 10s
		// timeout caused spurious zero-status returns; 30s gives Traefik
		// the headroom to resolve the new IngressRoute.
		Timeout: 30 * time.Second,
	}

	Eventually(func() int {
		resp, err := client.Get(targetURL)
		if err != nil {
			GinkgoWriter.Printf("HTTP probe %s: %v\n", targetURL, err)
			return 0
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}, timeout, httpPollInterval).Should(Equal(http.StatusOK),
		"expected HTTP 200 from %s via Traefik port %s", targetURL, port)
}

// httpGetWithHost performs an HTTP GET to a host via local Traefik and returns
// status code and body.
//
// KEDA cold-start handling: when the target Service has 0 replicas, the KEDA
// HTTP Add-on interceptor returns the literal body "no available server" while
// it triggers scale-up. The test assertion expects real app content, so this
// helper retries up to 6 attempts (~30s) when it detects the KEDA placeholder
// to cover scale-from-zero latency.
func httpGetWithHost(host, path string) (int, string, error) {
	port := resolveTraefikPort(e2eTenantID)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	targetURL := fmt.Sprintf("https://%s%s", host, path)

	// Plan 2026-05-19-drill-skills-rigor-restoration: drill mode = real DNS.
	dialTarget := "127.0.0.1:" + port
	if os.Getenv("DRILL_KUBECONFIG_DIR") != "" {
		dialTarget = ""
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // E2E test against local k3d or drill DOKS
			ServerName:         host,
		},
	}
	if dialTarget != "" {
		transport.DialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("tcp", dialTarget, 5*time.Second)
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	const kedaColdStartPlaceholder = "no available server"
	var (
		lastStatus int
		lastBody   string
		lastErr    error
	)
	for attempt := 0; attempt < 6; attempt++ {
		resp, err := client.Get(targetURL)
		if err != nil {
			lastErr = err
			// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
			time.Sleep(5 * time.Second)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastStatus = resp.StatusCode
			lastErr = readErr
			// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
			time.Sleep(5 * time.Second)
			continue
		}
		lastStatus = resp.StatusCode
		lastBody = string(body)
		lastErr = nil
		if !strings.Contains(lastBody, kedaColdStartPlaceholder) {
			return lastStatus, lastBody, nil
		}
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(5 * time.Second)
	}
	return lastStatus, lastBody, lastErr
}
