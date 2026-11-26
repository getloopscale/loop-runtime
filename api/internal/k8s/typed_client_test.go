// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// TestNewScheme_registersLoopZeroTypes proves the typed client's scheme recognizes
// the loopzero.io/v1 CRD kinds (M8 T2.1) — the precondition for typed CR access.
func TestNewScheme_registersLoopZeroTypes(t *testing.T) {
	s, err := NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	cases := map[string]runtime.Object{
		"Tenant":              &apiv1.Tenant{},
		"LoopZeroApplication": &apiv1.LoopZeroApplication{},
		"PreviewEnvironment":  &apiv1.PreviewEnvironment{},
	}
	for want, obj := range cases {
		gvks, _, err := s.ObjectKinds(obj)
		if err != nil {
			t.Errorf("%s not registered: %v", want, err)
			continue
		}
		if len(gvks) == 0 || gvks[0].Kind != want || gvks[0].Group != "loopzero.io" || gvks[0].Version != "v1" {
			t.Errorf("%s resolved to wrong GVK: %v", want, gvks)
		}
	}
}
