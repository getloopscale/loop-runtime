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

	"github.com/getloopscale/loopzero/infra/tests/seed"
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
// 2026-05-20 drill-parity: uses kubectlForCluster(ClusterWorkload, ...).
func proveDead(tenant, project, env string) {
	ns := fmt.Sprintf("loopzero-%s-%s", tenant, env)

	Eventually(func() bool {
		// Check if namespace still exists.
		checkNS := kubectlForCluster(ClusterWorkload,
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
			checkRes := kubectlForCluster(ClusterWorkload,
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

// e2eBaseDomain returns the base domain for app hostnames in the current env.
// Plan 2026-05-19-drill-skills-rigor-restoration: drill DOKS uses CF wildcard
// (loopzero-drill.example) while local k3d uses loopzero.local. DRILL_KUBECONFIG_DIR
// presence is the signal for drill mode.
func e2eBaseDomain() string {
	if os.Getenv("DRILL_KUBECONFIG_DIR") != "" {
		return e2eBaseDomainDrill
	}
	return e2eBaseDomainLocal
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

// waitAppHealthyViaKubectl is the V9 Option-B bypass for local k3d
// HTTP probes that flake on Traefik+DNS routing under load. It uses
// `kubectl port-forward <pod>` to tunnel an HTTP request from the
// Ginkgo process directly to the pod — completely sidestepping the
// local k3d DNS+TLS+Traefik convergence path AND the in-container
// busybox/distroless tooling assumption (apps with scratch/distroless
// bases have no wget/curl, so kubectl exec probe returns exit 127).
//
// Parameters:
//   - project, app, env: identify the workload (namespace
//     "loopzero-<tenant>-<env>", label "app=<project>-<app>")
//   - path: HTTP path to GET (e.g., "/health")
//   - timeout: Eventually() budget
//
// Returns silently on success; fails the spec via Gomega Expect
// on failure (mirrors waitHTTP200WithHost behavior).
//
// Resilient to:
//   - Pod not yet provisioned (Eventually retries)
//   - Pod not yet Ready (port-forward fails, retry)
//   - Distroless/scratch base image (port-forward works regardless)
//
// V9 Final fix (post-L11): kubectl exec assumed wget/curl exist in the
// app image, but loopzero-packs uses distroless base → exit 127. port-forward
// works for any image because the kubelet tunnel runs OUTSIDE the
// container's filesystem.
func waitAppHealthyViaKubectl(project, app, env, path string, timeout time.Duration) {
	resName := project + "-" + app
	ns := "loopzero-" + seed.TenantID + "-" + env

	Eventually(func() error {
		// Find a Running+Ready pod for this app.
		out, err := kubectlForCluster(ClusterWorkload,
			"get", "pods", "-n", ns,
			"-l", "app="+resName,
			"-o", "jsonpath={range .items[*]}{.metadata.name}|{.status.phase}|{range .status.containerStatuses[*]}{.ready},{end}{\"\\n\"}{end}",
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("list pods in %s: %s", ns, strings.TrimSpace(string(out)))
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) == 0 || lines[0] == "" {
			return fmt.Errorf("no pods with label app=%s in namespace %s", resName, ns)
		}
		var podName string
		for _, line := range lines {
			parts := strings.Split(line, "|")
			if len(parts) < 3 {
				continue
			}
			if parts[1] != "Running" {
				continue
			}
			// containerStatuses ready comma-separated: "true,true,"
			if strings.Contains(parts[2], "false") {
				continue
			}
			podName = parts[0]
			break
		}
		if podName == "" {
			return fmt.Errorf("no Running+Ready pod for app=%s in %s (status: %s)", resName, ns, lines[0])
		}

		// Discover the container port the app listens on. loopzero-packs
		// app images typically expose 3000 (node) or 8080 (other). We
		// try the first containerPort declared.
		portOut, perr := kubectlForCluster(ClusterWorkload,
			"get", "pod", "-n", ns, podName,
			"-o", "jsonpath={.spec.containers[0].ports[0].containerPort}",
		).CombinedOutput()
		appPort := strings.TrimSpace(string(portOut))
		if perr != nil || appPort == "" {
			appPort = "3000" // safe default for loopzero-packs node template
		}

		// kubectl port-forward to a random local port, run HTTP GET,
		// then kill the port-forward. Random port avoids contention
		// when multiple journeys run sequentially.
		// We pick a port in the range 39000-39999 using crypto/rand.
		var portBuf [2]byte
		_, _ = rand.Read(portBuf[:])
		localPort := 39000 + (int(portBuf[0])<<8|int(portBuf[1]))%1000

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pfCmd := kubectlForCluster(ClusterWorkload,
			"port-forward", "-n", ns, "pod/"+podName,
			fmt.Sprintf("%d:%s", localPort, appPort),
		)
		// Start port-forward as background subprocess.
		stderr := &strings.Builder{}
		pfCmd.Stderr = stderr
		if err := pfCmd.Start(); err != nil {
			return fmt.Errorf("port-forward start failed: %v", err)
		}
		// Kill port-forward on function exit (deferred close).
		killCh := make(chan struct{})
		go func() {
			select {
			case <-killCh:
			case <-ctx.Done():
			}
			_ = pfCmd.Process.Kill()
			_, _ = pfCmd.Process.Wait()
		}()
		defer close(killCh)

		// Wait briefly for port-forward to bind.
		time.Sleep(500 * time.Millisecond)

		// HTTP GET via the port-forwarded localhost tunnel.
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d%s", localPort, path), nil)
		resp, herr := client.Do(req)
		if herr != nil {
			return fmt.Errorf("port-forward HTTP probe to %s:%d%s failed: %v (pf stderr: %s)",
				podName, localPort, path, herr, strings.TrimSpace(stderr.String()))
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("port-forward HTTP probe to %s:%d%s returned %d", podName, localPort, path, resp.StatusCode)
		}
		return nil
	}, timeout, 5*time.Second).Should(Succeed(),
		"expected /health probe to succeed via kubectl port-forward on app=%s in namespace=%s within %s",
		resName, ns, timeout,
	)
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
			time.Sleep(5 * time.Second)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastStatus = resp.StatusCode
			lastErr = readErr
			time.Sleep(5 * time.Second)
			continue
		}
		lastStatus = resp.StatusCode
		lastBody = string(body)
		lastErr = nil
		if !strings.Contains(lastBody, kedaColdStartPlaceholder) {
			return lastStatus, lastBody, nil
		}
		time.Sleep(5 * time.Second)
	}
	return lastStatus, lastBody, lastErr
}

// deployAndWaitSuccess runs loopzero deploy --json in the given directory, waits for
// the build to reach SUCCEEDED, and returns the build ID and full JSON response.
func deployAndWaitSuccess(dir, env string, timeout time.Duration) (buildID string, result map[string]interface{}) {
	args := []string{"deploy", "--env", env, "--no-wait", "--json"}
	cmd := exec.Command("loopzero", args...)
	cmd.Dir = dir
	// T2 amendment 2026-05-13: auth via configStore (XDG_CONFIG_HOME from
	// BeforeSuite), NOT LOOPZERO_TOKEN env injection. See finding-2026-05-13-e2e-
	// token-refresh-divergence.md. Hard-strip parent LOOPZERO_TOKEN so configStore
	// is the only source (Rule 10 — test↔prod auth path parity).
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_TOKEN=",
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
	// T2 amendment 2026-05-13: auth via configStore (XDG_CONFIG_HOME from
	// BeforeSuite). LOOPZERO_TOKEN= stripped to avoid env override of configStore.
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_API_TIMEOUT=120000",
		"LOOPZERO_TOKEN=",
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
