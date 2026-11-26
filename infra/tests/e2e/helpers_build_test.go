// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package e2e provides shared test helpers for all E2E test suites.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/gomega"
)

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

// deployAndWaitSuccess runs loopruntime deploy --json in the given directory, waits for
// the build to reach SUCCEEDED, and returns the build ID and full JSON response.
func deployAndWaitSuccess(dir, env string, timeout time.Duration) (buildID string, result map[string]interface{}) {
	stdout, stderr, exitCode := execLoopRuntime(loopruntimeInvocation{dir: dir},
		"deploy", "--env", env, "--no-wait", "--json")

	Expect(exitCode).To(Equal(0),
		"loopruntime deploy failed with exit %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)

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
