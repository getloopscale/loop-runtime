// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// BuildAPI is the one client of the LoopRuntime build contract the load scenarios
// speak — api/internal/builds/buildshttp:
//
//	POST /api/v1/builds       202 {"build_id": ..., "stream_url": ...}
//	GET  /api/v1/builds/{id}  200 {"build_id": ..., "status": ...}
//
// RealDeployHarness and DeploySimulator used to carry a copy each, and the
// copies disagreed: the simulator read `id` and polled `state`, fields the API
// does not send, so it never observed a finished build. One client means one
// reading of the contract.
type BuildAPI struct {
	APIURL string
	Client *http.Client
	// Auth applies the caller's identity headers to every request.
	Auth func(*http.Request)
	// PollInterval is how often WaitForBuild asks for the status.
	PollInterval time.Duration
	// RetryBackoff is the wait before each retry of a submit refused with 429
	// (cell at capacity) or 503 (circuit open) — the CLI's retry policy.
	RetryBackoff []time.Duration
}

// DefaultSubmitBackoff mirrors the CLI (cli/src/api/client.ts): load shedding is
// designed behaviour, not a failure, and is retried 1s/2s/4s/8s/16s.
var DefaultSubmitBackoff = []time.Duration{
	1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
}

// SubmitBuild posts payload and returns the build id the API assigned.
func (b BuildAPI) SubmitBuild(ctx context.Context, payload any, idempotencyKey string) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal build payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= len(b.RetryBackoff); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(b.RetryBackoff[attempt-1]):
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

func (b BuildAPI) post(ctx context.Context, body []byte, idempotencyKey string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.APIURL+"/api/v1/builds", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	b.Auth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)

	resp, err := b.Client.Do(req)
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

// WaitForBuild polls the build until it reaches a terminal status. It returns
// SUCCEEDED with no error, FAILED/CANCELLED with an error, and TIMEOUT when ctx
// ends first. A poll that fails to reach the API is retried on the next tick:
// under load a dropped status read is expected and says nothing about the build.
func (b BuildAPI) WaitForBuild(ctx context.Context, buildID string) (string, time.Duration, error) {
	start := time.Now()
	ticker := time.NewTicker(b.PollInterval)
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
func (b BuildAPI) status(ctx context.Context, buildID string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.APIURL+"/api/v1/builds/"+buildID, nil)
	if err != nil {
		return "", false
	}
	b.Auth(req)
	resp, err := b.Client.Do(req)
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

// ServiceAuthHeaders authenticates a request as tenant t through the
// X-Service-Auth bypass the load and capacity benches use.
func ServiceAuthHeaders(secret string, t TenantIdentity) func(*http.Request) {
	return func(req *http.Request) {
		req.Header.Set("X-Service-Auth", secret)
		req.Header.Set("X-LoopRuntime-User-ID", t.UserID)
		req.Header.Set("X-LoopRuntime-Tenant", t.TenantID)
		req.Header.Set("X-LoopRuntime-Email", t.Email)
	}
}

// UploadSource posts a source bundle with its manifest to
// POST /api/v1/source/upload and returns the commit SHA the API recorded.
func UploadSource(ctx context.Context, client *http.Client, apiURL string, auth func(*http.Request), loopruntimeYAML string, bundle []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("loopruntime_yaml", loopruntimeYAML); err != nil {
		return "", fmt.Errorf("write loopruntime_yaml field: %w", err)
	}
	bundlePart, err := writer.CreateFormFile("bundle", "source.bundle")
	if err != nil {
		return "", fmt.Errorf("create bundle part: %w", err)
	}
	if _, err := bundlePart.Write(bundle); err != nil {
		return "", fmt.Errorf("write bundle part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/v1/source/upload", &body)
	if err != nil {
		return "", err
	}
	auth(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read upload response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, respBody)
	}
	var uploadResp struct {
		CommitSHA string `json:"commit_sha"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", fmt.Errorf("decode upload response %q: %w", respBody, err)
	}
	return uploadResp.CommitSHA, nil
}
