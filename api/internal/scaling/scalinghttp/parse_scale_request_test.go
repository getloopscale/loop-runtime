// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scalinghttp

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Every basis path of parseScaleRequest: each refusal, the environment
// default, and both accepted modes.
func TestParseScaleRequest_BasisPaths(t *testing.T) {
	cases := []struct {
		name               string
		body               string
		wantCode           string
		wantFixed, wantAut bool
		wantEnv            string
	}{
		{name: "invalid JSON", body: `{`, wantCode: "INVALID_REQUEST"},
		{name: "missing app", body: `{"replicas":2}`, wantCode: "MISSING_FIELD"},
		{name: "fixed and autoscaling mixed", body: `{"app":"web","replicas":2,"max":4}`, wantCode: "INVALID_REQUEST"},
		{name: "neither mode", body: `{"app":"web"}`, wantCode: "MISSING_FIELD"},
		{name: "zero replicas", body: `{"app":"web","replicas":0}`, wantCode: "INVALID_REPLICAS"},
		{name: "negative replicas", body: `{"app":"web","replicas":-5}`, wantCode: "INVALID_REPLICAS"},
		{name: "negative min", body: `{"app":"web","min":-1}`, wantCode: "INVALID_REPLICAS"},
		{name: "zero max", body: `{"app":"web","max":0}`, wantCode: "INVALID_REPLICAS"},
		{name: "fixed defaults the environment", body: `{"app":"web","replicas":2}`, wantFixed: true, wantEnv: "production"},
		{name: "fixed keeps the environment", body: `{"app":"web","environment":"staging","replicas":2}`, wantFixed: true, wantEnv: "staging"},
		{name: "autoscaling by min only", body: `{"app":"web","min":0}`, wantAut: true, wantEnv: "production"},
		{name: "autoscaling by cpu only", body: `{"app":"web","cpu":70}`, wantAut: true, wantEnv: "production"},
		{name: "autoscaling by memory only", body: `{"app":"web","memory":70}`, wantAut: true, wantEnv: "production"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, fixed, auto, err := parseScaleRequest(httptest.NewRequest("POST", "/", strings.NewReader(c.body)))
			if c.wantCode != "" {
				if err == nil || err.Code != c.wantCode {
					t.Fatalf("err = %v, want code %s", err, c.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fixed != c.wantFixed || auto != c.wantAut || req.Environment != c.wantEnv {
				t.Errorf("got fixed=%v auto=%v env=%q, want fixed=%v auto=%v env=%q",
					fixed, auto, req.Environment, c.wantFixed, c.wantAut, c.wantEnv)
			}
		})
	}
}
