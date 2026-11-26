// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

func TestAnAbsentEnvironmentReadsAsAbsentToTheEnsurer(t *testing.T) {
	envs := []ports.EnvironmentEntry{{Name: "staging", Project: "web"}}

	if got := environmentDetail(envs, "api", "staging"); got != nil {
		t.Fatalf("environmentDetail for a missing tuple = %#v, want an untyped nil", got)
	}
}

func TestAPresentEnvironmentReadsAsPresentToTheEnsurer(t *testing.T) {
	envs := []ports.EnvironmentEntry{{Name: "staging", Project: "api"}}

	if environmentDetail(envs, "api", "staging") == nil {
		t.Fatal("environmentDetail for an existing tuple must be non-nil")
	}
}
