// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"errors"
	"strings"
	"testing"
)

// The message the user received was the CONSTANT "Failed to build manifests
// with custom domains": it named the stage and not the domain, the app or the
// reason (B-152). The real error went only to the log, so whoever received the
// failure could not tell which app stopped.
func TestManifestFailureReason_NamesTheAppOfTheRealError(t *testing.T) {
	err := errors.New(`manifest_builder: missing image digest for app "api"`)
	msg := manifestFailureReason(err)

	if !strings.Contains(msg, `"api"`) {
		t.Errorf("the message does not name the app of the real error:\n%s", msg)
	}
	if !strings.Contains(msg, "digest") {
		t.Errorf("the message does not carry the reason:\n%s", msg)
	}
}

// What makes carrying the reason safe: it passes through the same scrubber that
// protects the timeline event. Without it, embedding raw error text would trade
// a useless message for a leak of Unbreakable Rule 3.
func TestManifestFailureReason_ScrubsInternalTerms(t *testing.T) {
	err := errors.New("ArgoCD sync failed while rendering manifests")
	msg := manifestFailureReason(err)

	if strings.Contains(strings.ToLower(msg), "argocd") {
		t.Errorf("an internal term leaked into the user's message:\n%s", msg)
	}
}

// A nil error must not produce "Manifest build failed: " with an empty suffix.
func TestManifestFailureReason_NilErrorHasNoEmptySuffix(t *testing.T) {
	if msg := manifestFailureReason(nil); strings.HasSuffix(msg, ": ") {
		t.Errorf("message with an empty suffix: %q", msg)
	}
}
