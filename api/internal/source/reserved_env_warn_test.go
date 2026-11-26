// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// In warn mode a reserved key is accepted, so the acceptance must be seen:
// a log line per violation and a counter operators can watch before
// flipping to enforce.
func TestWarnModeUploadWithAReservedEnvKeyIsLoggedAndCounted(t *testing.T) {
	// Arrange
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	before := testutil.ToFloat64(metrics.ServicesReservedEnvKeyAccepted)
	manifest := `{"version":2,"project":"shop","services":[{"name":"api","runtime":"node","port":3000,"start":"node server.js","proxy":"/api","dev":"node server.js","healthcheck":"/health","cors":false,"passSetCookie":false,"env":{"LD_PRELOAD":"/x.so"}}]}`

	// Act
	_, err := ValidateServicesJSONNative(manifest, EnvBlocklistWarn)

	// Assert
	if err != nil {
		t.Fatalf("warn mode rejected the upload: %v", err)
	}
	if !strings.Contains(logs.String(), "LD_PRELOAD") {
		t.Errorf("no warning logged for the reserved key; logs: %q", logs.String())
	}
	if got := testutil.ToFloat64(metrics.ServicesReservedEnvKeyAccepted) - before; got != 1 {
		t.Errorf("accepted-violation counter moved by %v, want 1", got)
	}
}
