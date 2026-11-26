// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cliout

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/exitcode"
)

var errClosed = errors.New("closed pipe")

// brokenWriter fails every write, and counts how many it was asked for.
type brokenWriter struct{ calls int }

func (b *brokenWriter) Write([]byte) (int, error) {
	b.calls++
	return 0, errClosed
}

func TestWriterFormatsLikeFmt(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf)

	w.Printf("%s=%d\n", "a", 1)
	w.Println("b", 2)
	w.Print("c")

	if got, want := buf.String(), "a=1\nb 2\nc"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if w.Err() != nil {
		t.Fatalf("Err() = %v, want nil after successful writes", w.Err())
	}
}

func TestWriterKeepsTheFirstErrorAndStopsWriting(t *testing.T) {
	bw := &brokenWriter{}
	w := New(bw)

	w.Printf("one\n")
	w.Println("two")
	_, err := w.Write([]byte("three"))

	if !errors.Is(w.Err(), errClosed) {
		t.Fatalf("Err() = %v, want %v", w.Err(), errClosed)
	}
	if !errors.Is(err, errClosed) {
		t.Fatalf("Write after failure returned %v, want the recorded error", err)
	}
	if bw.calls != 1 {
		t.Fatalf("underlying writer called %d times, want 1 — writes after a failure must be dropped", bw.calls)
	}
}

func TestNewReturnsAnExistingWriterSoErrorsAreShared(t *testing.T) {
	outer := New(&brokenWriter{})

	New(outer).Println("lost")

	if !errors.Is(outer.Err(), errClosed) {
		t.Fatalf("outer Err() = %v, want the error recorded through the inner wrap", outer.Err())
	}
}

func TestSettleKeepsOKWhenEveryWriteArrived(t *testing.T) {
	var out, errOut bytes.Buffer

	if got := Settle(exitcode.OK, New(&out), New(&errOut)); got != exitcode.OK {
		t.Fatalf("Settle = %d, want %d", got, exitcode.OK)
	}
}

func TestSettleTurnsAnUndeliveredPassIntoIncomplete(t *testing.T) {
	var errOut bytes.Buffer
	out := New(&brokenWriter{})
	out.Println("PASS")

	got := Settle(exitcode.OK, out, New(&errOut))

	if got != exitcode.Incomplete {
		t.Fatalf("Settle = %d, want %d", got, exitcode.Incomplete)
	}
	if !strings.Contains(errOut.String(), "the report could not be written: closed pipe") {
		t.Fatalf("stderr = %q, want the write error named", errOut.String())
	}
}

func TestSettleKeepsAFailureStatus(t *testing.T) {
	var errOut bytes.Buffer
	out := New(&brokenWriter{})
	out.Println("FAIL")

	if got := Settle(exitcode.Failure, out, New(&errOut)); got != exitcode.Failure {
		t.Fatalf("Settle = %d, want %d — a violation must keep its own status", got, exitcode.Failure)
	}
}
