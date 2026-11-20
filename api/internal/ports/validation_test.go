// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ports

import (
	"testing"
)

func TestValidator_ValidDomains(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	valid := []string{
		"api.mycompany.com",
		"staging.mycompany.com",
		"my-app.example.io",
		"sub.domain.example.co.uk",
		"a.io",
		"my-really-long-subdomain.with-many-parts.mycompany.com",
	}

	for _, domain := range valid {
		if err := v.Validate(domain); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", domain, err)
		}
	}
}

func TestValidator_InvalidFormat(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	cases := []struct {
		domain string
		desc   string
	}{
		{"", "empty"},
		{"  ", "whitespace only"},
		{"https://api.example.com", "protocol prefix"},
		{"*.example.com", "wildcard"},
		{"api.example.com:8080", "port number"},
		{"api.example.com.", "trailing dot"},
		{"-invalid.com", "leading hyphen"},
		{"not a domain", "spaces"},
		{"justaword", "single label"},
	}

	for _, tc := range cases {
		if err := v.Validate(tc.domain); err == nil {
			t.Errorf("expected %q (%s) to be invalid, got nil error", tc.domain, tc.desc)
		}
	}
}

func TestValidator_MaxLength(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	// 254 chars should fail
	long := ""
	for i := 0; i < 25; i++ {
		long += "abcdefghij."
	}
	long += "com"
	if len(long) <= 253 {
		t.Skip("test string too short")
	}
	if err := v.Validate(long); err == nil {
		t.Errorf("expected domain over 253 chars to fail")
	}
}

func TestValidator_ReservedDomains(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	reserved := []string{
		"localhost",
		"app.localhost",
		"api.example.com",
		"sub.example.org",
		"anything.local",
		"something.internal",
		"app.test",
	}

	for _, domain := range reserved {
		if err := v.Validate(domain); err == nil {
			t.Errorf("expected reserved domain %q to be rejected", domain)
		}
	}
}

func TestValidator_PlatformDomainProtection(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	blocked := []string{
		// BaseDomain and subdomains
		"dev.getloopscale.com",
		"anything.dev.getloopscale.com",
		"deep.sub.dev.getloopscale.com",
		// SEC-001 fix: parent zone apex and ALL subdomains
		"getloopscale.com",         // root zone apex
		"www.getloopscale.com",     // unlisted subdomain
		"staging.getloopscale.com", // unlisted subdomain
		"app.getloopscale.com",     // unlisted subdomain
		"docs.getloopscale.com",    // unlisted subdomain
		// Named infra subdomains (still blocked)
		"api.getloopscale.com",
		"argocd.getloopscale.com",
		"git.getloopscale.com",
		"cname.getloopscale.com",
	}

	for _, domain := range blocked {
		if err := v.Validate(domain); err == nil {
			t.Errorf("expected platform domain %q to be rejected", domain)
		}
	}
}

func TestValidator_AllowedSimilarDomains(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	// These look similar but should be allowed
	allowed := []string{
		"loopzero.com",        // different TLD
		"notgetloopscale.com", // different domain
		"mygetloopscale.com",  // different domain
		"api.mycompany.dev",   // different domain, same TLD
	}

	for _, domain := range allowed {
		if err := v.Validate(domain); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", domain, err)
		}
	}
}

func TestValidator_BlockedSuffixes(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", []string{"blocked.example.net"})

	if err := v.Validate("api.blocked.example.net"); err == nil {
		t.Error("expected blocked suffix domain to be rejected")
	}

	if err := v.Validate("api.allowed.mycompany.net"); err != nil {
		t.Errorf("expected non-blocked domain to be allowed, got: %v", err)
	}
}

func TestValidator_PunycodeRejected(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	punycode := []string{
		"xn--pi-8cd.mycompany.com", // Cyrillic "а" homograph for "api"
		"xn--nxasmq6b.example.com", // Greek domain
		"sub.xn--test-cua.com",     // punycode in subdomain
	}

	for _, domain := range punycode {
		if err := v.Validate(domain); err == nil {
			t.Errorf("expected punycode domain %q to be rejected", domain)
		}
	}
}

func TestValidator_CaseInsensitive(t *testing.T) {
	v := NewValidator("dev.getloopscale.com", nil)

	// Should normalize and validate
	if err := v.Validate("API.MyCompany.Com"); err != nil {
		t.Errorf("expected case-insensitive domain to be valid, got: %v", err)
	}

	// Platform domain should be caught case-insensitively
	if err := v.Validate("ANYTHING.DEV.GETLOOPSCALE.COM"); err == nil {
		t.Error("expected case-insensitive platform domain to be rejected")
	}
}
