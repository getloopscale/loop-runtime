// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import "strings"

// SetReadinessSummary computes the kstatus universal summary
// (Ready / Reconciling / Stalled) from per-step detail conditions
// and writes the three summary conditions on the slice.
//
// Algorithm (mutually exclusive cascade):
//
//   - Stalled=True iff any detail has Status==False AND its Reason is
//     in {ReasonFailed, ReasonStalled, ReasonDegraded}. Stalled wins
//     over Reconciling.
//   - Reconciling=True iff (a) Stalled is not True AND (b) any detail
//     is missing OR Status==Unknown OR Status==False with a non-stalled
//     Reason.
//   - Ready=True iff every detail is Status==True with
//     ObservedGeneration matching the resource generation.
//
// Edge cases:
//
//   - Empty detailTypes → Ready=True with reason "NoDetailsRequired"
//     so kstatus consumers do not hang waiting forever.
//   - ObservedGeneration mismatch on a True detail → Reconciling=True
//     with a "spec changed" message.
//   - Unknown status → treated as in-progress.
//   - Idempotent: repeated calls with the same inputs do NOT advance
//     LastTransitionTime (SetCondition preserves it when Status holds).
//
// Per ADR-0032. The operator module reaches this implementation through
// operators/internal/conditions.SetReadinessSummaryViaPkg.
//
// MUST be the LAST status mutation in every Reconcile so the summary
// reflects the latest detail snapshot. Pair with `defer` so it runs
// even on early return / panic.
func SetReadinessSummary(conds *[]Condition, generation int64, detailTypes []string) {
	if conds == nil {
		return
	}
	if len(detailTypes) == 0 {
		writeSummary(conds, generation, summaryOutcome{
			ready: StatusTrue, reason: "NoDetailsRequired",
			readyMessage: "no detail conditions configured for this resource",
		})
		return
	}
	writeSummary(conds, generation, classifyDetails(*conds, generation, detailTypes).outcome())
}

// detailSnapshot is what the detail conditions say, before it is turned into
// a summary.
type detailSnapshot struct {
	stalledHits           []string // "Type=Reason" for every detail halted on a stalled reason
	missing               []string // detail types with no condition yet
	firstInProgress       string   // "Type=Reason" of the first detail still converging
	hasGenerationMismatch bool     // a True detail observed an older generation
}

// classifyDetails reads each declared detail condition, in declaration order.
func classifyDetails(conds []Condition, generation int64, detailTypes []string) detailSnapshot {
	var snap detailSnapshot
	for _, dt := range detailTypes {
		c := GetCondition(conds, dt)
		switch {
		case c == nil:
			snap.missing = append(snap.missing, dt)
		case c.Status == StatusTrue:
			if c.ObservedGeneration != 0 && c.ObservedGeneration != generation {
				snap.hasGenerationMismatch = true
			}
		case c.Status == StatusFalse && isStalledReason(c.Reason):
			snap.stalledHits = append(snap.stalledHits, dt+"="+c.Reason)
		case c.Status == StatusFalse:
			snap.noteInProgress(dt + "=" + c.Reason)
		default: // StatusUnknown or any non-canonical value
			snap.noteInProgress(dt + "=Unknown")
		}
	}
	return snap
}

func isStalledReason(reason string) bool {
	_, stalled := stalledReasons[reason]
	return stalled
}

// noteInProgress keeps the first in-progress detail: it is the one the
// message names.
func (s *detailSnapshot) noteInProgress(detail string) {
	if s.firstInProgress == "" {
		s.firstInProgress = detail
	}
}

// summaryOutcome is the three summary conditions to write. Reconciling and
// Stalled are the complement of ready unless set explicitly.
type summaryOutcome struct {
	ready        Status
	reason       string
	readyMessage string
	reconciling  Status
	stalled      Status
	// reconcilingMessage and stalledMessage are empty unless the branch
	// explains itself on that condition.
	reconcilingMessage string
	stalledMessage     string
}

// outcome applies the cascade: stalled beats reconciling, reconciling beats
// ready.
func (s detailSnapshot) outcome() summaryOutcome {
	if len(s.stalledHits) > 0 {
		msg := strings.Join(s.stalledHits, ", ")
		return summaryOutcome{
			ready: StatusFalse, reason: ReasonStalled, readyMessage: "halted: " + msg,
			reconciling: StatusFalse, reconcilingMessage: "halted: " + msg,
			stalled: StatusTrue, stalledMessage: msg,
		}
	}
	if msg, converging := s.convergingMessage(); converging {
		return summaryOutcome{
			ready: StatusFalse, reason: ReasonReconciling, readyMessage: msg,
			reconciling: StatusTrue, reconcilingMessage: msg,
		}
	}
	return summaryOutcome{ready: StatusTrue, reason: ReasonReady, readyMessage: "all detail conditions satisfied"}
}

// convergingMessage reports why the resource is still converging, and
// whether it is. Missing details are named first, then a spec change, then
// the first in-progress detail.
func (s detailSnapshot) convergingMessage() (string, bool) {
	switch {
	case len(s.missing) > 0:
		return "awaiting: " + strings.Join(s.missing, ", "), true
	case s.hasGenerationMismatch:
		return "spec changed; controller catching up", true
	case s.firstInProgress != "":
		return "in progress: " + s.firstInProgress, true
	}
	return "", false
}

func writeSummary(conds *[]Condition, generation int64, o summaryOutcome) {
	reconciling, stalled := o.reconciling, o.stalled
	if reconciling == "" {
		reconciling = StatusFalse
	}
	if stalled == "" {
		stalled = StatusFalse
	}
	SetCondition(conds, TypeStalled, stalled, o.reason, o.stalledMessage, generation)
	SetCondition(conds, TypeReconciling, reconciling, o.reason, o.reconcilingMessage, generation)
	SetCondition(conds, TypeReady, o.ready, o.reason, o.readyMessage, generation)
}
