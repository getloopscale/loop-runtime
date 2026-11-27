// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package capacity

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Hermetic tests for the open-loop runner every capacity bench shares. They run
// without LOOP_RUNTIME_CAPACITY: the request is a fake, so nothing leaves the process.

func shortRun() Config {
	return Config{RPS: 1000, Duration: 40 * time.Millisecond, Workers: 4}
}

func TestRunOpenLoopCountsEveryRequestItIssued(t *testing.T) {
	var calls atomic.Int64

	res, _ := RunOpenLoop(context.Background(), shortRun(), "fake", ServerErrors,
		func(context.Context, int64) (int, error) { calls.Add(1); return http.StatusOK, nil })

	if res.TotalReqs == 0 || res.TotalReqs != calls.Load() {
		t.Fatalf("TotalReqs = %d, calls = %d; want equal and non-zero", res.TotalReqs, calls.Load())
	}
	if res.Errors != 0 || res.ErrorRate != 0 {
		t.Fatalf("errors = %d rate = %v, want none for all-200 responses", res.Errors, res.ErrorRate)
	}
}

func TestRunOpenLoopCountsTransportFailuresAndClassifiedStatuses(t *testing.T) {
	var calls atomic.Int64

	res, statuses := RunOpenLoop(context.Background(), shortRun(), "fake", ServerErrors,
		func(context.Context, int64) (int, error) {
			switch calls.Add(1) % 3 {
			case 0:
				return 0, errors.New("connection reset")
			case 1:
				return http.StatusBadGateway, nil
			default:
				return http.StatusNotFound, nil // a 4xx is not a server error here
			}
		})

	wantErrors := statuses[http.StatusBadGateway] + (res.TotalReqs - statuses[http.StatusBadGateway] - statuses[http.StatusNotFound])
	if res.Errors != wantErrors || res.Errors == 0 {
		t.Fatalf("Errors = %d, want %d (transport failures + 5xx, not 4xx); statuses=%v", res.Errors, wantErrors, statuses)
	}
}

func TestRunOpenLoopNeverExceedsTheWorkerBound(t *testing.T) {
	var inFlight, peak atomic.Int32
	cfg := shortRun()

	RunOpenLoop(context.Background(), cfg, "fake", ServerErrors, func(context.Context, int64) (int, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond) // intentional-sleep: hold the slot so overlap is observable
		inFlight.Add(-1)
		return http.StatusOK, nil
	})

	if int(peak.Load()) > cfg.Workers {
		t.Fatalf("peak in flight = %d, want <= %d workers", peak.Load(), cfg.Workers)
	}
	if inFlight.Load() != 0 {
		t.Fatalf("%d requests still in flight after RunOpenLoop returned", inFlight.Load())
	}
}

func TestRunOpenLoopReportsZeroErrorRateWhenNothingRan(t *testing.T) {
	cfg := Config{RPS: 1, Duration: 0, Workers: 1}

	res, _ := RunOpenLoop(context.Background(), cfg, "fake", ServerErrors,
		func(context.Context, int64) (int, error) { return http.StatusOK, nil })

	if res.ErrorRate != 0 {
		t.Fatalf("ErrorRate = %v, want 0 (not NaN) when no request ran", res.ErrorRate)
	}
}

// Record is called from every request goroutine at once; it used to append to
// a shared slice with no lock ("race tolerable"), which the race detector
// reports and which can lose or tear samples.
func TestLatenciesRecordIsSafeForConcurrentUse(t *testing.T) {
	lat := NewLatencies(1000)
	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(d int) {
			defer wg.Done()
			lat.Record(time.Duration(d) * time.Millisecond)
		}(i)
	}
	wg.Wait()

	if got := lat.Count(); got != 500 {
		t.Fatalf("Count = %d, want 500", got)
	}
}

func TestLatenciesDropsSamplesPastItsCapacity(t *testing.T) {
	lat := NewLatencies(2)
	for i := 0; i < 5; i++ {
		lat.Record(time.Millisecond)
	}
	if got := lat.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2 (capacity)", got)
	}
}
