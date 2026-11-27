// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// B-176 — two write paths, one condition, one of them counting.
//
// `POST /secrets` and `POST /secrets/bulk` both persist a row and both then
// look at h.k8sSync. The single-set path emits
// loop_runtime_secret_externalsecret_total{result="skip"} when it is nil; the bulk
// path recorded the entry as "pending" and returned without touching any
// counter. A bulk write against a deployment with no workload sync therefore
// landed rows in `secrets` and left sum(loop_runtime_secret_externalsecret_total) at
// zero.
//
// That sum is not decoration. The B-126 canary deferral
// (docs/wiki/decisions/2026-09-04-secret-decrypt-canary-deferral.md § 2) names
// it as the trigger that fires "without anyone looking" — the signal that the
// `secrets` table stopped being empty and the deferred T2 canary became work.
// The hole sat exactly where the event would have passed.
//
// This file asserts BOTH paths, in one test, on purpose. The asymmetry got in
// once already; a test that watches only the single-set path would not have
// caught it, and would not catch it coming back.

package secretshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// externalSecretSkips reads the "skip" series. Used as a DELTA: the counter is
// a package-level global shared with every other test in this binary.
func externalSecretSkips() float64 {
	return testutil.ToFloat64(metrics.SecretExternalSecretTotal.WithLabelValues("skip"))
}

// TestExternalSecretSkip_BothWritePathsEmitWhenSyncIsNil locks the symmetry.
// Deleting either sub-test's increment turns the B-126 deferral trigger back
// into a signal that can miss the event it exists to announce.
func TestExternalSecretSkip_BothWritePathsEmitWhenSyncIsNil(t *testing.T) {
	t.Run("single set", func(t *testing.T) {
		store := &fakeSecretStoreT01{}
		h := NewSecretHandler(store, &lockCountingFake{}) // k8sSync stays nil — the condition under test

		body, err := json.Marshal(map[string]string{
			"environment": "production",
			"app":         "api",
			"name":        "DB_PASS",
			"value":       "s3cret",
		})
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=proj-x", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = routestest.WithAuth(req, routestest.DefaultUserContext())
		w := httptest.NewRecorder()

		before := externalSecretSkips()
		h.Set(w, req)

		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("want 200/201, got %d body=%s", w.Code, w.Body.String())
		}
		if store.setCalls != 1 {
			t.Fatalf("the row must be persisted before the counter is judged; store.Set calls = %d", store.setCalls)
		}
		if delta := externalSecretSkips() - before; delta != 1 {
			t.Fatalf("single-set path moved the skip counter by %v, want exactly 1 per persisted row", delta)
		}
	})

	t.Run("bulk", func(t *testing.T) {
		store := &fakeSecretStoreT01{}
		h := NewSecretHandler(store, &lockCountingFake{}) // k8sSync stays nil — the same condition

		entry := secretBulkEntry{
			Name:  "DB_PASS",
			Value: "s3cret",
			Envs:  []string{"production", "staging"},
		}

		before := externalSecretSkips()
		results := h.bulkProcess(context.Background(), routestest.DefaultUserContext(), "proj-x", []secretBulkEntry{entry})

		if store.setCalls != len(entry.Envs) {
			t.Fatalf("both rows must be persisted before the counter is judged; store.Set calls = %d, want %d",
				store.setCalls, len(entry.Envs))
		}
		// One emission per persisted row, matching the single-set path's
		// once-per-row contract. A per-entry emission would under-count a
		// multi-env write and reintroduce the same class of blind spot.
		if delta := externalSecretSkips() - before; delta != float64(len(entry.Envs)) {
			t.Fatalf("bulk path moved the skip counter by %v across %d envs, want exactly %d — B-176: a bulk write with no workload sync must not leave sum(loop_runtime_secret_externalsecret_total) at zero",
				delta, len(entry.Envs), len(entry.Envs))
		}
		// The API-facing status is unchanged by B-176: the caller still reads
		// "pending". Only the metric was missing.
		for _, env := range entry.Envs {
			if got := results[0].SyncStatusPerEnv[env]; got != "pending" {
				t.Fatalf("sync status for env %q = %q, want %q — the counter was the gap, not the response contract",
					env, got, "pending")
			}
		}
	})
}
