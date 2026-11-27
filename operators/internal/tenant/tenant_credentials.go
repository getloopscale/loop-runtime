// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	k "github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/httpclient"
)

// BuildCredentials encapsulates all credential fields needed by TenantReconciler
// for provisioning build namespace secrets (storage + registry).
// Extracted from TenantReconciler to avoid a God Struct with 12+ fields
// and to separate credential concerns from reconciliation logic.
type BuildCredentials struct {
	// MinIOEndpoint is the S3-compatible endpoint for source bundles and build cache.
	MinIOEndpoint string

	// SpacesAccessKeyID and SpacesSecretAccessKey are global storage credentials
	// used when per-tenant MinIO provisioner is disabled.
	SpacesAccessKeyID     string
	SpacesSecretAccessKey string

	// DOToken is the DigitalOcean API token for generating registry credentials.
	// Mutually exclusive with RegistryDockerConfig.
	DOToken string

	// RegistryDockerConfig is the raw Docker config.json for local k3d development.
	// Mutually exclusive with DOToken.
	RegistryDockerConfig string

	// HTTPClient is used for external API calls (DO registry). If nil, a default
	// client with HTTPTimeoutExternal is used.
	HTTPClient *http.Client
}

// StorageSecretData returns the StringData map for the storage credentials Secret.
// Returns nil if no storage credentials are configured.
func (c *BuildCredentials) StorageSecretData() map[string]string {
	if c.SpacesAccessKeyID == "" || c.SpacesSecretAccessKey == "" {
		return nil
	}
	return map[string]string{
		k.KeyAWSAccessKeyID:     c.SpacesAccessKeyID,
		k.KeyAWSSecretAccessKey: c.SpacesSecretAccessKey,
		k.KeyMinIOEndpoint:      c.MinIOEndpoint,
	}
}

// FetchRegistryDockerConfig returns the Docker config.json for registry authentication.
// For DO deployments, it calls the DO API to generate credentials.
// For local dev, it returns the pre-configured config.
// Returns ("", nil) when no registry credentials are configured.
func (c *BuildCredentials) FetchRegistryDockerConfig(ctx context.Context) (string, error) {
	if c.DOToken != "" {
		return c.fetchDORegistryCredentials(ctx)
	}
	return c.RegistryDockerConfig, nil
}

// fetchDORegistryCredentials calls the DigitalOcean API to generate read-write
// registry credentials in Docker config.json format.
func (c *BuildCredentials) fetchDORegistryCredentials(ctx context.Context) (string, error) {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = httpclient.NewWithTimeout(k.HTTPTimeoutExternal)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?read_write=true&expiry_seconds=%d", k.DORegistryCredentialsURL, k.DORegistryTokenExpirySeconds),
		nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.DOToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call DO API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DO API returned %d: %s", resp.StatusCode, string(body))
	}

	// Validate the Docker config.json structure: must have "auths" with at least
	// one registry entry containing an "auth" field.
	var config dockerConfigJSON
	if err := json.Unmarshal(body, &config); err != nil {
		return "", fmt.Errorf("invalid JSON from DO API: %w", err)
	}
	if len(config.Auths) == 0 {
		return "", fmt.Errorf("DO API response missing 'auths' field or no registries")
	}
	for registry, auth := range config.Auths {
		if auth.Auth == "" {
			return "", fmt.Errorf("DO API response: registry %q has empty 'auth' field", registry)
		}
	}

	return string(body), nil
}

// dockerConfigJSON represents the Docker config.json structure for validation.
type dockerConfigJSON struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Auth string `json:"auth"`
}
