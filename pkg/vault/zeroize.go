// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package vault zeroize: T3.1 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2 — defense against heap dump
// + core dump exposure of plaintext secrets.
package vault

// Zeroize overwrites every byte of buf with 0x00.
//
// LIMITATION: this function operates ONLY on []byte slices. Go strings
// are immutable and cannot be zeroized — once plaintext lands in a
// string, it stays in the heap until the GC reclaims it (timing
// uncontrolled).
//
// To minimize heap exposure of secrets:
//   - Accept plaintext as []byte at API boundaries (not string)
//   - Read directly from io.Reader into a pre-allocated []byte
//   - Avoid json.Unmarshal of secret-bearing fields into string struct
//     tags
//   - When unavoidable, document the residual exposure in an ADR
//
// See edge-case EC-07 of plan 2026-06-10-j22-openbao-transit-eso-sync.
//
// Compiler will NOT elide the write because buf escapes to this function
// (verified via `go build -gcflags=-m ./pkg/vault/`).
//
// Signature accepts ONLY []byte by design — no string overload. A
// Zeroize(string) would have NO observable effect on the caller's
// string (Go strings are immutable; we would zero a local copy of the
// string header). FOOTGUN prevention.
func Zeroize(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}
