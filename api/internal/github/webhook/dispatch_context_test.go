// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package webhook

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A push handler runs after GitHub already has its 200, so its context must
// come from the server's lifetime (cancelled on shutdown) and carry a
// deadline — never context.Background().
func TestPushDispatchRunsUnderABoundedContextFromTheServerLifetime(t *testing.T) {
	// Arrange
	lifetime, stop := context.WithCancel(context.Background())
	got := make(chan context.Context, 1)
	h := NewHandler(lifetime, testSecret, slog.Default()).OnPush(func(ctx context.Context, _ PushEvent) error {
		got <- ctx
		return nil
	})
	body := `{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"acme/app","default_branch":"main"}}`
	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sign(body, testSecret))
	req.Header.Set(eventHeader, "push")

	// Act
	h.ServeHTTP(httptest.NewRecorder(), req)
	var ctx context.Context
	select {
	case ctx = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("push handler was never called")
	}

	// Assert
	if _, ok := ctx.Deadline(); !ok {
		t.Error("dispatch context has no deadline")
	}
	stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("dispatch context was not cancelled when the server lifetime ended")
	}
}
