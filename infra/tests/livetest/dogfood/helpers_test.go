// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package dogfood

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T3.2 helper-level hermetic tests. Validates the dogfood helpers without
// touching LIVE dev-public infrastructure. Per-journey LIVE specs live in
// journey_jXX_live_test.go files gated por build tag `live`.

func TestDefaultEnv_SetsCanonicalURLs(t *testing.T) {
	env := DefaultEnv()
	if env.CloudBaseURL != "https://cloud-dev.getloopscale.com" {
		t.Errorf("CloudBaseURL = %q", env.CloudBaseURL)
	}
	if env.AppBaseURL != "https://app-dev.getloopscale.com" {
		t.Errorf("AppBaseURL = %q", env.AppBaseURL)
	}
	if env.AuthBaseURL != "https://auth-dev.getloopscale.com" {
		t.Errorf("AuthBaseURL = %q", env.AuthBaseURL)
	}
	if env.HTTPClient.Timeout != 30*time.Second {
		t.Errorf("HTTPClient.Timeout = %v, want 30s", env.HTTPClient.Timeout)
	}
}

func TestDefaultEnv_TargetsTheEnvironmentTheVariablesName(t *testing.T) {
	t.Setenv("LOOPZERO_DOGFOOD_CLOUD_URL", "https://cloud.staging.example")
	t.Setenv("LOOPZERO_DOGFOOD_APP_URL", "https://app.staging.example")
	t.Setenv("LOOPZERO_DOGFOOD_AUTH_URL", "https://auth.staging.example")
	t.Setenv("LOOPZERO_DOGFOOD_DEEP_HEALTH_URL", "https://api.staging.example/api/v1/health/deep")
	t.Setenv("LOOPZERO_DOGFOOD_JWT_PATH", "/var/lib/staging/jwt")

	env := DefaultEnv()

	want := Env{
		CloudBaseURL:  "https://cloud.staging.example",
		AppBaseURL:    "https://app.staging.example",
		AuthBaseURL:   "https://auth.staging.example",
		DeepHealthURL: "https://api.staging.example/api/v1/health/deep",
		JWTPath:       "/var/lib/staging/jwt",
	}
	got := env
	got.HTTPClient = nil
	if got != want {
		t.Errorf("DefaultEnv() = %+v, want %+v", got, want)
	}
}

func TestDefaultEnv_KeepsTheDevPublicDeepHealthWhenNoVariableIsSet(t *testing.T) {
	env := DefaultEnv()
	if env.DeepHealthURL != "https://cloud-dev.getloopscale.com/health/deep" {
		t.Errorf("DeepHealthURL = %q", env.DeepHealthURL)
	}
}

func TestHealthcheckCloud_ProbesTheDeepHealthURL(t *testing.T) {
	var probed string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probed = r.URL.Path
		if r.URL.Path != "/api/v1/health/deep" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	env := Env{CloudBaseURL: "http://unused.invalid", DeepHealthURL: srv.URL + "/api/v1/health/deep", HTTPClient: srv.Client()}

	err := HealthcheckCloud(context.Background(), env)

	if probed != "/api/v1/health/deep" {
		t.Fatalf("probed %q, want the deep health URL", probed)
	}
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("a 503 deep health returned %v, want an error naming HTTP 503", err)
	}
}

func TestLoadJWT_Reads0600Token(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "token.txt")
	if err := os.WriteFile(path, []byte("eyJhbGc.testtoken.abc\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := Env{JWTPath: path}
	got, err := LoadJWT(env)
	if err != nil {
		t.Fatal(err)
	}
	if got != "eyJhbGc.testtoken.abc" {
		t.Errorf("LoadJWT = %q (trim whitespace failed)", got)
	}
}

func TestLoadJWT_RejectsMissingFile(t *testing.T) {
	env := Env{JWTPath: "/tmp/definitely-does-not-exist-12345"}
	_, err := LoadJWT(env)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "seed-admin-loopzero-dev.sh") {
		t.Errorf("error message should hint at seed script: %v", err)
	}
}

func TestLoadJWT_RejectsEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	if err := os.WriteFile(path, []byte("   \n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := Env{JWTPath: path}
	_, err := LoadJWT(env)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' error, got %v", err)
	}
}

func TestCurlJSON_ParsesResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			t.Errorf("missing or wrong auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","count":42}`))
	}))
	defer srv.Close()

	env := DefaultEnv()
	env.HTTPClient = srv.Client()
	status, body, err := CurlJSON(context.Background(), env, "GET", srv.URL, "test-jwt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Errorf("status = %d", status)
	}
	if body["status"] != "ok" {
		t.Errorf("body.status = %v", body["status"])
	}
	// JSON numbers parse as float64
	if body["count"].(float64) != 42 {
		t.Errorf("body.count = %v", body["count"])
	}
}

func TestPause_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := Pause(ctx, 1*time.Second)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}
