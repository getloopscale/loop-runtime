// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	openbao "github.com/openbao/openbao/api/v2"
)

// Client is the thin LoopZero helper over openbao SDK *api.Client.
// Safe for concurrent use after NewClient returns.
type Client struct {
	cfg Config

	// sdk is the underlying openbao SDK client. Created in NewClient
	// (no network); token populated by Login.
	sdk *openbao.Client

	// mu protects re-login flow (EC-1 double-check locking).
	mu sync.RWMutex

	// authenticated tracks whether Login has succeeded at least once.
	authenticated bool
}

// NewClient validates config, applies defaults, constructs SDK client.
// Does NOT make network calls. Caller must invoke Login(ctx) explicitly.
func NewClient(cfg *Config) (*Client, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	// Defensive copy — caller mutations won't affect Client.
	cfgCopy := *cfg
	applyDefaults(&cfgCopy)

	sdkCfg := openbao.DefaultConfig()
	sdkCfg.Address = cfgCopy.Address
	if cfgCopy.CACertPath != "" {
		tlsCfg := &openbao.TLSConfig{
			CACert:   cfgCopy.CACertPath,
			Insecure: cfgCopy.TLSInsecureSkipVerify,
		}
		if err := sdkCfg.ConfigureTLS(tlsCfg); err != nil {
			return nil, fmt.Errorf("%w: configure TLS: %w", ErrInvalidConfig, err)
		}
	} else if cfgCopy.TLSInsecureSkipVerify {
		tlsCfg := &openbao.TLSConfig{Insecure: true}
		if err := sdkCfg.ConfigureTLS(tlsCfg); err != nil {
			return nil, fmt.Errorf("%w: configure TLS insecure: %w", ErrInvalidConfig, err)
		}
	}

	sdk, err := openbao.NewClient(sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: SDK NewClient: %w", ErrInvalidConfig, err)
	}

	return &Client{
		cfg: cfgCopy,
		sdk: sdk,
	}, nil
}

// Config returns a copy of active config (read-only).
func (c *Client) Config() Config {
	return c.cfg
}

// SDK returns the underlying openbao SDK *api.Client.
// Consumers use it directly for KVv2/Logical/etc — no LoopZero-wrapped methods.
// Returns the SDK regardless of authentication state — caller must ensure
// Login() was called before making requests requiring auth.
func (c *Client) SDK() *openbao.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sdk
}

// Login reads the SA JWT and calls Vault kubernetes auth method.
// On success, the SDK client has the Vault token set and subsequent calls
// via SDK() are authenticated. Safe to call multiple times (re-login).
//
// EC-3 mitigation: empty SA JWT file (pod terminating) returns ErrShuttingDown
// instead of generic auth failure.
//
// EC-1 mitigation: double-check locking — after acquiring write lock,
// caller-perceived "expired" state may have been resolved by another
// goroutine's re-login. We do not gate Login() itself here (idempotent);
// consumers that want "login once or fast-path cached" use the (future)
// EnsureAuthenticated helper, not Login directly.
func (c *Client) Login(ctx context.Context) error {
	jwt, err := os.ReadFile(c.cfg.SAJWTPath)
	if err != nil {
		return fmt.Errorf("%w: read SA JWT %s: %w", ErrAuthFailed, c.cfg.SAJWTPath, err)
	}
	if len(jwt) == 0 {
		// EC-3 — pod shutdown likely
		return ErrShuttingDown
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.LoginTimeout)
	defer cancel()

	authPath := fmt.Sprintf("auth/%s/login", c.cfg.K8sAuthPath)
	resp, err := c.sdk.Logical().WriteWithContext(ctx, authPath, map[string]interface{}{
		"role": c.cfg.K8sRole,
		"jwt":  string(jwt),
	})
	if err != nil {
		return c.mapAuthErr(err)
	}
	if resp == nil || resp.Auth == nil || resp.Auth.ClientToken == "" {
		return fmt.Errorf("%w: login response missing auth token", ErrAuthFailed)
	}

	// Mutate SDK token under write lock.
	c.mu.Lock()
	c.sdk.SetToken(resp.Auth.ClientToken)
	c.authenticated = true
	c.mu.Unlock()
	return nil
}

// mapAuthErr maps SDK auth errors to typed errors.
func (c *Client) mapAuthErr(err error) error {
	var respErr *openbao.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.StatusCode {
		case 400, 401, 403:
			return fmt.Errorf("%w: %w", ErrAuthFailed, err)
		default:
			return fmt.Errorf("%w: %w", ErrVaultUnreachable, err)
		}
	}
	// Network errors / timeout
	return fmt.Errorf("%w: %w", ErrVaultUnreachable, err)
}
