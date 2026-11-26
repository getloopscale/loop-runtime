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
