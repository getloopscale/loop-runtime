// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package baseline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IsCritical + InitialMinRequired
// =============================================================================

func TestIsCritical_KnownPackages(t *testing.T) {
	for _, p := range CriticalPackages {
		assert.True(t, IsCritical(p), "should be critical: %s", p)
	}
}

func TestIsCritical_NonCritical(t *testing.T) {
	for _, p := range []string{
		"github.com/getloopscale/loop-runtime/pkg/randompkg",
		"some/other/path",
		"",
	} {
		assert.False(t, IsCritical(p), "should NOT be critical: %q", p)
	}
}

func TestInitialMinRequired_CriticalPinned90(t *testing.T) {
	// Even with low/high coverage, critical pkgs pin to 90.
	for _, cov := range []float64{0, 50, 75, 95, 100} {
		got := InitialMinRequired("github.com/getloopscale/loop-runtime/pkg/errors", cov)
		assert.Equal(t, 90.0, got, "critical pkg @ coverage=%.0f", cov)
	}
}

func TestInitialMinRequired_NonCritical_FloorCoverageMinus5(t *testing.T) {
	// 87.5 → floor(82.5) = 82
	assert.Equal(t, 82.0, InitialMinRequired("some/pkg", 87.5))
	// 65 → floor(60) = 60
	assert.Equal(t, 60.0, InitialMinRequired("some/pkg", 65.0))
}

func TestInitialMinRequired_FloorClampedAt50(t *testing.T) {
	// 30 → floor(25) = 25 → clamped to 50.
	assert.Equal(t, 50.0, InitialMinRequired("some/pkg", 30.0))
}

func TestInitialMinRequired_CeilingClampedAt90(t *testing.T) {
	// 99 → floor(94) = 94 → clamped to 90.
	assert.Equal(t, 90.0, InitialMinRequired("some/pkg", 99.0))
}

// =============================================================================
// Load + Save round-trip
// =============================================================================

func TestLoad_HappyPath(t *testing.T) {
	// JSON wire format uses "line" key (matches live infra/quality/
	// coverage-baseline.json + bash baseline-{compare,update}.sh).
	tmp := filepath.Join(t.TempDir(), "baseline.json")
	require.NoError(t, os.WriteFile(tmp, []byte(`{
  "packages": {
    "github.com/getloopscale/loop-runtime/pkg/errors": { "line": 92.0, "min_required": 90 },
    "github.com/getloopscale/loop-runtime/pkg/random":  { "line": 75.0, "min_required": 70 }
  }
}`), 0o600))

	b, err := Load(tmp)
	require.NoError(t, err)
	assert.Len(t, b.Packages, 2)
	assert.Equal(t, 92.0, b.Packages["github.com/getloopscale/loop-runtime/pkg/errors"].Coverage)
	assert.Equal(t, 70.0, b.Packages["github.com/getloopscale/loop-runtime/pkg/random"].MinRequired)
}

func TestLoad_MissingFile_OpsUsage(t *testing.T) {
	_, err := Load("/nonexistent/baseline.json")
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
	assert.Contains(t, te.Message, "cannot read")
}

func TestLoad_MalformedJSON_OpsUsage(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(tmp, []byte("{not valid json"), 0o600))
	_, err := Load(tmp)
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
	assert.Contains(t, te.Message, "not valid JSON")
}

func TestLoad_NilPackages_InitializedEmpty(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "empty.json")
	require.NoError(t, os.WriteFile(tmp, []byte(`{}`), 0o600))
	b, err := Load(tmp)
	require.NoError(t, err)
	assert.NotNil(t, b.Packages)
	assert.Empty(t, b.Packages)
}

func TestSave_RoundTrip(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "out.json")
	original := &Baseline{Packages: map[string]PackageBaseline{
		"a-pkg": {Coverage: 85.5, MinRequired: 80},
		"z-pkg": {Coverage: 70.0, MinRequired: 65},
	}}
	require.NoError(t, Save(tmp, original))

	body, err := os.ReadFile(tmp)
	require.NoError(t, err)
	// Trailing newline preserved.
	assert.True(t, len(body) > 0 && body[len(body)-1] == '\n')

	// JSON parses + matches.
	var loaded Baseline
	require.NoError(t, json.Unmarshal(body, &loaded))
	assert.Equal(t, original.Packages, loaded.Packages)
}

func TestSave_AlphabeticalKeyOrder(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "sorted.json")
	b := &Baseline{Packages: map[string]PackageBaseline{
		"z-late":   {Coverage: 90, MinRequired: 80},
		"a-first":  {Coverage: 91, MinRequired: 80},
		"m-middle": {Coverage: 92, MinRequired: 80},
	}}
	require.NoError(t, Save(tmp, b))

	body, _ := os.ReadFile(tmp)
	yaml := string(body)
	aIdx := indexOf(yaml, "a-first")
	mIdx := indexOf(yaml, "m-middle")
	zIdx := indexOf(yaml, "z-late")
	require.Positive(t, aIdx)
	require.Greater(t, mIdx, aIdx)
	require.Greater(t, zIdx, mIdx)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// =============================================================================
// PackageNames
// =============================================================================

func TestPackageNames_SortedAlphabetically(t *testing.T) {
	b := &Baseline{Packages: map[string]PackageBaseline{
		"z": {}, "a": {}, "m": {},
	}}
	assert.Equal(t, []string{"a", "m", "z"}, b.PackageNames())
}

func TestPackageNames_NilOrEmpty(t *testing.T) {
	assert.Nil(t, (*Baseline)(nil).PackageNames())
	assert.Nil(t, (&Baseline{}).PackageNames())
}

// =============================================================================
// Compare
// =============================================================================

func TestCompare_AllPackagesAboveMin_NoViolations(t *testing.T) {
	// baseline + current both above min, no regression.
	base := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 88, MinRequired: 80},
		"b": {Coverage: 78, MinRequired: 70},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 85}, // dipped 3pp but >= 88-3=85, also >= 80
		"b": {Coverage: 75}, // dipped 3pp but >= 78-3=75, also >= 70
	}}
	v := base.Compare(current, CompareOptions{})
	assert.Empty(t, v)
}

func TestCompare_BelowMinRequired_Violation(t *testing.T) {
	// Existing pkg: floor = max(min_required, baseline.line - slack).
	// baseline=80, min=80, slack=3 → floor = max(80, 77) = 80.
	// current=76 → 76 < 80 → violation.
	base := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 80, MinRequired: 80},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 76},
	}}
	v := base.Compare(current, CompareOptions{})
	require.Len(t, v, 1)
	assert.Equal(t, "a", v[0].Package)
	assert.Equal(t, 76.0, v[0].Current)
	assert.Equal(t, 80.0, v[0].MinRequired)
	assert.InDelta(t, 4.0, v[0].Drop, 0.001)
}

func TestCompare_BelowBaselineSlack_Violation(t *testing.T) {
	// Existing pkg with baseline.line ABOVE min_required.
	// baseline=95, min=80, slack=3 → floor = max(80, 95-3=92) = 92.
	// current=85: passes min_required (80) but dips MORE than slack
	// below baseline (95-85=10pp > 3pp slack) → violation.
	base := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 95, MinRequired: 80},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 85},
	}}
	v := base.Compare(current, CompareOptions{})
	require.Len(t, v, 1)
	assert.Equal(t, 85.0, v[0].Current)
	// Drop = floor - current = 92 - 85 = 7
	assert.InDelta(t, 7.0, v[0].Drop, 0.001)
}

func TestCompare_WithinBaselineSlack_NoViolation(t *testing.T) {
	// baseline=88, min=80, slack=3 → floor = max(80, 85) = 85.
	// current=86: above floor → no violation (within slack of baseline).
	base := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 88, MinRequired: 80},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 86},
	}}
	v := base.Compare(current, CompareOptions{})
	assert.Empty(t, v)
}

func TestCompare_NewPackageUsesDefaultMin(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"newpkg": {Coverage: 50},
	}}
	// Default min=80, slack=3 → threshold=77; current=50 → violation
	v := base.Compare(current, CompareOptions{})
	require.Len(t, v, 1)
	assert.Equal(t, "newpkg", v[0].Package)
	assert.Equal(t, 80.0, v[0].MinRequired)
}

func TestCompare_CustomDefaultsHonored(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"x": {Coverage: 55},
	}}
	// DefaultMin=60, slack=0 → threshold=60; current=55 → violation
	v := base.Compare(current, CompareOptions{DefaultMin: 60, SlackPP: 0.001 /* avoid auto-default */})
	require.Len(t, v, 1)
	assert.Equal(t, 60.0, v[0].MinRequired)
}

func TestCompare_NilCurrent_NoViolations(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{"a": {MinRequired: 80}}}
	assert.Empty(t, base.Compare(nil, CompareOptions{}))
}

func TestCompare_AlphabeticalOrder(t *testing.T) {
	// 3 violations sorted by package name.
	base := &Baseline{Packages: map[string]PackageBaseline{
		"z": {MinRequired: 80},
		"a": {MinRequired: 80},
		"m": {MinRequired: 80},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"z": {Coverage: 50},
		"a": {Coverage: 60},
		"m": {Coverage: 55},
	}}
	v := base.Compare(current, CompareOptions{})
	require.Len(t, v, 3)
	assert.Equal(t, "a", v[0].Package)
	assert.Equal(t, "m", v[1].Package)
	assert.Equal(t, "z", v[2].Package)
}

// =============================================================================
// MergeUpdate
// =============================================================================

func TestMergeUpdate_ExistingPackage_PreservesMinRequired(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{
		"a-pkg": {Coverage: 80, MinRequired: 75},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"a-pkg": {Coverage: 95}, // coverage went up — min stays at 75
	}}
	merged := base.MergeUpdate(current)
	assert.Equal(t, 95.0, merged.Packages["a-pkg"].Coverage)
	assert.Equal(t, 75.0, merged.Packages["a-pkg"].MinRequired,
		"min_required preserved (idempotent)")
}

func TestMergeUpdate_NewPackage_SeedsMinFromCoverage(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"new-pkg": {Coverage: 87.5},
	}}
	merged := base.MergeUpdate(current)
	// floor(87.5 - 5) = 82
	assert.Equal(t, 82.0, merged.Packages["new-pkg"].MinRequired)
	assert.Equal(t, 87.5, merged.Packages["new-pkg"].Coverage)
}

func TestMergeUpdate_CriticalPackage_PinsTo90(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{
		"github.com/getloopscale/loop-runtime/pkg/errors": {Coverage: 95, MinRequired: 75 /* drifted */},
	}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"github.com/getloopscale/loop-runtime/pkg/errors": {Coverage: 92},
	}}
	merged := base.MergeUpdate(current)
	assert.Equal(t, 90.0, merged.Packages["github.com/getloopscale/loop-runtime/pkg/errors"].MinRequired,
		"critical pkg drifted min_required pinned back to 90")
}

func TestMergeUpdate_NewCriticalPackage_StartsAt90(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{}}
	current := &Baseline{Packages: map[string]PackageBaseline{
		"github.com/getloopscale/loop-runtime/pkg/errors": {Coverage: 88},
	}}
	merged := base.MergeUpdate(current)
	// Critical pkg, even with low coverage, starts at 90.
	assert.Equal(t, 90.0, merged.Packages["github.com/getloopscale/loop-runtime/pkg/errors"].MinRequired)
}

func TestMergeUpdate_NilCurrent_ReturnsCopyOfBase(t *testing.T) {
	base := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 80, MinRequired: 75},
	}}
	merged := base.MergeUpdate(nil)
	assert.Equal(t, base.Packages, merged.Packages)
	// Mutation safety: changing merged doesn't affect base.
	merged.Packages["a"] = PackageBaseline{Coverage: 0, MinRequired: 0}
	assert.Equal(t, 80.0, base.Packages["a"].Coverage)
}

func TestMergeUpdate_NilBase_HandlesGracefully(t *testing.T) {
	current := &Baseline{Packages: map[string]PackageBaseline{
		"a": {Coverage: 87.5},
	}}
	merged := (*Baseline)(nil).MergeUpdate(current)
	require.Len(t, merged.Packages, 1)
	assert.Equal(t, 82.0, merged.Packages["a"].MinRequired)
}
