package source

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// These tests run against real MinIO.
// Set MINIO_ENDPOINT to enable (e.g., "localhost:9000").
// Without it, tests are skipped — not faked.

func skipIfNoMinIO(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("MINIO_ENDPOINT not set — skipping MinIO integration test")
	}
	return endpoint
}

func newTestStore(t *testing.T) *MinIOSourceStore {
	t.Helper()
	endpoint := skipIfNoMinIO(t)

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "loopzero-admin"
	}
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "loopzero-dev-password"
	}

	store, err := NewMinIOSourceStore(MinIOConfig{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    "loopzero-artifacts",
		UseSSL:    false,
	})
	if err != nil {
		t.Fatalf("failed to create MinIO store: %v", err)
	}
	return store
}

func TestMinIOSourceStore_PushAndRetrieve(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	bundle := []byte("#!/bin/bash\necho hello world\n")
	commitSHA, err := store.PushBundle(ctx, "acme", "my-project", bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("PushBundle failed: %v", err)
	}

	if commitSHA == "" {
		t.Fatal("expected non-empty commit SHA")
	}
	if len(commitSHA) != 64 {
		t.Errorf("expected SHA-256 hex (64 chars), got %d chars: %s", len(commitSHA), commitSHA)
	}
}

func TestMinIOSourceStore_PushEmptyBundle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.PushBundle(ctx, "acme", "my-project", bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected error for empty bundle")
	}
}

func TestMinIOSourceStore_PushTwiceReturnsDifferentSHA(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sha1, err := store.PushBundle(ctx, "acme", "proj-a", bytes.NewReader([]byte("version-1")))
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}

	sha2, err := store.PushBundle(ctx, "acme", "proj-a", bytes.NewReader([]byte("version-2")))
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}

	if sha1 == sha2 {
		t.Error("expected different SHAs for different content")
	}
}

func TestMinIOSourceStore_SameContentSameSHA(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	content := []byte("deterministic-content-42")

	sha1, err := store.PushBundle(ctx, "acme", "proj-b", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("first push failed: %v", err)
	}

	sha2, err := store.PushBundle(ctx, "acme", "proj-b", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}

	if sha1 != sha2 {
		t.Errorf("expected same SHA for same content, got %s vs %s", sha1, sha2)
	}
}

func TestMinIOSourceStore_TenantIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sha1, err := store.PushBundle(ctx, "tenant-a", "proj", bytes.NewReader([]byte("data-a")))
	if err != nil {
		t.Fatalf("push for tenant-a failed: %v", err)
	}

	sha2, err := store.PushBundle(ctx, "tenant-b", "proj", bytes.NewReader([]byte("data-b")))
	if err != nil {
		t.Fatalf("push for tenant-b failed: %v", err)
	}

	// Different tenants, different content, different SHAs
	if sha1 == sha2 {
		t.Error("expected different SHAs for different tenants with different content")
	}
}
