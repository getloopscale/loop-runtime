// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package objectstore

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// fakeUploader records calls and returns a canned error, letting us assert the
// adapter delegates verbatim and propagates errors without swallowing.
type fakeUploader struct {
	gotBucket, gotKey string
	gotData           string
	gotTTL            time.Duration
	uploadErr         error
	presignURL        string
	presignErr        error
}

func (f *fakeUploader) Upload(_ context.Context, bucket, key string, data io.Reader) error {
	f.gotBucket, f.gotKey = bucket, key
	b, _ := io.ReadAll(data)
	f.gotData = string(b)
	return f.uploadErr
}

func (f *fakeUploader) Presign(_ context.Context, bucket, key string, ttl time.Duration) (string, error) {
	f.gotBucket, f.gotKey, f.gotTTL = bucket, key, ttl
	return f.presignURL, f.presignErr
}

// TestS3Adapter_UploadDelegates: adapter.Upload forwards the exact args to the
// wrapped uploader.
func TestS3Adapter_UploadDelegates(t *testing.T) {
	f := &fakeUploader{}
	a := s3Adapter{up: f}
	err := a.Upload(context.Background(), "bkt", "some/key", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("Upload returned %v, want nil", err)
	}
	if f.gotBucket != "bkt" || f.gotKey != "some/key" || f.gotData != "payload" {
		t.Errorf("delegation mismatch: bucket=%q key=%q data=%q", f.gotBucket, f.gotKey, f.gotData)
	}
}

// TestS3Adapter_PresignDelegates: adapter.Presign forwards args and returns the
// wrapped URL.
func TestS3Adapter_PresignDelegates(t *testing.T) {
	f := &fakeUploader{presignURL: "https://signed"}
	a := s3Adapter{up: f}
	got, err := a.Presign(context.Background(), "bkt", "k", 5*time.Minute)
	if err != nil {
		t.Fatalf("Presign returned %v, want nil", err)
	}
	if got != "https://signed" || f.gotTTL != 5*time.Minute {
		t.Errorf("presign mismatch: url=%q ttl=%v", got, f.gotTTL)
	}
}

// TestS3Adapter_UploadPropagatesError (EC-4): a LoopZeroError from the uploader is
// returned as-is — the thin adapter never swallows (rules/error-handling.md).
func TestS3Adapter_UploadPropagatesError(t *testing.T) {
	sentinel := loopzeroerrors.NewLoopZeroError("INTERNAL", "s3 down", 500)
	f := &fakeUploader{uploadErr: sentinel}
	a := s3Adapter{up: f}
	err := a.Upload(context.Background(), "b", "k", strings.NewReader("x"))
	if !errors.Is(err, sentinel) {
		t.Errorf("adapter.Upload() = %v, want the same LoopZeroError propagated (no swallow, EC-4)", err)
	}
}

// TestNewS3ObjectStore_ImplementsInterface: compile-time proof the adapter
// satisfies ObjectStore.
func TestNewS3ObjectStore_ImplementsInterface(t *testing.T) {
	var _ ObjectStore = s3Adapter{}
}
