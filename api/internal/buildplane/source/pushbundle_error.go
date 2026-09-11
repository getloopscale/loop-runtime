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
	// CR-9: literal product names normalized BEFORE DNS scrubs so hostname
	// patterns (e.g., "gitea-http.loopzero-system.svc.cluster.local") still match
	// the DNS rule below — the literal-word patterns use word-boundary regex
	// to avoid double-matching inside the DNS forms.
	{regexp.MustCompile(`(?i)\bgitea\b`), "source backend"},
	{regexp.MustCompile(`(?i)\bminio\b`), "object store"},
	// DNS-form internal hostnames (case-insensitive — defense for
	// "GITEA-HTTP.loopzero-system.svc.cluster.local" mixed-case variants).
	{regexp.MustCompile(`(?i)source backend(?:-http)?\.[a-z0-9-]+\.svc\.cluster\.local`), "source backend"},
	{regexp.MustCompile(`(?i)object store(?:-[a-z0-9-]+)?\.[a-z0-9-]+\.svc\.cluster\.local`), "object store"},
	// Sprint 6 /review CODE-001 MEDIUM — Postgres / Redis / etc. driver errors
	// (`pq: could not connect to host postgresql.loopzero-system.svc.cluster.local`)
	// leaked the namespace-qualified hostname through the Sprint 5 suffix-only
	// strip. Generic catch-all matches ANY `<host>.<ns>.svc.cluster.local` form
	// BEFORE the suffix strip so the full hostname collapses to "<internal
	// service>" cleanly. Specific gitea/minio patterns above still win because
	// they fire first (Go regex slice is ordered).
	{regexp.MustCompile(`(?i)\b[a-z0-9-]+\.[a-z0-9-]+\.svc\.cluster\.local`), "<internal service>"},
	{regexp.MustCompile(`(?i)\.svc\.cluster\.local`), ""},
	// Sprint 7 plan qa-sprint7-cleanup-2026-06-25 T3.1 (closes loopzero-cloud#35
	// info-disclosure CWE-209): the verbatim cause exposure from Sprint 5/6
	// leaked internal stack names (Argo Workflows + ArgoCD + k8s ServiceAccount
	// + namespace + git-bundle paths) to tenant-`owner` accounts via
	// `loopzero deploy --json` error messages. Patterns use word-boundaries so
	// legitimate user input (e.g., project name "my-argo-test") survives.
	// Sprint 7 #35: argo leak patterns. Bare `\bargo\b` is INTENTIONALLY
	// NOT used here — `\b` treats `-` as word boundary so it would match
	// "argo" inside legitimate user terms like "my-argo-test" project names.
	// Instead, target the specific leak patterns from the Red Team evidence:
	// `argo: <op>` (colon-suffixed), `argocd` (concatenated form), and
	// `argoproj.io` (FQDN form). Standalone "argo" surrounded by dashes/
	// alphanumerics survives so user input is preserved.
	{regexp.MustCompile(`(?i)argoproj\.io`), "<orchestrator>"},
	{regexp.MustCompile(`(?i)argo:\s*create\s+\w+`), "<orchestrator>: <op>"},
	{regexp.MustCompile(`(?i)\bargocd\b`), "<orchestrator>"},
	{regexp.MustCompile(`(?i)argo\s+(?:workflows?|server)\b`), "<orchestrator>"},
	{regexp.MustCompile(`(?i)system:serviceaccount:[a-z0-9-]+:[a-z0-9-]+`), "<service identity>"},
	{regexp.MustCompile(`(?i)\bserviceaccount\b/?`), "<service identity>"},
	{regexp.MustCompile(`(?i)\bnamespace\b\s+"[^"]*"`), "<scope>"},
	{regexp.MustCompile(`(?i)\bnamespace\b`), "<scope>"},
	{regexp.MustCompile(`/tmp/loopzero-[a-zA-Z0-9_-]+/[^\s'"` + "`" + `]*`), "<server tmp>"},
	{regexp.MustCompile(`/tmp/loopzero-[a-zA-Z0-9_-]+`), "<server tmp>"},
	{regexp.MustCompile(`(?i)\bgit\s+bundle\b`), "<source archive>"},
	{regexp.MustCompile(`(?i)\bbundle\b`), "<source archive>"},
	// RFC1918 internal CIDR ranges (10/8 + 172.16/12 + 192.168/16). F-sec-1:
	// closes the gap where production CNIs (Calico 192.168/16, EKS vpc-cni
	// 172.16/12) produced internal addresses that the previous 10/8-only
	// scrub missed.
	{regexp.MustCompile(`dial tcp (?:10\.\d+\.\d+\.\d+|172\.(?:1[6-9]|2\d|3[01])\.\d+\.\d+|192\.168\.\d+\.\d+):\d+`), "dial tcp <internal>"},
	// Credential redaction. F-code-1: \S+ only consumed the FIRST whitespace-
	// separated token, exposing the credential value in two-token schemes
	// (Authorization: Bearer <token>, Authorization: Basic <b64>). [^\r\n]+
	// consumes the full header value to end-of-line. Defense-in-depth pattern
	// catches inline Bearer/Basic values that appear without the header keyword
	// (F-code-2). A bare "token <hex>" literal (e.g., from git's extraHeader
	// echo) is also caught.
	{regexp.MustCompile(`(?i)Authorization:\s*[^\r\n]+`), "Authorization: <redacted>"},
	{regexp.MustCompile(`(?i)(?:Bearer|Basic)\s+[A-Za-z0-9+/=_\-.]{16,}`), "<redacted>"},
	{regexp.MustCompile(`(?i)token\s+[A-Za-z0-9_\-.]{16,}`), "token <redacted>"},
}

func scrubInternalTerms(s string) string {
	return ScrubInternalTerms(s)
}

// ScrubInternalTerms is the canonical Rule 3 scrub helper used by ANY package
// that composes downstream error details into a user-facing message. Sprint 6
// plan qa-sprint6-cleanup-2026-06-25 T1.2 exported this from package-private
// (Sprint 5 T2.3 canonical) so api/internal/services/build_submit_error.go
// can reuse it without duplication (DRY). The Sprint 5 /review HIGH+MEDIUM
// scrub fixes (F-code-1 Authorization regex / F-sec-1 RFC1918 / F-sec-2
// literal product names) are inherited verbatim — Sprint 6 callers benefit
// from the hardened patterns automatically.
//
// Returns the input string with cluster-internal hostnames, DNS suffixes,
// internal CIDR addresses, Authorization headers (Bearer/Basic/token full
// value), inline Bearer/Basic credentials, and bare token literals replaced
// by safe operator-facing equivalents. github.com / gitlab.com and other
// legitimate external hostnames are NOT touched (negative-direction guard).
func ScrubInternalTerms(s string) string {
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
