// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vault

import (
	"bytes"
	"testing"
)

// TestZeroize_100ByteBuffer — plan T3.1 acceptance: full byte clearance.
func TestZeroize_100ByteBuffer(t *testing.T) {
	buf := bytes.Repeat([]byte{0xff}, 100)
	Zeroize(buf)
	expected := make([]byte, 100)
	if !bytes.Equal(buf, expected) {
		t.Errorf("100-byte buffer not zeroized: got %v want all-zero", buf)
	}
}

// TestZeroize_NonAlignedLengths — plan T3.1 acceptance: lengths 1, 7, 13
// (non-power-of-two, non-aligned to common SIMD widths) MUST also zero
// every byte. Defense against compiler/runtime length-rounding hiccups.
func TestZeroize_NonAlignedLengths(t *testing.T) {
	for _, n := range []int{1, 7, 13} {
		buf := bytes.Repeat([]byte{0xaa}, n)
		Zeroize(buf)
		expected := make([]byte, n)
		if !bytes.Equal(buf, expected) {
			t.Errorf("length=%d not zeroized: got %v want all-zero", n, buf)
		}
	}
}

// TestZeroize_EmptySlice — defensive: zero-length slice is a no-op + does
// not panic. Caller may pass an empty buffer (e.g., decrypt result before
// any bytes read).
func TestZeroize_EmptySlice(t *testing.T) {
	var buf []byte
	Zeroize(buf) // must not panic
	if len(buf) != 0 {
		t.Errorf("empty slice should stay empty: got len=%d", len(buf))
	}
}

// TestZeroize_NilSlice — defensive: nil slice is a no-op + does not panic.
func TestZeroize_NilSlice(t *testing.T) {
	Zeroize(nil) // must not panic
}

// TestZeroize_SliceWithSpareCap — only the visible len(buf) bytes are
// zeroized; bytes beyond len but within cap are NOT touched. This is the
// documented semantic: callers MUST pass the exact buffer they want
// cleared. A buf[:n] sub-slice will only clear n bytes — the underlying
// array's tail stays as-is.
//
// This semantic is intentional: if the caller wanted the full cap zeroed,
// they would pass buf[:cap(buf)] explicitly. Zeroing beyond len would be a
// surprise for callers using sub-slices of larger buffers.
func TestZeroize_SliceWithSpareCap(t *testing.T) {
	backing := bytes.Repeat([]byte{0xff}, 20)
	subSlice := backing[:5] // len=5, cap=20

	Zeroize(subSlice)

	// First 5 bytes zeroed
	for i := 0; i < 5; i++ {
		if backing[i] != 0 {
			t.Errorf("backing[%d]: want 0, got %x", i, backing[i])
		}
	}
	// Bytes 5..19 untouched (still 0xff)
	for i := 5; i < 20; i++ {
		if backing[i] != 0xff {
			t.Errorf("backing[%d] (beyond len): want 0xff, got %x", i, backing[i])
		}
	}
}
