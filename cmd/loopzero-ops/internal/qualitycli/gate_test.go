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
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// closedPipe refuses every write, like stdout piped into a reader that exited.
type closedPipe struct{}

func (closedPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// fixedResult is a check outcome that amounts to a fixed verdict.
type fixedResult verdict.Verdict

func (r fixedResult) Verdict() verdict.Verdict { return verdict.Verdict(r) }

// pointerResult is an outcome with a pointer receiver, the shape of every
// check that returns *Outcome.
type pointerResult struct{ v verdict.Verdict }

func (r *pointerResult) Verdict() verdict.Verdict { return r.v }

// gateReturning is a gate outside the registry whose check prints one line and
// returns the given result and error.
func gateReturning(res verdict.Result, err error) Gate {
	return Gate{
		Name:    "reporting",
		Summary: "prints its verdict",
		NoRoot:  true,
		Setup: func(*flag.FlagSet) Check {
			return func(inv Invocation) (verdict.Result, error) {
				inv.Stdout.Println("verdict")
				return res, err
			}
		},
	}
}

func run(g Gate) (rc int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	rc = g.Run(nil, &out, &errOut)
	return rc, out.String(), errOut.String()
}

func TestGateRun_APassWhoseReportCannotBeWrittenIsIncomplete(t *testing.T) {
	var stderr bytes.Buffer

	rc := gateReturning(fixedResult(verdict.Pass), nil).Run(nil, closedPipe{}, &stderr)

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d — a PASS nobody received must not exit 0", rc, exitcode.Incomplete)
	}
	if !strings.Contains(stderr.String(), "broken pipe") {
		t.Fatalf("stderr = %q, want the write error named", stderr.String())
	}
}

func TestGateRun_AViolationKeepsItsStatusWhenTheReportCannotBeWritten(t *testing.T) {
	rc := gateReturning(fixedResult(verdict.Fail), nil).Run(nil, closedPipe{}, &bytes.Buffer{})

	if rc != exitcode.Failure {
		t.Fatalf("exit = %d, want %d", rc, exitcode.Failure)
	}
}

func TestGateRun_ADeliveredPassExitsOK(t *testing.T) {
	rc, stdout, _ := run(gateReturning(fixedResult(verdict.Pass), nil))

	if rc != exitcode.OK || stdout != "verdict\n" {
		t.Fatalf("exit = %d, stdout = %q; want 0 and the verdict line", rc, stdout)
	}
}

func TestGateRun_EachVerdictHasOneExitStatus(t *testing.T) {
	cases := []struct {
		v    verdict.Verdict
		want int
	}{
		{verdict.Pass, exitcode.OK},
		{verdict.Fail, exitcode.Failure},
		{verdict.Skip, exitcode.OK},
		{verdict.NotVerified, exitcode.Incomplete},
	}
	for _, c := range cases {
		t.Run(c.v.String(), func(t *testing.T) {
			rc, _, stderr := run(gateReturning(fixedResult(c.v), nil))

			if rc != c.want {
				t.Fatalf("exit = %d, want %d", rc, c.want)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q; the runner adds nothing to a verdict the check reported", stderr)
			}
		})
	}
}

func TestGateRun_ACheckThatCouldNotMeasureIsIncomplete(t *testing.T) {
	rc, _, stderr := run(gateReturning(nil, errors.New("input unreadable")))

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d", rc, exitcode.Incomplete)
	}
	if want := "reporting: ERROR — could not complete the check: input unreadable\n"; stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestGateRun_AnErrorOutranksAPassingResult(t *testing.T) {
	rc, _, _ := run(gateReturning(fixedResult(verdict.Pass), errors.New("tool crashed")))

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d — a check that could not measure never passes", rc, exitcode.Incomplete)
	}
}

func TestGateRun_AnUndecidedVerdictIsIncomplete(t *testing.T) {
	rc, _, stderr := run(gateReturning(fixedResult(0), nil))

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d — an outcome that never decided must not pass", rc, exitcode.Incomplete)
	}
	if !strings.Contains(stderr, "no verdict") {
		t.Fatalf("stderr = %q, want the missing verdict named", stderr)
	}
}

func TestGateRun_ANilResultIsIncomplete(t *testing.T) {
	rc, _, stderr := run(gateReturning(nil, nil))

	if rc != exitcode.Incomplete || !strings.Contains(stderr, "no result") {
		t.Fatalf("exit = %d, stderr = %q; want %d and the missing result named", rc, stderr, exitcode.Incomplete)
	}
}

func TestGateRun_ANilPointerResultIsIncompleteNotACrash(t *testing.T) {
	var res *pointerResult

	rc, _, stderr := run(gateReturning(res, nil))

	if rc != exitcode.Incomplete || !strings.Contains(stderr, "no result") {
		t.Fatalf("exit = %d, stderr = %q; want %d and the missing result named", rc, stderr, exitcode.Incomplete)
	}
}

func TestGateRun_APanickingCheckIsIncompleteNotExit2(t *testing.T) {
	g := Gate{
		Name: "crashing", Summary: "panics", NoRoot: true,
		Setup: func(*flag.FlagSet) Check {
			return func(Invocation) (verdict.Result, error) {
				var m map[string]int
				m["boom"]++ // a nil-map write: a runtime panic
				return nil, nil
			}
		},
	}

	rc, _, stderr := run(g)

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d — a crashed check reached no verdict", rc, exitcode.Incomplete)
	}
	if !strings.Contains(stderr, "crashing: ERROR — the check panicked") {
		t.Fatalf("stderr = %q, want the panic named", stderr)
	}
}

func TestGateRun_AUsageErrorExitsUsage(t *testing.T) {
	rc, _, stderr := run(gateReturning(nil, usageError("bad --mode %q", "x")))

	if rc != exitcode.Usage || !strings.Contains(stderr, `bad --mode "x"`) {
		t.Fatalf("exit = %d, stderr = %q; want %d and the message", rc, stderr, exitcode.Usage)
	}
}
