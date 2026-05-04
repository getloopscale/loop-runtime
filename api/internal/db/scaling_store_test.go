// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"testing"
)

// Plan T2.1 unit tests for ScalingConfig validation + canonical hash.
// DB-level Postgres tests live in tests/integration (with real DB).

func TestScalingConfig_Validate_RejectsNegativeMin(t *testing.T) {
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: -1, MaxReplicas: 5,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for min_replicas=-1")
	}
}

func TestScalingConfig_Validate_RejectsMaxLessThanMin(t *testing.T) {
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: 5, MaxReplicas: 3,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for max < min")
	}
}

func TestScalingConfig_Validate_RejectsTargetCPUOutOfRange(t *testing.T) {
	tooHigh := int32(200)
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: 1, MaxReplicas: 5, TargetCPUPct: &tooHigh,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for target_cpu_pct=200")
	}

	tooLow := int32(0)
	c.TargetCPUPct = &tooLow
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for target_cpu_pct=0")
	}
}

func TestScalingConfig_Validate_RejectsScaleToZeroWithMinNonZero(t *testing.T) {
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: 1, MaxReplicas: 5, ScaleToZero: true,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error: scale_to_zero requires min_replicas=0")
	}
}

func TestScalingConfig_Validate_RejectsInvalidEnvironment(t *testing.T) {
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "bogus", AppName: "web",
		MinReplicas: 1, MaxReplicas: 5,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for environment=bogus")
	}
}

func TestScalingConfig_Validate_AcceptsFixedReplicas(t *testing.T) {
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: 3, MaxReplicas: 3,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid for fixed replicas: %v", err)
	}
}

func TestScalingConfig_Validate_AcceptsHPAConfig(t *testing.T) {
	cpu := int32(70)
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: 2, MaxReplicas: 10, TargetCPUPct: &cpu,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid for HPA config: %v", err)
	}
}

func TestScalingConfig_Validate_AcceptsScaleToZero(t *testing.T) {
	c := ScalingConfig{
		TenantID: "acme", ProjectID: "api", Environment: "production", AppName: "web",
		MinReplicas: 0, MaxReplicas: 5, ScaleToZero: true,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid for scale_to_zero with min=0: %v", err)
	}
}

func TestScalingConfig_CanonicalHash_StableAcrossCalls(t *testing.T) {
	cpu := int32(75)
	c := ScalingConfig{
		MinReplicas: 2, MaxReplicas: 10, TargetCPUPct: &cpu, ScaleToZero: false,
	}
	h1 := c.CanonicalHash()
	h2 := c.CanonicalHash()
	if h1 != h2 {
		t.Fatalf("hash not stable: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected sha256 hex (64 chars), got %d", len(h1))
	}
}

func TestScalingConfig_CanonicalHash_DiffersWhenConfigChanges(t *testing.T) {
	cpu1 := int32(70)
	cpu2 := int32(80)
	a := ScalingConfig{MinReplicas: 1, MaxReplicas: 5, TargetCPUPct: &cpu1}
	b := ScalingConfig{MinReplicas: 1, MaxReplicas: 5, TargetCPUPct: &cpu2}
	if a.CanonicalHash() == b.CanonicalHash() {
		t.Fatal("expected different hashes for cpu=70 vs cpu=80")
	}
}

func TestScalingConfig_CanonicalHash_NilCPUDoesNotCollide(t *testing.T) {
	cpu := int32(70)
	withCPU := ScalingConfig{MinReplicas: 1, MaxReplicas: 5, TargetCPUPct: &cpu}
	noCPU := ScalingConfig{MinReplicas: 1, MaxReplicas: 5, TargetCPUPct: nil}
	if withCPU.CanonicalHash() == noCPU.CanonicalHash() {
		t.Fatal("expected nil-CPU and 70-CPU to hash differently")
	}
}
