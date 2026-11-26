// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// canonical-tests-rewrite-v2 — Ginkgo suite bootstrap for the cleanroom LIVE
// chain. Without this RunSpecs entry point the `Describe` block(s) are never
// executed (`go test -tags=live` finds 0 specs → false-green, the "Suite
// skipped" trap in loopruntime/CLAUDE.md § Common Mistakes). Closes issue #236
// (mirrors the dogfood_suite_test.go fix in commit 7fa629b9).
//
// Runs ONLY under -tags=live against a live cluster:
//   go test -tags=live ./infra/tests/livetest/cleanroom/...

package cleanroom

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCleanroomLive(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cleanroom LIVE Suite (canonical-tests-rewrite-v2)")
}
