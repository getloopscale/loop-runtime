// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package load

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFanOutReturnsResultsAlignedWithTheirIndex(t *testing.T) {
	got := fanOut(5, fanOutOptions{concurrency: 2}, func(i int) int { return i * 10 })

	for i, v := range got {
		if v != i*10 {
			t.Fatalf("result[%d] = %d, want %d (results must stay index-aligned)", i, v, i*10)
		}
	}
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
}

func TestFanOutNeverExceedsTheConcurrencyBound(t *testing.T) {
	var inFlight, peak, entered atomic.Int32
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)

	done := make(chan struct{})
	go func() {
		defer close(done)
		fanOut(6, fanOutOptions{concurrency: 2}, func(i int) int {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			// The first two to enter hold their slots until released, so the
			// other four can only run if the bound is broken.
			if entered.Add(1) <= 2 {
				started.Done()
			}
			<-release
			inFlight.Add(-1)
			return i
		})
	}()
	started.Wait()
	close(release)
	<-done

	if peak.Load() > 2 {
		t.Fatalf("peak in-flight = %d, want <= 2", peak.Load())
	}
}

func TestFanOutPacesArrivalsBetweenStartsOnly(t *testing.T) {
	var gaps []time.Duration
	opts := fanOutOptions{
		concurrency: 4,
		arrivalGap:  30 * time.Second,
		sleep:       func(d time.Duration) { gaps = append(gaps, d) },
	}

	fanOut(3, opts, func(i int) int { return i })

	if len(gaps) != 2 || gaps[0] != 30*time.Second {
		t.Fatalf("gaps = %v, want two 30s pauses (between three starts, none after the last)", gaps)
	}
}

func TestTallyDeploysCountsSuccessFailureAndLiveApps(t *testing.T) {
	results := []FullDeployResult{
		{Success: true, AppLive: true},
		{Success: true},
		{Success: false},
	}

	got := tallyDeploys(results)

	if got.Succeeded != 2 || got.Failed != 1 || got.Live != 1 {
		t.Fatalf("tally = %+v, want succeeded=2 failed=1 live=1", got)
	}
}

func TestSummarizeSimulatedDeploysCountsAndTakesPercentiles(t *testing.T) {
	results := []DeployResult{
		{Success: true, Latency: StageLatency{Total: 1 * time.Second, Upload: 100 * time.Millisecond}},
		{Success: false, Latency: StageLatency{Total: 3 * time.Second, Upload: 300 * time.Millisecond}},
		{Success: true, Latency: StageLatency{Total: 2 * time.Second, Upload: 200 * time.Millisecond}},
	}

	got := summarizeSimulatedDeploys(results, time.Minute)

	if got.TotalDeploys != 3 || got.SuccessCount != 2 || got.FailureCount != 1 {
		t.Fatalf("counts = %d/%d/%d, want 3 total, 2 ok, 1 failed", got.TotalDeploys, got.SuccessCount, got.FailureCount)
	}
	if got.DeployLatencyP50 != 2*time.Second || got.DeployLatencyP99 != 3*time.Second || got.UploadLatencyP50 != 200*time.Millisecond {
		t.Fatalf("percentiles = p50 %s p99 %s upload-p50 %s", got.DeployLatencyP50, got.DeployLatencyP99, got.UploadLatencyP50)
	}
	if got.Duration != time.Minute || len(got.Results) != 3 {
		t.Fatalf("duration=%s results=%d", got.Duration, len(got.Results))
	}
}
