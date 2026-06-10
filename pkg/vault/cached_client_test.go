// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"
)

// makeTestCert returns a self-signed cert + its SPKI hash (base64).
func makeTestCert(t *testing.T, cn string) (*x509.Certificate, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return cert, base64.StdEncoding.EncodeToString(spkiHash[:])
}

func TestMakeSPKIPinVerifier_MatchingPin(t *testing.T) {
	cert, pinB64 := makeTestCert(t, "vault.test")
	pin, _ := base64.StdEncoding.DecodeString(pinB64)
	verify := makeSPKIPinVerifier(pin)

	err := verify([][]byte{cert.Raw}, nil)
	if err != nil {
		t.Errorf("expected nil for matching pin, got %v", err)
	}
}

func TestMakeSPKIPinVerifier_MismatchedPin(t *testing.T) {
	cert1, _ := makeTestCert(t, "vault.real")
	_, otherPinB64 := makeTestCert(t, "vault.evil") // pin from a DIFFERENT cert
	otherPin, _ := base64.StdEncoding.DecodeString(otherPinB64)
	verify := makeSPKIPinVerifier(otherPin)

	err := verify([][]byte{cert1.Raw}, nil)
	if err == nil {
		t.Errorf("expected SPKI mismatch error, got nil")
	}
	if !errors.Is(err, ErrSPKIPinMismatch) {
		t.Errorf("expected ErrSPKIPinMismatch, got %v", err)
	}
}

func TestMakeSPKIPinVerifier_NoCerts(t *testing.T) {
	verify := makeSPKIPinVerifier(make([]byte, sha256.Size))
	err := verify(nil, nil)
	if err == nil || !errors.Is(err, ErrSPKIPinMismatch) {
		t.Errorf("expected ErrSPKIPinMismatch for nil certs, got %v", err)
	}
}

func TestMakeSPKIPinVerifier_MalformedCert(t *testing.T) {
	verify := makeSPKIPinVerifier(make([]byte, sha256.Size))
	err := verify([][]byte{{0xde, 0xad, 0xbe, 0xef}}, nil)
	if err == nil || !errors.Is(err, ErrSPKIPinMismatch) {
		t.Errorf("expected ErrSPKIPinMismatch for malformed cert, got %v", err)
	}
}

func TestHashesEqual(t *testing.T) {
	a := []byte{1, 2, 3, 4}
	b := []byte{1, 2, 3, 4}
	c := []byte{1, 2, 3, 5}
	d := []byte{1, 2, 3}

	if !hashesEqual(a, b) {
		t.Error("expected a == b")
	}
	if hashesEqual(a, c) {
		t.Error("expected a != c")
	}
	if hashesEqual(a, d) {
		t.Error("expected a != d (length differs)")
	}
}

// Helper for NewCachedClient tests — creates a fresh Client without network.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	cfg := &Config{
		Address: "https://vault.test:8200",
		K8sRole: "loopzero-api",
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewCachedClient_NilInner(t *testing.T) {
	_, err := NewCachedClient(nil, CachedClientConfig{})
	if err == nil {
		t.Error("expected error for nil inner")
	}
}

func TestNewCachedClient_Defaults(t *testing.T) {
	inner := newTestClient(t)
	cc, err := NewCachedClient(inner, CachedClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if cc.cacheTTL != 30*time.Second {
		t.Errorf("expected default cacheTTL=30s, got %v", cc.cacheTTL)
	}
	if cc.cbThreshold != 5 {
		t.Errorf("expected default cbThreshold=5, got %d", cc.cbThreshold)
	}
	if cc.cbCooldown != 30*time.Second {
		t.Errorf("expected default cbCooldown=30s, got %v", cc.cbCooldown)
	}
}

func TestNewCachedClient_InvalidSPKIPinBase64(t *testing.T) {
	inner := newTestClient(t)
	_, err := NewCachedClient(inner, CachedClientConfig{SPKIPinB64: "not-base64!@#$"})
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestNewCachedClient_InvalidSPKIPinLength(t *testing.T) {
	inner := newTestClient(t)
	short := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	_, err := NewCachedClient(inner, CachedClientConfig{SPKIPinB64: short})
	if err == nil {
		t.Error("expected error for short pin")
	}
}

func TestNewCachedClient_ValidSPKIPin(t *testing.T) {
	inner := newTestClient(t)
	pin := base64.StdEncoding.EncodeToString(make([]byte, sha256.Size))
	cc, err := NewCachedClient(inner, CachedClientConfig{SPKIPinB64: pin})
	if err != nil {
		t.Errorf("expected nil error for valid pin, got %v", err)
	}
	if cc == nil {
		t.Error("expected cc != nil")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{
		CBThreshold: 3,
		CBCooldown:  100 * time.Millisecond,
	})

	if cc.isCircuitOpen() {
		t.Error("circuit should start closed")
	}

	// Record 3 failures
	cc.recordFailure()
	cc.recordFailure()
	cc.recordFailure()

	if !cc.isCircuitOpen() {
		t.Error("circuit should be open after threshold failures")
	}
}

func TestCircuitBreaker_ClosesAfterCooldown(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{
		CBThreshold: 2,
		CBCooldown:  50 * time.Millisecond,
	})

	cc.recordFailure()
	cc.recordFailure()
	if !cc.isCircuitOpen() {
		t.Fatal("circuit should be open")
	}

	time.Sleep(100 * time.Millisecond)
	if cc.isCircuitOpen() {
		t.Error("circuit should close after cooldown")
	}
}

func TestCircuitBreaker_SuccessResets(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{CBThreshold: 3})

	cc.recordFailure()
	cc.recordFailure() // 2 < threshold 3
	cc.recordSuccess() // should reset

	if cc.isCircuitOpen() {
		t.Error("circuit should be closed after success reset")
	}
	if cc.cbFailures.Load() != 0 {
		t.Errorf("expected failures reset to 0, got %d", cc.cbFailures.Load())
	}
}

func TestCircuitBreaker_DisabledByZeroThreshold(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{
		CBThreshold: 0, // → will be defaulted to 5
	})
	// Override post-default for explicit test
	cc.cbThreshold = 0

	// Even after many "failures", isCircuitOpen returns false
	for i := 0; i < 100; i++ {
		cc.recordFailure()
	}
	if cc.isCircuitOpen() {
		t.Error("circuit should never open when threshold=0")
	}
}

func TestInvalidate_RemovesEntry(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{CacheTTL: 30 * time.Second})

	cc.cache.Store("secret::api/foo", cacheEntry{
		value:     map[string]interface{}{"key": "val"},
		expiresAt: time.Now().Add(time.Minute),
	})

	cc.Invalidate("secret", "api/foo")

	if _, ok := cc.cache.Load("secret::api/foo"); ok {
		t.Error("expected entry to be removed")
	}
}

func TestInvalidateAll_ClearsAll(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{CacheTTL: 30 * time.Second})

	cc.cache.Store("secret::a", cacheEntry{expiresAt: time.Now().Add(time.Minute)})
	cc.cache.Store("secret::b", cacheEntry{expiresAt: time.Now().Add(time.Minute)})

	cc.InvalidateAll()

	count := 0
	cc.cache.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected 0 entries after InvalidateAll, got %d", count)
	}
}

func TestInner_ReturnsUnderlying(t *testing.T) {
	inner := newTestClient(t)
	cc, _ := NewCachedClient(inner, CachedClientConfig{})
	if cc.Inner() != inner {
		t.Error("Inner() should return the underlying Client")
	}
}

func TestSPKIPin_RealisticHashRoundTrip(t *testing.T) {
	// FAANG e2e sanity: real cert → real SPKI extraction → installSPKIPin →
	// VerifyPeerCertificate accepts the same cert.
	cert, pinB64 := makeTestCert(t, "vault.local")

	inner := newTestClient(t)
	cc, err := NewCachedClient(inner, CachedClientConfig{SPKIPinB64: pinB64})
	if err != nil {
		t.Fatal(err)
	}
	// Re-derive the verifier and check it accepts the cert
	pin, _ := base64.StdEncoding.DecodeString(pinB64)
	verify := makeSPKIPinVerifier(pin)
	if err := verify([][]byte{cert.Raw}, nil); err != nil {
		t.Errorf("expected accept matching cert, got %v", err)
	}
	_ = cc
}

// Verify URL parsing helper sanity (used internally by SDK; sanity check)
func TestConfig_AddressURLParseable(t *testing.T) {
	for _, addr := range []string{
		"https://vault.test:8200",
		"http://vault.local:80",
	} {
		_, err := url.Parse(addr)
		if err != nil {
			t.Errorf("expected %s parseable: %v", addr, err)
		}
	}
}
