// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdiffstructural

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// When the unified diff cannot be computed, STRUCT-3 (the security-context
// downgrade class) was never evaluated. The gate must fail loudly rather
// than report "NO structural risks detected" over a check that did not run.
func TestCheck_DiffFailure_FailsInsteadOfSkippingSecurityChecks(t *testing.T) {
	bp, pp := writePair(t, "kind: Deployment\nrunAsNonRoot: true\n", "kind: Deployment\nrunAsNonRoot: false\n")
	orig := computeDiff
	computeDiff = func(string, string) (string, error) { return "", errors.New("diff: executable file not found") }
	t.Cleanup(func() { computeDiff = orig })

	out, err := Check(Config{Chart: "demo", BasePath: bp, PRPath: pp})
	require.Error(t, err)
	assert.Nil(t, out)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Contains(t, te.Message, "STRUCT-3")
}
