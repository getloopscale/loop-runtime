// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package charts holds the chart render tests and the classifiers they use.
//
// The package comment lives HERE, not in a `_test.go`, because this is the
// package's first non-test file — while it held only tests, ST1000 never fired.
package charts

import "strings"

// ClassifyRenderFailure says whether the output of a failed `helmfile` /
// `helm template` points at the NETWORK, and returns the text to append to the
// test's failure message.
//
// # Why this exists (B-156)
//
// `delivery-gate.sh` runs `task quality:all` before EVERY push and has no bypass.
// A render that contacts a remote repository turns network unavailability into a
// blocked delivery — and the operator's message is `helmfile template failed:
// exit status 1` followed by hundreds of lines. Measured 2026-08-19: one such
// failure took 123.89s and blocked a commit that only touched `CHANGELOG.md` and
// `BACKLOG.md`.
//
// The `loopruntime-cloud` case was fixed at the root — it renders a LOCAL chart, so
// `--skip-deps` removes the network (measured: 17 repo calls and 10s became zero
// and 0s). The `openbao` case cannot be: the chart comes from a remote
// repository, and with `--skip-deps` the render fails with `repo openbao not
// found`. For that one, what remains is the DoD's second branch — the output
// must say WHICH of the two things happened.
//
// # What it deliberately does NOT classify as network
//
// `repo <name> not found` is left out on purpose. That is an undeclared
// repository — configuration, not connectivity — and it is exactly what showed up
// when `--skip-deps` was applied in the wrong place. Calling it network would send
// the next person looking for a connection defect over a file error.
func ClassifyRenderFailure(output string) (network bool, explanation string) {
	signatures := []string{
		"dial tcp",
		"i/o timeout",
		"no such host",
		"connection refused",
		"TLS handshake timeout",
		"context deadline exceeded",
		"temporary failure in name resolution",
		"could not download",
		"failed to fetch",
	}
	lower := strings.ToLower(output)
	for _, s := range signatures {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true, "THE FAILURE IS NETWORK, not chart content: the output contains " +
				"`" + s + "`. This render contacts a remote repository, so network " +
				"unavailability fails the delivery gate while nothing in the repository is " +
				"wrong (B-156). Re-run with network, or skip with LOOPRUNTIME_TEST_SKIP_HELMFILE=1."
		}
	}
	return false, "The output carries no network signature — treat it as a CONTENT " +
		"failure and read the helm error above."
}
