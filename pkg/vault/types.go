// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"errors"
	"time"
)

// Typed errors mapped from SDK errors. pkg/secrets.VaultProvider uses these
// to satisfy the Provider contract (Get returns ErrSecretNotFound on
// ErrKvNotFound; logs ErrVaultUnreachable for Chain fallback decision).
var (
	// ErrInvalidConfig — Config validation failed.
	ErrInvalidConfig = errors.New("vault: invalid config")
	// ErrAuthFailed — Kubernetes auth Login returned 4xx (JWT invalid, role missing).
	ErrAuthFailed = errors.New("vault: auth failed")
	// ErrVaultUnreachable — network/timeout/5xx after SDK retries.
	ErrVaultUnreachable = errors.New("vault: unreachable")
	// ErrKvNotFound — KVv2.Get on non-existent path (SDK returns 404 via nil secret).
	ErrKvNotFound = errors.New("vault: kv not found")
	// ErrPermissionDenied — 403 (role lacks policy on path).
	ErrPermissionDenied = errors.New("vault: permission denied")
	// ErrShuttingDown (EC-3) — SA token file empty during pod termination.
	ErrShuttingDown = errors.New("vault: shutting down")
	// ErrPolicyMisconfigured (D9 EC-19) — preflight KvGet on __health__
	// returned 403; role lacks expected policy on prefix path.
	ErrPolicyMisconfigured = errors.New("vault: policy misconfigured (role lacks expected policy)")
	// ErrNotAuthenticated — operation requires Login() to be called first.
	ErrNotAuthenticated = errors.New("vault: not authenticated (call Login first)")
)

// Config tells NewClient how to reach Vault and authenticate.
type Config struct {
	// Address is the Vault HTTP(S) endpoint. Required.
	Address string

	// K8sRole — Vault role registered under auth/<K8sAuthPath>/role/<name>.
	// Required (e.g. "loopzero-api", "loopzero-cloud", "loopzero-operators").
	K8sRole string

	// K8sAuthPath — Vault auth mount path. Default "kubernetes".
	K8sAuthPath string

	// SAJWTPath — file path containing K8s ServiceAccount JWT.
	// Default "/var/run/secrets/kubernetes.io/serviceaccount/token".
	SAJWTPath string

	// CACertPath — path to PEM CA bundle for Vault TLS verify.
	// Optional; empty uses system trust store.
	CACertPath string

	// PathPrefix — common prefix used by PreflightCheck (D9).
	// E.g. "loopzero/dev/api/secrets" → preflight reads "<prefix>/__health__".
	// Required for PreflightCheck; KvGet calls by consumers can use any path.
	PathPrefix string

	// KvMount — kv-v2 mount name. Default "secret".
	KvMount string

	// LoginTimeout — per-Login() timeout. Default 30s.
	LoginTimeout time.Duration

	// TLSInsecureSkipVerify — dev only. Production guard via consumer.
	TLSInsecureSkipVerify bool
}

// applyDefaults fills zero-value fields with safe defaults.
func applyDefaults(cfg *Config) {
	if cfg.K8sAuthPath == "" {
		cfg.K8sAuthPath = "kubernetes"
	}
	if cfg.SAJWTPath == "" {
		cfg.SAJWTPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}
	if cfg.KvMount == "" {
		cfg.KvMount = "secret"
	}
	if cfg.LoginTimeout == 0 {
		cfg.LoginTimeout = 30 * time.Second
	}
}

// validateConfig returns ErrInvalidConfig (with wrapped detail) if cfg unusable.
func validateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.Join(ErrInvalidConfig, errors.New("nil config"))
	}
	if cfg.Address == "" {
		return errors.Join(ErrInvalidConfig, errors.New("address required"))
	}
	if cfg.K8sRole == "" {
		return errors.Join(ErrInvalidConfig, errors.New("K8sRole required"))
	}
	if len(cfg.Address) < 7 ||
		(cfg.Address[:7] != "http://" && (len(cfg.Address) < 8 || cfg.Address[:8] != "https://")) {
		return errors.Join(ErrInvalidConfig, errors.New("address must be http:// or https:// URL"))
	}
	return nil
}
