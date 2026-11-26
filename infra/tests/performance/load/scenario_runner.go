// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package load

import (
	"context"
	"sync"
	"time"
)

// fanOutOptions bounds and paces a fan-out.
type fanOutOptions struct {
	// concurrency is the most work items in flight at once; < 1 means 1.
	concurrency int
	// arrivalGap pauses between starts to model a sustained arrival rate
	// rather than a burst. Zero launches everything at once.
	arrivalGap time.Duration
	// sleep is the pause used for arrivalGap; nil means time.Sleep. Injected so
	// the pacing is testable without waiting it out.
	sleep func(time.Duration)
}

// fanOut runs work(i) for every i in [0, n) with at most opts.concurrency in
// flight and returns the results index-aligned. It is the one copy of the
// semaphore + WaitGroup block the load scenarios used to re-type each time.
func fanOut[R any](n int, opts fanOutOptions, work func(i int) R) []R {
	concurrency := opts.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sleep := opts.sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	results := make([]R, n)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = work(idx)
		}(i)
		if opts.arrivalGap > 0 && i < n-1 {
			// intentional-sleep: load-tier pacing (sustained arrival, not burst)
			sleep(opts.arrivalGap)
		}
	}
	wg.Wait()
	return results
}

// DeployTally counts the outcomes of a set of real deploys.
type DeployTally struct {
	Succeeded int64
	Failed    int64
	// Live counts deploys whose app answered HTTP 200 after the build.
	Live int64
}

func tallyDeploys(results []FullDeployResult) DeployTally {
	var tally DeployTally
	for _, r := range results {
		if r.Success {
			tally.Succeeded++
		} else {
			tally.Failed++
		}
		if r.AppLive {
			tally.Live++
		}
	}
	return tally
}

// RunScenario deploys every identity through the full pipeline with bounded
// concurrency and optional arrival pacing. onResult, when set, is called as
// each deploy finishes (for progress logging); results come back
// index-aligned with identities, together with their tally.
func (h *RealDeployHarness) RunScenario(
	ctx context.Context,
	identities []TenantIdentity,
	opts fanOutOptions,
	onResult func(idx int, r FullDeployResult),
) ([]FullDeployResult, DeployTally) {
	results := fanOut(len(identities), opts, func(i int) FullDeployResult {
		r := h.RealDeploy(ctx, identities[i])
		if onResult != nil {
			onResult(i, r)
		}
		return r
	})
	return results, tallyDeploys(results)
}
