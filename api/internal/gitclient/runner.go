// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package gitclient provides a shared git command runner and Gitea repo manager.
// It centralizes exec.CommandContext usage, credential sanitization, and Gitea
// repo creation that were previously duplicated across source, gitops, and github.
package gitclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitRunner executes git commands via exec.CommandContext with credential
// sanitization and auth header injection.
//
// All git commands run with GIT_TERMINAL_PROMPT=0 to prevent interactive prompts.
// SEC-1: Auth credentials are passed via http.extraHeader, not embedded in URLs.
// SEC-002: Token is sanitized from all error outputs.
type GitRunner struct {
	token    string
	extraEnv []string
}

// RunnerOption configures a GitRunner.
type RunnerOption func(*GitRunner)

// WithEnv adds extra environment variables to all git commands.
// Useful for GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, etc.
func WithEnv(env ...string) RunnerOption {
	return func(r *GitRunner) {
		r.extraEnv = append(r.extraEnv, env...)
	}
}

// NewGitRunner creates a GitRunner with the given Gitea token for authentication.
// An empty token creates a runner for local-only operations (no auth header).
func NewGitRunner(token string, opts ...RunnerOption) *GitRunner {
	r := &GitRunner{token: token}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes a git command in the specified directory.
// SEC-002: Output and args are sanitized to prevent token leakage in error logs.
func (r *GitRunner) Run(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204 false positive: binary literal "git"; args from trusted internal callers; SEC-002 sanitizes output
	cmd.Dir = dir
	cmd.Env = r.buildEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		sanitizedOutput := r.sanitizeOutput(string(output))
		sanitizedArgs := r.sanitizeArgs(args)
		return &CommandError{Args: strings.Join(sanitizedArgs, " "), Output: strings.TrimSpace(sanitizedOutput), Err: err}
	}
	return nil
}

// Output executes a git command and returns stdout trimmed.
// SEC-002: Args are sanitized in error messages.
func (r *GitRunner) Output(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204 false positive: binary literal "git"; args from trusted internal callers; SEC-002 sanitizes output
	cmd.Dir = dir
	cmd.Env = r.buildEnv()
	output, err := cmd.Output()
	if err != nil {
		sanitizedArgs := r.sanitizeArgs(args)
		return "", fmt.Errorf("git %s: %w", strings.Join(sanitizedArgs, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// RunWithAuth runs a git command with the auth token injected as an
// http.extraHeader. SEC-1: the credential is never embedded in the URL, and it
// is passed through git's environment configuration (GIT_CONFIG_COUNT /
// GIT_CONFIG_KEY_n / GIT_CONFIG_VALUE_n, git >= 2.31) rather than as a `-c`
// argument: argv is readable by every process in the pod through
// /proc/<pid>/cmdline and shows in process listings, the environment is not.
func (r *GitRunner) RunWithAuth(ctx context.Context, dir string, gitArgs ...string) error {
	if r.token == "" {
		return r.Run(ctx, dir, gitArgs...)
	}
	authed := *r
	authed.extraEnv = append(append([]string(nil), r.extraEnv...),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: token "+r.token,
	)
	return authed.Run(ctx, dir, gitArgs...)
}

// buildEnv returns the environment for git commands.
func (r *GitRunner) buildEnv() []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	env = append(env, r.extraEnv...)
	return env
}

// sanitizeOutput removes the token from git command output to prevent leakage in logs.
func (r *GitRunner) sanitizeOutput(output string) string {
	if r.token == "" {
		return output
	}
	return strings.ReplaceAll(output, r.token, "***")
}

// sanitizeArgs removes credentials from git command arguments for safe logging.
func (r *GitRunner) sanitizeArgs(args []string) []string {
	if r.token == "" {
		return args
	}
	safe := make([]string, len(args))
	for i, arg := range args {
		safe[i] = strings.ReplaceAll(arg, r.token, "***")
	}
	return safe
}
