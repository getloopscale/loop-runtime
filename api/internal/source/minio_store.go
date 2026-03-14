package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOConfig holds the configuration for connecting to MinIO.
type MinIOConfig struct {
	Endpoint  string // e.g., "minio.loopzero-system.svc.cluster.local:9000"
	AccessKey string
	SecretKey string
	Bucket    string // e.g., "loopzero-artifacts"
	UseSSL    bool
}

// MinIOSourceStore implements SourceStore using MinIO (S3-compatible) storage.
// Bundles are stored at: {bucket}/source/{tenantID}/{projectID}/{sha256}.tar.gz
// The SHA-256 hash of the content is used as the commit SHA, ensuring:
//   - Same content → same SHA (idempotent)
//   - Different content → different SHA
//   - Tenant isolation via path prefix
type MinIOSourceStore struct {
	client *minio.Client
	bucket string
}

// NewMinIOSourceStore creates a new MinIO-backed SourceStore.
func NewMinIOSourceStore(cfg MinIOConfig) (*MinIOSourceStore, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	return &MinIOSourceStore{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

// PushBundle uploads a source bundle to MinIO and returns its SHA-256 hash.
// The bundle is stored at: source/{tenantID}/{projectID}/{sha256}.tar.gz
func (s *MinIOSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (string, error) {
	// Read the entire bundle to compute SHA-256
	data, err := io.ReadAll(bundle)
	if err != nil {
		return "", fmt.Errorf("source store: reading bundle: %w", err)
	}

	if len(data) == 0 {
		return "", fmt.Errorf("source store: bundle is empty")
	}

	// Compute SHA-256 as the content-addressable identifier
	hash := sha256.Sum256(data)
	commitSHA := hex.EncodeToString(hash[:])

	// Object path: source/{tenant}/{project}/{sha}.tar.gz
	objectName := fmt.Sprintf("source/%s/%s/%s.tar.gz", tenantID, projectID, commitSHA)

	// Upload to MinIO
	reader := bytes.NewReader(data)
	_, err = s.client.PutObject(ctx, s.bucket, objectName, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	if err != nil {
		return "", fmt.Errorf("source store: upload to minio: %w", err)
	}

	return commitSHA, nil
}
