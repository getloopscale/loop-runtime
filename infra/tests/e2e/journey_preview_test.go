// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey 09: Preview Environments (CRUD via CLI)
//
// Migrated from: infra/tests/dogfood/journeys/journey-09-preview.sh
//
// Flow: create preview (API) → list → inspect → delete → verify gone
//
// Note: preview_lifecycle_test.go already covers the webhook simulation flow.
// This journey adds CLI command coverage (list/inspect/delete) on top.
package e2e

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey 09: Preview", Ordered, Label("journey"), func() {
	var (
		project   string
		previewID string
		prNumber  int
		workDir   string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		project = fmt.Sprintf("ginkgo-preview-%s", uniqueSuffix())
		prNumber = rand.Intn(9000) + 1000

		// Create a workspace with loopzero.yaml so CLI commands have project context.
		var err error
		workDir, err = os.MkdirTemp("", "loop-runtime-e2e-preview-")
		Expect(err).NotTo(HaveOccurred())

		loopRuntimeYAML := fmt.Sprintf("version: 1\nproject: %s\napps:\n  api:\n    path: .\n    type: server\n    build: dockerfile\n", project)
		err = os.WriteFile(workDir+"/loopzero.yaml", []byte(loopRuntimeYAML), 0644)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		// Cleanup: delete preview if still exists.
		if previewID != "" {
			runLoopRuntimeInDir(workDir, "preview", "delete", previewID, "--yes", "--json")
		}
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("creates preview via API", func() {
		// Create preview using the API directly (as journey-09 does).
		commitSHA := fmt.Sprintf("%040x", rand.Int63())

		body := fmt.Sprintf(`{
			"pr_number": %d,
			"branch": "feature/ginkgo-test",
			"commit_sha": "%s",
			"repo_full_name": "getloopscale/loopzerokit-example",
			"installation_id": 99999999,
			"ttl_hours": 1
		}`, prNumber, commitSHA)

		url := fmt.Sprintf("%s/api/v1/projects/%s/previews", apiURL(), project)
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		Expect(err).NotTo(HaveOccurred())

		req.Header.Set("Authorization", "Bearer "+resolveToken())
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("ginkgo-preview-%s", uniqueSuffix()))

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		Expect(err).NotTo(HaveOccurred())

		// Extract preview ID.
		id, ok := result["id"].(string)
		Expect(ok).To(BeTrue(), "preview create should return id field")
		Expect(id).To(HavePrefix("prev-"))
		previewID = id

		GinkgoWriter.Printf("created preview: %s for PR #%d\n", previewID, prNumber)
	})

	It("preview list shows the created preview", func() {
		stdout, _, exitCode := runLoopRuntimeInDir(workDir, "preview", "list", "--json")
		Expect(exitCode).To(Equal(0))
		Expect(stdout).To(ContainSubstring(previewID),
			"preview list should contain the created preview")

		assertNoForbiddenTermsInAll(map[string]string{
			"preview list stdout": stdout,
		})
	})

	It("preview inspect returns details", func() {
		stdout, _, exitCode := runLoopRuntimeInDir(workDir, "preview", "inspect", previewID, "--json")
		Expect(exitCode).To(Equal(0))

		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// PR number should match.
		prNum, _ := result["pr_number"].(float64)
		Expect(int(prNum)).To(Equal(prNumber),
			"inspect should return matching PR number")

		assertNoForbiddenTermsInAll(map[string]string{
			"preview inspect stdout": stdout,
		})
	})

	It("preview delete succeeds", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "preview", "delete", previewID, "--yes", "--json")

		Expect(exitCode).To(Equal(0),
			"loop-runtime preview delete should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"preview delete stdout": stdout,
			"preview delete stderr": stderr,
		})
	})

	It("preview gone from list after delete", func() {
		Eventually(func() bool {
			stdout, _, _ := runLoopRuntimeInDir(workDir, "preview", "list", "--json")
			result, err := parseJSON(stdout)
			if err != nil {
				return true
			}
			previews, ok := result["previews"].([]interface{})
			if !ok {
				return true
			}
			for _, p := range previews {
				pm, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				id, _ := pm["id"].(string)
				status, _ := pm["status"].(string)
				if id == previewID && status != "deleted" && status != "deleting" {
					return false
				}
			}
			return true
		}, cleanupTimeout, cleanupPollInterval).Should(BeTrue(),
			"preview should be deleted from list")
	})

	It("audit records preview operations", func() {
		assertAuditEntry("preview.created", project)
	})
})
