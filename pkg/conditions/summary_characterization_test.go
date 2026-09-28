// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import "testing"

// summaryTriple is what SetReadinessSummary writes for one summary type.
type summaryTriple struct {
	status  Status
	reason  string
	message string
}

// TestSummary_WritesExactlyTheseTriples pins the full output of every
// branch of the cascade — status, reason and message of all three summary
// conditions — so the algorithm can be restructured without changing what a
// kstatus consumer or a person reading `kubectl describe` sees.
func TestSummary_WritesExactlyTheseTriples(t *testing.T) {
	cases := []struct {
		name    string
		details []Condition
		types   []string
		want    map[string]summaryTriple
	}{
		{
			name:  "no details required",
			types: nil,
			want: map[string]summaryTriple{
				TypeReady:       {StatusTrue, "NoDetailsRequired", "no detail conditions configured for this resource"},
				TypeReconciling: {StatusFalse, "NoDetailsRequired", ""},
				TypeStalled:     {StatusFalse, "NoDetailsRequired", ""},
			},
		},
		{
			name:    "all true, including an unstamped generation",
			details: []Condition{detail("A", StatusTrue, ReasonReady, 3), detail("B", StatusTrue, ReasonReady, 0)},
			types:   []string{"A", "B"},
			want: map[string]summaryTriple{
				TypeReady:       {StatusTrue, ReasonReady, "all detail conditions satisfied"},
				TypeReconciling: {StatusFalse, ReasonReady, ""},
				TypeStalled:     {StatusFalse, ReasonReady, ""},
			},
		},
		{
			name: "every stalled detail is named, in declaration order",
			details: []Condition{
				detail("A", StatusFalse, ReasonDegraded, 3),
				detail("B", StatusFalse, "Pending", 3),
				detail("C", StatusFalse, ReasonFailed, 3),
			},
			types: []string{"A", "B", "C"},
			want: map[string]summaryTriple{
				TypeReady:       {StatusFalse, ReasonStalled, "halted: A=Degraded, C=Failed"},
				TypeReconciling: {StatusFalse, ReasonStalled, "halted: A=Degraded, C=Failed"},
				TypeStalled:     {StatusTrue, ReasonStalled, "A=Degraded, C=Failed"},
			},
		},
		{
			name:    "missing details win the message over a mismatch and a pending detail",
			details: []Condition{detail("A", StatusTrue, ReasonReady, 2), detail("C", StatusFalse, "Pending", 3)},
			types:   []string{"A", "B", "C", "D"},
			want: map[string]summaryTriple{
				TypeReady:       {StatusFalse, ReasonReconciling, "awaiting: B, D"},
				TypeReconciling: {StatusTrue, ReasonReconciling, "awaiting: B, D"},
				TypeStalled:     {StatusFalse, ReasonReconciling, ""},
			},
		},
		{
			name:    "a generation mismatch wins the message over a pending detail",
			details: []Condition{detail("A", StatusFalse, "Pending", 3), detail("B", StatusTrue, ReasonReady, 2)},
			types:   []string{"A", "B"},
			want: map[string]summaryTriple{
				TypeReady:       {StatusFalse, ReasonReconciling, "spec changed; controller catching up"},
				TypeReconciling: {StatusTrue, ReasonReconciling, "spec changed; controller catching up"},
				TypeStalled:     {StatusFalse, ReasonReconciling, ""},
			},
		},
		{
			name:    "the first in-progress detail is the one named",
			details: []Condition{detail("A", StatusUnknown, "", 3), detail("B", StatusFalse, "Pending", 3)},
			types:   []string{"A", "B"},
			want: map[string]summaryTriple{
				TypeReady:       {StatusFalse, ReasonReconciling, "in progress: A=Unknown"},
				TypeReconciling: {StatusTrue, ReasonReconciling, "in progress: A=Unknown"},
				TypeStalled:     {StatusFalse, ReasonReconciling, ""},
			},
		},
		{
			name:    "a non-stalled False reason is named with its reason",
			details: []Condition{detail("A", StatusFalse, "Pending", 3), detail("B", Status("Weird"), "", 3)},
			types:   []string{"A", "B"},
			want: map[string]summaryTriple{
				TypeReady:       {StatusFalse, ReasonReconciling, "in progress: A=Pending"},
				TypeReconciling: {StatusTrue, ReasonReconciling, "in progress: A=Pending"},
				TypeStalled:     {StatusFalse, ReasonReconciling, ""},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(withFixedClock(2026))
			conds := append([]Condition(nil), tc.details...)

			SetReadinessSummary(&conds, 3, tc.types)

			for typ, want := range tc.want {
				got := GetCondition(conds, typ)
				if got == nil {
					t.Fatalf("%s not written", typ)
				}
				if got.Status != want.status || got.Reason != want.reason || got.Message != want.message {
					t.Errorf("%s = (%s, %q, %q), want (%s, %q, %q)",
						typ, got.Status, got.Reason, got.Message, want.status, want.reason, want.message)
				}
				if got.ObservedGeneration != 3 {
					t.Errorf("%s observedGeneration = %d, want 3", typ, got.ObservedGeneration)
				}
			}
		})
	}
}
