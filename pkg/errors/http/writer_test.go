// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package http

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

func TestWriteJSON_EmitsRFC9457Shape(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/projects/foo/deploys", nil)
	WriteJSON(rr, req, loopruntimeerrors.ErrLockTimeout)

	if got := rr.Code; got != http.StatusConflict {
		t.Errorf("status = %d, want 409", got)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	var pd map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"type", "title", "status", "detail", "code"} {
		if _, ok := pd[key]; !ok {
			t.Errorf("missing required field %q in payload: %v", key, pd)
		}
	}
	if pd["code"] != "LOCK_TIMEOUT" {
		t.Errorf("code = %v, want LOCK_TIMEOUT", pd["code"])
	}
	if !strings.HasPrefix(pd["type"].(string), "https://getloopscale.com/errors/") {
		t.Errorf("type = %v, want https://getloopscale.com/errors/* URI", pd["type"])
	}
	if pd["instance"] != "/projects/foo/deploys" {
		t.Errorf("instance = %v, want /projects/foo/deploys", pd["instance"])
	}
}

func TestWriteJSON_NonLoopRuntimeErrorEmitsInternalError(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	WriteJSON(rr, req, stderrors.New("oops"))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("non-LoopRuntimeError must produce 500; got %d", rr.Code)
	}
	var pd map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pd["code"] != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR code; got %v", pd["code"])
	}
}

func TestWriteJSON_OmitsInstanceWhenRequestNil(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, nil, loopruntimeerrors.ErrUnauthorized)
	var pd map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &pd)
	if _, ok := pd["instance"]; ok {
		t.Errorf("instance should be omitted when request is nil; got: %v", pd)
	}
}

func TestKebabCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LOCK_TIMEOUT", "lock-timeout"},
		{"REPLICAS_EXCEED_PLAN", "replicas-exceed-plan"},
		{"INTERNAL_ERROR", "internal-error"},
		{"", ""},
	}
	for _, c := range cases {
		if got := kebabCase(c.in); got != c.want {
			t.Errorf("kebabCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTitleFromCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LOCK_TIMEOUT", "Lock Timeout"},
		{"REPLICAS_EXCEED_PLAN", "Replicas Exceed Plan"},
		{"INTERNAL_ERROR", "Internal Error"},
		{"", "Error"},
	}
	for _, c := range cases {
		if got := titleFromCode(c.in); got != c.want {
			t.Errorf("titleFromCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
