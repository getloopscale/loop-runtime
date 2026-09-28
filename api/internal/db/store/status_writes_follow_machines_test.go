// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"slices"
	"testing"
)

func TestProjectDeleteCancelsOnlyTheStatusesTheReleaseMachineAllows(t *testing.T) {
	present := []string{"ACTIVE", "ROLLED_BACK", "ARCHIVED", "CANCELLED", "SUPERSEDED", "PENDING"}

	got := statusesCancelledOnProjectDelete(present)

	want := []string{"ACTIVE", "ROLLED_BACK", "SUPERSEDED", "PENDING"}
	if !slices.Equal(got, want) {
		t.Fatalf("statusesCancelledOnProjectDelete(%v) = %v, want %v", present, got, want)
	}
}

func TestProjectDeleteNeverCancelsAStatusTheMachineDoesNotKnow(t *testing.T) {
	if got := statusesCancelledOnProjectDelete([]string{"NOT_A_STATUS"}); len(got) != 0 {
		t.Fatalf("an unknown status must be left alone, got %v", got)
	}
}

func TestTheProvisionerClaimMovesAVerifiedDomainToProvisioning(t *testing.T) {
	if got := verifiedClaimTarget(); got != "provisioning" {
		t.Fatalf("verifiedClaimTarget() = %q, want provisioning (the Domain machine's DomainProvision edge)", got)
	}
}
