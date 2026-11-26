// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

func silentObjectStoreLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSetupObjectUploader_UsesTheAWSCredentialChainWithoutStaticKeys(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	cfg := config.Config{ObjectStoreAWSCredentials: true}

	up, err := setupObjectUploader(cfg, silentObjectStoreLogger())

	if err != nil {
		t.Fatalf("setupObjectUploader: %v", err)
	}
	if up == nil {
		t.Fatal("no uploader for an IRSA pod, want one signing with the AWS credential chain")
	}
}

func TestSetupObjectUploader_UsesStaticKeysWhenConfigured(t *testing.T) {
	cfg := config.Config{MinIOEndpoint: "http://minio:9000", MinIOAccessKey: "ak", MinIOSecretKey: "sk"}

	up, err := setupObjectUploader(cfg, silentObjectStoreLogger())

	if err != nil || up == nil {
		t.Fatalf("setupObjectUploader = %v, %v; want a static-key uploader", up, err)
	}
}

func TestSetupObjectUploader_IsOffWithNoCredentials(t *testing.T) {
	cfg := config.Config{MinIOEndpoint: "http://minio:9000"}

	up, err := setupObjectUploader(cfg, silentObjectStoreLogger())

	if err != nil || up != nil {
		t.Fatalf("setupObjectUploader = %v, %v; want no uploader and no error", up, err)
	}
}
