// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T4.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md —
// property-based extension for LoopZeroError invariants. Closes M04 LOW
// gap (property-based testing escopo limitado a naming + statemachine).

package errors

import (
	"errors"
	"strings"
	"testing"
	"testing/quick"
)

// TestProperty_LoopZeroErrorErrorContainsCode verifies that Error()
// output ALWAYS embeds the code field — downstream log parsers and
// HTTP transports rely on this (pkg/errors/http extracts code from
// the error chain).
func TestProperty_LoopZeroErrorErrorContainsCode(t *testing.T) {
	f := func(code, message string) bool {
		if code == "" {
			return true // empty code is degenerate; caller must validate
		}
		err := NewLoopZeroError(code, message)
		return strings.Contains(err.Error(), code)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("Error() must contain code: %v", err)
	}
}

// TestProperty_LoopZeroErrorIsReflexive — every LoopZeroError MUST report
// itself as matching itself via errors.Is. Catches regressions where
// the Is implementation drifts from comparing Code.
func TestProperty_LoopZeroErrorIsReflexive(t *testing.T) {
	f := func(code, message string) bool {
		err := NewLoopZeroError(code, message)
		return errors.Is(err, err)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("errors.Is(err, err) must be true: %v", err)
	}
}

// TestProperty_LoopZeroErrorIsMatchesByCode — TWO LoopZeroErrors with the
// SAME code MUST match via errors.Is regardless of message
// differences. This is the sentinel pattern callers rely on
// (errors.Is(err, ErrSentinel) for typed-error checks).
func TestProperty_LoopZeroErrorIsMatchesByCode(t *testing.T) {
	f := func(code, msg1, msg2 string) bool {
		if code == "" {
			return true
		}
		a := NewLoopZeroError(code, msg1)
		b := NewLoopZeroError(code, msg2)
		return errors.Is(a, b) && errors.Is(b, a)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("errors.Is must match by code: %v", err)
	}
}

// TestProperty_LoopZeroErrorIsDifferentCodes — different codes MUST NOT
// match via errors.Is. Catches accidental "always-true" Is impl.
func TestProperty_LoopZeroErrorIsDifferentCodes(t *testing.T) {
	f := func(code1, code2, msg string) bool {
		if code1 == "" || code2 == "" || code1 == code2 {
			return true
		}
		a := NewLoopZeroError(code1, msg)
		b := NewLoopZeroError(code2, msg)
		return !errors.Is(a, b)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("errors.Is must distinguish codes: %v", err)
	}
}

// TestProperty_WrapPreservesCode — Wrap MUST preserve the original
// Code field. Wrapped errors must remain identifiable by their
// sentinel code at any depth.
func TestProperty_WrapPreservesCode(t *testing.T) {
	f := func(code, message, context string) bool {
		if code == "" {
			return true
		}
		original := NewLoopZeroError(code, message)
		wrapped := original.Wrap(context)
		return wrapped.Code == code
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("Wrap must preserve code: %v", err)
	}
}

// TestProperty_WrapWithStackPreservesIs — wrapping must NOT break
// errors.Is — chain walk must reach the original.
func TestProperty_WrapWithStackPreservesIs(t *testing.T) {
	f := func(code, message, context string) bool {
		if code == "" {
			return true
		}
		original := NewLoopZeroError(code, message)
		wrapped := original.WrapWithStack(context)
		return errors.Is(wrapped, original)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("WrapWithStack must preserve errors.Is chain: %v", err)
	}
}

// TestProperty_ToJSONHasCodeAndMessage — JSON serialization MUST
// always include code + message keys. Transport contracts rely on this.
func TestProperty_ToJSONHasCodeAndMessage(t *testing.T) {
	f := func(code, message string) bool {
		err := NewLoopZeroError(code, message)
		m := err.ToJSON()
		_, hasCode := m["code"]
		_, hasMessage := m["message"]
		return hasCode && hasMessage
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatalf("ToJSON must include code+message: %v", err)
	}
}

// TestProperty_DeepWrapChainPreservesCode — wrap N times, code at
// the bottom must still be retrievable. Bounded depth (32 max per
// stackDepth cap) prevents stack overflow.
func TestProperty_DeepWrapChainPreservesCode(t *testing.T) {
	f := func(code string, depth uint8) bool {
		if code == "" {
			return true
		}
		// Cap depth at 30 to stay below stackDepth=32 + leave room for runtime frames.
		d := int(depth%30) + 1
		original := NewLoopZeroError(code, "base")
		current := original
		for i := 0; i < d; i++ {
			current = current.Wrap("layer")
		}
		return current.Code == code && errors.Is(current, original)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatalf("deep wrap chain must preserve code: %v", err)
	}
}
