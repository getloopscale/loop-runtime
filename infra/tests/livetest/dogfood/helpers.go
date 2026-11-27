// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package dogfood implements T3.2 of plan canonical-tests-rewrite-v2 v1.0
// per ADR-2026-06-17-T3 D1 (Ginkgo BDD + helpers replacing the 16 v1 bash
// wrappers archived in archive/canonical-tests-v1/scripts/local/dogfood-*.sh).
//
// Per-journey specs (dogfood_jXX_live_test.go) compose these helpers to
// exercise dashboard journeys end-to-end against dev-public. Each journey
// is gated por build tag `live` so hermetic CI runs Go helper unit tests
// without LIVE infra dependency.
package dogfood

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Env captures common per-journey environment configuration.
type Env struct {
	CloudBaseURL  string // https://cloud-dev.getloopscale.com
	AppBaseURL    string // https://app-dev.getloopscale.com
	AuthBaseURL   string // https://auth-dev.getloopscale.com
	DeepHealthURL string // https://cloud-dev.getloopscale.com/health/deep
	JWTPath       string // ~/.loop-runtime/local-dogfood-token.txt
	HTTPClient    *http.Client
}

// The variables that point the suite at another environment than dev-public, such as
// staging (infra/staging/validate.sh). Each one left unset keeps the dev-public value.
const (
	CloudURLEnv      = "LOOP_RUNTIME_DOGFOOD_CLOUD_URL"
	AppURLEnv        = "LOOP_RUNTIME_DOGFOOD_APP_URL"
	AuthURLEnv       = "LOOP_RUNTIME_DOGFOOD_AUTH_URL"
	DeepHealthURLEnv = "LOOP_RUNTIME_DOGFOOD_DEEP_HEALTH_URL"
	JWTPathEnv       = "LOOP_RUNTIME_DOGFOOD_JWT_PATH"
)

// DefaultEnv returns the environment the variables above name, dev-public for each one
// that is unset, with a 30s HTTP timeout.
//
// The deep health is a URL of its own because it is not served by loop-console
// everywhere: on staging the deep health is loop-runtime-api's /api/v1/health/deep, and
// loop-console answers 404 on /health/deep.
func DefaultEnv() Env {
	cloud := envOr(CloudURLEnv, "https://cloud-dev.getloopscale.com")
	return Env{
		CloudBaseURL:  cloud,
		AppBaseURL:    envOr(AppURLEnv, "https://app-dev.getloopscale.com"),
		AuthBaseURL:   envOr(AuthURLEnv, "https://auth-dev.getloopscale.com"),
		DeepHealthURL: envOr(DeepHealthURLEnv, cloud+"/health/deep"),
		JWTPath:       envOr(JWTPathEnv, os.Getenv("HOME")+"/.loop-runtime/local-dogfood-token.txt"),
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// LoadJWT reads the JWT seeded by infra/scripts/local/seed-admin-loop-runtime-dev.sh.
// The file is canonical (mode 0600) per memory feedback_post_cleanroom_seed_admin_canonical.
func LoadJWT(env Env) (string, error) {
	data, err := os.ReadFile(env.JWTPath)
	if err != nil {
		return "", fmt.Errorf("read JWT at %s: %w (run seed-admin-loop-runtime-dev.sh)", env.JWTPath, err)
	}
	jwt := strings.TrimSpace(string(data))
	if jwt == "" {
		return "", fmt.Errorf("JWT at %s is empty", env.JWTPath)
	}
	return jwt, nil
}

// CurlJSON performs an authenticated GET/POST and returns body as map.
// jwt may be "" for unauthenticated probes.
func CurlJSON(ctx context.Context, env Env, method, url, jwt string, bodyJSON []byte) (int, map[string]any, error) {
	var body io.Reader
	if bodyJSON != nil {
		body = strings.NewReader(string(bodyJSON))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, fmt.Errorf("build %s %s: %w", method, url, err)
	}
	req.Header.Set("Accept", "application/json")
	if bodyJSON != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	resp, err := env.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read body: %w", err)
	}
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out, nil
}

// HealthcheckCloud asserts the deep health (Env.DeepHealthURL) returns 200.
// Replaces the per-script preflight functions in v1 dogfood wrappers.
func HealthcheckCloud(ctx context.Context, env Env) error {
	status, _, err := CurlJSON(ctx, env, "GET", env.DeepHealthURL, "", nil)
	if err != nil {
		return fmt.Errorf("deep health: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("deep health %s: HTTP %d (want 200)", env.DeepHealthURL, status)
	}
	return nil
}

// Pause waits for d, honoring context cancellation.
func Pause(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
