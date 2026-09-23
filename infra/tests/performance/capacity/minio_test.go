// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestCapacityMinIOPut benchmarks MinIO PUT throughput via native S3 API.
//
// Context: bug #35 (MinIO saturation) caused cache-restore timeouts in L3 v1.
// Standalone MinIO with 512Mi memory saturated at ~50 concurrent clients.
// Post-fix (Fase 1: 2CPU/4Gi + MINIO_API_REQUESTS_MAX=10000), baseline
// expectation:
//   - p95 < 1s for 1MB blob PUT
//   - error rate < 1% at 100 RPS sustained
//
// Run:
//
//	LOOPZERO_CAPACITY=1 LOOPZERO_MINIO_ENDPOINT=http://localhost:19000 \
//	LOOPZERO_MINIO_ACCESS_KEY=... LOOPZERO_MINIO_SECRET_KEY=... \
//	LOOPZERO_MINIO_BUCKET=loopzero-artifacts \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityMinIO -v \
//	  -args -rps=50 -duration=60s -workers=20
func TestCapacityMinIOPut(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	endpoint := os.Getenv("LOOPZERO_MINIO_ENDPOINT")
	accessKey := os.Getenv("LOOPZERO_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("LOOPZERO_MINIO_SECRET_KEY")
	bucket := os.Getenv("LOOPZERO_MINIO_BUCKET")
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		t.Fatal("LOOPZERO_MINIO_ENDPOINT, _ACCESS_KEY, _SECRET_KEY, _BUCKET required")
	}

	cfg := NewConfig("minio-put")
	t.Logf("config: rps=%v duration=%v workers=%d bucket=%s", cfg.RPS, cfg.Duration, cfg.Workers, bucket)

	// 1MB payload — representative of a build cache entry chunk.
	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	result, _ := RunOpenLoop(context.Background(), cfg, "minio-put-1mb", AnyHTTPError,
		func(ctx context.Context, seq int64) (int, error) {
			key := fmt.Sprintf("capacity-bench/obj-%d-%d", seq, time.Now().UnixNano())
			url := fmt.Sprintf("%s/%s/%s", endpoint, bucket, key)
			req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
			if err != nil {
				return 0, err
			}
			// Simplified auth: MinIO accepts anonymous if policy allows (or use pre-signed).
			// For capacity bench, a test bucket with policy=public (PUT) is simplest.
			req.SetBasicAuth(accessKey, secretKey)
			req.Header.Set("Content-Type", "application/octet-stream")
			return DoHTTP(req)
		})
	result.Notes = "1MB PUT — representative of build cache chunk"

	t.Logf("=== minio-put-1mb ===")
	t.Logf("  requests:    %d (errors: %d, rate: %.2f%%)", result.TotalReqs, result.Errors, result.ErrorRate*100)
	t.Logf("  actual rps:  %.1f (target: %.0f)", result.ActualRPS, cfg.RPS)
	t.Logf("  p50/95/99:   %v / %v / %v", result.P50, result.P95, result.P99)

	if err := WriteResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:      %s", cfg.OutFile)
	}

	// Soft assertions — bug #35 regression guard
	if result.P95 > 1*time.Second {
		t.Errorf("p95 > 1s — MinIO saturating (bug #35 regression?)")
	}
	if result.ErrorRate > 0.01 {
		t.Errorf("error rate > 1%% at rps=%v", cfg.RPS)
	}
}
