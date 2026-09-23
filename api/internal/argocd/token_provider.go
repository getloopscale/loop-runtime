// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TokenProvider supplies ArgoCD API tokens to the SyncClient.
// Implementations handle token storage, caching, and refresh.
// MEET-2026-03-28-015: implemented for zero-downtime token rotation.
type TokenProvider interface {
	// Token returns the current valid token, or an error when none can be
	// had (never an empty string standing in for the error).
	Token() (string, error)
	// Refresh forces a re-read of the token source (e.g., mounted Secret file).
	// Returns nil if the token was successfully refreshed, error otherwise.
	Refresh() error
}

// AuthFailureNotifier is implemented by TokenProvider variants that can
// react to a 401/403 from ArgoCD by minting a fresh token. SyncClient
// type-asserts on this interface and calls OnAuthFailure() when an auth
// error is detected. Providers that don't implement this (eg.
// StaticTokenProvider, FileWatchTokenProvider) are simply skipped.
//
// CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15: motivated by ArgoCD admin
// session tokens expiring after 24h, leaving loopzero-api with no in-process
// recovery path. KubeMintingTokenProvider implements this.
type AuthFailureNotifier interface {
	OnAuthFailure()
}

// StaticTokenProvider returns a fixed token string.
// Used in tests and local development where token rotation is not needed.
type StaticTokenProvider struct {
	token string
}

// NewStaticTokenProvider creates a provider with a fixed token.
func NewStaticTokenProvider(token string) *StaticTokenProvider {
	return &StaticTokenProvider{token: token}
}

func (p *StaticTokenProvider) Token() (string, error) { return p.token, nil }
func (p *StaticTokenProvider) Refresh() error { return nil }

// FileWatchTokenProvider reads the token from a file (mounted K8s Secret)
// and periodically re-reads it to pick up rotations without pod restart.
//
// Architecture:
//   - Token is cached in an atomic.Value for lock-free reads (hot path)
//   - Background goroutine re-reads the file every pollInterval
//   - On-demand Refresh() for immediate re-read (called on 401)
//   - Single-flight coordination prevents stampede on concurrent 401s
type FileWatchTokenProvider struct {
	path         string
	token        atomic.Value // stores string
	logger       *slog.Logger
	pollInterval time.Duration
	refreshMu    sync.Mutex // serializes refresh calls (single-flight)
	stopCh       chan struct{}
}

// FileWatchConfig configures the FileWatchTokenProvider.
type FileWatchConfig struct {
	// Path is the file path to the token (e.g., /etc/loopzero/argocd-token).
	Path string
	// PollInterval is how often to re-read the file. Default: 60s.
	PollInterval time.Duration
	// Logger for token refresh events.
	Logger *slog.Logger
}

// NewFileWatchTokenProvider creates a provider that reads from a file.
// The file is typically a K8s Secret mounted as a volume.
// Call Start() to begin background polling.
func NewFileWatchTokenProvider(cfg FileWatchConfig) (*FileWatchTokenProvider, error) {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 60 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	p := &FileWatchTokenProvider{
		path:         cfg.Path,
		logger:       cfg.Logger,
		pollInterval: cfg.PollInterval,
		stopCh:       make(chan struct{}),
	}

	// Initial read — fail fast if file doesn't exist
	if err := p.Refresh(); err != nil {
		return nil, fmt.Errorf("initial token read from %s: %w", cfg.Path, err)
	}

	return p, nil
}

// Token returns the cached token (lock-free read via atomic.Value). The
// constructor reads the file once and fails if it cannot, so a token is
// always cached by the time a caller holds the provider.
func (p *FileWatchTokenProvider) Token() (string, error) {
	return p.cachedToken(), nil
}

func (p *FileWatchTokenProvider) cachedToken() string {
	v := p.token.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// Refresh re-reads the token from the file.
// Uses mutex to serialize concurrent refreshes (single-flight).
func (p *FileWatchTokenProvider) Refresh() error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	data, err := os.ReadFile(p.path)
	if err != nil {
		return fmt.Errorf("read token file %s: %w", p.path, err)
	}

	newToken := strings.TrimSpace(string(data))
	if newToken == "" {
		return fmt.Errorf("token file %s is empty", p.path)
	}

	oldToken := p.cachedToken()
	if newToken != oldToken {
		p.token.Store(newToken)
		if oldToken != "" {
			p.logger.Info("argocd token refreshed from file",
				"path", p.path, "old_prefix", safePrefix(oldToken), "new_prefix", safePrefix(newToken))
		}
	}

	return nil
}

// Start begins background polling for token changes.
// Call Stop() or cancel the context to end polling.
func (p *FileWatchTokenProvider) Start() {
	go func() {
		ticker := time.NewTicker(p.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-p.stopCh:
				return
			case <-ticker.C:
				if err := p.Refresh(); err != nil {
					p.logger.Warn("argocd token file refresh failed",
						"path", p.path, "error", err)
				}
			}
		}
	}()
}

// Stop ends background polling.
func (p *FileWatchTokenProvider) Stop() {
	close(p.stopCh)
}

// safePrefix returns the first 10 chars of a token for safe logging.
func safePrefix(token string) string {
	if len(token) <= 10 {
		return "***"
	}
	return token[:10] + "..."
}
