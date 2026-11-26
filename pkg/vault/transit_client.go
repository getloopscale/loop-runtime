// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T2.1 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — narrow HTTP
// client for the OpenBao transit secrets engine, per-tenant key routing,
// AppRole authentication with EC-04 (singleflight) thundering-herd
// deduplication on token re-login, and SPKI cert pin defense against
// rogue-CA mis-issuance.
//
// Designed against the openbao/openbao HTTP API v1; signatures match the
// upstream Vault transit engine byte-for-byte. We deliberately do NOT
// take a dependency on the openbao Go SDK here because the SDK pulls in
// a large transitive surface for KV/auth/lease management we do not
// need at this client; staying on net/http keeps the client narrow and
// makes EC-04 singleflight + SPKI pin straightforward.

package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

// Transit is the narrow interface SecretHandler (and the secret_wrapper
// read/write path) depends on. Production injects *TransitClient; tests
// pass a fake.
type Transit interface {
	// Encrypt sends plaintext to /v1/transit/encrypt/loop-runtime-tenants-<tenantID>
	// and returns the OpenBao-shaped ciphertext (prefix "vault:v1:").
	// Caller retains ownership of plaintext; Zeroize is the caller's job.
	Encrypt(ctx context.Context, tenantID string, plaintext []byte) (ciphertext string, err error)
	// Decrypt sends a vault:v1:-prefixed ciphertext to
	// /v1/transit/decrypt/loop-runtime-tenants-<tenantID> and returns the plaintext.
	// Caller MUST Zeroize the returned slice when done.
	Decrypt(ctx context.Context, tenantID string, ciphertext string) (plaintext []byte, err error)
}

// TransitConfig configures a TransitClient. Fields are immutable after
// NewTransitClient returns.
type TransitConfig struct {
	// Addr is the OpenBao server URL (e.g. https://openbao.secrets-system:8200).
	// MUST use https:// in production; http:// is allowed only for tests
	// via httptest.NewTLSServer where the server URL is generated.
	Addr string
	// RoleID is the AppRole role_id (durable identity).
	RoleID string
	// SecretID is the AppRole secret_id (short-lived password).
	SecretID string
	// SPKIPinSHA256 is the hex-encoded SHA-256 of the OpenBao TLS leaf
	// cert's SubjectPublicKeyInfo. Empty disables the pin (NOT recommended
	// in production — defense against rogue CA mis-issuance).
	SPKIPinSHA256 string
	// HTTPTimeout is per-request timeout; default 10s.
	HTTPTimeout time.Duration
	// HTTPClient is an optional override for tests that need a custom
	// transport (e.g., httptest.NewTLSServer.Client()). When nil,
	// NewTransitClient builds a TLS-pinned net/http client.
	HTTPClient *http.Client
	// RateLimitPerSec is the per-tenant token-bucket rate for transit
	// operations (default 10). The bucket has burst capacity equal to
	// this value. 0 or negative disables rate limiting (test escape
	// hatch; production MUST set > 0). T3.4 + EC-08.
	RateLimitPerSec int
	// RateLimiterMaxEntries bounds the per-tenant limiter LRU cache to
	// prevent unbounded memory growth across cumulative tenant churn
	// (EC-08 absorption). Default 10000. Evicted limiters reset to a
	// fresh bucket on the next request — acceptable trade-off because
	// evicted tenants are by definition idle.
	RateLimiterMaxEntries int
}

// Sentinel errors NEW for the transit client (reuses pkg-level
// ErrAuthFailed, ErrVaultUnreachable, ErrPermissionDenied from types.go).
// Callers MUST use errors.Is to match.
var (
	// ErrTLSPinMismatch surfaces when the TLS handshake's leaf cert SPKI
	// hash does not match TransitConfig.SPKIPinSHA256. We refuse to talk
	// to the imposter — NO retry path.
	ErrTLSPinMismatch = errors.New("vault: TLS SPKI pin mismatch")
	// ErrInvalidCiphertext surfaces when Decrypt receives a malformed
	// ciphertext (missing vault:v1: prefix, truncated payload).
	ErrInvalidCiphertext = errors.New("vault: invalid ciphertext")
	// ErrTransitKeyMissing surfaces when a transit/encrypt 404 cannot be
	// self-healed via auto-create (key still missing after create attempt).
	// EC-06 absorption sentinel.
	ErrTransitKeyMissing = errors.New("vault: transit key missing after auto-create")
	// ErrTransitKeyCreateFailed surfaces when the auto-create POST to
	// /v1/transit/keys returns 4xx (typically 403 policy denial). Caller
	// MUST NOT retry — fix the AppRole policy.
	ErrTransitKeyCreateFailed = errors.New("vault: transit key auto-create denied")
	// ErrRateLimited surfaces when the per-tenant token-bucket rate
	// limiter rejects a transit operation. T3.4 + EC-08 absorption.
	// Caller can retry with backoff; the limit is per-tenant-per-pod, so
	// other tenants are unaffected.
	ErrRateLimited = errors.New("vault: per-tenant rate limit exceeded")
)

// tenantKeyPrefix is the canonical per-tenant transit key name prefix.
// The OpenBao transit engine treats the key name as a SINGLE path
// segment (slashes are interpreted as endpoint delimiters and return
// 404 "unsupported path"), so the convention is hyphen-separated:
//
//	transit/keys/loop-runtime-tenants-<tenantID>
//	transit/encrypt/loop-runtime-tenants-<tenantID>
//	transit/decrypt/loop-runtime-tenants-<tenantID>
//
// LIVE-caught 2026-06-10 J-22 walkthrough: an earlier draft used
// `loop-runtime-tenants/<tenantID>` (slash-separated). Root token + AppRole
// both returned 4xx because OpenBao parses `loop-runtime-tenants` as the
// key name and `<tenantID>` as an unsupported sub-operation. The
// hyphen convention keeps the namespacing intent ("loop-runtime-tenants" as
// a logical group) while staying inside the single-segment grammar.
const tenantKeyPrefix = "loop-runtime-tenants-"

// TenantKeyName returns the canonical OpenBao transit key name for
// a tenant: tenantKeyPrefix + tenantID, single-segment safe. It is the one
// definition of the name: the api encrypts with it and the operator's
// crypto-shredding path deletes it, so the two can never name different keys.
func TenantKeyName(tenantID string) string {
	return tenantKeyPrefix + tenantID
}

// TransitClient implements Transit. Safe for concurrent use after
// NewTransitClient returns.
type TransitClient struct {
	cfg        TransitConfig
	httpClient *http.Client

	mu    sync.RWMutex
	token string // current Vault token (loaded by AppRole login)

	// loginGroup deduplicates concurrent re-login attempts (EC-04
	// absorption). When 100 goroutines see a 403 simultaneously, ONE
	// AppRole login fires; all 100 share its result.
	loginGroup singleflight.Group

	// rateLimiters is the LRU-bounded per-tenant token-bucket cache
	// (T3.4 + EC-08 absorption). The LRU bound prevents unbounded
	// memory growth across cumulative tenant churn (10K entries × ~200
	// bytes/entry ≈ 2 MiB ceiling). Evicted tenants get a fresh
	// limiter on their next request — acceptable trade-off because the
	// LRU eviction means they were idle.
	rateLimiters *lru.Cache[string, *rate.Limiter]
	rateLimit    rate.Limit
	rateBurst    int

	// now supplies the instant the rate limiter consults. It exists so the burst
	// capacity can be asserted without depending on the wall clock.
	//
	// B-014: the burst test counted real HTTP calls and failed with
	// `allowed: got 11 want 10` whenever the 15 calls took longer than 100ms — at
	// 10/s one extra token refilled mid-count. It failed on timing, not behaviour.
	//
	// No new clock abstraction: `rate.Limiter` already exposes `AllowN(t, n)`, which
	// takes the instant. The seam is a function field, nil in production.
	now func() time.Time
}

// NewTransitClient constructs a TransitClient with SPKI pinning baked
// into the TLS handshake. Returns a non-nil error if cfg.Addr is empty
// or cfg.RoleID/SecretID is missing.
func NewTransitClient(cfg TransitConfig) (*TransitClient, error) {
	if cfg.Addr == "" {
		return nil, errors.New("vault: TransitConfig.Addr is empty")
	}
	if cfg.RoleID == "" || cfg.SecretID == "" {
		return nil, errors.New("vault: TransitConfig.RoleID and SecretID are required")
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	if cfg.RateLimiterMaxEntries <= 0 {
		cfg.RateLimiterMaxEntries = 10000
	}

	c := &TransitClient{cfg: cfg}

	// T3.4 + EC-08: per-tenant LRU rate limiter cache.
	if cfg.RateLimitPerSec > 0 {
		cache, err := lru.New[string, *rate.Limiter](cfg.RateLimiterMaxEntries)
		if err != nil {
			return nil, fmt.Errorf("vault: build rate limiter cache: %w", err)
		}
		c.rateLimiters = cache
		c.rateLimit = rate.Limit(cfg.RateLimitPerSec)
		c.rateBurst = cfg.RateLimitPerSec
	}

	if cfg.HTTPClient != nil {
		c.httpClient = cfg.HTTPClient
	} else {
		tlsCfg := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		if cfg.SPKIPinSHA256 != "" {
			pin, err := hex.DecodeString(cfg.SPKIPinSHA256)
			if err != nil {
				return nil, fmt.Errorf("vault: SPKIPinSHA256 hex decode: %w", err)
			}
			if len(pin) != sha256.Size {
				return nil, fmt.Errorf("vault: SPKIPinSHA256 must be a hex SHA-256 digest (%d bytes), got %d bytes",
					sha256.Size, len(pin))
			}
			tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return verifySPKIPin(rawCerts, pin)
			}
			// gosec G123 defense: when VerifyPeerCertificate is set,
			// resumed TLS sessions can bypass it because the verifier
			// only runs on a NEW handshake. VerifyConnection runs on
			// EVERY handshake (new + resumed), so re-checking the leaf
			// SPKI here ensures the pin holds across session tickets.
			// Belt + suspenders alongside VerifyPeerCertificate.
			tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return ErrTLSPinMismatch
				}
				rawCerts := make([][]byte, len(cs.PeerCertificates))
				for i, peer := range cs.PeerCertificates {
					rawCerts[i] = peer.Raw
				}
				return verifySPKIPin(rawCerts, pin)
			}
			// Skip the default chain verification because we are pinning
			// the leaf SPKI explicitly — chain validity is irrelevant.
			tlsCfg.InsecureSkipVerify = true
		}
		c.httpClient = &http.Client{
			Timeout: cfg.HTTPTimeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		}
	}

	return c, nil
}

// verifySPKIPin returns nil iff the leaf cert's SubjectPublicKeyInfo
// SHA-256 hash matches expectedPin.
func verifySPKIPin(rawCerts [][]byte, expectedPin []byte) error {
	if len(rawCerts) == 0 {
		return ErrTLSPinMismatch
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("vault: parse leaf cert: %w", err)
	}
	spki := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if !bytes.Equal(spki[:], expectedPin) {
		return ErrTLSPinMismatch
	}
	return nil
}

// timeNow returns the instant the rate limiter uses: the real clock in production;
// a test replaces `now` to freeze or advance time deliberately.
func (c *TransitClient) timeNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// acquireRateToken checks the per-tenant rate limit (T3.4 + EC-08). When
// rate limiting is disabled (cfg.RateLimitPerSec <= 0), it always
// returns nil. Returns ErrRateLimited when the tenant's bucket is empty.
func (c *TransitClient) acquireRateToken(tenantID string) error {
	if c.rateLimiters == nil {
		return nil
	}
	lim, ok := c.rateLimiters.Get(tenantID)
	if !ok {
		lim = rate.NewLimiter(c.rateLimit, c.rateBurst)
		c.rateLimiters.Add(tenantID, lim)
	}
	if !lim.AllowN(c.timeNow(), 1) {
		return fmt.Errorf("vault: tenant %s: %w", tenantID, ErrRateLimited)
	}
	return nil
}

// Encrypt sends plaintext to OpenBao's transit/encrypt endpoint scoped
// to the per-tenant key path. On a 404 (transit key missing for this
// tenant), EC-06 absorption fires: POST to
// /v1/transit/keys/loop-runtime-tenants-<tenantID> to create the key idempotently
// — HYPHEN, per the convention documented above — then retry the encrypt
// exactly once. This self-heals the first-secret-for-tenant race where
// the secret create call arrives before tenant-controller's transit key
// bootstrap completes (openbao adoption Phase 3 task 4).
//
// Auto-create is idempotent server-side: parallel callers see a 204 from
// OpenBao when the key already exists. Policy denial (403 on key-create)
// surfaces as ErrTransitKeyCreateFailed — fix the AppRole policy, do
// NOT retry.
//
// T3.4 + EC-08: per-tenant rate limiter enforced BEFORE any HTTP call
// fires. A tenant exceeding RateLimitPerSec receives ErrRateLimited
// immediately; other tenants are unaffected.
func (c *TransitClient) Encrypt(ctx context.Context, tenantID string, plaintext []byte) (string, error) {
	if tenantID == "" {
		return "", errors.New("vault: tenantID is required")
	}
	if err := c.acquireRateToken(tenantID); err != nil {
		return "", err
	}
	reqBody, err := json.Marshal(map[string]string{"plaintext": base64.StdEncoding.EncodeToString(plaintext)})
	if err != nil {
		return "", fmt.Errorf("vault: encode encrypt request: %w", err)
	}

	ct, status, err := c.tryEncrypt(ctx, tenantID, reqBody)
	if err != nil {
		return "", err
	}
	// EC-06 self-heal trigger: both 404 (root-token semantics) AND 403
	// (non-root-token enumeration-prevention semantics) can indicate
	// "key does not exist". OpenBao returns 403 on encrypt against a
	// non-existent key when the caller is NOT root, to avoid leaking
	// key existence via 404-vs-403 timing. The createTransitKey call
	// below is idempotent: 204 if key already exists, 200 if newly
	// created, real 403 if the policy lacks `create` on the key path
	// (surfaced as ErrTransitKeyCreateFailed so operators can fix the
	// policy). LIVE-caught 2026-06-10 J-22 walkthrough — earlier draft
	// only handled 404 and surfaced 403 as "transit/encrypt returned
	// 403 for tenant" without firing self-heal.
	if status == http.StatusNotFound || status == http.StatusForbidden {
		if cerr := c.createTransitKey(ctx, tenantID); cerr != nil {
			return "", cerr
		}
		ct, status, err = c.tryEncrypt(ctx, tenantID, reqBody)
		if err != nil {
			return "", err
		}
		if status == http.StatusNotFound || status == http.StatusForbidden {
			return "", fmt.Errorf("vault: encrypt still %d after auto-create for tenant %s: %w",
				status, tenantID, ErrTransitKeyMissing)
		}
	}
	if status >= 400 {
		return "", fmt.Errorf("vault: transit/encrypt returned %d for tenant %s", status, tenantID)
	}
	if ct == "" {
		return "", fmt.Errorf("vault: empty ciphertext from transit/encrypt: %w", ErrVaultUnreachable)
	}
	return ct, nil
}

// tryEncrypt performs a single transit/encrypt POST. Returns (ciphertext,
// status, transport-or-auth-err). status is exposed so the caller can
// distinguish 404 (EC-06 self-heal trigger) from other 4xx surfaces.
func (c *TransitClient) tryEncrypt(ctx context.Context, tenantID string, reqBody []byte) (string, int, error) {
	path := fmt.Sprintf("/v1/transit/encrypt/%s", TenantKeyName(tenantID))
	status, respBody, err := c.doWithRelogin(ctx, http.MethodPost, path, reqBody)
	if err != nil {
		return "", 0, err
	}
	if status >= 500 {
		return "", status, fmt.Errorf("vault: transit/encrypt %d: %w", status, ErrVaultUnreachable)
	}
	if status >= 200 && status < 300 {
		var resp struct {
			Data struct {
				Ciphertext string `json:"ciphertext"`
			} `json:"data"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return "", status, fmt.Errorf("vault: decode encrypt response: %w", err)
		}
		return resp.Data.Ciphertext, status, nil
	}
	// 4xx (excluding 403 already handled above) → caller branches on
	// status (404 triggers EC-06 self-heal; other 4xx surface as error
	// above in Encrypt).
	return "", status, nil
}

// createTransitKey POSTs to /v1/transit/keys/loop-runtime-tenants-<tenantID> with
// the standard aes256-gcm96 key type. Idempotent: when the key already
// exists OpenBao returns 204. EC-06 absorption.
//
// The separator is a HYPHEN, and this comment said `loop-runtime-tenants/<id>` until
// 2026-08-13. That form is not a typo with no consequence: transit reads the
// slash as an endpoint delimiter and returns 404 "unsupported path" — the trap
// the cutover runbook records as LIVE-caught on 2026-06-10. A reader trusting the
// old comment writes the policy for a path that can never be reached, which is
// the item this correction belongs to (B-072).
func (c *TransitClient) createTransitKey(ctx context.Context, tenantID string) error {
	reqBody, err := json.Marshal(map[string]string{
		"type":    "aes256-gcm96",
		"derived": "false",
	})
	if err != nil {
		return fmt.Errorf("vault: encode key-create request: %w", err)
	}
	path := fmt.Sprintf("/v1/transit/keys/%s", TenantKeyName(tenantID))
	status, respBody, err := c.doWithRelogin(ctx, http.MethodPost, path, reqBody)
	if err != nil {
		return err
	}
	// 200, 204, or 200-range: success (idempotent create). 4xx: policy
	// denial; surface as ErrTransitKeyCreateFailed so caller can alert.
	if status >= 200 && status < 300 {
		return nil
	}
	if status >= 400 && status < 500 {
		return fmt.Errorf("vault: transit/keys create returned %d for tenant %s: %s: %w",
			status, tenantID, truncate(respBody, 200), ErrTransitKeyCreateFailed)
	}
	return fmt.Errorf("vault: transit/keys create returned %d: %w", status, ErrVaultUnreachable)
}

// Decrypt sends a vault:v1:-prefixed ciphertext to OpenBao's
// transit/decrypt endpoint. Returns plaintext bytes; caller MUST Zeroize
// the returned slice when finished.
//
// T3.4 + EC-08: per-tenant rate limiter enforced BEFORE any HTTP call.
func (c *TransitClient) Decrypt(ctx context.Context, tenantID, ciphertext string) ([]byte, error) {
	if tenantID == "" {
		return nil, errors.New("vault: tenantID is required")
	}
	if err := c.acquireRateToken(tenantID); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(ciphertext, "vault:v1:") {
		return nil, fmt.Errorf("vault: missing vault:v1: prefix: %w", ErrInvalidCiphertext)
	}
	reqBody, err := json.Marshal(map[string]string{"ciphertext": ciphertext})
	if err != nil {
		return nil, fmt.Errorf("vault: encode decrypt request: %w", err)
	}

	path := fmt.Sprintf("/v1/transit/decrypt/%s", TenantKeyName(tenantID))
	var resp struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := c.doAuthed(ctx, http.MethodPost, path, reqBody, &resp); err != nil {
		return nil, err
	}
	plaintext, err := base64.StdEncoding.DecodeString(resp.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("vault: decode plaintext: %w", ErrInvalidCiphertext)
	}
	return plaintext, nil
}

// doWithRelogin performs a request with the current token. A 403 means the
// token expired or was revoked: it re-logs in once through the EC-04
// singleflight (at most one login per concurrent expiry burst) and retries
// exactly once. The status of the final attempt is returned for the caller
// to interpret.
func (c *TransitClient) doWithRelogin(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	token, err := c.tokenOrLogin(ctx)
	if err != nil {
		return 0, nil, err
	}
	status, respBody, err := c.do(ctx, method, path, body, token)
	if err != nil || status != http.StatusForbidden {
		return status, respBody, err
	}
	newToken, err := c.refreshToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, method, path, body, newToken)
}

// doAuthed performs doWithRelogin and decodes a 2xx JSON response into out.
func (c *TransitClient) doAuthed(ctx context.Context, method, path string, body []byte, out interface{}) error {
	status, respBody, err := c.doWithRelogin(ctx, method, path, body)
	if err != nil {
		return err
	}
	if status >= 500 {
		return fmt.Errorf("vault: %s %s returned %d: %w", method, path, status, ErrVaultUnreachable)
	}
	if status >= 400 {
		return fmt.Errorf("vault: %s %s returned %d: %s", method, path, status, truncate(respBody, 200))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("vault: decode response: %w", err)
		}
	}
	return nil
}

// do performs a single HTTP request without any auth retry logic.
func (c *TransitClient) do(ctx context.Context, method, path string, body []byte, token string) (status int, respBody []byte, err error) {
	url := c.cfg.Addr + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// A pin mismatch during the TLS handshake arrives wrapped in *url.Error;
		// errors.Is walks the chain, so the typed sentinel is surfaced as-is.
		if errors.Is(err, ErrTLSPinMismatch) {
			return 0, nil, ErrTLSPinMismatch
		}
		return 0, nil, fmt.Errorf("vault: HTTP %s %s: %w", method, path, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("vault: close %s %s response: %w", method, path, cerr)
		}
	}()

	const maxBody = 1 << 20 // 1 MiB
	respBody, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return 0, nil, fmt.Errorf("vault: read %s %s response: %w", method, path, err)
	}
	if len(respBody) > maxBody {
		return 0, nil, fmt.Errorf("vault: %s %s response body exceeded %d bytes", method, path, maxBody)
	}
	return resp.StatusCode, respBody, nil
}

// tokenOrLogin returns the cached token or triggers a login when empty.
// First call after construction always logs in.
func (c *TransitClient) tokenOrLogin(ctx context.Context) (string, error) {
	c.mu.RLock()
	t := c.token
	c.mu.RUnlock()
	if t != "" {
		return t, nil
	}
	return c.refreshToken(ctx)
}

// refreshToken performs AppRole login deduplicated via singleflight
// (EC-04 absorption). Concurrent calls during a token-expiry burst all
// share the SAME login HTTP call + receive the SAME result.
func (c *TransitClient) refreshToken(ctx context.Context) (string, error) {
	v, err, _ := c.loginGroup.Do("approle-login", func() (interface{}, error) {
		return c.doLogin(ctx)
	})
	if err != nil {
		return "", err
	}
	token, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("vault: AppRole login returned %T, want a token string: %w", v, ErrAuthFailed)
	}
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
	return token, nil
}

// doLogin performs the actual POST /v1/auth/approle/login call. Called
// ONLY through refreshToken (via singleflight) so multiple concurrent
// callers see at most one HTTP login in flight.
func (c *TransitClient) doLogin(ctx context.Context) (string, error) {
	reqBody, err := json.Marshal(map[string]string{
		"role_id":   c.cfg.RoleID,
		"secret_id": c.cfg.SecretID,
	})
	if err != nil {
		return "", fmt.Errorf("vault: encode AppRole login request: %w", err)
	}
	status, respBody, err := c.do(ctx, http.MethodPost, "/v1/auth/approle/login", reqBody, "")
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("vault: AppRole login returned %d: %s: %w",
			status, truncate(respBody, 200), ErrAuthFailed)
	}
	var resp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("vault: decode login response: %w", err)
	}
	if resp.Auth.ClientToken == "" {
		return "", fmt.Errorf("vault: empty client_token: %w", ErrAuthFailed)
	}
	return resp.Auth.ClientToken, nil
}

// truncate clips a byte slice to maxLen with a trailing ellipsis for
// safe inclusion in error messages.
func truncate(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "..."
}
