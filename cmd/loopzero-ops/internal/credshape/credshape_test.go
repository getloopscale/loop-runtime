// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package credshape

import (
	"errors"
	"strings"
	"testing"
)

func TestIsPlaceholder_TreatsBlankAsPlaceholder(t *testing.T) {
	for _, v := range []string{"", "   ", "\n"} {
		if !IsPlaceholder(v) {
			t.Errorf("IsPlaceholder(%q) = false, want true", v)
		}
	}
}

func TestIsPlaceholder_RecognisesEveryMarkerTheRepositoryWrites(t *testing.T) {
	for _, v := range []string{
		"whsec_dev_abc", "REPLACE_VIA_PULUMI", "REPLACE_VIA_SPONSOR", "REPLACE_ME",
		"whsec_PLACEHOLDER", "changeme", "CHANGEME", "TODO_SET_ME", "sk_test_REPLACE_ME",
	} {
		if !IsPlaceholder(v) {
			t.Errorf("IsPlaceholder(%q) = false, want true", v)
		}
	}
}

func TestIsPlaceholder_AcceptsRealLookingValues(t *testing.T) {
	for _, v := range []string{"whsec_test_realvalue", "rk_test_realkey", "GOCSPX-real", "Ov23li3uhReal"} {
		if IsPlaceholder(v) {
			t.Errorf("IsPlaceholder(%q) = true, want false", v)
		}
	}
}

func TestPlaceholderMarker_NamesTheMostSpecificMarker(t *testing.T) {
	got, ok := PlaceholderMarker(`client_id: "REPLACE_VIA_PULUMI"`)
	if !ok || got != "REPLACE_VIA_PULUMI" {
		t.Fatalf("PlaceholderMarker = (%q, %v), want (REPLACE_VIA_PULUMI, true)", got, ok)
	}
}

func TestParseStripeSecretKey_AcceptsTheFourSecretPrefixes(t *testing.T) {
	for _, k := range []string{"rk_test_abc", "rk_live_abc", "sk_test_abc", "sk_live_abc"} {
		if err := ParseStripeSecretKey(k); err != nil {
			t.Errorf("ParseStripeSecretKey(%q) = %v, want nil", k, err)
		}
	}
}

func TestParseStripeSecretKey_ReportsAbsenceAsPlaceholder(t *testing.T) {
	for _, k := range []string{"", "  ", "sk_test_REPLACE_ME", "sk_test_PLACEHOLDER", "whsec_dev_x"} {
		if err := ParseStripeSecretKey(k); !errors.Is(err, ErrStripeKeyPlaceholder) {
			t.Errorf("ParseStripeSecretKey(%q) = %v, want ErrStripeKeyPlaceholder", k, err)
		}
	}
}

func TestParseStripeSecretKey_RejectsNonSecretShapes(t *testing.T) {
	for _, k := range []string{"pk_test_publishable", "rk_dev_x", "sk_org_test_x", "not-a-key"} {
		if err := ParseStripeSecretKey(k); !errors.Is(err, ErrStripeKeyWrongPrefix) {
			t.Errorf("ParseStripeSecretKey(%q) = %v, want ErrStripeKeyWrongPrefix", k, err)
		}
	}
}

func TestParseStripeSecretKey_WrongPrefixErrorDoesNotEchoTheKey(t *testing.T) {
	const k = "pk_test_publishable_value_that_is_long"
	err := ParseStripeSecretKey(k)
	if err == nil {
		t.Fatal("want error")
	}
	if got := err.Error(); strings.Contains(got, "publishable_value") {
		t.Errorf("error echoes the key body: %q", got)
	}
}
