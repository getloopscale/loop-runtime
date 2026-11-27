// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"regexp"
	"strings"
	"testing"
)

// The data-plane host checks (17a/17c/17e) prove each service's process answers on its
// host port. They probe the auth-exempt liveness endpoint, which answers 200: the
// ecosystem's HTTP surface contract (B-115) makes `/v1/health` public in every service.
// 17c used to expect 401 there — true of the RAG image the compose deployment ran, which
// predated the contract. After RAG moved to Helmfile on a current image (#446), the check
// failed on a healthy service. Whether auth is enforced is the edge checks' job (#458).
func TestDataPlaneHostChecksProbeThePublicLivenessEndpoint(t *testing.T) {
	body := readScript(t, "seed-post-cleanroom-all.sh")
	for _, step := range []string{"17a-data-plane-memory-host", "17c-data-plane-rag-host", "17e-data-plane-lens-host"} {
		line := regexp.MustCompile(`run_step "` + step + `" "[^\n]*`).FindString(body)
		if line == "" {
			t.Errorf("%s not found", step)
			continue
		}
		if strings.Contains(line, "'401'") {
			t.Errorf("%s expects 401 from a liveness endpoint the contract makes public:\n%s", step, line)
		}
		if !strings.Contains(line, "curl -fsS") {
			t.Errorf("%s does not fail on a non-2xx answer:\n%s", step, line)
		}
	}
}
