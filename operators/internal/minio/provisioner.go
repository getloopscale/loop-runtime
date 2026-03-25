// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package minio provides MinIO credential provisioning for tenant isolation.
//
// Each tenant must have isolated MinIO credentials scoped to its own prefix
// (loopzero-builds/{tenant}/). Without this, a compromised build can read source
// bundles and poison caches of other tenants (spec §17.2, §17.4).
//
// The provisioner creates per-tenant MinIO users with policies that restrict
// access to the tenant's prefix only. Credentials are stored as K8s Secrets
// in the tenant's build namespace.
package minio

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CredentialProvisioner manages per-tenant MinIO credentials.
type CredentialProvisioner interface {
	// EnsureTenantCredentials creates or updates MinIO user and policy for a tenant.
	// Returns the access key and secret key. The caller is responsible for storing
	// them in a K8s Secret.
	EnsureTenantCredentials(ctx context.Context, tenantID string) (accessKey, secretKey string, err error)
}

// AdminProvisioner implements CredentialProvisioner using MinIO Admin API.
// It creates per-tenant users with bucket policies scoped to the tenant prefix.
type AdminProvisioner struct {
	endpoint  string // MinIO admin endpoint (e.g., "http://minio.loopzero-system:9000")
	accessKey string // MinIO admin access key
	secretKey string // MinIO admin secret key
	client    *http.Client
}

// AdminConfig configures the MinIO admin provisioner.
type AdminConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
}

// NewAdminProvisioner creates a provisioner that uses MinIO Admin API.
func NewAdminProvisioner(cfg AdminConfig) *AdminProvisioner {
	return &AdminProvisioner{
		endpoint:  cfg.Endpoint,
		accessKey: cfg.AccessKey,
		secretKey: cfg.SecretKey,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// EnsureTenantCredentials creates a MinIO user for the tenant with a policy
// that restricts access to loopzero-builds/{tenantID}/* only.
//
// The user is named "loopzero-build-{tenantID}" with a randomly generated secret key.
// If the user already exists, the existing credentials are rotated.
//
// Policy enforced:
//
//	{
//	  "Statement": [{
//	    "Effect": "Allow",
//	    "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"],
//	    "Resource": [
//	      "arn:aws:s3:::loopzero-builds/{tenantID}/*",
//	      "arn:aws:s3:::loopzero-builds"
//	    ],
//	    "Condition": {
//	      "StringLike": {"s3:prefix": ["{tenantID}/*"]}
//	    }
//	  }]
//	}
func (p *AdminProvisioner) EnsureTenantCredentials(ctx context.Context, tenantID string) (string, string, error) {
	accessKey := fmt.Sprintf("loopzero-build-%s", tenantID)
	secretKey, err := generateSecretKey()
	if err != nil {
		return "", "", fmt.Errorf("minio: generate secret key: %w", err)
	}

	// Create or update MinIO user via Admin API
	if err := p.ensureUser(ctx, accessKey, secretKey); err != nil {
		return "", "", fmt.Errorf("minio: ensure user %s: %w", accessKey, err)
	}

	// Create or update policy scoped to tenant prefix
	policyName := fmt.Sprintf("loopzero-tenant-%s", tenantID)
	if err := p.ensurePolicy(ctx, policyName, tenantID); err != nil {
		return "", "", fmt.Errorf("minio: ensure policy %s: %w", policyName, err)
	}

	// Attach policy to user
	if err := p.attachPolicy(ctx, policyName, accessKey); err != nil {
		return "", "", fmt.Errorf("minio: attach policy %s to %s: %w", policyName, accessKey, err)
	}

	return accessKey, secretKey, nil
}

// ensureUser creates or updates a MinIO user via the Admin API.
func (p *AdminProvisioner) ensureUser(ctx context.Context, accessKey, secretKey string) error {
	body, _ := json.Marshal(map[string]string{
		"accessKey": accessKey,
		"secretKey": secretKey,
		"status":    "enabled",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		fmt.Sprintf("%s/minio/admin/v3/add-user?accessKey=%s", p.endpoint, accessKey),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	p.signRequest(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("add-user request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add-user: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ensurePolicy creates or updates a MinIO canned policy scoped to the tenant prefix.
func (p *AdminProvisioner) ensurePolicy(ctx context.Context, policyName, tenantID string) error {
	policy := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Effect": "Allow",
				"Action": []string{
					"s3:GetObject",
					"s3:PutObject",
					"s3:DeleteObject",
				},
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::loopzero-builds/%s/*", tenantID),
				},
			},
			{
				"Effect": "Allow",
				"Action": []string{
					"s3:ListBucket",
				},
				"Resource": []string{
					"arn:aws:s3:::loopzero-builds",
				},
				"Condition": map[string]interface{}{
					"StringLike": map[string]string{
						"s3:prefix": fmt.Sprintf("%s/*", tenantID),
					},
				},
			},
		},
	}

	body, _ := json.Marshal(policy)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		fmt.Sprintf("%s/minio/admin/v3/add-canned-policy?name=%s", p.endpoint, policyName),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	p.signRequest(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("add-policy request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add-policy: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// attachPolicy attaches a named policy to a MinIO user.
func (p *AdminProvisioner) attachPolicy(ctx context.Context, policyName, accessKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		fmt.Sprintf("%s/minio/admin/v3/set-user-or-group-policy?policyName=%s&userOrGroup=%s&isGroup=false",
			p.endpoint, policyName, accessKey),
		nil)
	if err != nil {
		return err
	}
	p.signRequest(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("set-policy request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set-policy: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// signRequest adds MinIO admin API authentication headers.
func (p *AdminProvisioner) signRequest(req *http.Request) {
	req.SetBasicAuth(p.accessKey, p.secretKey)
}

// generateSecretKey creates a cryptographically secure random secret key.
func generateSecretKey() (string, error) {
	b := make([]byte, 20) // 40-char hex string
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
