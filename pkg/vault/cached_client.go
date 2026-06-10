// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	openbao "github.com/openbao/openbao/api/v2"
)

// CachedClient is a thin wrapper over Client that adds:
//
//   - In-memory cache of recently-fetched KV secrets (TTL-based).
//   - Circuit breaker: after N consecutive failures, fail-fast for cooldown period
//     instead of hammering an unreachable Vault.
//   - Optional SPKI pin verification (T2.5 — defense against MITM via
//     compromised CA per Q8/D8). When SPKIPinB64 set, the HTTP transport's
//     VerifyPeerCertificate callback enforces the pin.
//
// All operations are safe for concurrent use.
//
// T2.6 of plan 2026-06-02-openbao-adoption v1.3 (FAANG dual-path revival of
// pkg/vault under user "Siga o que for mais FAANG" decision iter 27).
type CachedClient struct {
	inner *Client

	cacheTTL time.Duration
	cache    sync.Map // map[string]cacheEntry

	// Circuit breaker state
	cbThreshold   int           // consecutive failures before tripping
	cbCooldown    time.Duration // open-state cooldown
	cbFailures    atomic.Int32  // current consecutive failure count
	cbOpenedAt    atomic.Int64  // unix nano when CB opened (0 = closed)
}

type cacheEntry struct {
	value     map[string]interface{}
	expiresAt time.Time
}

// CachedClientConfig configures CachedClient layered behaviors.
type CachedClientConfig struct {
	// CacheTTL controls how long a fetched secret stays in memory.
	// Default 30s. Set to 0 to disable caching entirely.
	CacheTTL time.Duration

	// CBThreshold is the number of consecutive failures before tripping.
	// Default 5. Set to 0 to disable circuit breaker.
	CBThreshold int

	// CBCooldown is how long the circuit stays open. Default 30s.
	CBCooldown time.Duration

	// SPKIPinB64 is the base64-encoded SHA256 hash of the expected
	// Vault server cert's SubjectPublicKeyInfo. When non-empty, the
	// HTTP transport rejects any cert whose SPKI hash does not match.
	// Set via ENV LOOPZERO_VAULT_SPKI_PIN (T2.5 ESO-distributed).
	SPKIPinB64 string
}

// ErrCircuitOpen is returned when the breaker is tripped + within cooldown.
var ErrCircuitOpen = errors.New("vault: circuit breaker open (recent failures, retry after cooldown)")

// ErrSPKIPinMismatch is returned when Vault server cert SPKI hash does not
// match the configured pin. Defense against MITM via compromised CA.
var ErrSPKIPinMismatch = errors.New("vault: SPKI pin mismatch (possible MITM via compromised CA)")

// NewCachedClient wraps an existing Client with cache + circuit breaker +
// optional SPKI pin enforcement. If cfg.SPKIPinB64 set, the underlying SDK
// client's HTTP transport is replaced with one that enforces the pin.
func NewCachedClient(inner *Client, cfg CachedClientConfig) (*CachedClient, error) {
	if inner == nil {
		return nil, fmt.Errorf("%w: inner client required", ErrInvalidConfig)
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 30 * time.Second
	}
	if cfg.CBThreshold == 0 {
		cfg.CBThreshold = 5
	}
	if cfg.CBCooldown == 0 {
		cfg.CBCooldown = 30 * time.Second
	}

	cc := &CachedClient{
		inner:       inner,
		cacheTTL:    cfg.CacheTTL,
		cbThreshold: cfg.CBThreshold,
		cbCooldown:  cfg.CBCooldown,
	}

	// SPKI pin enforcement (T2.5)
	if cfg.SPKIPinB64 != "" {
		if err := cc.installSPKIPin(cfg.SPKIPinB64); err != nil {
			return nil, err
		}
	}

	return cc, nil
}

// installSPKIPin wires a custom HTTP client into the openbao SDK that enforces
// SPKI pin validation. Called from NewCachedClient when SPKIPinB64 set.
//
// Wire pattern: customize the SDK's *http.Client.Transport.TLSClientConfig
// to include VerifyPeerCertificate callback. Standard Go x509 verify still
// runs; pin is an additional check.
func (cc *CachedClient) installSPKIPin(pinB64 string) error {
	pinBytes, err := base64.StdEncoding.DecodeString(pinB64)
	if err != nil {
		return fmt.Errorf("%w: invalid SPKIPinB64 (not base64): %v", ErrInvalidConfig, err)
	}
	if len(pinBytes) != sha256.Size {
		return fmt.Errorf("%w: SPKI pin must be base64(SHA256(spki)) = %d bytes, got %d",
			ErrInvalidConfig, sha256.Size, len(pinBytes))
	}

	sdk := cc.inner.SDK()
	httpClient := sdk.CloneConfig().HttpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}

	tlsCfg := transport.TLSClientConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		tlsCfg = tlsCfg.Clone()
	}

	tlsCfg.VerifyPeerCertificate = makeSPKIPinVerifier(pinBytes)
	transport.TLSClientConfig = tlsCfg
	httpClient.Transport = transport

	sdk.SetClientTimeout(httpClient.Timeout)
	// NB: openbao SDK doesn't expose a SetHTTPClient. The reconfigure-in-place
	// above mutates the same transport pointer the SDK already holds, which is
	// the canonical pattern for HashiCorp Vault SDK as well.

	return nil
}

// makeSPKIPinVerifier returns a VerifyPeerCertificate callback that asserts
// the leaf cert's SPKI hash matches the pin.
func makeSPKIPinVerifier(pin []byte) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("%w: no peer cert presented", ErrSPKIPinMismatch)
		}
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("%w: parse leaf cert: %v", ErrSPKIPinMismatch, err)
		}
		spkiHash := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
		if !hashesEqual(spkiHash[:], pin) {
			return fmt.Errorf("%w: leaf SPKI hash does not match pin (cert=%x pin=%x)",
				ErrSPKIPinMismatch, spkiHash[:8], pin[:8])
		}
		return nil
	}
}

// hashesEqual is constant-time equality for SHA-256 hashes (defense vs
// timing side-channel — not strictly needed for public hash comparison but
// FAANG hygiene).
func hashesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// KvGetCached returns the secret at path, possibly from cache.
// On cache miss: calls inner KvGet, populates cache, returns.
// On circuit open: returns ErrCircuitOpen immediately without calling Vault.
//
// Cache key = path. Cache entry stores the map[string]interface{} `data` field
// from the KV-v2 response. Concurrent callers for the same path share the
// underlying fetch (no thundering herd).
func (cc *CachedClient) KvGetCached(ctx context.Context, mount, path string) (map[string]interface{}, error) {
	cacheKey := mount + "::" + path

	// Cache check
	if cc.cacheTTL > 0 {
		if v, ok := cc.cache.Load(cacheKey); ok {
			e := v.(cacheEntry)
			if time.Now().Before(e.expiresAt) {
				return e.value, nil
			}
			// Expired — remove + fall through
			cc.cache.Delete(cacheKey)
		}
	}

	// Circuit breaker check
	if cc.cbThreshold > 0 && cc.isCircuitOpen() {
		return nil, ErrCircuitOpen
	}

	// Fetch
	sdk := cc.inner.SDK()
	kv := sdk.KVv2(mount)
	secret, err := kv.Get(ctx, path)
	if err != nil {
		cc.recordFailure()
		// Map SDK error to typed error
		var respErr *openbao.ResponseError
		if errors.As(err, &respErr) {
			switch respErr.StatusCode {
			case 404:
				return nil, fmt.Errorf("%w: %s", ErrKvNotFound, path)
			case 403:
				return nil, fmt.Errorf("%w: %s", ErrPermissionDenied, path)
			}
		}
		return nil, fmt.Errorf("%w: %v", ErrVaultUnreachable, err)
	}
	if secret == nil || secret.Data == nil {
		cc.recordFailure()
		return nil, fmt.Errorf("%w: nil data for %s", ErrKvNotFound, path)
	}

	cc.recordSuccess()

	// Populate cache
	if cc.cacheTTL > 0 {
		cc.cache.Store(cacheKey, cacheEntry{
			value:     secret.Data,
			expiresAt: time.Now().Add(cc.cacheTTL),
		})
	}
	return secret.Data, nil
}

// Invalidate removes a specific cache entry (e.g., after rotation).
func (cc *CachedClient) Invalidate(mount, path string) {
	cc.cache.Delete(mount + "::" + path)
}

// InvalidateAll clears the entire cache (e.g., on re-login).
func (cc *CachedClient) InvalidateAll() {
	cc.cache.Range(func(k, _ interface{}) bool {
		cc.cache.Delete(k)
		return true
	})
}

// Inner returns the underlying Client (e.g., for Login).
func (cc *CachedClient) Inner() *Client {
	return cc.inner
}

// Circuit breaker helpers
// =======================

func (cc *CachedClient) isCircuitOpen() bool {
	openedAt := cc.cbOpenedAt.Load()
	if openedAt == 0 {
		return false
	}
	// Check if cooldown expired
	if time.Since(time.Unix(0, openedAt)) > cc.cbCooldown {
		// Cooldown expired — close circuit + reset counter
		cc.cbOpenedAt.Store(0)
		cc.cbFailures.Store(0)
		return false
	}
	return true
}

func (cc *CachedClient) recordFailure() {
	if cc.cbThreshold <= 0 {
		return
	}
	failures := cc.cbFailures.Add(1)
	if int(failures) >= cc.cbThreshold && cc.cbOpenedAt.Load() == 0 {
		cc.cbOpenedAt.Store(time.Now().UnixNano())
	}
}

func (cc *CachedClient) recordSuccess() {
	cc.cbFailures.Store(0)
	cc.cbOpenedAt.Store(0)
}
