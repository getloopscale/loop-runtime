// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package objectstore is the platform's one entry point to S3-compatible
// object storage (MinIO in-cluster, DigitalOcean Spaces in production).
//
// Request signing is delegated to minio-go, the maintained client the
// operators module already uses against the same stores: AWS Signature V4 is an
// authentication protocol, and the hand-rolled version this package replaced
// mis-signed any key needing URI encoding. Consumers depend on their own
// narrow interfaces (an uploader, a URL signer); S3Store satisfies them.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// signingRegion is the SigV4 region both MinIO and DigitalOcean Spaces
	// accept for path-style requests.
	signingRegion = "us-east-1"
	// uploadTimeout bounds one upload end to end.
	uploadTimeout = 60 * time.Second
	// uploadPartSize bounds the memory an upload of unknown length buffers per
	// part; without it the client sizes parts for a 5 TiB object.
	uploadPartSize = 16 << 20
	maxPresignTTL  = 7 * 24 * time.Hour
)

// S3Store uploads objects and signs download URLs against one S3-compatible
// endpoint, addressing objects path-style: {endpoint}/{bucket}/{key}.
type S3Store struct {
	client *minio.Client
}

// NewS3Store builds a store for endpoint, which may be a full URL
// ("https://nyc3.digitaloceanspaces.com") or a bare host[:port]; a bare host
// is treated as plain HTTP, as in-cluster MinIO is.
func NewS3Store(endpoint, accessKey, secretKey string) (*S3Store, error) {
	host, secure, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(host, &minio.Options{
		Creds:        miniocreds.NewStaticV4(accessKey, secretKey, ""),
		Secure:       secure,
		Region:       signingRegion,
		BucketLookup: minio.BucketLookupPath,
		Transport:    otelhttp.NewTransport(http.DefaultTransport),
	})
	if err != nil {
		return nil, fmt.Errorf("objectstore: client for %s: %w", host, err)
	}
	return &S3Store{client: client}, nil
}

func parseEndpoint(endpoint string) (host string, secure bool, err error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return "", false, fmt.Errorf("objectstore: endpoint is empty")
	}
	if !strings.Contains(endpoint, "://") {
		return endpoint, false, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("objectstore: invalid endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false, fmt.Errorf("objectstore: endpoint %q: scheme must be http or https", endpoint)
	}
	if u.Host == "" || u.Path != "" {
		return "", false, fmt.Errorf("objectstore: endpoint %q must be scheme://host[:port]", endpoint)
	}
	return u.Host, u.Scheme == "https", nil
}

// Upload writes data to bucket/key: one signed PUT when the length is known,
// otherwise a multipart upload in bounded parts.
func (s *S3Store) Upload(ctx context.Context, bucket, key string, data io.Reader) error {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.upload",
		trace.WithAttributes(
			attribute.String("loopzero.s3.bucket", bucket),
			attribute.String("loopzero.s3.key", key),
		))
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	info, err := s.client.PutObject(ctx, bucket, key, data, knownLength(data), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
		PartSize:    uploadPartSize,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "upload failed")
		return fmt.Errorf("objectstore: upload %s/%s: %w", bucket, key, err)
	}
	span.SetAttributes(attribute.Int64("loopzero.s3.size_bytes", info.Size))
	return nil
}

// knownLength returns the byte count still to be read from data when the
// reader can tell (in-memory readers, regular files), so the upload is a
// single signed PUT; otherwise -1, and the client streams it in parts.
func knownLength(data io.Reader) int64 {
	switch r := data.(type) {
	case interface{ Len() int }:
		return int64(r.Len())
	case *os.File:
		fi, err := r.Stat()
		if err != nil || !fi.Mode().IsRegular() {
			return -1
		}
		pos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return -1
		}
		return fi.Size() - pos
	}
	return -1
}

// Presign returns a presigned GET URL for bucket/key, computed locally with no
// round trip, so a client can download without holding the store's keys. A
// TTL below one second defaults to one hour; above seven days (the SigV4
// maximum) it is clamped.
func (s *S3Store) Presign(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	if bucket == "" || key == "" {
		return "", fmt.Errorf("objectstore: bucket and key are required")
	}
	if ttl < time.Second {
		ttl = time.Hour
	}
	if ttl > maxPresignTTL {
		ttl = maxPresignTTL
	}
	u, err := s.client.PresignedGetObject(ctx, bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("objectstore: presign %s/%s: %w", bucket, key, err)
	}
	return u.String(), nil
}
