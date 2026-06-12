// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeVaultTransit captures every call so we can assert delegation.
type fakeVaultTransit struct {
	mu           sync.Mutex
	encryptCalls []string
	decryptCalls []string
	plaintext    []byte
	ciphertext   string
	encryptErr   error
	decryptErr   error
}

func (f *fakeVaultTransit) Encrypt(_ context.Context, tenantID string, plaintext []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.encryptCalls = append(f.encryptCalls, tenantID+":"+string(plaintext))
	return f.ciphertext, f.encryptErr
}

func (f *fakeVaultTransit) Decrypt(_ context.Context, tenantID, ciphertext string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decryptCalls = append(f.decryptCalls, tenantID+":"+ciphertext)
	return f.plaintext, f.decryptErr
}

// TestVaultTransitAdapter_DelegatesEncrypt — adapter must forward to
// Client.Encrypt and return the result unchanged.
func TestVaultTransitAdapter_DelegatesEncrypt(t *testing.T) {
	fake := &fakeVaultTransit{ciphertext: "vault:v1:sentinel"}
	a := NewVaultTransitAdapter(fake)
	if a == nil {
		t.Fatalf("NewVaultTransitAdapter returned nil for non-nil client")
	}

	got, err := a.EncryptForTenant(context.Background(), "tn-A", []byte("plaintext"))
	if err != nil {
		t.Fatalf("EncryptForTenant: %v", err)
	}
	if got != "vault:v1:sentinel" {
		t.Errorf("ciphertext: got %q want %q", got, "vault:v1:sentinel")
	}
	if len(fake.encryptCalls) != 1 || fake.encryptCalls[0] != "tn-A:plaintext" {
		t.Errorf("delegation: got %v want [tn-A:plaintext]", fake.encryptCalls)
	}
}

// TestVaultTransitAdapter_DelegatesDecrypt — adapter must forward to
// Client.Decrypt and return the result unchanged.
func TestVaultTransitAdapter_DelegatesDecrypt(t *testing.T) {
	fake := &fakeVaultTransit{plaintext: []byte("postgres://x")}
	a := NewVaultTransitAdapter(fake)

	got, err := a.DecryptForTenant(context.Background(), "tn-A", "vault:v1:abc")
	if err != nil {
		t.Fatalf("DecryptForTenant: %v", err)
	}
	if string(got) != "postgres://x" {
		t.Errorf("plaintext: got %q want %q", string(got), "postgres://x")
	}
	if len(fake.decryptCalls) != 1 || fake.decryptCalls[0] != "tn-A:vault:v1:abc" {
		t.Errorf("delegation: got %v want [tn-A:vault:v1:abc]", fake.decryptCalls)
	}
}

// TestVaultTransitAdapter_PropagatesEncryptError — errors from wrapped
// client surface unchanged. errors.Is must continue to work.
func TestVaultTransitAdapter_PropagatesEncryptError(t *testing.T) {
	wantErr := errors.New("upstream encrypt failure")
	fake := &fakeVaultTransit{encryptErr: wantErr}
	a := NewVaultTransitAdapter(fake)

	_, err := a.EncryptForTenant(context.Background(), "tn-A", []byte("v"))
	if !errors.Is(err, wantErr) {
		t.Errorf("want errors.Is(wantErr); got %v", err)
	}
}

// TestVaultTransitAdapter_NilClientReturnsNil — constructor refuses nil
// client by returning nil. Caller (cmd/setup_server.go) is responsible
// for the fail-loud refusal per D6 ADR.
func TestVaultTransitAdapter_NilClientReturnsNil(t *testing.T) {
	a := NewVaultTransitAdapter(nil)
	if a != nil {
		t.Errorf("NewVaultTransitAdapter(nil) want nil, got %+v", a)
	}
}
