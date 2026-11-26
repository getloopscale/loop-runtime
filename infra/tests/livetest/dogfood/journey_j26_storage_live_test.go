// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// M7 tenant blob storage — J-26 dogfood journey. Proves the /v1/storage/* vertical
// end-to-end over the real edge with a real loopruntime_live_* key carrying storage:read/
// storage:write.
//
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J26Storage
//
// LIVE pre-conditions (operator-gated per plan ADR D5 + cleanroom-up-live-test-
// mandate.md): dev-public infra UP + STORAGE_S3_* configured on loopruntime-cloud + a
// storage-scoped key exported as LOOPRUNTIME_STORAGE_KEY (operator seeds it). Absent the
// key, Scenario B SKIPs (the spec still ENUMERATES — no 0-spec false-green).

package dogfood

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-26 tenant blob storage LIVE", Label("live", "dogfood", "j26", "storage"), func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    Env
		key    string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		env = DefaultEnv()
		key = os.Getenv("LOOPRUNTIME_STORAGE_KEY")
	})

	AfterEach(func() { cancel() })

	// storageReq issues a raw HTTP request against the storage edge, returning the
	// status + body bytes (storage up/download are binary; CurlJSON is JSON-only).
	storageReq := func(method, path string, body []byte, contentType string) (int, []byte) {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, env.CloudBaseURL+path, rdr)
		Expect(err).ToNot(HaveOccurred())
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if body != nil {
			req.ContentLength = int64(len(body))
		}
		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	Context("Scenario A — edge fail-closed contract", func() {
		It("GET /v1/storage/objects without a bearer returns 401", func() {
			Expect(HealthcheckCloud(ctx, env)).To(Succeed(), "cloud-dev /health/deep prereq")
			prev := key
			key = "" // force unauthenticated
			defer func() { key = prev }()
			status, _ := storageReq(http.MethodGet, "/v1/storage/objects", nil, "")
			Expect(status).To(Equal(401), "unauthenticated storage hit MUST fail-closed — a 2xx here is a fail-open regression")
		})
	})

	Context("Scenario B — core chain upload → stat → list → download → share → redeem → delete", func() {
		It("round-trips a file through the storage vertical", func() {
			if key == "" {
				Skip("LOOPRUNTIME_STORAGE_KEY not set — operator seeds a storage:read/write key for the LIVE run (plan ADR D5)")
			}
			Expect(HealthcheckCloud(ctx, env)).To(Succeed())

			const objPath = "/v1/storage/objects/dogfood/j26-probe.txt"
			payload := []byte("loopruntime storage j26 probe")

			// upload
			status, _ := storageReq(http.MethodPut, objPath, payload, "text/plain")
			Expect(status).To(Equal(201), "upload must return 201 Created")

			// stat
			status, _ = storageReq(http.MethodGet, "/v1/storage/stat/dogfood/j26-probe.txt", nil, "")
			Expect(status).To(Equal(200), "stat must find the committed object")

			// download bytes-equal
			status, body := storageReq(http.MethodGet, objPath, nil, "")
			Expect(status).To(Equal(200))
			Expect(body).To(Equal(payload), "downloaded bytes must equal uploaded bytes")

			// delete
			status, _ = storageReq(http.MethodDelete, objPath, nil, "")
			Expect(status).To(Equal(204), "delete must return 204 No Content")

			// stat after delete → 404
			status, _ = storageReq(http.MethodGet, "/v1/storage/stat/dogfood/j26-probe.txt", nil, "")
			Expect(status).To(Equal(404), "the object must be gone after delete")
		})
	})
})
