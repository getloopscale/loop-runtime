// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"time"
)

// pollUntilTerminal calls emit at once and then every interval, until emit
// reports a terminal event, returns an error, or ctx ends. It is the one poll
// loop every polling source reader shares.
func pollUntilTerminal(ctx context.Context, interval time.Duration, emit func() (terminal bool, err error)) error {
	if terminal, err := emit(); err != nil || terminal {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if terminal, err := emit(); err != nil || terminal {
				return err
			}
		}
	}
}
