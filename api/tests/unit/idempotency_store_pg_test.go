// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

func TestPostgresIdempotencyStoreImplementsInterface(t *testing.T) {
	// Compile-time check: PostgresIdempotencyStore implements middleware.IdempotencyStore.
	var _ middleware.IdempotencyStore = (*db.PostgresIdempotencyStore)(nil)
}

func TestPostgresIdempotencyStoreStoreAndGet(t *testing.T) {
	t.Run("store a record and get it back with all fields matching", func(t *testing.T) {
		// Arrange
		fake := newFakeIdempotencyDB()
		store := &db.PostgresIdempotencyStore{DB: fake}
		ctx := context.Background()

		rec := &middleware.IdempotencyRecord{
			Key:            "idem-key-123",
			Endpoint:       "/api/v1/projects",
			TenantID:       "tenant-acme",
			RequestHash:    "abc123hash",
			ResponseStatus: 201,
			ResponseBody:   []byte(`{"id":"proj-abc"}`),
		}

		// Act — store
		err := store.Store(ctx, rec)
		if err != nil {
			t.Fatalf("Store() returned error: %v", err)
		}

		// Act — get
		got, err := store.Get(ctx, "idem-key-123", "tenant-acme")
		if err != nil {
			t.Fatalf("Get() returned error: %v", err)
		}

		// Assert
		if got == nil {
			t.Fatal("Get() returned nil, expected record")
		}
		if got.Key != rec.Key {
			t.Errorf("Key: got %q, want %q", got.Key, rec.Key)
		}
		if got.Endpoint != rec.Endpoint {
			t.Errorf("Endpoint: got %q, want %q", got.Endpoint, rec.Endpoint)
		}
		if got.TenantID != rec.TenantID {
			t.Errorf("TenantID: got %q, want %q", got.TenantID, rec.TenantID)
		}
		if got.RequestHash != rec.RequestHash {
			t.Errorf("RequestHash: got %q, want %q", got.RequestHash, rec.RequestHash)
		}
		if got.ResponseStatus != rec.ResponseStatus {
			t.Errorf("ResponseStatus: got %d, want %d", got.ResponseStatus, rec.ResponseStatus)
		}
		if string(got.ResponseBody) != string(rec.ResponseBody) {
			t.Errorf("ResponseBody: got %q, want %q", got.ResponseBody, rec.ResponseBody)
		}
	})
}

func TestPostgresIdempotencyStoreGetNonexistent(t *testing.T) {
	t.Run("get with unknown key returns nil without error", func(t *testing.T) {
		// Arrange
		fake := newFakeIdempotencyDB()
		store := &db.PostgresIdempotencyStore{DB: fake}
		ctx := context.Background()

		// Act
		got, err := store.Get(ctx, "nonexistent-key", "tenant-acme")

		// Assert
		if err != nil {
			t.Fatalf("Get() returned error: %v", err)
		}
		if got != nil {
			t.Errorf("Get() returned %+v, expected nil", got)
		}
	})
}

func TestPostgresIdempotencyStoreGetExpired(t *testing.T) {
	t.Run("get with expired key returns nil", func(t *testing.T) {
		// Arrange
		fake := newFakeIdempotencyDB()
		store := &db.PostgresIdempotencyStore{DB: fake}
		ctx := context.Background()

		// Store a record, then mark it as expired in the fake
		rec := &middleware.IdempotencyRecord{
			Key:            "expired-key",
			Endpoint:       "/api/v1/projects",
			TenantID:       "tenant-acme",
			RequestHash:    "hash456",
			ResponseStatus: 200,
			ResponseBody:   []byte(`{}`),
		}
		err := store.Store(ctx, rec)
		if err != nil {
			t.Fatalf("Store() returned error: %v", err)
		}

		// Expire the record in the fake
		fake.expireKey("expired-key")

		// Act
		got, err := store.Get(ctx, "expired-key", "tenant-acme")

		// Assert
		if err != nil {
			t.Fatalf("Get() returned error: %v", err)
		}
		if got != nil {
			t.Errorf("Get() returned %+v, expected nil for expired key", got)
		}
	})
}

func TestPostgresIdempotencyStoreCleanupRemovesExpired(t *testing.T) {
	t.Run("cleanup removes expired records and returns count", func(t *testing.T) {
		// Arrange
		fake := newFakeIdempotencyDB()
		store := &db.PostgresIdempotencyStore{DB: fake}
		ctx := context.Background()

		// Store 3 records, expire 2 of them
		for i, key := range []string{"key-a", "key-b", "key-c"} {
			rec := &middleware.IdempotencyRecord{
				Key:            key,
				Endpoint:       "/api/v1/projects",
				TenantID:       "tenant-acme",
				RequestHash:    "hash",
				ResponseStatus: 200,
				ResponseBody:   []byte(`{}`),
			}
			err := store.Store(ctx, rec)
			if err != nil {
				t.Fatalf("Store() record %d returned error: %v", i, err)
			}
		}
		fake.expireKey("key-a")
		fake.expireKey("key-b")

		// Act
		count, err := store.Cleanup(ctx)

		// Assert
		if err != nil {
			t.Fatalf("Cleanup() returned error: %v", err)
		}
		if count != 2 {
			t.Errorf("Cleanup() removed %d records, want 2", count)
		}
	})
}

func TestPostgresIdempotencyStoreCleanupPreservesValid(t *testing.T) {
	t.Run("cleanup preserves non-expired records", func(t *testing.T) {
		// Arrange
		fake := newFakeIdempotencyDB()
		store := &db.PostgresIdempotencyStore{DB: fake}
		ctx := context.Background()

		// Store 3 records, expire only 1
		for i, key := range []string{"valid-1", "expired-1", "valid-2"} {
			rec := &middleware.IdempotencyRecord{
				Key:            key,
				Endpoint:       "/api/v1/projects",
				TenantID:       "tenant-acme",
				RequestHash:    "hash",
				ResponseStatus: 200,
				ResponseBody:   []byte(`{}`),
			}
			err := store.Store(ctx, rec)
			if err != nil {
				t.Fatalf("Store() record %d returned error: %v", i, err)
			}
		}
		fake.expireKey("expired-1")

		// Act
		count, err := store.Cleanup(ctx)
		if err != nil {
			t.Fatalf("Cleanup() returned error: %v", err)
		}
		if count != 1 {
			t.Errorf("Cleanup() removed %d records, want 1", count)
		}

		// Assert valid records are still retrievable
		for _, key := range []string{"valid-1", "valid-2"} {
			got, err := store.Get(ctx, key, "tenant-acme")
			if err != nil {
				t.Fatalf("Get(%q) returned error: %v", key, err)
			}
			if got == nil {
				t.Errorf("Get(%q) returned nil, expected valid record to be preserved", key)
			}
		}

		// Assert expired record is gone
		got, err := store.Get(ctx, "expired-1", "tenant-acme")
		if err != nil {
			t.Fatalf("Get(expired-1) returned error: %v", err)
		}
		if got != nil {
			t.Errorf("Get(expired-1) returned record, expected nil after cleanup")
		}
	})
}

// --- Fake DB implementation ---

// fakeIdempotencyRecord stores a record with its expiry for the fake DB.
type fakeIdempotencyRecord struct {
	key            string
	tenantID       string
	endpoint       string
	requestHash    string
	responseStatus int
	responseBody   []byte
	expiresAt      time.Time
}

// fakeIdempotencyDB implements both db.QueryExecutor and db.ExecQueryExecutor
// for testing the PostgresIdempotencyStore without a real database.
type fakeIdempotencyDB struct {
	records map[string]*fakeIdempotencyRecord
}

func newFakeIdempotencyDB() *fakeIdempotencyDB {
	return &fakeIdempotencyDB{
		records: make(map[string]*fakeIdempotencyRecord),
	}
}

// expireKey sets a record's expiry to the past.
func (f *fakeIdempotencyDB) expireKey(key string) {
	if rec, ok := f.records[key]; ok {
		rec.expiresAt = time.Now().Add(-1 * time.Hour)
	}
}

// Query simulates SELECT from idempotency_keys WHERE key = $1 AND tenant_id = $2 AND expires_at > NOW().
func (f *fakeIdempotencyDB) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	key := args[0].(string)
	tenantID := args[1].(string)
	rec, ok := f.records[key]
	if !ok || rec.expiresAt.Before(time.Now()) {
		return &fakeIdempotencyRows{records: nil, pos: 0}, nil
	}
	if tenantID != "" && rec.tenantID != tenantID {
		return &fakeIdempotencyRows{records: nil, pos: 0}, nil
	}
	return &fakeIdempotencyRows{records: []*fakeIdempotencyRecord{rec}, pos: 0}, nil
}

// Exec simulates INSERT INTO idempotency_keys and DELETE WHERE expires_at < NOW().
func (f *fakeIdempotencyDB) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	// Detect if this is an INSERT (Store) or DELETE (Cleanup) based on args count.
	// Store passes 7 args (key, tenant_id, endpoint, request_hash, response_status, response_body, expires_at).
	// Cleanup passes 0 args.
	if len(args) == 7 {
		// INSERT
		rec := &fakeIdempotencyRecord{
			key:            args[0].(string),
			tenantID:       args[1].(string),
			endpoint:       args[2].(string),
			requestHash:    args[3].(string),
			responseStatus: args[4].(int),
			responseBody:   args[5].([]byte),
			expiresAt:      args[6].(time.Time),
		}
		f.records[rec.key] = rec
		return db.ExecResult{RowsAffected: 1}, nil
	}

	// DELETE expired — Cleanup
	now := time.Now()
	var count int64
	for key, rec := range f.records {
		if rec.expiresAt.Before(now) {
			delete(f.records, key)
			count++
		}
	}
	return db.ExecResult{RowsAffected: count}, nil
}

// fakeIdempotencyRows implements db.Rows for the fake.
type fakeIdempotencyRows struct {
	records []*fakeIdempotencyRecord
	pos     int
}

func (r *fakeIdempotencyRows) Next() bool {
	return r.pos < len(r.records)
}

func (r *fakeIdempotencyRows) Scan(dest ...any) error {
	rec := r.records[r.pos]
	r.pos++
	// Scan order must match the SELECT columns:
	// key, tenant_id, endpoint, request_hash, response_status, response_body
	*dest[0].(*string) = rec.key
	*dest[1].(*string) = rec.tenantID
	*dest[2].(*string) = rec.endpoint
	*dest[3].(*string) = rec.requestHash
	*dest[4].(*int) = rec.responseStatus
	*dest[5].(*[]byte) = rec.responseBody
	return nil
}

func (r *fakeIdempotencyRows) Err() error { return nil }
func (r *fakeIdempotencyRows) Close()     {}
