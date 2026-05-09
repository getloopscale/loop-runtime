// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package e2e provides shared test helpers for all E2E test suites.
package e2e

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

// fixturesPath returns the absolute path to the E2E fixtures directory.
func fixturesPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "fixtures")
}

// apiURL returns the E2E API base URL from environment or default.
func apiURL() string {
	if url := os.Getenv("LOOPZERO_API_URL"); url != "" {
		return url
	}
	if url := os.Getenv("API_URL"); url != "" {
		return url
	}
	return "http://localhost:3001"
}

// apiHealthy checks if the API deep health endpoint returns 200.
func apiHealthy() bool {
	url := fmt.Sprintf("%s/api/v1/health/deep", apiURL())
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// clusterRunning checks if kubectl can reach the cluster.
func clusterRunning() bool {
	cmd := exec.Command("kubectl", "cluster-info")
	return cmd.Run() == nil
}

// uniqueSuffix returns a short random hex string for test isolation.
func uniqueSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// copyFixture copies a fixture directory to a temporary directory and returns
// the path. Each test gets its own copy to avoid cross-test contamination.
func copyFixture(fixtureName string) string {
	src := filepath.Join(fixturesPath(), fixtureName)
	dst, err := os.MkdirTemp("", "loopzero-e2e-"+fixtureName+"-")
	Expect(err).NotTo(HaveOccurred())

	cmd := exec.Command("cp", "-r", src+"/.", dst)
	output, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "failed to copy fixture: %s", string(output))

	return dst
}

// httpGet performs an HTTP GET and returns the status code, body, and error.
func httpGet(url string) (statusCode int, body string, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, "", fmt.Errorf("HTTP GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("failed to read response body: %w", err)
	}

	return resp.StatusCode, string(bodyBytes), nil
}

// cleanupTenant deletes the Tenant CR and waits for namespace cleanup.
func cleanupTenant(tenantID string, timeout time.Duration) {
	kubeCtx := os.Getenv("LOOPZERO_KUBE_CONTEXT")
	if kubeCtx == "" {
		kubeCtx = "k3d-loopzero-mgmt"
	}

	cmd := exec.Command("kubectl", "--context", kubeCtx,
		"delete", "tenant", tenantID, "--namespace", "default",
		"--ignore-not-found", "--timeout=30s")
	output, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("cleanup: failed to delete tenant %s: %s\n", tenantID, string(output))
	}

	// Wait for namespace deletion.
	deadline := time.Now().Add(timeout)
	expectedNamespaces := []string{
		tenantID + "-production",
		tenantID + "-staging",
		"build-" + tenantID,
	}

	for time.Now().Before(deadline) {
		allGone := true
		for _, ns := range expectedNamespaces {
			checkCmd := exec.Command("kubectl", "--context", kubeCtx,
				"get", "namespace", ns, "--no-headers", "-o", "name")
			if checkCmd.Run() == nil {
				allGone = false
				break
			}
		}
		if allGone {
			return
		}
		time.Sleep(2 * time.Second)
	}

	GinkgoWriter.Printf("cleanup: timeout waiting for namespace deletion (tenant: %s)\n", tenantID)
}

// verifyNoActiveBuilds checks that no active builds remain for the tenant.
func verifyNoActiveBuilds(tenantID string) {
	url := fmt.Sprintf("%s/api/v1/builds?tenant=%s&status=running", apiURL(), tenantID)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		GinkgoWriter.Printf("cleanup: failed to check active builds: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}

	if builds, ok := result["builds"].([]interface{}); ok && len(builds) > 0 {
		GinkgoWriter.Printf("cleanup: WARNING -- %d active builds remain for tenant %s\n", len(builds), tenantID)
	}
}

// verifyAPIHealth checks that the API server is accessible before running tests.
func verifyAPIHealth() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL() + "/api/v1/health")
	if err != nil {
		return fmt.Errorf("API health request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API health returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

// --- Journey Helpers ---
//
// These helpers support full E2E journey tests migrated from bash scripts
// in infra/tests/dogfood/journeys/. They use Eventually() for polling instead
// of sleep loops.

// waitBuildTerminal polls the build status API until the build reaches a terminal
// state (SUCCEEDED, FAILED, CANCELLED, TIMEOUT) or the timeout expires.
// Returns the final status string. Handles 401 by using the seed token.
func waitBuildTerminal(buildID string, timeout time.Duration) string {
	url := fmt.Sprintf("%s/api/v1/builds/%s", apiURL(), buildID)
	client := &http.Client{Timeout: 10 * time.Second}
	var finalStatus string

	Eventually(func() bool {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+resolveToken())

		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()

		// On 401, token might have expired — re-seed would be needed.
		// For now, just retry with the resolved token.
		if resp.StatusCode == http.StatusUnauthorized {
			return false
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return false
		}

		status, ok := result["status"].(string)
		if !ok {
			return false
		}

		if buildTerminalStates[status] {
			finalStatus = status
			return true
		}
		return false
	}, timeout, buildPollInterval).Should(BeTrue(),
		"build %s did not reach terminal state within %s", buildID, timeout)

	return finalStatus
}

// proveDead verifies that all resources for a project are cleaned up
// (namespace gone or zero resources with loopzero.io/project label).
// This is the "Prova de Morte" assertion from bash journeys.
func proveDead(tenant, project, env string) {
	ns := fmt.Sprintf("loopzero-%s-%s", tenant, env)
	ctx := workloadKubeContext

	Eventually(func() bool {
		// Check if namespace still exists.
		checkNS := exec.Command("kubectl", "--context", ctx,
			"get", "namespace", ns, "--no-headers", "-o", "name")
		if checkNS.Run() != nil {
			// Namespace doesn't exist — fully cleaned up.
			return true
		}

		// Namespace exists — check for remaining labeled resources.
		// Include both Deployment (legacy) and Rollout (progressive
		// delivery, default-on per argo-rollouts plan) so a stuck
		// Rollout/TraefikService doesn't yield a false-positive "cleaned".
		label := fmt.Sprintf("loopzero.io/project=%s", project)
		for _, resource := range []string{
			"deployment",
			"rollout.argoproj.io",
			"service",
			"ingressroute.traefik.io",
			"traefikservice.traefik.io",
		} {
			checkRes := exec.Command("kubectl", "--context", ctx,
				"-n", ns, "get", resource, "-l", label,
				"--no-headers", "--ignore-not-found", "-o", "name")
			output, err := checkRes.CombinedOutput()
			if err == nil && strings.TrimSpace(string(output)) != "" {
				return false
			}
		}
		return true
	}, cleanupTimeout, cleanupPollInterval).Should(BeTrue(),
		"Prova de Morte failed: resources still exist for project %s in %s", project, ns)
}

// resolveTraefikPort returns the Traefik HTTPS port for a tenant based on cell assignment.
// cell-2 uses port 8444, everything else uses 8443.
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

	if cellID, ok := result["cell_id"].(string); ok && cellID == "cell-2" {
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

	// Create TLS client that connects to localhost:port but sends the host header.
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // E2E test against local k3d
			ServerName:         host,
		},
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			// Override DNS resolution to point at localhost Traefik.
			return net.DialTimeout("tcp", "127.0.0.1:"+port, 5*time.Second)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
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
func httpGetWithHost(host, path string) (int, string, error) {
	port := resolveTraefikPort(e2eTenantID)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	targetURL := fmt.Sprintf("https://%s%s", host, path)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // E2E test against local k3d
			ServerName:         host,
		},
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("tcp", "127.0.0.1:"+port, 5*time.Second)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	resp, err := client.Get(targetURL)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}

// deployAndWaitSuccess runs loopzero deploy --json in the given directory, waits for
// the build to reach SUCCEEDED, and returns the build ID and full JSON response.
func deployAndWaitSuccess(dir, env string, timeout time.Duration) (buildID string, result map[string]interface{}) {
	args := []string{"deploy", "--env", env, "--no-wait", "--json"}
	cmd := exec.Command("loopzero", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_TOKEN="+resolveToken(),
	)

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	Expect(exitCode).To(Equal(0),
		"loopzero deploy failed with exit %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)

	// Parse JSON to extract build_id.
	result = make(map[string]interface{})
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") {
			if err := json.Unmarshal([]byte(line), &result); err == nil {
				break
			}
		}
	}

	Expect(result).To(HaveKey("build_id"),
		"deploy JSON output missing build_id\nstdout: %s", stdout)

	buildID, ok := result["build_id"].(string)
	Expect(ok).To(BeTrue(), "build_id should be a string")
	Expect(buildID).NotTo(BeEmpty())

	// Wait for build to succeed.
	status := waitBuildTerminal(buildID, timeout)
	Expect(status).To(Equal("SUCCEEDED"),
		"build %s ended with status %s, expected SUCCEEDED", buildID, status)

	return buildID, result
}

// assertAuditEntry verifies that the audit log contains an entry with the
// given action substring and optional project filter.
func assertAuditEntry(action string, project string) {
	url := fmt.Sprintf("%s/api/v1/audit?limit=200", apiURL())
	client := &http.Client{Timeout: 10 * time.Second}

	Eventually(func() bool {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+resolveToken())

		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return false
		}

		entries, ok := result["entries"].([]interface{})
		if !ok {
			// Try alternate key "items".
			entries, ok = result["items"].([]interface{})
			if !ok {
				return false
			}
		}

		for _, raw := range entries {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}

			entryAction, _ := entry["action"].(string)
			if !strings.Contains(entryAction, action) {
				continue
			}

			// If project filter specified, check metadata/resource_id.
			if project != "" {
				entryJSON, _ := json.Marshal(entry)
				if !strings.Contains(string(entryJSON), project) {
					continue
				}
			}
			return true
		}
		return false
	}, 30*time.Second, 5*time.Second).Should(BeTrue(),
		"expected audit entry with action %q for project %q", action, project)
}

// ensureProjectDeleted idempotently deletes a project if it exists.
// Used for cleanup before/after journey tests.
//
// LOOPZERO_API_TIMEOUT bumped to 120s (vs CLI default 30s) — project delete
// cascades through cancel builds + gitops remove + ArgoCD sync and exceeds
// 30s on LIVE k3d (observed flakes in journey 73/75 run, 2026-05-06).
func ensureProjectDeleted(project string) {
	cmd := exec.Command("loopzero", "project", "delete", project, "--yes", "--json")
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_API_TIMEOUT=120000",
		"LOOPZERO_TOKEN="+resolveToken(),
	)
	// Ignore errors — project may not exist.
	_ = cmd.Run()
}

// resolveToken returns the JWT to inject into spawned loopzero CLI processes.
// Resolution order (first non-empty wins):
//  1. process env LOOPZERO_TOKEN (CI/operator override — real Hydra-minted JWT)
//  2. ~/.loopzero/local-dogfood-token.txt (dogfood-seeded JWT)
//  3. seed.AuthToken (legacy placeholder; only valid in dev-mode API)
//
// 2026-05-09 fix: prefer the FILE over env when both exist. The file is
// regenerated by `seed-local-user.sh` (mid-run JWT refresh). The env var
// is captured by the test process at boot and never re-read, so a long
// suite that crosses the 1h Hydra TTL would 401 indefinitely. By
// preferring the file, mid-run `bash infra/helmfile/scripts/seed-local-user.sh`
// surfaces immediately to subsequent runLoopZero calls.
func resolveToken() string {
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(home + "/.loopzero/local-dogfood-token.txt"); err == nil {
			if s := strings.TrimSpace(string(data)); s != "" {
				return s
			}
		}
	}
	if t := os.Getenv("LOOPZERO_TOKEN"); t != "" {
		return t
	}
	return seed.AuthToken
}

// skipIfNotE2E skips the current spec if E2E prerequisites are not met.
func skipIfNotE2E() {
	if os.Getenv("LOOPZERO_E2E") == "" {
		Skip("Set LOOPZERO_E2E=1 to run E2E tests")
	}
	if !clusterRunning() {
		Skip("kubectl cluster-info failed — clusters not running")
	}
	if !apiHealthy() {
		Skip("API deep health check failed — API not ready")
	}
}

// seedE2E runs the idempotent E2E seeder.
func seedE2E() {
	err := seed.Run(seed.Config{})
	Expect(err).NotTo(HaveOccurred(), "E2E seed failed")
}

// assertNoForbiddenTermsInAll checks that none of the forbidden internal terms
// appear in any of the provided strings. Uses the canonical forbiddenInternalTerms
// list from constants_test.go.
func assertNoForbiddenTermsInAll(outputs map[string]string) {
	for label, output := range outputs {
		lowered := strings.ToLower(output)
		for _, term := range forbiddenInternalTerms {
			Expect(lowered).NotTo(
				ContainSubstring(strings.ToLower(term)),
				"Rule 3 violation: %q found in %s", term, label,
			)
		}
	}
}

// waitReleaseActive polls the project status API until the release for the
// given build becomes ACTIVE.
func waitReleaseActive(project, env, buildID string, timeout time.Duration) {
	url := fmt.Sprintf("%s/api/v1/projects/%s/status?env=%s", apiURL(), project, env)
	client := &http.Client{Timeout: 10 * time.Second}

	Eventually(func() bool {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+resolveToken())

		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return false
		}

		activeRelease, ok := result["active_release"].(map[string]interface{})
		if !ok {
			return false
		}

		status, _ := activeRelease["status"].(string)
		releaseBuildID, _ := activeRelease["build_id"].(string)

		return status == "ACTIVE" && releaseBuildID == buildID
	}, timeout, releasePollInterval).Should(BeTrue(),
		"release for build %s did not become ACTIVE in env %s within %s", buildID, env, timeout)
}
