// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"testing"
)

func TestParseArgoAppName_SingleHyphen(t *testing.T) {
	tenant, env, ok := ParseArgoAppName("a-b")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tenant != "a" {
		t.Errorf("tenant = %q, want a", tenant)
	}
	if env != "b" {
		t.Errorf("env = %q, want b", env)
	}
}

func TestParseArgoAppName_ThreeSegments(t *testing.T) {
	tenant, env, ok := ParseArgoAppName("tn-acme-production")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tenant != "tn-acme" {
		t.Errorf("tenant = %q, want tn-acme", tenant)
	}
	if env != "production" {
		t.Errorf("env = %q, want production", env)
	}
}

func TestParseArgoAppName_FourSegments(t *testing.T) {
	tenant, env, ok := ParseArgoAppName("tn-loopzero-e2e-staging")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tenant != "tn-loopzero-e2e" {
		t.Errorf("tenant = %q, want tn-loopzero-e2e", tenant)
	}
	if env != "staging" {
		t.Errorf("env = %q, want staging", env)
	}
}

func TestParseArgoAppName_JustHyphen(t *testing.T) {
	_, _, ok := ParseArgoAppName("-")
	if ok {
		t.Error("expected false for just a hyphen")
	}
}
