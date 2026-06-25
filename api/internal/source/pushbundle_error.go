// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxBundleBytes is the size ceiling for source bundles. Larger payloads
// short-circuit at PushBundle entry with ReasonBundleTooLarge so the user
// gets a specific actionable message instead of a generic upload failure.
// Default 64 MiB matches the canonical Gitea repo size guidance for source
// bundles (per docs/architecture/loopzerokit-loopzerocloud-integration.md § sizing).
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
// The detail is scrubbed for internal terms (Rule 3: no Gitea/MinIO/SVC DNS
// in user-facing strings) per Sprint 5 plan qa-sprint5-cleanup-2026-06-25
// T2.3. The Cause is preserved unscrubbed so internal observability tools
// (logs, audit_log) see the raw error.
func NewPushBundleError(reason PushBundleErrorReason, cause error, detail string) *PushBundleError {
	return &PushBundleError{Reason: reason, Cause: cause, Detail: scrubInternalTerms(detail)}
}

// scrubInternalTerms removes cluster-internal hostnames, DNS suffixes, internal
// CIDR addresses and Authorization tokens from a string so it is safe to expose
// to operators. Sprint 5 plan ADR D3 consequence: detail is composed into the
// user-facing LoopZeroError message; without scrubbing, Rule 3 ("no internal terms
// in CLI output") would be violated. Pre-compiled at package init.
var scrubPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`gitea(?:-http)?\.[a-z0-9-]+\.svc\.cluster\.local`), "source backend"},
	{regexp.MustCompile(`minio(?:-[a-z0-9-]+)?\.[a-z0-9-]+\.svc\.cluster\.local`), "object store"},
	{regexp.MustCompile(`\.svc\.cluster\.local`), ""},
	{regexp.MustCompile(`dial tcp 10\.\d+\.\d+\.\d+:\d+`), "dial tcp <internal>"},
	{regexp.MustCompile(`(?i)Authorization:\s*\S+`), "Authorization: <redacted>"},
	{regexp.MustCompile(`(?i)token\s+[A-Za-z0-9_\-.]{16,}`), "token <redacted>"},
}

func scrubInternalTerms(s string) string {
	for _, p := range scrubPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
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
