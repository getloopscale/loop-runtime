// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package cliout is how a loopzero-ops command writes its report: every line goes
// through a Writer that keeps the first write error, and the command settles
// its exit status against those errors once, at the end.
//
// Checking the error of every `fmt.Fprintf` inline turns a report into more
// error handling than report, and ignoring them lets a gate print PASS into a
// closed pipe and exit 0 — a verdict nobody received read as a verdict
// delivered. The sticky writer is the middle path: writing stays one call per
// line, and a report that did not reach its reader still changes the outcome.
package cliout

import (
	"errors"
	"fmt"
	"io"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/exitcode"
)

// Writer writes to an underlying io.Writer and remembers the first error.
// After an error every further write is dropped: the output is already
// incomplete, and writing past the gap would only make it misleading.
type Writer struct {
	w   io.Writer
	err error
}

// New wraps w. A *Writer is returned as is, so a function handed one by its
// caller shares the caller's error state instead of hiding it in a copy.
func New(w io.Writer) *Writer {
	if cw, ok := w.(*Writer); ok {
		return cw
	}
	return &Writer{w: w}
}

// Write implements io.Writer, so a Writer can be handed to an encoder or any
// other io.Writer consumer and still record what failed.
func (w *Writer) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	w.record(err)
	return n, err
}

// Printf formats like fmt.Printf.
func (w *Writer) Printf(format string, a ...any) {
	if w.err == nil {
		_, err := fmt.Fprintf(w.w, format, a...)
		w.record(err)
	}
}

// Println formats like fmt.Println.
func (w *Writer) Println(a ...any) {
	if w.err == nil {
		_, err := fmt.Fprintln(w.w, a...)
		w.record(err)
	}
}

// Print formats like fmt.Print.
func (w *Writer) Print(a ...any) {
	if w.err == nil {
		_, err := fmt.Fprint(w.w, a...)
		w.record(err)
	}
}

// Err is the first write error, or nil when everything written arrived.
func (w *Writer) Err() error { return w.err }

func (w *Writer) record(err error) {
	if err != nil && w.err == nil {
		w.err = err
	}
}

// Settle returns the exit status a command should end with, given the status
// it computed and the writers its report went through.
//
// A command that failed keeps its status: it is already non-zero, and the more
// specific status says more than "the output broke". A command that succeeded
// but could not deliver its report is Incomplete — a PASS nobody could read
// must not exit 0. The cause is reported, best effort, on the last writer
// (conventionally stderr); if that writer is the broken one, the exit status
// is the report.
func Settle(code int, writers ...*Writer) int {
	errs := make([]error, 0, len(writers))
	for _, w := range writers {
		errs = append(errs, w.Err())
	}
	err := errors.Join(errs...)
	if err == nil || code != exitcode.OK {
		return code
	}
	if len(writers) > 0 {
		writers[len(writers)-1].Printf("ERROR — the report could not be written: %v\n", err)
	}
	return exitcode.Incomplete
}
