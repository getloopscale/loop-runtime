// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exitcode"
)

// closedPipe refuses every write, like stdout piped into a reader that exited.
type closedPipe struct{}

func (closedPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// reportingGate is a gate outside the registry whose check prints one line and
// returns the given outcome.
func reportingGate(outcome Outcome) Gate {
	return Gate{
		Name:    "reporting",
		Summary: "prints its verdict",
		NoRoot:  true,
		Setup: func(*flag.FlagSet) Check {
			return func(inv Invocation) (Outcome, error) {
				inv.Stdout.Println("verdict")
				return outcome, nil
			}
		},
	}
}

func TestGateRun_APassWhoseReportCannotBeWrittenIsIncomplete(t *testing.T) {
	var stderr bytes.Buffer

	rc := reportingGate(Pass).Run(nil, closedPipe{}, &stderr)

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d — a PASS nobody received must not exit 0", rc, exitcode.Incomplete)
	}
	if !strings.Contains(stderr.String(), "broken pipe") {
		t.Fatalf("stderr = %q, want the write error named", stderr.String())
	}
}

func TestGateRun_AViolationKeepsItsStatusWhenTheReportCannotBeWritten(t *testing.T) {
	rc := reportingGate(Fail).Run(nil, closedPipe{}, &bytes.Buffer{})

	if rc != exitcode.Failure {
		t.Fatalf("exit = %d, want %d", rc, exitcode.Failure)
	}
}

func TestGateRun_ADeliveredPassExitsOK(t *testing.T) {
	var stdout bytes.Buffer

	rc := reportingGate(Pass).Run(nil, &stdout, &bytes.Buffer{})

	if rc != exitcode.OK || stdout.String() != "verdict\n" {
		t.Fatalf("exit = %d, stdout = %q; want 0 and the verdict line", rc, stdout.String())
	}
}
