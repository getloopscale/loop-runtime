// Package auth — BFFKeyVault: server-side loopzero_live_* key mint + cache + revoke
// for the Dashboard BFF route family (/v1/dashboard/memory/*).
//
// Per plan dashboard-memory-bff-server-side-key-vault v1.2 § T1.1.
//
// Architectural invariants (NON-NEGOTIABLE per ADR D1 + user mandate):
//   - The minted loopzero_live_* secret NEVER leaves the vault except via the
//     internal handler-delegate boundary (BFF handler calls MintOrFetch then
//     IMMEDIATELY discards the secret; only used for audit-trail bookkeeping).
//   - Errors returned by MintOrFetch NEVER serialize the secret (EC-14).
//   - Per-(subject, tenantID) cache key is server-derived ONLY (no client
//     input; defeats cache-poisoning per ADR D1 + EC-10).
//
// Storage model: PG source-of-truth (loopzero_api_keys with bff_minted=true);
// in-memory LRU cache (256 entries) for hot-path lookups. Pod restart loses
// cache → cold-path always revokes-existing-then-mints-fresh (EC-3+EC-8).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// bffSecretPrefix matches Plan 0's user-minted key prefix for SDK compatibility.
const bffSecretPrefix = "loopzero_live_"

// bffKeyName marks BFF-minted rows; cleanup query uses this as defense if
// bff_minted column is dropped (EC-12 down-migration safety net).
const bffKeyName = "bff-dashboard"

// bffScopesJSON is the literal scopes payload written to loopzero_api_keys.scopes.
// Matches the format consumed by RequireScope middleware.
const bffScopesJSON = `["memory:read","memory:write","memory:delete"]`

// bffCacheSize bounds the in-memory LRU cache.
const bffCacheSize = 256

// BFFVaultError is a wrapped error that NEVER contains the secret (EC-14).
type BFFVaultError struct {
	Code string // INVALID_INPUT | PG_ERROR | RAND_ERROR | LOOKUP_RACE
	Msg  string // user-safe; NEVER the secret
}

func (e *BFFVaultError) Error() string { return e.Code + ": " + e.Msg }

// cachedKey is the LRU cache value (key is the cache-key SHA-256 hex).
type cachedKey struct {
	secret    string
	expiresAt time.Time
}

// BFFKeyVault mints + caches + revokes per-user loopzero_live_* API keys for
// browser-trust BFF endpoints. Concurrent-safe.
type BFFKeyVault struct {
	db    *sql.DB
	cache *lru.Cache[string, cachedKey]
	mu    sync.Mutex // serializes mint per process; PG ON CONFLICT serializes cross-process
	ttl   time.Duration
}

// NewBFFKeyVault constructs a vault with the given TTL (in hours).
// ttlHours MUST be ≥ 1 AND ≤ 168 per .claude/rules/loopzero-design.md fail-loud.
func NewBFFKeyVault(db *sql.DB, ttlHours int) (*BFFKeyVault, error) {
	if db == nil {
		return nil, &BFFVaultError{Code: "INVALID_INPUT", Msg: "db handle required"}
	}
	if ttlHours < 1 || ttlHours > 168 {
		return nil, &BFFVaultError{Code: "INVALID_INPUT", Msg: "ttlHours must be in [1, 168]"}
	}
	cache, err := lru.New[string, cachedKey](bffCacheSize)
	if err != nil {
		return nil, &BFFVaultError{Code: "INVALID_INPUT", Msg: "lru.New failed"}
	}
	return &BFFKeyVault{
		db:    db,
		cache: cache,
		ttl:   time.Duration(ttlHours) * time.Hour,
	}, nil
}

// cacheKey derives the LRU cache key from (subject, tenantID). Server-side
// only; never accepts client input (EC-10 collision defense).
func cacheKey(subject, tenantID string) string {
	h := sha256.Sum256([]byte(subject + ":" + tenantID))
	return hex.EncodeToString(h[:])
}

// MintOrFetch returns the cached BFF secret for (subject, tenantID) when
// present-and-not-expired; otherwise mints fresh (revoking any prior active
// PG row first per EC-3+EC-8).
//
// Returns "" + non-nil error on ANY failure (EC-14 no partial-mint).
func (v *BFFKeyVault) MintOrFetch(ctx context.Context, subject, tenantID string) (string, error) {
	// EC-10 — input validation FIRST (cache-key collision defense)
	if subject == "" || tenantID == "" {
		return "", &BFFVaultError{Code: "INVALID_INPUT", Msg: "subject and tenantID required"}
	}

	ck := cacheKey(subject, tenantID)
	now := time.Now()

	// EC-2 — hot path: expiresAt > now (strict; equal-moment treated as expired)
	if entry, ok := v.cache.Get(ck); ok && entry.expiresAt.After(now) {
		return entry.secret, nil
	}

	// Serialize concurrent mints for same (subject, tenantID)
	v.mu.Lock()
	defer v.mu.Unlock()

	// Re-check cache after lock acquired (avoid double-mint by goroutines that
	// queued behind the lock)
	if entry, ok := v.cache.Get(ck); ok && entry.expiresAt.After(now) {
		return entry.secret, nil
	}

	// EC-3+EC-8 — ALWAYS revoke any existing active BFF row before minting
	// fresh. Handles LRU eviction with active PG row + Pod restart scenarios.
	const revokeSQL = `UPDATE loopzero_api_keys
		SET revoked_at = NOW()
		WHERE subject = $1 AND tenant_id = $2
		  AND bff_minted = true AND revoked_at IS NULL`
	if _, err := v.db.ExecContext(ctx, revokeSQL, subject, tenantID); err != nil {
		return "", &BFFVaultError{Code: "PG_ERROR", Msg: "revoke-existing failed"}
	}

	// Mint fresh: loopzero_live_<48 random base64url chars> (matches Plan 0 shape)
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", &BFFVaultError{Code: "RAND_ERROR", Msg: "crypto/rand failed"}
	}
	secret := bffSecretPrefix + base64.RawURLEncoding.EncodeToString(rawBytes)
	keyHashBytes := sha256.Sum256([]byte(secret))
	keyHash := hex.EncodeToString(keyHashBytes[:])
	keyPrefix := secret[:12]

	// EC-21 deterministic id: SHA-256(secret) prefix → guaranteed unique per fresh secret
	// (rand collision = 2^-256). Lets ON CONFLICT defense fire if test stubs reuse rand.
	id := "bff-" + keyHash[:16]

	expiresAt := now.Add(v.ttl)

	// EC-1 INSERT ... ON CONFLICT (id) DO NOTHING + check RETURNING
	const insertSQL = `INSERT INTO loopzero_api_keys
		(id, subject, tenant_id, key_name, key_hash, key_prefix, scopes, expires_at, bff_minted, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, true, NOW())
		ON CONFLICT (id) DO NOTHING
		RETURNING id`
	row := v.db.QueryRowContext(ctx, insertSQL,
		id, subject, tenantID, bffKeyName, keyHash, keyPrefix, bffScopesJSON, expiresAt)
	var returnedID string
	if err := row.Scan(&returnedID); err != nil {
		// EC-1 — ON CONFLICT returned zero rows = another goroutine raced ahead
		// AND we hold the mutex = impossible in same process; multi-process race
		// (multi-replica future) = treat as transient lookup race
		if errors.Is(err, sql.ErrNoRows) {
			return "", &BFFVaultError{Code: "LOOKUP_RACE", Msg: "concurrent mint detected; retry"}
		}
		return "", &BFFVaultError{Code: "PG_ERROR", Msg: "insert failed"}
	}

	// Persist to cache + return
	v.cache.Add(ck, cachedKey{secret: secret, expiresAt: expiresAt})
	return secret, nil
}

// Revoke evicts cache entry + marks PG rows revoked for (subject, tenantID).
// Idempotent.
func (v *BFFKeyVault) Revoke(ctx context.Context, subject, tenantID string) error {
	if subject == "" || tenantID == "" {
		return &BFFVaultError{Code: "INVALID_INPUT", Msg: "subject and tenantID required"}
	}
	v.cache.Remove(cacheKey(subject, tenantID))
	const sqlStmt = `UPDATE loopzero_api_keys
		SET revoked_at = NOW()
		WHERE subject = $1 AND tenant_id = $2
		  AND bff_minted = true AND revoked_at IS NULL`
	if _, err := v.db.ExecContext(ctx, sqlStmt, subject, tenantID); err != nil {
		return &BFFVaultError{Code: "PG_ERROR", Msg: "revoke failed"}
	}
	return nil
}

// CleanupExpired deletes BFF-minted rows past TTL. Idempotent. Returns count.
// Intended for hourly cron invocation; safe to call concurrently with mint.
func (v *BFFKeyVault) CleanupExpired(ctx context.Context) (int64, error) {
	// EC-12 belt-and-suspenders: filter on BOTH bff_minted column AND key_name
	// so post-rollback orphans (column dropped) can still be cleaned by future
	// operator runs targeting key_name='bff-dashboard'.
	const sqlStmt = `DELETE FROM loopzero_api_keys
		WHERE bff_minted = true
		  AND key_name = $1
		  AND expires_at < NOW()`
	res, err := v.db.ExecContext(ctx, sqlStmt, bffKeyName)
	if err != nil {
		return 0, &BFFVaultError{Code: "PG_ERROR", Msg: "cleanup-expired failed"}
	}
	n, _ := res.RowsAffected()
	return n, nil
}
