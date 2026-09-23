// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// buildAPI is the one client of the LoopZero build contract the load scenarios
// speak — api/internal/routes/builds:
//
//	POST /api/v1/builds       202 {"build_id": ..., "stream_url": ...}
//	GET  /api/v1/builds/{id}  200 {"build_id": ..., "status": ...}
//
// RealDeployHarness and DeploySimulator used to carry a copy each, and the
// copies disagreed: the simulator read `id` and polled `state`, fields the API
// does not send, so it never observed a finished build. One client means one
// reading of the contract.
type buildAPI struct {
	apiURL string
	client *http.Client
	// auth applies the caller's identity headers to every request.
	auth func(*http.Request)
	// pollInterval is how often waitForBuild asks for the status.
	pollInterval time.Duration
	// retryBackoff is the wait before each retry of a submit refused with 429
	// (cell at capacity) or 503 (circuit open) — the CLI's retry policy.
	retryBackoff []time.Duration
}

// defaultSubmitBackoff mirrors the CLI (cli/src/api/client.ts): load shedding is
// designed behaviour, not a failure, and is retried 1s/2s/4s/8s/16s.
var defaultSubmitBackoff = []time.Duration{
	1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
}

// submitBuild posts payload and returns the build id the API assigned.
func (b buildAPI) submitBuild(ctx context.Context, payload any, idempotencyKey string) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal build payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= len(b.retryBackoff); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(b.retryBackoff[attempt-1]):
			}
		}

		status, respBody, err := b.post(ctx, body, idempotencyKey)
		if err != nil {
			return "", err
		}
		if status == http.StatusAccepted || status == http.StatusCreated {
			var resp struct {
				BuildID string `json:"build_id"`
			}
			if err := json.Unmarshal(respBody, &resp); err != nil {
				return "", fmt.Errorf("decode build submit response %q: %w", respBody, err)
			}
			if resp.BuildID == "" {
				return "", fmt.Errorf("build submit HTTP %d carried no build_id: %s", status, respBody)
			}
			return resp.BuildID, nil
		}

		lastErr = fmt.Errorf("submit build: HTTP %d: %s", status, respBody)
		if status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable {
			return "", lastErr
		}
	}
	return "", fmt.Errorf("submit exhausted retries: %w", lastErr)
}

func (b buildAPI) post(ctx context.Context, body []byte, idempotencyKey string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL+"/api/v1/builds", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	b.auth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("submit build: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read build submit response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// waitForBuild polls the build until it reaches a terminal status. It returns
// SUCCEEDED with no error, FAILED/CANCELLED with an error, and TIMEOUT when ctx
// ends first. A poll that fails to reach the API is retried on the next tick:
// under load a dropped status read is expected and says nothing about the build.
func (b buildAPI) waitForBuild(ctx context.Context, buildID string) (string, time.Duration, error) {
	start := time.Now()
	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "TIMEOUT", time.Since(start), fmt.Errorf("build %s: %w", buildID, ctx.Err())
		case <-ticker.C:
			status, ok := b.status(ctx, buildID)
			if !ok {
				continue
			}
			switch statemachine.State(status) {
			case statemachine.BuildSucceeded:
				return status, time.Since(start), nil
			case statemachine.BuildFailed, statemachine.BuildCancelled:
				return status, time.Since(start), fmt.Errorf("build %s: %s", buildID, status)
			}
		}
	}
}

// status reads the build's status once; ok is false when the read failed.
func (b buildAPI) status(ctx context.Context, buildID string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.apiURL+"/api/v1/builds/"+buildID, nil)
	if err != nil {
		return "", false
	}
	b.auth(req)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var build struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&build); err != nil {
		return "", false
	}
	return build.Status, true
}
