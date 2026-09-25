// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domain_test

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// fakeGitOpsPusher implements deploycontracts.GitOpsPusher.
type fakeGitOpsPusher struct {
	pushErr   error
	pushed    []deploycontracts.GitOpsPushOpts
	commitSHA string
}

func (f *fakeGitOpsPusher) Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	f.pushed = append(f.pushed, opts)
	if f.pushErr != nil {
		return "", f.pushErr
	}
	if f.commitSHA != "" {
		return f.commitSHA, nil
	}
	return "abc123def456abc123def456abc123def456abcd", nil
}

func (f *fakeGitOpsPusher) Remove(ctx context.Context, opts deploycontracts.GitOpsRemoveOpts) (string, error) {
	if f.commitSHA != "" {
		return f.commitSHA, nil
	}
	return "abc123def456abc123def456abc123def456abcd", nil
}

// fakeEnvironmentLocker runs fn with a nil tx, or fails with lockErr.
type fakeEnvironmentLocker struct {
	lockErr error
}

func (f *fakeEnvironmentLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	return fn(nil)
}
