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
//
// COMP-002: Uses the official minio/madmin-go SDK for correct AWS SigV4 authentication.
package minio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/getloopscale/loopzero/pkg/naming"
)

// Compile-time interface compliance check.
var _ CredentialProvisioner = (*AdminProvisioner)(nil)

// CredentialProvisioner manages per-tenant MinIO credentials.
type CredentialProvisioner interface {
	// EnsureTenantCredentials creates or updates MinIO user and policy for a tenant.
	// Returns the access key and secret key. The caller is responsible for storing
	// them in a K8s Secret.
	EnsureTenantCredentials(ctx context.Context, tenantID string) (accessKey, secretKey string, err error)

	// DeleteTenantCredentials removes the MinIO user and policy for a tenant.
	// Called during tenant deletion to clean up per-tenant MinIO resources.
	// The operation is idempotent: if the user/policy does not exist, returns nil.
	DeleteTenantCredentials(ctx context.Context, tenantID string) error
}

// AdminProvisioner implements CredentialProvisioner using the official MinIO Admin SDK.
// Uses AWS SigV4 authentication via madmin-go (COMP-002 fix).
type AdminProvisioner struct {
	client *madmin.AdminClient
}

// AdminConfig configures the MinIO admin provisioner.
type AdminConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
}

// NewAdminProvisioner creates a provisioner that uses the MinIO Admin SDK.
// COMP-002: Uses madmin-go which handles AWS SigV4 authentication correctly,
// replacing the previous HTTP Basic Auth implementation that returned 403.
//
// cfg.Endpoint may carry a scheme: https:// enables TLS, http:// or a bare
// host:port (the in-cluster ClusterIP service, SEC-003) does not. The admin
// credentials therefore travel in clear text only when the endpoint says so.
func NewAdminProvisioner(cfg AdminConfig) (*AdminProvisioner, error) {
	host, secure, err := adminEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	client, err := madmin.NewWithOptions(host, &madmin.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: create admin client: %w", err)
	}
	return &AdminProvisioner{client: client}, nil
}

// adminEndpoint splits an endpoint into the host madmin expects and whether
// to use TLS, taken from the scheme. A bare host:port is plain HTTP.
func adminEndpoint(endpoint string) (host string, secure bool, err error) {
	if endpoint == "" {
		return "", false, errors.New("minio: admin endpoint is empty")
	}
	if !strings.Contains(endpoint, "://") {
		return endpoint, false, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("minio: parse admin endpoint %q: %w", endpoint, err)
	}
	switch u.Scheme {
	case "https":
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("minio: admin endpoint %q: scheme must be http or https", endpoint)
	}
}

// EnsureTenantCredentials creates a MinIO user for the tenant with a policy
// that restricts access to loopzero-builds/{tenantID}/* only.
//
// The user is named "loopzero-build-{tenantID}" with a randomly generated secret key.
// If the user already exists, the existing credentials are preserved.
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
	if _, err := naming.BuildNamespace(tenantID); err != nil {
		return "", "", fmt.Errorf("minio: invalid tenantID %q: %w", tenantID, err)
	}
	accessKey := fmt.Sprintf("loopzero-build-%s", tenantID)
	policyName := fmt.Sprintf("loopzero-tenant-%s", tenantID)

	// Check if user already exists — avoid rotating credentials on every reconcile,
	// which would break in-flight build workflows using the previous secret key.
	exists, err := p.userExists(ctx, accessKey)
	if err != nil {
		return "", "", err
	}
	if exists {
		// User and policy already provisioned. Ensure policy is up-to-date (idempotent).
		if err := p.ensurePolicy(ctx, policyName, tenantID); err != nil {
			return "", "", fmt.Errorf("minio: ensure policy %s: %w", policyName, err)
		}
		if err := p.attachPolicy(ctx, policyName, accessKey); err != nil {
			return "", "", fmt.Errorf("minio: attach policy %s to %s: %w", policyName, accessKey, err)
		}
		return accessKey, "", nil
	}

	// New user — generate credentials.
	secretKey, err := generateSecretKey()
	if err != nil {
		return "", "", fmt.Errorf("minio: generate secret key: %w", err)
	}

	if err := p.client.AddUser(ctx, accessKey, secretKey); err != nil {
		return "", "", fmt.Errorf("minio: add user %s: %w", accessKey, err)
	}

	if err := p.ensurePolicy(ctx, policyName, tenantID); err != nil {
		return "", "", fmt.Errorf("minio: ensure policy %s: %w", policyName, err)
	}

	if err := p.attachPolicy(ctx, policyName, accessKey); err != nil {
		return "", "", fmt.Errorf("minio: attach policy %s to %s: %w", policyName, accessKey, err)
	}

	return accessKey, secretKey, nil
}

// DeleteTenantCredentials removes the MinIO user and policy for a tenant.
// Reverses EnsureTenantCredentials: deletes user "loopzero-build-{tenantID}" and
// policy "loopzero-tenant-{tenantID}". Idempotent — returns nil if resources
// do not exist.
func (p *AdminProvisioner) DeleteTenantCredentials(ctx context.Context, tenantID string) error {
	if _, err := naming.BuildNamespace(tenantID); err != nil {
		return fmt.Errorf("minio: invalid tenantID %q: %w", tenantID, err)
	}
	accessKey := fmt.Sprintf("loopzero-build-%s", tenantID)
	policyName := fmt.Sprintf("loopzero-tenant-%s", tenantID)

	// Delete the user first (detaches policy implicitly in MinIO).
	if err := p.client.RemoveUser(ctx, accessKey); err != nil {
		// Idempotent: ignore "user does not exist" errors.
		if !isNotFoundError(err) {
			return fmt.Errorf("minio: delete user %s: %w", accessKey, err)
		}
	}

	// Delete the canned policy.
	if err := p.client.RemoveCannedPolicy(ctx, policyName); err != nil {
		if !isNotFoundError(err) {
			return fmt.Errorf("minio: delete policy %s: %w", policyName, err)
		}
	}

	return nil
}

// userExists reports whether a MinIO user already exists. Only MinIO's
// no-such-user answer means absent; any other failure is returned, because
// reading it as absent would re-add the user with a new secret.
func (p *AdminProvisioner) userExists(ctx context.Context, accessKey string) (bool, error) {
	_, err := p.client.GetUserInfo(ctx, accessKey)
	switch {
	case err == nil:
		return true, nil
	case isNotFoundError(err):
		return false, nil
	default:
		return false, fmt.Errorf("minio: look up user %s: %w", accessKey, err)
	}
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

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("minio: marshal policy: %w", err)
	}

	if err := p.client.AddCannedPolicy(ctx, policyName, policyJSON); err != nil {
		return fmt.Errorf("minio: add policy %s: %w", policyName, err)
	}
	return nil
}

// attachPolicy attaches a named policy to a MinIO user.
func (p *AdminProvisioner) attachPolicy(ctx context.Context, policyName, accessKey string) error {
	req := madmin.PolicyAssociationReq{
		Policies: []string{policyName},
		User:     accessKey,
	}
	if _, err := p.client.AttachPolicy(ctx, req); err != nil {
		return fmt.Errorf("minio: attach policy %s to %s: %w", policyName, accessKey, err)
	}
	return nil
}

// isNotFoundError reports whether err is MinIO's answer that the user or
// canned policy does not exist, read from the admin API's error code rather
// than from message text.
func isNotFoundError(err error) bool {
	var resp madmin.ErrorResponse
	if !errors.As(err, &resp) {
		return false
	}
	switch resp.Code {
	case "XMinioAdminNoSuchUser", "XMinioAdminNoSuchPolicy":
		return true
	default:
		return false
	}
}

// generateSecretKey creates a cryptographically secure random secret key.
func generateSecretKey() (string, error) {
	b := make([]byte, 20) // 40-char hex string
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
