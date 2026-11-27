// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"bytes"
	"errors"
	"testing"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

// A caller capturing `$(loop-runtime-ops dsn password)` whose pipe is gone never got
// the value; exiting 0 would tell it the opposite.
func TestEmitReportsAResultItCouldNotWrite(t *testing.T) {
	if code := emit(brokenWriter{}, &bytes.Buffer{}, 0, "pw\n", ""); code != 5 {
		t.Errorf("exit %d, want 5 when stdout cannot be written", code)
	}
}

func TestEmitReportsAMessageItCouldNotWrite(t *testing.T) {
	if code := emit(&bytes.Buffer{}, brokenWriter{}, 1, "", "bad field\n"); code != 5 {
		t.Errorf("exit %d, want 5 when stderr cannot be written", code)
	}
}

func TestEmitPassesTheCodeThroughWhenBothWritesSucceed(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := emit(&out, &errOut, 1, "x", "y"); code != 1 || out.String() != "x" || errOut.String() != "y" {
		t.Errorf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
