// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStaticTokenProvider(t *testing.T) {
	p := NewStaticTokenProvider("my-jwt-token")

	if p.Token() != "my-jwt-token" {
		t.Errorf("expected 'my-jwt-token', got %q", p.Token())
	}
	if err := p.Refresh(); err != nil {
		t.Errorf("Refresh should be no-op, got error: %v", err)
	}
}

func TestFileWatchTokenProvider_InitialRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	_ = os.WriteFile(path, []byte("eyJhbGciOiJIUzI1NiJ9.test"), 0644)

	p, err := NewFileWatchTokenProvider(FileWatchConfig{
		Path:   path,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if p.Token() != "eyJhbGciOiJIUzI1NiJ9.test" {
		t.Errorf("expected JWT token, got %q", p.Token())
	}
}

func TestFileWatchTokenProvider_FailsOnMissingFile(t *testing.T) {
	_, err := NewFileWatchTokenProvider(FileWatchConfig{
		Path:   "/nonexistent/path/token",
		Logger: slog.Default(),
	})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestFileWatchTokenProvider_FailsOnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	_ = os.WriteFile(path, []byte(""), 0644)

	_, err := NewFileWatchTokenProvider(FileWatchConfig{
		Path:   path,
		Logger: slog.Default(),
	})
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
}

func TestFileWatchTokenProvider_RefreshDetectsChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	_ = os.WriteFile(path, []byte("token-v1"), 0644)

	p, err := NewFileWatchTokenProvider(FileWatchConfig{
		Path:   path,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Token() != "token-v1" {
		t.Fatalf("expected token-v1, got %q", p.Token())
	}

	// Simulate Secret rotation
	_ = os.WriteFile(path, []byte("token-v2"), 0644)

	if err := p.Refresh(); err != nil {
		t.Fatalf("refresh error: %v", err)
	}

	if p.Token() != "token-v2" {
		t.Errorf("expected token-v2 after refresh, got %q", p.Token())
	}
}

func TestFileWatchTokenProvider_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	_ = os.WriteFile(path, []byte("  my-token\n  "), 0644)

	p, err := NewFileWatchTokenProvider(FileWatchConfig{
		Path:   path,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Token() != "my-token" {
		t.Errorf("expected trimmed 'my-token', got %q", p.Token())
	}
}

func TestFileWatchTokenProvider_BackgroundPoll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	_ = os.WriteFile(path, []byte("initial"), 0644)

	p, err := NewFileWatchTokenProvider(FileWatchConfig{
		Path:         path,
		PollInterval: 50 * time.Millisecond,
		Logger:       slog.Default(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p.Start()
	defer p.Stop()

	// Change the file
	_ = os.WriteFile(path, []byte("rotated"), 0644)

	// Poll until the background watcher detects the change.
	deadline := time.After(5 * time.Second)
	for {
		if p.Token() == "rotated" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for background poll to detect file change; token still %q", p.Token())
		default:
			time.Sleep(5 * time.Millisecond) // intentional: tight poll checking background file watcher
		}
	}
}
