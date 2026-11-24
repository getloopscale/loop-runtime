// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// docker push sends each image layer in one request, and the emulator refuses a body over
// 16 MiB unless EMULATOR_MAX_BODY_BYTES raises it: a tenant image with a larger layer would
// never reach staging's ECR.
func TestTheStagingEmulatorAcceptsImageLayersUpToOneGibibyte(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "staging", "systemd", "aws-emulator.service"))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "Environment=EMULATOR_MAX_BODY_BYTES="
	for _, line := range strings.Split(string(data), "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if !found {
			continue
		}
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatalf("EMULATOR_MAX_BODY_BYTES=%q is not a byte count: %v", value, err)
		}
		if limit < 1<<30 {
			t.Errorf("EMULATOR_MAX_BODY_BYTES=%d, want at least 1 GiB (%d)", limit, int64(1<<30))
		}
		return
	}
	t.Errorf("aws-emulator.service sets no %s, so image layers over 16 MiB are refused", prefix)
}

// The emulated ECR holds at most 512 MiB unless EMULATOR_ECR_MAX_STORED_BYTES raises it,
// and the first tenant build on staging (a full Node.js stage plus its registry cache)
// was refused with 413 Payload Too Large past that. The registry lives in the
// emulator's memory, so the ceiling is sized to the host.
func TestTheStagingEmulatorHoldsTenantImagesUpToEightGibibytes(t *testing.T) {
	limit := emulatorEnvBytes(t, "EMULATOR_ECR_MAX_STORED_BYTES")
	if limit < 8<<30 {
		t.Errorf("EMULATOR_ECR_MAX_STORED_BYTES=%d, want at least 8 GiB (%d)", limit, int64(8<<30))
	}
}

// emulatorEnvBytes reads a byte count the staging emulator unit sets in its environment.
func emulatorEnvBytes(t *testing.T, name string) int64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "staging", "systemd", "aws-emulator.service"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "Environment=" + name + "="
	for _, line := range strings.Split(string(data), "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if !found {
			continue
		}
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatalf("%s=%q is not a byte count: %v", name, value, err)
		}
		return limit
	}
	t.Fatalf("aws-emulator.service sets no %s", name)
	return 0
}
