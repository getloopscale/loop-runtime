// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package baselinecompare

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pkgEntry is a tiny test fixture for writeBaseline.
type pkgEntry struct {
	Line float64
	Min  float64
}

// writeBaseline writes a JSON baseline fixture using the canonical
// `"line"` wire key (matches live coverage-baseline.json).
func writeBaseline(t *testing.T, name string, pkgs map[string]pkgEntry) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`{"packages":{`)
	first := true
	for pkg, p := range pkgs {
		if !first {
			sb.WriteString(",")
		}
		first = false
		fmt.Fprintf(&sb, `"%s":{"line":%g,"min_required":%g}`, pkg, p.Line, p.Min)
	}
	sb.WriteString("}}")
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o600))
	return path
}

// =============================================================================
// EC-2 escape valve
// =============================================================================

func TestRun_DeferLabelWithReason_PassDeferred(t *testing.T) {
	out, err := Run(Config{
		DeferLabelPresent: true,
		DeferReason:       "Coverage push planned next sprint",
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.True(t, out.Deferred)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "EC-2: coverage-baseline-defer label active")
	assert.Contains(t, joined, "Coverage push planned next sprint")
	assert.Contains(t, joined, "PASS — gate deferred")
}

func TestRun_DeferLabelWithoutReason_OpsUsage(t *testing.T) {
	_, err := Run(Config{DeferLabelPresent: true})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
	assert.Contains(t, te.Message, "EC-2 guard")
}

// =============================================================================
// Config validation
// =============================================================================

func TestRun_MissingBaselinePath_OpsUsage(t *testing.T) {
	_, err := Run(Config{CurrentPath: "/tmp/c.json"})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
	assert.Contains(t, te.Message, "BaselinePath")
}

func TestRun_MissingCurrentPath_OpsUsage(t *testing.T) {
	_, err := Run(Config{BaselinePath: "/tmp/b.json"})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Contains(t, te.Message, "CurrentPath")
}

func TestRun_BaselineFileMissing_OpsUsage(t *testing.T) {
	_, err := Run(Config{
		BaselinePath: "/totally/missing/baseline.json",
		CurrentPath:  "/tmp/c.json",
	})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestRun_CurrentFileMissing_OpsUsage(t *testing.T) {
	basePath := writeBaseline(t, "base.json", map[string]pkgEntry{
		"a": {Line: 80, Min: 70},
	})
	_, err := Run(Config{
		BaselinePath: basePath,
		CurrentPath:  "/totally/missing/current.json",
	})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

// =============================================================================
// Happy path — all packages above floor
// =============================================================================

func TestRun_HappyPath_AllPackagesAboveFloor_Pass(t *testing.T) {
	basePath := writeBaseline(t, "base.json", map[string]pkgEntry{
		"a": {Line: 90, Min: 80},
		"b": {Line: 75, Min: 70},
	})
	curPath := writeBaseline(t, "cur.json", map[string]pkgEntry{
		"a": {Line: 89, Min: 80}, // -1pp from baseline, within 3pp slack
		"b": {Line: 76, Min: 70}, // up from baseline → trivially passes
	})

	out, err := Run(Config{BaselinePath: basePath, CurrentPath: curPath})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.False(t, out.Deferred)
	assert.Empty(t, out.Violations)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "PASS — all 2 packages above floor")
}

// =============================================================================
// Regression — existing package drops below floor
// =============================================================================

func TestRun_RegressionExistingPackage_Fail(t *testing.T) {
	// baseline=95, min=80, slack=3 → floor = max(80, 92) = 92.
	// current=85 → 85 < 92 → violation.
	basePath := writeBaseline(t, "base.json", map[string]pkgEntry{
		"pkg/x": {Line: 95, Min: 80},
	})
	curPath := writeBaseline(t, "cur.json", map[string]pkgEntry{
		"pkg/x": {Line: 85, Min: 80},
	})

	out, err := Run(Config{BaselinePath: basePath, CurrentPath: curPath})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	require.Len(t, out.Violations, 1)
	assert.Equal(t, "pkg/x", out.Violations[0].Package)

	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "FAIL — 1 package(s) below floor")
	assert.Contains(t, joined, "pkg/x: current=85.0% < floor=92.0%")
	assert.Contains(t, joined, "min_required=80")
	assert.Contains(t, joined, "baseline=95.0")
}

// =============================================================================
// New package — uses DefaultMin floor without slack
// =============================================================================

func TestRun_NewPackageBelowDefault_Fail_AnnotatedAsNew(t *testing.T) {
	basePath := writeBaseline(t, "base.json", map[string]pkgEntry{})
	curPath := writeBaseline(t, "cur.json", map[string]pkgEntry{
		"pkg/new": {Line: 50, Min: 80},
	})
	out, err := Run(Config{
		BaselinePath: basePath, CurrentPath: curPath,
		DefaultMin: 80, SlackPP: 3,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	require.Len(t, out.Violations, 1)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "(NEW package, no baseline)",
		"new pkg annotation surfaces in diagnostic")
	assert.Contains(t, joined, "baseline=N/A")
}

// =============================================================================
// WriteMessages
// =============================================================================

func TestOutcome_WriteMessages(t *testing.T) {
	o := &Outcome{Messages: []string{"line1", "line2", "line3"}}
	var buf bytes.Buffer
	require.NoError(t, o.WriteMessages(&buf))
	assert.Equal(t, "line1\nline2\nline3\n", buf.String())
}

func TestOutcome_WriteMessages_Empty(t *testing.T) {
	o := &Outcome{}
	var buf bytes.Buffer
	require.NoError(t, o.WriteMessages(&buf))
	assert.Empty(t, buf.String())
}

// =============================================================================
// SlackPP default behaviour
// =============================================================================

func TestRun_DefaultSlackUsedInMessage(t *testing.T) {
	basePath := writeBaseline(t, "base.json", map[string]pkgEntry{
		"a": {Line: 80, Min: 70},
	})
	curPath := writeBaseline(t, "cur.json", map[string]pkgEntry{
		"a": {Line: 80, Min: 70},
	})
	// SlackPP omitted → defaults to 3.
	out, err := Run(Config{BaselinePath: basePath, CurrentPath: curPath})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "slack=3.0pp")
}
