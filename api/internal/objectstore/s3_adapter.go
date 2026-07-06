// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package objectstore

import (
	"context"
	"io"
	"time"

	"github.com/getloopscale/loopzero/api/internal/s3"
)

// s3Uploader is the minimal surface of *s3.Uploader that the adapter needs.
// Declaring it here (consumer-side interface, ISP) keeps the adapter testable
// with a fake without dragging the real S3 client into unit tests.
type s3Uploader interface {
	Upload(ctx context.Context, bucket, key string, data io.Reader) error
	Presign(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

// s3Adapter is the thin adapter that makes *s3.Uploader satisfy ObjectStore.
// It delegates verbatim — no recovery, no transformation — so a backend swap
// is a new adapter, not a call-site change (D4).
type s3Adapter struct{ up s3Uploader }

// NewS3ObjectStore wraps an existing *s3.Uploader as an ObjectStore. Reuses the
// S3 client loopzero already ships (api/internal/s3) — zero new SDK.
func NewS3ObjectStore(up *s3.Uploader) ObjectStore { return s3Adapter{up: up} }

func (a s3Adapter) Upload(ctx context.Context, bucket, key string, data io.Reader) error {
	return a.up.Upload(ctx, bucket, key, data)
}

func (a s3Adapter) Presign(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	return a.up.Presign(ctx, bucket, key, ttl)
}
