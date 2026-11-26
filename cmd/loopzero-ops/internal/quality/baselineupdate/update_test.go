// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package baselineupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeGoProfile writes a Go coverage profile fixture.
func writeGoProfile(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage.out")
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))
	return path
}

// writeLCOV writes an lcov fixture.
func writeLCOV(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lcov.info")
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))
	return path
}

// fixedClock returns a deterministic timestamp source.
func fixedClock(ts string) func() time.Time {
	parsed, _ := time.Parse(time.RFC3339, ts)
	return func() time.Time { return parsed }
}

// =============================================================================
// Validation
// =============================================================================

func TestRun_MissingBaselinePath_OpsUsage(t *testing.T) {
	_, err := Run(Config{})
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestRun_BothProfilesEmpty_OpsPartial(t *testing.T) {
	_, err := Run(Config{
		BaselinePath: filepath.Join(t.TempDir(), "baseline.json"),
	})
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_PARTIAL", te.Code)
	assert.Contains(t, te.Message, "no packages discovered")
}

func TestRun_MissingProfileFiles_SkipsSilently(t *testing.T) {
	// Missing files (not just empty paths) → silent skip → OPS_PARTIAL.
	_, err := Run(Config{
		BaselinePath:  filepath.Join(t.TempDir(), "baseline.json"),
		GoProfilePath: "/totally/missing/coverage.out",
		LCOVPath:      "/totally/missing/lcov.info",
	})
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_PARTIAL", te.Code,
		"missing files are skip-not-fail; combined empty triggers OPS_PARTIAL")
}

// =============================================================================
// Happy path: fresh baseline (no existing file)
// =============================================================================

func TestRun_FreshBaseline_GoProfileOnly(t *testing.T) {
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/bar.go:1.0,2.0 10 8
github.com/getloopscale/loop-runtime/pkg/bar/baz.go:1.0,2.0 10 5
`)
	out := filepath.Join(t.TempDir(), "baseline.json")
	res, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  out,
		Clock:         fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)
	assert.Equal(t, 2, res.PackagesDiscovered)
	assert.Equal(t, 2, res.PackagesWritten)
	assert.Equal(t, out, res.OutputPath)

	// File exists + parses.
	body, err := os.ReadFile(out)
	require.NoError(t, err)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(body, &loaded))
	assert.Equal(t, "1.0", loaded["schema_version"])
	assert.Equal(t, "2026-05-21T10:00:00Z", loaded["generated_at"])
	assert.Contains(t, loaded, "critical_packages")
	assert.Contains(t, loaded, "packages")
}

func TestRun_FreshBaseline_LCOVOnly(t *testing.T) {
	lcovPath := writeLCOV(t, `SF:src/cli/auth/login.ts
LF:100
LH:80
end_of_record
`)
	out := filepath.Join(t.TempDir(), "baseline.json")
	res, err := Run(Config{
		LCOVPath:     lcovPath,
		BaselinePath: out,
		Clock:        fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.PackagesDiscovered)
	assert.Equal(t, 1, res.PackagesWritten)

	body, _ := os.ReadFile(out)
	assert.Contains(t, string(body), `"src/cli/auth"`)
	assert.Contains(t, string(body), `"line": 80`)
}

func TestRun_FreshBaseline_BothSources_Combined(t *testing.T) {
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/bar.go:1.0,2.0 10 8
`)
	lcovPath := writeLCOV(t, `SF:src/cli/auth/login.ts
LF:100
LH:80
end_of_record
`)
	out := filepath.Join(t.TempDir(), "baseline.json")
	res, err := Run(Config{
		GoProfilePath: goPath,
		LCOVPath:      lcovPath,
		BaselinePath:  out,
		Clock:         fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)
	assert.Equal(t, 2, res.PackagesDiscovered, "1 Go + 1 lcov package")
	assert.Equal(t, 2, res.PackagesWritten)
}

// =============================================================================
// MergeUpdate path: existing baseline → preserve min_required
// =============================================================================

func TestRun_ExistingBaseline_PreservesMinRequired(t *testing.T) {
	out := filepath.Join(t.TempDir(), "baseline.json")
	// Seed existing baseline with min_required=75 for foo pkg.
	require.NoError(t, os.WriteFile(out, []byte(`{
  "packages": {
    "github.com/getloopscale/loop-runtime/pkg/foo": { "line": 70, "min_required": 75 }
  }
}`), 0o600))

	// New measurement: 9 covered + 1 uncovered = 90% — min_required
	// stays 75 (preserved across re-run).
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/bar.go:1.0,2.0 9 1
github.com/getloopscale/loop-runtime/pkg/foo/bar.go:3.0,4.0 1 0
`)
	_, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  out,
		Clock:         fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)

	body, _ := os.ReadFile(out)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(body, &loaded))
	pkgs := loaded["packages"].(map[string]any)
	foo := pkgs["github.com/getloopscale/loop-runtime/pkg/foo"].(map[string]any)
	assert.Equal(t, 90.0, foo["line"])
	assert.Equal(t, 75.0, foo["min_required"], "min_required preserved (idempotent)")
}

func TestRun_CriticalPackage_PinsTo90(t *testing.T) {
	out := filepath.Join(t.TempDir(), "baseline.json")
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/errors/foo.go:1.0,2.0 10 9
`)
	_, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  out,
		Clock:         fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)

	body, _ := os.ReadFile(out)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(body, &loaded))
	pkgs := loaded["packages"].(map[string]any)
	errPkg := pkgs["github.com/getloopscale/loop-runtime/pkg/errors"].(map[string]any)
	assert.Equal(t, 90.0, errPkg["min_required"], "critical pkg pinned to 90 even on fresh install")
}

// =============================================================================
// DryRun
// =============================================================================

func TestRun_DryRun_DoesNotWriteFile(t *testing.T) {
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/x.go:1.0,2.0 10 10
`)
	out := filepath.Join(t.TempDir(), "baseline.json")
	res, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  out,
		DryRun:        true,
		Clock:         fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)
	assert.Equal(t, "stdout", res.OutputPath)
	assert.NotEmpty(t, res.JSON)
	assert.True(t, strings.HasSuffix(string(res.JSON), "\n"))

	// File NOT written.
	_, err = os.Stat(out)
	assert.True(t, os.IsNotExist(err), "DryRun must NOT write baseline file")
}

func TestRun_DryRun_JSONShapeValid(t *testing.T) {
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/x.go:1.0,2.0 10 10
`)
	res, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  filepath.Join(t.TempDir(), "baseline.json"),
		DryRun:        true,
		Clock:         fixedClock("2026-05-21T10:00:00Z"),
	})
	require.NoError(t, err)

	// JSON parses + has all metadata fields.
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(res.JSON, &parsed))
	assert.Equal(t, "1.0", parsed["schema_version"])
	assert.Equal(t, "2026-05-21T10:00:00Z", parsed["generated_at"])
	assert.NotEmpty(t, parsed["critical_packages"])
	assert.NotEmpty(t, parsed["packages"])
}

// =============================================================================
// Failure modes
// =============================================================================

func TestRun_MalformedExistingBaseline_OpsUsage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "baseline.json")
	require.NoError(t, os.WriteFile(out, []byte("{not valid json"), 0o600))

	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/x.go:1.0,2.0 10 10
`)
	_, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  out,
	})
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestRun_DefaultClockUsedWhenNil(t *testing.T) {
	// Just verify nil clock doesn't crash; the actual timestamp is
	// time.Now() — non-deterministic, asserted only as RFC3339-ish.
	goPath := writeGoProfile(t, `mode: atomic
github.com/getloopscale/loop-runtime/pkg/x/y.go:1.0,2.0 10 10
`)
	out := filepath.Join(t.TempDir(), "baseline.json")
	_, err := Run(Config{
		GoProfilePath: goPath,
		BaselinePath:  out,
	})
	require.NoError(t, err)
	body, _ := os.ReadFile(out)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(body, &loaded))
	ts := loaded["generated_at"].(string)
	// RFC3339 has 'T' separator and 'Z' OR offset.
	assert.True(t, strings.Contains(ts, "T"))
}
