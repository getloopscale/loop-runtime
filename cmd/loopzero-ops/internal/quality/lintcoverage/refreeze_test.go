// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestRefreezeWritesWhatWasMeasuredEvenAfterARegression(t *testing.T) {
	root := repoWith(t, `{"lint":{"api":0},"gofmt":{"api":0}}`, "api")
	r := &fakeRunner{lint: map[string]int{"api": 3}}

	got, err := Refreeze(Config{RepoRoot: root, Runner: r})

	if err != nil || got.Verdict() != verdict.Pass {
		t.Fatalf("refreeze: err=%v verdict=%v", err, got.Verdict())
	}
	body, rerr := os.ReadFile(filepath.Join(root, BaselineRelPath))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(body), `"api": 3`) {
		t.Fatalf("the baseline must carry the measured count:\n%s", body)
	}
}

// brokenRunner measures nothing: every module fails in both dimensions.
type brokenRunner struct{}

func (brokenRunner) Lint(_, _ string) (int, error)  { return 0, errors.New("lint crashed") }
func (brokenRunner) Gofmt(_, _ string) (int, error) { return 0, errors.New("gofmt crashed") }

func TestRefreezeRefusesARunThatMeasuredNothing(t *testing.T) {
	root := repoWith(t, `{"lint":{},"gofmt":{}}`, "api")

	_, err := Refreeze(Config{RepoRoot: root, Runner: brokenRunner{}})

	if err == nil || !strings.Contains(err.Error(), "nothing measured") {
		t.Fatalf("err = %v, want the empty measurement refused", err)
	}
}
