// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitRunner_Run_Success(t *testing.T) {
	dir := t.TempDir()
	r := NewGitRunner("")

	// git init should succeed in a temp dir.
	if err := r.Run(context.Background(), dir, "init"); err != nil {
		t.Fatalf("Run(init) failed: %v", err)
	}

	// Verify .git dir was created.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf(".git dir not found after init: %v", err)
	}
}

func TestGitRunner_Run_Failure(t *testing.T) {
	dir := t.TempDir()
	r := NewGitRunner("")

	// Running a git command that fails.
	err := r.Run(context.Background(), dir, "log")
	if err == nil {
		t.Fatal("expected error from git log in empty dir")
	}
}

func TestGitRunner_Output_Success(t *testing.T) {
	dir := t.TempDir()
	r := NewGitRunner("")

	_ = r.Run(context.Background(), dir, "init")

	// Create a file and commit it.
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = r.Run(context.Background(), dir, "add", "-A")
	_ = r.Run(context.Background(), dir, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "init")

	sha, err := r.Output(context.Background(), dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Output(rev-parse HEAD) failed: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q (len=%d)", sha, len(sha))
	}
}

func TestGitRunner_SanitizeOutput_RemovesToken(t *testing.T) {
	r := NewGitRunner("ghp_SECRET_TOKEN_123")

	input := "fatal: unable to access 'https://ghp_SECRET_TOKEN_123@gitea.example.com/': error"
	sanitized := r.sanitizeOutput(input)
	if strings.Contains(sanitized, "ghp_SECRET_TOKEN_123") {
		t.Errorf("token not sanitized from output: %s", sanitized)
	}
	if !strings.Contains(sanitized, "***") {
		t.Errorf("expected *** replacement in: %s", sanitized)
	}
}

func TestGitRunner_SanitizeArgs_RemovesToken(t *testing.T) {
	r := NewGitRunner("ghp_SECRET_TOKEN_123")

	args := []string{"-c", "http.extraHeader=Authorization: token ghp_SECRET_TOKEN_123", "push", "origin", "main"}
	sanitized := r.sanitizeArgs(args)
	for _, arg := range sanitized {
		if strings.Contains(arg, "ghp_SECRET_TOKEN_123") {
			t.Errorf("token not sanitized from arg: %s", arg)
		}
	}
}

func TestGitRunner_SanitizeOutput_EmptyToken(t *testing.T) {
	r := NewGitRunner("")
	input := "some output"
	if r.sanitizeOutput(input) != input {
		t.Error("sanitize with empty token should return input unchanged")
	}
}

func TestGitRunner_WithEnv(t *testing.T) {
	r := NewGitRunner("", WithEnv("GIT_AUTHOR_NAME=LoopRuntime", "GIT_AUTHOR_EMAIL=loop-runtime@test.com"))

	env := r.buildEnv()
	found := 0
	for _, e := range env {
		if e == "GIT_AUTHOR_NAME=LoopRuntime" || e == "GIT_AUTHOR_EMAIL=loop-runtime@test.com" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 extra env vars, found %d", found)
	}
}

func TestGitRunner_RunWithAuth_NoToken(t *testing.T) {
	dir := t.TempDir()
	r := NewGitRunner("")

	// With no token, RunWithAuth should behave like Run.
	if err := r.Run(context.Background(), dir, "init"); err != nil {
		t.Fatal(err)
	}
	// Running RunWithAuth on a valid command should succeed.
	err := r.RunWithAuth(context.Background(), dir, "status")
	if err != nil {
		t.Fatalf("RunWithAuth without token failed: %v", err)
	}
}

func TestGitRunner_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	r := NewGitRunner("")
	_ = r.Run(context.Background(), dir, "init")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := r.Run(ctx, dir, "status")
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
