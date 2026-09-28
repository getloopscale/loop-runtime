// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"testing"
)

// A cell's tenant capacity has one definition: the capacity declared on the
// cell (cells.capacity_tenants_max) when there is one, the platform-wide
// MAX_TENANTS_PER_CELL otherwise. Automatic placement and the admin pin read
// the same column.

func TestAssignSkipsACellAtItsDeclaredCapacity(t *testing.T) {
	// Arrange: cell-0 is the least loaded, but an operator capped it at 3.
	q := &fakeQuerier{
		counts:     map[string]int{"cell-0": 3, "cell-1": 10},
		capacities: map[string]int{"cell-0": 3},
	}
	engine := NewRoundRobinEngine(newMultiCellRouter("cell-0", "cell-1"), q, 500)

	// Act
	got, err := engine.AssignCell(context.Background())

	// Assert
	if err != nil {
		t.Fatalf("AssignCell: %v", err)
	}
	if got != "cell-1" {
		t.Errorf("assigned %q; cell-0 is at its declared capacity of 3", got)
	}
}

func TestAssignUsesTheGlobalCapacityForACellWithoutADeclaration(t *testing.T) {
	q := &fakeQuerier{counts: map[string]int{"cell-0": 2}, capacities: map[string]int{}}
	engine := NewRoundRobinEngine(newMultiCellRouter("cell-0"), q, 2)

	_, err := engine.AssignCell(context.Background())

	if err == nil {
		t.Fatal("cell-0 holds 2 tenants against a global capacity of 2, yet a tenant was placed")
	}
}

func TestAssignHonoursADeclaredCapacityAboveTheGlobalOne(t *testing.T) {
	q := &fakeQuerier{counts: map[string]int{"cell-0": 2}, capacities: map[string]int{"cell-0": 5}}
	engine := NewRoundRobinEngine(newMultiCellRouter("cell-0"), q, 2)

	got, err := engine.AssignCell(context.Background())

	if err != nil || got != "cell-0" {
		t.Fatalf("AssignCell = (%q, %v); cell-0 declares room for 5", got, err)
	}
}
