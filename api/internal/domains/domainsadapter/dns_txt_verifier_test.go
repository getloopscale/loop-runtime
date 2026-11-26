// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domainsadapter

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeTXTResolver struct {
	records  []string
	err      error
	lastName string
}

func (r *fakeTXTResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	r.lastName = name
	return r.records, r.err
}

func TestDNSTXTVerifier_FindsThePublishedToken(t *testing.T) {
	r := &fakeTXTResolver{records: []string{"other", "tok-123"}}
	ok, err := (&DNSTXTVerifier{Resolver: r}).VerifyTXT(context.Background(), "api.acme.com", "tok-123")
	if err != nil || !ok {
		t.Fatalf("got (%v, %v), want (true, nil)", ok, err)
	}
	if r.lastName != "_loopruntime-verify.api.acme.com" {
		t.Errorf("looked up %q, want _loopruntime-verify.api.acme.com", r.lastName)
	}
}

func TestDNSTXTVerifier_AbsentRecordIsNotVerifiedWithoutError(t *testing.T) {
	r := &fakeTXTResolver{err: &net.DNSError{Err: "no such host", IsNotFound: true}}
	ok, err := (&DNSTXTVerifier{Resolver: r}).VerifyTXT(context.Background(), "api.acme.com", "tok")
	if err != nil || ok {
		t.Fatalf("got (%v, %v), want (false, nil)", ok, err)
	}
}

func TestDNSTXTVerifier_LookupFailureIsAnError(t *testing.T) {
	r := &fakeTXTResolver{err: &net.DNSError{Err: "timeout", IsTimeout: true}}
	if _, err := (&DNSTXTVerifier{Resolver: r}).VerifyTXT(context.Background(), "api.acme.com", "tok"); err == nil {
		t.Fatal("a DNS timeout must be reported, not read as an absent record")
	}
}

func TestDNSTXTVerifier_WrongTokenIsNotVerified(t *testing.T) {
	r := &fakeTXTResolver{records: []string{"someone-else"}}
	ok, err := (&DNSTXTVerifier{Resolver: r}).VerifyTXT(context.Background(), "api.acme.com", "tok")
	if err != nil || ok {
		t.Fatalf("got (%v, %v), want (false, nil)", ok, err)
	}
}

func TestIsDNSNotFound_PlainNoSuchHostError(t *testing.T) {
	if !isDNSNotFound(errors.New("lookup failed: no such host")) {
		t.Error("a 'no such host' error means the record is absent")
	}
	if isDNSNotFound(errors.New("connection refused")) {
		t.Error("an unrelated error is not an absent record")
	}
}
