// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ports

import (
	"errors"
	"net"
	"testing"
)

// =============================================================================
// isDNSNotFound tests (previously 0% coverage)
// =============================================================================

func TestIsDNSNotFound_NilError(t *testing.T) {
	if isDNSNotFound(nil) {
		t.Error("nil error should not be DNS not found")
	}
}

func TestIsDNSNotFound_DNSErrorNotFound(t *testing.T) {
	err := &net.DNSError{
		Err:        "no such host",
		Name:       "_loopzero-verify.example.com",
		IsNotFound: true,
	}
	if !isDNSNotFound(err) {
		t.Error("DNSError with IsNotFound=true should return true")
	}
}

func TestIsDNSNotFound_DNSErrorOther(t *testing.T) {
	err := &net.DNSError{
		Err:        "timeout",
		Name:       "_loopzero-verify.example.com",
		IsNotFound: false,
	}
	if isDNSNotFound(err) {
		t.Error("DNSError with IsNotFound=false should return false")
	}
}

func TestIsDNSNotFound_NonDNSError_ContainsNoSuchHost(t *testing.T) {
	err := errors.New("lookup failed: no such host")
	if !isDNSNotFound(err) {
		t.Error("error containing 'no such host' should return true")
	}
}

func TestIsDNSNotFound_NonDNSError_Other(t *testing.T) {
	err := errors.New("connection refused")
	if isDNSNotFound(err) {
		t.Error("generic error should return false")
	}
}

// =============================================================================
// DNSVerifier struct existence test
// =============================================================================

func TestDNSVerifier_Exists(t *testing.T) {
	v := &DNSVerifier{}
	_ = v // struct is non-nil by construction
}
