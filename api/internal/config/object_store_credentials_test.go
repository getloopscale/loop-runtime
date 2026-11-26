// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package config

import (
	"strings"
	"testing"
)

// An API pod on EKS carries an IRSA role (AWS_ROLE_ARN plus a projected web
// identity token) and no object store key. The object store then signs with
// the AWS credential chain and resolves the S3 endpoint the AWS way, and
// production mode accepts it: there is no static key to give it, and there
// should not be one.

func setIRSARole(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ROLE_ARN", "arn:aws:iam::000000000000:role/loopruntime-staging-core-loopruntime-api")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/secrets/eks.amazonaws.com/serviceaccount/token")
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")
}

func clearIRSARole(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ROLE_ARN", "")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")
}

func TestLoad_AnIRSAPodWithNoObjectStoreKeyUsesTheAWSCredentialChain(t *testing.T) {
	setIRSARole(t)

	cfg := Load()

	if !cfg.ObjectStoreAWSCredentials {
		t.Error("ObjectStoreAWSCredentials = false, want true for an IRSA pod with no static key")
	}
	if cfg.MinIOAccessKey != "" || cfg.MinIOSecretKey != "" {
		t.Errorf("static keys = %q/%q, want none (the dev defaults must not reach an IRSA pod)", cfg.MinIOAccessKey, cfg.MinIOSecretKey)
	}
	if cfg.MinIOEndpoint != "" {
		t.Errorf("MinIOEndpoint = %q, want empty so the store resolves the AWS S3 endpoint", cfg.MinIOEndpoint)
	}
}

func TestLoad_StaticKeysWinOverTheIRSARole(t *testing.T) {
	setIRSARole(t)
	t.Setenv("MINIO_ENDPOINT", "https://nyc3.digitaloceanspaces.com")
	t.Setenv("MINIO_ACCESS_KEY", "spaces-key")
	t.Setenv("MINIO_SECRET_KEY", "spaces-secret")

	cfg := Load()

	if cfg.ObjectStoreAWSCredentials {
		t.Error("ObjectStoreAWSCredentials = true, want false when static keys are configured")
	}
	if cfg.MinIOAccessKey != "spaces-key" || cfg.MinIOEndpoint != "https://nyc3.digitaloceanspaces.com" {
		t.Errorf("got key %q endpoint %q, want the configured ones", cfg.MinIOAccessKey, cfg.MinIOEndpoint)
	}
}

func TestLoad_WithoutARoleTheObjectStoreKeepsTheInClusterDefaults(t *testing.T) {
	clearIRSARole(t)

	cfg := Load()

	if cfg.ObjectStoreAWSCredentials {
		t.Error("ObjectStoreAWSCredentials = true without a role")
	}
	if cfg.MinIOAccessKey != devDefaultMinIOAccessKey || cfg.MinIOEndpoint == "" {
		t.Errorf("got key %q endpoint %q, want the in-cluster MinIO defaults", cfg.MinIOAccessKey, cfg.MinIOEndpoint)
	}
}

func TestValidate_ProductionAcceptsTheAWSCredentialChainWithoutStaticKeys(t *testing.T) {
	setIRSARole(t)
	t.Setenv("LOOPRUNTIME_ENV", "production")

	err := Load().validateProductionCredentials()

	if err != nil && strings.Contains(err.Error(), "MINIO_") {
		t.Fatalf("production refused an IRSA pod's object store: %v", err)
	}
}

func TestValidate_ProductionStillRefusesTheDevDefaultsWithoutARole(t *testing.T) {
	clearIRSARole(t)
	t.Setenv("LOOPRUNTIME_ENV", "production")

	err := Load().validateProductionCredentials()

	if err == nil || !strings.Contains(err.Error(), "MINIO_ACCESS_KEY") {
		t.Fatalf("err = %v, want the dev default MINIO_ACCESS_KEY refused", err)
	}
}

func TestValidate_ProductionRefusesHalfAStaticKeyPair(t *testing.T) {
	c := minProdConfig(t)
	c.MinIOSecretKey = ""

	err := c.Validate()

	if err == nil || !strings.Contains(err.Error(), "MINIO_SECRET_KEY") {
		t.Fatalf("err = %v, want an error naming the missing MINIO_SECRET_KEY", err)
	}
}

func TestValidate_ProductionRefusesAnObjectStoreWithNoCredentials(t *testing.T) {
	c := minProdConfig(t)
	c.MinIOAccessKey = ""
	c.MinIOSecretKey = ""
	c.ObjectStoreAWSCredentials = false

	err := c.Validate()

	if err == nil || !strings.Contains(err.Error(), "object store") {
		t.Fatalf("err = %v, want an error saying the object store has no credentials", err)
	}
}
