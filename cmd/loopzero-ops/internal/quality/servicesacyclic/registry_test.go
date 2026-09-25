// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package servicesacyclic

import (
	"errors"
	"strings"
	"testing"
)

// The allow-list is the gate's own claim about the architecture. A gate named
// "acyclic" whose allow-list permits a cycle would pass every tree that
// realises that cycle, so the registry is checked before any file is scanned.

func TestRegistryIsAcyclic_FrozenRegistryPasses(t *testing.T) {
	if err := checkRegistryAcyclic(allowed); err != nil {
		t.Fatalf("the frozen registry must be acyclic, got %v", err)
	}
}

func TestRegistryIsAcyclic_TwoNodeCycleIsRejected(t *testing.T) {
	registry := map[string]map[string]bool{
		"build":   {"release": true},
		"release": {"build": true},
	}
	err := checkRegistryAcyclic(registry)
	if !errors.Is(err, ErrRegistryCycle) {
		t.Fatalf("want ErrRegistryCycle, got %v", err)
	}
	if !strings.Contains(err.Error(), "build") || !strings.Contains(err.Error(), "release") {
		t.Errorf("the error must name the contexts on the cycle, got %q", err)
	}
}

func TestRegistryIsAcyclic_SelfEdgeIsRejected(t *testing.T) {
	registry := map[string]map[string]bool{"build": {"build": true}}
	if err := checkRegistryAcyclic(registry); !errors.Is(err, ErrRegistryCycle) {
		t.Fatalf("a context allowed to import itself is a cycle; want ErrRegistryCycle, got %v", err)
	}
}

func TestRegistryIsAcyclic_EdgeToUnregisteredContextIsRejected(t *testing.T) {
	registry := map[string]map[string]bool{"build": {"ghost": true}}
	if err := checkRegistryAcyclic(registry); !errors.Is(err, ErrRegistryUnknownTarget) {
		t.Fatalf("an allowed edge to a context the registry does not declare hides a node from the cycle check; want ErrRegistryUnknownTarget, got %v", err)
	}
}

func TestCheck_RefusesToRunOnACyclicRegistry(t *testing.T) {
	saved := allowed
	t.Cleanup(func() { allowed = saved })
	allowed = map[string]map[string]bool{
		"build":   {"release": true},
		"release": {"build": true},
	}
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/release/r.go", "package release\n\nvar X = 1\n")
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrRegistryCycle) {
		t.Fatalf("Check must refuse a cyclic registry before scanning; got %v", err)
	}
}
