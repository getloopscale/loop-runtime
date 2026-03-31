// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
)

func TestFindCondition_Found(t *testing.T) {
	conds := []metav1.Condition{
		{Type: "Foo", Status: metav1.ConditionTrue},
		{Type: conditions.TypeApplicationHealthy, Status: metav1.ConditionFalse},
	}

	c := findCondition(conds, conditions.TypeApplicationHealthy)
	if c == nil {
		t.Fatal("expected to find ApplicationHealthy condition")
	}
	if c.Status != metav1.ConditionFalse {
		t.Errorf("expected status False, got %s", c.Status)
	}
}

func TestFindCondition_NotFound(t *testing.T) {
	conds := []metav1.Condition{
		{Type: "Foo", Status: metav1.ConditionTrue},
	}

	c := findCondition(conds, conditions.TypeApplicationHealthy)
	if c != nil {
		t.Error("expected nil for missing condition")
	}
}

func TestReaperController_RequestRollback_Success(t *testing.T) {
	var receivedBody map[string]string
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rc := &ReaperController{
		APIBaseURL: server.URL,
		APIToken:   "test-token",
	}

	app := newTestApp("unhealthy-app", map[string]string{
		labelTenant:      "acme",
		labelProject:     "webapp",
		labelEnvironment: "production",
	})

	err := rc.requestRollback(t.Context(), app)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedBody["project_id"] != "webapp" {
		t.Errorf("expected project_id=webapp, got %s", receivedBody["project_id"])
	}
	if receivedBody["environment"] != "production" {
		t.Errorf("expected environment=production, got %s", receivedBody["environment"])
	}
	if receivedHeaders.Get("X-Triggered-By") != "reaper-controller" {
		t.Errorf("expected X-Triggered-By header, got %s", receivedHeaders.Get("X-Triggered-By"))
	}
	if receivedHeaders.Get("Authorization") != "Bearer test-token" {
		t.Errorf("expected Bearer auth header")
	}
}

func TestReaperController_RequestRollback_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict) // Lock contention
	}))
	defer server.Close()

	rc := &ReaperController{APIBaseURL: server.URL}
	app := newTestApp("locked-app", map[string]string{
		labelTenant: "acme", labelProject: "webapp", labelEnvironment: "production",
	})

	err := rc.requestRollback(t.Context(), app)
	if err == nil {
		t.Fatal("expected error on 409 conflict")
	}
}

func TestReaperController_RequestRollback_NoHealthyRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // No healthy release
	}))
	defer server.Close()

	rc := &ReaperController{APIBaseURL: server.URL}
	app := newTestApp("no-history-app", map[string]string{
		labelTenant: "acme", labelProject: "webapp", labelEnvironment: "production",
	})

	err := rc.requestRollback(t.Context(), app)
	if err != nil {
		t.Fatalf("404 should not return error (no healthy release), got: %v", err)
	}
}

func TestReaperController_RequestRollback_MissingLabels(t *testing.T) {
	rc := &ReaperController{APIBaseURL: "http://localhost:9999"}
	app := newTestApp("no-labels", map[string]string{})

	err := rc.requestRollback(t.Context(), app)
	if err == nil {
		t.Fatal("expected error when labels are missing")
	}
}

func TestDefaultUnhealthyThreshold(t *testing.T) {
	if DefaultUnhealthyThreshold != 5*time.Minute {
		t.Errorf("default threshold should be 5 minutes, got %v", DefaultUnhealthyThreshold)
	}
}

func TestReaperRequeueInterval(t *testing.T) {
	if ReaperRequeueInterval != 60*time.Second {
		t.Errorf("requeue interval should be 60s, got %v", ReaperRequeueInterval)
	}
}

// --- Test helpers ---

func newTestApp(name string, labels map[string]string) loopzerov1.LoopZeroApplication {
	return loopzerov1.LoopZeroApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "loopzero-acme-production",
			Labels:    labels,
		},
	}
}
