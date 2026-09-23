// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeCreds(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "dev-credentials.json")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatalf("write temp creds: %v", err)
	}
	// os.WriteFile is subject to umask; force the exact mode.
	if err := os.Chmod(p, mode); err != nil {
		t.Fatalf("chmod temp creds: %v", err)
	}
	return p
}

func TestLoadCreds_AbsentIsSkipError(t *testing.T) {
	_, err := LoadCreds(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, ErrCredsAbsent) {
		t.Fatalf("absent file: want ErrCredsAbsent, got %v", err)
	}
}

func TestLoadCreds_WrongModeIsFail(t *testing.T) {
	p := writeCreds(t, `{"github":{"app_id":"x"}}`, 0o644)
	_, err := LoadCreds(p)
	if !errors.Is(err, ErrCredsBadMode) {
		t.Fatalf("0644 file: want ErrCredsBadMode, got %v", err)
	}
}

func TestLoadCreds_MalformedJSONIsFail(t *testing.T) {
	// EC-4: present + 0600 + corrupt = FAIL (typed), never SKIP.
	p := writeCreds(t, `{"github": {truncated`, 0o600)
	_, err := LoadCreds(p)
	if !errors.Is(err, ErrCredsMalformed) {
		t.Fatalf("malformed 0600 file: want ErrCredsMalformed, got %v", err)
	}
}

func TestLoadCreds_ValidRoundTrips(t *testing.T) {
	p := writeCreds(t, `{"github":{"app_id":"123","webhook_secret":"whsec_test_real"},"stripe":{"api_key":"rk_test_x","price_pro":"price_abc"}}`, 0o600)
	c, err := LoadCreds(p)
	if err != nil {
		t.Fatalf("valid 0600 file: unexpected err %v", err)
	}
	if c.GitHub.AppID != "123" {
		t.Errorf("GitHub.AppID = %q, want 123", c.GitHub.AppID)
	}
	if c.Stripe.PricePro != "price_abc" {
		t.Errorf("Stripe.PricePro = %q, want price_abc", c.Stripe.PricePro)
	}
}
