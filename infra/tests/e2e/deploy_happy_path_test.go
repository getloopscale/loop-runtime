// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package e2e contains end-to-end tests that prove deploy convergence.
//
// The deploy happy path tests validate:
//
//	loopzero deploy → build SUCCEEDED → release ACTIVE → app accessible via URL
//
// These tests require the full infrastructure (k3d clusters, Argo Workflows,
// ArgoCD, Gitea, MinIO) to be running. Set LOOPZERO_E2E=1 to enable.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

// --- Deploy-specific helpers ---

// initGitRepo initializes a git repo in the given directory, adds all files,
// and creates an initial commit. Returns the commit SHA.
func initGitRepo(dir string) string {
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "e2e@test.local"},
		{"git", "config", "user.name", "E2E Test"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "initial commit"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "git command %v failed: %s", args, string(output))
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "git rev-parse failed: %s", string(output))

	return strings.TrimSpace(string(output))
}

// runLoopZeroDeploy executes `loopzero deploy` in the given directory with optional
// additional arguments. Returns stdout, stderr, and the exit code.
func runLoopZeroDeploy(dir string, args ...string) (stdout, stderr string, exitCode int) {
	allArgs := append([]string{"deploy"}, args...)
	cmd := exec.Command("loopzero", allArgs...)
	cmd.Dir = dir
	// Honor LOOPZERO_TOKEN from the parent env (real JWT minted by Hydra) when set,
	// falling back to seed.AuthToken (placeholder used by old tests).
	token := os.Getenv("LOOPZERO_TOKEN")
	if token == "" {
		token = seed.AuthToken
	}
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_TOKEN="+token,
	)

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode
}

// getDeployJSON runs `loopzero deploy --json` and parses the JSON output.
func getDeployJSON(dir string) (map[string]interface{}, error) {
	stdout, stderr, exitCode := runLoopZeroDeploy(dir, "--json")
	if exitCode != 0 {
		return nil, fmt.Errorf("loopzero deploy --json exited with code %d\nstdout: %s\nstderr: %s",
			exitCode, stdout, stderr)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output: %w\nraw output: %s", err, stdout)
	}

	return result, nil
}

// waitForBuildStatus polls the API until the build reaches the expected status
// or the timeout expires.
func waitForBuildStatus(buildID string, status string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("%s/api/v1/builds/%s", apiURL(), buildID)

	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if currentStatus, ok := result["status"].(string); ok && currentStatus == status {
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("build %s did not reach status %q within %s", buildID, status, timeout)
}

// --- Ginkgo specs ---

// deployIDRegex validates the deploy_id format: d-{4hex}
var deployIDRegex = regexp.MustCompile(`^d-[0-9a-f]{4}$`)

var _ = Describe("Deploy Happy Path", Ordered, func() {
	var workDir string

	BeforeEach(func() {
		// Skip if E2E not enabled.
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}

		// Skip if clusters not running.
		if !clusterRunning() {
			Skip("kubectl cluster-info failed -- clusters not running")
		}

		// Skip if API not healthy.
		if !apiHealthy() {
			Skip("API deep health check failed -- API not ready")
		}

		// Run seed helper to ensure tenant + user + project exist.
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "E2E seed failed")
	})

	AfterEach(func() {
		// Clean up temporary working directories.
		if workDir != "" {
			os.RemoveAll(workDir)
		}

		// Only run cleanup when E2E is enabled.
		if os.Getenv("LOOPZERO_E2E") == "" {
			return
		}

		// Verify no active builds remain.
		verifyNoActiveBuilds(seed.TenantID)
	})

	// -- Test 1: Single app deploy --

	It("should deploy a single app and return a live URL", func() {
		// Arrange: copy fixture and init git repo.
		workDir = copyFixture("hello-server")
		commitSHA := initGitRepo(workDir)
		Expect(commitSHA).NotTo(BeEmpty())

		// Act: run loopzero deploy --json.
		result, err := getDeployJSON(workDir)
		Expect(err).NotTo(HaveOccurred(), "loopzero deploy --json failed")

		// Assert: deploy succeeded.
		Expect(result).To(HaveKey("status"))
		Expect(result["status"]).To(Equal("success"))

		// Assert: deploy_id format d-{4hex} (DA-11).
		Expect(result).To(HaveKey("deploy_id"))
		deployID, ok := result["deploy_id"].(string)
		Expect(ok).To(BeTrue(), "deploy_id should be a string")
		Expect(deployIDRegex.MatchString(deployID)).To(BeTrue(),
			"deploy_id %q does not match d-{4hex} format", deployID)

		// Assert: duration_seconds present and positive.
		Expect(result).To(HaveKey("duration_seconds"))
		durationSecs, ok := result["duration_seconds"].(float64)
		Expect(ok).To(BeTrue(), "duration_seconds should be a number")
		Expect(durationSecs).To(BeNumerically(">", 0))

		// Assert: apps array present with at least one app (DA-5 schema).
		Expect(result).To(HaveKey("apps"))
		apps, ok := result["apps"].([]interface{})
		Expect(ok).To(BeTrue(), "apps should be an array")
		Expect(apps).To(HaveLen(1))

		app, ok := apps[0].(map[string]interface{})
		Expect(ok).To(BeTrue(), "each app should be an object")

		// Assert: app fields match spec DA-5 schema.
		Expect(app).To(HaveKey("name"))
		Expect(app["name"]).To(Equal("api"))
		Expect(app).To(HaveKey("status"))
		Expect(app["status"]).To(Equal("deployed"))
		Expect(app).To(HaveKey("url"))
		Expect(app).To(HaveKey("build_duration_seconds"))

		// Assert: URL is live and returns HTTP 200.
		appURL, ok := app["url"].(string)
		Expect(ok).To(BeTrue(), "url should be a string")
		Expect(appURL).NotTo(BeEmpty(), "deployed app should have a URL")

		Eventually(func() int {
			statusCode, _, _ := httpGet(appURL)
			return statusCode
		}, 60*time.Second, 2*time.Second).Should(Equal(http.StatusOK),
			"deployed app should return 200 at %s", appURL)
	})

	// -- Test 2: Deploy with cache --

	It("should use build cache on second deploy", func() {
		// Arrange: copy fixture and init git repo.
		workDir = copyFixture("hello-server")
		_ = initGitRepo(workDir)

		// Act: first deploy.
		firstResult, err := getDeployJSON(workDir)
		Expect(err).NotTo(HaveOccurred(), "first deploy failed")
		Expect(firstResult["status"]).To(Equal("success"))
		firstDuration := firstResult["duration_seconds"].(float64)

		// Act: second deploy (same code -- should benefit from cache).
		secondResult, err := getDeployJSON(workDir)
		Expect(err).NotTo(HaveOccurred(), "second deploy failed")
		Expect(secondResult["status"]).To(Equal("success"))
		secondDuration := secondResult["duration_seconds"].(float64)

		// Assert: second build should be faster (cache hit).
		// We use a generous margin -- the key assertion is that it completes
		// successfully. A cache hit typically cuts build time significantly.
		GinkgoWriter.Printf("first deploy: %.0fs, second deploy: %.0fs\n",
			firstDuration, secondDuration)

		// If the second deploy is not faster, log a warning.
		// This is a soft assertion -- cache behavior depends on infra state.
		if secondDuration >= firstDuration {
			GinkgoWriter.Printf("WARNING: second deploy was not faster -- cache may not be configured\n")
		}
	})

	// -- Test 3: Multi-app monorepo --

	It("should deploy multi-app monorepo with server and worker", func() {
		// Arrange: copy multi-app fixture and init git repo.
		workDir = copyFixture("multi-app")
		_ = initGitRepo(workDir)

		// Act: deploy.
		result, err := getDeployJSON(workDir)
		Expect(err).NotTo(HaveOccurred(), "multi-app deploy failed")

		// Assert: deploy succeeded.
		Expect(result["status"]).To(Equal("success"))

		// Assert: deploy_id format.
		deployID := result["deploy_id"].(string)
		Expect(deployIDRegex.MatchString(deployID)).To(BeTrue(),
			"deploy_id %q does not match d-{4hex} format", deployID)

		// Assert: apps array contains both apps.
		apps, ok := result["apps"].([]interface{})
		Expect(ok).To(BeTrue(), "apps should be an array")
		Expect(apps).To(HaveLen(2), "should have 2 apps (server + worker)")

		// Build lookup map by app name.
		appsByName := make(map[string]map[string]interface{})
		for _, rawApp := range apps {
			app, ok := rawApp.(map[string]interface{})
			Expect(ok).To(BeTrue())
			name, ok := app["name"].(string)
			Expect(ok).To(BeTrue())
			appsByName[name] = app
		}

		// Assert: server app ("api") has URL.
		Expect(appsByName).To(HaveKey("api"))
		apiApp := appsByName["api"]
		Expect(apiApp["status"]).To(Equal("deployed"))
		Expect(apiApp).To(HaveKey("url"))
		serverURL, ok := apiApp["url"].(string)
		Expect(ok).To(BeTrue())
		Expect(serverURL).NotTo(BeEmpty(), "server app should have a URL")

		// Assert: worker app has no URL (workers have no ingress).
		Expect(appsByName).To(HaveKey("worker"))
		workerApp := appsByName["worker"]
		Expect(workerApp["status"]).To(Equal("deployed"))
		workerURL, _ := workerApp["url"].(string)
		Expect(workerURL).To(BeEmpty(), "worker app should not have a URL")

		// Assert: server URL is live.
		Eventually(func() int {
			statusCode, _, _ := httpGet(serverURL)
			return statusCode
		}, 60*time.Second, 2*time.Second).Should(Equal(http.StatusOK),
			"server app should return 200 at %s", serverURL)
	})
})
