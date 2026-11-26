// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package circuitbreaker_test

// M3 (plan m3-extract-loopruntime-contracts) — engine-side compile-time guard that the
// concrete *circuitbreaker.Breaker keeps satisfying the consumer-defined
// jwt.Breaker interface (DIP). This assertion lives HERE, where both types are
// visible, because the jwt package was extracted to the standalone
// github.com/getloopscale/contracts module and MUST NOT import the
// engine-internal circuitbreaker package (that would re-couple the contracts
// base to the engine). jwt's own side of the Breaker contract is proven in
// loopruntime-contracts/jwt with a fake double; circuitbreaker's tripping behavior is
// proven by its own tests in this package. This file closes the loop: the real
// breaker fits the extracted contract.

import (
	"github.com/getloopscale/contracts/jwt"
	"github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

// Compile-time guard: if a future signature drift breaks structural
// satisfaction, the build fails here — not in production.
var _ jwt.Breaker = (*circuitbreaker.Breaker)(nil)
