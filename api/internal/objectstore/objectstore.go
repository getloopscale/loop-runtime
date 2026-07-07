// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package objectstore defines the domain-level ObjectStore interface (M14 gap
// #5, plan ADR D4). The domain declares the contract; infrastructure adapters
// (currently only S3) satisfy it. This is DIP per rules/architecture.md § 2:
// callers depend on the interface, not on a concrete cloud SDK, so a backend
// swap never touches a call-site.
//
// Unlike KServe's pkg/credentials (blueprint Q6) which confines each backend to
// a subpackage but still links every cloud SDK into the binary, loopzero inverts
// it: one interface, one adapter (S3), zero new SDK. Multi-backend arrives only
// with a real requirement — not speculatively (YAGNI).
package objectstore

import (
	"context"
	"io"
	"time"
)

// ObjectStore is the artifact-storage contract the domain depends on. It mirrors
// exactly the surface loopzero uses today (Upload + Presign) — no speculative
// methods.
type ObjectStore interface {
	// Upload writes data to bucket/key. Errors are returned typed and unwrapped
	// — the store adds no recovery; the caller decides.
	Upload(ctx context.Context, bucket, key string, data io.Reader) error
	// Presign returns a time-limited signed URL for bucket/key.
	Presign(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}
