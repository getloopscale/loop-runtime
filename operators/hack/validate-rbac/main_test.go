// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"go/token"
	"strings"
	"testing"
)

// Test_parseRBACMarker_ValidMarker — happy path: extract groups, resources, verbs.
func Test_parseRBACMarker_ValidMarker(t *testing.T) {
	rule, err := parseRBACMarker(
		"+kubebuilder:rbac:groups=runtime.getloopscale.com,resources=tenants,verbs=get;list;watch",
		token.Position{Filename: "test.go", Line: 1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(rule.verbs) == 0 || !strings.Contains(rule.verbs[0], "get") {
		t.Errorf("verbs not parsed: %v", rule.verbs)
	}
	if len(rule.resources) == 0 || !strings.Contains(rule.resources[0], "tenants") {
		t.Errorf("resources not parsed: %v", rule.resources)
	}
}

// Test_parseRBACMarker_MissingVerbs — per K8s convention, verbs= is mandatory.
func Test_parseRBACMarker_MissingVerbs(t *testing.T) {
	_, err := parseRBACMarker(
		"+kubebuilder:rbac:groups=runtime.getloopscale.com,resources=tenants",
		token.Position{Filename: "test.go", Line: 1})
	if err == nil {
		t.Errorf("expected error for missing verbs=, got nil")
	}
}

// Test_parseRBACMarker_WildcardResources — wildcard ban detection (analyzer
// flags this in analyzeFile; here we verify the parser surfaces the value
// so the caller can detect).
func Test_parseRBACMarker_WildcardResources(t *testing.T) {
	rule, err := parseRBACMarker(
		"+kubebuilder:rbac:groups=core,resources=*,verbs=get",
		token.Position{Filename: "test.go", Line: 1})
	if err != nil {
		t.Fatalf("parser should not reject wildcards (analyzer enforces): %v", err)
	}
	if rule.resources[0] != "*" {
		t.Errorf("resources[0] = %q, want '*'", rule.resources[0])
	}
}

// Test_verbMatch_CaseInsensitive — call verb is Go-cased ("Get"),
// marker verb is lowercase ("get"). Helper normalizes.
func Test_verbMatch_CaseInsensitive(t *testing.T) {
	if !verbMatch("Get", []string{"get;list;watch"}) {
		t.Errorf("verbMatch(Get, [get;list;watch]) = false, want true")
	}
}

// Test_verbMatch_Mismatch — Delete call vs only get/list markers.
func Test_verbMatch_Mismatch(t *testing.T) {
	if verbMatch("Delete", []string{"get;list"}) {
		t.Errorf("verbMatch(Delete, [get;list]) = true, want false")
	}
}

// Test_errExit_AccumulatesViolations — collector preserves order and count.
func Test_errExit_AccumulatesViolations(t *testing.T) {
	e := &errExit{}
	e.add("first")
	e.add("second %d", 2)
	if len(e.violations) != 2 {
		t.Fatalf("violations count = %d, want 2", len(e.violations))
	}
	if !strings.Contains(e.violations[1], "second 2") {
		t.Errorf("formatted message wrong: %q", e.violations[1])
	}
}
