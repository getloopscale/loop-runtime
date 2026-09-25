// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scrub maps internal infrastructure identifiers to user-facing
// product language before they leave the API boundary. Per Cross-Project
// Rule 9 (loopzero/CLAUDE.md), internal terms NEVER reach user-facing output:
// the scrub runs server-side at WRITE time (per plan ADR D4), so the SSE
// event store never holds an unscrubbed term.
//
// Per plan ADR EC-1 absorption v1.1: the denylist is regex-based and
// case-insensitive. Exact-match (v1.0 design) missed canonical variants
// across casing and separators — the regex patterns in
// canonicalScrubRules cover them.
package scrub

import "regexp"

// PhaseEvent is the canonical 5-phase timeline event shape. Fields that
// carry potentially-internal text MUST be scrubbed before emission.
type PhaseEvent struct {
	Phase           string
	Status          string
	DegradedReason  string
	SubstateMessage string
}

// scrubRule pairs a compiled regex with its product-language replacement.
type scrubRule struct {
	pattern     *regexp.Regexp
	replacement string
}

// canonicalScrubRules is the ONE Rule 3 table: every path that turns an
// internal message into user-facing text (the timeline event, a build failure
// reason, a push or build-submit error detail) reads it, so a term fixed here
// is fixed for all of them.
//
// Order matters and runs most-specific first: credentials before anything can
// rewrite part of them, whole cluster hostnames before the product words they
// contain, and multi-word forms before single words.
var canonicalScrubRules = []scrubRule{
	// Credentials. [^\r\n]+ takes the whole header value, so a two-token
	// scheme (Bearer <token>, Basic <b64>) cannot leave its second token.
	{regexp.MustCompile(`(?i)Authorization:\s*[^\r\n]+`), "Authorization: <redacted>"},
	{regexp.MustCompile(`(?i)(?:Bearer|Basic)\s+[A-Za-z0-9+/=_\-.]{16,}`), "<redacted>"},
	{regexp.MustCompile(`(?i)token\s+[A-Za-z0-9_\-.]{16,}`), "token <redacted>"},
	// Cluster DNS: the named stores first, then any <host>.<ns>.svc.cluster.local,
	// then a bare suffix.
	{regexp.MustCompile(`(?i)\bgitea(?:-http)?\.[a-z0-9-]+\.svc\.cluster\.local`), "source backend"},
	{regexp.MustCompile(`(?i)\bminio(?:-[a-z0-9-]+)?\.[a-z0-9-]+\.svc\.cluster\.local`), "object store"},
	{regexp.MustCompile(`(?i)\b[a-z0-9-]+\.[a-z0-9-]+\.svc\.cluster\.local`), "<internal service>"},
	{regexp.MustCompile(`(?i)\.svc\.cluster\.local`), ""},
	// Internal RFC1918 addresses (10/8, 172.16/12, 192.168/16).
	{regexp.MustCompile(`dial tcp (?:10\.\d+\.\d+\.\d+|172\.(?:1[6-9]|2\d|3[01])\.\d+\.\d+|192\.168\.\d+\.\d+):\d+`), "dial tcp <internal>"},
	// Kubernetes identity and scope.
	{regexp.MustCompile(`(?i)system:serviceaccount:[a-z0-9-]+:[a-z0-9-]+`), "<service identity>"},
	{regexp.MustCompile(`(?i)\bserviceaccount\b/?`), "<service identity>"},
	{regexp.MustCompile(`(?i)\bnamespace\b\s+"[^"]*"`), "<scope>"},
	{regexp.MustCompile(`(?i)\bnamespace\b`), "<scope>"},
	{regexp.MustCompile(`(?i)\bCRDs?\b`), "platform resource"},
	{regexp.MustCompile(`(?i)\bkubebuilder\b`), "the platform"},
	{regexp.MustCompile(`(?i)\benvtest\b`), "the platform"},
	// Server paths and source archives.
	{regexp.MustCompile(`/tmp/loopzero-[a-zA-Z0-9_-]+/[^\s'"` + "`" + `]*`), "<server tmp>"},
	{regexp.MustCompile(`/tmp/loopzero-[a-zA-Z0-9_-]+`), "<server tmp>"},
	{regexp.MustCompile(`(?i)\bgit\s+bundle\b`), "<source archive>"},
	{regexp.MustCompile(`(?i)\bbundle\b`), "<source archive>"},
	// Stores and delivery.
	{regexp.MustCompile(`(?i)\bgitea\b`), "source backend"},
	{regexp.MustCompile(`(?i)\bminio\b`), "object store"},
	{regexp.MustCompile(`(?i)\bgitops[\s-]push\b`), "deployment update"},
	{regexp.MustCompile(`(?i)\bgitops\b`), "deployment configuration"},
	// The orchestrator. A bare "argo" is deliberately NOT matched: \b treats
	// "-" as a boundary, so it would rewrite a user's "my-argo-test".
	{regexp.MustCompile(`(?i)argoproj\.io`), "<orchestrator>"},
	{regexp.MustCompile(`(?i)argo:\s*create\s+\w+`), "<orchestrator>: <op>"},
	{regexp.MustCompile(`(?i)\bargo[\s-]?cd\b`), "the deployment system"},
	{regexp.MustCompile(`(?i)\bargo[\s-]?rollouts?\b`), "the rollout controller"},
	{regexp.MustCompile(`(?i)\bargo[\s-]?workflows?\b`), "the build pipeline"},
	{regexp.MustCompile(`(?i)\bargo\s+server\b`), "<orchestrator>"},
	// Cell topology: cell-N, cell-runtime-N, cell-mgmt.
	{regexp.MustCompile(`(?i)\bcell-[\w-]+\b`), "your team's runtime"},
	// Build and supply chain.
	{regexp.MustCompile(`(?i)\bcosign\b`), "image signing"},
	{regexp.MustCompile(`(?i)\b(?:syft|sbom)\b`), "supply-chain manifest"},
	{regexp.MustCompile(`(?i)\bbuild[\s-]?kit\b`), "the build engine"},
	{regexp.MustCompile(`(?i)\bkaniko\b`), "the build engine"},
	{regexp.MustCompile(`(?i)\bwebpack\b`), "the bundler"},
	{regexp.MustCompile(`(?i)\bturbopack\b`), "the bundler"},
	{regexp.MustCompile(`(?i)\besbuild\b`), "the bundler"},
	{regexp.MustCompile(`(?i)\blambda\b`), "Function"},
	// Data plane internals.
	{regexp.MustCompile(`(?i)\bpg_advisory\w*`), "deploy lock"},
	{regexp.MustCompile(`(?i)\badvisory\s+lock\b`), "deploy lock"},
	{regexp.MustCompile(`(?i)\btenant_id\b`), "team ID"},
}

// ScrubEvent returns a copy of in with every internal-term field replaced
// by its product-language equivalent. Pure function — safe to call from
// any goroutine. Empty input returns empty output.
func ScrubEvent(in PhaseEvent) PhaseEvent {
	out, _ := ScrubEventWithCount(in)
	return out
}

// ScrubEventWithCount is the counted variant used by the aggregator to
// feed loopzero_deploy_timeline_scrub_replacements_total. The integer is
// the total number of regex-pattern matches replaced across all four
// fields. Pure function — safe to call from any goroutine.
func ScrubEventWithCount(in PhaseEvent) (PhaseEvent, int) {
	out := in
	var total int
	out.Phase, total = applyAll(in.Phase)
	var n int
	out.Status, n = applyAll(in.Status)
	total += n
	out.DegradedReason, n = applyAll(in.DegradedReason)
	total += n
	out.SubstateMessage, n = applyAll(in.SubstateMessage)
	total += n
	return out, total
}

// applyAll runs every canonical rule against s in declaration order.
// Most-specific rules SHOULD come first in canonicalScrubRules to avoid
// partial-match underreach (a broader rule consuming text that a
// narrower rule would have rewritten more precisely).
//
// Returns the rewritten string AND the count of replacements applied —
// callers feed this count to the loopzero_deploy_timeline_scrub_replacements_total
// metric (T5.2 wiring + closes HIGH H2 dead-metric finding from review).
func applyAll(s string) (string, int) {
	replacements := 0
	for _, r := range canonicalScrubRules {
		matches := r.pattern.FindAllStringIndex(s, -1)
		if len(matches) > 0 {
			replacements += len(matches)
			s = r.pattern.ReplaceAllString(s, r.replacement)
		}
	}
	return s, replacements
}

// Text sanitises a loose string — an error message, a failure reason — before it
// crosses the API boundary.
//
// `ScrubEvent` covers the timeline event, which was the only path that existed
// when this package was born. A build failure message leaves by another path and
// used to reach the user as a CONSTANT, without the reason, precisely to avoid
// leaking an internal term (B-152). With this it can carry the reason: the same
// replacement table that protects the event protects the message.
func Text(s string) string {
	out, _ := applyAll(s)
	return out
}
