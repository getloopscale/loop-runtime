// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"strings"
	"testing"
)

// The secret-fetch broker serves every cell (#456). Each cell's External Secrets presents
// its own bearer, and the cell a request comes from is the cell whose token it presented —
// never a header the caller chooses. LOOPZERO_BROKER_TOKENS lists them as cell=token pairs;
// LOOPZERO_BROKER_TOKEN, the single-cell form, stays the default cell's token.

func TestBrokerCellTokens_ReadsOneTokenPerCell(t *testing.T) {
	t.Setenv("LOOPZERO_BROKER_TOKEN", "")
	t.Setenv("LOOPZERO_BROKER_TOKENS", "cell-0=tok-a, cell-1=tok-b")

	got, err := Load().BrokerCellTokens()

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["cell-0"] != "tok-a" || got["cell-1"] != "tok-b" {
		t.Errorf("tokens = %v, want cell-0=tok-a and cell-1=tok-b", got)
	}
}

func TestBrokerCellTokens_TheSingleTokenIsTheDefaultCells(t *testing.T) {
	t.Setenv("LOOPZERO_BROKER_TOKEN", "legacy")
	t.Setenv("LOOPZERO_BROKER_TOKENS", "")

	got, err := Load().BrokerCellTokens()

	if err != nil || len(got) != 1 || got["cell-0"] != "legacy" {
		t.Errorf("tokens = %v, err = %v; want only cell-0=legacy", got, err)
	}
}

func TestBrokerCellTokens_NoTokenDisablesTheBroker(t *testing.T) {
	t.Setenv("LOOPZERO_BROKER_TOKEN", "")
	t.Setenv("LOOPZERO_BROKER_TOKENS", "")

	got, err := Load().BrokerCellTokens()

	if err != nil || len(got) != 0 {
		t.Errorf("tokens = %v, err = %v; want none", got, err)
	}
}

// Two cells sharing a token could not be told apart, so a request would bind to either.
func TestValidate_RefusesTwoCellsWithOneToken(t *testing.T) {
	t.Setenv("LOOPZERO_BROKER_TOKEN", "")
	t.Setenv("LOOPZERO_BROKER_TOKENS", "cell-0=tok-shared,cell-1=tok-shared")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_BROKER_TOKENS") {
		t.Fatalf("Validate = %v, want a LOOPZERO_BROKER_TOKENS error", err)
	}
	if strings.Contains(err.Error(), "tok-shared") {
		t.Errorf("the error leaks the token: %v", err)
	}
}

func TestValidate_RefusesAMalformedBrokerTokenEntry(t *testing.T) {
	for _, raw := range []string{"cell-0", "=tok", "cell-0=", "cell-0=a,cell-0=b"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("LOOPZERO_BROKER_TOKEN", "")
			t.Setenv("LOOPZERO_BROKER_TOKENS", raw)
			if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "LOOPZERO_BROKER_TOKENS") {
				t.Errorf("Validate(%q) = %v, want a LOOPZERO_BROKER_TOKENS error", raw, err)
			}
		})
	}
}

// The default cell cannot be given two tokens by setting both forms.
func TestValidate_RefusesTheDefaultCellInBothForms(t *testing.T) {
	t.Setenv("LOOPZERO_BROKER_TOKEN", "legacy")
	t.Setenv("LOOPZERO_BROKER_TOKENS", "cell-0=other,cell-1=b")

	if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "LOOPZERO_BROKER_TOKEN") {
		t.Errorf("Validate = %v, want an error naming both variables", err)
	}
}
