// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/gitclient"
	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

func reasonOf(t *testing.T, err error) PushBundleErrorReason {
	t.Helper()
	var pe *PushBundleError
	if !errors.As(wrapDownstreamPushError(err), &pe) {
		t.Fatalf("not a PushBundleError: %v", err)
	}
	return pe.Reason
}

// Substrings of a status code or of "eof" occur inside SHAs, offsets and
// paths; those must not decide the message the user sees.
func TestPushErrorTextThatOnlyContainsAStatusDigitsIsNotAuth(t *testing.T) {
	if got := reasonOf(t, errors.New("packfile a1b2403c9 is corrupt")); got == ReasonSourceBackendAuthFailure {
		t.Errorf("a SHA containing 403 classified as an auth failure")
	}
}

func TestPushErrorTextContainingEofInsideAWordIsNotNetwork(t *testing.T) {
	if got := reasonOf(t, errors.New("delta reoffset mismatch in pack")); got == ReasonSourceBackendUnreachable {
		t.Errorf("'reoffset' classified as a network failure")
	}
}

func TestPushErrorClassifiesATypedGiteaStatus(t *testing.T) {
	err := fmt.Errorf("ensure repo: %w", &gitearepo.StatusError{Op: "create", Repo: "loop-runtime/acme-api-source", StatusCode: 403})
	if got := reasonOf(t, err); got != ReasonSourceBackendAuthFailure {
		t.Errorf("reason = %v, want auth failure", got)
	}
}

func TestPushErrorClassifiesATypedNetworkError(t *testing.T) {
	err := fmt.Errorf("check repo: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")})
	if got := reasonOf(t, err); got != ReasonSourceBackendUnreachable {
		t.Errorf("reason = %v, want unreachable", got)
	}
}

func TestPushErrorClassifiesGitStderrFromACommandError(t *testing.T) {
	err := &gitclient.CommandError{Args: "push origin main", Output: "fatal: unable to access 'x': The requested URL returned error: 403", Err: errors.New("exit status 128")}
	if got := reasonOf(t, err); got != ReasonSourceBackendAuthFailure {
		t.Errorf("reason = %v, want auth failure", got)
	}
}
