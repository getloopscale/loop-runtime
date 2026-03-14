// Package e2e provides shared test helpers for all E2E test suites.
package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
