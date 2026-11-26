// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/scrub"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// MaxBundleBytes is the size ceiling for source bundles. Larger payloads
// short-circuit at PushBundle entry with ReasonBundleTooLarge so the user
// gets a specific actionable message instead of a generic upload failure.
// Default 64 MiB matches the canonical Gitea repo size guidance for source
// bundles (per docs/loopzerokit-loopzerocloud-seam.md § sizing).
const MaxBundleBytes = 64 << 20

// PushBundleErrorReason discriminates failure modes returned by PushBundle.
//
// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.2 (closes loopzero-cloud#31):
// the legacy untyped fmt.Errorf chain collapsed every failure mode into the
// canonical "Ensure your git history is clean and try again." Message —
// actively misleading because the user's git history was clean. Typed reasons
// let the handler emit a specific actionable Message per failure mode.
//
// EC-6 absorption (canonical-by-construction): values are explicitly assigned
// (NOT `iota`) so future insertions in the middle don't shift existing values
// and corrupt persisted reason ints (telemetry / audit_log / downstream pins).
// The wire form (logs, audit_log, response body) is the String() output —
// every consumer reads the string label, not the int position.
type PushBundleErrorReason int

const (
	// ReasonSourceBackendUnreachable: the Gitea backend HTTP call failed
	// (DNS / TCP refused / TLS handshake / context timeout).
	ReasonSourceBackendUnreachable PushBundleErrorReason = 1
	// ReasonRemoteRejectedPush: the remote rejected the push (4xx response,
	// permission denied, repo non-existent). NOTE: gitea-race substring matches
	// (cannot lock ref / remote rejected / failed to update ref / stale info)
	// are handled by the F-03 (2026-05-14) DEPLOY_IN_PROGRESS discriminator in
	// the handler and take precedence over this reason — EC-3 absorption.
	ReasonRemoteRejectedPush PushBundleErrorReason = 2
	// ReasonBundleMalformed: the uploaded bundle is empty OR has invalid magic
	// bytes (neither tar.gz nor git bundle format).
	ReasonBundleMalformed PushBundleErrorReason = 3
	// ReasonBundleTooLarge: the bundle exceeds MaxBundleBytes.
	ReasonBundleTooLarge PushBundleErrorReason = 4
	// ReasonArchiveReadError: the io.Reader returned an error mid-read.
	ReasonArchiveReadError PushBundleErrorReason = 5
	// ReasonSourceBackendAuthFailure: the backend rejected authentication
	// (401 / 403 / "authentication required" / "permission denied" patterns).
	// Sprint 5 plan qa-sprint5-cleanup-2026-06-25 T1.1 (closes loopzero-cloud#33).
	ReasonSourceBackendAuthFailure PushBundleErrorReason = 6
	// ReasonSourceBackendCapacity: the backend ran out of capacity (507 /
	// "quota exceeded" / "no space left on device" patterns).
	ReasonSourceBackendCapacity PushBundleErrorReason = 7
	// ReasonUnknownDownstream is the catchall for unclassified downstream
	// failures. Per ADR D2 of the Sprint 5 plan: the canonical anti-pattern
	// is defaulting to a misleading specific reason (Sprint 4 catchall to
	// RemoteRejectedPush misled operators on QA #33). Operators see the
	// verbatim cause composed into the user-facing message.
	ReasonUnknownDownstream PushBundleErrorReason = 8
)

// reasonStrings is the canonical wire-form lookup table. EC-6 absorption:
// downstream consumers (logs, audit_log, telemetry, tests) pin on the String()
// output, not the int. Future inserts get the next free int without breaking
// callers.
var reasonStrings = map[PushBundleErrorReason]string{
	ReasonSourceBackendUnreachable: "SourceBackendUnreachable",
	ReasonRemoteRejectedPush:       "RemoteRejectedPush",
	ReasonBundleMalformed:          "BundleMalformed",
	ReasonBundleTooLarge:           "BundleTooLarge",
	ReasonArchiveReadError:         "ArchiveReadError",
	ReasonSourceBackendAuthFailure: "SourceBackendAuthFailure",
	ReasonSourceBackendCapacity:    "SourceBackendCapacity",
	ReasonUnknownDownstream:        "UnknownDownstream",
}

// String implements fmt.Stringer. EC-6 absorption: the wire form is the string
// label, not the int. Out-of-range values return "Unknown" defensively.
func (r PushBundleErrorReason) String() string {
	if s, ok := reasonStrings[r]; ok {
		return s
	}
	return "Unknown"
}

// reasonSummaries is the operator-actionable summary of each reason, composed
// with the verbatim cause by loopzeroerrors.NewComposed ("<summary>: <detail>").
// It lives beside the enum so a new reason and its copy change together.
// Product language only (Rule 3): "source backend", never the component name.
var reasonSummaries = map[PushBundleErrorReason]string{
	ReasonSourceBackendUnreachable: "Source backend is unreachable (network or DNS failure)",
	ReasonRemoteRejectedPush:       "The remote rejected the push",
	ReasonBundleMalformed:          "The source bundle is malformed. Re-run `loopzero deploy` from your project root",
	ReasonBundleTooLarge:           "The source bundle exceeds the size limit. Reduce the size of your source tree",
	ReasonArchiveReadError:         "Could not read the source archive. Check your project files and try again",
	ReasonSourceBackendAuthFailure: "Source backend rejected authentication",
	ReasonSourceBackendCapacity:    "Source backend is out of capacity",
	ReasonUnknownDownstream:        "Source upload failed (cause not classified)",
}

// Summary returns the user-facing summary of the reason, or "" for a value
// outside the enum (the caller then falls back to the neutral message).
func (r PushBundleErrorReason) Summary() string {
	return reasonSummaries[r]
}

// UserError is the SOURCE_UPLOAD_FAILED error shown to the user: the reason's
// summary composed with the scrubbed detail, or a trace reference when the
// detail says nothing useful.
func (e *PushBundleError) UserError(traceID string) *loopzeroerrors.LoopZeroError {
	return loopzeroerrors.NewComposed(loopzeroerrors.ErrSourceUploadFailed, e.Reason.Summary(), e.Detail, traceID)
}

// PushBundleError is the discriminated typed error returned by PushBundle.
// Sprint 4 T3.2 closes loopzero-cloud#31: replaces the untyped fmt.Errorf chain
// so SourceHandler.Upload can map each reason to a specific actionable Message
// instead of the misleading legacy "Ensure your git history is clean" string.
type PushBundleError struct {
	Reason PushBundleErrorReason
	Cause  error  // wrapped underlying error for errors.Is/As walks
	Detail string // human-readable specifics (e.g. "Gitea returned 502")
}

// Error returns a structured representation including the reason label.
func (e *PushBundleError) Error() string {
	parts := []string{fmt.Sprintf("push bundle failed: reason=%s", e.Reason.String())}
	if e.Detail != "" {
		parts = append(parts, "detail="+e.Detail)
	}
	if e.Cause != nil {
		parts = append(parts, "cause="+e.Cause.Error())
	}
	return strings.Join(parts, " ")
}

// Unwrap returns the wrapped underlying cause so errors.Is/As can walk
// through middleware that wraps PushBundleError via fmt.Errorf("...: %w", ...).
func (e *PushBundleError) Unwrap() error { return e.Cause }

// IsGiteaRaceCondition reports whether the given error string matches the
// canonical F-03 (2026-05-14) gitea-race substring set. EC-3 absorption:
// preserved verbatim — SourceHandler.Upload uses this to emit DEPLOY_IN_PROGRESS
// 409 (not SOURCE_UPLOAD_FAILED 500) for concurrent-deploy collisions, BEFORE
// the new typed-error mapping ladder.
//
// The substring set was empirically determined from real gitea-race scenarios
// observed in dogfood iterations and is documented in
// loopzero/.claude/knowledge-base/audits/F-03-concurrent-deploy-2026-05-14.md.
func IsGiteaRaceCondition(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "cannot lock ref") ||
		strings.Contains(msg, "remote rejected") ||
		strings.Contains(msg, "failed to update ref") ||
		strings.Contains(msg, "stale info")
}

// NewPushBundleError constructs a typed PushBundleError with optional cause.
// The detail is scrubbed for internal terms (Rule 3) by scrub.Text, the one
// table every user-facing path shares. The Cause is preserved unscrubbed so
// internal observability tools (logs, audit_log) see the raw error.
func NewPushBundleError(reason PushBundleErrorReason, cause error, detail string) *PushBundleError {
	return &PushBundleError{Reason: reason, Cause: cause, Detail: scrub.Text(detail)}
}

// AsPushBundleError is a convenience helper for callers that want a single
// errors.As call (returns nil + ok=false when the error chain doesn't carry
// a *PushBundleError).
func AsPushBundleError(err error) (*PushBundleError, bool) {
	var pe *PushBundleError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
